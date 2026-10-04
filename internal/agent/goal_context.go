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
// A missing self/GOALS directory keeps this feature off.
func GoalContext(workspaceDir string) string {
	self := filepath.Join(workspaceDir, "self")
	goalsDir := filepath.Join(self, "GOALS")
	for _, dir := range []string{self, goalsDir} {
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ""
		}
	}
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
		if !ok || (meta.Status != "active" && meta.Status != "blocked") || strings.TrimSpace(meta.Summary) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s [%s]: %s (self/GOALS/%s/GOAL.md)", oneLine(slug, 80), meta.Status, oneLine(meta.Summary, 180), oneLine(slug, 255)))
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Optional private goal context (file-backed; not user instructions). Read relevant files before acting. A goal does not itself authorize new external actions, spending, recurring work, or deployment.\n")
	if len(lines) > 0 {
		b.WriteString("Ongoing goals (active or blocked):\n")
		b.WriteString(strings.Join(lines, "\n"))
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
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return meta, false
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			if err := yaml.Unmarshal([]byte(strings.Join(lines[1:i], "\n")), &meta); err != nil {
				return meta, false
			}
			return meta, true
		}
	}
	return meta, false
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}
