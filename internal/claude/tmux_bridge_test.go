package claude

import (
	"strings"
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

func TestBuildPromptEnvelope_Slack(t *testing.T) {
	result := buildPromptEnvelope("slack", "C12345", "hello world")

	if !strings.Contains(result, `"message"`) {
		t.Error("missing message key")
	}
	if !strings.Contains(result, "hello world") {
		t.Error("missing user prompt")
	}
	if !strings.Contains(result, `"source"`) {
		t.Error("missing source key")
	}
	if !strings.Contains(result, "slack") {
		t.Error("missing slack source value")
	}
	if !strings.Contains(result, "SLACK_MESSAGE_FORMATTING.md") {
		t.Error("should use SLACK formatting doc for slack channel")
	}
	if !strings.Contains(result, "C12345") {
		t.Error("missing chat_id")
	}
	if !strings.Contains(result, `./goat send_user_message --chat C12345`) {
		t.Error("missing respond_with command")
	}
}

func TestBuildPromptEnvelope_Telegram(t *testing.T) {
	result := buildPromptEnvelope("telegram", "999", "test msg")

	if !strings.Contains(result, "TELEGRAM_MESSAGE_FORMATTING.md") {
		t.Error("should use TELEGRAM formatting doc for telegram channel")
	}
	if !strings.Contains(result, "telegram") {
		t.Error("missing telegram source")
	}
}

func TestBuildPromptEnvelope_UnknownChannelDefaultsTelegram(t *testing.T) {
	result := buildPromptEnvelope("unknown", "111", "test")
	if !strings.Contains(result, "TELEGRAM_MESSAGE_FORMATTING.md") {
		t.Error("unknown channel should default to telegram formatting doc")
	}
}

func TestBuildPromptEnvelope_TrimWhitespace(t *testing.T) {
	result := buildPromptEnvelope("slack", "C1", "  hello  ")
	if !strings.Contains(result, "hello") {
		t.Error("missing trimmed message")
	}
	// The prompt should be trimmed
	if strings.Contains(result, "  hello  ") {
		t.Error("message was not trimmed")
	}
}

func TestBuildPromptEnvelope_IsPydictFormat(t *testing.T) {
	result := buildPromptEnvelope("slack", "C1", "test")
	// Should start with { and end with }
	trimmed := strings.TrimSpace(result)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Errorf("expected pydict format (dict literal), got: %s", trimmed)
	}
}
