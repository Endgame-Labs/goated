package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/agent"
	"goated/internal/app"
	runtimepkg "goated/internal/runtime"
)

func makeRuntime() (agent.Runtime, error) {
	cfg := app.LoadConfig()
	return runtimepkg.New(cfg)
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage the active agent runtime session",
}

var sessionRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Kill and restart the active agent runtime session",
	RunE: func(cmd *cobra.Command, args []string) error {
		runtime, err := makeRuntime()
		if err != nil {
			return err
		}
		session := runtime.Session()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		fmt.Fprintln(os.Stderr, "Killing existing session...")
		if err := session.StopSession(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
		time.Sleep(2 * time.Second)

		fmt.Fprintln(os.Stderr, "Starting fresh session...")
		if err := session.EnsureSession(ctx); err != nil {
			return fmt.Errorf("failed to start session: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Session ready.")
		return nil
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active runtime session health and busy state",
	RunE: func(cmd *cobra.Command, args []string) error {
		runtime, err := makeRuntime()
		if err != nil {
			return err
		}
		session := runtime.Session()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		fmt.Printf("Runtime: %s\n", session.Descriptor().DisplayName)
		health, err := session.GetHealth(ctx)
		if err != nil {
			fmt.Printf("Health: unknown (%v)\n", err)
		} else if !health.OK {
			recovery := "recoverable"
			if !health.Recoverable {
				recovery = "manual-intervention"
			}
			fmt.Printf("Health: UNHEALTHY (%s: %s)\n", recovery, health.Summary)
		} else {
			fmt.Println("Health: OK")
		}

		state, err := session.GetSessionState(ctx)
		if err != nil {
			fmt.Printf("Busy: unknown (%v)\n", err)
		} else if state.SafeIdle() {
			fmt.Println("Busy: no (idle at prompt)")
		} else {
			fmt.Printf("Busy: yes (%s)\n", state.Summary)
		}

		if session.Descriptor().Capabilities.SupportsContextEstimate {
			estimate, err := session.GetContextEstimate(ctx, "")
			if err != nil || estimate.State != agent.ContextEstimateKnown {
				fmt.Println("Context: unknown")
			} else {
				fmt.Printf("Context: ~%d%% (%s)\n", estimate.PercentUsed, estimate.RawSummary)
			}
		} else {
			fmt.Println("Context: unsupported")
		}

		return nil
	},
}

var sessionSendCmd = &cobra.Command{
	Use:   "send [text]",
	Short: "Send text to the active runtime session",
	Long: `Paste text into the active runtime session and press Enter.
Text can be provided as arguments or piped via stdin.

Examples:
  ./goated session send /context
  ./goated session send "What are you working on?"
  echo "/context" | ./goated session send`,
	RunE: func(cmd *cobra.Command, args []string) error {
		runtime, err := makeRuntime()
		if err != nil {
			return err
		}
		session := runtime.Session()
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

		if err := session.EnsureSession(ctx); err != nil {
			return err
		}

		if err := session.SendControlCommand(ctx, text); err != nil {
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
