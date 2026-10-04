package memory

import (
	"context"
	"reflect"
	"testing"
)

func TestCommandHookJSONArrayRoundTrip(t *testing.T) {
	input := []Chunk{{Source: "vault/x.md", Text: "fact"}}
	h := CommandHook{Args: []string{"cat"}}
	got, err := h.Apply(context.Background(), input, HookContext{Query: "test"})
	if err != nil || !reflect.DeepEqual(got, input) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
func TestCommandHookRejectsNonArray(t *testing.T) {
	h := CommandHook{Args: []string{"printf", "null"}}
	if _, err := h.Apply(context.Background(), []Chunk{}, HookContext{}); err == nil {
		t.Fatal("accepted null output")
	}
}
func TestEnginePreservesResultsAfterInvalidHook(t *testing.T) {
	want := []Chunk{{Source: "vault/x.md", Text: "fact"}}
	e := Engine{Searcher: fakeSearch(want), Hooks: []Hook{CommandHook{Args: []string{"printf", "null"}}}}
	got, err := e.Retrieve(context.Background(), "query", History{})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
