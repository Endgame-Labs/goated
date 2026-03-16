package claudetui

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
}

func NewHeadlessRuntime(workspaceDir string) *HeadlessRuntime {
	return &HeadlessRuntime{WorkspaceDir: workspaceDir}
}

func (h *HeadlessRuntime) Descriptor() agent.RuntimeDescriptor {
	return NewSessionRuntime(h.WorkspaceDir, "").Descriptor()
}

func (h *HeadlessRuntime) RunSync(ctx context.Context, store *db.Store, req agent.HeadlessRequest) (agent.HeadlessResult, error) {
	opts := subagent.RunOpts{
		WorkspaceDir: chooseWorkspace(req.WorkspaceDir, h.WorkspaceDir),
		Prompt:       req.Prompt,
		LogPath:      req.LogPath,
		Source:       req.Source,
		CronID:       req.CronID,
		ChatID:       req.ChatID,
		Silent:       req.Silent,
		LogCaller:    req.LogCaller,
		SessionName:  "goat_claude_tui_main",
		Runtime: db.ExecutionRuntime{
			Provider: "claude_tui",
			Mode:     "headless_exec",
			Version:  h.Version(ctx),
		},
	}

	output, err := subagent.RunSync(ctx, store, opts)
	result := agent.HeadlessResult{
		Status:          "ok",
		RuntimeProvider: opts.Runtime.Provider,
		RuntimeMode:     opts.Runtime.Mode,
		RuntimeVersion:  opts.Runtime.Version,
		Output:          output,
	}
	if err != nil {
		result.Status = "error"
	}
	return result, err
}

func (h *HeadlessRuntime) RunBackground(store *db.Store, req agent.HeadlessRequest) (agent.HeadlessResult, error) {
	opts := subagent.RunOpts{
		WorkspaceDir: chooseWorkspace(req.WorkspaceDir, h.WorkspaceDir),
		Prompt:       req.Prompt,
		LogPath:      req.LogPath,
		Source:       req.Source,
		CronID:       req.CronID,
		ChatID:       req.ChatID,
		Silent:       req.Silent,
		LogCaller:    req.LogCaller,
		SessionName:  "goat_claude_tui_main",
		Runtime: db.ExecutionRuntime{
			Provider: "claude_tui",
			Mode:     "headless_exec",
			Version:  h.Version(context.Background()),
		},
	}

	pid, err := subagent.RunBackground(store, opts)
	result := agent.HeadlessResult{
		PID:             pid,
		Status:          "running",
		RuntimeProvider: opts.Runtime.Provider,
		RuntimeMode:     opts.Runtime.Mode,
		RuntimeVersion:  opts.Runtime.Version,
	}
	return result, err
}

func (h *HeadlessRuntime) Version(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "claude", "--version")
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
