package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"goated/internal/claude"
	"goated/internal/db"
)

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

	// Check session health before sending; retry with restart up to 5 times
	if err := s.ensureHealthySession(ctx, responder); err != nil {
		return responder.SendMessage(ctx, msg.ChatID, friendlyError(err))
	}

	if err := s.Bridge.SendAndWait(ctx, msg.Channel, msg.ChatID, text, 30*time.Minute); err != nil {
		return responder.SendMessage(ctx, msg.ChatID, friendlyError(err))
	}
	// Claude sends its response directly via ./goat send_user_message
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
