package claudetui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"goated/internal/agent"
	"goated/internal/tmux"
)

type TmuxBridge struct {
	WorkspaceDir string
	LogDir       string
	SessionName  string
}

func NewSessionRuntime(workspaceDir, logDir string) *TmuxBridge {
	return &TmuxBridge{
		WorkspaceDir: workspaceDir,
		LogDir:       logDir,
		SessionName:  "goat_claude_tui_main",
	}
}

func (b *TmuxBridge) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{
		Provider:    agent.RuntimeClaudeTUI,
		DisplayName: "Claude Code TUI",
		SessionName: b.sessionName(),
		Capabilities: agent.Capabilities{
			SupportsInteractiveSession: true,
			SupportsContextEstimate:    true,
			SupportsCompaction:         true,
			SupportsReset:              true,
		},
	}
}

func (b *TmuxBridge) SendAndWait(ctx context.Context, channel, chatID string, userPrompt string, _ time.Duration) error {
	return b.SendUserPrompt(ctx, channel, chatID, userPrompt, nil, "", "")
}

func (b *TmuxBridge) SendUserPrompt(ctx context.Context, channel, chatID string, userPrompt string, attachments *agent.MessageAttachments, messageID, threadID string) error {
	if err := b.EnsureSession(ctx); err != nil {
		return err
	}

	wrapped := agent.BuildPromptEnvelope(channel, chatID, userPrompt, attachments, messageID, threadID)
	return tmux.PasteAndEnterFor(ctx, b.sessionName(), wrapped)
}

// IsSessionBusy returns true if Claude is not idle. Uses content-change
// detection (two captures 2s apart) rather than a single ❯ check, because
// the prompt is often visible even while Claude is actively working.
func (b *TmuxBridge) IsSessionBusy(ctx context.Context) (bool, error) {
	state, err := b.GetSessionState(ctx)
	if err != nil {
		return true, err
	}
	return state.Busy(), nil
}

func (b *TmuxBridge) WaitForAwaitingInput(ctx context.Context, timeout time.Duration) (agent.SessionState, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := b.GetSessionState(ctx)
		if err == nil && state.SafeIdle() {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return agent.SessionState{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return agent.SessionState{}, fmt.Errorf("timed out waiting for Claude session to become idle")
}

func (b *TmuxBridge) EnsureSession(ctx context.Context) error {
	if err := os.MkdirAll(b.WorkspaceDir, 0o755); err != nil {
		return fmt.Errorf("mkdir workspace dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(b.LogDir, "telegram"), 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}

	session := b.sessionName()
	created := false
	if !tmux.SessionExistsFor(ctx, session) {
		cmd := fmt.Sprintf("cd %q && unset CLAUDECODE && claude --dangerously-skip-permissions", b.WorkspaceDir)
		if err := tmux.Run(ctx, "new-session", "-d", "-s", session, cmd); err != nil {
			return fmt.Errorf("start claude tmux session: %w", err)
		}
		created = true
	}
	if created {
		if err := waitForClaudeReadyFor(ctx, session, 25*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (b *TmuxBridge) ClearSession(ctx context.Context, _ string) error {
	_, err := b.ResetConversation(ctx, "")
	return err
}

func (b *TmuxBridge) ResetConversation(ctx context.Context, _ string) (agent.ResetResult, error) {
	if err := b.StopSession(ctx); err != nil {
		return agent.ResetResult{}, err
	}
	if err := b.EnsureSession(ctx); err != nil {
		return agent.ResetResult{}, err
	}
	return agent.ResetResult{
		Scope:   agent.ResetScopeHard,
		Summary: "Started a fresh Claude Code session.",
	}, nil
}

// ContextUsagePercent pastes /context into the Claude Code session and parses
// the real token usage percentage from the output. Polls for the regex pattern
// directly rather than relying on idle detection.
func (b *TmuxBridge) ContextUsagePercent(_ string) int {
	estimate, err := b.GetContextEstimate(context.Background(), "")
	if err != nil || estimate.State != agent.ContextEstimateKnown {
		return -1
	}
	return estimate.PercentUsed
}

func (b *TmuxBridge) GetContextEstimate(parent context.Context, _ string) (agent.ContextEstimate, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	state, err := b.GetSessionState(ctx)
	if err != nil || !state.SafeIdle() {
		return agent.ContextEstimate{
			State:      agent.ContextEstimateUnknown,
			RawSummary: "session is busy",
		}, nil
	}

	// Snapshot pane before pasting so we can detect new output
	before, _ := tmux.CaptureVisibleFor(ctx, b.sessionName())

	if err := b.SendControlCommand(ctx, "/context"); err != nil {
		return agent.ContextEstimate{}, err
	}

	// Poll until the context output pattern appears in the pane.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return agent.ContextEstimate{}, ctx.Err()
		default:
		}
		time.Sleep(2 * time.Second)
		out, err := tmux.CaptureVisibleFor(ctx, b.sessionName())
		if err != nil {
			continue
		}
		if out == before {
			continue
		}
		if pct := parseContextOutput(out); pct >= 0 {
			return agent.ContextEstimate{
				State:       agent.ContextEstimateKnown,
				PercentUsed: pct,
				RawSummary:  summarizeContextLine(out),
			}, nil
		}
	}
	return agent.ContextEstimate{
		State:      agent.ContextEstimateUnknown,
		RawSummary: "unable to parse /context output",
	}, nil
}

// contextPctRe matches the summary line from /context output:
//
//	"claude-opus-4-6 · 85k/200k tokens (42%)"
var contextPctRe = regexp.MustCompile(`[\d.]+k/[\d.]+k\s+tokens\s+\((\d+)%\)`)

func summarizeContextLine(output string) string {
	m := contextPctRe.FindString(output)
	if m == "" {
		return "unable to parse /context output"
	}
	return m
}

func parseContextOutput(output string) int {
	if m := contextPctRe.FindStringSubmatch(output); len(m) > 1 {
		pct, _ := strconv.Atoi(m[1])
		return pct
	}
	return -1
}

func (b *TmuxBridge) GetSessionState(ctx context.Context) (agent.SessionState, error) {
	if !tmux.SessionExistsFor(ctx, b.sessionName()) {
		return agent.SessionState{
			Kind:    agent.SessionStateDead,
			Summary: "no tmux session",
		}, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	stateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snap1, err := tmux.CaptureVisibleFor(stateCtx, b.sessionName())
	if err != nil {
		return agent.SessionState{
			Kind:    agent.SessionStateDead,
			Summary: err.Error(),
		}, nil
	}
	time.Sleep(2 * time.Second)
	snap2, err := tmux.CaptureVisibleFor(stateCtx, b.sessionName())
	if err != nil {
		return agent.SessionState{
			Kind:    agent.SessionStateDead,
			Summary: err.Error(),
		}, nil
	}

	tail := lastLines(snap2, 20)
	switch {
	case strings.Contains(tail, "Please run /login"),
		strings.Contains(tail, "OAuth token has expired"),
		strings.Contains(tail, "authentication_error"):
		return agent.SessionState{
			Kind:    agent.SessionStateBlockedIntervene,
			Summary: "Claude Code requires manual login",
		}, nil
	case snap1 == snap2 && tmux.HasPrompt(snap2):
		return agent.SessionState{
			Kind:    agent.SessionStateAwaitingInput,
			Summary: "idle at prompt",
		}, nil
	case snap1 == snap2:
		return agent.SessionState{
			Kind:    agent.SessionStateUnknownStable,
			Summary: "pane is stable without a prompt",
		}, nil
	default:
		return agent.SessionState{
			Kind:    agent.SessionStateGenerating,
			Summary: "processing",
		}, nil
	}
}

// SessionHealthy checks if the Claude Code session is in a usable state.
// Returns an error describing the problem if unhealthy, nil if OK.
func (b *TmuxBridge) SessionHealthy(ctx context.Context) error {
	health, err := b.GetHealth(ctx)
	if err != nil {
		return err
	}
	if !health.OK {
		return fmt.Errorf("%s", health.Summary)
	}
	return nil
}

func (b *TmuxBridge) GetHealth(ctx context.Context) (agent.HealthStatus, error) {
	session := b.sessionName()
	if !tmux.SessionExistsFor(ctx, session) {
		return agent.HealthStatus{
			OK:          false,
			Recoverable: true,
			Summary:     "no tmux session",
		}, nil
	}

	snap, err := tmux.CapturePaneFor(ctx, session)
	if err != nil {
		return agent.HealthStatus{
			OK:          false,
			Recoverable: true,
			Summary:     fmt.Sprintf("cannot capture pane: %v", err),
		}, nil
	}

	tail := lastLines(snap, 20)
	errorPatterns := []string{
		"API Error: 401",
		"authentication_error",
		"OAuth token has expired",
		"Please run /login",
		"API Error: 403",
		"overloaded_error",
		"Could not connect",
	}
	for _, pat := range errorPatterns {
		if strings.Contains(tail, pat) {
			recoverable := true
			if pat == "API Error: 401" || pat == "authentication_error" || pat == "OAuth token has expired" || pat == "Please run /login" {
				recoverable = false
			}
			return agent.HealthStatus{
				OK:          false,
				Recoverable: recoverable,
				Summary:     fmt.Sprintf("session error: %s", pat),
			}, nil
		}
	}

	return agent.HealthStatus{
		OK:          true,
		Recoverable: true,
		Summary:     "ok",
	}, nil
}

// RestartSession kills the existing session and starts a fresh one.
func (b *TmuxBridge) RestartSession(ctx context.Context) error {
	session := b.sessionName()
	_ = tmux.Run(ctx, "kill-session", "-t", session)
	// Small delay to let the process clean up
	time.Sleep(2 * time.Second)
	return b.EnsureSession(ctx)
}

func (b *TmuxBridge) sessionName() string {
	if b.SessionName != "" {
		return b.SessionName
	}
	return "goat_claude_tui_main"
}

// SendRaw pastes arbitrary text into the tmux session and presses Enter.
// Unlike SendAndWait, it does not wrap the text in a prompt envelope.
func (b *TmuxBridge) SendRaw(ctx context.Context, text string) error {
	return b.SendControlCommand(ctx, text)
}

func (b *TmuxBridge) SendControlCommand(ctx context.Context, text string) error {
	if err := b.EnsureSession(ctx); err != nil {
		return err
	}
	return tmux.PasteAndEnterFor(ctx, b.sessionName(), text)
}

func (b *TmuxBridge) DetectRetryableError(ctx context.Context) string {
	return tmux.CheckPaneForErrorFor(ctx, b.sessionName())
}

func (b *TmuxBridge) Version(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "claude", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func waitForClaudeReady(ctx context.Context, timeout time.Duration) error {
	return waitForClaudeReadyFor(ctx, "goat_claude_tui_main", timeout)
}

func waitForClaudeReadyFor(ctx context.Context, session string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := tmux.CapturePaneFor(ctx, session)
		if err == nil {
			if strings.Contains(out, "Claude Code") && strings.Contains(out, "❯") {
				return nil
			}
		}
		time.Sleep(350 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for Claude session readiness")
}

func (b *TmuxBridge) StopSession(ctx context.Context) error {
	session := b.sessionName()
	if err := tmux.Run(ctx, "kill-session", "-t", session); err != nil {
		if strings.Contains(err.Error(), "can't find session") || strings.Contains(err.Error(), "no server running") {
			return nil
		}
		return err
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	return strings.Join(lines[start:], "\n")
}
