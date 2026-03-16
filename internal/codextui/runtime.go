package codextui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"goated/internal/agent"
	"goated/internal/tmux"
	"goated/internal/util"
)

var (
	statusPercentRe = regexp.MustCompile(`(?i)(?:context window|token usage):\s*(\d+)%\s+used`)
	readyPromptRe   = regexp.MustCompile(`(?m)^\s*[>›❯]\s`)
)

type SessionRuntime struct {
	WorkspaceDir string
	LogDir       string
	SessionName  string

	mu         sync.Mutex
	lastSendAt time.Time
}

func NewSessionRuntime(workspaceDir, logDir string) *SessionRuntime {
	return &SessionRuntime{
		WorkspaceDir: workspaceDir,
		LogDir:       logDir,
		SessionName:  "goat_codex_tui_main",
	}
}

func (s *SessionRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{
		Provider:    agent.RuntimeCodexTUI,
		DisplayName: "Codex TUI",
		SessionName: s.sessionName(),
		Capabilities: agent.Capabilities{
			SupportsInteractiveSession: true,
			SupportsContextEstimate:    true,
			SupportsCompaction:         true,
			SupportsReset:              true,
		},
	}
}

func (s *SessionRuntime) EnsureSession(ctx context.Context) error {
	if err := os.MkdirAll(s.WorkspaceDir, 0o755); err != nil {
		return fmt.Errorf("mkdir workspace dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(s.LogDir, "telegram"), 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	if !fileExists(filepath.Join(s.WorkspaceDir, "GOATED.md")) {
		return fmt.Errorf("missing %s", filepath.Join(s.WorkspaceDir, "GOATED.md"))
	}

	session := s.sessionName()
	if !tmux.SessionExistsFor(ctx, session) {
		cmd := fmt.Sprintf(
			`cd %q && export LOG_CALLER=main-session && codex --no-alt-screen --sandbox danger-full-access --ask-for-approval never -c 'model_instructions_file="GOATED.md"'`,
			s.WorkspaceDir,
		)
		if err := tmux.Run(ctx, "new-session", "-d", "-s", session, cmd); err != nil {
			return fmt.Errorf("start codex tmux session: %w", err)
		}
	}
	return s.waitForReady(ctx, 25*time.Second)
}

func (s *SessionRuntime) StopSession(ctx context.Context) error {
	if err := tmux.Run(ctx, "kill-session", "-t", s.sessionName()); err != nil {
		if strings.Contains(err.Error(), "can't find session") || strings.Contains(err.Error(), "no server running") {
			return nil
		}
		return err
	}
	return nil
}

func (s *SessionRuntime) RestartSession(ctx context.Context) error {
	if err := s.StopSession(ctx); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return s.EnsureSession(ctx)
}

func (s *SessionRuntime) ResetConversation(ctx context.Context, _ string) (agent.ResetResult, error) {
	if err := s.RestartSession(ctx); err != nil {
		return agent.ResetResult{}, err
	}
	return agent.ResetResult{
		Scope:   agent.ResetScopeHard,
		Summary: "Started a fresh Codex session.",
	}, nil
}

func (s *SessionRuntime) SendUserPrompt(ctx context.Context, channel, chatID, userPrompt string, _ *agent.MessageAttachments, messageID, threadID string) error {
	if err := s.EnsureSession(ctx); err != nil {
		return err
	}
	s.markSend()
	return tmux.PasteAndEnterFor(ctx, s.sessionName(), agent.BuildPromptEnvelope(channel, chatID, userPrompt, nil, messageID, threadID))
}

func (s *SessionRuntime) SendControlCommand(ctx context.Context, text string) error {
	if err := s.EnsureSession(ctx); err != nil {
		return err
	}
	s.markSend()
	return tmux.PasteAndEnterFor(ctx, s.sessionName(), text)
}

func (s *SessionRuntime) GetContextEstimate(parent context.Context, _ string) (agent.ContextEstimate, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	state, err := s.GetSessionState(ctx)
	if err != nil || !state.SafeIdle() {
		return agent.ContextEstimate{
			State:      agent.ContextEstimateUnknown,
			RawSummary: "session is busy",
		}, nil
	}

	before, _ := tmux.CaptureVisibleFor(ctx, s.sessionName())
	if err := s.SendControlCommand(ctx, "/status"); err != nil {
		return agent.ContextEstimate{}, err
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return agent.ContextEstimate{}, ctx.Err()
		default:
		}
		time.Sleep(2 * time.Second)
		out, err := tmux.CaptureVisibleFor(ctx, s.sessionName())
		if err != nil || out == before {
			continue
		}
		pct, raw := parseStatusEstimate(util.CleanTerminalText(out))
		if pct >= 0 {
			return agent.ContextEstimate{
				State:       agent.ContextEstimateKnown,
				PercentUsed: pct,
				RawSummary:  raw,
			}, nil
		}
	}

	return agent.ContextEstimate{
		State:      agent.ContextEstimateUnknown,
		RawSummary: "unable to parse /status output",
	}, nil
}

func (s *SessionRuntime) GetSessionState(ctx context.Context) (agent.SessionState, error) {
	if !tmux.SessionExistsFor(ctx, s.sessionName()) {
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

	snap1, err := tmux.CaptureVisibleFor(stateCtx, s.sessionName())
	if err != nil {
		return agent.SessionState{
			Kind:    agent.SessionStateDead,
			Summary: err.Error(),
		}, nil
	}
	time.Sleep(2 * time.Second)
	snap2, err := tmux.CaptureVisibleFor(stateCtx, s.sessionName())
	if err != nil {
		return agent.SessionState{
			Kind:    agent.SessionStateDead,
			Summary: err.Error(),
		}, nil
	}

	clean1 := util.CleanTerminalText(snap1)
	clean2 := util.CleanTerminalText(snap2)
	stable := clean1 == clean2

	switch {
	case isBlockedAuth(clean2):
		return agent.SessionState{
			Kind:    agent.SessionStateBlockedAuth,
			Summary: "Codex requires sign-in or API key setup",
		}, nil
	case isBlockedIntervention(clean2):
		return agent.SessionState{
			Kind:    agent.SessionStateBlockedIntervene,
			Summary: "Codex is waiting for manual intervention",
		}, nil
	case stable && readyPromptRe.MatchString(clean2):
		return agent.SessionState{
			Kind:    agent.SessionStateAwaitingInput,
			Summary: "idle at prompt",
		}, nil
	case stable && s.recentlySent(6*time.Second):
		return agent.SessionState{
			Kind:    agent.SessionStateGenerating,
			Summary: "waiting for Codex to react to the last command",
		}, nil
	case stable:
		return agent.SessionState{
			Kind:    agent.SessionStateUnknownStable,
			Summary: "pane is stable but readiness is unknown",
		}, nil
	default:
		return agent.SessionState{
			Kind:    agent.SessionStateGenerating,
			Summary: "processing",
		}, nil
	}
}

func (s *SessionRuntime) WaitForAwaitingInput(ctx context.Context, timeout time.Duration) (agent.SessionState, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := s.GetSessionState(ctx)
		if err != nil {
			return agent.SessionState{}, err
		}
		switch state.Kind {
		case agent.SessionStateAwaitingInput:
			return state, nil
		case agent.SessionStateBlockedAuth, agent.SessionStateBlockedIntervene:
			return state, nil
		}

		select {
		case <-ctx.Done():
			return agent.SessionState{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return agent.SessionState{}, fmt.Errorf("timed out waiting for Codex session to become idle")
}

func (s *SessionRuntime) GetHealth(ctx context.Context) (agent.HealthStatus, error) {
	if !tmux.SessionExistsFor(ctx, s.sessionName()) {
		return agent.HealthStatus{
			OK:          false,
			Recoverable: true,
			Summary:     "no tmux session",
		}, nil
	}
	out, err := tmux.CapturePaneFor(ctx, s.sessionName())
	if err != nil {
		return agent.HealthStatus{
			OK:          false,
			Recoverable: true,
			Summary:     fmt.Sprintf("cannot capture pane: %v", err),
		}, nil
	}
	clean := util.CleanTerminalText(out)
	switch {
	case isBlockedAuth(clean):
		return agent.HealthStatus{
			OK:          false,
			Recoverable: false,
			Summary:     "Codex requires sign-in or API key setup",
		}, nil
	case isBlockedIntervention(clean):
		return agent.HealthStatus{
			OK:          false,
			Recoverable: false,
			Summary:     "Codex is waiting for manual intervention",
		}, nil
	default:
		return agent.HealthStatus{
			OK:          true,
			Recoverable: true,
			Summary:     "ok",
		}, nil
	}
}

func (s *SessionRuntime) DetectRetryableError(ctx context.Context) string {
	out, err := tmux.CaptureVisibleFor(ctx, s.sessionName())
	if err != nil {
		return ""
	}
	tail := lastLines(util.CleanTerminalText(out), 20)
	for _, pat := range []string{
		"Internal server error",
		"server_error",
		"overloaded",
		"Too Many Requests",
		"status 500",
		"status 502",
		"status 503",
		"status 529",
	} {
		if strings.Contains(tail, pat) {
			return pat
		}
	}
	return ""
}

func (s *SessionRuntime) Version(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "codex", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *SessionRuntime) waitForReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	stableCount := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := tmux.CaptureVisibleFor(ctx, s.sessionName())
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		clean := util.CleanTerminalText(out)
		switch {
		case isBlockedAuth(clean):
			return fmt.Errorf("Codex requires sign-in or API key setup")
		case isBlockedIntervention(clean):
			return fmt.Errorf("Codex is waiting for manual intervention")
		case clean == "":
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if clean == last {
			stableCount++
			if stableCount >= 2 {
				return nil
			}
		} else {
			last = clean
			stableCount = 0
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for Codex session readiness")
}

func (s *SessionRuntime) markSend() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSendAt = time.Now()
}

func (s *SessionRuntime) recentlySent(within time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSendAt.IsZero() {
		return false
	}
	return time.Since(s.lastSendAt) < within
}

func (s *SessionRuntime) sessionName() string {
	if s.SessionName != "" {
		return s.SessionName
	}
	return "goat_codex_tui_main"
}

func parseStatusEstimate(output string) (int, string) {
	m := statusPercentRe.FindStringSubmatch(output)
	if len(m) < 2 {
		return -1, "unable to parse /status output"
	}
	return atoiSafe(m[1]), statusPercentRe.FindString(output)
}

func atoiSafe(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func isBlockedAuth(s string) bool {
	for _, pat := range []string{
		"Welcome to Codex",
		"Sign in with ChatGPT",
		"Provide your own API key",
		"Press Enter to continue",
		"connect an API key",
	} {
		if strings.Contains(s, pat) {
			return true
		}
	}
	return false
}

func isBlockedIntervention(s string) bool {
	for _, pat := range []string{
		"waiting for approval",
		"allow this action",
		"manual intervention",
	} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(pat)) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	return strings.Join(lines[start:], "\n")
}
