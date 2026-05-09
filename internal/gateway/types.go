package gateway

import (
	"context"
	"encoding/json"
)

type AttachmentResult struct {
	Index      int
	FileID     string
	Filename   string
	Path       string
	Outcome    string
	ReasonCode string
	Reason     string
	Bytes      int64
	MIMEType   string
}

type IncomingMessage struct {
	Channel              string
	ChatID               string
	UserID               string
	UserName             string // display name of the sender (first + last)
	UserUsername         string // @handle of the sender (no @), may be empty
	ChatType             string // "private", "group", "supergroup", "channel"
	Text                 string
	MessageID            string // platform message ID (e.g. Slack ts)
	ThreadID             string // platform thread ID (e.g. Slack thread_ts)
	Reaction             string // emoji name if this is a reaction event (e.g. "white_check_mark")
	ReactionMessageID    string // message timestamp the reaction was applied to
	Attachments          []string
	AttachmentResults    []AttachmentResult
	AttachmentsFailed    []AttachmentResult
	AttachmentsSucceeded []AttachmentResult
	ReplyToText          string // text of the message being replied to (empty if not a reply)
	ReplyToUserName      string // display name of the author of the replied-to message
}

type Responder interface {
	SendMessage(ctx context.Context, chatID, text string) error
}

type ThreadedResponder interface {
	SendThreadMessage(ctx context.Context, chatID, threadTS, text string) error
}

type MediaResponder interface {
	SendMedia(ctx context.Context, chatID, filePath, caption, mediaType string) error
}

// BlockResponder sends rich Block Kit messages. The blocksJSON payload is
// kept as json.RawMessage so the gateway package stays Slack-agnostic.
type BlockResponder interface {
	SendBlockMessage(ctx context.Context, chatID, fallbackText string, blocksJSON json.RawMessage) error
	SendThreadBlockMessage(ctx context.Context, chatID, threadTS, fallbackText string, blocksJSON json.RawMessage) error
}

type Handler interface {
	HandleMessage(ctx context.Context, msg IncomingMessage, responder Responder) error
	HandleBatchMessage(ctx context.Context, msgs []IncomingMessage, responder Responder) error
}

type Connector interface {
	Run(ctx context.Context, handler Handler) error
}
