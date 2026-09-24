package pi

import (
	"context"
	"os/exec"
	"strings"

	"goated/internal/agent"
	"goated/internal/db"
	"goated/internal/subagent"
)

type HeadlessRuntime struct {
	WorkspaceDir string
	Provider     string
	Model        string
}

func NewHeadlessRuntime(workspaceDir, provider, model string) *HeadlessRuntime {
	return &HeadlessRuntime{
		WorkspaceDir: workspaceDir,
		Provider:     strings.TrimSpace(provider),
		Model:        strings.TrimSpace(model),
	}
}

func (h *HeadlessRuntime) Descriptor() agent.RuntimeDescriptor {
	return NewSessionRuntime(h.WorkspaceDir, "", "", "", "").Descriptor()
}

func (h *HeadlessRuntime) piArgs(prompt, model string) []string {
	args := []string{
		"-p", prompt,
		"--mode", "json",
		"--no-session",
	}
	if h.Provider != "" {
		args = append(args, "--provider", h.Provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	return args
}

// effectiveModel returns the per-run override when set, otherwise the
// runtime's configured default model.
func (h *HeadlessRuntime) effectiveModel(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return h.Model
}

func (h *HeadlessRuntime) RunSync(ctx context.Context, store *db.Store, req agent.HeadlessRequest) (agent.HeadlessResult, error) {
	version := h.Version(ctx)
	workspaceDir := chooseWorkspace(req.WorkspaceDir, h.WorkspaceDir)
	cmd := exec.CommandContext(ctx, "pi", h.piArgs(req.Prompt, h.effectiveModel(req.Model))...)
	cmd.Dir = workspaceDir

	result, err := subagent.RunSyncCommand(ctx, store, cmd, subagent.RunOpts{
		WorkspaceDir:      cmd.Dir,
		Prompt:            req.Prompt,
		LogPath:           req.LogPath,
		Source:            req.Source,
		CronID:            req.CronID,
		ChatID:            req.ChatID,
		NotifyMainSession: req.NotifyMainSession,
		LogCaller:         req.LogCaller,
		Runtime: db.ExecutionRuntime{
			Provider: "pi",
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
	cmd := exec.Command("pi", h.piArgs(req.Prompt, h.effectiveModel(req.Model))...)
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
		Runtime: db.ExecutionRuntime{
			Provider: "pi",
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
	cmd := exec.CommandContext(ctx, "pi", "--version")
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
