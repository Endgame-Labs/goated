package util

import (
	"testing"
)

func TestExtractUserMessage_CurrentFormat(t *testing.T) {
	input := "some preamble\n:START_USER_MESSAGE:\nhello world\n:END_USER_MESSAGE:\nsome postamble"
	got := ExtractUserMessage(input)
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestExtractUserMessage_OldFormat(t *testing.T) {
	input := "preamble\n<<<START_USER_MESSAGE>>>\nold format message\n<<<END_USER_MESSAGE>>>\npost"
	got := ExtractUserMessage(input)
	if got != "old format message" {
		t.Errorf("got %q, want %q", got, "old format message")
	}
}

func TestExtractUserMessage_OldFormatTruncatedEnd(t *testing.T) {
	// The regex also handles <<<END_USER_ESSAGE>>> (missing M)
	input := "<<<START_USER_MESSAGE>>>\ntruncated\n<<<END_USER_ESSAGE>>>"
	got := ExtractUserMessage(input)
	if got != "truncated" {
		t.Errorf("got %q, want %q", got, "truncated")
	}
}

func TestExtractUserMessage_LegacyFormat(t *testing.T) {
	input := "preamble\n<<>>\nlegacy message\n<<>>\npost"
	got := ExtractUserMessage(input)
	if got != "legacy message" {
		t.Errorf("got %q, want %q", got, "legacy message")
	}
}

func TestExtractUserMessage_NoMatch(t *testing.T) {
	input := "just some random text without delimiters"
	got := ExtractUserMessage(input)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractUserMessage_PlaceholderOnly(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"parenthesized", ":START_USER_MESSAGE:\n(your response)\n:END_USER_MESSAGE:"},
		{"plain", ":START_USER_MESSAGE:\nyour response\n:END_USER_MESSAGE:"},
		{"extra spaces", ":START_USER_MESSAGE:\n Your Response \n:END_USER_MESSAGE:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractUserMessage(tt.input)
			if got != "" {
				t.Errorf("got %q, want empty (placeholder should be filtered)", got)
			}
		})
	}
}

func TestExtractUserMessage_MultipleMatches(t *testing.T) {
	// Should return the LAST match
	input := ":START_USER_MESSAGE:\nfirst\n:END_USER_MESSAGE:\n:START_USER_MESSAGE:\nsecond\n:END_USER_MESSAGE:"
	got := ExtractUserMessage(input)
	if got != "second" {
		t.Errorf("got %q, want %q (should return last match)", got, "second")
	}
}

func TestExtractUserMessage_WithANSI(t *testing.T) {
	// ANSI escape sequences should be stripped before matching
	input := "\x1b[32m:START_USER_MESSAGE:\x1b[0m\nhello\n\x1b[32m:END_USER_MESSAGE:\x1b[0m"
	got := ExtractUserMessage(input)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExtractUserMessage_WithTUIBullets(t *testing.T) {
	input := "● :START_USER_MESSAGE:\nhello\n● :END_USER_MESSAGE:"
	got := ExtractUserMessage(input)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExtractUserMessage_WhitespaceNormalization(t *testing.T) {
	input := ":START_USER_MESSAGE:\n\n\n\nhello\n\n\n\nworld\n\n\n\n:END_USER_MESSAGE:"
	got := ExtractUserMessage(input)
	want := "hello\n\nworld"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractUserMessage_CursorForward(t *testing.T) {
	// \x1b[5C should be replaced with 5 spaces
	input := ":START_USER_MESSAGE:\x1b[5Chello:END_USER_MESSAGE:"
	got := ExtractUserMessage(input)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExtractUserMessage_TrailingSpaces(t *testing.T) {
	input := ":START_USER_MESSAGE:\nhello   \nworld  \n:END_USER_MESSAGE:"
	got := ExtractUserMessage(input)
	want := "hello\nworld"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
