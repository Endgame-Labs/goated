package agentlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"toolbox-example/internal/workspace"
)

// Init tees stdout/stderr to logs/<tool>/<date>.log under the self repo.
func Init(toolName string) (func(), error) {
	selfDir, err := workspace.SelfDirFromExecutable()
	if err != nil {
		return nil, err
	}

	logDir := filepath.Join(selfDir, "logs", toolName)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir log dir: %w", err)
	}

	path := filepath.Join(logDir, time.Now().UTC().Format("2006-01-02")+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	fmt.Fprintf(f, "\n=== %s invoked at %s ===\n", toolName, time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(f, "args: %v\n", os.Args)

	origStdout := os.Stdout
	origStderr := os.Stderr

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("pipe stdout: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		f.Close()
		return nil, fmt.Errorf("pipe stderr: %w", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	go func() {
		_, _ = io.Copy(io.MultiWriter(origStdout, f), stdoutR)
	}()
	go func() {
		_, _ = io.Copy(io.MultiWriter(origStderr, f), stderrR)
	}()

	cleanup := func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = stdoutW.Close()
		_ = stderrW.Close()
		time.Sleep(50 * time.Millisecond)
		_ = f.Close()
	}

	return cleanup, nil
}
