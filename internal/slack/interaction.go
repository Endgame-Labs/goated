package slack

import (
	"context"

	"github.com/slack-go/slack"
)

// InteractionHandler is a callback invoked when a Slack interactive event
// (button click, menu selection, etc.) is received.
type InteractionHandler func(ctx context.Context, callback slack.InteractionCallback) error

// SetInteractionHandler registers a handler that will be called for every
// interactive payload received over Socket Mode.
func (c *Connector) SetInteractionHandler(h InteractionHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interactionHandler = h
}
