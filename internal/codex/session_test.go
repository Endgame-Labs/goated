package codex

import (
	"reflect"
	"testing"
)

func TestSessionPromptArgsFreshReadsPromptFromStdin(t *testing.T) {
	r := NewSessionRuntime("/tmp/workspace", "/tmp/logs")

	got := r.promptArgs("")
	want := []string{
		"exec",
		"--json",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
		"-",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("promptArgs(\"\") = %#v, want %#v", got, want)
	}
}

func TestSessionPromptArgsResumeReadsPromptFromStdin(t *testing.T) {
	r := NewSessionRuntime("/tmp/workspace", "/tmp/logs")

	got := r.promptArgs("thread-123")
	want := []string{
		"exec", "resume",
		"--json",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
		"thread-123", "-",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("promptArgs(thread) = %#v, want %#v", got, want)
	}
}
