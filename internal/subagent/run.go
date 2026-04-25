package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"goated/internal/agent"
	"goated/internal/db"
	"goated/internal/tmux"
)

const basePreamble = `You are a Goated subagent.

Before doing any work in this workspace, read these files in order:
1. GOATED_CLI_README.md
2. GOATED.md
3. self/CLAUDE.md
4. self/AGENTS.md (if it exists)

Follow the shared Goated runtime contract from GOATED.md plus any private guidance from self/CLAUDE.md and self/AGENTS.md.`

func BuildPreamble(extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return basePreamble
	}
	return basePreamble + "\n\n" + extra
}

type BuildPromptOpts struct {
	ChatID  string
	Source  string
	LogPath string
	Cron    *db.CronJob
}

// BuildPrompt constructs the prompt for a headless subagent.
// preamble is an optional prefix (e.g. "Read CRON.md before executing.").
// opts carries optional runtime-specific execution context.
func BuildPrompt(preamble, userPrompt string, opts BuildPromptOpts) string {
	var b strings.Builder
	if preamble != "" {
		b.WriteString(preamble)
		b.WriteString("\n\n")
	}
	if opts.Cron != nil {
		b.WriteString(buildCronContextBlock(*opts.Cron))
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(userPrompt))
	b.WriteString("\n")
	if opts.ChatID != "" {
		b.WriteString("\nSend your response to the user by piping markdown into:\n")
		sendCmd := fmt.Sprintf("  ./goat send_user_message --chat %s", opts.ChatID)
		if opts.Source != "" {
			sendCmd += fmt.Sprintf(" --source %s", opts.Source)
		}
		if opts.LogPath != "" {
			sendCmd += fmt.Sprintf(" --log %s", opts.LogPath)
		}
		b.WriteString(sendCmd + "\n")
		b.WriteString("Keep any provided --source/--log flags intact so background execution stays properly correlated.\n")
		b.WriteString("IMPORTANT: Do NOT read, cat, tail, or inspect the --log file path. It is a write-only output stream and reading it will cause a feedback loop.\n")
		b.WriteString("\nSee GOATED_CLI_README.md for formatting details.\n")
	}
	return b.String()
}

func buildCronContextBlock(job db.CronJob) string {
	payload := struct {
		CurrentTime string `json:"current_time"`
		Schedule    string `json:"schedule"`
		Timezone    string `json:"timezone"`
		NotifyUser  bool   `json:"notify_user"`
		ChatID      string `json:"chat_id"`
		PromptFile  string `json:"prompt_file,omitempty"`
	}{
		CurrentTime: currentTimeIn(job.Timezone),
		Schedule:    job.Schedule,
		Timezone:    job.Timezone,
		NotifyUser:  job.EffectiveNotifyUser(),
		ChatID:      job.ChatID,
		PromptFile:  job.PromptFile,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "Cron execution context: (unavailable)"
	}
	return "Cron execution context (authoritative):\n```json\n" + string(data) + "\n```"
}

func currentTimeIn(timezone string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format(time.RFC3339)
}

// RunOpts configures a subagent run.
type RunOpts struct {
	WorkspaceDir      string
	Prompt            string
	LogPath           string
	Source            string // "cron", "cli", "gateway"
	CronID            uint64 // only for cron-sourced runs
	ChatID            string
	NotifyMainSession bool
	SessionName       string
	Model             string // claude CLI --model value; empty means default
	Runtime           db.ExecutionRuntime
	LogCaller         string // propagated as LOG_CALLER env var to child process
}

type Result struct {
	PID             int
	Status          string
	Output          []byte
	RuntimeProvider string
	RuntimeMode     string
	RuntimeVersion  string
}

var resolveExecutable = os.Executable

// handleCompletion records the subagent's final status and notifies the main
// interactive runtime session. Shared by RunSync and RunBackground.
func HandleCompletion(store *db.Store, runID uint64, runErr error, opts RunOpts) {
	status := "ok"
	if runErr != nil {
		status = "error"
	}
	if store != nil && runID > 0 {
		_ = store.RecordSubagentFinish(runID, status)
	}
	if !opts.NotifyMainSession && status == "ok" {
		return
	}
	NotifyMainSession(opts, status)
}

// NotifyMainSession pastes a no-op system notice envelope into the configured
// tmux session so the main interactive runtime has context about background
// work without being prompted to reply.
func NotifyMainSession(opts RunOpts, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessionName := opts.SessionName
	if sessionName == "" {
		sessionName = "goat_claude_main"
	}

	if !tmux.SessionExistsFor(ctx, sessionName) {
		return
	}

	logTail := strings.TrimSpace(readLogTail(opts.LogPath, 300))
	message := buildBackgroundNoticeMessage(opts, status, logTail)

	metadata := map[string]string{}
	if opts.CronID > 0 {
		metadata["cron_id"] = fmt.Sprint(opts.CronID)
	}
	if opts.LogPath != "" {
		metadata["log"] = opts.LogPath
	}

	channel := "slack"
	noticeChatID := opts.ChatID
	if opts.Source == "cron" || opts.ChatID == "" {
		channel = "internal"
		noticeChatID = ""
	}

	notice := agent.BuildSystemNoticeEnvelope(channel, noticeChatID, opts.Source, message, metadata)
	_ = tmux.PasteAndEnterFor(ctx, sessionName, notice)
}

func buildBackgroundNoticeMessage(opts RunOpts, status, logTail string) string {
	label := "Background job"
	switch {
	case opts.Source == "cron" && opts.CronID > 0:
		label = fmt.Sprintf("Cron #%d", opts.CronID)
	case opts.Source != "":
		label = "Background " + opts.Source
	}

	message := fmt.Sprintf("%s: %s.", label, status)
	if summary := summarizeLogTail(logTail); summary != "" {
		message += " " + summary
	}
	return message
}

func summarizeLogTail(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "", "(log not readable)":
		return s
	case "No new emails. Nothing to report.", "No new emails - nothing to report.", "No new emails — nothing to report.":
		return "No new emails."
	}

	lines := strings.Split(s, "\n")
	if len(lines) == 1 && len(lines[0]) <= 120 {
		return lines[0]
	}
	return ""
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

// buildEnv filters CLAUDECODE vars and injects LOG_CALLER if set.
func buildEnv(logCaller string) []string {
	env := filterEnv(os.Environ(), "CLAUDECODE")
	if logCaller != "" {
		// Remove any existing LOG_CALLER first
		env = filterEnv(env, "LOG_CALLER")
		env = append(env, "LOG_CALLER="+logCaller)
	}
	return env
}

// RunSync runs a Claude-compatible subagent synchronously, blocking until it completes.
// Tracks the run in the database if store is non-nil.
func RunSync(ctx context.Context, store *db.Store, opts RunOpts) ([]byte, error) {
	args := []string{"--dangerously-skip-permissions", "-p", opts.Prompt}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = opts.WorkspaceDir
	cmd.Env = buildEnv(opts.LogCaller)
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
	args := []string{"--dangerously-skip-permissions", "-p", opts.Prompt}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = opts.WorkspaceDir
	cmd.Env = buildEnv(opts.LogCaller)
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
	// Inject LOG_CALLER into the child process environment if set.
	if opts.LogCaller != "" {
		if cmd.Env == nil {
			cmd.Env = buildEnv(opts.LogCaller)
		} else {
			cmd.Env = filterEnv(cmd.Env, "LOG_CALLER")
			cmd.Env = append(cmd.Env, "LOG_CALLER="+opts.LogCaller)
		}
	}

	// Write stdout/stderr to a separate stream file to prevent feedback
	// loops if the model reads the log path exposed in its prompt.
	streamPath := opts.LogPath + ".stream"
	outFile, err := os.Create(streamPath)
	if err != nil {
		return Result{}, fmt.Errorf("create stream log %s: %w", streamPath, err)
	}
	sw := &sizeWatchWriter{w: outFile, limit: 500 * 1024 * 1024} // 500 MB
	cmd.Stdout = sw
	cmd.Stderr = sw

	if err := cmd.Start(); err != nil {
		outFile.Close()
		os.Remove(streamPath)
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

	if sw.exceeded {
		log.Printf("WARNING: subagent output exceeded %d bytes, log truncated (pid=%d)", sw.limit, cmd.Process.Pid)
	}

	// Move stream file to the canonical log path now that the process is done.
	if err := os.Rename(streamPath, opts.LogPath); err != nil {
		// Rename may fail across filesystems; fall back to copy.
		if data, readErr := os.ReadFile(streamPath); readErr == nil {
			_ = os.WriteFile(opts.LogPath, data, 0o644)
		}
		os.Remove(streamPath)
	}

	output, _ := os.ReadFile(opts.LogPath)
	HandleCompletion(store, runID, runErr, opts)
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
	// Inject LOG_CALLER into the child process environment if set.
	if opts.LogCaller != "" {
		if cmd.Env == nil {
			cmd.Env = buildEnv(opts.LogCaller)
		} else {
			cmd.Env = filterEnv(cmd.Env, "LOG_CALLER")
			cmd.Env = append(cmd.Env, "LOG_CALLER="+opts.LogCaller)
		}
	}

	if err := os.MkdirAll(filepath.Dir(opts.LogPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	logFile, err := os.OpenFile(opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Result{}, fmt.Errorf("create log file: %w", err)
	}
	_ = logFile.Close()

	var runID uint64

	stdinPath := ""
	if cmd.Stdin != nil {
		stdinData, err := io.ReadAll(cmd.Stdin)
		if err != nil {
			if runID > 0 {
				_ = store.RecordSubagentFinish(runID, "error")
			}
			return Result{}, fmt.Errorf("read subagent stdin: %w", err)
		}
		stdinFile, err := os.CreateTemp(filepath.Dir(opts.LogPath), "goat-subagent-stdin-*")
		if err != nil {
			if runID > 0 {
				_ = store.RecordSubagentFinish(runID, "error")
			}
			return Result{}, fmt.Errorf("create stdin file: %w", err)
		}
		stdinPath = stdinFile.Name()
		if _, err := stdinFile.Write(stdinData); err != nil {
			_ = stdinFile.Close()
			_ = os.Remove(stdinPath)
			if runID > 0 {
				_ = store.RecordSubagentFinish(runID, "error")
			}
			return Result{}, fmt.Errorf("write stdin file: %w", err)
		}
		if err := stdinFile.Close(); err != nil {
			_ = os.Remove(stdinPath)
			if runID > 0 {
				_ = store.RecordSubagentFinish(runID, "error")
			}
			return Result{}, fmt.Errorf("close stdin file: %w", err)
		}
	}

	wrapper, err := os.CreateTemp(filepath.Dir(opts.LogPath), "goat-subagent-*.sh")
	if err != nil {
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		if runID > 0 {
			_ = store.RecordSubagentFinish(runID, "error")
		}
		return Result{}, fmt.Errorf("create wrapper script: %w", err)
	}

	helperPath, err := resolveExecutable()
	if err != nil {
		_ = wrapper.Close()
		_ = os.Remove(wrapper.Name())
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		if runID > 0 {
			_ = store.RecordSubagentFinish(runID, "error")
		}
		return Result{}, fmt.Errorf("resolve helper path: %w", err)
	}

	invocation := shellJoin(append([]string{cmd.Path}, cmd.Args[1:]...))
	if stdinPath != "" {
		invocation += " < " + shellQuote(stdinPath)
	}
	statusFlag := "ok"
	if store == nil {
		statusFlag = "unknown"
	}
	finishCmd := baseFinishCommand(helperPath, store, opts)

	gatePath := wrapper.Name() + ".ready"
	runIDPath := wrapper.Name() + ".runid"
	scriptContent := func(finishCmd []string) string {
		cleanupCmd := "rm -f \"$0\" " + shellQuote(gatePath)
		if store != nil {
			cleanupCmd += " " + shellQuote(runIDPath)
		}
		if stdinPath != "" {
			cleanupCmd += "\nrm -f " + shellQuote(stdinPath)
		}
		runIDSetup := ""
		if store != nil {
			runIDSetup = fmt.Sprintf("run_id=$(cat %s)\n", shellQuote(runIDPath))
		}
		return fmt.Sprintf(`#!/bin/sh
while [ ! -f %s ]; do sleep 0.05; done
%s
cd %s || exit 1
%s >> %s 2>&1
status=$?
finish_status=%s
if [ "$status" -ne 0 ]; then
  finish_status=error
fi
%s >/dev/null 2>&1 || true
%s
exit "$status"
`, shellQuote(gatePath), runIDSetup, shellQuote(cmd.Dir), invocation, shellQuote(opts.LogPath), statusFlag, strings.Join(finishCmd, " "), cleanupCmd)
	}

	if _, err := wrapper.WriteString(scriptContent(finishCmd)); err != nil {
		_ = wrapper.Close()
		_ = os.Remove(wrapper.Name())
		_ = os.Remove(gatePath)
		_ = os.Remove(runIDPath)
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		return Result{}, fmt.Errorf("write wrapper script: %w", err)
	}
	if err := wrapper.Close(); err != nil {
		_ = os.Remove(wrapper.Name())
		_ = os.Remove(gatePath)
		_ = os.Remove(runIDPath)
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		return Result{}, fmt.Errorf("close wrapper script: %w", err)
	}
	if err := os.Chmod(wrapper.Name(), 0o700); err != nil {
		_ = os.Remove(wrapper.Name())
		_ = os.Remove(gatePath)
		_ = os.Remove(runIDPath)
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		return Result{}, fmt.Errorf("chmod wrapper script: %w", err)
	}

	wrapped := exec.Command("/bin/sh", wrapper.Name())
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := wrapped.Start(); err != nil {
		_ = os.Remove(wrapper.Name())
		_ = os.Remove(gatePath)
		_ = os.Remove(runIDPath)
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		return Result{}, fmt.Errorf("start subagent wrapper: %w", err)
	}

	pid := wrapped.Process.Pid

	if store != nil {
		var err error
		runID, err = store.RecordSubagentStart(
			pid,
			opts.Source,
			opts.CronID,
			opts.ChatID,
			opts.Prompt,
			opts.LogPath,
			opts.Runtime,
		)
		if err != nil {
			_ = wrapped.Process.Kill()
			_ = os.Remove(wrapper.Name())
			_ = os.Remove(gatePath)
			_ = os.Remove(runIDPath)
			if stdinPath != "" {
				_ = os.Remove(stdinPath)
			}
			return Result{}, fmt.Errorf("record subagent start: %w", err)
		}
		if err := os.WriteFile(runIDPath, []byte(fmt.Sprint(runID)+"\n"), 0o600); err != nil {
			_ = store.RecordSubagentFinish(runID, "error")
			_ = wrapped.Process.Kill()
			_ = os.Remove(wrapper.Name())
			_ = os.Remove(gatePath)
			_ = os.Remove(runIDPath)
			if stdinPath != "" {
				_ = os.Remove(stdinPath)
			}
			return Result{}, fmt.Errorf("write subagent run id: %w", err)
		}
	}

	if err := os.WriteFile(gatePath, []byte("ready\n"), 0o600); err != nil {
		if runID > 0 {
			_ = store.RecordSubagentFinish(runID, "error")
		}
		_ = wrapped.Process.Kill()
		_ = os.Remove(wrapper.Name())
		_ = os.Remove(runIDPath)
		if stdinPath != "" {
			_ = os.Remove(stdinPath)
		}
		return Result{}, fmt.Errorf("release subagent wrapper: %w", err)
	}

	return Result{
		PID:             pid,
		Status:          "running",
		RuntimeProvider: opts.Runtime.Provider,
		RuntimeMode:     opts.Runtime.Mode,
		RuntimeVersion:  opts.Runtime.Version,
	}, nil
}

func baseFinishCommand(helperPath string, store *db.Store, opts RunOpts) []string {
	finishCmd := []string{
		shellQuote(helperPath), "subagent-finish",
		"--status", "$finish_status", // shell variable expanded by wrapper
		"--source", shellQuote(opts.Source),
		"--log", shellQuote(opts.LogPath),
		"--session", shellQuote(opts.SessionName),
	}
	if store != nil {
		finishCmd = append(finishCmd, "--db", shellQuote(store.Path()))
		finishCmd = append(finishCmd, "--run-id", "$run_id")
	}
	if opts.ChatID != "" {
		finishCmd = append(finishCmd, "--chat", shellQuote(opts.ChatID))
	}
	if opts.CronID > 0 {
		finishCmd = append(finishCmd, "--cron-id", shellQuote(fmt.Sprint(opts.CronID)))
	}
	if opts.Runtime.Provider != "" {
		finishCmd = append(finishCmd, "--runtime-provider", shellQuote(opts.Runtime.Provider))
	}
	if opts.Runtime.Mode != "" {
		finishCmd = append(finishCmd, "--runtime-mode", shellQuote(opts.Runtime.Mode))
	}
	if opts.Runtime.Version != "" {
		finishCmd = append(finishCmd, "--runtime-version", shellQuote(opts.Runtime.Version))
	}
	return finishCmd
}

func shellJoin(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		quoted = append(quoted, shellQuote(p))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sizeWatchWriter wraps an io.Writer and stops writing (discards) once the
// cumulative bytes written exceed `limit`. This prevents runaway log growth
// from feedback loops or infinite model output.
type sizeWatchWriter struct {
	w        *os.File
	limit    int64
	written  int64
	exceeded bool
	mu       sync.Mutex
}

func (sw *sizeWatchWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.exceeded {
		return len(p), nil // discard silently
	}
	sw.written += int64(len(p))
	if sw.written > sw.limit {
		sw.exceeded = true
		// Write a truncation marker and stop.
		marker := []byte("\n\n--- LOG TRUNCATED: exceeded size limit ---\n")
		_, _ = sw.w.Write(marker)
		return len(p), nil
	}
	return sw.w.Write(p)
}
