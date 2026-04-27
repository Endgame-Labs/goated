package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/msglog"
)

var sendUserFileCmd = &cobra.Command{
	Use:   "send_user_file",
	Short: "Queue a file or image for delivery via the daemon",
	Long: `Queue a file for the daemon to send to the user.

Examples:
  ./goat send_user_file --chat 123456 --path /tmp/screenshot.png
  ./goat send_user_file --chat 123456 --path /tmp/report.pdf --caption "Latest report"
  ./goat send_user_file --chat 123456 --path /tmp/screenshot.png --type photo`,
	RunE: func(cmd *cobra.Command, args []string) error {
		chatID, _ := cmd.Flags().GetString("chat")
		filePath, _ := cmd.Flags().GetString("path")
		caption, _ := cmd.Flags().GetString("caption")
		mediaType, _ := cmd.Flags().GetString("type")
		source, _ := cmd.Flags().GetString("source")
		logPath, _ := cmd.Flags().GetString("log")

		if chatID == "" {
			return fmt.Errorf("--chat is required")
		}
		if strings.TrimSpace(filePath) == "" {
			return fmt.Errorf("--path is required")
		}
		if _, err := os.Stat(filePath); err != nil {
			return fmt.Errorf("stat %s: %w", filePath, err)
		}
		if mediaType == "" {
			mediaType = "auto"
		}
		switch mediaType {
		case "auto", "photo", "document":
		default:
			return fmt.Errorf("--type must be one of: auto, photo, document")
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
			FilePath:  filePath,
			Caption:   strings.TrimSpace(caption),
			MediaType: mediaType,
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
			return fmt.Errorf("daemon rejected file send: %s", resp.Error)
		}

		fmt.Fprintf(os.Stderr, "Queued daemon file delivery for chat %s (%s)\n", chatID, filePath)
		return nil
	},
}

func init() {
	sendUserFileCmd.Flags().String("chat", "", "Chat/channel ID to send to (required)")
	sendUserFileCmd.Flags().String("path", "", "Local file path to send (required)")
	sendUserFileCmd.Flags().String("caption", "", "Optional caption/description")
	sendUserFileCmd.Flags().String("type", "auto", "Media type: auto, photo, or document")
	sendUserFileCmd.Flags().String("source", "", "Caller source (e.g. cron, subagent) — reserved for daemon delivery")
	sendUserFileCmd.Flags().String("log", "", "Path to the caller's log file")
	rootCmd.AddCommand(sendUserFileCmd)
}
