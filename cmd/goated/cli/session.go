package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/claude"
)

func makeBridge() *claude.TmuxBridge {
	cfg := app.LoadConfig()
	return &claude.TmuxBridge{
		WorkspaceDir:        cfg.WorkspaceDir,
		LogDir:              cfg.LogDir,
		ContextWindowTokens: cfg.ContextWindowTokens,
	}
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage the Claude Code tmux session",
}

var sessionRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Kill and restart the Claude Code tmux session",
	RunE: func(cmd *cobra.Command, args []string) error {
		bridge := makeBridge()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		fmt.Fprintln(os.Stderr, "Killing existing session...")
		if err := bridge.StopSession(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
		// Let the process clean up
		time.Sleep(2 * time.Second)

		fmt.Fprintln(os.Stderr, "Starting fresh session...")
		if err := bridge.EnsureSession(ctx); err != nil {
			return fmt.Errorf("failed to start session: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Session ready.")
		return nil
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Claude Code session health and busy state",
	RunE: func(cmd *cobra.Command, args []string) error {
		bridge := makeBridge()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := bridge.SessionHealthy(ctx); err != nil {
			fmt.Printf("Health: UNHEALTHY (%v)\n", err)
		} else {
			fmt.Println("Health: OK")
		}

		busy, err := bridge.IsSessionBusy(ctx)
		if err != nil {
			fmt.Printf("Busy: unknown (%v)\n", err)
		} else if busy {
			fmt.Println("Busy: yes (working)")
		} else {
			fmt.Println("Busy: no (idle at prompt)")
		}

		pct := bridge.ContextUsagePercent("")
		fmt.Printf("Context: ~%d%% (rough estimate from scrollback)\n", pct)

		return nil
	},
}

var sessionSendCmd = &cobra.Command{
	Use:   "send [text]",
	Short: "Send text to the Claude Code tmux session",
	Long: `Paste text into the Claude Code tmux session and press Enter.
Text can be provided as arguments or piped via stdin.

Examples:
  ./goated session send /context
  ./goated session send "What are you working on?"
  echo "/context" | ./goated session send`,
	RunE: func(cmd *cobra.Command, args []string) error {
		bridge := makeBridge()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var text string
		if len(args) > 0 {
			text = strings.Join(args, " ")
		} else {
			// Try stdin
			stat, _ := os.Stdin.Stat()
			if stat.Mode()&os.ModeCharDevice == 0 {
				data, err := os.ReadFile("/dev/stdin")
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				text = strings.TrimSpace(string(data))
			}
		}
		if text == "" {
			return fmt.Errorf("no text provided; pass as arguments or pipe via stdin")
		}

		if err := bridge.EnsureSession(ctx); err != nil {
			return err
		}

		if err := bridge.SendRaw(ctx, text); err != nil {
			return fmt.Errorf("send failed: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Sent to session (%d chars)\n", len(text))
		return nil
	},
}

func init() {
	sessionCmd.AddCommand(sessionRestartCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionSendCmd)
	rootCmd.AddCommand(sessionCmd)
}
