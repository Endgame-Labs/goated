package subagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"goated/internal/db"
	"goated/internal/tmux"
)

// BuildPrompt constructs the prompt for a headless subagent.
// preamble is an optional prefix (e.g. "Read CRON.md before executing.").
// chatID, if non-empty, adds send_user_message instructions.
// source and logPath are passed through so the main session gets context.
func BuildPrompt(preamble, userPrompt, chatID, source, logPath string) string {
	var b strings.Builder
	if preamble != "" {
		b.WriteString(preamble)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(userPrompt))
	b.WriteString("\n")
	if chatID != "" {
		b.WriteString("\nSend your response to the user by piping markdown into:\n")
		sendCmd := fmt.Sprintf("  ./goat send_user_message --chat %s", chatID)
		if source != "" {
			sendCmd += fmt.Sprintf(" --source %s", source)
		}
		if logPath != "" {
			sendCmd += fmt.Sprintf(" --log %s", logPath)
		}
		b.WriteString(sendCmd + "\n")
		b.WriteString("\nSee GOATED_CLI_README.md for formatting details.\n")
	}
	return b.String()
}

// RunOpts configures a subagent run.
type RunOpts struct {
	WorkspaceDir string
	Prompt       string
	LogPath      string
	Source       string // "cron", "cli", "gateway"
	CronID       uint64 // only for cron-sourced runs
	ChatID       string
	Silent       bool // suppress success notifications to main session
	SessionName  string
	Runtime      db.ExecutionRuntime
}

type Result struct {
	PID             int
	Status          string
	Output          []byte
	RuntimeProvider string
	RuntimeMode     string
	RuntimeVersion  string
}

// handleCompletion records the subagent's final status and notifies the main
// interactive runtime session. Shared by RunSync and RunBackground.
func handleCompletion(store *db.Store, runID uint64, runErr error, opts RunOpts) {
	status := "ok"
	if runErr != nil {
		status = "error"
	}
	if store != nil && runID > 0 {
		_ = store.RecordSubagentFinish(runID, status)
	}
	// Silent crons skip notification on success; errors always notify.
	if opts.Silent && status == "ok" {
		return
	}
	notifyMainSession(opts, status)
}

// notifyMainSession pastes a <subagent-notification> into the configured tmux
// session so the main interactive runtime knows a job finished.
func notifyMainSession(opts RunOpts, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessionName := opts.SessionName
	if sessionName == "" {
		sessionName = "goat_claude_main"
	}

	if !tmux.SessionExistsFor(ctx, sessionName) {
		return
	}

	logTail := readLogTail(opts.LogPath, 1000)

	var attrs []string
	attrs = append(attrs, fmt.Sprintf("source=%q", opts.Source))
	attrs = append(attrs, fmt.Sprintf("status=%q", status))
	if opts.CronID > 0 {
		attrs = append(attrs, fmt.Sprintf("cron_id=%q", fmt.Sprint(opts.CronID)))
	}
	if opts.ChatID != "" {
		attrs = append(attrs, fmt.Sprintf("chat_id=%q", opts.ChatID))
	}
	attrs = append(attrs, fmt.Sprintf("log=%q", opts.LogPath))
	if opts.Runtime.Provider != "" {
		attrs = append(attrs, fmt.Sprintf("runtime_provider=%q", opts.Runtime.Provider))
	}
	if opts.Runtime.Mode != "" {
		attrs = append(attrs, fmt.Sprintf("runtime_mode=%q", opts.Runtime.Mode))
	}
	if opts.Runtime.Version != "" {
		attrs = append(attrs, fmt.Sprintf("runtime_version=%q", opts.Runtime.Version))
	}

	notification := fmt.Sprintf("<subagent-notification %s>\n%s\n</subagent-notification>",
		strings.Join(attrs, " "), logTail)

	_ = tmux.PasteAndEnterFor(ctx, sessionName, notification)
}

// readLogTail returns the last maxBytes of a log file.
func readLogTail(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(log not readable)"
	}
	s := strings.TrimSpace(string(data))
	if len(s) > maxBytes {
		s = "...\n" + s[len(s)-maxBytes:]
	}
	return s
}

func filterEnv(env []string, remove string) []string {
	prefix := remove + "="
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// RunSync runs a Claude-compatible subagent synchronously, blocking until it completes.
// Tracks the run in the database if store is non-nil.
func RunSync(ctx context.Context, store *db.Store, opts RunOpts) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "claude", "--dangerously-skip-permissions", "-p", opts.Prompt)
	cmd.Dir = opts.WorkspaceDir
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	if opts.Runtime.Provider == "" {
		opts.Runtime = db.ExecutionRuntime{
			Provider: "claude",
			Mode:     "headless_exec",
		}
	}
	if opts.SessionName == "" {
		opts.SessionName = "goat_claude_main"
	}
	result, err := RunSyncCommand(ctx, store, cmd, opts)
	return result.Output, err
}

// RunBackground starts a Claude-compatible subagent in the background and returns immediately.
// Tracks the run in the database if store is non-nil.
func RunBackground(store *db.Store, opts RunOpts) (pid int, err error) {
	cmd := exec.Command("claude", "--dangerously-skip-permissions", "-p", opts.Prompt)
	cmd.Dir = opts.WorkspaceDir
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	if opts.Runtime.Provider == "" {
		opts.Runtime = db.ExecutionRuntime{
			Provider: "claude",
			Mode:     "headless_exec",
		}
	}
	if opts.SessionName == "" {
		opts.SessionName = "goat_claude_main"
	}
	result, err := RunBackgroundCommand(store, cmd, opts)
	return result.PID, err
}

// RunSyncCommand runs a prepared process synchronously, blocking until it completes.
func RunSyncCommand(ctx context.Context, store *db.Store, cmd *exec.Cmd, opts RunOpts) (Result, error) {
	outFile, err := os.Create(opts.LogPath)
	if err != nil {
		return Result{}, fmt.Errorf("create log %s: %w", opts.LogPath, err)
	}
	cmd.Stdout = outFile
	cmd.Stderr = outFile

	if err := cmd.Start(); err != nil {
		outFile.Close()
		return Result{}, fmt.Errorf("start subagent: %w", err)
	}

	var runID uint64
	if store != nil {
		runID, _ = store.RecordSubagentStart(
			cmd.Process.Pid,
			opts.Source,
			opts.CronID,
			opts.ChatID,
			opts.Prompt,
			opts.LogPath,
			opts.Runtime,
		)
	}

	runErr := cmd.Wait()
	outFile.Close()

	output, _ := os.ReadFile(opts.LogPath)
	handleCompletion(store, runID, runErr, opts)
	status := "ok"
	if runErr != nil {
		status = "error"
	}
	return Result{
		PID:             cmd.Process.Pid,
		Status:          status,
		Output:          output,
		RuntimeProvider: opts.Runtime.Provider,
		RuntimeMode:     opts.Runtime.Mode,
		RuntimeVersion:  opts.Runtime.Version,
	}, runErr
}

// RunBackgroundCommand starts a prepared process in the background and returns immediately.
func RunBackgroundCommand(store *db.Store, cmd *exec.Cmd, opts RunOpts) (Result, error) {
	f, err := os.Create(opts.LogPath)
	if err != nil {
		return Result{}, fmt.Errorf("create log file: %w", err)
	}
	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Start(); err != nil {
		f.Close()
		return Result{}, fmt.Errorf("start subagent: %w", err)
	}

	pid := cmd.Process.Pid

	var runID uint64
	if store != nil {
		runID, _ = store.RecordSubagentStart(
			pid,
			opts.Source,
			opts.CronID,
			opts.ChatID,
			opts.Prompt,
			opts.LogPath,
			opts.Runtime,
		)
	}

	go func() {
		runErr := cmd.Wait()
		f.Close()
		handleCompletion(store, runID, runErr, opts)
	}()

	return Result{
		PID:             pid,
		Status:          "running",
		RuntimeProvider: opts.Runtime.Provider,
		RuntimeMode:     opts.Runtime.Mode,
		RuntimeVersion:  opts.Runtime.Version,
	}, nil
}
