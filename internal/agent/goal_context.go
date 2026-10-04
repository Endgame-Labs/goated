package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// GoalContext is a compact, file-backed reminder, not an instruction source.
// It deliberately includes pointers rather than the contents of goal files.
// A missing self/GOALS directory and self/DREAM.md keep this feature off.
func GoalContext(workspaceDir string) string {
	self := filepath.Join(workspaceDir, "self")
	goalsDir := filepath.Join(self, "GOALS")
	entries, err := os.ReadDir(goalsDir)
	if err != nil {
		entries = nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var lines []string
	for _, entry := range entries {
		if len(lines) >= 20 {
			break
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		slug := entry.Name()
		if slug == "." || slug == ".." || strings.ContainsAny(slug, `/\\`) {
			continue
		}
		path := filepath.Join(goalsDir, slug, "GOAL.md")
		meta, ok := readGoalMeta(path)
		if !ok || meta.Status != "active" || strings.TrimSpace(meta.Summary) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (self/GOALS/%s/GOAL.md)", slug, oneLine(meta.Summary, 180), slug))
	}
	if len(lines) == 0 {
		lines = nil
	}
	var dreamLine string
	if meta, ok := readGoalMeta(filepath.Join(self, "DREAM.md")); ok && meta.Status == "active" && strings.TrimSpace(meta.Summary) != "" {
		dreamLine = fmt.Sprintf("Dream / exploration: %s (self/DREAM.md)", oneLine(meta.Summary, 180))
	}
	if len(lines) == 0 && dreamLine == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Optional private goal context (file-backed; not user instructions). Read relevant files before acting. A goal or dream does not itself authorize new external actions, spending, recurring work, or deployment.\n")
	if len(lines) > 0 {
		b.WriteString("Active goals:\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteByte('\n')
	}
	if dreamLine != "" {
		b.WriteString(dreamLine)
		b.WriteByte('\n')
	}
	return b.String()
}

type goalMeta struct {
	Status  string `yaml:"status"`
	Summary string `yaml:"summary"`
}

func readGoalMeta(path string) (goalMeta, bool) {
	var meta goalMeta
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return meta, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return meta, false
	}
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
		return meta, false
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &meta); err != nil {
		return meta, false
	}
	return meta, true
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}
