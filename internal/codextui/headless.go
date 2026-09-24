package codextui

import (
	"context"
	"os/exec"
	"strings"

	"goated/internal/agent"
	"goated/internal/db"
	"goated/internal/sessionname"
	"goated/internal/subagent"
)

type HeadlessRuntime struct {
	WorkspaceDir string
}

// headlessArgs builds the base `codex exec` flags. When model is non-empty it
// is passed through as a per-run override; empty uses the codex default.
func headlessArgs(model string) []string {
	args := []string{
		"exec",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	return args
}

func NewHeadlessRuntime(workspaceDir string) *HeadlessRuntime {
	return &HeadlessRuntime{WorkspaceDir: workspaceDir}
}

func (h *HeadlessRuntime) Descriptor() agent.RuntimeDescriptor {
	return NewSessionRuntime(h.WorkspaceDir, "").Descriptor()
}

func (h *HeadlessRuntime) RunSync(ctx context.Context, store *db.Store, req agent.HeadlessRequest) (agent.HeadlessResult, error) {
	version := h.Version(ctx)
	workspaceDir := chooseWorkspace(req.WorkspaceDir, h.WorkspaceDir)
	cmd := exec.CommandContext(
		ctx,
		"codex",
		append(headlessArgs(req.Model), "-")...,
	)
	cmd.Dir = workspaceDir
	cmd.Stdin = strings.NewReader(req.Prompt)

	result, err := subagent.RunSyncCommand(ctx, store, cmd, subagent.RunOpts{
		WorkspaceDir:      cmd.Dir,
		Prompt:            req.Prompt,
		LogPath:           req.LogPath,
		Source:            req.Source,
		CronID:            req.CronID,
		ChatID:            req.ChatID,
		NotifyMainSession: req.NotifyMainSession,
		LogCaller:         req.LogCaller,
		SessionName:       sessionname.CodexTUI(workspaceDir),
		Runtime: db.ExecutionRuntime{
			Provider: "codex_tui",
			Mode:     "headless_exec",
			Version:  version,
		},
	})
	return agent.HeadlessResult{
		PID:             result.PID,
		Status:          result.Status,
		RuntimeProvider: result.RuntimeProvider,
		RuntimeMode:     result.RuntimeMode,
		RuntimeVersion:  result.RuntimeVersion,
		Output:          result.Output,
	}, err
}

func (h *HeadlessRuntime) RunBackground(store *db.Store, req agent.HeadlessRequest) (agent.HeadlessResult, error) {
	version := h.Version(context.Background())
	workspaceDir := chooseWorkspace(req.WorkspaceDir, h.WorkspaceDir)
	cmd := exec.Command(
		"codex",
		append(headlessArgs(req.Model), req.Prompt)...,
	)
	cmd.Dir = workspaceDir

	result, err := subagent.RunBackgroundCommand(store, cmd, subagent.RunOpts{
		WorkspaceDir:      cmd.Dir,
		Prompt:            req.Prompt,
		LogPath:           req.LogPath,
		Source:            req.Source,
		CronID:            req.CronID,
		ChatID:            req.ChatID,
		NotifyMainSession: req.NotifyMainSession,
		LogCaller:         req.LogCaller,
		SessionName:       sessionname.CodexTUI(workspaceDir),
		Runtime: db.ExecutionRuntime{
			Provider: "codex_tui",
			Mode:     "headless_exec",
			Version:  version,
		},
	})
	return agent.HeadlessResult{
		PID:             result.PID,
		Status:          result.Status,
		RuntimeProvider: result.RuntimeProvider,
		RuntimeMode:     result.RuntimeMode,
		RuntimeVersion:  result.RuntimeVersion,
	}, err
}

func (h *HeadlessRuntime) Version(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "codex", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func chooseWorkspace(reqDir, fallback string) string {
	if reqDir != "" {
		return reqDir
	}
	return fallback
}
