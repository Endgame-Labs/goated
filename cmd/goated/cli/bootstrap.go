package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

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

		// Seed .env from .env.example if it doesn't exist yet
		if _, err := os.Stat(".env"); os.IsNotExist(err) {
			if example, err := os.ReadFile(".env.example"); err == nil {
				_ = os.WriteFile(".env", example, 0o600)
				fmt.Println("Created .env from .env.example")
			}
		}

		// Prompt for common settings first
		existing := loadExistingEnv(".env")
		tz := prompt(reader, "Default timezone", withDefault(existing["GOAT_DEFAULT_TIMEZONE"], "America/Los_Angeles"))

		// Interactive channel setup
		fmt.Println()
		ch, err := promptChannel(reader)
		if err != nil {
			return err
		}

		// Write initial .env with common settings so LoadConfig works
		var envBuilder strings.Builder
		envBuilder.WriteString("# goated configuration\n")
		envBuilder.WriteString(fmt.Sprintf("GOAT_DEFAULT_TIMEZONE=%s\n", tz))
		if v := existing["GOAT_DB_PATH"]; v != "" {
			envBuilder.WriteString(fmt.Sprintf("GOAT_DB_PATH=%s\n", v))
		}
		if v := existing["GOAT_WORKSPACE_DIR"]; v != "" {
			envBuilder.WriteString(fmt.Sprintf("GOAT_WORKSPACE_DIR=%s\n", v))
		}
		if v := existing["GOAT_LOG_DIR"]; v != "" {
			envBuilder.WriteString(fmt.Sprintf("GOAT_LOG_DIR=%s\n", v))
		}
		if err := os.WriteFile(".env", []byte(envBuilder.String()), 0o600); err != nil {
			return fmt.Errorf("write .env: %w", err)
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

		// Write final .env with channel config active
		if err := writeChannelEnv(cfg, ch); err != nil {
			return fmt.Errorf("write .env: %w", err)
		}
		fmt.Println("Wrote .env")

		// Add hourly self-sync system cron
		syncCmd := "./goat sync_self_to_github"
		_, err = store.AddCron("system", "", "0 * * * *", "", "", syncCmd, tz, true)
		if err != nil {
			fmt.Printf("Warning: could not create sync cron: %v\n", err)
		} else {
			fmt.Println("Added hourly sync_self_to_github system cron.")
		}

		fmt.Println()
		fmt.Println("Bootstrap complete. Run ./goated_daemon to start.")
		return nil
	},
}

func loadExistingEnv(path string) map[string]string {
	m := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return m
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}
