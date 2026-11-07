// Package authnotice tells the admin chat once when the agent's Claude login
// has expired, and once more when it works again (#12). Chat dispatch and cron
// jobs both report into one Notifier, so an expiry produces a single notice
// however many messages or jobs hit it.
package authnotice

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Patterns are output strings that mean a Claude request failed on auth.
var Patterns = []string{
	"API Error: 401",
	"authentication_error",
	"OAuth token has expired",
	"Please run /login",
}

// Match returns the first auth-failure pattern found in text, or "".
func Match(text string) string {
	for _, pat := range Patterns {
		if strings.Contains(text, pat) {
			return pat
		}
	}
	return ""
}

// Notifier sends the expired and restored notices. A nil *Notifier, or one
// without Send or AdminChatID, records state but sends nothing.
type Notifier struct {
	Send        func(ctx context.Context, chatID, text string) error
	AdminChatID string
	SessionName string // tmux session where /login is run

	mu      sync.Mutex
	expired bool
}

const sendTimeout = 15 * time.Second

// Expired records a confirmed auth failure. Only the first call after the
// login last worked sends a notice.
func (n *Notifier) Expired(ctx context.Context) {
	if n == nil || !n.transition(true) {
		return
	}
	attach := "attach to the agent's tmux session on the server"
	if n.SessionName != "" {
		attach = fmt.Sprintf("run `tmux attach -t %s` on the server", n.SessionName)
	}
	n.send(ctx, "Claude Code's login has expired, so messages and cron jobs can't run. "+
		"To fix it, "+attach+", then type /login. "+
		"Cron runs missed in the meantime won't be replayed: each job runs again at its next scheduled time.")
}

// Restored records that a Claude request succeeded. It sends a notice only
// if an expiry was reported before.
func (n *Notifier) Restored(ctx context.Context) {
	if n == nil || !n.transition(false) {
		return
	}
	n.send(ctx, "Claude Code's login works again. Messages and cron jobs are running normally.")
}

// transition sets the state and reports whether it changed.
func (n *Notifier) transition(expired bool) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.expired == expired {
		return false
	}
	n.expired = expired
	return true
}

func (n *Notifier) send(ctx context.Context, text string) {
	fmt.Fprintf(os.Stderr, "[%s] auth notice: %s\n", time.Now().Format(time.RFC3339), text)
	if n.Send == nil || n.AdminChatID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	if err := n.Send(ctx, n.AdminChatID, text); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] auth notice failed: %v\n", time.Now().Format(time.RFC3339), err)
	}
}
