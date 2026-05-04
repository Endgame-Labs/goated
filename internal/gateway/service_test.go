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
	return agent.RuntimeDescriptor{DisplayName: "Claude Code TUI"}
}
func (s stubRuntime) EnsureSession(context.Context) error  { return nil }
func (s stubRuntime) StopSession(context.Context) error    { return nil }
func (s stubRuntime) RestartSession(context.Context) error { return nil }
func (s stubRuntime) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (s stubRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
	return nil
}
func (s stubRuntime) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
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

type recoverRuntime struct {
	restartCalls int
}

type deadRecoverRuntime struct {
	restartCalls int
}

type promptSpyRuntime struct {
	sendUserPromptCalls int
}

func (r *recoverRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{DisplayName: "Claude Code TUI"}
}
func (r *recoverRuntime) EnsureSession(context.Context) error  { return nil }
func (r *recoverRuntime) StopSession(context.Context) error    { return nil }
func (r *recoverRuntime) RestartSession(context.Context) error { r.restartCalls++; return nil }
func (r *recoverRuntime) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (r *recoverRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
	return nil
}
func (r *recoverRuntime) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
	return nil
}
func (r *recoverRuntime) SendControlCommand(context.Context, string) error { return nil }
func (r *recoverRuntime) GetContextEstimate(context.Context, string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{}, nil
}
func (r *recoverRuntime) GetSessionState(context.Context) (agent.SessionState, error) {
	return agent.SessionState{Kind: agent.SessionStateUnknownStable, Summary: "pane is stable without a prompt"}, nil
}
func (r *recoverRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{}, errors.New("timed out waiting for Claude session to become idle")
}
func (r *recoverRuntime) GetHealth(context.Context) (agent.HealthStatus, error) {
	return agent.HealthStatus{OK: true, Recoverable: true, Summary: "ok"}, nil
}
func (r *recoverRuntime) DetectRetryableError(context.Context) string { return "" }
func (r *recoverRuntime) Version(context.Context) string              { return "" }

func (r *deadRecoverRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{DisplayName: "Claude Code TUI"}
}
func (r *deadRecoverRuntime) EnsureSession(context.Context) error  { return nil }
func (r *deadRecoverRuntime) StopSession(context.Context) error    { return nil }
func (r *deadRecoverRuntime) RestartSession(context.Context) error { r.restartCalls++; return nil }
func (r *deadRecoverRuntime) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (r *deadRecoverRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
	return nil
}
func (r *deadRecoverRuntime) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
	return nil
}
func (r *deadRecoverRuntime) SendControlCommand(context.Context, string) error { return nil }
func (r *deadRecoverRuntime) GetContextEstimate(context.Context, string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{}, nil
}
func (r *deadRecoverRuntime) GetSessionState(context.Context) (agent.SessionState, error) {
	return agent.SessionState{Kind: agent.SessionStateDead, Summary: "no tmux session"}, nil
}
func (r *deadRecoverRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{}, errors.New("timed out waiting for Claude session to become idle")
}
func (r *deadRecoverRuntime) GetHealth(context.Context) (agent.HealthStatus, error) {
	return agent.HealthStatus{OK: false, Recoverable: true, Summary: "tmux session missing"}, nil
}
func (r *deadRecoverRuntime) DetectRetryableError(context.Context) string { return "" }
func (r *deadRecoverRuntime) Version(context.Context) string              { return "" }

func (r *promptSpyRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{DisplayName: "Claude Code TUI"}
}
func (r *promptSpyRuntime) EnsureSession(context.Context) error  { return nil }
func (r *promptSpyRuntime) StopSession(context.Context) error    { return nil }
func (r *promptSpyRuntime) RestartSession(context.Context) error { return nil }
func (r *promptSpyRuntime) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (r *promptSpyRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
	r.sendUserPromptCalls++
	return nil
}
func (r *promptSpyRuntime) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
	return nil
}
func (r *promptSpyRuntime) SendControlCommand(context.Context, string) error { return nil }
func (r *promptSpyRuntime) GetContextEstimate(context.Context, string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{}, nil
}
func (r *promptSpyRuntime) GetSessionState(context.Context) (agent.SessionState, error) {
	return agent.SessionState{}, nil
}
func (r *promptSpyRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{}, nil
}
func (r *promptSpyRuntime) GetHealth(context.Context) (agent.HealthStatus, error) {
	return agent.HealthStatus{OK: true}, nil
}
func (r *promptSpyRuntime) DetectRetryableError(context.Context) string { return "" }
func (r *promptSpyRuntime) Version(context.Context) string              { return "" }

type responderSpy struct {
	messages []string
}

func (r *responderSpy) SendMessage(_ context.Context, _ string, text string) error {
	r.messages = append(r.messages, text)
	return nil
}

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
	want := "Claude Code TUI took too long to respond (timed out). Try again or simplify your request."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_TimedOut(t *testing.T) {
	svc := newTestService()
	err := errors.New("timed out waiting for claude response")
	got := svc.friendlyError(err)
	want := "Claude Code TUI didn't finish in time. Try again or use /clear to start a fresh session."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_SessionReadiness(t *testing.T) {
	svc := newTestService()
	err := errors.New("timed out waiting for Claude session readiness")
	got := svc.friendlyError(err)
	want := "Claude Code TUI session failed to start. Try /clear to reset, or check that the daemon is healthy."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_PaneToChange(t *testing.T) {
	svc := newTestService()
	err := errors.New("waiting for pane to change but it stalled")
	got := svc.friendlyError(err)
	want := "Failed to send your message to Claude Code TUI. The session may be stuck — try /clear."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_UnhealthyAfter(t *testing.T) {
	svc := newTestService()
	err := errors.New("claude code session unhealthy after 5 restart attempts")
	got := svc.friendlyError(err)
	want := "Claude Code TUI session is down and couldn't be auto-restarted. The admin has been notified."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_GenericError(t *testing.T) {
	svc := newTestService()
	err := errors.New("something unexpected")
	got := svc.friendlyError(err)
	want := "Something went wrong talking to Claude Code TUI: something unexpected"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_AuthManualIntervention(t *testing.T) {
	svc := newTestService()
	err := errors.New("Claude Code TUI requires manual intervention: Claude Code login expired; run /login in the server session")
	got := svc.friendlyError(err)
	want := "Claude Code TUI login expired and needs manual re-auth on the server. Please run /login there, then try again."
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

func TestTryRecoverAfterIdleTimeout_DoesNotRestartHealthyStableSession(t *testing.T) {
	rt := &recoverRuntime{}
	svc := &Service{Session: rt}

	recovered, err := svc.tryRecoverAfterIdleTimeout(context.Background(), "", "chat-1", errors.New("timed out waiting for Claude session to become idle"))
	if err != nil {
		t.Fatalf("tryRecoverAfterIdleTimeout returned error: %v", err)
	}
	if recovered {
		t.Fatal("expected healthy stable session to avoid restart")
	}
	if rt.restartCalls != 0 {
		t.Fatalf("expected no restart, got %d", rt.restartCalls)
	}
}

func TestTryRecoverAfterIdleTimeout_RestartsDeadRecoverableSession(t *testing.T) {
	rt := &deadRecoverRuntime{}
	svc := &Service{Session: rt}

	recovered, err := svc.tryRecoverAfterIdleTimeout(context.Background(), "", "chat-1", errors.New("timed out waiting for Claude session to become idle"))
	if err != nil {
		t.Fatalf("tryRecoverAfterIdleTimeout returned error: %v", err)
	}
	if !recovered {
		t.Fatal("expected dead recoverable session to restart")
	}
	if rt.restartCalls != 1 {
		t.Fatalf("expected exactly one restart, got %d", rt.restartCalls)
	}
}

type blockedAuthRuntime struct{}

func (r *blockedAuthRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{DisplayName: "Claude Code TUI"}
}
func (r *blockedAuthRuntime) EnsureSession(context.Context) error  { return nil }
func (r *blockedAuthRuntime) StopSession(context.Context) error    { return nil }
func (r *blockedAuthRuntime) RestartSession(context.Context) error { return nil }
func (r *blockedAuthRuntime) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (r *blockedAuthRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
	return nil
}
func (r *blockedAuthRuntime) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
	return nil
}
func (r *blockedAuthRuntime) SendControlCommand(context.Context, string) error { return nil }
func (r *blockedAuthRuntime) GetContextEstimate(context.Context, string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{}, nil
}
func (r *blockedAuthRuntime) GetSessionState(context.Context) (agent.SessionState, error) {
	return agent.SessionState{Kind: agent.SessionStateBlockedAuth, Summary: "Claude Code login expired; run /login in the server session"}, nil
}
func (r *blockedAuthRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{}, errors.New("timed out waiting for Claude session to become idle")
}
func (r *blockedAuthRuntime) GetHealth(context.Context) (agent.HealthStatus, error) {
	return agent.HealthStatus{OK: false, Recoverable: false, Summary: "Claude Code login expired; run /login in the server session"}, nil
}
func (r *blockedAuthRuntime) DetectRetryableError(context.Context) string { return "" }
func (r *blockedAuthRuntime) Version(context.Context) string              { return "" }

func TestTryRecoverAfterIdleTimeout_ReturnsAuthErrorImmediately(t *testing.T) {
	rt := &blockedAuthRuntime{}
	svc := &Service{Session: rt}

	recovered, err := svc.tryRecoverAfterIdleTimeout(context.Background(), "", "chat-1", errors.New("timed out waiting for Claude session to become idle"))
	if err == nil {
		t.Fatal("expected auth blocker error")
	}
	if recovered {
		t.Fatal("did not expect recovery on auth blocker")
	}
	want := "Claude Code TUI requires manual intervention: Claude Code login expired; run /login in the server session"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestHandleMessage_FailedAttachmentOnlyRepliesToUser(t *testing.T) {
	rt := &promptSpyRuntime{}
	resp := &responderSpy{}
	svc := &Service{Session: rt}

	err := svc.HandleMessage(context.Background(), IncomingMessage{
		Channel: "telegram",
		ChatID:  "chat-1",
		AttachmentsFailed: []AttachmentResult{
			{
				Index:      0,
				Filename:   "report.exe",
				ReasonCode: "unsupported_type",
				Reason:     "unsupported file type",
			},
		},
	}, resp)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if rt.sendUserPromptCalls != 0 {
		t.Fatalf("expected no prompt to be sent to runtime, got %d", rt.sendUserPromptCalls)
	}
	if len(resp.messages) != 1 {
		t.Fatalf("expected 1 user-facing message, got %d", len(resp.messages))
	}
	want := "I couldn't process that attachment:\n- report.exe: unsupported file type\nSupported uploads include images, PDF, CSV/TSV, DOCX, and XLSX within the configured size limits."
	if resp.messages[0] != want {
		t.Fatalf("reply = %q, want %q", resp.messages[0], want)
	}
}

func TestFailedAttachmentReply_LimitsListedFailures(t *testing.T) {
	got := failedAttachmentReply([]AttachmentResult{
		{Index: 0, Filename: "one.pdf", Reason: "failed one"},
		{Index: 1, Filename: "two.pdf", Reason: "failed two"},
		{Index: 2, Filename: "three.pdf", Reason: "failed three"},
		{Index: 3, Filename: "four.pdf", Reason: "failed four"},
	})

	want := "I couldn't process those attachments:\n- one.pdf: failed one\n- two.pdf: failed two\n- three.pdf: failed three\n- and 1 more\nSupported uploads include images, PDF, CSV/TSV, DOCX, and XLSX within the configured size limits."
	if got != want {
		t.Fatalf("failedAttachmentReply() = %q, want %q", got, want)
	}
}
