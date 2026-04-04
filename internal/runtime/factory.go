package runtime

import (
	"fmt"

	"goated/internal/agent"
	"goated/internal/app"
	"goated/internal/claude"
	"goated/internal/claudetui"
	"goated/internal/codex"
	"goated/internal/codextui"
)

type runtimeImpl struct {
	session    agent.SessionRuntime
	headless   agent.HeadlessRuntime
	descriptor agent.RuntimeDescriptor
}

func (r *runtimeImpl) Session() agent.SessionRuntime {
	return r.session
}

func (r *runtimeImpl) Headless() agent.HeadlessRuntime {
	return r.headless
}

func (r *runtimeImpl) Descriptor() agent.RuntimeDescriptor {
	return r.descriptor
}

func New(cfg app.Config) (agent.Runtime, error) {
	switch agent.RuntimeProvider(cfg.AgentRuntime) {
	case "", agent.RuntimeClaude:
		session := claude.NewSessionRuntime(cfg.WorkspaceDir, cfg.LogDir, cfg.Model)
		headless := claude.NewHeadlessRuntime(cfg.WorkspaceDir, cfg.Model)
		return &runtimeImpl{
			session:    session,
			headless:   headless,
			descriptor: session.Descriptor(),
		}, nil
	case agent.RuntimeClaudeTUI:
		session := claudetui.NewSessionRuntime(cfg.WorkspaceDir, cfg.LogDir)
		headless := claudetui.NewHeadlessRuntime(cfg.WorkspaceDir)
		return &runtimeImpl{
			session:    session,
			headless:   headless,
			descriptor: session.Descriptor(),
		}, nil
	case agent.RuntimeCodex:
		session := codex.NewSessionRuntime(cfg.WorkspaceDir, cfg.LogDir)
		headless := codex.NewHeadlessRuntime(cfg.WorkspaceDir)
		return &runtimeImpl{
			session:    session,
			headless:   headless,
			descriptor: session.Descriptor(),
		}, nil
	case agent.RuntimeCodexTUI:
		session := codextui.NewSessionRuntime(cfg.WorkspaceDir, cfg.LogDir)
		headless := codextui.NewHeadlessRuntime(cfg.WorkspaceDir)
		return &runtimeImpl{
			session:    session,
			headless:   headless,
			descriptor: session.Descriptor(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported GOAT_AGENT_RUNTIME %q (use claude, codex, claude_tui, or codex_tui)", cfg.AgentRuntime)
	}
}
