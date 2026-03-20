package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goated/internal/agent"
	"goated/internal/msglog"
)

// postSendWaitTimeout is how long SendUserPrompt will wait for a previous
// claude process to finish before giving up.
const postSendWaitTimeout = 6 * time.Minute

// SessionRuntime implements agent.SessionRuntime using `claude -p --resume`.
// No persistent process or tmux session — each user message spawns a short-lived
// `claude -p` process that reconnects via --resume <session_id>.
type SessionRuntime struct {
	workspaceDir string
	logDir       string
	model        string // claude CLI --model value; empty means default
	redactor     *msglog.Redactor

	mu         sync.Mutex
	proc       *exec.Cmd     // current running process, nil if idle
	procErr    error         // last process error
	lastStderr string        // last captured stderr
	done       chan struct{} // closed when proc exits
}

func NewSessionRuntime(workspaceDir, logDir, model string) *SessionRuntime {
	credsDir := filepath.Join(workspaceDir, "creds")
	return &SessionRuntime{
		workspaceDir: workspaceDir,
		logDir:       logDir,
		model:        model,
		redactor:     msglog.NewRedactor(credsDir),
	}
}

func (r *SessionRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{
		Provider:    agent.RuntimeClaude,
		DisplayName: "Claude Code",
		SessionName: "",
		Capabilities: agent.Capabilities{
			SupportsInteractiveSession: false,
			SupportsContextEstimate:    false,
			SupportsCompaction:         false,
			SupportsReset:              true,
		},
	}
}

// baseArgs returns the common CLI flags for all claude -p invocations.
func (r *SessionRuntime) baseArgs() []string {
	args := []string{
		"--output-format", "json",
		"--dangerously-skip-permissions",
		"--setting-sources", "project,local",
		"--settings", r.hooksSettingsFile(),
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
		// Fallback must differ from the main model
		fallback := "sonnet"
		if r.model == "sonnet" || r.model == "claude-sonnet-4-6" {
			fallback = "haiku"
		}
		args = append(args, "--fallback-model", fallback)
	} else {
		args = append(args, "--fallback-model", "sonnet")
	}
	return args
}

// sessionDir returns the directory for claude session state files.
func (r *SessionRuntime) sessionDir() string {
	return filepath.Join(r.logDir, "claude_session")
}

// hookDir returns the directory where hook output is written.
func (r *SessionRuntime) hookDir() string {
	return r.sessionDir()
}

// runsDir returns the directory for per-run output logs.
func (r *SessionRuntime) runsDir() string {
	return filepath.Join(r.sessionDir(), "runs")
}

// credsDir returns the path to the creds directory under workspace.
func (r *SessionRuntime) credsDir() string {
	return filepath.Join(r.workspaceDir, "creds")
}

// hooksSettingsFile returns the absolute path to the hooks settings file.
// Passed explicitly via --settings so Claude doesn't need to discover it.
func (r *SessionRuntime) hooksSettingsFile() string {
	p := hooksSettingsPath(r.workspaceDir)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// sessionIDPath returns the path to the session ID file.
func (r *SessionRuntime) sessionIDPath() string {
	return filepath.Join(r.sessionDir(), "session_id")
}

// readSessionID reads the current session ID, or empty string if none.
func (r *SessionRuntime) readSessionID() string {
	data, err := os.ReadFile(r.sessionIDPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeSessionID writes a session ID to the state file.
func (r *SessionRuntime) writeSessionID(id string) error {
	return os.WriteFile(r.sessionIDPath(), []byte(id+"\n"), 0o644)
}

func (r *SessionRuntime) EnsureSession(ctx context.Context) error {
	// Create session directories
	for _, dir := range []string{r.sessionDir(), r.runsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Write hooks config (resolve workspace to absolute for reliable paths)
	absWorkspace, _ := filepath.Abs(r.workspaceDir)
	absCredsDir, _ := filepath.Abs(r.credsDir())
	if err := writeHooksConfig(absWorkspace, r.hookDir(), absCredsDir); err != nil {
		return fmt.Errorf("write hooks config: %w", err)
	}

	// Warm up: if no session ID exists, run a no-op prompt to create one.
	// This lets Claude read the workspace docs and prepares the session for
	// instant --resume on the first real user message.
	if r.readSessionID() == "" {
		if err := r.warmUpSession(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] session warm-up failed: %v\n", time.Now().Format(time.RFC3339), err)
			// Non-fatal — first real message will create the session instead
		}
	}

	return nil
}

// warmUpSession runs a lightweight claude -p to initialize the session and
// capture a session_id for subsequent --resume calls.
func (r *SessionRuntime) warmUpSession(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "[%s] warming up claude session...\n", time.Now().Format(time.RFC3339))

	args := append([]string{
		"-p", "You are being initialized. Read your docs and say 'ready' — nothing else.",
	}, r.baseArgs()...)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = r.workspaceDir
	cmd.Env = append(
		filterEnv(os.Environ(), "CLAUDECODE"),
		fmt.Sprintf("GOATED_HOOK_DIR=%s", r.hookDir()),
	)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("warm-up claude -p: %w (stderr: %s)", err, stderrBuf.String())
	}

	sid := parseSessionID(string(out))
	if sid == "" {
		return fmt.Errorf("warm-up did not return a session_id")
	}

	if err := r.writeSessionID(sid); err != nil {
		return fmt.Errorf("save warm-up session ID: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[%s] claude session ready (id=%s)\n", time.Now().Format(time.RFC3339), sid)
	return nil
}

func (r *SessionRuntime) SendUserPrompt(ctx context.Context, channel, chatID, userPrompt string, attachments *agent.MessageAttachments, messageID, threadID string) error {
	envelope := agent.BuildPromptEnvelope(channel, chatID, userPrompt, attachments, messageID, threadID)
	return r.sendEnvelope(ctx, envelope)
}

func (r *SessionRuntime) SendBatchPrompt(ctx context.Context, channel, chatID string, messages []agent.PromptMessage) error {
	envelope := agent.BuildBatchEnvelope(channel, chatID, messages)
	return r.sendEnvelope(ctx, envelope)
}

// sendEnvelope launches a claude -p process with the given pre-built envelope.
// It waits for any in-flight process to finish first (FIFO — only one caller
// can be waiting at a time when called from the sequential message queue).
func (r *SessionRuntime) sendEnvelope(ctx context.Context, envelope string) error {
	if err := r.EnsureSession(ctx); err != nil {
		return err
	}

	// Build the claude command — only use --resume if we have a session ID
	args := append([]string{"-p", envelope}, r.baseArgs()...)
	sessionID := r.readSessionID()
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = r.workspaceDir
	cmd.Env = append(
		filterEnv(os.Environ(), "CLAUDECODE"),
		fmt.Sprintf("GOATED_HOOK_DIR=%s", r.hookDir()),
	)
	if reqID := msglog.RequestIDFromContext(ctx); reqID != "" {
		cmd.Env = append(cmd.Env, "GOAT_REQUEST_ID="+reqID)
	}

	// Log output to a per-run file
	runFile := filepath.Join(r.runsDir(), fmt.Sprintf("%d.json", time.Now().UnixNano()))
	outFile, err := os.Create(runFile)
	if err != nil {
		return fmt.Errorf("create run log: %w", err)
	}

	// Capture stdout separately so we can parse session_id from JSON output.
	// Buffer stays raw; file gets redacted (run output is all content).
	var stdoutBuf strings.Builder
	cmd.Stdout = &teeWriter{file: outFile, buf: &stdoutBuf, redactor: r.redactor}

	// Capture stderr separately for error detection.
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrWriter{file: outFile, buf: &stderrBuf, redactor: r.redactor}

	// Serialize access: if a process is already running, wait for it to finish.
	// Only one goroutine should be waiting here at a time (the sequential
	// message queue ensures this), so there is no FIFO contention.
	r.mu.Lock()
	for r.proc != nil {
		done := r.done
		r.mu.Unlock()

		fmt.Fprintf(os.Stderr, "[%s] queued: waiting for in-flight claude process to finish\n",
			time.Now().Format(time.RFC3339))

		select {
		case <-done:
			// Previous process finished — re-check under lock
		case <-time.After(postSendWaitTimeout):
			outFile.Close()
			return fmt.Errorf("timed out waiting for previous claude process to finish")
		case <-ctx.Done():
			outFile.Close()
			return ctx.Err()
		}

		r.mu.Lock()
	}

	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		outFile.Close()
		return fmt.Errorf("start claude: %w", err)
	}

	r.proc = cmd
	r.procErr = nil
	r.lastStderr = ""
	r.done = make(chan struct{})
	done := r.done
	r.mu.Unlock()

	// Wait for process exit in background
	go func() {
		runErr := cmd.Wait()
		outFile.Close()

		// Parse session_id from JSON output and save for future --resume
		if sid := parseSessionID(stdoutBuf.String()); sid != "" {
			_ = r.writeSessionID(sid)
		}

		r.mu.Lock()
		r.proc = nil
		r.procErr = runErr
		r.lastStderr = stderrBuf.String()
		r.mu.Unlock()

		close(done)
	}()

	return nil
}

func (r *SessionRuntime) WaitForAwaitingInput(ctx context.Context, timeout time.Duration) (agent.SessionState, error) {
	r.mu.Lock()
	done := r.done
	proc := r.proc
	r.mu.Unlock()

	// No process running — already idle
	if proc == nil || done == nil {
		return agent.SessionState{
			Kind:    agent.SessionStateAwaitingInput,
			Summary: "idle (no process)",
		}, nil
	}

	// Wait for process to finish
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		// Process exited — check result
		r.mu.Lock()
		lastErr := r.procErr
		r.mu.Unlock()

		if lastErr != nil {
			return agent.SessionState{
				Kind:    agent.SessionStateAwaitingInput,
				Summary: fmt.Sprintf("process exited with error: %v", lastErr),
			}, nil
		}
		return agent.SessionState{
			Kind:    agent.SessionStateAwaitingInput,
			Summary: "process completed",
		}, nil

	case <-timer.C:
		return agent.SessionState{
			Kind:    agent.SessionStateGenerating,
			Summary: "timed out waiting for process",
		}, fmt.Errorf("timed out waiting for claude process to finish")

	case <-ctx.Done():
		return agent.SessionState{}, ctx.Err()
	}
}

func (r *SessionRuntime) GetSessionState(ctx context.Context) (agent.SessionState, error) {
	r.mu.Lock()
	proc := r.proc
	done := r.done
	lastErr := r.procErr
	lastStderr := r.lastStderr
	r.mu.Unlock()

	// Process running and not yet done
	if proc != nil && done != nil {
		select {
		case <-done:
			// Already finished
		default:
			return agent.SessionState{
				Kind:    agent.SessionStateGenerating,
				Summary: "processing",
			}, nil
		}
	}

	// Check for auth errors in last stderr
	if lastStderr != "" {
		for _, pat := range []string{"authentication_error", "Please run /login", "OAuth token has expired"} {
			if strings.Contains(lastStderr, pat) {
				return agent.SessionState{
					Kind:    agent.SessionStateBlockedIntervene,
					Summary: "Claude Code requires manual login",
				}, nil
			}
		}
	}

	if lastErr != nil {
		return agent.SessionState{
			Kind:    agent.SessionStateAwaitingInput,
			Summary: fmt.Sprintf("last process error: %v", lastErr),
		}, nil
	}

	return agent.SessionState{
		Kind:    agent.SessionStateAwaitingInput,
		Summary: "idle",
	}, nil
}

func (r *SessionRuntime) StopSession(ctx context.Context) error {
	r.mu.Lock()
	proc := r.proc
	done := r.done
	r.mu.Unlock()

	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
		// Wait briefly for cleanup
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
	}

	// Delete session ID file
	_ = os.Remove(r.sessionIDPath())

	return nil
}

func (r *SessionRuntime) RestartSession(ctx context.Context) error {
	if err := r.StopSession(ctx); err != nil {
		return err
	}
	return r.EnsureSession(ctx)
}

func (r *SessionRuntime) ResetConversation(ctx context.Context, chatID string) (agent.ResetResult, error) {
	// Delete session ID — next message creates a fresh session
	_ = os.Remove(r.sessionIDPath())

	return agent.ResetResult{
		Scope:   agent.ResetScopeHard,
		Summary: "Session ID cleared. We are now in a fresh chat session.",
	}, nil
}

func (r *SessionRuntime) SendControlCommand(ctx context.Context, text string) error {
	// No-op for headless claude -p runtime. Claude -p handles context internally.
	return nil
}

func (r *SessionRuntime) SendSystemNotice(ctx context.Context, channel, chatID, source, message string, metadata map[string]string) error {
	envelope := agent.BuildSystemNoticeEnvelope(channel, chatID, source, message, metadata)
	return r.sendEnvelope(ctx, envelope)
}

func (r *SessionRuntime) GetContextEstimate(ctx context.Context, chatID string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{
		State:      agent.ContextEstimateUnknown,
		RawSummary: "claude -p manages context automatically",
	}, nil
}

func (r *SessionRuntime) GetHealth(ctx context.Context) (agent.HealthStatus, error) {
	// Check that claude binary exists
	if _, err := exec.LookPath("claude"); err != nil {
		return agent.HealthStatus{
			OK:          false,
			Recoverable: false,
			Summary:     "claude binary not found on PATH",
		}, nil
	}

	// Check for auth errors from last run
	r.mu.Lock()
	lastStderr := r.lastStderr
	r.mu.Unlock()

	if lastStderr != "" {
		for _, pat := range []string{"authentication_error", "OAuth token has expired", "Please run /login"} {
			if strings.Contains(lastStderr, pat) {
				return agent.HealthStatus{
					OK:          false,
					Recoverable: false,
					Summary:     fmt.Sprintf("auth error: %s", pat),
				}, nil
			}
		}
	}

	return agent.HealthStatus{
		OK:          true,
		Recoverable: true,
		Summary:     "ok",
	}, nil
}

func (r *SessionRuntime) DetectRetryableError(ctx context.Context) string {
	r.mu.Lock()
	lastStderr := r.lastStderr
	r.mu.Unlock()

	for _, pat := range []string{
		"API Error: 500",
		"API Error: 502",
		"API Error: 503",
		"API Error: 529",
		"Internal server error",
		"overloaded_error",
		"overloaded",
		"status 500",
	} {
		if strings.Contains(lastStderr, pat) {
			return pat
		}
	}
	return ""
}

func (r *SessionRuntime) Version(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "claude", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// filterEnv removes environment variables with the given key prefix.
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

// parseSessionID extracts session_id from the JSON output of claude -p --output-format json.
func parseSessionID(jsonOutput string) string {
	// Simple extraction — look for "session_id":"<uuid>"
	const key = `"session_id":"`
	idx := strings.Index(jsonOutput, key)
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(jsonOutput[start:], `"`)
	if end < 0 {
		return ""
	}
	return jsonOutput[start : start+end]
}

// teeWriter writes to both a file and a string builder.
// The buffer gets raw data (for session ID parsing), while the file gets
// redacted output (run logs are all content).
type teeWriter struct {
	file     *os.File
	buf      *strings.Builder
	redactor *msglog.Redactor
}

func (w *teeWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.redactor != nil {
		redacted := w.redactor.Redact(string(p))
		_, err := w.file.WriteString(redacted)
		if err != nil {
			return 0, err
		}
	} else {
		if _, err := w.file.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// stderrWriter tees stderr to both a file and a string builder.
// The buffer gets raw data (for error detection), while the file gets
// redacted output.
type stderrWriter struct {
	file     *os.File
	buf      *strings.Builder
	redactor *msglog.Redactor
}

func (w *stderrWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.redactor != nil {
		redacted := w.redactor.Redact(string(p))
		_, err := w.file.WriteString(redacted)
		if err != nil {
			return 0, err
		}
	} else {
		if _, err := w.file.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
