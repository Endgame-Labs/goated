package slack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"goated/internal/app"
	"goated/internal/gateway"
	runtimepkg "goated/internal/runtime"
	"goated/internal/util"
)

const (
	defaultAttachmentsRootRel     = "workspace/tmp/slack/attachments"
	defaultAttachmentMaxBytes     = int64(25 * 1024 * 1024)
	defaultAttachmentTotalMax     = int64(251 * 1024 * 1024)
	defaultAttachmentRetention    = 30 * 24 * time.Hour
	defaultAttachmentSweepEvery   = 4 * time.Hour
	defaultAttachmentParallelHint = 3
)

var allowedAttachmentMIMEs = map[string]struct{}{
	"application/pdf":           {},
	"text/csv":                  {},
	"text/tab-separated-values": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/csv":                                  {},
	"application/vnd.ms-excel":                         {},
	"application/vnd.ms-excel.sheet.macroenabled.12":   {},
	"application/vnd.ms-word.document.macroenabled.12": {},
}

var allowedAttachmentExts = map[string]struct{}{
	".png":  {},
	".jpg":  {},
	".jpeg": {},
	".gif":  {},
	".webp": {},
	".bmp":  {},
	".heic": {},
	".heif": {},
	".csv":  {},
	".tsv":  {},
	".xlsx": {},
	".docx": {},
	".pdf":  {},
}

// OffsetStore persists metadata so restarts can track state.
type OffsetStore interface {
	GetMeta(key string) string
	SetMeta(key, value string) error
}

// Connector receives messages from a single Slack DM channel via Socket Mode
// and sends responses back through the Slack Web API.
type Connector struct {
	api       *slack.Client
	socket    *socketmode.Client
	store     OffsetStore
	channelID string // the single allowed DM channel
	botToken  string

	httpClient *http.Client

	attachmentsRootRel string
	attachmentsRootAbs string

	attachmentMaxBytes  int64
	attachmentTotalMax  int64
	attachmentRetention time.Duration
	sweepEvery          time.Duration
	parallelHint        int

	mu         sync.Mutex
	thinkingTS string          // timestamp of the current "_thinking..._" message
	seenEvents map[string]bool // dedup retried Slack events

	msgQueue chan slackevents.EventsAPIEvent // serializes message processing
}

type AttachmentConfig struct {
	RootPath      string
	MaxBytes      int64
	MaxTotalBytes int64
	MaxParallel   int
}

type attachmentSidecar struct {
	IngestedAt         string `json:"ingested_at"`
	FileID             string `json:"file_id"`
	Filename           string `json:"filename"`
	MIMEType           string `json:"mime_type"`
	StoredName         string `json:"stored_name"`
	StoredRelativePath string `json:"stored_relative_path"`
	Bytes              int64  `json:"bytes"`
	SHA256             string `json:"sha256"`
}

// NewConnector creates a Slack connector.
// botToken is the Bot User OAuth Token (xoxb-...).
// appToken is the App-Level Token (xapp-...) required for Socket Mode.
// channelID restricts the bot to a single DM channel.
func NewConnector(botToken, appToken, channelID string, store OffsetStore, attachmentCfg AttachmentConfig) (*Connector, error) {
	if botToken == "" {
		return nil, fmt.Errorf("slack bot token is required")
	}
	if appToken == "" {
		return nil, fmt.Errorf("slack app token is required (xapp-... for socket mode)")
	}
	if channelID == "" {
		return nil, fmt.Errorf("slack channel ID is required")
	}

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken), slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stderr, "slack-api: ", log.Lshortfile|log.LstdFlags)))
	socket := socketmode.New(api,
		socketmode.OptionDebug(true),
		socketmode.OptionLog(log.New(os.Stderr, "slack-socket: ", log.Lshortfile|log.LstdFlags)))

	rootPath := strings.TrimSpace(attachmentCfg.RootPath)
	if rootPath == "" {
		rootPath = defaultAttachmentsRootRel
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve attachments root: %w", err)
	}
	if err := os.MkdirAll(rootAbs, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir attachments root: %w", err)
	}
	rootRel := filepath.ToSlash(rootPath)
	if filepath.IsAbs(rootPath) {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, rootPath); err == nil {
				rootRel = filepath.ToSlash(rel)
			}
		}
	}
	maxBytes := attachmentCfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultAttachmentMaxBytes
	}
	maxTotalBytes := attachmentCfg.MaxTotalBytes
	if maxTotalBytes <= 0 {
		maxTotalBytes = defaultAttachmentTotalMax
	}
	maxParallel := attachmentCfg.MaxParallel
	if maxParallel <= 0 {
		maxParallel = defaultAttachmentParallelHint
	}

	return &Connector{
		api:                 api,
		socket:              socket,
		store:               store,
		channelID:           channelID,
		botToken:            botToken,
		httpClient:          &http.Client{Timeout: 2 * time.Minute},
		attachmentsRootRel:  rootRel,
		attachmentsRootAbs:  rootAbs,
		attachmentMaxBytes:  maxBytes,
		attachmentTotalMax:  maxTotalBytes,
		attachmentRetention: defaultAttachmentRetention,
		sweepEvery:          defaultAttachmentSweepEvery,
		parallelHint:        maxParallel,
		seenEvents:          make(map[string]bool),
		msgQueue:            make(chan slackevents.EventsAPIEvent, 100),
	}, nil
}

// Run connects via Socket Mode and processes incoming messages.
func (c *Connector) Run(ctx context.Context, handler gateway.Handler) error {
	go c.runAttachmentSweeper(ctx)

	// Single goroutine processes messages sequentially (like Telegram).
	// This ensures only one thinking indicator is active at a time and
	// messages are handled in order.
	go c.processMessageQueue(ctx, handler)

	go func() {
		for evt := range c.socket.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				c.socket.Ack(*evt.Request)
				c.msgQueue <- eventsAPIEvent

			case socketmode.EventTypeConnecting:
				fmt.Fprintln(os.Stderr, "Slack Socket Mode: connecting...")

			case socketmode.EventTypeConnected:
				fmt.Fprintln(os.Stderr, "Slack Socket Mode: connected")

			case socketmode.EventTypeConnectionError:
				fmt.Fprintln(os.Stderr, "Slack Socket Mode: connection error")

			case socketmode.EventTypeHello:
				// No action needed — connection is alive

			case socketmode.EventTypeDisconnect:
				fmt.Fprintln(os.Stderr, "Slack Socket Mode: disconnect requested, reconnecting...")

			case socketmode.EventTypeInteractive:
				if evt.Request != nil {
					c.socket.Ack(*evt.Request)
				}

			case socketmode.EventTypeSlashCommand:
				if evt.Request != nil {
					c.socket.Ack(*evt.Request)
				}

			default:
				fmt.Fprintf(os.Stderr, "Slack Socket Mode: unhandled event type %s\n", evt.Type)
				if evt.Request != nil && evt.Request.EnvelopeID != "" {
					c.socket.Ack(*evt.Request)
				}
			}
		}
	}()

	return c.socket.RunContext(ctx)
}

// processMessageQueue drains msgQueue sequentially, processing one message
// at a time. After each message completes, any messages that accumulated
// during processing are drained and sent as a single batch.
func (c *Connector) processMessageQueue(ctx context.Context, handler gateway.Handler) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-c.msgQueue:
			c.handleEventsAPI(ctx, handler, event)
			// After processing, drain any messages that arrived and batch them
			c.drainAndBatch(ctx, handler)
		}
	}
}

// drainAndBatch collects all currently queued events and sends them as a
// single batch message. This prevents ordering issues when multiple messages
// arrive while the runtime is busy processing.
func (c *Connector) drainAndBatch(ctx context.Context, handler gateway.Handler) {
	var batch []slackevents.EventsAPIEvent
	for {
		select {
		case e := <-c.msgQueue:
			batch = append(batch, e)
		default:
			// No more queued events
			if len(batch) == 0 {
				return
			}
			c.handleBatchEventsAPI(ctx, handler, batch)
			// Recurse: the batch handler may have taken time, more could have arrived
			c.drainAndBatch(ctx, handler)
			return
		}
	}
}

func (c *Connector) handleEventsAPI(ctx context.Context, handler gateway.Handler, event slackevents.EventsAPIEvent) {
	msg, ok := c.eventToMessage(ctx, event)
	if !ok {
		return
	}

	// Post a thinking indicator while processing
	c.postThinking(msg.ChatID)

	if err := handler.HandleMessage(ctx, msg, c); err != nil {
		_ = c.SendMessage(ctx, msg.ChatID, "Error: "+err.Error())
	}
}

// handleBatchEventsAPI processes multiple events as a single batch message.
func (c *Connector) handleBatchEventsAPI(ctx context.Context, handler gateway.Handler, events []slackevents.EventsAPIEvent) {
	var msgs []gateway.IncomingMessage
	for _, event := range events {
		msg, ok := c.eventToMessage(ctx, event)
		if !ok {
			continue
		}
		msgs = append(msgs, msg)
	}

	if len(msgs) == 0 {
		return
	}

	// Post a thinking indicator
	c.postThinking(msgs[0].ChatID)

	fmt.Fprintf(os.Stderr, "[%s] batching %d queued messages\n",
		time.Now().Format(time.RFC3339), len(msgs))

	if err := handler.HandleBatchMessage(ctx, msgs, c); err != nil {
		_ = c.SendMessage(ctx, msgs[0].ChatID, "Error: "+err.Error())
	}
}

// eventToMessage converts a Slack EventsAPI event into a gateway IncomingMessage.
// Returns false if the event should be skipped (bot message, dedup, wrong channel, etc).
func (c *Connector) eventToMessage(ctx context.Context, event slackevents.EventsAPIEvent) (gateway.IncomingMessage, bool) {
	if event.Type != slackevents.CallbackEvent {
		return gateway.IncomingMessage{}, false
	}
	var outerEventID string
	switch cb := event.Data.(type) {
	case slackevents.EventsAPICallbackEvent:
		outerEventID = cb.EventID
	case *slackevents.EventsAPICallbackEvent:
		outerEventID = cb.EventID
	}

	innerEvent := event.InnerEvent

	// Handle reaction_added events
	if rev, ok := innerEvent.Data.(*slackevents.ReactionAddedEvent); ok {
		return c.reactionToMessage(rev, outerEventID, event.TeamID)
	}

	ev, ok := innerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return gateway.IncomingMessage{}, false
	}

	if ev.BotID != "" {
		return gateway.IncomingMessage{}, false
	}

	files := extractSlackFiles(ev)
	if ev.SubType != "" && ev.SubType != slack.MsgSubTypeFileShare && len(files) == 0 {
		return gateway.IncomingMessage{}, false
	}

	dedupKey := c.messageDedupKey(event.TeamID, outerEventID, ev, files)
	c.mu.Lock()
	if c.seenEvents[dedupKey] {
		c.mu.Unlock()
		return gateway.IncomingMessage{}, false
	}
	c.seenEvents[dedupKey] = true
	c.mu.Unlock()

	// Redirect messages from non-monitored channels
	if ev.Channel != c.channelID {
		_ = c.SendMessage(ctx, ev.Channel,
			"This isn't the channel I'm monitoring. Go to the configured DM channel to chat with me.")
		return gateway.IncomingMessage{}, false
	}

	text := strings.TrimSpace(ev.Text)
	attachments, attachmentResults, failed, succeeded := c.processAttachments(ctx, ev, files)
	if text == "" && len(files) == 0 {
		return gateway.IncomingMessage{}, false
	}

	if len(files) > 0 {
		fmt.Fprintf(os.Stderr,
			"slack attachments summary: count=%d accepted=%d failed=%d bytes=%d\n",
			len(files), len(succeeded), len(failed), sumAttachmentBytes(succeeded),
		)
	}

	return gateway.IncomingMessage{
		Channel:              "slack",
		ChatID:               ev.Channel,
		UserID:               ev.User,
		Text:                 text,
		MessageID:            ev.TimeStamp,
		ThreadID:             ev.ThreadTimeStamp,
		Attachments:          attachments,
		AttachmentResults:    attachmentResults,
		AttachmentsFailed:    failed,
		AttachmentsSucceeded: succeeded,
	}, true
}

// reactionToMessage converts a ReactionAddedEvent into a gateway IncomingMessage.
// Only reactions on messages in the monitored channel are forwarded.
func (c *Connector) reactionToMessage(ev *slackevents.ReactionAddedEvent, outerEventID, teamID string) (gateway.IncomingMessage, bool) {
	// Only handle reactions on messages (not files or file comments)
	if ev.Item.Type != "message" {
		return gateway.IncomingMessage{}, false
	}

	// Only handle reactions in the monitored channel
	if ev.Item.Channel != c.channelID {
		return gateway.IncomingMessage{}, false
	}

	// Dedup using outer event ID + reaction details
	dedupKey := fmt.Sprintf("reaction:%s:%s:%s:%s", teamID, outerEventID, ev.Reaction, ev.Item.Timestamp)
	c.mu.Lock()
	if c.seenEvents[dedupKey] {
		c.mu.Unlock()
		return gateway.IncomingMessage{}, false
	}
	c.seenEvents[dedupKey] = true
	c.mu.Unlock()

	fmt.Fprintf(os.Stderr, "[%s] reaction_added: user=%s reaction=%s on message=%s channel=%s\n",
		time.Now().Format(time.RFC3339), ev.User, ev.Reaction, ev.Item.Timestamp, ev.Item.Channel)

	return gateway.IncomingMessage{
		Channel:           "slack",
		ChatID:            ev.Item.Channel,
		UserID:            ev.User,
		Text:              ev.Reaction,
		MessageID:         ev.EventTimestamp,
		ThreadID:          ev.Item.Timestamp,
		Reaction:          ev.Reaction,
		ReactionMessageID: ev.Item.Timestamp,
	}, true
}

// SendMessage sends a message to the specified Slack channel, converting
// markdown to Slack's mrkdwn format. Clears any active thinking indicator first.
func (c *Connector) SendMessage(_ context.Context, channelID, text string) error {
	c.clearThinkingIfNeeded(channelID)

	mrkdwn := util.MarkdownToSlackMrkdwn(text)

	// Slack has a 4000-char limit per message; split if needed
	chunks := splitMessage(mrkdwn, 4000)
	for _, chunk := range chunks {
		_, _, err := c.api.PostMessage(channelID,
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionDisableLinkUnfurl(),
		)
		if err != nil {
			return fmt.Errorf("send slack message: %w", err)
		}
	}

	return nil
}

// SendThreadMessage sends a message as a reply to a specific thread.
func (c *Connector) SendThreadMessage(_ context.Context, channelID, threadTS, text string) error {
	c.clearThinkingIfNeeded(channelID)

	mrkdwn := util.MarkdownToSlackMrkdwn(text)

	chunks := splitMessage(mrkdwn, 4000)
	for _, chunk := range chunks {
		_, _, err := c.api.PostMessage(channelID,
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionDisableLinkUnfurl(),
			slack.MsgOptionTS(threadTS),
		)
		if err != nil {
			return fmt.Errorf("send slack thread message: %w", err)
		}
	}

	return nil
}

// postThinking posts a "_thinking..._" message and records its timestamp
// so it can be updated with the real response or deleted later.
// Skips posting if a thinking indicator is already active (e.g. queued messages).
// Also spawns a TTL reaper to guarantee cleanup even if normal paths fail.
func (c *Connector) postThinking(channel string) {
	c.mu.Lock()
	if c.thinkingTS != "" {
		c.mu.Unlock()
		return // already showing a thinking indicator
	}
	c.mu.Unlock()

	_, ts, err := c.api.PostMessage(channel,
		slack.MsgOptionText("_thinking..._", false),
	)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.thinkingTS = ts
	c.mu.Unlock()
	_ = WriteThinkingTS(ts)
	go reapThinkingIndicator(c.api, channel, ts)
}

// clearThinkingIfNeeded deletes the thinking message if it's still present.
// Returns true if a thinking indicator was active (whether we deleted it or
// the CLI already did).
func (c *Connector) clearThinkingIfNeeded(channel string) bool {
	c.mu.Lock()
	ts := c.thinkingTS
	c.thinkingTS = ""
	c.mu.Unlock()
	if ts == "" {
		return false
	}
	// Atomically claim the file so no other process can also try to delete
	// the Slack message. If ClaimThinkingTS returns empty, the CLI already
	// handled it — but we still delete using our in-memory ts.
	ClaimThinkingTS()
	_, _, _ = c.api.DeleteMessage(channel, ts)
	return true
}

// reapThinkingIndicator is a TTL safety net for thinking indicators.
// Soft deadline: 4 minutes — deletes if the active runtime is idle.
// If the runtime is still busy, rechecks every minute.
// Hard deadline: 20 minutes — deletes unconditionally.
func reapThinkingIndicator(api *slack.Client, channel, ts string) {
	const softDeadline = 4 * time.Minute
	const hardDeadline = 20 * time.Minute
	const recheckInterval = 1 * time.Minute

	time.Sleep(softDeadline)

	// If the file is already gone, another path cleaned it up — we're done.
	if !thinkingFileHasTS(ts) {
		return
	}

	cfg := app.LoadConfig()
	runtime, _ := runtimepkg.New(cfg)
	hardCutoff := time.Now().Add(hardDeadline - softDeadline)

	for {
		if time.Now().After(hardCutoff) {
			break // hard deadline reached, delete unconditionally
		}
		if runtime != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			state, err := runtime.Session().GetSessionState(ctx)
			cancel()
			if err == nil && state.SafeIdle() {
				break // runtime is idle, safe to delete
			}
		}
		time.Sleep(recheckInterval)
		if !thinkingFileHasTS(ts) {
			return // cleaned up by normal path while we waited
		}
	}

	// Delete the Slack message (no-op if already deleted)
	_, _, _ = api.DeleteMessage(channel, ts)
	// Atomically claim and remove ThinkingFile if it still holds our timestamp
	ClaimThinkingTS()
	fmt.Fprintf(os.Stderr, "TTL reaper cleaned up thinking indicator %s in channel %s\n", ts, channel)
}

// thinkingFileHasTS returns true if ThinkingFile exists and contains the given timestamp.
func thinkingFileHasTS(ts string) bool {
	data, err := os.ReadFile(ThinkingFile)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == ts
}

// ReapThinkingIndicator is the exported version for use by CLI processes.
func ReapThinkingIndicator(api *slack.Client, channel, ts string) {
	reapThinkingIndicator(api, channel, ts)
}

// splitMessage breaks a message into chunks that fit Slack's size limit.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Try to split at a newline
		cut := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
			cut = idx + 1
		}

		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	return chunks
}

func extractSlackFiles(ev *slackevents.MessageEvent) []slack.File {
	if ev == nil || ev.Message == nil || len(ev.Message.Files) == 0 {
		return nil
	}
	return ev.Message.Files
}

func (c *Connector) messageDedupKey(teamID, outerEventID string, ev *slackevents.MessageEvent, files []slack.File) string {
	if outerEventID != "" {
		return outerEventID
	}
	fileIDs := make([]string, 0, len(files))
	for _, f := range files {
		if f.ID != "" {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	sort.Strings(fileIDs)
	return fmt.Sprintf("%s|%s|%s|%s", teamID, ev.Channel, ev.TimeStamp, strings.Join(fileIDs, ","))
}

func (c *Connector) processAttachments(ctx context.Context, ev *slackevents.MessageEvent, files []slack.File) ([]string, []gateway.AttachmentResult, []gateway.AttachmentResult, []gateway.AttachmentResult) {
	attachments := make([]string, 0, len(files))
	results := make([]gateway.AttachmentResult, 0, len(files))
	failed := make([]gateway.AttachmentResult, 0, len(files))
	succeeded := make([]gateway.AttachmentResult, 0, len(files))

	seenFileIDs := map[string]struct{}{}
	var acceptedTotal int64

	for i, f := range files {
		res := gateway.AttachmentResult{
			Index:    i,
			FileID:   f.ID,
			Filename: f.Name,
			MIMEType: strings.ToLower(strings.TrimSpace(f.Mimetype)),
		}

		if f.ID != "" {
			if _, ok := seenFileIDs[f.ID]; ok {
				res.Outcome = "failed"
				res.ReasonCode = "deduped"
				res.Reason = "duplicate file id in message"
				results = append(results, res)
				failed = append(failed, res)
				continue
			}
			seenFileIDs[f.ID] = struct{}{}
		}

		if !isAllowedByMetadata(f.Name, f.Mimetype) {
			res.Outcome = "failed"
			res.ReasonCode = "unsupported_type"
			res.Reason = "unsupported file type"
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		if f.Size > 0 && int64(f.Size) > c.attachmentMaxBytes {
			res.Outcome = "failed"
			res.ReasonCode = "too_large"
			res.Reason = "file exceeds per-attachment size limit"
			res.Bytes = int64(f.Size)
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		if f.Size > 0 && acceptedTotal+int64(f.Size) > c.attachmentTotalMax {
			res.Outcome = "failed"
			res.ReasonCode = "too_large"
			res.Reason = "aggregate attachment size exceeds per-message limit"
			res.Bytes = int64(f.Size)
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		relPath, written, detectedMIME, reasonCode, reason := c.downloadAndPersistAttachment(ctx, ev, f)
		if reasonCode != "" {
			res.Outcome = "failed"
			res.ReasonCode = reasonCode
			res.Reason = reason
			res.Bytes = written
			results = append(results, res)
			failed = append(failed, res)
			continue
		}
		if acceptedTotal+written > c.attachmentTotalMax {
			_ = os.Remove(filepath.Join(c.attachmentsRootAbs, filepath.Base(relPath)))
			_ = os.Remove(filepath.Join(c.attachmentsRootAbs, filepath.Base(relPath)+".meta.json"))
			res.Outcome = "failed"
			res.ReasonCode = "too_large"
			res.Reason = "aggregate attachment size exceeds per-message limit"
			res.Bytes = written
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		acceptedTotal += written
		res.Outcome = "succeeded"
		res.Path = relPath
		res.Bytes = written
		if detectedMIME != "" {
			res.MIMEType = detectedMIME
		}
		attachments = append(attachments, relPath)
		results = append(results, res)
		succeeded = append(succeeded, res)
	}

	return attachments, results, failed, succeeded
}

func (c *Connector) downloadAndPersistAttachment(ctx context.Context, ev *slackevents.MessageEvent, f slack.File) (relativePath string, written int64, detectedMIME string, reasonCode string, reason string) {
	if f.URLPrivateDownload == "" && f.URLPrivate == "" {
		return "", 0, "", "download_failed", "missing file download URL"
	}

	url := f.URLPrivateDownload
	if url == "" {
		url = f.URLPrivate
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, "", "download_failed", "failed creating download request"
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, "", "download_failed", "download request failed"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", 0, "", "unauthorized", "unauthorized while downloading attachment"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, "", "download_failed", fmt.Sprintf("download failed with status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(c.attachmentsRootAbs, "download-*.part")
	if err != nil {
		return "", 0, "", "download_failed", "failed creating temp file"
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	h := sha256.New()
	buf := make([]byte, 32*1024)
	sniff := make([]byte, 0, 512)
	for {
		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			if written+int64(nr) > c.attachmentMaxBytes {
				_ = tmpFile.Close()
				return "", written, "", "too_large", "file exceeds per-attachment size limit"
			}
			if _, ew := tmpFile.Write(buf[:nr]); ew != nil {
				_ = tmpFile.Close()
				return "", written, "", "download_failed", "failed writing attachment"
			}
			if _, ew := h.Write(buf[:nr]); ew != nil {
				_ = tmpFile.Close()
				return "", written, "", "download_failed", "failed hashing attachment"
			}
			if len(sniff) < 512 {
				need := 512 - len(sniff)
				if need > nr {
					need = nr
				}
				sniff = append(sniff, buf[:need]...)
			}
			written += int64(nr)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			_ = tmpFile.Close()
			return "", written, "", "download_failed", "attachment download interrupted"
		}
	}

	if written == 0 {
		_ = tmpFile.Close()
		return "", 0, "", "download_failed", "attachment was empty"
	}

	detectedMIME = http.DetectContentType(sniff)
	if !isAllowedByContent(f.Name, f.Mimetype, detectedMIME) {
		_ = tmpFile.Close()
		return "", written, detectedMIME, "corrupt", "file content type does not match allowed formats"
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return "", written, detectedMIME, "download_failed", "failed flushing attachment to disk"
	}
	if err := tmpFile.Close(); err != nil {
		return "", written, detectedMIME, "download_failed", "failed closing attachment temp file"
	}

	ext := normalizedAttachmentExt(f.Name)
	if ext == "" {
		ext = extFromMIME(f.Mimetype)
	}
	seed := fmt.Sprintf("%s|%s|%s|%d", f.ID, f.Name, ev.TimeStamp, written)
	seedHash := sha256.Sum256([]byte(seed))
	storedName := hex.EncodeToString(seedHash[:16]) + ext
	finalAbs := filepath.Join(c.attachmentsRootAbs, storedName)
	if !isSubPath(c.attachmentsRootAbs, finalAbs) {
		return "", written, detectedMIME, "download_failed", "refusing to write outside attachment root"
	}
	if err := os.Rename(tmpName, finalAbs); err != nil {
		return "", written, detectedMIME, "download_failed", "failed moving attachment into place"
	}

	relPath := filepath.ToSlash(filepath.Join(c.attachmentsRootRel, storedName))
	sidecar := attachmentSidecar{
		IngestedAt:         time.Now().UTC().Format(time.RFC3339),
		FileID:             f.ID,
		Filename:           f.Name,
		MIMEType:           strings.ToLower(strings.TrimSpace(f.Mimetype)),
		StoredName:         storedName,
		StoredRelativePath: relPath,
		Bytes:              written,
		SHA256:             hex.EncodeToString(h.Sum(nil)),
	}
	if err := c.writeSidecar(finalAbs+".meta.json", sidecar); err != nil {
		_ = os.Remove(finalAbs)
		return "", written, detectedMIME, "download_failed", "failed writing sidecar metadata"
	}

	return relPath, written, detectedMIME, "", ""
}

func (c *Connector) writeSidecar(metaPath string, meta attachmentSidecar) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := metaPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, metaPath)
}

func (c *Connector) runAttachmentSweeper(ctx context.Context) {
	c.cleanupExpiredAttachments()
	ticker := time.NewTicker(c.sweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanupExpiredAttachments()
		}
	}
}

func (c *Connector) cleanupExpiredAttachments() {
	entries, err := os.ReadDir(c.attachmentsRootAbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slack attachments sweep error: %v\n", err)
		return
	}

	now := time.Now().UTC()
	scanned := 0
	deleted := 0
	skippedRecent := 0
	skippedLocked := 0
	errors := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		scanned++
		metaPath := filepath.Join(c.attachmentsRootAbs, entry.Name())
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			errors++
			continue
		}
		var meta attachmentSidecar
		if err := json.Unmarshal(metaData, &meta); err != nil {
			errors++
			continue
		}
		ingestedAt, err := time.Parse(time.RFC3339, meta.IngestedAt)
		if err != nil {
			errors++
			continue
		}
		if now.Sub(ingestedAt) <= c.attachmentRetention {
			skippedRecent++
			continue
		}

		storedName := meta.StoredName
		if storedName == "" {
			storedName = strings.TrimSuffix(entry.Name(), ".meta.json")
		}
		attachmentPath := filepath.Join(c.attachmentsRootAbs, storedName)
		if _, err := os.Stat(attachmentPath + ".lock"); err == nil {
			skippedLocked++
			continue
		}

		if err := os.Remove(attachmentPath); err != nil && !os.IsNotExist(err) {
			errors++
			continue
		}
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			errors++
			continue
		}
		deleted++
	}

	fmt.Fprintf(os.Stderr,
		"slack attachments sweep: scanned=%d deleted=%d skipped_recent=%d skipped_locked=%d errors=%d\n",
		scanned, deleted, skippedRecent, skippedLocked, errors,
	)
}

func isAllowedByMetadata(filename, mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	ext := normalizedAttachmentExt(filename)
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	if _, ok := allowedAttachmentMIMEs[mime]; ok {
		return true
	}
	_, ok := allowedAttachmentExts[ext]
	return ok
}

func isAllowedByContent(filename, mime, detected string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	detected = strings.ToLower(strings.TrimSpace(detected))
	ext := normalizedAttachmentExt(filename)

	if strings.HasPrefix(mime, "image/") {
		return strings.HasPrefix(detected, "image/")
	}
	if ext == ".pdf" || mime == "application/pdf" {
		return detected == "application/pdf"
	}
	if ext == ".docx" || ext == ".xlsx" ||
		mime == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		mime == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		return detected == "application/zip" || detected == "application/octet-stream"
	}
	if ext == ".csv" || ext == ".tsv" || mime == "text/csv" || mime == "text/tab-separated-values" || mime == "application/csv" {
		return strings.HasPrefix(detected, "text/plain") || detected == "application/octet-stream"
	}
	if _, ok := allowedAttachmentMIMEs[mime]; ok {
		return detected == mime || detected == "application/octet-stream"
	}
	return false
}

func normalizedAttachmentExt(name string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(name)))
	if ext == ".tiff" {
		return ".tif"
	}
	return ext
}

func extFromMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/csv", "application/csv":
		return ".csv"
	case "text/tab-separated-values":
		return ".tsv"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	default:
		return ""
	}
}

func isSubPath(root, target string) bool {
	rootClean := filepath.Clean(root)
	targetClean := filepath.Clean(target)
	if rootClean == targetClean {
		return true
	}
	prefix := rootClean + string(os.PathSeparator)
	return strings.HasPrefix(targetClean, prefix)
}

func sumAttachmentBytes(items []gateway.AttachmentResult) int64 {
	var total int64
	for _, i := range items {
		total += i.Bytes
	}
	return total
}
