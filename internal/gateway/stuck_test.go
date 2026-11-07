package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"goated/internal/agent"
)

// stuckRuntime ends every wait with a stable pane and no prompt found, then
// answers the gateway's single recheck with recheck.
type stuckRuntime struct {
	stubRuntime
	waitState    agent.SessionState
	recheck      agent.SessionState
	recheckCalls int
	sends        int
}

func (r *stuckRuntime) SendUserPrompt(context.Context, string, string, string, *agent.MessageAttachments, string, string, *agent.MessageContext) error {
	r.sends++
	return nil
}
func (r *stuckRuntime) SendBatchPrompt(context.Context, string, string, []agent.PromptMessage) error {
	r.sends++
	return nil
}
func (r *stuckRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return r.waitState, nil
}
func (r *stuckRuntime) GetSessionState(context.Context) (agent.SessionState, error) {
	r.recheckCalls++
	return r.recheck, nil
}

type chatMessage struct{ chatID, text string }

type chatSpy struct{ messages []chatMessage }

func (c *chatSpy) SendMessage(_ context.Context, chatID, text string) error {
	c.messages = append(c.messages, chatMessage{chatID, text})
	return nil
}

func (c *chatSpy) to(chatID string) []string {
	var out []string
	for _, m := range c.messages {
		if m.chatID == chatID {
			out = append(out, m.text)
		}
	}
	return out
}

var (
	unknownStable = agent.SessionState{Kind: agent.SessionStateUnknownStable, Summary: "pane is stable without a prompt"}
	idle          = agent.SessionState{Kind: agent.SessionStateAwaitingInput, Summary: "idle at prompt"}
)

func TestUnknownStable_RecheckFindsPrompt(t *testing.T) {
	rt := &stuckRuntime{waitState: unknownStable, recheck: idle}
	chat := &chatSpy{}
	svc := &Service{Session: rt, AdminChatID: "admin"}

	if err := svc.sendWithRetry(context.Background(), IncomingMessage{ChatID: "user", Text: "hi"}, chat); err != nil {
		t.Fatalf("sendWithRetry: %v", err)
	}
	if rt.recheckCalls != 1 || rt.sends != 1 {
		t.Fatalf("recheckCalls=%d sends=%d, want 1 and 1", rt.recheckCalls, rt.sends)
	}
	if len(chat.messages) != 0 {
		t.Fatalf("expected no messages, got %+v", chat.messages)
	}
}

func TestUnknownStable_StuckTellsSenderAndAdminOnce(t *testing.T) {
	rt := &stuckRuntime{waitState: unknownStable, recheck: unknownStable}
	chat := &chatSpy{}
	svc := &Service{Session: rt, AdminChatID: "admin"}
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 2; i++ {
		if err := svc.sendWithRetry(ctx, IncomingMessage{ChatID: "user", Text: "hi"}, chat); err != nil {
			t.Fatalf("sendWithRetry: %v", err)
		}
	}
	if err := svc.sendBatchWithRetry(ctx, "telegram", "user", []agent.PromptMessage{{}}, chat); err != nil {
		t.Fatalf("sendBatchWithRetry: %v", err)
	}
	// One recheck per send, no retries or restarts: the wait stays bounded.
	if rt.recheckCalls != 3 || rt.sends != 3 {
		t.Fatalf("recheckCalls=%d sends=%d, want 3 and 3", rt.recheckCalls, rt.sends)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("stuck sends took %s", d)
	}
	user := chat.to("user")
	if len(user) != 3 || !strings.Contains(user[0], "stuck without an input prompt") {
		t.Fatalf("user messages = %q", user)
	}
	if admin := chat.to("admin"); len(admin) != 1 {
		t.Fatalf("admin messages = %q, want exactly 1", admin)
	}

	// Once a send reaches the prompt, the next stuck send alerts the admin again.
	rt.waitState = idle
	if err := svc.sendWithRetry(ctx, IncomingMessage{ChatID: "user", Text: "hi"}, chat); err != nil {
		t.Fatalf("sendWithRetry: %v", err)
	}
	rt.waitState = unknownStable
	if err := svc.sendWithRetry(ctx, IncomingMessage{ChatID: "user", Text: "hi"}, chat); err != nil {
		t.Fatalf("sendWithRetry: %v", err)
	}
	if admin := chat.to("admin"); len(admin) != 2 {
		t.Fatalf("admin messages after recovery = %q, want 2", admin)
	}
}

func TestUnknownStable_AdminChatGetsOneMessage(t *testing.T) {
	rt := &stuckRuntime{waitState: unknownStable, recheck: unknownStable}
	chat := &chatSpy{}
	svc := &Service{Session: rt, AdminChatID: "admin"}

	if err := svc.sendWithRetry(context.Background(), IncomingMessage{ChatID: "admin", Text: "hi"}, chat); err != nil {
		t.Fatalf("sendWithRetry: %v", err)
	}
	if admin := chat.to("admin"); len(admin) != 1 {
		t.Fatalf("admin messages = %q, want 1", admin)
	}
}
