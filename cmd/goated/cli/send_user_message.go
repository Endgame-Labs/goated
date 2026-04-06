package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/msglog"
)

var sendUserMessageCmd = &cobra.Command{
	Use:   "send_user_message",
	Short: "Queue a markdown message for delivery via the daemon",
	Long: `Queue a message for the daemon to send to the user. The message is read
from stdin as markdown.

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
		threadTS, _ := cmd.Flags().GetString("thread")
		source, _ := cmd.Flags().GetString("source")
		logPath, _ := cmd.Flags().GetString("log")
		if chatID == "" {
			return fmt.Errorf("--chat is required")
		}

		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return fmt.Errorf("empty message; pipe markdown into stdin")
		}

		cfg := app.LoadConfig()
		requestID := os.Getenv("GOAT_REQUEST_ID")
		if requestID == "" {
			requestID = msglog.NewRequestID()
		}
		socketPath := filepath.Join(cfg.LogDir, "goated.sock")
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return fmt.Errorf("connect daemon socket %s: %w", socketPath, err)
		}
		defer conn.Close()

		if err := json.NewEncoder(conn).Encode(daemonSendRequest{
			RequestID: requestID,
			ChatID:    chatID,
			ThreadTS:  strings.TrimSpace(threadTS),
			Text:      text,
			Source:    strings.TrimSpace(source),
			LogPath:   strings.TrimSpace(logPath),
		}); err != nil {
			return fmt.Errorf("send daemon request: %w", err)
		}

		var resp daemonSendResponse
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			return fmt.Errorf("read daemon response: %w", err)
		}
		if !resp.OK {
			if resp.Error == "" {
				resp.Error = "unknown daemon error"
			}
			return fmt.Errorf("daemon rejected message: %s", resp.Error)
		}

		fmt.Fprintf(os.Stderr, "Queued daemon delivery for chat %s (%d chars)\n", chatID, len(text))
		if resp.MessageTS != "" {
			fmt.Println(resp.MessageTS)
		}
		return nil
	},
}

func init() {
	sendUserMessageCmd.Flags().String("chat", "", "Chat/channel ID to send to (required)")
	sendUserMessageCmd.Flags().String("thread", "", "Thread timestamp to reply to (Slack thread_ts)")
	sendUserMessageCmd.Flags().String("source", "", "Caller source (e.g. cron, subagent) — reserved for daemon delivery")
	sendUserMessageCmd.Flags().String("log", "", "Path to the caller's log file")
	rootCmd.AddCommand(sendUserMessageCmd)
}
