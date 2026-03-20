package cli

import (
	"context"
	"testing"
	"time"

	"goated/internal/agent"
)

type noticeSpySession struct {
	channel  string
	chatID   string
	source   string
	message  string
	metadata map[string]string
	calls    int
}

func (s *noticeSpySession) Descriptor() agent.RuntimeDescriptor  { return agent.RuntimeDescriptor{} }
func (s *noticeSpySession) EnsureSession(context.Context) error  { return nil }
func (s *noticeSpySession) StopSession(context.Context) error    { return nil }
func (s *noticeSpySession) RestartSession(context.Context) error { return nil }
func (s *noticeSpySession) ResetConversation(context.Context, string) (agent.ResetResult, error) {
	return agent.ResetResult{}, nil
}
func (s *noticeSpySession) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string) error {
	return nil
}
func (s *noticeSpySession) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
	return nil
}
func (s *noticeSpySession) SendControlCommand(context.Context, string) error { return nil }
func (s *noticeSpySession) GetContextEstimate(context.Context, string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{}, nil
}
func (s *noticeSpySession) GetSessionState(context.Context) (agent.SessionState, error) {
	return agent.SessionState{}, nil
}
func (s *noticeSpySession) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{}, nil
}
func (s *noticeSpySession) GetHealth(context.Context) (agent.HealthStatus, error) {
	return agent.HealthStatus{}, nil
}
func (s *noticeSpySession) DetectRetryableError(context.Context) string { return "" }
func (s *noticeSpySession) Version(context.Context) string              { return "" }
func (s *noticeSpySession) SendSystemNotice(_ context.Context, channel, chatID, source, message string, metadata map[string]string) error {
	s.calls++
	s.channel = channel
	s.chatID = chatID
	s.source = source
	s.message = message
	s.metadata = metadata
	return nil
}

func TestMaybeMirrorSystemNotice_ForwardsMessageMetadata(t *testing.T) {
	session := &noticeSpySession{}
	req := daemonSendRequest{
		ChatID:  "chat-1",
		Text:    "nightly sync complete",
		Source:  "cron",
		LogPath: "/tmp/job.log",
	}

	if err := maybeMirrorSystemNotice(context.Background(), session, "telegram", req); err != nil {
		t.Fatalf("maybeMirrorSystemNotice() error = %v", err)
	}
	if session.calls != 1 {
		t.Fatalf("calls = %d, want 1", session.calls)
	}
	if session.channel != "telegram" || session.chatID != "chat-1" || session.source != "cron" {
		t.Fatalf("forwarded identity mismatch: channel=%q chatID=%q source=%q", session.channel, session.chatID, session.source)
	}
	if session.message != "nightly sync complete" {
		t.Fatalf("message = %q", session.message)
	}
	if got := session.metadata["log_path"]; got != "/tmp/job.log" {
		t.Fatalf("log_path = %q, want /tmp/job.log", got)
	}
}

func TestMaybeMirrorSystemNotice_SkipsWhenSourceMissing(t *testing.T) {
	session := &noticeSpySession{}
	if err := maybeMirrorSystemNotice(context.Background(), session, "telegram", daemonSendRequest{ChatID: "chat-1", Text: "hello"}); err != nil {
		t.Fatalf("maybeMirrorSystemNotice() error = %v", err)
	}
	if session.calls != 0 {
		t.Fatalf("calls = %d, want 0", session.calls)
	}
}
