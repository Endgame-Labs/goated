package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
				allowed := parseAllowedChatIDs(ch.Config["allowed_chat_ids"])
				if len(allowed) == 0 {
					fmt.Printf("  %-20s  allowed_chat_ids=(none — bot will refuse to start)\n", "")
				} else {
					fmt.Printf("  %-20s  allowed_chat_ids=%s\n", "", strings.Join(allowed, ","))
				}
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

	name := promptRequired(reader, "Channel name (e.g. my-telegram, work-slack)", "")

	chType := promptOneOf(reader, "Type (telegram/slack)", "slack", []string{"telegram", "slack"})

	config := make(map[string]string)

	switch chType {
	case "telegram":
		fmt.Println()
		fmt.Println("  To get a Telegram bot token:")
		fmt.Println("  1. Open Telegram and message @BotFather")
		fmt.Println("  2. Send /newbot and follow the prompts")
		fmt.Println("  3. Copy the bot token it gives you")
		fmt.Println("  https://core.telegram.org/bots#botfather")
		fmt.Println()

		config["bot_token"] = promptSecretRequired(reader, "Telegram bot token")

		fmt.Println()
		fmt.Println("  Allowed chat IDs restrict who can message the bot. The daemon will")
		fmt.Println("  refuse to start until at least one chat ID is allowed.")
		fmt.Println()
		fmt.Println("  How to get your personal chat ID:")
		fmt.Println("    DM @userinfobot on Telegram; it replies with your numeric ID.")
		fmt.Println()
		fmt.Println("  How to get a group chat ID (optional, for group-chat support):")
		fmt.Println("    Add the bot to the group, send `/start@<yourbotname>`, then check")
		fmt.Println("    logs/goated_daemon.log for a `rejected ... chat_id=<negative-id>` line.")
		fmt.Println("    You can add group IDs now (comma-separated) or later via")
		fmt.Println("    `goated channel allow -- <name> <chat-id>` (the `--` is required")
		fmt.Println("    because group IDs start with `-`).")
		fmt.Println()
		allowed := prompt(reader, "Allowed chat IDs (comma-separated)", "")
		if parsed, err := normalizeAllowedChatIDs(allowed); err != nil {
			return nil, err
		} else {
			config["allowed_chat_ids"] = parsed
		}

		mode := prompt(reader, "Mode (polling/webhook)", "polling")
		config["mode"] = mode

		if mode == "webhook" {
			config["webhook_url"] = prompt(reader, "Webhook public URL", "")
			config["webhook_addr"] = prompt(reader, "Webhook listen address", ":8080")
			config["webhook_path"] = prompt(reader, "Webhook path", "/telegram/webhook")
		}

	case "slack":
		appName := prompt(reader, "Slack app display name", name)

		manifest := slackAppManifest(appName)
		fmt.Println()
		fmt.Println("  Copy the manifest below and use it to create or update your Slack app:")
		fmt.Println()
		fmt.Println("  New app:      https://api.slack.com/apps → Create New App → From a manifest")
		fmt.Println("  Existing app: https://api.slack.com/apps → Your app → App Manifest → paste & save")
		fmt.Println()
		fmt.Println("  ── manifest.json ──")
		fmt.Println(manifest)
		fmt.Println("  ────────────────────")
		fmt.Println()
		fmt.Println("  After creating/updating the app:")
		fmt.Println("  1. Install the app to your workspace (OAuth & Permissions → Install)")
		fmt.Println("  2. Copy the Bot Token (xoxb-...) from OAuth & Permissions")
		fmt.Println("  3. Go to Socket Mode → generate an App Token (xapp-...)")
		fmt.Println("     Add the 'connections:write' scope when prompted")
		fmt.Println("  4. To find your channel ID, right-click a channel in Slack")
		fmt.Println("     → View channel details → copy the ID at the bottom")
		fmt.Println()

		config["bot_token"] = promptSecretRequired(reader, "Slack bot token (xoxb-...)")
		config["app_token"] = promptSecretRequired(reader, "Slack app token (xapp-...)")
		config["channel_id"] = promptRequired(reader, "Slack DM channel ID", "")
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
		allowed := parseAllowedChatIDs(ch.Config["allowed_chat_ids"])
		ids := make([]any, 0, len(allowed))
		for _, id := range allowed {
			ids = append(ids, id)
		}
		telegram["allowed_chat_ids"] = ids
		existing["telegram"] = telegram

	case "slack":
		slack := make(map[string]any)
		if s, ok := existing["slack"].(map[string]any); ok {
			slack = s
		}
		if v := ch.Config["channel_id"]; v != "" {
			slack["channel_id"] = v
		}
		existing["slack"] = slack
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
		if err := app.RemoveCred(credsDir, "GOAT_SLACK_CHANNEL_ID"); err != nil {
			return err
		}
	}
	return nil
}

// slackAppManifest returns a JSON manifest for creating/updating a Slack app
// with all the scopes, events, and settings goated needs.
func slackAppManifest(displayName string) string {
	manifest := map[string]any{
		"display_information": map[string]any{
			"name": displayName,
		},
		"features": map[string]any{
			"app_home": map[string]any{
				"home_tab_enabled":               false,
				"messages_tab_enabled":           true,
				"messages_tab_read_only_enabled": false,
			},
			"bot_user": map[string]any{
				"display_name":  displayName,
				"always_online": true,
			},
		},
		"oauth_config": map[string]any{
			"scopes": map[string]any{
				"bot": []string{
					"channels:history",
					"channels:read",
					"chat:write",
					"files:read",
					"files:write",
					"im:history",
					"im:read",
					"im:write",
				},
			},
		},
		"settings": map[string]any{
			"event_subscriptions": map[string]any{
				"bot_events": []string{
					"message.channels",
					"message.im",
				},
			},
			"org_deploy_enabled":  false,
			"socket_mode_enabled": true,
		},
	}

	data, _ := json.MarshalIndent(manifest, "  ", "  ")
	return "  " + string(data)
}

var channelAllowCmd = &cobra.Command{
	Use:   "allow <channel> <chat-id>",
	Short: "Add a Telegram chat ID to the allowlist for a channel",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateAllowedChatIDs(args[0], args[1], true)
	},
}

var channelUnallowCmd = &cobra.Command{
	Use:   "unallow <channel> <chat-id>",
	Short: "Remove a Telegram chat ID from the allowlist for a channel",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateAllowedChatIDs(args[0], args[1], false)
	},
}

var channelAllowListCmd = &cobra.Command{
	Use:   "allow-list <channel>",
	Short: "Show the Telegram chat ID allowlist for a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		ch, err := store.GetChannel(args[0])
		if err != nil {
			return err
		}
		if ch.Type != "telegram" {
			return fmt.Errorf("channel %q is type %q; allow-list only applies to telegram channels", ch.Name, ch.Type)
		}
		ids := parseAllowedChatIDs(ch.Config["allowed_chat_ids"])
		if len(ids) == 0 {
			fmt.Println("(empty — bot will refuse to start)")
			return nil
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return nil
	},
}

func mutateAllowedChatIDs(channelName, chatIDArg string, add bool) error {
	chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDArg), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat id %q: %w", chatIDArg, err)
	}

	cfg := app.LoadConfig()
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	ch, err := store.GetChannel(channelName)
	if err != nil {
		return err
	}
	if ch.Type != "telegram" {
		return fmt.Errorf("channel %q is type %q; allowlist only applies to telegram channels", ch.Name, ch.Type)
	}

	existing := parseAllowedChatIDs(ch.Config["allowed_chat_ids"])
	idStr := strconv.FormatInt(chatID, 10)
	updated := make([]string, 0, len(existing)+1)
	found := false
	for _, id := range existing {
		if id == idStr {
			found = true
			if add {
				updated = append(updated, id)
			}
			continue
		}
		updated = append(updated, id)
	}
	if add {
		if found {
			fmt.Printf("Chat ID %s already allowed on channel %q.\n", idStr, ch.Name)
			return nil
		}
		updated = append(updated, idStr)
	} else if !found {
		return fmt.Errorf("chat id %s not in allowlist for channel %q", idStr, ch.Name)
	}

	if ch.Config == nil {
		ch.Config = make(map[string]string)
	}
	ch.Config["allowed_chat_ids"] = strings.Join(updated, ",")

	if err := store.UpdateChannel(*ch); err != nil {
		return err
	}

	active := store.GetMeta("active_channel")
	if active == ch.Name {
		if err := writeChannelConfig(ch); err != nil {
			return err
		}
		fmt.Println("Updated active channel; restart the daemon for changes to take effect.")
	}

	if add {
		fmt.Printf("Added chat ID %s to channel %q. Allowlist now: %s\n", idStr, ch.Name, strings.Join(updated, ","))
	} else {
		if len(updated) == 0 {
			fmt.Printf("Removed chat ID %s from channel %q. Allowlist now empty.\n", idStr, ch.Name)
		} else {
			fmt.Printf("Removed chat ID %s from channel %q. Allowlist now: %s\n", idStr, ch.Name, strings.Join(updated, ","))
		}
	}
	return nil
}

// parseAllowedChatIDs splits a stored comma-separated allowlist into a slice of
// trimmed non-empty ID strings. Deduplicates while preserving order.
func parseAllowedChatIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// normalizeAllowedChatIDs validates that each comma-separated entry parses as an
// int64 and returns a canonical comma-separated string.
func normalizeAllowedChatIDs(raw string) (string, error) {
	ids := parseAllowedChatIDs(raw)
	for _, id := range ids {
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return "", fmt.Errorf("invalid chat id %q: %w", id, err)
		}
	}
	return strings.Join(ids, ","), nil
}

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelAddCmd)
	channelCmd.AddCommand(channelSwitchCmd)
	channelCmd.AddCommand(channelDeleteCmd)
	channelCmd.AddCommand(channelAllowCmd)
	channelCmd.AddCommand(channelUnallowCmd)
	channelCmd.AddCommand(channelAllowListCmd)
	rootCmd.AddCommand(channelCmd)
}
