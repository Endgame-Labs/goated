package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"goated/internal/agent"
)

func Validate(ctx context.Context, rt agent.Runtime, workspaceDir string) error {
	// Binary check applies to all runtimes
	switch rt.Descriptor().Provider {
	case agent.RuntimeClaude, agent.RuntimeClaudeTUI:
		if _, err := exec.LookPath("claude"); err != nil {
			return fmt.Errorf("claude binary not found on PATH: %w", err)
		}
	case agent.RuntimeCodexTUI:
		if _, err := exec.LookPath("codex"); err != nil {
			return fmt.Errorf("codex binary not found on PATH: %w", err)
		}
	}

	// Tmux and workspace validation only for interactive (TUI) runtimes
	if rt.Descriptor().Capabilities.SupportsInteractiveSession {
		if _, err := exec.LookPath("tmux"); err != nil {
			return fmt.Errorf("tmux is required: %w", err)
		}
		if workspaceDir == "" {
			return fmt.Errorf("workspace directory is not configured")
		}
		if err := validateWorkspace(workspaceDir); err != nil {
			return err
		}
	}

	if err := rt.Session().EnsureSession(ctx); err != nil {
		return err
	}
	health, err := rt.Session().GetHealth(ctx)
	if err != nil {
		return err
	}
	if !health.OK && !health.Recoverable {
		return fmt.Errorf("%s", health.Summary)
	}
	return nil
}

func validateWorkspace(workspaceDir string) error {
	info, err := os.Stat(workspaceDir)
	if err != nil {
		return fmt.Errorf("workspace directory %s is not accessible: %w", workspaceDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace path %s is not a directory", workspaceDir)
	}
	if err := requireExecutable(filepath.Join(workspaceDir, "goat")); err != nil {
		return err
	}
	if err := requireFile(filepath.Join(workspaceDir, "GOATED.md")); err != nil {
		return err
	}
	return nil
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required file %s is missing: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("required file %s is a directory", path)
	}
	return nil
}

func requireExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required executable %s is missing: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("required executable %s is a directory", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("required executable %s is not executable", path)
	}
	return nil
}
