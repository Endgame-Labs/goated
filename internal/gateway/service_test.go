package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"goated/internal/agent"
)

// stubRuntime satisfies agent.SessionRuntime for testing friendlyError.
type stubRuntime struct{}

func (s stubRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{DisplayName: "Claude Code"}
}
func (s stubRuntime) EnsureSession(context.Context) error                { return nil }
func (s stubRuntime) StopSession(context.Context) error                  { return nil }
func (s stubRuntime) RestartSession(context.Context) error               { return nil }
func (s stubRuntime) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (s stubRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string) error {
	return nil
}
func (s stubRuntime) SendControlCommand(context.Context, string) error { return nil }
func (s stubRuntime) GetContextEstimate(context.Context, string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{}, nil
}
func (s stubRuntime) GetSessionState(context.Context) (agent.SessionState, error) {
	return agent.SessionState{}, nil
}
func (s stubRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{}, nil
}
func (s stubRuntime) GetHealth(context.Context) (agent.HealthStatus, error) {
	return agent.HealthStatus{OK: true}, nil
}
func (s stubRuntime) DetectRetryableError(context.Context) string { return "" }
func (s stubRuntime) Version(context.Context) string              { return "" }

func newTestService() *Service {
	return &Service{Session: stubRuntime{}}
}

func TestFriendlyError_Canceled(t *testing.T) {
	svc := newTestService()
	got := svc.friendlyError(context.Canceled)
	want := "The bot was restarted while processing your message. Please send it again."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_DeadlineExceeded(t *testing.T) {
	svc := newTestService()
	got := svc.friendlyError(context.DeadlineExceeded)
	want := "Claude Code took too long to respond (timed out). Try again or simplify your request."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_TimedOut(t *testing.T) {
	svc := newTestService()
	err := errors.New("timed out waiting for claude response")
	got := svc.friendlyError(err)
	want := "Claude Code didn't finish in time. Try again or use /clear to start a fresh session."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_SessionReadiness(t *testing.T) {
	svc := newTestService()
	err := errors.New("timed out waiting for Claude session readiness")
	got := svc.friendlyError(err)
	want := "Claude Code session failed to start. Try /clear to reset, or check that the daemon is healthy."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_PaneToChange(t *testing.T) {
	svc := newTestService()
	err := errors.New("waiting for pane to change but it stalled")
	got := svc.friendlyError(err)
	want := "Failed to send your message to Claude Code. The session may be stuck — try /clear."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_UnhealthyAfter(t *testing.T) {
	svc := newTestService()
	err := errors.New("claude code session unhealthy after 5 restart attempts")
	got := svc.friendlyError(err)
	want := "Claude Code session is down and couldn't be auto-restarted. The admin has been notified."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_GenericError(t *testing.T) {
	svc := newTestService()
	err := errors.New("something unexpected")
	got := svc.friendlyError(err)
	want := "Something went wrong talking to Claude Code: something unexpected"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_WrappedCanceled(t *testing.T) {
	svc := newTestService()
	got := svc.friendlyError(context.Canceled)
	want := "The bot was restarted while processing your message. Please send it again."
	if got != want {
		t.Errorf("wrapped canceled: got %q, want %q", got, want)
	}
}
