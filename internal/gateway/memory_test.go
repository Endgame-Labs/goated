package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"goated/internal/agent"
	"goated/internal/memory"
)

type memorySearchSpy struct{ calls int }

func (s *memorySearchSpy) Search(context.Context, string) ([]memory.Chunk, error) {
	s.calls++
	return []memory.Chunk{{Source: "note.md", Text: "remember this"}}, nil
}

type compactMemoryRuntime struct {
	stubRuntime
	svc       *Service
	responder *responderSpy
	contexts  []*agent.MessageContext
}

func (r *compactMemoryRuntime) WaitForAwaitingInput(context.Context, time.Duration) (agent.SessionState, error) {
	return agent.SessionState{Kind: agent.SessionStateAwaitingInput}, nil
}

func (r *compactMemoryRuntime) SendControlCommand(ctx context.Context, _ string) error {
	return r.svc.HandleMessage(ctx, IncomingMessage{ChatID: "chat", Text: "steering"}, r.responder)
}
func (r *compactMemoryRuntime) SendUserPrompt(_ context.Context, _, _, _ string, _ *agent.MessageAttachments, _, _ string, mc *agent.MessageContext) error {
	r.contexts = append(r.contexts, mc)
	return nil
}

func TestCompactionQueuedMessageGetsMemory(t *testing.T) {
	search := &memorySearchSpy{}
	responder := &responderSpy{}
	rt := &compactMemoryRuntime{responder: responder}
	svc := &Service{Session: rt, Memory: &memory.Engine{Searcher: search}}
	rt.svc = svc
	trigger := IncomingMessage{ChatID: "chat", Text: "trigger"}
	svc.enrichMemory(context.Background(), &trigger)
	if err := svc.compactAndFlush(context.Background(), trigger, responder); err != nil {
		t.Fatal(err)
	}
	if search.calls != 2 || len(rt.contexts) != 2 {
		t.Fatalf("searches=%d sends=%d", search.calls, len(rt.contexts))
	}
	for _, mc := range rt.contexts {
		if mc == nil || !strings.Contains(mc.RetrievedMemory, "remember this") {
			t.Fatalf("missing memory: %#v", mc)
		}
	}
}
