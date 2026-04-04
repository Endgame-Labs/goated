package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// SelfDirFromExecutable resolves the enclosing self repo from a binary located
// at self/tools/<binary>.
func SelfDirFromExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks: %w", err)
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// WorkspaceDir returns the parent workspace directory that contains self/.
func WorkspaceDir() (string, error) {
	selfDir, err := SelfDirFromExecutable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(selfDir), nil
}

// GoatPath returns the expected path to workspace/goat.
func GoatPath() (string, error) {
	workspaceDir, err := WorkspaceDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(workspaceDir, "goat"), nil
}
