package slack

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"goated/internal/gateway"
	"goated/internal/tmux"
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

	mu         sync.Mutex
	thinkingTS string // timestamp of the current "_thinking..._" message
	seenEvents map[string]bool // dedup retried Slack events
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
		api:        api,
		socket:     socket,
		store:      store,
		channelID:  channelID,
		seenEvents: make(map[string]bool),
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
				go c.handleEventsAPI(ctx, handler, eventsAPIEvent)

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

		// Deduplicate retried events from Slack using message timestamp
		c.mu.Lock()
		if c.seenEvents[ev.TimeStamp] {
			c.mu.Unlock()
			return
		}
		c.seenEvents[ev.TimeStamp] = true
		c.mu.Unlock()

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

		// Post a thinking indicator while processing
		c.postThinking(ev.Channel)

		if err := handler.HandleMessage(ctx, msg, c); err != nil {
			_ = c.SendMessage(ctx, ev.Channel, "Error: "+err.Error())
		}
	}
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

// postThinking posts a "_thinking..._" message and records its timestamp
// so it can be updated with the real response or deleted later.
// Also spawns a TTL reaper to guarantee cleanup even if normal paths fail.
func (c *Connector) postThinking(channel string) {
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
// Soft deadline: 4 minutes — deletes if Claude is idle.
// If Claude is still busy, rechecks every minute.
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

	ctx := context.Background()
	hardCutoff := time.Now().Add(hardDeadline - softDeadline)

	for {
		if time.Now().After(hardCutoff) {
			break // hard deadline reached, delete unconditionally
		}
		if tmux.IsIdle(ctx) {
			break // Claude is idle, safe to delete
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
