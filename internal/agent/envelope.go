package agent

import (
	"fmt"
	"sort"
	"strings"

	"goated/internal/pydict"
)

// BuildPromptEnvelope constructs the pydict-encoded prompt envelope that gets
// pasted into a tmux agent session. This is runtime-agnostic — both Claude and
// Codex sessions receive the same envelope format.
func BuildPromptEnvelope(channel, chatID, userPrompt string, attachments *MessageAttachments, messageID, threadID string) string {
	var formattingDoc string
	switch channel {
	case "slack":
		formattingDoc = "SLACK_MESSAGE_FORMATTING.md"
	default:
		formattingDoc = "TELEGRAM_MESSAGE_FORMATTING.md"
	}

	kvs := []pydict.KV{
		{"message", strings.TrimSpace(userPrompt)},
		{"source", channel},
		{"chat_id", chatID},
	}

	if messageID != "" {
		kvs = append(kvs, pydict.KV{"message_id", messageID})
	}
	if threadID != "" {
		kvs = append(kvs, pydict.KV{"thread_id", threadID})
	}

	if attachments != nil {
		paths := make([]any, 0, len(attachments.Paths))
		for _, p := range attachments.Paths {
			paths = append(paths, p)
		}
		kvs = append(kvs, pydict.KV{"attachments", paths})
		kvs = append(kvs, pydict.KV{"attachments_failed", attachmentInfosToMaps(attachments.Failed)})
		kvs = append(kvs, pydict.KV{"attachments_succeeded", attachmentInfosToMaps(attachments.Succeeded)})
	}

	kvs = append(kvs,
		pydict.KV{"respond_with", fmt.Sprintf("./goat send_user_message --chat %s", chatID)},
		pydict.KV{"formatting", formattingDoc},
		pydict.KV{"instruction", "Send a plan message first if the task will take longer than 30s."},
	)

	return pydict.EncodeOrdered(kvs)
}

// PromptMessage represents a single message in a batch prompt.
type PromptMessage struct {
	Text        string
	Attachments *MessageAttachments
	MessageID   string
	ThreadID    string
}

// BuildBatchEnvelope constructs a pydict-encoded prompt envelope containing
// multiple user messages. Used when messages accumulate while the runtime is
// busy processing a previous prompt.
func BuildBatchEnvelope(channel, chatID string, messages []PromptMessage) string {
	var formattingDoc string
	switch channel {
	case "slack":
		formattingDoc = "SLACK_MESSAGE_FORMATTING.md"
	default:
		formattingDoc = "TELEGRAM_MESSAGE_FORMATTING.md"
	}

	// Build the messages array
	msgItems := make([]any, 0, len(messages))
	for _, m := range messages {
		item := map[string]any{
			"text": strings.TrimSpace(m.Text),
		}
		if m.MessageID != "" {
			item["message_id"] = m.MessageID
		}
		if m.ThreadID != "" {
			item["thread_id"] = m.ThreadID
		}
		if m.Attachments != nil {
			paths := make([]any, 0, len(m.Attachments.Paths))
			for _, p := range m.Attachments.Paths {
				paths = append(paths, p)
			}
			item["attachments"] = paths
			item["attachments_failed"] = attachmentInfosToMaps(m.Attachments.Failed)
			item["attachments_succeeded"] = attachmentInfosToMaps(m.Attachments.Succeeded)
		}
		msgItems = append(msgItems, item)
	}

	kvs := []pydict.KV{
		{"messages", msgItems},
		{"source", channel},
		{"chat_id", chatID},
		{"respond_with", fmt.Sprintf("./goat send_user_message --chat %s", chatID)},
		{"formatting", formattingDoc},
		{"instruction", "Send a plan message first if the task will take longer than 30s."},
	}

	return pydict.EncodeOrdered(kvs)
}

func BuildSystemNoticeEnvelope(channel, chatID, source, message string, metadata map[string]string) string {
	var metaItems []any
	if len(metadata) > 0 {
		keys := make([]string, 0, len(metadata))
		for k := range metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		metaItems = make([]any, 0, len(keys))
		for _, k := range keys {
			metaItems = append(metaItems, map[string]any{
				"key":   k,
				"value": metadata[k],
			})
		}
	}

	kvs := []pydict.KV{
		{"kind", "system_notice"},
		{"source", channel},
		{"chat_id", chatID},
		{"notice_source", strings.TrimSpace(source)},
		{"message", strings.TrimSpace(message)},
		{"instruction", "Informational system message for context only. No response is needed unless the user explicitly asks about it."},
	}
	if len(metaItems) > 0 {
		kvs = append(kvs, pydict.KV{"metadata", metaItems})
	}
	return pydict.EncodeOrdered(kvs)
}

func attachmentInfosToMaps(infos []AttachmentInfo) []any {
	out := make([]any, 0, len(infos))
	for _, r := range infos {
		out = append(out, map[string]any{
			"index":       r.Index,
			"file_id":     r.FileID,
			"filename":    r.Filename,
			"path":        r.Path,
			"outcome":     r.Outcome,
			"reason_code": r.ReasonCode,
			"reason":      r.Reason,
			"bytes":       r.Bytes,
			"mime_type":   r.MIMEType,
		})
	}
	return out
}
