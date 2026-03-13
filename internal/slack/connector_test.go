package slack

import (
	"testing"
)

func TestSplitMessage_Short(t *testing.T) {
	chunks := splitMessage("hello", 4000)
	if len(chunks) != 1 {
		t.Fatalf("len = %d, want 1", len(chunks))
	}
	if chunks[0] != "hello" {
		t.Errorf("chunks[0] = %q", chunks[0])
	}
}

func TestSplitMessage_ExactLimit(t *testing.T) {
	msg := "abc"
	chunks := splitMessage(msg, 3)
	if len(chunks) != 1 {
		t.Fatalf("len = %d, want 1", len(chunks))
	}
}

func TestSplitMessage_SplitsAtNewline(t *testing.T) {
	msg := "line1\nline2\nline3"
	// max=10 means "line1\nline" fits but prefer splitting at newline
	chunks := splitMessage(msg, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
	// First chunk should end at a newline boundary
	if chunks[0] != "line1\n" {
		t.Errorf("chunks[0] = %q, expected split at newline", chunks[0])
	}
}

func TestSplitMessage_NoNewline(t *testing.T) {
	msg := "abcdefghij"
	chunks := splitMessage(msg, 5)
	if len(chunks) != 2 {
		t.Fatalf("len = %d, want 2", len(chunks))
	}
	if chunks[0] != "abcde" {
		t.Errorf("chunks[0] = %q", chunks[0])
	}
	if chunks[1] != "fghij" {
		t.Errorf("chunks[1] = %q", chunks[1])
	}
}

func TestSplitMessage_Empty(t *testing.T) {
	chunks := splitMessage("", 100)
	if len(chunks) != 1 {
		t.Fatalf("len = %d, want 1", len(chunks))
	}
}

func TestThinkingFileHasTS(t *testing.T) {
	// Write and check
	WriteThinkingTS("1234.5678")
	defer ClaimThinkingTS()

	if !thinkingFileHasTS("1234.5678") {
		t.Error("expected true for matching timestamp")
	}
	if thinkingFileHasTS("9999.9999") {
		t.Error("expected false for non-matching timestamp")
	}
}

func TestThinkingFileHasTS_NoFile(t *testing.T) {
	ClaimThinkingTS() // ensure file is gone
	if thinkingFileHasTS("anything") {
		t.Error("expected false when file doesn't exist")
	}
}
