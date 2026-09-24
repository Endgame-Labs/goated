// Package adminctl implements owner-only "/admin ..." control commands that a
// connector handles directly — before a message is routed to the runtime — so
// they work as a remote escape hatch even when the runtime session is wedged or
// busy. It is connector-agnostic: connectors parse the command, call Execute,
// send back Result.Reply, and run Result.After (if any).
package adminctl

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const helpText = "*Admin commands* (owner only)\n" +
	"`/admin status` — daemon pid, host, time\n" +
	"`/admin restart` — restart the daemon\n" +
	"`/admin help` — this message"

// Result is what a connector should do after Execute: send Reply to the owner,
// then call After (if non-nil). After is separated from Execute so the reply is
// delivered before a self-terminating action like restart kills the process.
type Result struct {
	Reply string
	After func()
}

// Parse reports whether text is an "/admin ..." invocation and returns the
// lower-cased subcommand ("help" when bare "/admin"). It does not authorize the
// caller — the connector must confirm the message came from the owner first.
func Parse(text string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 || fields[0] != "/admin" {
		return "", false
	}
	if len(fields) == 1 {
		return "help", true
	}
	return strings.ToLower(fields[1]), true
}

// Execute runs an admin subcommand and returns the reply (and optional deferred
// action). It assumes the caller is already authorized as the owner.
func Execute(sub string) Result {
	switch sub {
	case "help":
		return Result{Reply: helpText}
	case "status":
		return Result{Reply: statusText()}
	case "restart":
		return Result{
			Reply: "🔄 Restarting daemon… back in a moment.",
			After: restartDaemon,
		}
	default:
		return Result{Reply: "Unknown admin command `" + sub + "`.\n\n" + helpText}
	}
}

func statusText() string {
	host, _ := os.Hostname()
	return fmt.Sprintf(
		"✅ Daemon alive and responsive\npid: %d\nhost: %s\ntime: %s",
		os.Getpid(), host, time.Now().Format(time.RFC3339),
	)
}

// restartDaemon launches a detached "goated daemon restart". It survives the
// SIGTERM that the restart sends to this (the current) daemon.
func restartDaemon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "daemon", "restart", "--reason", "owner remote /admin restart")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = cmd.Start()
	// Detached on purpose — do not Wait. The restart will SIGTERM this process.
}
