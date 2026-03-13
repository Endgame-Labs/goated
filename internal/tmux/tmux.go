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

const defaultSession = "goat_claude_main"

func targetForSession(session string) string {
	return session + ":0.0"
}

// SessionExists checks if the default tmux session is alive.
func SessionExists(ctx context.Context) bool {
	return SessionExistsFor(ctx, defaultSession)
}

// SessionExistsFor checks if the given tmux session is alive.
func SessionExistsFor(ctx context.Context, session string) bool {
	return run(ctx, "has-session", "-t", session) == nil
}

// PasteAndEnter writes text into the default tmux pane and presses Enter.
// It snapshots the visible pane before pasting, then polls until the pane
// changes (confirming the paste landed) before sending Enter.
func PasteAndEnter(ctx context.Context, text string) error {
	return PasteAndEnterFor(ctx, defaultSession, text)
}

// PasteAndEnterFor writes text into the given tmux session pane and presses
// Enter after the pasted text becomes visible.
func PasteAndEnterFor(ctx context.Context, session, text string) error {
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

	// Snapshot the visible pane before pasting
	visibleBefore, _ := CaptureVisibleFor(ctx, session)

	if err := run(ctx, "load-buffer", "-b", "goat", tmp.Name()); err != nil {
		return fmt.Errorf("load-buffer: %w", err)
	}
	if err := run(ctx, "paste-buffer", "-b", "goat", "-t", targetForSession(session)); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}

	// Poll until the visible pane changes (paste arrived) or timeout.
	_ = waitForVisibleChange(ctx, session, visibleBefore, 5*time.Second)

	if err := run(ctx, "send-keys", "-t", targetForSession(session), "Enter"); err != nil {
		return fmt.Errorf("send enter: %w", err)
	}
	return nil
}

// CapturePane returns the full scrollback of the default pane.
func CapturePane(ctx context.Context) (string, error) {
	return CapturePaneFor(ctx, defaultSession)
}

// CapturePaneFor returns the full scrollback of the given tmux pane.
func CapturePaneFor(ctx context.Context, session string) (string, error) {
	return runOutput(ctx, "capture-pane", "-t", targetForSession(session), "-p", "-S", "-")
}

// CaptureVisible returns only the visible portion of the pane (no scrollback).
func CaptureVisible(ctx context.Context) (string, error) {
	return CaptureVisibleFor(ctx, defaultSession)
}

// CaptureVisibleFor returns only the visible portion of the pane (no scrollback).
func CaptureVisibleFor(ctx context.Context, session string) (string, error) {
	return runOutput(ctx, "capture-pane", "-t", targetForSession(session), "-p")
}

// Run executes an arbitrary tmux command.
func Run(ctx context.Context, args ...string) error {
	return run(ctx, args...)
}

// RunOutput executes a tmux command and returns its output.
func RunOutput(ctx context.Context, args ...string) (string, error) {
	return runOutput(ctx, args...)
}

// waitForVisibleChange polls until the visible pane differs from before.
func waitForVisibleChange(ctx context.Context, session, before string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current, err := CaptureVisibleFor(ctx, session)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if current != before {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for pane to change")
}

// WaitForIdle polls until Claude returns to the ❯ prompt and the pane stops
// changing. A single ❯ check is insufficient because the prompt is often
// visible even while Claude is actively working. Instead we require the pane
// content to be stable (unchanged across consecutive captures) AND contain ❯.
func WaitForIdle(ctx context.Context, timeout time.Duration) error {
	return WaitForIdleFor(ctx, defaultSession, timeout)
}

// WaitForIdleFor polls until the given session returns to the ❯ prompt and the
// pane stops changing.
func WaitForIdleFor(ctx context.Context, session string, timeout time.Duration) error {
	// Give Claude a moment to start processing
	time.Sleep(3 * time.Second)

	deadline := time.Now().Add(timeout)
	var lastSnap string
	stableCount := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := CaptureVisibleFor(ctx, session)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if out == lastSnap {
			stableCount++
			// Stable for 2 consecutive checks (4+ seconds) and has ❯ → idle
			if stableCount >= 2 && HasPrompt(out) {
				return nil
			}
		} else {
			stableCount = 0
			lastSnap = out
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for Claude to return to prompt")
}

// IsIdle checks whether Claude is idle by capturing the pane twice with a
// short delay. Returns true only if the pane is stable and contains ❯.
func IsIdle(ctx context.Context) bool {
	return IsIdleFor(ctx, defaultSession)
}

// IsIdleFor checks whether the given session is idle by capturing the pane
// twice with a short delay. Returns true only if the pane is stable and
// contains ❯.
func IsIdleFor(ctx context.Context, session string) bool {
	snap1, err := CaptureVisibleFor(ctx, session)
	if err != nil {
		return false
	}
	time.Sleep(2 * time.Second)
	snap2, err := CaptureVisibleFor(ctx, session)
	if err != nil {
		return false
	}
	return snap1 == snap2 && HasPrompt(snap2)
}

// HasPrompt checks if ❯ appears in the last 5 lines of the pane output.
func HasPrompt(paneOutput string) bool {
	lines := strings.Split(strings.TrimRight(paneOutput, "\n "), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
		if strings.Contains(lines[i], "❯") {
			return true
		}
	}
	return false
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
	return CheckPaneForErrorFor(ctx, defaultSession)
}

// CheckPaneForErrorFor examines the last N lines of the given pane for API
// errors that appeared after a message was sent. Returns the matched error
// string, or empty if no error found.
func CheckPaneForErrorFor(ctx context.Context, session string) string {
	out, err := CaptureVisibleFor(ctx, session)
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
