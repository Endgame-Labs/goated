package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"goated/internal/app"
)

var migrateConfigCmd = &cobra.Command{
	Use:   "migrate-config",
	Short: "Migrate .env to goated.json + creds files",
	Long:  "Reads the existing .env file, splits settings into goated.json and secrets into workspace/creds/*.txt, then renames .env to .env.bak.",
	RunE: func(cmd *cobra.Command, args []string) error {
		envPath := ".env"
		if _, err := os.Stat(envPath); os.IsNotExist(err) {
			return fmt.Errorf(".env not found in current directory — nothing to migrate")
		}

		env := parseEnvFile(envPath)
		if len(env) == 0 {
			return fmt.Errorf(".env is empty — nothing to migrate")
		}

		// Build goated.json config map
		configMap := make(map[string]any)

		// Top-level settings
		if v := env["GOAT_GATEWAY"]; v != "" {
			configMap["gateway"] = v
		}
		if v := env["GOAT_AGENT_RUNTIME"]; v != "" {
			configMap["agent_runtime"] = v
		}
		if v := env["GOAT_DEFAULT_TIMEZONE"]; v != "" {
			configMap["default_timezone"] = v
		}
		if v := env["GOAT_WORKSPACE_DIR"]; v != "" {
			configMap["workspace_dir"] = v
		}
		if v := env["GOAT_DB_PATH"]; v != "" {
			configMap["db_path"] = v
		}
		if v := env["GOAT_LOG_DIR"]; v != "" {
			configMap["log_dir"] = v
		}

		// Telegram settings
		telegram := make(map[string]any)
		if v := env["GOAT_TELEGRAM_MODE"]; v != "" {
			telegram["mode"] = v
		}
		if v := env["GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR"]; v != "" {
			telegram["webhook_addr"] = v
		}
		if v := env["GOAT_TELEGRAM_WEBHOOK_PATH"]; v != "" {
			telegram["webhook_path"] = v
		}
		if len(telegram) > 0 {
			configMap["telegram"] = telegram
		}

		// Slack settings
		slack := make(map[string]any)
		if v := env["GOAT_SLACK_ATTACHMENTS_ROOT"]; v != "" {
			slack["attachments_root"] = v
		}
		if v := env["GOAT_SLACK_ATTACHMENT_MAX_BYTES"]; v != "" {
			slack["attachment_max_bytes"] = v
		}
		if v := env["GOAT_SLACK_ATTACHMENT_MAX_TOTAL_BYTES"]; v != "" {
			slack["attachment_max_total_bytes"] = v
		}
		if v := env["GOAT_SLACK_ATTACHMENT_MAX_PARALLEL"]; v != "" {
			slack["attachment_max_parallel"] = v
		}
		if len(slack) > 0 {
			configMap["slack"] = slack
		}

		// Write goated.json
		configPath := "goated.json"
		if err := app.WriteConfigJSON(configPath, configMap); err != nil {
			return fmt.Errorf("write goated.json: %w", err)
		}
		fmt.Printf("Wrote settings to %s\n", configPath)

		// Determine workspace for creds dir
		workspace := env["GOAT_WORKSPACE_DIR"]
		if workspace == "" {
			workspace = "workspace"
		}
		credsDir := filepath.Join(workspace, "creds")

		// Migrate secrets to creds files
		secrets := map[string]string{
			"GOAT_TELEGRAM_BOT_TOKEN":    env["GOAT_TELEGRAM_BOT_TOKEN"],
			"GOAT_TELEGRAM_WEBHOOK_URL":  env["GOAT_TELEGRAM_WEBHOOK_URL"],
			"GOAT_SLACK_BOT_TOKEN":       env["GOAT_SLACK_BOT_TOKEN"],
			"GOAT_SLACK_APP_TOKEN":       env["GOAT_SLACK_APP_TOKEN"],
			"GOAT_SLACK_CHANNEL_ID":      env["GOAT_SLACK_CHANNEL_ID"],
			"GOAT_ADMIN_CHAT_ID":         env["GOAT_ADMIN_CHAT_ID"],
		}

		for key, val := range secrets {
			if val == "" {
				continue
			}
			if err := app.WriteCred(credsDir, key, val); err != nil {
				return fmt.Errorf("write cred %s: %w", key, err)
			}
			fmt.Printf("Wrote secret %s to %s/%s.txt\n", key, credsDir, key)
		}

		// Rename .env to .env.bak
		bakPath := envPath + ".bak"
		if err := os.Rename(envPath, bakPath); err != nil {
			return fmt.Errorf("rename .env: %w", err)
		}
		fmt.Printf("Renamed %s → %s\n", envPath, bakPath)

		fmt.Println()
		fmt.Println("Migration complete! The daemon will now use goated.json + workspace/creds/.")
		fmt.Println("Restart the daemon for changes to take effect.")
		return nil
	},
}

// parseEnvFile reads a .env file into a key-value map.
func parseEnvFile(path string) map[string]string {
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
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		m[key] = value
	}
	return m
}

func init() {
	rootCmd.AddCommand(migrateConfigCmd)
}
