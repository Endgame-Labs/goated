package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goated/internal/tmux"
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
	return tmux.PasteAndEnter(ctx, wrapped)
}

// IsSessionBusy returns true if Claude is not at the ❯ prompt (i.e., working).
func (b *TmuxBridge) IsSessionBusy(ctx context.Context) (bool, error) {
	snap, err := tmux.CapturePane(ctx)
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
func (b *TmuxBridge) waitForIdleOrStall(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	var lastSnap string
	unchangedCount := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		snap, err := tmux.CapturePane(ctx)
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
	if err := tmux.Run(ctx, "has-session", "-t", session); err != nil {
		cmd := fmt.Sprintf("cd %q && unset CLAUDECODE && claude --dangerously-skip-permissions", b.WorkspaceDir)
		if err := tmux.Run(ctx, "new-session", "-d", "-s", session, cmd); err != nil {
			return fmt.Errorf("start claude tmux session: %w", err)
		}
		created = true
	}
	if created {
		if err := waitForClaudeReady(ctx, 25*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (b *TmuxBridge) ClearSession(ctx context.Context, _ string) error {
	session := b.sessionName()
	_ = tmux.Run(ctx, "kill-session", "-t", session)
	return b.EnsureSession(ctx)
}

func (b *TmuxBridge) ContextUsagePercent(_ string) int {
	// Rough estimate from scrollback size
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := tmux.CapturePane(ctx)
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
	if err := tmux.Run(ctx, "has-session", "-t", session); err != nil {
		return fmt.Errorf("no tmux session")
	}

	snap, err := tmux.CapturePane(ctx)
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
	_ = tmux.Run(ctx, "kill-session", "-t", session)
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
	return tmux.PasteAndEnter(ctx, text)
}

func buildPromptEnvelope(channel, chatID, userPrompt string) string {
	return fmt.Sprintf(`<user-message source=%q chat_id=%q>
%s
</user-message>

<instructions>
Respond to the user by piping your markdown response into:
  ./goat send_user_message --chat %s

See GOATED_CLI_README.md for formatting details.
</instructions>
`, channel, chatID, strings.TrimSpace(userPrompt), chatID)
}

func waitForClaudeReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := tmux.CapturePane(ctx)
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
