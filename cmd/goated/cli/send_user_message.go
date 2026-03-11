package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/app"
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

		// If sent by a subagent/cron, share context with the main session
		source, _ := cmd.Flags().GetString("source")
		logPath, _ := cmd.Flags().GetString("log")
		if source != "" {
			notifyMainSession(chatID, source, logPath, text)
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

	_, _, err := client.PostMessage(channelID,
		slackapi.MsgOptionText(mrkdwn, false),
		slackapi.MsgOptionDisableLinkUnfurl(),
	)
	if err != nil {
		return fmt.Errorf("send slack message: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Message sent to channel %s (%d chars)\n", channelID, len(text))
	return nil
}

// notifyMainSession pastes a context-only notification into the goat_main
// tmux session so the interactive Claude has awareness of messages sent by
// subagents and cron jobs.
func notifyMainSession(chatID, source, logPath, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if main session exists
	if err := exec.CommandContext(ctx, "tmux", "has-session", "-t", "goat_main").Run(); err != nil {
		return
	}

	// Truncate message for context efficiency
	truncated := message
	if len(truncated) > 500 {
		truncated = truncated[:500] + "\n... (truncated)"
	}

	var logLine string
	if logPath != "" {
		logLine = fmt.Sprintf("\nLog: %s", logPath)
	}

	notification := fmt.Sprintf(
		"[Context from %s — sent to chat %s]%s\n\n%s\n\nThis is background context only. Do NOT respond or call send_user_message.",
		source, chatID, logLine, truncated,
	)

	// Write to temp file and paste into tmux
	tmp, err := os.CreateTemp("", "goat-notify-*.txt")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(notification); err != nil {
		tmp.Close()
		return
	}
	tmp.Close()

	target := "goat_main:0.0"
	_ = exec.CommandContext(ctx, "tmux", "load-buffer", "-b", "goat_notify", tmp.Name()).Run()
	_ = exec.CommandContext(ctx, "tmux", "paste-buffer", "-b", "goat_notify", "-t", target).Run()
	_ = exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "Enter").Run()
}

func init() {
	sendUserMessageCmd.Flags().String("chat", "", "Chat/channel ID to send to (required)")
	sendUserMessageCmd.Flags().String("source", "", "Caller source (e.g. cron, subagent) — triggers main session notification")
	sendUserMessageCmd.Flags().String("log", "", "Path to the caller's log file")
	rootCmd.AddCommand(sendUserMessageCmd)
}
