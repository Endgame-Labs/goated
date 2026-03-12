package tmux

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultSession = "goat_main"
const defaultTarget = defaultSession + ":0.0"

// SessionExists checks if the goat_main tmux session is alive.
func SessionExists(ctx context.Context) bool {
	return run(ctx, "has-session", "-t", defaultSession) == nil
}

// PasteAndEnter writes text into the goat_main tmux pane and presses Enter.
// It snapshots the ❯ prompt line before pasting, then polls until the line
// changes (confirming the paste landed) before sending Enter.
func PasteAndEnter(ctx context.Context, text string) error {
	tmp, err := os.CreateTemp("", "goat-paste-*.txt")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, text); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Snapshot the prompt line before pasting
	promptBefore := findPromptLine(ctx)

	if err := run(ctx, "load-buffer", "-b", "goat", tmp.Name()); err != nil {
		return fmt.Errorf("load-buffer: %w", err)
	}
	if err := run(ctx, "paste-buffer", "-b", "goat", "-t", defaultTarget); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}

	// Poll until prompt line changes (paste arrived) or timeout
	_ = waitForPromptLineChange(ctx, promptBefore, 5*time.Second)

	if err := run(ctx, "send-keys", "-t", defaultTarget, "Enter"); err != nil {
		return fmt.Errorf("send enter: %w", err)
	}
	return nil
}

// CapturePane returns the full scrollback of the goat_main pane.
func CapturePane(ctx context.Context) (string, error) {
	return runOutput(ctx, "capture-pane", "-t", defaultTarget, "-p", "-S", "-")
}

// CaptureVisible returns only the visible portion of the pane (no scrollback).
func CaptureVisible(ctx context.Context) (string, error) {
	return runOutput(ctx, "capture-pane", "-t", defaultTarget, "-p")
}

// Run executes an arbitrary tmux command.
func Run(ctx context.Context, args ...string) error {
	return run(ctx, args...)
}

// RunOutput executes a tmux command and returns its output.
func RunOutput(ctx context.Context, args ...string) (string, error) {
	return runOutput(ctx, args...)
}

// findPromptLine returns the last line containing ❯ from the visible pane.
func findPromptLine(ctx context.Context) string {
	out, err := runOutput(ctx, "capture-pane", "-t", defaultTarget, "-p")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "❯") {
			return lines[i]
		}
	}
	return ""
}

// waitForPromptLineChange polls until the ❯ line differs from before.
func waitForPromptLineChange(ctx context.Context, before string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current := findPromptLine(ctx)
		if current != before {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for prompt line to change")
}

// WaitForIdle polls until Claude returns to the ❯ prompt.
// Returns nil on success, error on timeout or context cancellation.
func WaitForIdle(ctx context.Context, timeout time.Duration) error {
	// Give Claude a moment to start processing
	time.Sleep(3 * time.Second)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := CaptureVisible(ctx)
		if err == nil {
			lines := strings.Split(strings.TrimRight(out, "\n "), "\n")
			for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
				if strings.Contains(lines[i], "❯") {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for Claude to return to prompt")
}

// API/transport error patterns that indicate a failed request (retryable).
var retryableErrors = []string{
	"API Error: 500",
	"API Error: 502",
	"API Error: 503",
	"API Error: 529",
	"Internal server error",
	"overloaded_error",
}

// CheckPaneForError examines the last N lines of the pane for API errors
// that appeared after a message was sent. Returns the matched error string,
// or empty if no error found.
func CheckPaneForError(ctx context.Context) string {
	out, err := CaptureVisible(ctx)
	if err != nil {
		return ""
	}
	lines := strings.Split(out, "\n")
	// Check last 15 lines (enough to cover the error + prompt return)
	start := 0
	if len(lines) > 15 {
		start = len(lines) - 15
	}
	tail := strings.Join(lines[start:], "\n")

	for _, pat := range retryableErrors {
		if strings.Contains(tail, pat) {
			return pat
		}
	}
	return ""
}

func run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
