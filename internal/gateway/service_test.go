package gateway

import (
	"context"
	"errors"
	"testing"
)

func TestFriendlyError_Canceled(t *testing.T) {
	got := friendlyError(context.Canceled)
	want := "The bot was restarted while processing your message. Please send it again."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_DeadlineExceeded(t *testing.T) {
	got := friendlyError(context.DeadlineExceeded)
	want := "Claude took too long to respond (timed out). Try again or simplify your request."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_TimedOut(t *testing.T) {
	err := errors.New("timed out waiting for claude response")
	got := friendlyError(err)
	if got != "Claude didn't finish in time. Try again or use /clear to start a fresh session." {
		t.Errorf("got %q", got)
	}
}

func TestFriendlyError_SessionReadiness(t *testing.T) {
	err := errors.New("timed out waiting for Claude session readiness")
	got := friendlyError(err)
	if got != "Claude session failed to start. Try /clear to reset, or check that the daemon is healthy." {
		t.Errorf("got %q", got)
	}
}

func TestFriendlyError_PasteNotReceived(t *testing.T) {
	err := errors.New("paste not received by tmux")
	got := friendlyError(err)
	if got != "Failed to send your message to Claude. The session may be stuck — try /clear." {
		t.Errorf("got %q", got)
	}
}

func TestFriendlyError_UnhealthyAfter(t *testing.T) {
	err := errors.New("claude session unhealthy after 5 restart attempts")
	got := friendlyError(err)
	if got != "Claude Code session is down and couldn't be auto-restarted. The admin has been notified." {
		t.Errorf("got %q", got)
	}
}

func TestFriendlyError_GenericError(t *testing.T) {
	err := errors.New("something unexpected")
	got := friendlyError(err)
	want := "Something went wrong talking to Claude: something unexpected"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_WrappedCanceled(t *testing.T) {
	err := context.Canceled
	got := friendlyError(err)
	if got != "The bot was restarted while processing your message. Please send it again." {
		t.Errorf("wrapped canceled: got %q", got)
	}
}
