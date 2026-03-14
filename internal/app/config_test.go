package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultWorkspaceDir(t *testing.T) {
	t.Parallel()

	t.Run("prefers workspace child when present", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		mustWriteFile(t, filepath.Join(root, "workspace", "goat"))

		got := defaultWorkspaceDir(root, "")
		want := filepath.Join(root, "workspace")
		if got != want {
			t.Fatalf("defaultWorkspaceDir() = %q, want %q", got, want)
		}
	})

	t.Run("uses cwd when cwd is already workspace", func(t *testing.T) {
		t.Parallel()

		workspace := t.TempDir()
		mustWriteFile(t, filepath.Join(workspace, "goat"))
		mustWriteFile(t, filepath.Join(workspace, "GOATED.md"))

		if got := defaultWorkspaceDir(workspace, ""); got != workspace {
			t.Fatalf("defaultWorkspaceDir() = %q, want %q", got, workspace)
		}
	})
}

func TestDefaultBaseDir(t *testing.T) {
	t.Parallel()

	t.Run("uses repo root when workspace child exists", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		mustWriteFile(t, filepath.Join(root, "workspace", "goat"))

		if got := defaultBaseDir(root, ""); got != root {
			t.Fatalf("defaultBaseDir() = %q, want %q", got, root)
		}
	})

	t.Run("uses parent when executable lives in workspace", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		exeDir := filepath.Join(root, "workspace")

		if got := defaultBaseDir("", exeDir); got != root {
			t.Fatalf("defaultBaseDir() = %q, want %q", got, root)
		}
	})
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
