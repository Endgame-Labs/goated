package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"goated/internal/agent"
	"goated/internal/db"
	"goated/internal/msglog"
)

const contextCheckInterval = 5     // check context every N messages
const contextCompactThreshold = 80 // compact if context usage exceeds this %

type queuedMessage struct {
	msg       IncomingMessage
	responder Responder
}

type Service struct {
	Session         agent.SessionRuntime
	Store           *db.Store
	DefaultTimezone string
	AdminChatID     string // chat ID for escalation alerts
	MsgLogger       *msglog.Logger
	SessionIDPath   string // path to claude session ID file for lifecycle tracking

	// DrainCtx is a context that stays alive during graceful shutdown so
	// in-flight handlers can finish. Set this to a context that only cancels
	// after the flush timeout. If nil, the caller-provided ctx is used.
	DrainCtx context.Context

	inflight sync.WaitGroup
	draining atomic.Bool // set when shutdown begins; prevents new inflight work

	msgCount     uint64 // atomic; counts non-command messages
	mu           sync.Mutex
	compacting   bool
	compactQueue []queuedMessage
}

// WaitInflight blocks until all in-flight message handlers have completed.
func (s *Service) WaitInflight() {
	s.draining.Store(true)
	s.inflight.Wait()
}

// trackInflight increments the in-flight counter if the service is not
// draining. Returns false if the service is shutting down.
func (s *Service) trackInflight() bool {
	if s.draining.Load() {
		return false
	}
	s.inflight.Add(1)
	return true
}

func (s *Service) handleCtx(callerCtx context.Context) context.Context {
	if s.DrainCtx != nil {
		return s.DrainCtx
	}
	return callerCtx
}

func (s *Service) HandleMessage(ctx context.Context, msg IncomingMessage, responder Responder) error {
	if !s.trackInflight() {
		return nil // shutting down, drop message
	}
	defer s.inflight.Done()

	// Use drain context so in-flight work survives gateway shutdown
	ctx = s.handleCtx(ctx)

	text := strings.TrimSpace(msg.Text)
	msg.Text = text
	if text == "" && len(msg.Attachments) == 0 {
		if len(msg.AttachmentsFailed) > 0 {
			return responder.SendMessage(ctx, msg.ChatID, failedAttachmentReply(msg.AttachmentsFailed))
		}
		return nil
	}

	// Generate request ID for message correlation
	requestID := msglog.NewRequestID()
	ctx = msglog.WithRequestID(ctx, requestID)

	switch {
	case strings.EqualFold(text, "/clear"):
		s.logCommand(requestID, "clear", msg.ChatID)
		result, err := s.Session.ResetConversation(ctx, msg.ChatID)
		if err != nil {
			return responder.SendMessage(ctx, msg.ChatID, "Failed to clear session: "+err.Error())
		}
		return responder.SendMessage(ctx, msg.ChatID, result.Summary)
	case strings.EqualFold(text, "/chatid"):
		s.logCommand(requestID, "chatid", msg.ChatID)
		return responder.SendMessage(ctx, msg.ChatID, fmt.Sprintf("Your chat ID is: %s", msg.ChatID))
	case strings.EqualFold(text, "/context"):
		s.logCommand(requestID, "context", msg.ChatID)
		estimate, err := s.Session.GetContextEstimate(ctx, msg.ChatID)
		if err != nil || estimate.State != agent.ContextEstimateKnown {
			return responder.SendMessage(ctx, msg.ChatID, "Could not read context usage right now.")
		}
		return responder.SendMessage(ctx, msg.ChatID, fmt.Sprintf("Context usage: %d%%", estimate.PercentUsed))
	case strings.HasPrefix(text, "/schedule "):
		s.logCommand(requestID, "schedule", msg.ChatID)
		return s.handleScheduleCommand(ctx, msg, responder)
	}

	// Log the user message with status=pending
	s.logUserMessage(requestID, msg, msglog.StatusPending)

	// If we're currently compacting, queue this message
	s.mu.Lock()
	if s.compacting {
		s.compactQueue = append(s.compactQueue, queuedMessage{msg: msg, responder: responder})
		s.mu.Unlock()
		return responder.SendMessage(ctx, msg.ChatID,
			"Received your additional steering message. Will add it to the queue for once I'm done compacting...")
	}
	s.mu.Unlock()

	// Check session health before sending; retry with restart up to 5 times
	if err := s.ensureHealthySession(ctx, responder); err != nil {
		return responder.SendMessage(ctx, msg.ChatID, s.friendlyError(err))
	}

	// Periodically check context usage and compact if needed
	count := atomic.AddUint64(&s.msgCount, 1)
	if count%contextCheckInterval == 0 && s.Session.Descriptor().Capabilities.SupportsContextEstimate {
		estimate, err := s.Session.GetContextEstimate(ctx, msg.ChatID)
		if err == nil && estimate.State == agent.ContextEstimateKnown {
			fmt.Fprintf(os.Stderr, "[%s] context check: ~%d%%\n", time.Now().Format(time.RFC3339), estimate.PercentUsed)
			if estimate.PercentUsed > contextCompactThreshold && s.Session.Descriptor().Capabilities.SupportsCompaction {
				return s.compactAndFlush(ctx, msg, responder)
			}
		}
	}

	return s.sendWithRetry(ctx, msg, responder)
}

// HandleBatchMessage processes multiple messages that accumulated while the
// runtime was busy. Each message is logged individually, then they are sent
// to the runtime as a single batch prompt.
func (s *Service) HandleBatchMessage(ctx context.Context, msgs []IncomingMessage, responder Responder) error {
	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) == 1 {
		return s.HandleMessage(ctx, msgs[0], responder)
	}

	if !s.trackInflight() {
		return nil // shutting down, drop message
	}
	defer s.inflight.Done()

	ctx = s.handleCtx(ctx)

	// Generate a single request ID for the batch
	requestID := msglog.NewRequestID()
	ctx = msglog.WithRequestID(ctx, requestID)

	channel := msgs[0].Channel
	chatID := msgs[0].ChatID
	promptMsgs := make([]agent.PromptMessage, 0, len(msgs))

	// Log each message individually
	for i := range msgs {
		msgs[i].Text = strings.TrimSpace(msgs[i].Text)
		if msgs[i].Text == "" && len(msgs[i].Attachments) == 0 {
			if len(msgs[i].AttachmentsFailed) > 0 {
				if err := responder.SendMessage(ctx, msgs[i].ChatID, failedAttachmentReply(msgs[i].AttachmentsFailed)); err != nil {
					return err
				}
			}
			continue
		}
		s.logUserMessage(requestID, msgs[i], msglog.StatusPending)

		promptMsgs = append(promptMsgs, agent.PromptMessage{
			Text:        msgs[i].Text,
			Attachments: msgAttachments(msgs[i]),
			MessageID:   msgs[i].MessageID,
			ThreadID:    msgs[i].ThreadID,
			Context:     msgContext(msgs[i]),
		})
	}

	if len(promptMsgs) == 0 {
		return nil
	}

	// Check session health before sending
	if err := s.ensureHealthySession(ctx, responder); err != nil {
		return responder.SendMessage(ctx, chatID, s.friendlyError(err))
	}

	fmt.Fprintf(os.Stderr, "[%s] sending batch of %d messages\n",
		time.Now().Format(time.RFC3339), len(promptMsgs))

	return s.sendBatchWithRetry(ctx, channel, chatID, promptMsgs, responder)
}

// sendBatchWithRetry sends a batch prompt and retries on transient API errors.
func (s *Service) sendBatchWithRetry(ctx context.Context, channel, chatID string, msgs []agent.PromptMessage, responder Responder) error {
	requestID := msglog.RequestIDFromContext(ctx)
	prevSessionID := s.readSessionID()

	for attempt := 0; attempt <= maxSendRetries; attempt++ {
		s.logStatus(requestID, msglog.EntryUserMessage, msglog.StatusSentToAgent)

		if err := s.Session.SendBatchPrompt(ctx, channel, chatID, msgs); err != nil {
			s.logEvent(requestID, msglog.EventData{Name: "send_failed", Detail: err.Error()})
			return responder.SendMessage(ctx, chatID, s.friendlyError(err))
		}

		state, idleErr := s.Session.WaitForAwaitingInput(ctx, postSendTimeout)
		if idleErr != nil {
			fmt.Fprintf(os.Stderr, "[%s] WaitForAwaitingInput: %v (suppressed)\n",
				time.Now().Format(time.RFC3339), idleErr)
			recovered, recoverErr := s.tryRecoverAfterIdleTimeout(ctx, requestID, chatID, idleErr)
			if recoverErr != nil {
				return responder.SendMessage(ctx, chatID, s.friendlyError(recoverErr))
			}
			if recovered {
				continue
			}
			return nil
		}
		if !state.SafeIdle() {
			if state.Kind == agent.SessionStateBlockedAuth || state.Kind == agent.SessionStateBlockedIntervene {
				return responder.SendMessage(ctx, chatID, s.runtimeDisplayName()+" needs manual intervention: "+state.Summary)
			}
			return nil
		}

		apiErr := s.Session.DetectRetryableError(ctx)
		if apiErr == "" {
			s.logStatus(requestID, msglog.EntryUserMessage, msglog.StatusAgentReceived)
			s.detectSessionChange(requestID, prevSessionID)
			return nil
		}

		s.logEvent(requestID, msglog.EventData{Name: "api_error", Detail: apiErr})
		fmt.Fprintf(os.Stderr, "[%s] API error after batch send (attempt %d/%d): %s\n",
			time.Now().Format(time.RFC3339), attempt+1, maxSendRetries+1, apiErr)

		if attempt < maxSendRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}

	return responder.SendMessage(ctx, chatID,
		s.runtimeDisplayName()+" hit an API error and retries didn't help. Try again in a minute, or use /clear if it persists.")
}

// logUserMessage logs a user message if the logger is configured.
func (s *Service) logUserMessage(requestID string, msg IncomingMessage, status msglog.MessageStatus) {
	if s.MsgLogger == nil {
		return
	}
	s.MsgLogger.LogUserMessage(requestID, msglog.UserMessageData{
		Channel:         msg.Channel,
		ChatID:          msg.ChatID,
		UserID:          msg.UserID,
		Text:            msg.Text,
		MessageID:       msg.MessageID,
		ThreadID:        msg.ThreadID,
		HasAttachments:  len(msg.Attachments) > 0,
		AttachmentCount: len(msg.Attachments),
	}, status)
}

// logCommand logs a command invocation if the logger is configured.
func (s *Service) logCommand(requestID, name, chatID string) {
	if s.MsgLogger == nil {
		return
	}
	s.MsgLogger.LogCommand(requestID, msglog.CommandData{Name: name, ChatID: chatID})
}

// logEvent logs a system event if the logger is configured.
func (s *Service) logEvent(requestID string, event msglog.EventData) {
	if s.MsgLogger == nil {
		return
	}
	s.MsgLogger.LogEvent(requestID, event)
}

const maxSendRetries = 2
const postSendTimeout = 5 * time.Minute

// sendWithRetry sends a message to the active runtime and monitors for API errors.
// If a retryable error is detected, it re-sends up to maxSendRetries times.
func (s *Service) sendWithRetry(ctx context.Context, msg IncomingMessage, responder Responder) error {
	requestID := msglog.RequestIDFromContext(ctx)

	// Track session changes for session file management
	prevSessionID := s.readSessionID()

	for attempt := 0; attempt <= maxSendRetries; attempt++ {
		s.logStatus(requestID, msglog.EntryUserMessage, msglog.StatusSentToAgent)

		if err := s.Session.SendUserPrompt(ctx, msg.Channel, msg.ChatID, msg.Text, msgAttachments(msg), msg.MessageID, msg.ThreadID, msgContext(msg)); err != nil {
			s.logEvent(requestID, msglog.EventData{Name: "send_failed", Detail: err.Error()})
			return responder.SendMessage(ctx, msg.ChatID, s.friendlyError(err))
		}

		// Wait for the runtime to return to an input-ready state, then check for errors.
		// Timeouts are not user-facing errors — the runtime sends its own responses
		// via send_user_message, so a slow return to idle is expected for long tasks.
		state, idleErr := s.Session.WaitForAwaitingInput(ctx, postSendTimeout)
		if idleErr != nil {
			fmt.Fprintf(os.Stderr, "[%s] WaitForAwaitingInput: %v (suppressed)\n",
				time.Now().Format(time.RFC3339), idleErr)
			recovered, recoverErr := s.tryRecoverAfterIdleTimeout(ctx, requestID, msg.ChatID, idleErr)
			if recoverErr != nil {
				return responder.SendMessage(ctx, msg.ChatID, s.friendlyError(recoverErr))
			}
			if recovered {
				continue
			}
			// Don't update status — leave as sent_to_agent; if the runtime is
			// still legitimately working, goat send_user_message will log the
			// response later.
			return nil
		}
		if !state.SafeIdle() {
			if state.Kind == agent.SessionStateBlockedAuth || state.Kind == agent.SessionStateBlockedIntervene {
				return responder.SendMessage(ctx, msg.ChatID, s.runtimeDisplayName()+" needs manual intervention: "+state.Summary)
			}
			// Unknown/stable states are not errors — runtime may still be working.
			return nil
		}

		apiErr := s.Session.DetectRetryableError(ctx)
		if apiErr == "" {
			s.logStatus(requestID, msglog.EntryUserMessage, msglog.StatusAgentReceived)
			s.detectSessionChange(requestID, prevSessionID)
			return nil
		}

		s.logEvent(requestID, msglog.EventData{Name: "api_error", Detail: apiErr})
		fmt.Fprintf(os.Stderr, "[%s] API error after send (attempt %d/%d): %s\n",
			time.Now().Format(time.RFC3339), attempt+1, maxSendRetries+1, apiErr)

		if attempt < maxSendRetries {
			// Wait a few seconds before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}

	return responder.SendMessage(ctx, msg.ChatID,
		s.runtimeDisplayName()+" hit an API error and retries didn't help. Try again in a minute, or use /clear if it persists.")
}

func (s *Service) tryRecoverAfterIdleTimeout(ctx context.Context, requestID, chatID string, idleErr error) (bool, error) {
	state, stateErr := s.Session.GetSessionState(ctx)
	if stateErr == nil {
		switch state.Kind {
		case agent.SessionStateBlockedAuth, agent.SessionStateBlockedIntervene:
			return false, fmt.Errorf("%s requires manual intervention: %s", s.runtimeDisplayName(), state.Summary)
		case agent.SessionStateAwaitingInput:
			return false, nil
		case agent.SessionStateGenerating, agent.SessionStateUnknownStable:
			s.logEvent(requestID, msglog.EventData{
				Name:   "idle_timeout_ignored",
				Detail: fmt.Sprintf("state=%s (%s)", state.Kind, state.Summary),
			})
			return false, nil
		}
	}

	health, healthErr := s.Session.GetHealth(ctx)
	if healthErr == nil && !health.OK && !health.Recoverable {
		return false, fmt.Errorf("%s session requires manual intervention: %s", s.runtimeDisplayName(), health.Summary)
	}
	if healthErr == nil && health.OK {
		detail := idleErr.Error()
		if stateErr == nil && state.Summary != "" {
			detail = detail + "; state=" + string(state.Kind) + " (" + state.Summary + ")"
		}
		if health.Summary != "" {
			detail = detail + "; health=" + health.Summary
		}
		s.logEvent(requestID, msglog.EventData{Name: "idle_timeout_ignored", Detail: detail})
		return false, nil
	}

	detail := idleErr.Error()
	if stateErr == nil && state.Summary != "" {
		detail = detail + "; state=" + string(state.Kind) + " (" + state.Summary + ")"
	}
	if healthErr == nil && health.Summary != "" {
		detail = detail + "; health=" + health.Summary
	}
	s.logEvent(requestID, msglog.EventData{Name: "idle_timeout_recover", Detail: detail})

	fmt.Fprintf(os.Stderr, "[%s] restarting %s after idle timeout\n",
		time.Now().Format(time.RFC3339), s.runtimeDisplayName())
	if err := s.Session.RestartSession(ctx); err != nil {
		s.logEvent(requestID, msglog.EventData{Name: "idle_timeout_restart_failed", Detail: err.Error()})
		return false, err
	}

	s.logEvent(requestID, msglog.EventData{Name: "idle_timeout_restarted"})
	return true, nil
}

// logStatus updates the status of a message if the logger is configured.
func (s *Service) logStatus(requestID string, entryType msglog.EntryType, status msglog.MessageStatus) {
	if s.MsgLogger == nil || requestID == "" {
		return
	}
	s.MsgLogger.UpdateStatus(requestID, entryType, status)
}

// readSessionID reads the current Claude session ID from the session ID file.
func (s *Service) readSessionID() string {
	if s.SessionIDPath == "" {
		return ""
	}
	data, err := os.ReadFile(s.SessionIDPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// detectSessionChange checks if the session ID changed and triggers a new
// session file if the logger is configured.
func (s *Service) detectSessionChange(requestID, prevSessionID string) {
	if s.MsgLogger == nil {
		return
	}
	newSessionID := s.readSessionID()
	if newSessionID == "" || newSessionID == prevSessionID {
		return
	}
	seq := s.MsgLogger.SessionManager().NewSession(newSessionID)
	s.logEvent(requestID, msglog.EventData{
		Name:   "session_start",
		Detail: fmt.Sprintf("session=%s seq=%s", newSessionID, seq),
	})
}

// compactAndFlush triggers /compact on the active runtime session, queues the trigger
// message (and any that arrive while compacting), then flushes them all once
// the runtime returns to an input-ready state.
func (s *Service) compactAndFlush(ctx context.Context, triggerMsg IncomingMessage, responder Responder) error {
	s.mu.Lock()
	s.compacting = true
	s.compactQueue = []queuedMessage{{msg: triggerMsg, responder: responder}}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.compacting = false
		s.compactQueue = nil
		s.mu.Unlock()
	}()

	// Notify user
	_ = responder.SendMessage(ctx, triggerMsg.ChatID,
		"Message received. But let me first compact my context window before I address it...")

	if err := s.waitForSessionIdle(ctx, 5*time.Minute); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[%s] sending /compact to %s session\n", time.Now().Format(time.RFC3339), s.runtimeDisplayName())
	if err := s.Session.SendControlCommand(ctx, "/compact"); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] /compact send failed: %v\n", time.Now().Format(time.RFC3339), err)
	} else {
		if err := s.waitForSessionIdle(ctx, 5*time.Minute); err != nil && !strings.Contains(err.Error(), "timed out waiting") {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "[%s] compaction done, flushing queued messages\n", time.Now().Format(time.RFC3339))

	// Grab the full queue
	s.mu.Lock()
	queue := make([]queuedMessage, len(s.compactQueue))
	copy(queue, s.compactQueue)
	s.mu.Unlock()

	// Build summary of queued message texts
	var msgTexts []string
	for _, qm := range queue {
		msgTexts = append(msgTexts, qm.msg.Text)
	}

	// Notify user
	_ = responder.SendMessage(ctx, triggerMsg.ChatID,
		fmt.Sprintf("Compaction done! Handling your message now:\n\n%s", strings.Join(msgTexts, "\n\n")))

	// Flush all queued messages to tmux
	for _, qm := range queue {
		if err := s.Session.SendUserPrompt(ctx, qm.msg.Channel, qm.msg.ChatID, qm.msg.Text, msgAttachments(qm.msg), qm.msg.MessageID, qm.msg.ThreadID, msgContext(qm.msg)); err != nil {
			_ = qm.responder.SendMessage(ctx, qm.msg.ChatID, s.friendlyError(err))
		}
	}

	return nil
}

func (s *Service) handleScheduleCommand(ctx context.Context, msg IncomingMessage, responder Responder) error {
	payload := strings.TrimSpace(strings.TrimPrefix(msg.Text, "/schedule"))
	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 {
		return responder.SendMessage(ctx, msg.ChatID, "Usage: /schedule <cron_expr> | <prompt>")
	}
	schedule := strings.TrimSpace(parts[0])
	prompt := strings.TrimSpace(parts[1])
	if schedule == "" || prompt == "" {
		return responder.SendMessage(ctx, msg.ChatID, "Both cron expression and prompt are required.")
	}
	_, err := s.Store.AddCron("subagent", msg.ChatID, schedule, prompt, "", "", s.DefaultTimezone, false)
	if err != nil {
		return responder.SendMessage(ctx, msg.ChatID, "Failed to save schedule: "+err.Error())
	}
	return responder.SendMessage(ctx, msg.ChatID, "Saved scheduled job.")
}

const maxSessionRetries = 5

// ensureHealthySession checks if the active runtime session is healthy. If not, it
// restarts it up to maxSessionRetries times (once per minute). After exhausting
// retries, it DMs the admin to request manual intervention.
func (s *Service) ensureHealthySession(ctx context.Context, responder Responder) error {
	for attempt := 1; attempt <= maxSessionRetries; attempt++ {
		health, err := s.Session.GetHealth(ctx)
		if err == nil && health.OK {
			return nil
		}
		summary := "unknown error"
		recoverable := true
		if err != nil {
			summary = err.Error()
		} else {
			summary = health.Summary
			recoverable = health.Recoverable
		}
		fmt.Fprintf(os.Stderr, "[%s] session unhealthy (attempt %d/%d): %s\n",
			time.Now().Format(time.RFC3339), attempt, maxSessionRetries, summary)
		if !recoverable {
			return fmt.Errorf("%s session requires manual intervention: %s", s.runtimeDisplayName(), summary)
		}

		fmt.Fprintf(os.Stderr, "[%s] restarting %s session...\n",
			time.Now().Format(time.RFC3339), s.runtimeDisplayName())
		if err := s.Session.RestartSession(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] restart failed: %v\n",
				time.Now().Format(time.RFC3339), err)
		}

		health, err = s.Session.GetHealth(ctx)
		if err == nil && health.OK {
			fmt.Fprintf(os.Stderr, "[%s] session recovered after restart\n",
				time.Now().Format(time.RFC3339))
			return nil
		}

		if attempt < maxSessionRetries {
			// Wait 1 minute before next attempt
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Minute):
			}
		}
	}

	// Exhausted retries — alert admin
	if s.AdminChatID != "" && responder != nil {
		_ = responder.SendMessage(ctx, s.AdminChatID,
			fmt.Sprintf("%s session needs manual intervention. "+
				"Tried restarting %d times over %d minutes but it won't recover. "+
				"Check the server and run /clear or restart the daemon.",
				s.runtimeDisplayName(), maxSessionRetries, maxSessionRetries))
	}

	return fmt.Errorf("%s session unhealthy after %d restart attempts", strings.ToLower(s.runtimeDisplayName()), maxSessionRetries)
}

func (s *Service) waitForSessionIdle(ctx context.Context, timeout time.Duration) error {
	state, err := s.Session.WaitForAwaitingInput(ctx, timeout)
	if err != nil {
		return err
	}
	if !state.SafeIdle() {
		return fmt.Errorf("%s", state.Summary)
	}
	return nil
}

func (s *Service) runtimeDisplayName() string {
	return s.Session.Descriptor().DisplayName
}

// msgAttachments converts gateway attachment data into the agent-layer struct.
// Returns nil if the message has no attachments.
func msgContext(msg IncomingMessage) *agent.MessageContext {
	if msg.UserID == "" && msg.UserName == "" && msg.UserUsername == "" && msg.ChatType == "" && msg.ReplyToText == "" {
		return nil
	}
	return &agent.MessageContext{
		UserID:          msg.UserID,
		UserName:        msg.UserName,
		UserUsername:    msg.UserUsername,
		ChatType:        msg.ChatType,
		ReplyToText:     msg.ReplyToText,
		ReplyToUserName: msg.ReplyToUserName,
	}
}

func msgAttachments(msg IncomingMessage) *agent.MessageAttachments {
	if len(msg.Attachments) == 0 && len(msg.AttachmentsFailed) == 0 && len(msg.AttachmentsSucceeded) == 0 {
		return nil
	}
	convert := func(results []AttachmentResult) []agent.AttachmentInfo {
		out := make([]agent.AttachmentInfo, 0, len(results))
		for _, r := range results {
			out = append(out, agent.AttachmentInfo{
				Index:      r.Index,
				FileID:     r.FileID,
				Filename:   r.Filename,
				Path:       r.Path,
				Outcome:    r.Outcome,
				ReasonCode: r.ReasonCode,
				Reason:     r.Reason,
				Bytes:      r.Bytes,
				MIMEType:   r.MIMEType,
			})
		}
		return out
	}
	return &agent.MessageAttachments{
		Paths:     msg.Attachments,
		Failed:    convert(msg.AttachmentsFailed),
		Succeeded: convert(msg.AttachmentsSucceeded),
	}
}

func (s *Service) friendlyError(err error) string {
	name := s.runtimeDisplayName()
	switch {
	case errors.Is(err, context.Canceled):
		return "The bot was restarted while processing your message. Please send it again."
	case errors.Is(err, context.DeadlineExceeded):
		return name + " took too long to respond (timed out). Try again or simplify your request."
	case strings.Contains(err.Error(), "session readiness"):
		return name + " session failed to start. Try /clear to reset, or check that the daemon is healthy."
	case strings.Contains(strings.ToLower(err.Error()), "timed out waiting"):
		return name + " didn't finish in time. Try again or use /clear to start a fresh session."
	case strings.Contains(err.Error(), "pane to change"):
		return "Failed to send your message to " + name + ". The session may be stuck — try /clear."
	case strings.Contains(err.Error(), "requires manual intervention"):
		return name + " requires manual intervention before it can continue. Check the server session."
	case strings.Contains(err.Error(), "unhealthy after"):
		return name + " session is down and couldn't be auto-restarted. The admin has been notified."
	default:
		return "Something went wrong talking to " + name + ": " + err.Error()
	}
}

func failedAttachmentReply(failed []AttachmentResult) string {
	if len(failed) == 0 {
		return "I couldn't process that attachment."
	}

	var lines []string
	if len(failed) == 1 {
		lines = append(lines, "I couldn't process that attachment:")
	} else {
		lines = append(lines, "I couldn't process those attachments:")
	}

	limit := len(failed)
	if limit > 3 {
		limit = 3
	}
	for _, item := range failed[:limit] {
		name := strings.TrimSpace(item.Filename)
		if name == "" {
			name = fmt.Sprintf("attachment %d", item.Index+1)
		}
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = "upload failed validation"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", name, reason))
	}
	if len(failed) > limit {
		lines = append(lines, fmt.Sprintf("- and %d more", len(failed)-limit))
	}

	lines = append(lines, "Supported uploads include images, PDF, CSV/TSV, DOCX, and XLSX within the configured size limits.")
	return strings.Join(lines, "\n")
}
