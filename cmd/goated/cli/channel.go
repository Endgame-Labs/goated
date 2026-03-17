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

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage messaging channels (Telegram, Slack)",
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured channels",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		channels, err := store.AllChannels()
		if err != nil {
			return err
		}
		if len(channels) == 0 {
			fmt.Println("No channels configured. Run: goated channel add")
			return nil
		}

		activeChannelName := store.GetMeta("active_channel")

		for _, ch := range channels {
			marker := "  "
			if ch.Name == activeChannelName {
				marker = "* "
			}
			fmt.Printf("%s%-20s  type=%-10s  created=%s\n", marker, ch.Name, ch.Type, ch.CreatedAt)

			switch ch.Type {
			case "telegram":
				mode := ch.Config["mode"]
				if mode == "" {
					mode = "polling"
				}
				fmt.Printf("  %-20s  mode=%s\n", "", mode)
			case "slack":
				chID := ch.Config["channel_id"]
				if chID != "" {
					fmt.Printf("  %-20s  channel=%s\n", "", chID)
				}
			}
		}

		fmt.Println()
		fmt.Println("* = active channel")
		return nil
	},
}

var channelAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new messaging channel",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		ch, err := promptChannel(bufio.NewReader(os.Stdin))
		if err != nil {
			return err
		}

		if err := store.AddChannel(*ch); err != nil {
			return err
		}

		fmt.Printf("\nChannel %q (%s) added.\n", ch.Name, ch.Type)
		fmt.Println("To activate it, run: goated channel switch", ch.Name)
		return nil
	},
}

var channelSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Switch the active messaging channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		ch, err := store.GetChannel(name)
		if err != nil {
			return err
		}

		if err := writeChannelConfig(ch); err != nil {
			return err
		}

		// Write channel secrets to creds files
		credsDir := filepath.Join(cfg.WorkspaceDir, "creds")
		if err := writeChannelCreds(credsDir, ch); err != nil {
			return err
		}

		if err := store.SetMeta("active_channel", name); err != nil {
			return err
		}

		fmt.Printf("Switched to channel %q (%s).\n", ch.Name, ch.Type)
		fmt.Println("Restart the daemon for changes to take effect.")
		return nil
	},
}

var channelDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a configured channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		activeChannel := store.GetMeta("active_channel")
		if name == activeChannel {
			return fmt.Errorf("cannot delete the active channel %q — switch to another channel first", name)
		}

		if err := store.DeleteChannel(name); err != nil {
			return err
		}

		fmt.Printf("Channel %q deleted.\n", name)
		return nil
	},
}

// promptChannel runs the interactive prompts for adding a channel.
func promptChannel(reader *bufio.Reader) (*db.Channel, error) {
	fmt.Println("=== Add Channel ===")
	fmt.Println()

	name := prompt(reader, "Channel name (e.g. my-telegram, work-slack)", "")
	if name == "" {
		return nil, fmt.Errorf("channel name is required")
	}

	chType := prompt(reader, "Type (telegram/slack)", "telegram")
	if chType != "telegram" && chType != "slack" {
		return nil, fmt.Errorf("type must be telegram or slack")
	}

	config := make(map[string]string)

	switch chType {
	case "telegram":
		token := prompt(reader, "Telegram bot token", "")
		if token == "" {
			return nil, fmt.Errorf("telegram bot token is required")
		}
		config["bot_token"] = token

		mode := prompt(reader, "Mode (polling/webhook)", "polling")
		config["mode"] = mode

		if mode == "webhook" {
			config["webhook_url"] = prompt(reader, "Webhook public URL", "")
			config["webhook_addr"] = prompt(reader, "Webhook listen address", ":8080")
			config["webhook_path"] = prompt(reader, "Webhook path", "/telegram/webhook")
		}

	case "slack":
		botToken := prompt(reader, "Slack bot token (xoxb-...)", "")
		if botToken == "" {
			return nil, fmt.Errorf("slack bot token is required")
		}
		config["bot_token"] = botToken

		appToken := prompt(reader, "Slack app token (xapp-...)", "")
		if appToken == "" {
			return nil, fmt.Errorf("slack app token is required")
		}
		config["app_token"] = appToken

		channelID := prompt(reader, "Slack DM channel ID", "")
		if channelID == "" {
			return nil, fmt.Errorf("slack channel ID is required")
		}
		config["channel_id"] = channelID
	}

	return &db.Channel{
		Name:   name,
		Type:   chType,
		Config: config,
	}, nil
}

// writeChannelConfig updates goated.json to activate the given channel.
// Preserves non-gateway settings (timezone, workspace, db path, etc.).
func writeChannelConfig(ch *db.Channel) error {
	configPath := "goated.json"
	existing, err := app.ReadConfigJSON(configPath)
	if err != nil {
		return err
	}

	// Update gateway type
	existing["gateway"] = ch.Type

	// Update gateway-specific settings
	switch ch.Type {
	case "telegram":
		telegram := make(map[string]any)
		if t, ok := existing["telegram"].(map[string]any); ok {
			telegram = t
		}
		mode := withDefault(ch.Config["mode"], "polling")
		telegram["mode"] = mode
		if mode == "webhook" {
			if v := ch.Config["webhook_addr"]; v != "" {
				telegram["webhook_addr"] = v
			}
			if v := ch.Config["webhook_path"]; v != "" {
				telegram["webhook_path"] = v
			}
		}
		existing["telegram"] = telegram

	case "slack":
		// Slack settings section preserved (attachment limits etc.)
		// Secrets go to creds files, not config
	}

	return app.WriteConfigJSON(configPath, existing)
}

// writeChannelCreds writes channel secrets to workspace/creds/*.txt.
func writeChannelCreds(credsDir string, ch *db.Channel) error {
	switch ch.Type {
	case "telegram":
		if v := ch.Config["bot_token"]; v != "" {
			if err := app.WriteCred(credsDir, "GOAT_TELEGRAM_BOT_TOKEN", v); err != nil {
				return err
			}
		}
		if v := ch.Config["webhook_url"]; v != "" {
			if err := app.WriteCred(credsDir, "GOAT_TELEGRAM_WEBHOOK_URL", v); err != nil {
				return err
			}
		}
	case "slack":
		if v := ch.Config["bot_token"]; v != "" {
			if err := app.WriteCred(credsDir, "GOAT_SLACK_BOT_TOKEN", v); err != nil {
				return err
			}
		}
		if v := ch.Config["app_token"]; v != "" {
			if err := app.WriteCred(credsDir, "GOAT_SLACK_APP_TOKEN", v); err != nil {
				return err
			}
		}
		if v := ch.Config["channel_id"]; v != "" {
			if err := app.WriteCred(credsDir, "GOAT_SLACK_CHANNEL_ID", v); err != nil {
				return err
			}
		}
	}
	return nil
}

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelAddCmd)
	channelCmd.AddCommand(channelSwitchCmd)
	channelCmd.AddCommand(channelDeleteCmd)
	rootCmd.AddCommand(channelCmd)
}
