package subagent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"goated/internal/db"
)

func TestRunBackgroundCommandPreservesStdinAndFinishRunID(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "goated.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	helperPath := filepath.Join(dir, "finish-helper")
	helperArgsPath := filepath.Join(dir, "helper.args")
	helper := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(helperArgsPath) + "\n"
	if err := os.WriteFile(helperPath, []byte(helper), 0o700); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	oldResolve := resolveExecutable
	resolveExecutable = func() (string, error) { return helperPath, nil }
	t.Cleanup(func() { resolveExecutable = oldResolve })

	stdinOut := filepath.Join(dir, "stdin.out")
	cmd := exec.Command("/bin/sh", "-c", "cat > "+shellQuote(stdinOut))
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("large prompt via stdin")

	logPath := filepath.Join(dir, "run.log")
	result, err := RunBackgroundCommand(store, cmd, RunOpts{
		WorkspaceDir: dir,
		Prompt:       "tracked prompt",
		LogPath:      logPath,
		Source:       "test",
		Runtime: db.ExecutionRuntime{
			Provider: "test",
			Mode:     "headless_exec",
		},
	})
	if err != nil {
		t.Fatalf("RunBackgroundCommand: %v", err)
	}
	if result.PID <= 0 {
		t.Fatalf("PID = %d, want positive", result.PID)
	}

	waitForFile(t, stdinOut)
	stdinData, err := os.ReadFile(stdinOut)
	if err != nil {
		t.Fatalf("read stdin output: %v", err)
	}
	if string(stdinData) != "large prompt via stdin" {
		t.Fatalf("stdin output = %q", string(stdinData))
	}

	waitForFile(t, helperArgsPath)
	argsData, err := os.ReadFile(helperArgsPath)
	if err != nil {
		t.Fatalf("read helper args: %v", err)
	}
	args := strings.Fields(string(argsData))
	runID := flagValue(args, "--run-id")
	if runID == "" {
		t.Fatalf("helper args missing --run-id: %q", string(argsData))
	}
	parsedRunID, err := strconv.ParseUint(runID, 10, 64)
	if err != nil {
		t.Fatalf("run id = %q, want uint: %v", runID, err)
	}

	running, err := store.RunningSubagents()
	if err != nil {
		t.Fatalf("RunningSubagents: %v", err)
	}
	if len(running) != 1 {
		t.Fatalf("running subagents = %d, want 1", len(running))
	}
	if running[0].ID != parsedRunID {
		t.Fatalf("recorded run id = %d, want %d", running[0].ID, parsedRunID)
	}
	if running[0].PID != result.PID {
		t.Fatalf("recorded PID = %d, want %d", running[0].PID, result.PID)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func flagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
