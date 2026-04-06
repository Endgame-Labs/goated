package gateway

import "context"

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
	Text                 string
	MessageID            string // platform message ID (e.g. Slack ts)
	ThreadID             string // platform thread ID (e.g. Slack thread_ts)
	Attachments          []string
	AttachmentResults    []AttachmentResult
	AttachmentsFailed    []AttachmentResult
	AttachmentsSucceeded []AttachmentResult
}

type Responder interface {
	SendMessage(ctx context.Context, chatID, text string) error
}

type ThreadedResponder interface {
	SendThreadMessage(ctx context.Context, chatID, threadTS, text string) error
}

// TSResponder extends Responder to return the platform message timestamp.
type TSResponder interface {
	SendMessageTS(ctx context.Context, chatID, text string) (string, error)
	SendThreadMessageTS(ctx context.Context, chatID, threadTS, text string) (string, error)
}

type MediaResponder interface {
	SendMedia(ctx context.Context, chatID, filePath, caption, mediaType string) error
}

type Handler interface {
	HandleMessage(ctx context.Context, msg IncomingMessage, responder Responder) error
	HandleBatchMessage(ctx context.Context, msgs []IncomingMessage, responder Responder) error
}

type Connector interface {
	Run(ctx context.Context, handler Handler) error
}
