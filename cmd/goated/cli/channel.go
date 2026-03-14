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

		activeGateway := cfg.Gateway
		activeID := cfg.AdminChatID // we'll match on the active channel name from meta
		_ = activeID

		activeChannelName := store.GetMeta("active_channel")

		for _, ch := range channels {
			marker := "  "
			if ch.Name == activeChannelName {
				marker = "* "
			}
			fmt.Printf("%s%-20s  type=%-10s  created=%s\n", marker, ch.Name, ch.Type, ch.CreatedAt)

			// Show key config details
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

		_ = activeGateway
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

		if err := writeChannelEnv(cfg, ch); err != nil {
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

// writeChannelEnv updates the .env file to activate the given channel.
// Preserves non-gateway settings (DB path, workspace, timezone, etc.).
func writeChannelEnv(cfg app.Config, ch *db.Channel) error {
	existing := loadExistingEnv(".env")

	var b strings.Builder
	b.WriteString("# goated configuration\n")
	b.WriteString(fmt.Sprintf("GOAT_GATEWAY=%s\n", ch.Type))
	b.WriteString(fmt.Sprintf("GOAT_AGENT_RUNTIME=%s\n",
		withDefault(existing["GOAT_AGENT_RUNTIME"], "claude")))

	// Preserve common settings
	b.WriteString(fmt.Sprintf("GOAT_DEFAULT_TIMEZONE=%s\n",
		withDefault(existing["GOAT_DEFAULT_TIMEZONE"], "America/Los_Angeles")))
	if v := existing["GOAT_DB_PATH"]; v != "" {
		b.WriteString(fmt.Sprintf("GOAT_DB_PATH=%s\n", v))
	}
	b.WriteString(fmt.Sprintf("GOAT_WORKSPACE_DIR=%s\n",
		withDefault(existing["GOAT_WORKSPACE_DIR"], "workspace")))
	if v := existing["GOAT_LOG_DIR"]; v != "" {
		b.WriteString(fmt.Sprintf("GOAT_LOG_DIR=%s\n", v))
	}
	if v := existing["GOAT_ADMIN_CHAT_ID"]; v != "" {
		b.WriteString(fmt.Sprintf("GOAT_ADMIN_CHAT_ID=%s\n", v))
	}
	if v := existing["GOAT_CONTEXT_WINDOW_TOKENS"]; v != "" {
		b.WriteString(fmt.Sprintf("GOAT_CONTEXT_WINDOW_TOKENS=%s\n", v))
	}

	// Write gateway-specific settings
	switch ch.Type {
	case "telegram":
		b.WriteString(fmt.Sprintf("GOAT_TELEGRAM_BOT_TOKEN=%s\n", ch.Config["bot_token"]))
		mode := withDefault(ch.Config["mode"], "polling")
		b.WriteString(fmt.Sprintf("GOAT_TELEGRAM_MODE=%s\n", mode))
		if mode == "webhook" {
			if v := ch.Config["webhook_url"]; v != "" {
				b.WriteString(fmt.Sprintf("GOAT_TELEGRAM_WEBHOOK_URL=%s\n", v))
			}
			if v := ch.Config["webhook_addr"]; v != "" {
				b.WriteString(fmt.Sprintf("GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR=%s\n", v))
			}
			if v := ch.Config["webhook_path"]; v != "" {
				b.WriteString(fmt.Sprintf("GOAT_TELEGRAM_WEBHOOK_PATH=%s\n", v))
			}
		}
	case "slack":
		b.WriteString(fmt.Sprintf("GOAT_SLACK_BOT_TOKEN=%s\n", ch.Config["bot_token"]))
		b.WriteString(fmt.Sprintf("GOAT_SLACK_APP_TOKEN=%s\n", ch.Config["app_token"]))
		b.WriteString(fmt.Sprintf("GOAT_SLACK_CHANNEL_ID=%s\n", ch.Config["channel_id"]))
	}

	return os.WriteFile(".env", []byte(b.String()), 0o600)
}

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelAddCmd)
	channelCmd.AddCommand(channelSwitchCmd)
	channelCmd.AddCommand(channelDeleteCmd)
	rootCmd.AddCommand(channelCmd)
}
