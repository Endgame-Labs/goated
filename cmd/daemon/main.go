package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"goated/internal/app"
	cronpkg "goated/internal/cron"
	"goated/internal/db"
	"goated/internal/gateway"
	runtimepkg "goated/internal/runtime"
	slackpkg "goated/internal/slack"
	"goated/internal/telegram"
)

func main() {
	cfg := app.LoadConfig()
	pidPath := filepath.Join(cfg.LogDir, "goated_daemon.pid")
	logPath := filepath.Join(cfg.LogDir, "goated_daemon.log")

	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		fatal("mkdir log dir: %v", err)
	}

	// Refuse to start if another daemon is running — use `./run daemon restart --reason "..."` instead
	if existingPID := readExistingPID(pidPath); existingPID > 0 {
		fatal("daemon already running (pid=%d). Use: ./goated daemon restart --reason \"...\"", existingPID)
	}

	// If not the daemon child, re-exec backgrounded via shell nohup
	if os.Getenv("_GOATED_DAEMON") != "1" {
		exe, err := os.Executable()
		if err != nil {
			fatal("resolve executable: %v", err)
		}
		// Use shell to nohup + background so the child inherits a usable environment
		shell := fmt.Sprintf(
			`_GOATED_DAEMON=1 nohup %q >> %q 2>&1 & echo $!`,
			exe, logPath,
		)
		cmd := exec.Command("sh", "-c", shell)
		cmd.Env = os.Environ()
		out, err := cmd.Output()
		if err != nil {
			fatal("start daemon: %v", err)
		}
		pid := stripNewline(string(out))
		fmt.Printf("goated_daemon started (pid=%s, log=%s)\n", pid, logPath)
		return
	}

	// We are the daemon child
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		fatal("write pid file: %v", err)
	}
	defer os.Remove(pidPath)

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		fatal("open db: %v", err)
	}
	defer store.Close()

	runtime, err := runtimepkg.New(cfg)
	if err != nil {
		fatal("init runtime: %v", err)
	}
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer startupCancel()
	if err := runtimepkg.Validate(startupCtx, runtime, cfg.WorkspaceDir); err != nil {
		fatal("runtime validation: %v", err)
	}

	// drainCtx stays alive during shutdown so in-flight handlers can finish
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
			fatal("GOAT_SLACK_BOT_TOKEN is required")
		}
		if cfg.SlackAppToken == "" {
			fatal("GOAT_SLACK_APP_TOKEN is required")
		}
		if cfg.SlackChannelID == "" {
			fatal("GOAT_SLACK_CHANNEL_ID is required")
		}

		conn, err := slackpkg.NewConnector(cfg.SlackBotToken, cfg.SlackAppToken, cfg.SlackChannelID, store)
		if err != nil {
			fatal("init slack: %v", err)
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
			fmt.Fprintf(os.Stderr, "[%s] goated_daemon running (pid=%d, gateway=slack)\n",
				time.Now().Format(time.RFC3339), os.Getpid())
			return conn.Run(ctx, svc)
		}

	default: // "telegram"
		if cfg.TelegramBotToken == "" {
			fatal("GOAT_TELEGRAM_BOT_TOKEN is required")
		}

		conn, err := telegram.NewConnector(cfg.TelegramBotToken, store)
		if err != nil {
			fatal("init telegram: %v", err)
		}

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
			fmt.Fprintf(os.Stderr, "[%s] goated_daemon running (pid=%d, gateway=%s)\n",
				time.Now().Format(time.RFC3339), os.Getpid(), mode)
			return conn.Run(ctx, svc, mode, telegram.WebhookOptions{
				PublicURL:  cfg.TelegramWebhookURL,
				ListenAddr: cfg.TelegramWebhookAddr,
				Path:       cfg.TelegramWebhookPath,
			})
		}
	}

	if err := runGateway(); err != nil && err != context.Canceled {
		fatal("gateway: %v", err)
	}

	// Wait for in-flight message handlers to finish before exiting
	fmt.Fprintf(os.Stderr, "[%s] shutting down, waiting for in-flight messages...\n",
		time.Now().Format(time.RFC3339))
	done := make(chan struct{})
	go func() {
		svc.WaitInflight()
		close(done)
	}()
	select {
	case <-done:
		fmt.Fprintf(os.Stderr, "[%s] all messages flushed, exiting\n",
			time.Now().Format(time.RFC3339))
	case <-time.After(2 * time.Minute):
		fmt.Fprintf(os.Stderr, "[%s] flush timeout (2m), exiting anyway\n",
			time.Now().Format(time.RFC3339))
	}
}

// readExistingPID returns the PID of a running daemon, or 0 if none.
func readExistingPID(pidPath string) int {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(stripNewline(string(data)))
	if err != nil {
		return 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return 0 // stale pid file
	}
	return pid
}

func runCronTicker(ctx context.Context, runner *cronpkg.Runner) {
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Until(next)):
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	runCronOnce(ctx, runner)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCronOnce(ctx, runner)
		}
	}
}

func runCronOnce(ctx context.Context, runner *cronpkg.Runner) {
	if err := runner.Run(ctx, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] cron error: %v\n", time.Now().Format(time.RFC3339), err)
	}
}

func stripNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "goated_daemon: "+format+"\n", args...)
	os.Exit(1)
}
