package claudetui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// credentialsState is what the on-disk Claude Code OAuth credentials can tell
// us about auth, independent of whatever the TUI has rendered.
type credentialsState int

const (
	// credentialsUnknown means no readable credential file — e.g. macOS
	// Keychain storage, API-key auth, or a corrupt file. The live auth probe,
	// rather than this local metadata, determines whether pane text is stale.
	credentialsUnknown credentialsState = iota
	// credentialsValid means an unexpired OAuth token is on disk.
	credentialsValid
	// credentialsExpired means the on-disk token is past its expiry. Claude
	// Code may still auto-refresh it with the refresh token, so the live probe
	// remains authoritative and this state is diagnostic only.
	credentialsExpired
)

// oauthCredentialsState reports the state of the Claude Code OAuth token on
// disk. Claude Code stores it at $CLAUDE_CONFIG_DIR/.credentials.json
// (default ~/.claude/.credentials.json) with expiresAt in epoch milliseconds.
func oauthCredentialsState(now time.Time) credentialsState {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return credentialsUnknown
		}
		dir = filepath.Join(home, ".claude")
	}
	return oauthCredentialsStateAt(filepath.Join(dir, ".credentials.json"), now)
}

func oauthCredentialsStateAt(path string, now time.Time) credentialsState {
	data, err := os.ReadFile(path)
	if err != nil {
		return credentialsUnknown
	}
	var creds struct {
		ClaudeAiOauth struct {
			ExpiresAt int64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil || creds.ClaudeAiOauth.ExpiresAt == 0 {
		return credentialsUnknown
	}
	if time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt).After(now) {
		return credentialsValid
	}
	return credentialsExpired
}
