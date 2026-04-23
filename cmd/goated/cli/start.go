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
	cronpkg "goated/internal/cron"
	"goated/internal/db"
	"goated/internal/gateway"
	runtimepkg "goated/internal/runtime"
	slackpkg "goated/internal/slack"
	"goated/internal/telegram"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the gateway and cron scheduler (foreground, no PID file)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		runtime, err := runtimepkg.New(cfg)
		if err != nil {
			return err
		}
		startupCtx, startupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer startupCancel()
		if err := runtimepkg.Validate(startupCtx, runtime, cfg.WorkspaceDir); err != nil {
			return err
		}

		drainCtx, drainCancel := context.WithCancel(context.Background())
		defer drainCancel()

		svc := &gateway.Service{
			Session:         runtime.Session(),
			Store:           store,
			DefaultTimezone: cfg.DefaultTimezone,
			AdminChatID:     cfg.AdminChatID,
			DrainCtx:        drainCtx,
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		var runGateway func() error

		switch cfg.Gateway {
		case "slack":
			if cfg.SlackBotToken == "" {
				return fmt.Errorf("GOAT_SLACK_BOT_TOKEN is required")
			}
			if cfg.SlackAppToken == "" {
				return fmt.Errorf("GOAT_SLACK_APP_TOKEN is required")
			}
			if cfg.SlackChannelID == "" {
				return fmt.Errorf("GOAT_SLACK_CHANNEL_ID is required")
			}

			conn, err := slackpkg.NewConnector(cfg.SlackBotToken, cfg.SlackAppToken, cfg.SlackChannelID, store, slackpkg.AttachmentConfig{
				RootPath:      cfg.SlackAttachmentsRoot,
				MaxBytes:      cfg.SlackAttachmentMaxBytes,
				MaxTotalBytes: cfg.SlackAttachmentMaxTotalBytes,
				MaxParallel:   cfg.SlackAttachmentMaxParallel,
			})
			if err != nil {
				return err
			}

			runner := &cronpkg.Runner{
				Store:        store,
				WorkspaceDir: cfg.WorkspaceDir,
				LogDir:       cfg.LogDir,
				Notifier:     conn,
				Headless:     runtime.Headless(),
			}
			go runCronTicker(ctx, runner)

			runGateway = func() error {
				fmt.Fprintln(os.Stderr, "Starting goated (gateway=slack, cron=1m ticker)")
				return conn.Run(ctx, svc)
			}

		default: // "telegram"
			if cfg.TelegramBotToken == "" {
				return fmt.Errorf("GOAT_TELEGRAM_BOT_TOKEN is required")
			}
			if len(cfg.TelegramAllowedChatIDs) == 0 {
				return fmt.Errorf("telegram.allowed_chat_ids is required (configure via `goated channel allow <channel> <chat-id>`)")
			}
			allowedIDs, err := cfg.ParsedTelegramAllowedChatIDs()
			if err != nil {
				return err
			}

			conn, err := telegram.NewConnector(cfg.TelegramBotToken, allowedIDs, store, telegram.AttachmentConfig{
				RootPath:      cfg.TelegramAttachmentsRoot,
				MaxBytes:      cfg.TelegramAttachmentMaxBytes,
				MaxTotalBytes: cfg.TelegramAttachmentMaxTotalBytes,
			})
			if err != nil {
				return err
			}
			conn.SetRespondAll(cfg.TelegramGroupRespondAll)

			runner := &cronpkg.Runner{
				Store:        store,
				WorkspaceDir: cfg.WorkspaceDir,
				LogDir:       cfg.LogDir,
				Notifier:     conn,
				Headless:     runtime.Headless(),
			}
			go runCronTicker(ctx, runner)

			mode := telegram.RunModePolling
			if cfg.TelegramMode == "webhook" {
				mode = telegram.RunModeWebhook
			}

			runGateway = func() error {
				fmt.Fprintf(os.Stderr, "Starting goated (gateway=%s, cron=1m ticker)\n", mode)
				return conn.Run(ctx, svc, mode, telegram.WebhookOptions{
					PublicURL:  cfg.TelegramWebhookURL,
					ListenAddr: cfg.TelegramWebhookAddr,
					Path:       cfg.TelegramWebhookPath,
				})
			}
		}

		if err := runGateway(); err != nil && err != context.Canceled {
			return err
		}

		fmt.Fprintln(os.Stderr, "Shutting down, waiting for in-flight messages...")
		svc.WaitInflight()
		fmt.Fprintln(os.Stderr, "All messages flushed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
