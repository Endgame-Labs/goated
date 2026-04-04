package codex

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

const postSendWaitTimeout = 6 * time.Minute

type SessionRuntime struct {
	workspaceDir string
	logDir       string
	redactor     *msglog.Redactor

	mu         sync.Mutex
	proc       *exec.Cmd
	procErr    error
	lastStderr string
	done       chan struct{}
}

func NewSessionRuntime(workspaceDir, logDir string) *SessionRuntime {
	credsDir := filepath.Join(workspaceDir, "creds")
	return &SessionRuntime{
		workspaceDir: workspaceDir,
		logDir:       logDir,
		redactor:     msglog.NewRedactor(credsDir),
	}
}

func (r *SessionRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{
		Provider:    agent.RuntimeCodex,
		DisplayName: "Codex",
		SessionName: "",
		Capabilities: agent.Capabilities{
			SupportsInteractiveSession: false,
			SupportsContextEstimate:    false,
			SupportsCompaction:         false,
			SupportsReset:              true,
		},
	}
}

func (r *SessionRuntime) sessionDir() string {
	return filepath.Join(r.logDir, "codex_session")
}

func (r *SessionRuntime) runsDir() string {
	return filepath.Join(r.sessionDir(), "runs")
}

func (r *SessionRuntime) threadIDPath() string {
	return filepath.Join(r.sessionDir(), "thread_id")
}

func (r *SessionRuntime) readThreadID() string {
	data, err := os.ReadFile(r.threadIDPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (r *SessionRuntime) writeThreadID(id string) error {
	return os.WriteFile(r.threadIDPath(), []byte(id+"\n"), 0o644)
}

func (r *SessionRuntime) baseArgs() []string {
	return []string{
		"--json",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
	}
}

func (r *SessionRuntime) EnsureSession(ctx context.Context) error {
	for _, dir := range []string{r.sessionDir(), r.runsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if r.readThreadID() == "" {
		if err := r.warmUpSession(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] codex session warm-up failed: %v\n", time.Now().Format(time.RFC3339), err)
		}
	}
	return nil
}

func (r *SessionRuntime) warmUpSession(ctx context.Context) error {
	return r.sendPrompt(ctx, "You are being initialized. Read your docs and say 'ready' — nothing else.", true)
}

func (r *SessionRuntime) SendUserPrompt(ctx context.Context, channel, chatID, userPrompt string, attachments *agent.MessageAttachments, messageID, threadID string) error {
	envelope := agent.BuildPromptEnvelope(channel, chatID, userPrompt, attachments, messageID, threadID)
	return r.sendEnvelope(ctx, envelope)
}

func (r *SessionRuntime) SendBatchPrompt(ctx context.Context, channel, chatID string, messages []agent.PromptMessage) error {
	envelope := agent.BuildBatchEnvelope(channel, chatID, messages)
	return r.sendEnvelope(ctx, envelope)
}

func (r *SessionRuntime) sendEnvelope(ctx context.Context, envelope string) error {
	if err := r.EnsureSession(ctx); err != nil {
		return err
	}
	return r.sendPrompt(ctx, envelope, false)
}

func (r *SessionRuntime) sendPrompt(ctx context.Context, prompt string, forceFresh bool) error {
	threadID := ""
	if !forceFresh {
		threadID = r.readThreadID()
	}

	args := []string{"exec"}
	if threadID != "" {
		args = append(args, "resume", threadID)
	}
	args = append(args, r.baseArgs()...)
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = r.workspaceDir
	cmd.Stdin = strings.NewReader(prompt)
	if reqID := msglog.RequestIDFromContext(ctx); reqID != "" {
		cmd.Env = append(filterEnv(os.Environ(), "GOAT_REQUEST_ID"), "GOAT_REQUEST_ID="+reqID)
	}

	runFile := filepath.Join(r.runsDir(), fmt.Sprintf("%d.jsonl", time.Now().UnixNano()))
	outFile, err := os.Create(runFile)
	if err != nil {
		return fmt.Errorf("create run log: %w", err)
	}

	var stdoutBuf strings.Builder
	cmd.Stdout = &teeWriter{file: outFile, buf: &stdoutBuf, redactor: r.redactor}

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrWriter{file: outFile, buf: &stderrBuf, redactor: r.redactor}

	r.mu.Lock()
	for r.proc != nil {
		done := r.done
		r.mu.Unlock()

		select {
		case <-done:
		case <-time.After(postSendWaitTimeout):
			outFile.Close()
			return fmt.Errorf("timed out waiting for previous codex process to finish")
		case <-ctx.Done():
			outFile.Close()
			return ctx.Err()
		}

		r.mu.Lock()
	}

	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		outFile.Close()
		return fmt.Errorf("start codex: %w", err)
	}

	r.proc = cmd
	r.procErr = nil
	r.lastStderr = ""
	r.done = make(chan struct{})
	done := r.done
	r.mu.Unlock()

	go func() {
		runErr := cmd.Wait()
		outFile.Close()

		if tid := parseThreadID(stdoutBuf.String()); tid != "" {
			_ = r.writeThreadID(tid)
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

	if proc == nil || done == nil {
		return agent.SessionState{Kind: agent.SessionStateAwaitingInput, Summary: "idle (no process)"}, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		r.mu.Lock()
		lastErr := r.procErr
		r.mu.Unlock()
		if lastErr != nil {
			return agent.SessionState{Kind: agent.SessionStateAwaitingInput, Summary: fmt.Sprintf("process exited with error: %v", lastErr)}, nil
		}
		return agent.SessionState{Kind: agent.SessionStateAwaitingInput, Summary: "process completed"}, nil
	case <-timer.C:
		return agent.SessionState{Kind: agent.SessionStateGenerating, Summary: "timed out waiting for process"}, fmt.Errorf("timed out waiting for codex process to finish")
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

	if proc != nil && done != nil {
		select {
		case <-done:
		default:
			return agent.SessionState{Kind: agent.SessionStateGenerating, Summary: "processing"}, nil
		}
	}

	for _, pat := range []string{"API key", "OPENAI_API_KEY", "unauthorized", "authentication"} {
		if strings.Contains(strings.ToLower(lastStderr), strings.ToLower(pat)) {
			return agent.SessionState{Kind: agent.SessionStateBlockedIntervene, Summary: "Codex requires manual login"}, nil
		}
	}

	if lastErr != nil {
		return agent.SessionState{Kind: agent.SessionStateAwaitingInput, Summary: fmt.Sprintf("last process error: %v", lastErr)}, nil
	}

	return agent.SessionState{Kind: agent.SessionStateAwaitingInput, Summary: "idle"}, nil
}

func (r *SessionRuntime) StopSession(ctx context.Context) error {
	r.mu.Lock()
	proc := r.proc
	done := r.done
	r.mu.Unlock()

	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
	}

	_ = os.Remove(r.threadIDPath())
	return nil
}

func (r *SessionRuntime) RestartSession(ctx context.Context) error {
	if err := r.StopSession(ctx); err != nil {
		return err
	}
	return r.EnsureSession(ctx)
}

func (r *SessionRuntime) ResetConversation(ctx context.Context, chatID string) (agent.ResetResult, error) {
	_ = os.Remove(r.threadIDPath())
	return agent.ResetResult{
		Scope:   agent.ResetScopeHard,
		Summary: "Codex thread cleared. The next message starts a fresh session.",
	}, nil
}

func (r *SessionRuntime) SendControlCommand(ctx context.Context, text string) error {
	return nil
}

func (r *SessionRuntime) SendSystemNotice(ctx context.Context, channel, chatID, source, message string, metadata map[string]string) error {
	envelope := agent.BuildSystemNoticeEnvelope(channel, chatID, source, message, metadata)
	return r.sendEnvelope(ctx, envelope)
}

func (r *SessionRuntime) GetContextEstimate(ctx context.Context, chatID string) (agent.ContextEstimate, error) {
	return agent.ContextEstimate{
		State:      agent.ContextEstimateUnknown,
		RawSummary: "codex exec manages context automatically",
	}, nil
}

func (r *SessionRuntime) GetHealth(ctx context.Context) (agent.HealthStatus, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return agent.HealthStatus{OK: false, Recoverable: false, Summary: "codex binary not found on PATH"}, nil
	}

	r.mu.Lock()
	lastStderr := r.lastStderr
	r.mu.Unlock()

	for _, pat := range []string{"API key", "OPENAI_API_KEY", "unauthorized", "authentication"} {
		if strings.Contains(strings.ToLower(lastStderr), strings.ToLower(pat)) {
			return agent.HealthStatus{OK: false, Recoverable: false, Summary: fmt.Sprintf("auth error: %s", pat)}, nil
		}
	}

	return agent.HealthStatus{OK: true, Recoverable: true, Summary: "ok"}, nil
}

func (r *SessionRuntime) DetectRetryableError(ctx context.Context) string {
	r.mu.Lock()
	lastStderr := strings.ToLower(r.lastStderr)
	r.mu.Unlock()

	for _, pat := range []string{"server_error", "rate_limit", "overloaded", "status 500", "status 502", "status 503"} {
		if strings.Contains(lastStderr, pat) {
			return pat
		}
	}
	return ""
}

func (r *SessionRuntime) Version(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "codex", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
