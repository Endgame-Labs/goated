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
func BuildPromptEnvelope(channel, chatID, userPrompt string, attachments *MessageAttachments, messageID, threadID string, msgCtx *MessageContext) string {
	var formattingDoc string
	switch channel {
	case "slack":
		formattingDoc = "SLACK_MESSAGE_FORMATTING.md"
	default:
		formattingDoc = "TELEGRAM_MESSAGE_FORMATTING.md"
	}

	kvs := []pydict.KV{
		{Key: "message", Value: strings.TrimSpace(userPrompt)},
		{Key: "source", Value: channel},
		{Key: "chat_id", Value: chatID},
	}

	if msgCtx != nil {
		if msgCtx.ChatType != "" {
			kvs = append(kvs, pydict.KV{Key: "chat_type", Value: msgCtx.ChatType})
		}
		if msgCtx.UserID != "" {
			kvs = append(kvs, pydict.KV{Key: "user_id", Value: msgCtx.UserID})
		}
		if msgCtx.UserName != "" {
			kvs = append(kvs, pydict.KV{Key: "user_name", Value: msgCtx.UserName})
		}
		if msgCtx.UserUsername != "" {
			kvs = append(kvs, pydict.KV{Key: "user_username", Value: msgCtx.UserUsername})
		}
		if msgCtx.ReplyToText != "" {
			kvs = append(kvs, pydict.KV{Key: "reply_to_text", Value: msgCtx.ReplyToText})
		}
		if msgCtx.ReplyToUserName != "" {
			kvs = append(kvs, pydict.KV{Key: "reply_to_user", Value: msgCtx.ReplyToUserName})
		}
	}

	if messageID != "" {
		kvs = append(kvs, pydict.KV{Key: "message_id", Value: messageID})
	}
	if threadID != "" {
		kvs = append(kvs, pydict.KV{Key: "thread_id", Value: threadID})
	}

	if attachments != nil {
		paths := make([]any, 0, len(attachments.Paths))
		for _, p := range attachments.Paths {
			paths = append(paths, p)
		}
		kvs = append(kvs, pydict.KV{Key: "attachments", Value: paths})
		kvs = append(kvs, pydict.KV{Key: "attachments_failed", Value: attachmentInfosToMaps(attachments.Failed)})
		kvs = append(kvs, pydict.KV{Key: "attachments_succeeded", Value: attachmentInfosToMaps(attachments.Succeeded)})
	}

	kvs = append(kvs,
		pydict.KV{Key: "respond_with", Value: fmt.Sprintf("./goat send_user_message --chat %s", chatID)},
		pydict.KV{Key: "formatting", Value: formattingDoc},
		pydict.KV{Key: "instruction", Value: "Send a plan message first if the task will take longer than 30s."},
	)

	return pydict.EncodeOrdered(kvs)
}

// PromptMessage represents a single message in a batch prompt.
type PromptMessage struct {
	Text        string
	Attachments *MessageAttachments
	MessageID   string
	ThreadID    string
	Context     *MessageContext
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
		if m.Context != nil {
			if m.Context.ChatType != "" {
				item["chat_type"] = m.Context.ChatType
			}
			if m.Context.UserID != "" {
				item["user_id"] = m.Context.UserID
			}
			if m.Context.UserName != "" {
				item["user_name"] = m.Context.UserName
			}
			if m.Context.UserUsername != "" {
				item["user_username"] = m.Context.UserUsername
			}
			if m.Context.ReplyToText != "" {
				item["reply_to_text"] = m.Context.ReplyToText
			}
			if m.Context.ReplyToUserName != "" {
				item["reply_to_user"] = m.Context.ReplyToUserName
			}
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
		{Key: "messages", Value: msgItems},
		{Key: "source", Value: channel},
		{Key: "chat_id", Value: chatID},
		{Key: "respond_with", Value: fmt.Sprintf("./goat send_user_message --chat %s", chatID)},
		{Key: "formatting", Value: formattingDoc},
		{Key: "instruction", Value: "Send a plan message first if the task will take longer than 30s."},
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
		{Key: "kind", Value: "system_notice"},
		{Key: "source", Value: channel},
		{Key: "chat_id", Value: chatID},
		{Key: "notice_source", Value: strings.TrimSpace(source)},
		{Key: "message", Value: strings.TrimSpace(message)},
		{Key: "instruction", Value: "Informational system message for context only. No response is needed unless the user explicitly asks about it."},
	}
	if len(metaItems) > 0 {
		kvs = append(kvs, pydict.KV{Key: "metadata", Value: metaItems})
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
