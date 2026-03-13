package slack

import (
	"os"
	"testing"
)

func TestWriteAndClaimThinkingTS(t *testing.T) {
	// Clean up any existing file
	os.Remove(ThinkingFile)
	os.Remove(ThinkingFile + ".tmp")
	defer os.Remove(ThinkingFile)

	// Write a timestamp
	if err := WriteThinkingTS("1234.5678"); err != nil {
		t.Fatalf("WriteThinkingTS: %v", err)
	}

	// File should exist
	if _, err := os.Stat(ThinkingFile); os.IsNotExist(err) {
		t.Fatal("thinking file should exist after write")
	}

	// Claim it
	ts := ClaimThinkingTS()
	if ts != "1234.5678" {
		t.Errorf("ClaimThinkingTS = %q, want 1234.5678", ts)
	}

	// File should be gone
	if _, err := os.Stat(ThinkingFile); !os.IsNotExist(err) {
		t.Error("thinking file should be removed after claim")
	}
}

func TestClaimThinkingTS_DoubleClaim(t *testing.T) {
	os.Remove(ThinkingFile)
	defer os.Remove(ThinkingFile)

	WriteThinkingTS("9999.0000")

	// First claim
	ts1 := ClaimThinkingTS()
	if ts1 != "9999.0000" {
		t.Errorf("first claim = %q, want 9999.0000", ts1)
	}

	// Second claim should return empty
	ts2 := ClaimThinkingTS()
	if ts2 != "" {
		t.Errorf("second claim = %q, want empty", ts2)
	}
}

func TestClaimThinkingTS_NoFile(t *testing.T) {
	os.Remove(ThinkingFile)

	ts := ClaimThinkingTS()
	if ts != "" {
		t.Errorf("ClaimThinkingTS with no file = %q, want empty", ts)
	}
}

func TestWriteThinkingTS_Overwrite(t *testing.T) {
	os.Remove(ThinkingFile)
	defer os.Remove(ThinkingFile)

	WriteThinkingTS("first.ts")
	WriteThinkingTS("second.ts")

	ts := ClaimThinkingTS()
	if ts != "second.ts" {
		t.Errorf("got %q, want second.ts (latest write should win)", ts)
	}
}
