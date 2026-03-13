package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/db"
	"goated/internal/gateway"
	runtimepkg "goated/internal/runtime"
	slackpkg "goated/internal/slack"
	"goated/internal/telegram"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Run a gateway connector",
}

var gatewayTelegramCmd = &cobra.Command{
	Use:   "telegram",
	Short: "Run the Telegram gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()

		if cfg.TelegramBotToken == "" {
			return fmt.Errorf("GOAT_TELEGRAM_BOT_TOKEN is required")
		}

		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		runtime, err := runtimepkg.New(cfg)
		if err != nil {
			return err
		}
		startupCtx, startupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer startupCancel()
		if err := runtimepkg.Validate(startupCtx, runtime, cfg.WorkspaceDir); err != nil {
			return err
		}

		svc := &gateway.Service{
			Session:         runtime.Session(),
			Store:           database,
			DefaultTimezone: cfg.DefaultTimezone,
			AdminChatID:     cfg.AdminChatID,
		}

		conn, err := telegram.NewConnector(cfg.TelegramBotToken, database)
		if err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		mode := telegram.RunModePolling
		if cfg.TelegramMode == "webhook" {
			mode = telegram.RunModeWebhook
		}

		fmt.Fprintf(os.Stderr, "Starting Telegram gateway (mode=%s)\n", mode)
		return conn.Run(ctx, svc, mode, telegram.WebhookOptions{
			PublicURL:  cfg.TelegramWebhookURL,
			ListenAddr: cfg.TelegramWebhookAddr,
			Path:       cfg.TelegramWebhookPath,
		})
	},
}

var gatewaySlackCmd = &cobra.Command{
	Use:   "slack",
	Short: "Run the Slack gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()

		if cfg.SlackBotToken == "" {
			return fmt.Errorf("GOAT_SLACK_BOT_TOKEN is required")
		}
		if cfg.SlackAppToken == "" {
			return fmt.Errorf("GOAT_SLACK_APP_TOKEN is required")
		}
		if cfg.SlackChannelID == "" {
			return fmt.Errorf("GOAT_SLACK_CHANNEL_ID is required")
		}

		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		runtime, err := runtimepkg.New(cfg)
		if err != nil {
			return err
		}
		startupCtx, startupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer startupCancel()
		if err := runtimepkg.Validate(startupCtx, runtime, cfg.WorkspaceDir); err != nil {
			return err
		}

		svc := &gateway.Service{
			Session:         runtime.Session(),
			Store:           database,
			DefaultTimezone: cfg.DefaultTimezone,
			AdminChatID:     cfg.AdminChatID,
		}

		conn, err := slackpkg.NewConnector(cfg.SlackBotToken, cfg.SlackAppToken, cfg.SlackChannelID, database)
		if err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		fmt.Fprintln(os.Stderr, "Starting Slack gateway (socket mode)")
		return conn.Run(ctx, svc)
	},
}

func init() {
	gatewayCmd.AddCommand(gatewayTelegramCmd)
	gatewayCmd.AddCommand(gatewaySlackCmd)
	rootCmd.AddCommand(gatewayCmd)
}
