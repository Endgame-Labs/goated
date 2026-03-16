package agent

import (
	"context"
	"time"

	"goated/internal/db"
)

type RuntimeProvider string

const (
	RuntimeClaude    RuntimeProvider = "claude"
	RuntimeClaudeTUI RuntimeProvider = "claude_tui"
	RuntimeCodexTUI  RuntimeProvider = "codex_tui"
)

type Capabilities struct {
	SupportsInteractiveSession bool
	SupportsContextEstimate    bool
	SupportsCompaction         bool
	SupportsReset              bool
}

type RuntimeDescriptor struct {
	Provider     RuntimeProvider
	DisplayName  string
	SessionName  string
	Capabilities Capabilities
}

type ContextEstimateState string

const (
	ContextEstimateKnown   ContextEstimateState = "known"
	ContextEstimateUnknown ContextEstimateState = "unknown"
)

type ContextEstimate struct {
	State       ContextEstimateState
	PercentUsed int
	RawSummary  string
}

type ResetScope string

const (
	ResetScopeHard        ResetScope = "hard"
	ResetScopeSoft        ResetScope = "soft"
	ResetScopePartial     ResetScope = "partial"
	ResetScopeUnavailable ResetScope = "unavailable"
)

type ResetResult struct {
	Scope   ResetScope
	Summary string
}

type SessionStateKind string

const (
	SessionStateAwaitingInput    SessionStateKind = "awaiting_input"
	SessionStateGenerating       SessionStateKind = "generating"
	SessionStateBlockedAuth      SessionStateKind = "blocked_auth"
	SessionStateBlockedIntervene SessionStateKind = "blocked_intervention"
	SessionStateUnknownStable    SessionStateKind = "unknown_stable"
	SessionStateDead             SessionStateKind = "dead"
)

type SessionState struct {
	Kind    SessionStateKind
	Summary string
}

func (s SessionState) SafeIdle() bool {
	return s.Kind == SessionStateAwaitingInput
}

func (s SessionState) Busy() bool {
	return !s.SafeIdle()
}

type HealthStatus struct {
	OK          bool
	Recoverable bool
	Summary     string
}

// AttachmentInfo describes a single attachment result (success or failure).
type AttachmentInfo struct {
	Index      int
	FileID     string
	Filename   string
	Path       string
	Outcome    string // "ok" or "failed"
	ReasonCode string // e.g. "unsupported_type", "too_large", "download_failed"
	Reason     string
	Bytes      int64
	MIMEType   string
}

// MessageAttachments carries attachment metadata alongside a user prompt.
// A nil value means the message has no attachments.
type MessageAttachments struct {
	Paths     []string         // local file paths of successfully downloaded attachments
	Failed    []AttachmentInfo // attachments that failed to download/validate
	Succeeded []AttachmentInfo // attachments that were successfully saved
}

type SessionRuntime interface {
	Descriptor() RuntimeDescriptor
	EnsureSession(ctx context.Context) error
	StopSession(ctx context.Context) error
	RestartSession(ctx context.Context) error
	ResetConversation(ctx context.Context, chatID string) (ResetResult, error)
	SendUserPrompt(ctx context.Context, channel, chatID, userPrompt string, attachments *MessageAttachments, messageID, threadID string) error
	SendControlCommand(ctx context.Context, text string) error
	GetContextEstimate(ctx context.Context, chatID string) (ContextEstimate, error)
	GetSessionState(ctx context.Context) (SessionState, error)
	WaitForAwaitingInput(ctx context.Context, timeout time.Duration) (SessionState, error)
	GetHealth(ctx context.Context) (HealthStatus, error)
	DetectRetryableError(ctx context.Context) string
	Version(ctx context.Context) string
}

type HeadlessRequest struct {
	WorkspaceDir string
	Prompt       string
	LogPath      string
	Source       string
	CronID       uint64
	ChatID       string
	Silent       bool
	LogCaller    string // e.g. "cron-7", "main-session", "cli"
}

type HeadlessResult struct {
	PID             int
	Status          string
	RuntimeProvider string
	RuntimeMode     string
	RuntimeVersion  string
	Output          []byte
}

type HeadlessRuntime interface {
	Descriptor() RuntimeDescriptor
	RunSync(ctx context.Context, store *db.Store, req HeadlessRequest) (HeadlessResult, error)
	RunBackground(store *db.Store, req HeadlessRequest) (HeadlessResult, error)
	Version(ctx context.Context) string
}

type Runtime interface {
	Session() SessionRuntime
	Headless() HeadlessRuntime
	Descriptor() RuntimeDescriptor
}
