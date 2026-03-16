package agent

import (
	"strings"
	"testing"
)

func TestBuildPromptEnvelope_Slack(t *testing.T) {
	result := BuildPromptEnvelope("slack", "C12345", "hello world", &MessageAttachments{
		Paths:     []string{"workspace/tmp/slack/attachments/a.png"},
		Failed:    []AttachmentInfo{{ReasonCode: "too_large"}},
		Succeeded: []AttachmentInfo{{Path: "workspace/tmp/slack/attachments/a.png"}},
	}, "1710000000.000100", "1709999000.000050")

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
	if !strings.Contains(result, `"message_id"`) {
		t.Error("missing message_id key")
	}
	if !strings.Contains(result, "1710000000.000100") {
		t.Error("missing message_id value")
	}
	if !strings.Contains(result, `"thread_id"`) {
		t.Error("missing thread_id key")
	}
	if !strings.Contains(result, "1709999000.000050") {
		t.Error("missing thread_id value")
	}
	if !strings.Contains(result, `"attachments_failed"`) {
		t.Error("missing attachments_failed key")
	}
	if !strings.Contains(result, `"reason_code": "too_large"`) {
		t.Error("missing failed attachment reason_code")
	}
	if !strings.Contains(result, `"attachments_succeeded"`) {
		t.Error("missing attachments_succeeded key")
	}
}

func TestBuildPromptEnvelope_NoAttachments(t *testing.T) {
	result := BuildPromptEnvelope("slack", "C1", "test", nil, "", "")
	if strings.Contains(result, "attachments") {
		t.Error("nil attachments should not produce attachment keys")
	}
}

func TestBuildPromptEnvelope_Telegram(t *testing.T) {
	result := BuildPromptEnvelope("telegram", "999", "test msg", nil, "", "")

	if !strings.Contains(result, "TELEGRAM_MESSAGE_FORMATTING.md") {
		t.Error("should use TELEGRAM formatting doc for telegram channel")
	}
	if !strings.Contains(result, "telegram") {
		t.Error("missing telegram source")
	}
}

func TestBuildPromptEnvelope_UnknownChannelDefaultsTelegram(t *testing.T) {
	result := BuildPromptEnvelope("unknown", "111", "test", nil, "", "")
	if !strings.Contains(result, "TELEGRAM_MESSAGE_FORMATTING.md") {
		t.Error("unknown channel should default to telegram formatting doc")
	}
}

func TestBuildPromptEnvelope_TrimWhitespace(t *testing.T) {
	result := BuildPromptEnvelope("slack", "C1", "  hello  ", nil, "", "")
	if !strings.Contains(result, "hello") {
		t.Error("missing trimmed message")
	}
	if strings.Contains(result, "  hello  ") {
		t.Error("message was not trimmed")
	}
}

func TestBuildPromptEnvelope_IsPydictFormat(t *testing.T) {
	result := BuildPromptEnvelope("slack", "C1", "test", nil, "", "")
	trimmed := strings.TrimSpace(result)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Errorf("expected pydict format (dict literal), got: %s", trimmed)
	}
}
