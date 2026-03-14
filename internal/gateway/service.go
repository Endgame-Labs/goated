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

	// DrainCtx is a context that stays alive during graceful shutdown so
	// in-flight handlers can finish. Set this to a context that only cancels
	// after the flush timeout. If nil, the caller-provided ctx is used.
	DrainCtx context.Context

	inflight sync.WaitGroup

	msgCount     uint64 // atomic; counts non-command messages
	mu           sync.Mutex
	compacting   bool
	compactQueue []queuedMessage
}

// WaitInflight blocks until all in-flight message handlers have completed.
func (s *Service) WaitInflight() {
	s.inflight.Wait()
}

func (s *Service) handleCtx(callerCtx context.Context) context.Context {
	if s.DrainCtx != nil {
		return s.DrainCtx
	}
	return callerCtx
}

func (s *Service) HandleMessage(ctx context.Context, msg IncomingMessage, responder Responder) error {
	s.inflight.Add(1)
	defer s.inflight.Done()

	// Use drain context so in-flight work survives gateway shutdown
	ctx = s.handleCtx(ctx)

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}

	switch {
	case strings.EqualFold(text, "/clear"):
		result, err := s.Session.ResetConversation(ctx, msg.ChatID)
		if err != nil {
			return responder.SendMessage(ctx, msg.ChatID, "Failed to clear session: "+err.Error())
		}
		return responder.SendMessage(ctx, msg.ChatID, result.Summary)
	case strings.EqualFold(text, "/chatid"):
		return responder.SendMessage(ctx, msg.ChatID, fmt.Sprintf("Your chat ID is: %s", msg.ChatID))
	case strings.EqualFold(text, "/context"):
		estimate, err := s.Session.GetContextEstimate(ctx, msg.ChatID)
		if err != nil || estimate.State != agent.ContextEstimateKnown {
			return responder.SendMessage(ctx, msg.ChatID, "Could not read context usage right now.")
		}
		return responder.SendMessage(ctx, msg.ChatID, fmt.Sprintf("Context usage: %d%%", estimate.PercentUsed))
	case strings.HasPrefix(text, "/schedule "):
		return s.handleScheduleCommand(ctx, msg, responder)
	}

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

	return s.sendWithRetry(ctx, msg, responder, text)
}

const maxSendRetries = 2
const postSendTimeout = 5 * time.Minute

// sendWithRetry sends a message to the active runtime and monitors for API errors.
// If a retryable error is detected, it re-sends up to maxSendRetries times.
func (s *Service) sendWithRetry(ctx context.Context, msg IncomingMessage, responder Responder, text string) error {
	for attempt := 0; attempt <= maxSendRetries; attempt++ {
		if err := s.Session.SendUserPrompt(ctx, msg.Channel, msg.ChatID, text); err != nil {
			return responder.SendMessage(ctx, msg.ChatID, s.friendlyError(err))
		}

		// Wait for the runtime to return to an input-ready state, then check for errors.
		// Timeouts are not user-facing errors — the runtime sends its own responses
		// via send_user_message, so a slow return to idle is expected for long tasks.
		state, idleErr := s.Session.WaitForAwaitingInput(ctx, postSendTimeout)
		if idleErr != nil {
			fmt.Fprintf(os.Stderr, "[%s] WaitForAwaitingInput: %v (suppressed)\n",
				time.Now().Format(time.RFC3339), idleErr)
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
			return nil
		}

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
		if err := s.Session.SendUserPrompt(ctx, qm.msg.Channel, qm.msg.ChatID, qm.msg.Text); err != nil {
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

func (s *Service) friendlyError(err error) string {
	name := s.runtimeDisplayName()
	switch {
	case errors.Is(err, context.Canceled):
		return "The bot was restarted while processing your message. Please send it again."
	case errors.Is(err, context.DeadlineExceeded):
		return name + " took too long to respond (timed out). Try again or simplify your request."
	case strings.Contains(strings.ToLower(err.Error()), "timed out waiting"):
		return name + " didn't finish in time. Try again or use /clear to start a fresh session."
	case strings.Contains(err.Error(), "session readiness"):
		return name + " session failed to start. Try /clear to reset, or check that the daemon is healthy."
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
