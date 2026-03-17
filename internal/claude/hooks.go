package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// allHookEvents lists the Claude Code lifecycle hook events that are valid
// in the current CLI version. Invalid event names poison the entire config,
// silently disabling ALL hooks.
//
// Known invalid (as of Claude Code ~1.x): PostCompact, InstructionsLoaded,
// ConfigChange, WorktreeCreate, WorktreeRemove, Elicitation, ElicitationResult.
var allHookEvents = []string{
	"SessionStart",
	"SessionEnd",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"PermissionRequest",
	"Notification",
	"SubagentStart",
	"SubagentStop",
	"Stop",
	"PreCompact",
	"TaskCompleted",
	"TeammateIdle",
}

// StopEvent represents the JSON payload written by the Claude Code Stop hook.
type StopEvent struct {
	SessionID            string `json:"session_id"`
	LastAssistantMessage string `json:"last_assistant_message"`
	StopHookActive       bool   `json:"stop_hook_active"`
}

// hooksConfig is the structure written to workspace/.claude/settings.local.json.
type hooksConfig struct {
	Hooks map[string][]hookMatcher `json:"hooks"`
}

type hookMatcher struct {
	Matcher string     `json:"matcher"`
	Hooks   []hookSpec `json:"hooks"`
}

type hookSpec struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// logCommand builds a shell command that pipes hook JSON from stdin into
// `./goat log-hook`, which handles sleeping (to let creds settle), redaction,
// timestamping, and appending to the daily JSONL file.
//
// Since cmd.Dir is workspace/, we use ./goat directly.
func logCommand(hookDir, credsDir, eventName string) string {
	return fmt.Sprintf(
		`sh -c 'cat | ./goat log-hook --event %s --hook-dir %s --creds-dir %s'`,
		eventName, hookDir, credsDir,
	)
}

// hooksSettingsPath returns the path where hook config will be written.
// Written to workspace/.claude/settings.local.json since cmd.Dir is workspace/.
func hooksSettingsPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".claude", "settings.local.json")
}

// writeHooksConfig writes the Claude Code hooks configuration to
// workspace/.claude/settings.local.json. Loaded via --settings flag since
// Claude Code discovers settings relative to the git root, not cwd.
// Every hook event is logged via `./goat log-hook` which handles redaction,
// timestamping, and appending to $LOG_DIR/claude_session/hooks/YYYY-MM-DD.jsonl.
// The Stop event is also written to last_stop.json for quick state access.
func writeHooksConfig(workspaceDir, hookDir, credsDir string) error {
	claudeDir := filepath.Join(workspaceDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("mkdir .claude: %w", err)
	}

	hooks := make(map[string][]hookMatcher, len(allHookEvents))
	for _, event := range allHookEvents {
		hooks[event] = []hookMatcher{
			{
				Matcher: "",
				Hooks: []hookSpec{
					{
						Type:    "command",
						Command: logCommand(hookDir, credsDir, event),
					},
				},
			},
		}
	}

	cfg := hooksConfig{Hooks: hooks}

	// Use json.Encoder with SetEscapeHTML(false) to avoid encoding > as \u003e
	// which Claude Code doesn't handle correctly in hook commands.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("marshal hooks config: %w", err)
	}

	path := hooksSettingsPath(workspaceDir)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readLastStop reads and parses the last Stop hook event from the hook dir.
func readLastStop(hookDir string) (*StopEvent, error) {
	path := filepath.Join(hookDir, "last_stop.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ev StopEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("parse last_stop.json: %w", err)
	}
	return &ev, nil
}
