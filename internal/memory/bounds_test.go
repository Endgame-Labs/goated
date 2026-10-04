package memory

import (
	"context"
	"testing"
	"time"
)

type expandingHook struct{}

func (expandingHook) Apply(context.Context, []Chunk, HookContext) ([]Chunk, error) {
	out := make([]Chunk, 100)
	for i := range out {
		out[i] = Chunk{Source: "x", Text: "y"}
	}
	return out, nil
}

func TestHooksCannotBypassMaxChunks(t *testing.T) {
	e := Engine{Searcher: fakeSearch{{Source: "x", Text: "y"}}, Hooks: []Hook{expandingHook{}, countCheckingHook{t}}, MaxChunks: 1}
	got, err := e.Retrieve(context.Background(), "q", History{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 1 {
		t.Fatalf("max_chunks=1, got %d", len(got))
	}
}

type countCheckingHook struct{ t *testing.T }

func (h countCheckingHook) Apply(_ context.Context, chunks []Chunk, _ HookContext) ([]Chunk, error) {
	if len(chunks) != 1 {
		h.t.Fatalf("downstream hook received %d chunks", len(chunks))
	}
	return chunks, nil
}

func TestCommandTimeoutWithInheritedPipe(t *testing.T) {
	for _, script := range []string{"sleep 2 & printf '[]'", "sleep 2 & wait"} {
		for _, kind := range []string{"search", "hook"} {
			t.Run(kind+"/"+script, func(t *testing.T) {
				args := []string{"sh", "-c", script}
				start := time.Now()
				var err error
				if kind == "search" {
					_, err = (CommandSearcher{Args: args, Timeout: 100 * time.Millisecond}).Search(context.Background(), "q")
				} else {
					_, err = (CommandHook{Args: args, Timeout: 100 * time.Millisecond}).Apply(context.Background(), nil, HookContext{})
				}
				if err == nil {
					t.Fatal("expected timeout or pipe-drain error")
				}
				if elapsed := time.Since(start); elapsed > time.Second {
					t.Fatalf("command exceeded time bound: %v", elapsed)
				}
			})
		}
	}
}
