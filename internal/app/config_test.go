package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadConfigResolvesRelativePathsFromConfigFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	mustWriteFile(t, filepath.Join(workspace, "goat"))
	mustWriteFile(t, filepath.Join(workspace, "GOATED.md"))

	config := map[string]any{
		"workspace_dir": "workspace",
		"db_path":       "goated.db",
		"log_dir":       "logs",
		"memory": map[string]any{
			"hooks": []map[string]any{{"enabled": true, "command": []string{"./goat", "memory", "hook", "jev"}}},
		},
		"slack": map[string]any{
			"attachments_root": "workspace/tmp/slack/attachments",
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "goated.json"), data, 0o644); err != nil {
		t.Fatalf("write goated.json: %v", err)
	}

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()

	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}

	cfg := LoadConfig()
	assertSamePath(t, cfg.WorkspaceDir, workspace)
	assertSamePath(t, cfg.DBPath, filepath.Join(root, "goated.db"))
	assertSamePath(t, cfg.LogDir, filepath.Join(root, "logs"))
	if len(cfg.MemoryHooks) != 1 || !cfg.MemoryHooks[0].Enabled || len(cfg.MemoryHooks[0].Command) != 4 {
		t.Fatalf("memory hooks were not loaded: %#v", cfg.MemoryHooks)
	}
	assertSamePath(t, cfg.SlackAttachmentsRoot, filepath.Join(root, "workspace", "tmp", "slack", "attachments"))
}

func TestEnsureLocalBinPathsPrependsExistingDirs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	npmBin := filepath.Join(home, ".npm-global", "bin")
	localBin := filepath.Join(home, ".local", "bin")
	goBin := filepath.Join(home, ".local", "goated-go", "bin")
	for _, dir := range []string{npmBin, localBin, goBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	oldHome, hadHome := os.LookupEnv("HOME")
	oldPath, hadPath := os.LookupEnv("PATH")
	t.Cleanup(func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
		if hadPath {
			_ = os.Setenv("PATH", oldPath)
		} else {
			_ = os.Unsetenv("PATH")
		}
	})

	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	if err := os.Setenv("PATH", strings.Join([]string{"/usr/bin", npmBin}, string(os.PathListSeparator))); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	ensureLocalBinPaths()

	got := filepath.SplitList(os.Getenv("PATH"))
	wantPrefix := []string{localBin, goBin, "/usr/bin", npmBin}
	if len(got) < len(wantPrefix) {
		t.Fatalf("PATH entries = %v, want prefix %v", got, wantPrefix)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("PATH[%d] = %q, want %q (full PATH=%v)", i, got[i], want, got)
		}
	}
}

func TestEnsureLocalBinPathsIsIdempotent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	npmBin := filepath.Join(home, ".npm-global", "bin")
	if err := os.MkdirAll(npmBin, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", npmBin, err)
	}

	oldHome, hadHome := os.LookupEnv("HOME")
	oldPath, hadPath := os.LookupEnv("PATH")
	t.Cleanup(func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
		if hadPath {
			_ = os.Setenv("PATH", oldPath)
		} else {
			_ = os.Unsetenv("PATH")
		}
	})

	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	if err := os.Setenv("PATH", npmBin); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	ensureLocalBinPaths()
	first := os.Getenv("PATH")
	ensureLocalBinPaths()
	second := os.Getenv("PATH")
	if second != first {
		t.Fatalf("ensureLocalBinPaths() changed PATH on second run: first=%q second=%q", first, second)
	}
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

func assertSamePath(t *testing.T, got, want string) {
	t.Helper()

	gotEval, err := evalPathWithMissingLeaf(got)
	if err != nil {
		t.Fatalf("evalPathWithMissingLeaf(%q): %v", got, err)
	}
	wantEval, err := evalPathWithMissingLeaf(want)
	if err != nil {
		t.Fatalf("evalPathWithMissingLeaf(%q): %v", want, err)
	}
	if gotEval != wantEval {
		t.Fatalf("path = %q (%q), want %q (%q)", got, gotEval, want, wantEval)
	}
}

func evalPathWithMissingLeaf(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	parent := absPath
	var tail []string
	for {
		if _, err := os.Stat(parent); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}

		dir, base := filepath.Dir(parent), filepath.Base(parent)
		if dir == parent {
			break
		}
		tail = append([]string{base}, tail...)
		parent = dir
	}

	parentEval, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}

	parts := append([]string{parentEval}, tail...)
	return filepath.Join(parts...), nil
}
