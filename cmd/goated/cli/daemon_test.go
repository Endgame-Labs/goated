package cli

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goated/internal/agent"
)

type blockingDaemonResponder struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingDaemonResponder) SendMessage(context.Context, string, string) error {
	close(r.started)
	<-r.release
	return nil
}

func TestDaemonReadinessProbeDetectsStuckRealSend(t *testing.T) {
	base := time.Now()
	current := base
	tracker := newDaemonSendTracker()
	tracker.nowFunc = func() time.Time { return current }
	responder := &blockingDaemonResponder{started: make(chan struct{}), release: make(chan struct{})}

	realServer, realClient := net.Pipe()
	defer realClient.Close()
	go handleDaemonSocketConn(context.Background(), realServer, responder, nil, nil, "test", tracker)
	if err := json.NewEncoder(realClient).Encode(daemonSendRequest{ChatID: "chat-1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	<-responder.started

	// The socket still accepts independent connections, but readiness must now
	// inspect owned send work rather than treating acceptance as sufficient.
	current = base.Add(daemonSendStuckAfter + time.Second)
	probeServer, probeClient := net.Pipe()
	defer probeClient.Close()
	go handleDaemonSocketConn(context.Background(), probeServer, responder, nil, nil, "test", tracker)
	if err := json.NewEncoder(probeClient).Encode(daemonSendRequest{Probe: true}); err != nil {
		t.Fatal(err)
	}
	var probeResp daemonSendResponse
	if err := json.NewDecoder(probeClient).Decode(&probeResp); err != nil {
		t.Fatal(err)
	}
	if probeResp.OK || !strings.Contains(probeResp.Error, "outbound send stuck") {
		t.Fatalf("probe response = %+v, want stuck-send failure", probeResp)
	}

	close(responder.release)
	var realResp daemonSendResponse
	if err := json.NewDecoder(realClient).Decode(&realResp); err != nil {
		t.Fatal(err)
	}
	if !realResp.OK {
		t.Fatalf("real send response = %+v, want success after release", realResp)
	}
}

func TestDaemonReadinessProbeAllowsRecentSend(t *testing.T) {
	base := time.Now()
	tracker := newDaemonSendTracker()
	tracker.nowFunc = func() time.Time { return base }
	finish := tracker.begin()
	defer finish()
	tracker.nowFunc = func() time.Time { return base.Add(daemonSendStuckAfter) }
	if err := tracker.readinessError(); err != nil {
		t.Fatalf("recent bounded send reported unhealthy: %v", err)
	}
}

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
func (s *noticeSpySession) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
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

func TestMaybeMirrorSystemNotice_SkipsMirrorWhenSourceMissing(t *testing.T) {
	session := &noticeSpySession{}
	if err := maybeMirrorSystemNotice(context.Background(), session, "telegram", daemonSendRequest{ChatID: "chat-1", Text: "hello"}); err != nil {
		t.Fatalf("maybeMirrorSystemNotice() error = %v", err)
	}
	if session.calls != 0 {
		t.Fatalf("calls = %d, want 0", session.calls)
	}
}

func TestRuntimeSessionIDPath(t *testing.T) {
	logDir := "/tmp/goated-logs"
	tests := []struct {
		name        string
		runtimeName string
		want        string
	}{
		{name: "default claude", runtimeName: "", want: filepath.Join(logDir, "claude_session", "session_id")},
		{name: "claude", runtimeName: string(agent.RuntimeClaude), want: filepath.Join(logDir, "claude_session", "session_id")},
		{name: "claude tui", runtimeName: string(agent.RuntimeClaudeTUI), want: filepath.Join(logDir, "claude_session", "session_id")},
		{name: "codex", runtimeName: string(agent.RuntimeCodex), want: filepath.Join(logDir, "codex_session", "thread_id")},
		{name: "codex tui", runtimeName: string(agent.RuntimeCodexTUI), want: ""},
		{name: "pi", runtimeName: string(agent.RuntimePi), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeSessionIDPath(logDir, tt.runtimeName); got != tt.want {
				t.Fatalf("runtimeSessionIDPath(%q) = %q, want %q", tt.runtimeName, got, tt.want)
			}
		})
	}
}
