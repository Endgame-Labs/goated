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

	"goated/internal/claude"
	"goated/internal/db"
	"goated/internal/tmux"
)

const contextCheckInterval = 5  // check context every N messages
const contextCompactThreshold = 80 // compact if context usage exceeds this %

type queuedMessage struct {
	msg       IncomingMessage
	responder Responder
}

type Service struct {
	Bridge          *claude.TmuxBridge
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
		if err := s.Bridge.ClearSession(ctx, msg.ChatID); err != nil {
			return responder.SendMessage(ctx, msg.ChatID, "Failed to clear session: "+err.Error())
		}
		return responder.SendMessage(ctx, msg.ChatID, "Started a new Claude session and rotated the chat log.")
	case strings.EqualFold(text, "/chatid"):
		return responder.SendMessage(ctx, msg.ChatID, fmt.Sprintf("Your chat ID is: %s", msg.ChatID))
	case strings.EqualFold(text, "/context"):
		pct := s.Bridge.ContextUsagePercent(msg.ChatID)
		return responder.SendMessage(ctx, msg.ChatID, fmt.Sprintf("Approx context usage: %d%%", pct))
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
		return responder.SendMessage(ctx, msg.ChatID, friendlyError(err))
	}

	// Periodically check context usage and compact if needed
	count := atomic.AddUint64(&s.msgCount, 1)
	if count%contextCheckInterval == 0 {
		pct := s.Bridge.ContextUsagePercent(msg.ChatID)
		fmt.Fprintf(os.Stderr, "[%s] context check: ~%d%%\n", time.Now().Format(time.RFC3339), pct)
		if pct > contextCompactThreshold {
			return s.compactAndFlush(ctx, msg, responder)
		}
	}

	return s.sendWithRetry(ctx, msg, responder, text)
}

const maxSendRetries = 2
const postSendTimeout = 5 * time.Minute

// sendWithRetry sends a message to Claude and monitors for API errors.
// If a retryable error is detected, it re-sends up to maxSendRetries times.
func (s *Service) sendWithRetry(ctx context.Context, msg IncomingMessage, responder Responder, text string) error {
	for attempt := 0; attempt <= maxSendRetries; attempt++ {
		if err := s.Bridge.SendAndWait(ctx, msg.Channel, msg.ChatID, text, 30*time.Minute); err != nil {
			return responder.SendMessage(ctx, msg.ChatID, friendlyError(err))
		}

		// Wait for Claude to return to prompt, then check for errors
		idleErr := tmux.WaitForIdle(ctx, postSendTimeout)
		if idleErr != nil {
			// Timed out — Claude is still working, which is fine (long task)
			return nil
		}

		// Claude returned to prompt — check if it was an error
		apiErr := tmux.CheckPaneForError(ctx)
		if apiErr == "" {
			// No error — Claude processed successfully
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

	// Exhausted retries — notify user
	return responder.SendMessage(ctx, msg.ChatID,
		"Claude hit an API error and retries didn't help. Try again in a minute, or use /clear if it persists.")
}

// compactAndFlush triggers /compact on the Claude session, queues the trigger
// message (and any that arrive while compacting), then flushes them all once
// Claude returns to the prompt.
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

	// Wait for Claude to be idle before sending /compact
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		busy, err := s.Bridge.IsSessionBusy(ctx)
		if err != nil || !busy {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Send /compact
	fmt.Fprintf(os.Stderr, "[%s] sending /compact to Claude session\n", time.Now().Format(time.RFC3339))
	if err := s.Bridge.SendRaw(ctx, "/compact"); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] /compact send failed: %v\n", time.Now().Format(time.RFC3339), err)
		// Fall through and try to send messages anyway
	} else {
		// Give Claude a moment to start processing before polling
		time.Sleep(3 * time.Second)

		// Poll until Claude returns to the prompt
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			busy, err := s.Bridge.IsSessionBusy(ctx)
			if err == nil && !busy {
				break
			}
			time.Sleep(2 * time.Second)
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
		if err := s.Bridge.SendAndWait(ctx, qm.msg.Channel, qm.msg.ChatID, qm.msg.Text, 30*time.Minute); err != nil {
			_ = qm.responder.SendMessage(ctx, qm.msg.ChatID, friendlyError(err))
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
	_, err := s.Store.AddCron(msg.ChatID, schedule, prompt, "", s.DefaultTimezone)
	if err != nil {
		return responder.SendMessage(ctx, msg.ChatID, "Failed to save schedule: "+err.Error())
	}
	return responder.SendMessage(ctx, msg.ChatID, "Saved scheduled job.")
}

const maxSessionRetries = 5

// ensureHealthySession checks if the Claude session is healthy. If not, it
// restarts it up to maxSessionRetries times (once per minute). After exhausting
// retries, it DMs the admin to request manual intervention.
func (s *Service) ensureHealthySession(ctx context.Context, responder Responder) error {
	for attempt := 1; attempt <= maxSessionRetries; attempt++ {
		if err := s.Bridge.SessionHealthy(ctx); err == nil {
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "[%s] session unhealthy (attempt %d/%d): %v\n",
				time.Now().Format(time.RFC3339), attempt, maxSessionRetries, err)
		}

		// Try restarting
		fmt.Fprintf(os.Stderr, "[%s] restarting Claude session...\n",
			time.Now().Format(time.RFC3339))
		if err := s.Bridge.RestartSession(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] restart failed: %v\n",
				time.Now().Format(time.RFC3339), err)
		}

		// Check again immediately after restart
		if err := s.Bridge.SessionHealthy(ctx); err == nil {
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
			fmt.Sprintf("Claude Code session needs manual intervention. "+
				"Tried restarting %d times over %d minutes but it won't recover. "+
				"Check the server and run /clear or restart the daemon.",
				maxSessionRetries, maxSessionRetries))
	}

	return fmt.Errorf("claude session unhealthy after %d restart attempts", maxSessionRetries)
}

func friendlyError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "The bot was restarted while processing your message. Please send it again."
	case errors.Is(err, context.DeadlineExceeded):
		return "Claude took too long to respond (timed out). Try again or simplify your request."
	case strings.Contains(err.Error(), "timed out waiting for claude response"):
		return "Claude didn't finish in time. Try again or use /clear to start a fresh session."
	case strings.Contains(err.Error(), "timed out waiting for Claude session readiness"):
		return "Claude session failed to start. Try /clear to reset, or check that the daemon is healthy."
	case strings.Contains(err.Error(), "paste not received"):
		return "Failed to send your message to Claude. The session may be stuck — try /clear."
	case strings.Contains(err.Error(), "unhealthy after"):
		return "Claude Code session is down and couldn't be auto-restarted. The admin has been notified."
	default:
		return "Something went wrong talking to Claude: " + err.Error()
	}
}
