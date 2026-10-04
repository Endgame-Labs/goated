package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	e := Engine{Searcher: fakeSearch{{Source: "one", Text: "A"}, {Source: "skip", Text: "B"}, {Source: "three", Text: "C"}}, Hooks: []Hook{JevHook{Judge: j, Parallel: 3}}}
	got, err := e.Retrieve(context.Background(), "query", History{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Chunk{{Source: "one", Text: "A"}, {Source: "three", Text: "C"}}) {
		t.Fatalf("got %#v", got)
	}
	if j.max.Load() < 2 {
		t.Fatalf("not concurrent: %d", j.max.Load())
	}
}
func TestParseResultsRequiresJSONArray(t *testing.T) {
	for _, bad := range []string{"null", "{}", "plain text", `[{"source":"x"}]`, `[{"text":"x"}]`} {
		if _, err := ParseResults([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	got, err := ParseResults([]byte(`[{"source":"vault/a.md","text":"alpha","kind":"file"}]`))
	if err != nil || len(got) != 1 || got[0].Source != "vault/a.md" || got[0].Text != "alpha" || string(got[0].Extra["kind"]) != `"file"` {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	encoded, err := json.Marshal(got)
	if err != nil || !json.Valid(encoded) || !strings.Contains(string(encoded), `"kind":"file"`) {
		t.Fatalf("metadata lost: %s, %v", encoded, err)
	}
	if got, err := ParseResults([]byte(`[]`)); err != nil || len(got) != 0 {
		t.Fatalf("empty array got=%#v err=%v", got, err)
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
	keep, err := j.Useful(context.Background(), "current", h, Chunk{Source: "vault/x.md", Text: "fact"})
	if err != nil || keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	for _, k := range []string{"current_user_message", "conversation_summary", "past_user_messages", "past_assistant_responses", "memory_chunk"} {
		if _, ok := state[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
}
