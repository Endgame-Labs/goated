package agent

import (
	"fmt"
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
