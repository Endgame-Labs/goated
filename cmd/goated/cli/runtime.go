package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/agent"
	"goated/internal/app"
	runtimepkg "goated/internal/runtime"
	"goated/internal/tmux"
)

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Manage the active agent runtime",
}

var runtimeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show configured runtime, version, readiness, and tmux sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		runtime, err := runtimepkg.New(cfg)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		fmt.Printf("Configured runtime: %s\n", runtime.Descriptor().DisplayName)
		fmt.Printf("Runtime key: %s\n", runtime.Descriptor().Provider)
		fmt.Printf("Workspace: %s\n", cfg.WorkspaceDir)
		fmt.Printf("Session name: %s\n", runtime.Descriptor().SessionName)

		version := runtime.Session().Version(ctx)
		if version == "" {
			version = "(unknown)"
		}
		fmt.Printf("Version: %s\n", version)

		fmt.Printf("Capabilities: interactive=%t context=%t compact=%t reset=%t\n",
			runtime.Descriptor().Capabilities.SupportsInteractiveSession,
			runtime.Descriptor().Capabilities.SupportsContextEstimate,
			runtime.Descriptor().Capabilities.SupportsCompaction,
			runtime.Descriptor().Capabilities.SupportsReset,
		)

		if err := runtimepkg.Validate(ctx, runtime, cfg.WorkspaceDir); err != nil {
			fmt.Printf("Readiness: NOT READY (%v)\n", err)
		} else {
			fmt.Println("Readiness: OK")
		}

		health, err := runtime.Session().GetHealth(ctx)
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

		state, err := runtime.Session().GetSessionState(ctx)
		if err != nil {
			fmt.Printf("Session state: unknown (%v)\n", err)
		} else {
			fmt.Printf("Session state: %s (%s)\n", state.Kind, state.Summary)
		}

		// Show claude (headless) runtime status
		claudeMarker := "inactive"
		if runtime.Descriptor().Provider == agent.RuntimeClaude {
			claudeMarker = "active"
		}
		fmt.Printf("\nRuntimes:\n")
		fmt.Printf("  %-16s mode=%-12s (%s)\n", "Claude Code", "headless", claudeMarker)

		fmt.Println("\nTmux sessions:")
		for _, desc := range []agent.RuntimeDescriptor{
			{
				Provider:    agent.RuntimeClaudeTUI,
				DisplayName: "Claude Code TUI",
				SessionName: "goat_claude_tui_main",
			},
			{
				Provider:    agent.RuntimeCodexTUI,
				DisplayName: "Codex TUI",
				SessionName: "goat_codex_tui_main",
			},
		} {
			marker := "inactive"
			if desc.Provider == runtime.Descriptor().Provider {
				marker = "active"
			}
			running := tmux.SessionExistsFor(ctx, desc.SessionName)
			fmt.Printf("  %-16s session=%-18s running=%t (%s)\n", desc.DisplayName, desc.SessionName, running, marker)
		}
		return nil
	},
}

var runtimeSwitchCmd = &cobra.Command{
	Use:   "switch <claude|claude_tui|codex_tui>",
	Short: "Switch the configured agent runtime",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		if target != string(agent.RuntimeClaude) && target != string(agent.RuntimeClaudeTUI) && target != string(agent.RuntimeCodexTUI) {
			return fmt.Errorf("runtime must be claude, claude_tui, or codex_tui")
		}

		existing := loadExistingEnv(".env")
		existing["GOAT_AGENT_RUNTIME"] = target
		if existing["GOAT_WORKSPACE_DIR"] == "" {
			existing["GOAT_WORKSPACE_DIR"] = "workspace"
		}
		if err := writeEnvMap(".env", existing); err != nil {
			return err
		}

		fmt.Printf("Configured runtime switched to %s.\n", target)
		fmt.Printf("Active session after restart: %s\n", sessionNameForRuntime(target))
		fmt.Println("Restart the daemon for changes to take effect.")
		return nil
	},
}

var runtimeCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up inactive runtime sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		inactive := sessionNameForRuntime(string(agent.RuntimeClaudeTUI))
		if cfg.AgentRuntime == string(agent.RuntimeClaudeTUI) {
			inactive = sessionNameForRuntime(string(agent.RuntimeCodexTUI))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if !tmux.SessionExistsFor(ctx, inactive) {
			fmt.Printf("No inactive runtime session to clean up (%s).\n", inactive)
			return nil
		}
		if err := tmux.Run(ctx, "kill-session", "-t", inactive); err != nil {
			return err
		}
		fmt.Printf("Removed inactive runtime session %s.\n", inactive)
		return nil
	},
}

func sessionNameForRuntime(runtime string) string {
	switch runtime {
	case string(agent.RuntimeClaude):
		return "goat_claude_main"
	case string(agent.RuntimeCodexTUI):
		return "goat_codex_tui_main"
	default:
		return "goat_claude_tui_main"
	}
}

func writeEnvMap(path string, values map[string]string) error {
	if values == nil {
		values = map[string]string{}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, "# goated configuration"); err != nil {
		return err
	}
	for _, k := range keys {
		if values[k] == "" {
			continue
		}
		if _, err := fmt.Fprintf(f, "%s=%s\n", k, values[k]); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	runtimeCmd.AddCommand(runtimeStatusCmd)
	runtimeCmd.AddCommand(runtimeSwitchCmd)
	runtimeCmd.AddCommand(runtimeCleanupCmd)
	rootCmd.AddCommand(runtimeCmd)
}
