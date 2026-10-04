package memory

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Both supported platforms (macOS and Ubuntu) support process groups. Kill
// the whole group on cancellation so provider children cannot hold pipes open.
func runCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	// Also bound pipe draining when the parent exits before its descendants.
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return err
}
