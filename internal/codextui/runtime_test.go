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
	if !isUpdatePrompt("A new version is available\nUpdate now\nSkip") {
		t.Fatal("expected update prompt to be detected")
	}
	if isUpdatePrompt("normal prompt output") {
		t.Fatal("did not expect update prompt on normal output")
	}
}

func TestSummarizeStartupScreen(t *testing.T) {
	t.Parallel()

	got := summarizeStartupScreen("line1\nline2\nline3\nline4\nline5\nline6\nline7")
	if got != "line2 line3 line4 line5 line6 line7" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestDismissUpdatePromptKeys(t *testing.T) {
	t.Parallel()

	got := dismissUpdatePromptKeys()
	if len(got) == 0 {
		t.Fatal("expected at least one dismissal key sequence")
	}
	if len(got[0]) != 2 || got[0][0] != "Down" || got[0][1] != "Enter" {
		t.Fatalf("first dismissal sequence = %v, want [Down Enter]", got[0])
	}
}
