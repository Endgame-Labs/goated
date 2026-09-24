package claudetui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// authProbeResult is the verdict of a headless auth probe: an actual request
// through the claude CLI, which is definitive in a way pane text and on-disk
// expiry timestamps are not (a token can be revoked server-side while still
// looking valid on disk).
type authProbeResult int

const (
	// authProbeInconclusive means the probe could not run or failed for a
	// non-auth reason (timeout, network, binary missing). Never cached.
	authProbeInconclusive authProbeResult = iota
	// authProbeOK means a headless request succeeded — credentials work.
	authProbeOK
	// authProbeFailed means the probe hit an auth error — a manual /login
	// really is required.
	authProbeFailed
)

// positiveProbeTTL bounds how long a passing probe is trusted without
// re-verification (and therefore how long a token revoked right after a
// passing probe could be masked).
const positiveProbeTTL = 15 * time.Minute

// negativeProbeTTL is short so a manual /login is noticed quickly.
const negativeProbeTTL = time.Minute

// probeRenewWindow keeps the common case cheap: if a verified-OK verdict
// expired while stale auth text was still on screen, classifySessionState
// would flip to BlockedAuth in the middle of a healthy long-running task.
// GetHealth's probing path treats a positive verdict with less than this
// much life left as a miss and re-probes, so a dispatch it admits starts
// with at least this margin — above one 5-min gateway post-send polling
// window plus paste/context-estimate/session-respawn overheads. Dispatches
// that outlast even this (the gateway chains up to three polling windows on
// retries) are covered by verifyBlockedAuth, which re-verifies an apparent
// auth block at the point it would otherwise be surfaced. cachedAuthState
// (the polling hot path) honors the verdict until actual expiry.
const probeRenewWindow = 8 * time.Minute

// runClaudeAuthProbe issues a minimal headless request, using the same
// invocation conventions as internal/subagent (flags, workspace dir,
// CLAUDECODE filtering).
func runClaudeAuthProbe(ctx context.Context, workspaceDir string) authProbeResult {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--dangerously-skip-permissions", "-p", "Reply with the single word: ok")
	cmd.Dir = workspaceDir
	cmd.Env = envWithoutClaudeCode()
	// The timeout must be enforceable: claude spawns children (MCP servers,
	// shell tools) that inherit the output pipes, and killing only the direct
	// process would leave CombinedOutput blocked on pipe EOF indefinitely.
	// Run the probe in its own process group, kill the whole group on
	// cancellation, and cap the post-kill pipe drain with WaitDelay.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if err == nil {
		return authProbeOK
	}
	if matchPattern(string(out), authErrorPatterns) != "" {
		return authProbeFailed
	}
	return authProbeInconclusive
}

func envWithoutClaudeCode() []string {
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "CLAUDECODE=") {
			kept = append(kept, kv)
		}
	}
	return kept
}

// verifiedAuthState returns the probe verdict, probing at most once per cache
// window. Probes are serialized on probeRunMu; the cache itself is guarded
// only by probeMu, which is never held across a probe — so cachedAuthState
// and invalidateAuthProbe stay wait-free even while a probe is in flight.
// Inconclusive results are never cached.
func (b *TmuxBridge) verifiedAuthState(ctx context.Context) authProbeResult {
	if res, ok := b.probingVerdict(); ok {
		return res
	}
	b.probeRunMu.Lock()
	defer b.probeRunMu.Unlock()
	// Another caller may have completed a probe while we waited.
	if res, ok := b.probingVerdict(); ok {
		return res
	}
	probe := b.authProbe
	if probe == nil {
		probe = runClaudeAuthProbe
	}
	res := probe(ctx, b.WorkspaceDir)
	b.probeMu.Lock()
	switch res {
	case authProbeOK:
		b.probeVerdict, b.probeExpiry = res, b.now().Add(positiveProbeTTL)
	case authProbeFailed:
		b.probeVerdict, b.probeExpiry = res, b.now().Add(negativeProbeTTL)
	}
	b.probeMu.Unlock()
	return res
}

// probingVerdict is the cache check for the probing path: a positive verdict
// about to expire within probeRenewWindow reads as a miss so it gets renewed
// before a dispatch relies on it. Negative verdicts are honored to actual
// expiry — their TTL is already short.
func (b *TmuxBridge) probingVerdict() (authProbeResult, bool) {
	b.probeMu.Lock()
	defer b.probeMu.Unlock()
	now := b.now()
	if !now.Before(b.probeExpiry) {
		return authProbeInconclusive, false
	}
	if b.probeVerdict == authProbeOK && !now.Add(probeRenewWindow).Before(b.probeExpiry) {
		return authProbeInconclusive, false
	}
	return b.probeVerdict, true
}

func (b *TmuxBridge) freshVerdict() (authProbeResult, bool) {
	b.probeMu.Lock()
	defer b.probeMu.Unlock()
	if b.now().Before(b.probeExpiry) {
		return b.probeVerdict, true
	}
	return authProbeInconclusive, false
}

// cachedAuthState returns the last unexpired probe verdict without probing
// and without waiting on any in-flight probe — safe for hot paths like
// GetSessionState polling loops.
func (b *TmuxBridge) cachedAuthState() authProbeResult {
	res, _ := b.freshVerdict()
	return res
}

func (b *TmuxBridge) invalidateAuthProbe() {
	b.probeMu.Lock()
	b.probeExpiry = time.Time{}
	b.probeMu.Unlock()
}

// verifyBlockedAuth re-checks an apparent auth block before it is surfaced to
// callers that act on it (user-facing "login expired" messages, aborted
// dispatches). Only authProbeFailed confirms the block. authProbeOK proves the
// pane text stale, while authProbeInconclusive must remain ambiguous rather
// than being promoted to a false manual-login requirement. The real request is
// authoritative for refreshed OAuth, Keychain, and API-key credentials, so
// local credential-file state must not gate it.
func (b *TmuxBridge) verifyBlockedAuth(ctx context.Context) authProbeResult {
	return b.verifiedAuthState(ctx)
}
