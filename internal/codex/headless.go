package codex

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

func headlessArgs() []string {
	return []string{
		"exec",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
		"-",
	}
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
		headlessArgs()...,
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
		SessionName:       sessionname.Codex(workspaceDir),
		Runtime: db.ExecutionRuntime{
			Provider: "codex",
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
		"exec",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"-c", `model_instructions_file="GOATED.md"`,
		req.Prompt,
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
		SessionName:       sessionname.Codex(workspaceDir),
		Runtime: db.ExecutionRuntime{
			Provider: "codex",
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
