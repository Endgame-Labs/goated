package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
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

type restartRecord struct {
	Timestamp string `json:"timestamp"`
	OldPID    int    `json:"old_pid,omitempty"`
	NewPID    string `json:"new_pid,omitempty"`
	Reason    string `json:"reason"`
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the goated daemon",
}

// daemonRunCmd is the actual daemon process — runs in the foreground, writes a
// PID file, and handles graceful shutdown. Intended to be launched by
// "daemon restart" (backgrounded via nohup) or the watchdog cron.
var daemonRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the daemon in the foreground (used by restart/watchdog)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		pidPath := filepath.Join(cfg.LogDir, "goated_daemon.pid")

		if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
			return fmt.Errorf("mkdir log dir: %w", err)
		}

		// Refuse to start if another daemon is running
		if existingPID := readExistingPID(pidPath); existingPID > 0 {
			return fmt.Errorf("daemon already running (pid=%d). Use: ./goated daemon restart --reason \"...\"", existingPID)
		}

		// If not the daemon child, run pre-flight checks before backgrounding.
		// This surfaces errors in the user's terminal instead of burying them
		// in the log file where they'd be invisible.
		if os.Getenv("_GOATED_DAEMON") != "1" {
			if failures := runPreflightChecks(cfg); len(failures) > 0 {
				fmt.Fprintf(os.Stderr, "Pre-flight check failed:\n")
				for _, f := range failures {
					fmt.Fprintf(os.Stderr, "  FAIL  %s: %s\n", f.Name, f.Detail)
					if f.FixHint != "" {
						fmt.Fprintf(os.Stderr, "        fix: %s\n", f.FixHint)
					}
				}
				fmt.Fprintf(os.Stderr, "\nRun ./goated doctor for full diagnostics.\n")
				return fmt.Errorf("pre-flight failed (%d issue(s))", len(failures))
			}

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}
			logPath := filepath.Join(cfg.LogDir, "goated_daemon.log")
			shell := fmt.Sprintf(
				`_GOATED_DAEMON=1 nohup %q daemon run >> %q 2>&1 & echo $!`,
				exe, logPath,
			)
			cmd := exec.Command("sh", "-c", shell)
			cmd.Env = os.Environ()
			out, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}
			pid := stripNL(string(out))
			fmt.Printf("goated daemon started (pid=%s, log=%s)\n", pid, logPath)
			return nil
		}

		// We are the daemon child — run in foreground
		if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		defer os.Remove(pidPath)

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer store.Close()

		runtime, err := runtimepkg.New(cfg)
		if err != nil {
			return fmt.Errorf("init runtime: %w", err)
		}
		startupCtx, startupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer startupCancel()
		if err := runtimepkg.Validate(startupCtx, runtime, cfg.WorkspaceDir); err != nil {
			return fmt.Errorf("runtime validation: %w", err)
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
				return fmt.Errorf("init slack: %w", err)
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
				fmt.Fprintf(os.Stderr, "[%s] goated daemon running (pid=%d, gateway=slack)\n",
					time.Now().Format(time.RFC3339), os.Getpid())
				return conn.Run(ctx, svc)
			}

		default: // "telegram"
			if cfg.TelegramBotToken == "" {
				return fmt.Errorf("GOAT_TELEGRAM_BOT_TOKEN is required")
			}

			conn, err := telegram.NewConnector(cfg.TelegramBotToken, store)
			if err != nil {
				return fmt.Errorf("init telegram: %w", err)
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
				fmt.Fprintf(os.Stderr, "[%s] goated daemon running (pid=%d, gateway=%s)\n",
					time.Now().Format(time.RFC3339), os.Getpid(), mode)
				return conn.Run(ctx, svc, mode, telegram.WebhookOptions{
					PublicURL:  cfg.TelegramWebhookURL,
					ListenAddr: cfg.TelegramWebhookAddr,
					Path:       cfg.TelegramWebhookPath,
				})
			}
		}

		if err := runGateway(); err != nil && err != context.Canceled {
			return fmt.Errorf("gateway: %w", err)
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
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the daemon with a logged reason",
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		if reason == "" {
			return fmt.Errorf("--reason is required")
		}

		cfg := app.LoadConfig()
		pidPath := filepath.Join(cfg.LogDir, "goated_daemon.pid")
		restartLog := filepath.Join(cfg.LogDir, "restarts.jsonl")

		if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
			return fmt.Errorf("mkdir log dir: %w", err)
		}

		rec := restartRecord{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Reason:    reason,
		}

		// Resolve the goated binary (ourselves)
		goatedBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		if abs, err := filepath.Abs(goatedBin); err == nil {
			goatedBin = abs
		}

		// Wait for in-flight subagents before stopping
		if store, err := db.Open(cfg.DBPath); err == nil {
			waitForSubagents(store)
			store.Close()
		}

		// Spawn a guardian process that will start the daemon if the restart
		// command itself gets killed (e.g. agent process interrupted).
		// "daemon run" is safe to call redundantly — it exits if one is already running.
		guardianCmd := exec.Command("sh", "-c",
			fmt.Sprintf("sleep 15 && %s daemon run >> %s 2>&1 || true",
				goatedBin, filepath.Join(cfg.LogDir, "goated_daemon.log")))
		guardianCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := guardianCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to start restart guardian: %v\n", err)
		} else {
			fmt.Printf("Started restart guardian (pid=%d) as safety net\n", guardianCmd.Process.Pid)
		}

		// Stop existing daemon gracefully
		if oldPID, err := stopDaemon(pidPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else if oldPID > 0 {
			rec.OldPID = oldPID
			fmt.Printf("Stopped daemon (pid=%d)\n", oldPID)
		} else {
			fmt.Println("No running daemon found.")
		}

		out, err := exec.Command(goatedBin, "daemon", "run").Output()
		if err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		rec.NewPID = stripNL(string(out))
		fmt.Print(string(out))

		// Append restart record
		if err := appendRestartRecord(restartLog, rec); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write restart log: %v\n", err)
		}

		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		pidPath := filepath.Join(cfg.LogDir, "goated_daemon.pid")

		pid, err := stopDaemon(pidPath)
		if err != nil {
			return err
		}
		if pid == 0 {
			fmt.Println("No running daemon found.")
		} else {
			fmt.Printf("Stopped daemon (pid=%d)\n", pid)
		}
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status and recent restarts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		pidPath := filepath.Join(cfg.LogDir, "goated_daemon.pid")
		restartLog := filepath.Join(cfg.LogDir, "restarts.jsonl")

		// Check if running
		if pid, running := readPID(pidPath); running {
			fmt.Printf("Daemon running (pid=%d)\n", pid)
		} else if pid > 0 {
			fmt.Printf("Daemon not running (stale pid=%d)\n", pid)
		} else {
			fmt.Println("Daemon not running (no pid file)")
		}

		// Show recent restarts
		data, err := os.ReadFile(restartLog)
		if err != nil {
			return nil // no restart log yet
		}
		lines := splitLines(string(data))
		start := 0
		if len(lines) > 10 {
			start = len(lines) - 10
		}
		if len(lines) > 0 {
			fmt.Printf("\nRecent restarts (last %d):\n", len(lines)-start)
			for _, line := range lines[start:] {
				var rec restartRecord
				if json.Unmarshal([]byte(line), &rec) == nil {
					fmt.Printf("  %s  pid %d→%s  %s\n", rec.Timestamp, rec.OldPID, rec.NewPID, rec.Reason)
				}
			}
		}
		return nil
	},
}

// readExistingPID returns the PID of a running daemon, or 0 if none.
func readExistingPID(pidPath string) int {
	pid, running := readPID(pidPath)
	if running {
		return pid
	}
	return 0
}

// waitForSubagents checks for running subagents and waits for them to finish.
func waitForSubagents(store *db.Store) {
	running, err := store.RunningSubagents()
	if err != nil || len(running) == 0 {
		return
	}

	// Filter to actually-alive processes
	var alive []db.SubagentRun
	for _, r := range running {
		proc, err := os.FindProcess(r.PID)
		if err != nil {
			continue
		}
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process already exited — mark it done
			_ = store.RecordSubagentFinish(r.ID, "ok")
			continue
		}
		alive = append(alive, r)
	}

	if len(alive) == 0 {
		return
	}

	fmt.Printf("Waiting for %d in-flight subagent(s) to finish...\n", len(alive))
	for _, r := range alive {
		fmt.Printf("  pid=%d source=%s log=%s\n", r.PID, r.Source, r.LogPath)
	}

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		allDone := true
		for _, r := range alive {
			proc, err := os.FindProcess(r.PID)
			if err != nil {
				continue
			}
			if err := proc.Signal(syscall.Signal(0)); err == nil {
				allDone = false
				break
			}
		}
		if allDone {
			fmt.Println("All subagents finished.")
			return
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintln(os.Stderr, "Subagent wait timeout (3m), proceeding with restart.")
}

// stopDaemon sends SIGTERM and waits for the process to exit (up to 3 minutes
// to allow in-flight messages to flush). Returns the old PID (0 if none).
func stopDaemon(pidPath string) (int, error) {
	pid, running := readPID(pidPath)
	if !running {
		return 0, nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, nil
	}

	// Graceful: SIGTERM first — daemon will drain in-flight messages
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return pid, fmt.Errorf("sending SIGTERM to pid %d: %w", pid, err)
	}

	fmt.Printf("Sent SIGTERM to daemon (pid=%d), waiting for in-flight messages to flush...\n", pid)

	// Wait up to 3 minutes for graceful exit (allows message flush)
	for i := 0; i < 1800; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			_ = os.Remove(pidPath)
			return pid, nil
		}
	}

	// Force kill if still alive after 3 minutes
	fmt.Fprintf(os.Stderr, "Daemon (pid=%d) didn't stop after 3m, sending SIGKILL\n", pid)
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	_ = os.Remove(pidPath)
	return pid, nil
}

// readPID reads the pid file and checks if the process is alive.
func readPID(pidPath string) (int, bool) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(stripNL(string(data)))
	if err != nil {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	return pid, true
}

func appendRestartRecord(path string, rec restartRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(rec)
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range splitOn(s, '\n') {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func splitOn(s string, sep byte) []string {
	var result []string
	for {
		i := indexOf(s, sep)
		if i < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:i])
		s = s[i+1:]
	}
	return result
}

func indexOf(s string, b byte) int {
	for i := range s {
		if s[i] == b {
			return i
		}
	}
	return -1
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

func stripNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func init() {
	daemonRestartCmd.Flags().String("reason", "", "reason for restarting (required)")
	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
}
