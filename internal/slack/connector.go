package slack

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"goated/internal/gateway"
	"goated/internal/util"
)

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
}

// NewConnector creates a Slack connector.
// botToken is the Bot User OAuth Token (xoxb-...).
// appToken is the App-Level Token (xapp-...) required for Socket Mode.
// channelID restricts the bot to a single DM channel.
func NewConnector(botToken, appToken, channelID string, store OffsetStore) (*Connector, error) {
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

	return &Connector{
		api:       api,
		socket:    socket,
		store:     store,
		channelID: channelID,
	}, nil
}

// Run connects via Socket Mode and processes incoming messages.
func (c *Connector) Run(ctx context.Context, handler gateway.Handler) error {
	go func() {
		for evt := range c.socket.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				c.socket.Ack(*evt.Request)
				c.handleEventsAPI(ctx, handler, eventsAPIEvent)

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

func (c *Connector) handleEventsAPI(ctx context.Context, handler gateway.Handler, event slackevents.EventsAPIEvent) {
	if event.Type != slackevents.CallbackEvent {
		return
	}

	innerEvent := event.InnerEvent
	switch ev := innerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		// Ignore bot messages (including our own)
		if ev.BotID != "" || ev.SubType != "" {
			return
		}

		// Redirect messages from non-monitored channels
		if ev.Channel != c.channelID {
			_ = c.SendMessage(ctx, ev.Channel,
				"This isn't the channel I'm monitoring. Go to the configured DM channel to chat with me.")
			return
		}

		text := strings.TrimSpace(ev.Text)
		if text == "" {
			return
		}

		msg := gateway.IncomingMessage{
			Channel: "slack",
			ChatID:  ev.Channel,
			UserID:  ev.User,
			Text:    text,
		}

		// Add a "thinking" reaction while processing
		stopTyping := c.startTypingIndicator(ev.Channel, ev.TimeStamp)
		defer stopTyping()

		if err := handler.HandleMessage(ctx, msg, c); err != nil {
			_ = c.SendMessage(ctx, ev.Channel, "Error: "+err.Error())
		}
	}
}

// SendMessage sends a message to the specified Slack channel, converting
// markdown to Slack's mrkdwn format.
func (c *Connector) SendMessage(_ context.Context, channelID, text string) error {
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

// startTypingIndicator adds an eyes emoji reaction to signal processing,
// and returns a function to remove it when done.
func (c *Connector) startTypingIndicator(channel, timestamp string) func() {
	_ = c.api.AddReaction("eyes", slack.ItemRef{
		Channel:   channel,
		Timestamp: timestamp,
	})
	return func() {
		_ = c.api.RemoveReaction("eyes", slack.ItemRef{
			Channel:   channel,
			Timestamp: timestamp,
		})
	}
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
