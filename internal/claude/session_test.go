package claude

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newSessionRuntimeForCompletionTest(t *testing.T) *SessionRuntime {
	t.Helper()
	root := t.TempDir()
	r := NewSessionRuntime(filepath.Join(root, "workspace"), filepath.Join(root, "logs"), "")
	if err := os.MkdirAll(r.sessionDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRecordRunCompletionClearsCorruptPreviousMessageSessionAndMarksRetryable(t *testing.T) {
	r := newSessionRuntimeForCompletionTest(t)
	if err := r.writeSessionID("stored-session"); err != nil {
		t.Fatal(err)
	}

	stderr := `API Error: 400 {"error":{"details":{"diagnostics.previous_message_id":"invalid"}}}`
	r.recordRunCompletion(`{"session_id":"replacement-from-failed-run"}`, stderr, errors.New("exit status 1"))

	if got := r.readSessionID(); got != "" {
		t.Fatalf("session ID = %q, want cleared so retry starts fresh", got)
	}
	if got := r.DetectRetryableError(nil); got != "diagnostics.previous_message_id" {
		t.Fatalf("DetectRetryableError() = %q, want diagnostics.previous_message_id", got)
	}
}

func TestRecordRunCompletionPreservesSessionForUnrelatedStderr(t *testing.T) {
	r := newSessionRuntimeForCompletionTest(t)
	if err := r.writeSessionID("stored-session"); err != nil {
		t.Fatal(err)
	}

	r.recordRunCompletion("", "some unrelated warning", errors.New("exit status 1"))

	if got := r.readSessionID(); got != "stored-session" {
		t.Fatalf("session ID = %q, want stored-session preserved", got)
	}
	if got := r.DetectRetryableError(nil); got != "" {
		t.Fatalf("DetectRetryableError() = %q, want unrelated stderr ignored", got)
	}
}
