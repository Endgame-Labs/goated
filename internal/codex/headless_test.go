package codex

import (
	"reflect"
	"testing"
)

func TestHeadlessArgsBase(t *testing.T) {
	got := headlessArgs("")
	want := []string{
		"exec",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("headlessArgs(\"\") = %#v, want %#v", got, want)
	}
}

func TestHeadlessArgsModelOverride(t *testing.T) {
	got := headlessArgs("claude-haiku-4-5")
	want := []string{
		"exec",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
		"--model", "claude-haiku-4-5",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("headlessArgs(model) = %#v, want %#v", got, want)
	}
}
