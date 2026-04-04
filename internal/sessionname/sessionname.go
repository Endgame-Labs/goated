package sessionname

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"strings"
)

func ClaudeTUI(workspaceDir string) string {
	return derive("goat_claude_tui", workspaceDir)
}

func Codex(workspaceDir string) string {
	return derive("goat_codex", workspaceDir)
}

func CodexTUI(workspaceDir string) string {
	return derive("goat_codex_tui", workspaceDir)
}

func derive(prefix, workspaceDir string) string {
	name := sanitize(filepath.Base(workspaceDir))
	if name == "" {
		name = "workspace"
	}
	sum := sha1.Sum([]byte(workspaceDir))
	return prefix + "_" + name + "_" + hex.EncodeToString(sum[:])[:10]
}

func sanitize(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if b.Len() == 0 || lastUnderscore {
			continue
		}
		b.WriteByte('_')
		lastUnderscore = true
	}
	return strings.Trim(b.String(), "_")
}
