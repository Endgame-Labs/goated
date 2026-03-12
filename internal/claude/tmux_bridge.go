package claude

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TmuxBridge struct {
	WorkspaceDir        string
	LogDir              string
	ContextWindowTokens int
}

func (b *TmuxBridge) SendAndWait(ctx context.Context, channel, chatID string, userPrompt string, _ time.Duration) error {
	if err := b.EnsureSession(ctx); err != nil {
		return err
	}

	wrapped := buildPromptEnvelope(channel, chatID, userPrompt)
	return b.sendKeys(ctx, wrapped)
}

// IsSessionBusy returns true if Claude is not at the ❯ prompt (i.e., working).
func (b *TmuxBridge) IsSessionBusy(ctx context.Context) (bool, error) {
	target := b.sessionName() + ":0.0"
	snap, err := capturePane(ctx, target)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimRight(snap, "\n "), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
		if strings.Contains(lines[i], "❯") {
			return false, nil
		}
	}
	return true, nil
}

// waitForIdleOrStall waits up to timeout for Claude to return to ❯.
// Returns true if it finished, false if the pane stopped changing (stalled).
func (b *TmuxBridge) waitForIdleOrStall(ctx context.Context, target string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	var lastSnap string
	unchangedCount := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		snap, err := capturePane(ctx, target)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		// Check if Claude returned to prompt
		lines := strings.Split(strings.TrimRight(snap, "\n "), "\n")
		for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
			if strings.Contains(lines[i], "❯") {
				return true
			}
		}

		// Track whether the pane is changing
		if snap == lastSnap {
			unchangedCount++
			// 30 seconds of no change = stalled
			if unchangedCount >= 10 {
				return false
			}
		} else {
			unchangedCount = 0
			lastSnap = snap
		}

		time.Sleep(3 * time.Second)
	}
	return false
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
	if err := runTmux(ctx, "has-session", "-t", session); err != nil {
		cmd := fmt.Sprintf("cd %q && unset CLAUDECODE && claude --dangerously-skip-permissions", b.WorkspaceDir)
		if err := runTmux(ctx, "new-session", "-d", "-s", session, cmd); err != nil {
			return fmt.Errorf("start claude tmux session: %w", err)
		}
		created = true
	}
	if created {
		if err := waitForClaudeReady(ctx, session+":0.0", 25*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (b *TmuxBridge) ClearSession(ctx context.Context, _ string) error {
	session := b.sessionName()
	_ = runTmux(ctx, "kill-session", "-t", session)
	return b.EnsureSession(ctx)
}

func (b *TmuxBridge) ContextUsagePercent(_ string) int {
	// Rough estimate from scrollback size
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	target := b.sessionName() + ":0.0"
	out, err := capturePane(ctx, target)
	if err != nil {
		return 0
	}
	estTokens := len(out) / 4
	if b.ContextWindowTokens <= 0 {
		return 0
	}
	pct := estTokens * 100 / b.ContextWindowTokens
	if pct > 100 {
		return 100
	}
	return pct
}

// SessionHealthy checks if the Claude Code session is in a usable state.
// Returns an error describing the problem if unhealthy, nil if OK.
func (b *TmuxBridge) SessionHealthy(ctx context.Context) error {
	session := b.sessionName()
	if err := runTmux(ctx, "has-session", "-t", session); err != nil {
		return fmt.Errorf("no tmux session")
	}

	target := session + ":0.0"
	snap, err := capturePane(ctx, target)
	if err != nil {
		return fmt.Errorf("cannot capture pane: %w", err)
	}

	// Check last ~20 lines for error indicators
	lines := strings.Split(snap, "\n")
	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}
	tail := strings.Join(lines[start:], "\n")

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
			return fmt.Errorf("session error: %s", pat)
		}
	}

	return nil
}

// RestartSession kills the existing session and starts a fresh one.
func (b *TmuxBridge) RestartSession(ctx context.Context) error {
	session := b.sessionName()
	_ = runTmux(ctx, "kill-session", "-t", session)
	// Small delay to let the process clean up
	time.Sleep(2 * time.Second)
	return b.EnsureSession(ctx)
}

func (b *TmuxBridge) sessionName() string {
	return "goat_main"
}

// SendRaw pastes arbitrary text into the tmux session and presses Enter.
// Unlike SendAndWait, it does not wrap the text in a prompt envelope.
func (b *TmuxBridge) SendRaw(ctx context.Context, text string) error {
	return b.sendKeys(ctx, text)
}

func (b *TmuxBridge) sendKeys(ctx context.Context, prompt string) error {
	tmp, err := os.CreateTemp("", "goat-prompt-*.txt")
	if err != nil {
		return fmt.Errorf("create temp prompt: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, prompt); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp prompt: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp prompt: %w", err)
	}

	target := b.sessionName() + ":0.0"
	if err := runTmux(ctx, "load-buffer", "-b", "goat", tmp.Name()); err != nil {
		return fmt.Errorf("load-buffer: %w", err)
	}
	if err := runTmux(ctx, "paste-buffer", "-b", "goat", "-t", target); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}
	// Wait until Claude Code's input box shows the pasted text
	firstLine := strings.SplitN(prompt, "\n", 2)[0]
	if err := waitForPaneContains(ctx, target, firstLine, 5*time.Second); err != nil {
		return fmt.Errorf("paste not received: %w", err)
	}
	if err := runTmux(ctx, "send-keys", "-t", target, "Enter"); err != nil {
		return fmt.Errorf("send enter: %w", err)
	}
	return nil
}

// waitForPromptReturn polls capture-pane until Claude returns to the interactive prompt (❯).
func (b *TmuxBridge) waitForPromptReturn(ctx context.Context, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Give Claude a moment to start processing before we check for the prompt
	time.Sleep(3 * time.Second)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for claude response")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		snap, err := capturePane(ctx, target)
		if err == nil {
			// Claude Code shows ❯ when it's ready for input.
			// Check the last few non-empty lines for the prompt character.
			// Ignore lines that contain "queued messages" — that's Claude Code's
			// multi-input queue, not an idle prompt.
			lines := strings.Split(strings.TrimRight(snap, "\n "), "\n")
			for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
				line := lines[i]
				if strings.Contains(line, "❯") && !strings.Contains(line, "queued") {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// capturePane returns the full scrollback of a tmux pane as clean text.
func capturePane(ctx context.Context, target string) (string, error) {
	return runTmuxOutput(ctx, "capture-pane", "-t", target, "-p", "-S", "-")
}

func buildPromptEnvelope(channel, chatID, userPrompt string) string {
	return fmt.Sprintf(`User message via %s (chat_id=%s):
%s

Respond to the user by piping your markdown response into:
  ./goat send_user_message --chat %s

See GOATED_CLI_README.md for formatting details.
`, channel, chatID, strings.TrimSpace(userPrompt), chatID)
}

func runTmux(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runTmuxOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func waitForPaneContains(ctx context.Context, target, needle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := capturePane(ctx, target)
		if err == nil && strings.Contains(out, needle) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %q in pane", needle)
}

func waitForClaudeReady(ctx context.Context, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := capturePane(ctx, target)
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
	if err := runTmux(ctx, "kill-session", "-t", session); err != nil {
		if strings.Contains(err.Error(), "can't find session") || strings.Contains(err.Error(), "no server running") {
			return nil
		}
		return err
	}
	return nil
}
