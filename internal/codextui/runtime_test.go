package codextui

import "testing"

func TestParseStatusEstimate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantPercent int
		wantSummary string
	}{
		{
			name:        "context window",
			input:       "Context window: 42% used",
			wantPercent: 42,
			wantSummary: "Context window: 42% used",
		},
		{
			name:        "token usage",
			input:       "Token usage: 7% used",
			wantPercent: 7,
			wantSummary: "Token usage: 7% used",
		},
		{
			name:        "unparseable",
			input:       "No context information here",
			wantPercent: -1,
			wantSummary: "unable to parse /status output",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPercent, gotSummary := parseStatusEstimate(tt.input)
			if gotPercent != tt.wantPercent {
				t.Fatalf("parseStatusEstimate(%q) percent = %d, want %d", tt.input, gotPercent, tt.wantPercent)
			}
			if gotSummary != tt.wantSummary {
				t.Fatalf("parseStatusEstimate(%q) summary = %q, want %q", tt.input, gotSummary, tt.wantSummary)
			}
		})
	}
}

func TestBlockedStateDetection(t *testing.T) {
	t.Parallel()

	if !isBlockedAuth("Welcome to Codex\nSign in with ChatGPT") {
		t.Fatal("expected auth blocker to be detected")
	}
	if isBlockedAuth("normal prompt output") {
		t.Fatal("did not expect auth blocker on normal output")
	}
	if !isBlockedIntervention("Waiting for approval before continuing") {
		t.Fatal("expected intervention blocker to be detected")
	}
	if isBlockedIntervention("idle at prompt") {
		t.Fatal("did not expect intervention blocker on idle output")
	}
}
