package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/agent"
	"goated/internal/app"
	cronpkg "goated/internal/cron"
	"goated/internal/db"
	"goated/internal/gateway"
	"goated/internal/logging"
	"goated/internal/msglog"
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

const maxReplayAge = 1 * time.Hour

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
		if err := ensureSelfRepo(cfg.WorkspaceDir); err != nil {
			return err
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
			pid, err := startDetachedDaemon(exe, logPath)
			if err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}
			fmt.Printf("goated daemon started (pid=%s, log=%s)\n", pid, logPath)
			return nil
		}

		// We are the daemon child — run in foreground
		if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		defer os.Remove(pidPath)
		socketPath := filepath.Join(cfg.LogDir, "goated.sock")
		_ = os.Remove(socketPath)
		defer os.Remove(socketPath)

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

		// Initialize message logger
		msgLogger, err := msglog.NewLogger(cfg.LogDir, cfg.WorkspaceDir, cfg.DefaultTimezone)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] warning: message logger init failed: %v\n",
				time.Now().Format(time.RFC3339), err)
			// Non-fatal — continue without message logging
		}

		// Initialize session file with current session ID (if one exists)
		if msgLogger != nil {
			sessionIDPath := filepath.Join(cfg.LogDir, "claude_session", "session_id")
			if data, err := os.ReadFile(sessionIDPath); err == nil {
				sid := strings.TrimSpace(string(data))
				if sid != "" {
					msgLogger.SessionManager().NewSession(sid)
				}
			}
		}

		// Replay stuck messages from previous run
		if msgLogger != nil {
			go func() {
				stuck, err := msglog.FindStuckMessages(msgLogger)
				if err != nil || len(stuck) == 0 {
					return
				}
				now := time.Now()
				recent, stale := msglog.FilterRecentStuckMessages(stuck, now, maxReplayAge)
				for _, sm := range stale {
					age := now.Sub(time.Unix(sm.Entry.TSUnix, 0)).Round(time.Second)
					fmt.Fprintf(os.Stderr, "[%s] skipping stale stuck message %s (age=%s)\n",
						time.Now().Format(time.RFC3339), sm.RequestID, age)
				}
				if len(recent) == 0 {
					return
				}
				fmt.Fprintf(os.Stderr, "[%s] found %d recent stuck message(s) <= %s, replaying...\n",
					time.Now().Format(time.RFC3339), len(recent), maxReplayAge)
				replayCtx, replayCancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer replayCancel()
				msglog.ReplayStuckMessages(replayCtx, msgLogger, runtime.Session(), recent)
			}()
		}

		drainCtx, drainCancel := context.WithCancel(context.Background())
		defer drainCancel()

		svc := &gateway.Service{
			Session:         runtime.Session(),
			Store:           store,
			DefaultTimezone: cfg.DefaultTimezone,
			AdminChatID:     cfg.AdminChatID,
			MsgLogger:       msgLogger,
			SessionIDPath:   filepath.Join(cfg.LogDir, "claude_session", "session_id"),
			DrainCtx:        drainCtx,
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		// Start daily re-redaction goroutine
		go runReRedact(ctx, cfg.LogDir, cfg.WorkspaceDir, cfg.DefaultTimezone)

		// Start log retention loop (cleans up old log files)
		go logging.StartRetentionLoop(ctx, logging.RetentionConfig{
			DaemonLogDir: cfg.LogDir,
			CronLogDir:   filepath.Join(cfg.LogDir, "cron", "jobs"),
			MaxAgeDays:   cfg.LogRetentionDays,
			MaxFiles:     cfg.LogRetentionMaxFiles,
			Compress:     cfg.LogRetentionCompress,
			Interval:     6 * time.Hour,
		})

		var runGateway func() error
		var responder gateway.Responder

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
			responder = conn

			// Wire interactive event router (buttons, menus) into the connector.
			iRouter := slackpkg.NewInteractionRouter(conn, svc)
			conn.SetInteractionHandler(iRouter.Handle)

			runner := &cronpkg.Runner{
				Store:        store,
				WorkspaceDir: cfg.WorkspaceDir,
				LogDir:       cfg.LogDir,
				Notifier:     cronNoticeNotifier{responder: conn, session: runtime.Session(), channel: cfg.Gateway},
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

			conn, err := telegram.NewConnector(cfg.TelegramBotToken, store, telegram.AttachmentConfig{
				RootPath:      cfg.TelegramAttachmentsRoot,
				MaxBytes:      cfg.TelegramAttachmentMaxBytes,
				MaxTotalBytes: cfg.TelegramAttachmentMaxTotalBytes,
			})
			if err != nil {
				return fmt.Errorf("init telegram: %w", err)
			}
			responder = conn

			runner := &cronpkg.Runner{
				Store:        store,
				WorkspaceDir: cfg.WorkspaceDir,
				LogDir:       cfg.LogDir,
				Notifier:     cronNoticeNotifier{responder: conn, session: runtime.Session(), channel: cfg.Gateway},
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

		if responder != nil {
			go runDaemonSocket(ctx, socketPath, responder, runtime.Session(), msgLogger, cfg.Gateway)
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

type daemonSendRequest struct {
	RequestID  string          `json:"request_id"`
	ChatID     string          `json:"chat_id"`
	ThreadTS   string          `json:"thread_ts,omitempty"`
	Text       string          `json:"text,omitempty"`
	FilePath   string          `json:"file_path,omitempty"`
	Caption    string          `json:"caption,omitempty"`
	MediaType  string          `json:"media_type,omitempty"`
	Source     string          `json:"source,omitempty"`
	LogPath    string          `json:"log_path,omitempty"`
	BlocksJSON json.RawMessage `json:"blocks_json,omitempty"`
}

type daemonSendResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func runDaemonSocket(ctx context.Context, socketPath string, responder gateway.Responder, session agent.SessionRuntime, logger *msglog.Logger, gatewayName string) {
	for {
		if ctx.Err() != nil {
			return
		}
		runDaemonSocketOnce(ctx, socketPath, responder, session, logger, gatewayName)
		// If we get here, the listener died — wait and retry
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "[%s] daemon socket died, restarting in 3s...\n", time.Now().Format(time.RFC3339))
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func runDaemonSocketOnce(ctx context.Context, socketPath string, responder gateway.Responder, session agent.SessionRuntime, logger *msglog.Logger, gatewayName string) {
	_ = os.Remove(socketPath)
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] daemon socket listen failed: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}
	defer ln.Close()
	_ = os.Chmod(socketPath, 0o600)

	// Monitor socket file existence — if deleted, close listener to trigger restart
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = ln.Close()
				return
			case <-ticker.C:
				if _, err := os.Stat(socketPath); os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "[%s] daemon socket file disappeared, closing listener\n", time.Now().Format(time.RFC3339))
					_ = ln.Close()
					return
				}
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Listener was closed (e.g. socket file disappeared) — return to trigger retry
			fmt.Fprintf(os.Stderr, "[%s] daemon socket accept failed: %v\n", time.Now().Format(time.RFC3339), err)
			return
		}
		go handleDaemonSocketConn(ctx, conn, responder, session, logger, gatewayName)
	}
}

func handleDaemonSocketConn(ctx context.Context, conn net.Conn, responder gateway.Responder, session agent.SessionRuntime, logger *msglog.Logger, gatewayName string) {
	defer conn.Close()

	var req daemonSendRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(daemonSendResponse{OK: false, Error: "invalid request"})
		return
	}
	if strings.TrimSpace(req.ChatID) == "" {
		_ = json.NewEncoder(conn).Encode(daemonSendResponse{OK: false, Error: "chat_id is required"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	req.FilePath = strings.TrimSpace(req.FilePath)
	req.Caption = strings.TrimSpace(req.Caption)
	req.MediaType = strings.TrimSpace(req.MediaType)
	req.ThreadTS = strings.TrimSpace(req.ThreadTS)
	req.Source = strings.TrimSpace(req.Source)
	req.LogPath = strings.TrimSpace(req.LogPath)
	if req.Text == "" && req.FilePath == "" && len(req.BlocksJSON) == 0 {
		_ = json.NewEncoder(conn).Encode(daemonSendResponse{OK: false, Error: "text, file_path, or blocks_json is required"})
		return
	}
	if req.FilePath != "" && (req.Text != "" || len(req.BlocksJSON) > 0) {
		_ = json.NewEncoder(conn).Encode(daemonSendResponse{OK: false, Error: "file_path is mutually exclusive with text/blocks_json"})
		return
	}

	if logger != nil {
		logText := req.Text
		if req.FilePath != "" {
			logText = req.Caption
			if logText == "" {
				logText = fmt.Sprintf("[media:%s] %s", withDefault(req.MediaType, "auto"), req.FilePath)
			}
		}
		logger.LogAgentResponse(req.RequestID, msglog.AgentResponseData{
			ChatID:  req.ChatID,
			Gateway: gatewayName,
			Text:    logText,
			TextLen: len(logText),
		}, msglog.StatusPending, "")
	}

	var sendErr error
	if req.FilePath != "" {
		mediaResponder, ok := responder.(gateway.MediaResponder)
		if !ok {
			sendErr = fmt.Errorf("gateway %s does not support outbound media yet", gatewayName)
		} else {
			sendErr = mediaResponder.SendMedia(ctx, req.ChatID, req.FilePath, req.Caption, req.MediaType)
		}
	} else if len(req.BlocksJSON) > 0 {
		blockResponder, ok := responder.(gateway.BlockResponder)
		if !ok {
			sendErr = fmt.Errorf("gateway %s does not support block messages", gatewayName)
		} else if req.ThreadTS != "" {
			sendErr = blockResponder.SendThreadBlockMessage(ctx, req.ChatID, req.ThreadTS, req.Text, req.BlocksJSON)
		} else {
			sendErr = blockResponder.SendBlockMessage(ctx, req.ChatID, req.Text, req.BlocksJSON)
		}
	} else if req.ThreadTS != "" {
		threadedResponder, ok := responder.(gateway.ThreadedResponder)
		if !ok {
			sendErr = fmt.Errorf("gateway %s does not support threaded messages", gatewayName)
		} else {
			sendErr = threadedResponder.SendThreadMessage(ctx, req.ChatID, req.ThreadTS, req.Text)
		}
	} else {
		sendErr = responder.SendMessage(ctx, req.ChatID, req.Text)
	}
	if sendErr != nil {
		if logger != nil {
			logText := req.Text
			if req.FilePath != "" {
				logText = req.Caption
				if logText == "" {
					logText = fmt.Sprintf("[media:%s] %s", withDefault(req.MediaType, "auto"), req.FilePath)
				}
			}
			logger.LogAgentResponse(req.RequestID, msglog.AgentResponseData{
				ChatID:  req.ChatID,
				Gateway: gatewayName,
				Text:    logText,
				TextLen: len(logText),
			}, msglog.StatusFailed, sendErr.Error())
		}
		_ = json.NewEncoder(conn).Encode(daemonSendResponse{OK: false, Error: sendErr.Error()})
		return
	}
	if logger != nil {
		logger.UpdateStatus(req.RequestID, msglog.EntryAgentResponse, msglog.StatusSent)
	}
	if err := json.NewEncoder(conn).Encode(daemonSendResponse{OK: true}); err != nil {
		return
	}
	if mirrorErr := maybeMirrorSystemNotice(ctx, session, gatewayName, req); mirrorErr != nil {
		fmt.Fprintf(os.Stderr, "[%s] mirror system notice failed: %v\n", time.Now().Format(time.RFC3339), mirrorErr)
	}
}

type cronNoticeNotifier struct {
	responder gateway.Responder
	session   agent.SessionRuntime
	channel   string
}

func (n cronNoticeNotifier) SendMessage(ctx context.Context, chatID, text string) error {
	if err := n.responder.SendMessage(ctx, chatID, text); err != nil {
		return err
	}
	req := daemonSendRequest{
		ChatID:  chatID,
		Text:    text,
		Source:  "cron",
		LogPath: "",
	}
	if err := maybeMirrorSystemNotice(ctx, n.session, n.channel, req); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] mirror cron notice failed: %v\n", time.Now().Format(time.RFC3339), err)
	}
	return nil
}

func maybeMirrorSystemNotice(ctx context.Context, session agent.SessionRuntime, channel string, req daemonSendRequest) error {
	if session == nil {
		return nil
	}
	// Main-session runtime replies already originate from the active session and
	// should not be mirrored back into that same session. Doing so can deadlock:
	// the runtime waits for send_user_message to return while the daemon waits
	// for the same runtime to accept the mirrored notice.
	if strings.TrimSpace(req.Source) == "" {
		return nil
	}
	sender, ok := session.(agent.SystemNoticeSender)
	if !ok {
		return nil
	}

	message := req.Text
	if req.FilePath != "" {
		message = req.Caption
		if message == "" {
			message = fmt.Sprintf("[media:%s] %s", withDefault(req.MediaType, "auto"), req.FilePath)
		}
	}
	noticeSource := req.Source
	if noticeSource == "" {
		noticeSource = "assistant_reply"
	}
	metadata := map[string]string{
		"source": noticeSource,
		"mirror": "true",
	}
	if req.LogPath != "" {
		metadata["log_path"] = req.LogPath
	}
	if req.FilePath != "" {
		metadata["file_path"] = req.FilePath
		metadata["media_type"] = withDefault(req.MediaType, "auto")
	}
	return sender.SendSystemNotice(ctx, channel, req.ChatID, noticeSource, message, metadata)
}

func ensureSelfRepo(workspaceDir string) error {
	return ensureSelfRepoExists(workspaceDir)
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
			fmt.Sprintf(
				"sleep 15 && if [ -f %q ] && pid=$(cat %q 2>/dev/null) && [ -n \"$pid\" ] && kill -0 \"$pid\" 2>/dev/null; then exit 0; fi; %s daemon run >> %s 2>&1 || true",
				pidPath, pidPath, goatedBin, filepath.Join(cfg.LogDir, "goated_daemon.log"),
			))
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

		logPath := filepath.Join(cfg.LogDir, "goated_daemon.log")
		pid, err := startDetachedDaemon(goatedBin, logPath)
		if err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		rec.NewPID = pid
		fmt.Printf("goated daemon started (pid=%s, log=%s)\n", pid, logPath)

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

func startDetachedDaemon(exePath, logPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir log dir: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(exePath, "daemon", "run")
	cmd.Env = append(os.Environ(), "_GOATED_DAEMON=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	return strconv.Itoa(cmd.Process.Pid), nil
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

// runReRedact re-redacts recent log files on startup and then hourly.
// On startup it scrubs yesterday + today (date-based) and the last 10 session
// files. Every hour it scrubs the 2 most recent files of every log type
// (daily, audit, sessions, hooks, runs), locking each file appropriately
// since one may be actively written to.
func runReRedact(ctx context.Context, logDir, workspaceDir, timezone string) {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		tz = time.FixedZone("UTC", 0)
	}

	// Startup: broad scrub of yesterday + today + recent sessions
	yesterday := time.Now().In(tz).AddDate(0, 0, -1).Format("2006-01-02")
	msglog.ReRedactDate(logDir, workspaceDir, yesterday)
	msglog.ReRedactDate(logDir, workspaceDir, time.Now().In(tz).Format("2006-01-02"))
	msglog.ReRedactRecentSessions(logDir, workspaceDir, 10)

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msglog.ReRedactRecentAll(logDir, workspaceDir, 2)
		}
	}
}

func init() {
	daemonRestartCmd.Flags().String("reason", "", "reason for restarting (required)")
	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
}
