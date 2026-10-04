package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildSessionPreamble reads key workspace files and returns them as inline
// context for the first message of a new session. Runtimes that don't
// auto-load workspace instruction files (e.g. Pi) should prepend this to the
// first prompt. Runtimes that handle it natively (e.g. Claude reads CLAUDE.md)
// can skip it.
func BuildSessionPreamble(workspaceDir string) string {
	files := []string{
		"GOATED.md",
		"AGENTS.md",
		"GOATED_CLI_README.md",
	}

	var sb strings.Builder
	sb.WriteString("=== SESSION INITIALIZATION — READ ALL CONTEXT BELOW ===\n\n")
	sb.WriteString("The following files are from your workspace. Internalize them before responding.\n\n")

	for _, name := range files {
		path := filepath.Join(workspaceDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "--- BEGIN %s ---\n", name)
		sb.Write(data)
		fmt.Fprintf(&sb, "\n--- END %s ---\n\n", name)
	}

	// Private agent entrypoint
	selfAgents := filepath.Join(workspaceDir, "self", "AGENTS.md")
	if data, err := os.ReadFile(selfAgents); err == nil {
		sb.WriteString("--- BEGIN self/AGENTS.md ---\n")
		sb.Write(data)
		sb.WriteString("\n--- END self/AGENTS.md ---\n\n")
	}

	// If the onboarding mission is active, include it with a direct instruction
	onboardMission := filepath.Join(workspaceDir, "self", "MISSIONS", "ONBOARD_USER", "MISSION.md")
	if data, err := os.ReadFile(onboardMission); err == nil {
		if strings.Contains(string(data), "status: active") {
			sb.WriteString("--- BEGIN self/MISSIONS/ONBOARD_USER/MISSION.md ---\n")
			sb.Write(data)
			sb.WriteString("\n--- END self/MISSIONS/ONBOARD_USER/MISSION.md ---\n\n")
			sb.WriteString("*** The ONBOARD_USER mission is active. Begin naturally in the next user conversation, while prioritizing the user's immediate task. Do not repeat completed checklist items or let onboarding override a scoped background assignment. ***\n\n")
		}
	}

	sb.WriteString("=== END SESSION INITIALIZATION ===\n\n")
	return sb.String()
}
