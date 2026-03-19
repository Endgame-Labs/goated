package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/msglog"
)

// hookContentKeys aliases the exported msglog.HookContentKeys for local use.
var hookContentKeys = msglog.HookContentKeys

var logHookCmd = &cobra.Command{
	Use:    "log-hook",
	Short:  "Log a Claude Code hook event (internal, reads JSON from stdin)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		eventName, _ := cmd.Flags().GetString("event")
		hookDir, _ := cmd.Flags().GetString("hook-dir")
		credsDir, _ := cmd.Flags().GetString("creds-dir")

		if eventName == "" || hookDir == "" || credsDir == "" {
			return fmt.Errorf("--event, --hook-dir, and --creds-dir are required")
		}

		// Read the hook JSON from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}

		// Create a redactor from the creds dir. For events that may carry
		// credential values (tool use, user prompts), sleep first to let
		// recently-written creds settle on disk before reading them.
		needsCredSleep := true
		switch eventName {
		case "SessionStart", "SessionEnd", "Stop", "Notification", "SubagentStart", "SubagentStop", "TeammateIdle":
			needsCredSleep = false
		}
		if needsCredSleep {
			time.Sleep(15 * time.Second)
		}
		redactor := msglog.NewRedactor(credsDir)

		// Extract inline cred values from the raw JSON — catches new cred values
		// that haven't been written to disk yet (e.g. "goat creds set foo SECRET"
		// in PreToolUse or UserPromptSubmit events).
		if inline := extractInlineCredValues(data); len(inline) > 0 {
			redactor.AddSecrets(inline)
		}

		// Parse body into map for selective redaction
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			// If we can't parse, redact the whole thing as a string fallback
			redacted := redactor.Redact(string(data))
			return writeHookEntry(hookDir, eventName, json.RawMessage(redacted))
		}

		// Selectively redact only content containers
		redactor.RedactMapContainers(body, hookContentKeys)

		bodyJSON, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("re-marshal body: %w", err)
		}

		if err := writeHookEntry(hookDir, eventName, bodyJSON); err != nil {
			return err
		}

		// For Stop events, also write last_stop.json with redacted content
		if eventName == "Stop" {
			stopPath := filepath.Join(hookDir, "last_stop.json")
			if err := os.WriteFile(stopPath, bodyJSON, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "[log-hook] warning: write last_stop.json: %v\n", err)
			}
		}

		return nil
	},
}

// writeHookEntry builds the {event, ts, body} wrapper and appends to
// hooks/YYYY-MM-DD.jsonl under file lock.
func writeHookEntry(hookDir, eventName string, bodyJSON json.RawMessage) error {
	hooksLogDir := filepath.Join(hookDir, "hooks")
	if err := os.MkdirAll(hooksLogDir, 0o755); err != nil {
		return fmt.Errorf("mkdir hooks: %w", err)
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	entry := map[string]any{
		"event": eventName,
		"ts":    ts,
		"body":  json.RawMessage(bodyJSON),
	}

	// Use Encoder with SetEscapeHTML(false) to avoid encoding > as \u003e
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(entry); err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	line := bytes.TrimRight(buf.Bytes(), "\n")

	dailyFile := filepath.Join(hooksLogDir, time.Now().UTC().Format("2006-01-02")+".jsonl")

	fl := msglog.NewFileLock(dailyFile)
	if err := fl.Lock(); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	defer fl.Unlock()

	f, err := os.OpenFile(dailyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", dailyFile, err)
	}
	defer f.Close()

	_, err = f.WriteString(string(line) + "\n")
	return err
}

// credsSetPattern matches "goat creds set <key> <value>" in raw text.
// Captures the value argument (group 1).
var credsSetPattern = regexp.MustCompile(`(?:goat|\.\/goat)\s+creds\s+set\s+\S+\s+(\S+)`)

// extractInlineCredValues scans raw hook JSON for "goat creds set <key> <value>"
// patterns and returns the values. This catches new credential values that appear
// in content (PreToolUse commands, UserPromptSubmit prompts) before they've been
// written to the creds directory on disk.
func extractInlineCredValues(data []byte) []string {
	matches := credsSetPattern.FindAllSubmatch(data, -1)
	seen := make(map[string]bool, len(matches))
	var values []string
	for _, m := range matches {
		val := strings.TrimRight(string(m[1]), `"'\`)
		if !seen[val] {
			values = append(values, val)
			seen[val] = true
		}
	}
	return values
}

func init() {
	logHookCmd.Flags().String("event", "", "Hook event name (e.g. SessionStart, Stop)")
	logHookCmd.Flags().String("hook-dir", "", "Directory for hook output files")
	logHookCmd.Flags().String("creds-dir", "", "Path to creds directory for redaction")
	rootCmd.AddCommand(logHookCmd)
}
