package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSearch []Chunk

func (f fakeSearch) Search(context.Context, string) ([]Chunk, error) { return f, nil }

type fakeJudge struct{ active, max atomic.Int32 }

func (f *fakeJudge) Useful(_ context.Context, _ string, _ History, c Chunk) (bool, error) {
	n := f.active.Add(1)
	for {
		old := f.max.Load()
		if n <= old || f.max.CompareAndSwap(old, n) {
			break
		}
	}
	time.Sleep(10 * time.Millisecond)
	f.active.Add(-1)
	return c.Source != "skip", nil
}
func TestEngineParallelOrder(t *testing.T) {
	j := &fakeJudge{}
	e := Engine{Searcher: fakeSearch{{"one", "A"}, {"skip", "B"}, {"three", "C"}}, Judge: j, Parallel: 3}
	got, err := e.Retrieve(context.Background(), "query", History{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Chunk{{"one", "A"}, {"three", "C"}}) {
		t.Fatalf("got %#v", got)
	}
	if j.max.Load() < 2 {
		t.Fatalf("not concurrent: %d", j.max.Load())
	}
}
func TestParseSections(t *testing.T) {
	got := parseSections("=== Remembering ===\n\n--- [file] vault/a.md ---\nalpha\n--- [tpuf-semantic] notes/b.md ---\nbeta\n")
	if !reflect.DeepEqual(got, []Chunk{{"vault/a.md", "alpha\n"}, {"notes/b.md", "beta\n"}}) {
		t.Fatalf("got %#v", got)
	}
}
func TestJevStateAndDecision(t *testing.T) {
	var state map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing bearer")
		}
		var req struct {
			State     map[string]any `json:"state"`
			Questions map[string]any `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		state = req.State
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{"useful":{"type":"noul","noul":0.3}}}`))
	}))
	defer server.Close()
	j := JevJudge{APIKey: "test", URL: server.URL, Threshold: 0.5}
	h := History{Users: []string{"u1"}, Assistants: []string{"a1"}, Summary: "older summary"}
	keep, err := j.Useful(context.Background(), "current", h, Chunk{"vault/x.md", "fact"})
	if err != nil || keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	for _, k := range []string{"current_user_message", "conversation_summary", "past_user_messages", "past_assistant_responses", "memory_chunk"} {
		if _, ok := state[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
}
