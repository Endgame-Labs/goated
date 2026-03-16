package claudetui

import (
	"testing"
)

func TestParseContextOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{
			"typical output",
			"claude-opus-4-6 · 85k/200k tokens (42%)",
			42,
		},
		{
			"zero percent",
			"claude-opus-4-6 · 0.5k/200k tokens (0%)",
			0,
		},
		{
			"100 percent",
			"claude-opus-4-6 · 200k/200k tokens (100%)",
			100,
		},
		{
			"embedded in multiline output",
			"some preamble\nmodel: claude-opus-4-6 · 120.5k/200k tokens (60%)\nsome postamble",
			60,
		},
		{
			"no match",
			"some random text without context info",
			-1,
		},
		{
			"empty string",
			"",
			-1,
		},
		{
			"partial match missing percent",
			"85k/200k tokens",
			-1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseContextOutput(tt.in)
			if got != tt.want {
				t.Errorf("parseContextOutput(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
