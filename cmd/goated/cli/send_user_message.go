package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/app"
	runtimepkg "goated/internal/runtime"
	slackpkg "goated/internal/slack"
	"goated/internal/util"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	slackapi "github.com/slack-go/slack"
)

var sendUserMessageCmd = &cobra.Command{
	Use:   "send_user_message",
	Short: "Send a markdown message to the user via the active gateway",
	Long: `Send a message to the user. The message is read from stdin as markdown.
The active gateway (telegram or slack) is determined by GOAT_GATEWAY.

Example:
  echo "Hello **world**" | ./goat send_user_message --chat 123456
  ./goat send_user_message --chat 123456 <<'EOF'
  Here is a code example:
` + "```python" + `
  print("hello")
` + "```" + `
  EOF`,
	RunE: func(cmd *cobra.Command, args []string) error {
		chatID, _ := cmd.Flags().GetString("chat")
		if chatID == "" {
			return fmt.Errorf("--chat is required")
		}

		cfg := app.LoadConfig()

		// Read message from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return fmt.Errorf("empty message; pipe markdown into stdin")
		}

		switch cfg.Gateway {
		case "slack":
			if err := sendViaSlack(cfg, chatID, text); err != nil {
				return err
			}
		default: // "telegram"
			if err := sendViaTelegram(cfg, chatID, text); err != nil {
				return err
			}
		}

		return nil
	},
}

func sendViaTelegram(cfg app.Config, chatID, text string) error {
	// Validate chat ID is a number (Telegram requires numeric IDs)
	if _, err := strconv.ParseInt(chatID, 10, 64); err != nil {
		return fmt.Errorf("invalid chat ID %q: must be a number", chatID)
	}

	token := cfg.TelegramBotToken
	if token == "" {
		return fmt.Errorf("GOAT_TELEGRAM_BOT_TOKEN is required")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return fmt.Errorf("init telegram: %w", err)
	}

	chat, _ := strconv.ParseInt(chatID, 10, 64)

	// Try HTML-formatted message first
	htmlText := util.MarkdownToTelegramHTML(text)
	msg := tgbotapi.NewMessage(chat, htmlText)
	msg.ParseMode = "HTML"
	if _, err := bot.Send(msg); err == nil {
		fmt.Fprintf(os.Stderr, "Message sent to chat %s (%d chars)\n", chatID, len(text))
	} else {
		// Fallback to plain text
		msg = tgbotapi.NewMessage(chat, text)
		if _, err := bot.Send(msg); err != nil {
			return fmt.Errorf("send message: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Message sent to chat %s (%d chars, plain text fallback)\n", chatID, len(text))
	}
	return nil
}

func sendViaSlack(cfg app.Config, channelID, text string) error {
	token := cfg.SlackBotToken
	if token == "" {
		return fmt.Errorf("GOAT_SLACK_BOT_TOKEN is required")
	}

	client := slackapi.New(token)
	mrkdwn := util.MarkdownToSlackMrkdwn(text)

	// Atomically claim the thinking indicator so no other process can race us
	hadThinking := false
	if ts := slackpkg.ClaimThinkingTS(); ts != "" {
		_, _, _ = client.DeleteMessage(channelID, ts)
		hadThinking = true
	}

	// Post the real response as a new message
	_, _, err := client.PostMessage(channelID,
		slackapi.MsgOptionText(mrkdwn, false),
		slackapi.MsgOptionDisableLinkUnfurl(),
	)
	if err != nil {
		return fmt.Errorf("send slack message: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Message sent to channel %s (%d chars)\n", channelID, len(text))

	// If we cleared a thinking indicator, check if the active runtime is still busy.
	// If so, post a new thinking indicator, then poll for idle and clean it up.
	if hadThinking {
		if isRuntimeBusy(cfg) {
			postSlackThinking(client, channelID)
			waitAndClearThinking(cfg, client, channelID)
		}
	}

	return nil
}

func isRuntimeBusy(cfg app.Config) bool {
	runtime, err := runtimepkg.New(cfg)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := runtime.Session().GetSessionState(ctx)
	if err != nil {
		return false
	}
	return state.Busy()
}

// waitAndClearThinking polls until the active runtime goes idle, then deletes
// any remaining thinking indicator. If another send_user_message call runs
// first and clears the ThinkingFile, this is a no-op.
func waitAndClearThinking(cfg app.Config, client *slackapi.Client, channelID string) {
	runtime, err := runtimepkg.New(cfg)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, _ = runtime.Session().WaitForAwaitingInput(ctx, 5*time.Minute)

	// Atomically claim the thinking indicator; if empty, another process handled it
	ts := slackpkg.ClaimThinkingTS()
	if ts == "" {
		return
	}
	_, _, _ = client.DeleteMessage(channelID, ts)
	fmt.Fprintf(os.Stderr, "Cleaned up orphaned thinking indicator in channel %s\n", channelID)
}

// postSlackThinking posts a new "_thinking..._" indicator and writes the
// ThinkingFile so the next send_user_message call can replace it.
// Also spawns a TTL reaper as a safety net against orphaned indicators.
func postSlackThinking(client *slackapi.Client, channelID string) {
	_, ts, err := client.PostMessage(channelID,
		slackapi.MsgOptionText("_thinking..._", false),
	)
	if err != nil {
		return
	}
	_ = slackpkg.WriteThinkingTS(ts)
	go slackpkg.ReapThinkingIndicator(client, channelID, ts)
}

func init() {
	sendUserMessageCmd.Flags().String("chat", "", "Chat/channel ID to send to (required)")
	sendUserMessageCmd.Flags().String("source", "", "Caller source (e.g. cron, subagent) — triggers main session notification")
	sendUserMessageCmd.Flags().String("log", "", "Path to the caller's log file")
	rootCmd.AddCommand(sendUserMessageCmd)
}
