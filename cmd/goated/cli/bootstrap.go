package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/db"
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Initialize database, workspace, and configure your first channel",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("=== goated bootstrap ===")
		fmt.Println()

		// Load existing config if present
		configPath := "goated.json"
		existing, _ := app.ReadConfigJSON(configPath)

		// Prompt for common settings
		tz := prompt(reader, "Default timezone", withDefault(strFromMap(existing, "default_timezone"), "America/Los_Angeles"))
		runtime := prompt(reader, "Agent runtime (claude/claude_tui/codex_tui)", withDefault(strFromMap(existing, "agent_runtime"), "claude"))
		if runtime != "claude" && runtime != "claude_tui" && runtime != "codex_tui" {
			return fmt.Errorf("agent runtime must be claude, claude_tui, or codex_tui")
		}

		// Interactive channel setup
		fmt.Println()
		ch, err := promptChannel(reader)
		if err != nil {
			return err
		}

		// Build config map
		configMap := make(map[string]any)
		configMap["gateway"] = ch.Type
		configMap["agent_runtime"] = runtime
		configMap["default_timezone"] = tz
		configMap["workspace_dir"] = withDefault(strFromMap(existing, "workspace_dir"), "workspace")
		if v := strFromMap(existing, "db_path"); v != "" {
			configMap["db_path"] = v
		}
		if v := strFromMap(existing, "log_dir"); v != "" {
			configMap["log_dir"] = v
		}

		// Write goated.json
		if err := app.WriteConfigJSON(configPath, configMap); err != nil {
			return fmt.Errorf("write goated.json: %w", err)
		}
		fmt.Println("Wrote goated.json")

		// Write channel secrets to creds files
		workspace := withDefault(strFromMap(existing, "workspace_dir"), "workspace")
		credsDir := filepath.Join(workspace, "creds")
		if err := writeChannelCreds(credsDir, ch); err != nil {
			return fmt.Errorf("write creds: %w", err)
		}

		// Init DB
		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()
		fmt.Println()
		fmt.Println("Database initialized at", cfg.DBPath)

		// Ensure workspace dir exists
		if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
			return fmt.Errorf("mkdir workspace: %w", err)
		}
		fmt.Println("Workspace directory:", cfg.WorkspaceDir)

		// Save channel to DB
		if err := store.AddChannel(*ch); err != nil {
			return err
		}
		if err := store.SetMeta("active_channel", ch.Name); err != nil {
			return err
		}
		fmt.Printf("Channel %q (%s) added and activated.\n", ch.Name, ch.Type)

		// Write final goated.json with channel config active
		if err := writeChannelConfig(ch); err != nil {
			return fmt.Errorf("write goated.json: %w", err)
		}

		// Add hourly self-sync system cron
		syncCmd := "./goat sync_self_to_github"
		_, err = store.AddCron("system", "", "0 * * * *", "", "", syncCmd, tz, true)
		if err != nil {
			fmt.Printf("Warning: could not create sync cron: %v\n", err)
		} else {
			fmt.Println("Added hourly sync_self_to_github system cron.")
		}

		fmt.Println()
		fmt.Println("Bootstrap complete. Next steps:")
		fmt.Println("  1. Build:       ./build.sh")
		fmt.Println("  2. Start:       ./goated daemon run")
		fmt.Println("  3. Watchdog:    Install the daemon watchdog cron (checks every 2 min):")
		fmt.Println()
		repoRoot, _ := os.Getwd()
		fmt.Printf("     (crontab -l 2>/dev/null; echo '*/2 * * * * %s/scripts/watchdog.sh') | crontab -\n", repoRoot)
		fmt.Println()
		return nil
	},
}

// strFromMap safely extracts a string value from a map[string]any.
func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}
