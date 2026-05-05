package slack

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"goated/internal/gateway"
)

// InteractionRouter maps Slack interactive payloads (button clicks, etc.)
// into synthetic IncomingMessage values and forwards them to a gateway Handler.
// After handling, it updates the original message to replace interactive
// elements with confirmation text.
type InteractionRouter struct {
	connector *Connector
	handler   gateway.Handler
}

// NewInteractionRouter creates a router that translates button actions into
// messages for the given handler.
func NewInteractionRouter(connector *Connector, handler gateway.Handler) *InteractionRouter {
	return &InteractionRouter{
		connector: connector,
		handler:   handler,
	}
}

// Handle processes a Slack InteractionCallback, mapping known action_ids to
// synthetic text messages.
func (r *InteractionRouter) Handle(ctx context.Context, callback slack.InteractionCallback) error {
	for _, action := range callback.ActionCallback.BlockActions {
		text := r.actionToText(action)
		if text == "" {
			fmt.Fprintf(os.Stderr, "[%s] interaction_router: unhandled action_id=%s\n",
				time.Now().Format(time.RFC3339), action.ActionID)
			continue
		}

		// Resolve the thread the response should land in. If the button is
		// inside a threaded reply, the existing parent thread_ts wins; for a
		// top-level message that hosts the button, use the message_ts itself
		// so the agent's reply opens (or continues) the thread under it.
		threadID := callback.Message.ThreadTimestamp
		if threadID == "" {
			threadID = callback.MessageTs
		}

		msg := gateway.IncomingMessage{
			Channel:   "slack",
			ChatID:    callback.Channel.ID,
			UserID:    callback.User.ID,
			Text:      text,
			MessageID: callback.MessageTs,
			ThreadID:  threadID,
		}

		if err := r.handler.HandleMessage(ctx, msg, r.connector); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] interaction_router: handler error for action %s: %v\n",
				time.Now().Format(time.RFC3339), action.ActionID, err)
		}

		// Note: visual button disabling is not implemented. Button clicks
		// route actions correctly; the user tracks state visually.
	}
	return nil
}

// actionToText maps an action_id to a synthetic text message.
func (r *InteractionRouter) actionToText(action *slack.BlockAction) string {
	switch {
	case action.ActionID == "daily_prep_approve_all":
		return "ok"
	case strings.HasPrefix(action.ActionID, "daily_prep_done_"):
		num := strings.TrimPrefix(action.ActionID, "daily_prep_done_")
		return num + "done"
	case strings.HasPrefix(action.ActionID, "daily_prep_skip_"):
		num := strings.TrimPrefix(action.ActionID, "daily_prep_skip_")
		return "skip " + num
	case strings.HasPrefix(action.ActionID, "daily_prep_push_"):
		num := strings.TrimPrefix(action.ActionID, "daily_prep_push_")
		return num + ">tomorrow"
	default:
		return ""
	}
}
