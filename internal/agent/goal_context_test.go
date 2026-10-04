package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalContextBoundsAndFrontmatter(t *testing.T) {
	workspace := t.TempDir()
	for i := 0; i < 25; i++ {
		dir := filepath.Join(workspace, "self", "GOALS", fmt.Sprintf("goal-%02d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("---\r\nstatus: active\r\nsummary: Keep --- inside YAML intact\r\n---\r\nbody"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got := GoalContext(workspace)
	if strings.Count(got, "[active]") != 20 || strings.Contains(got, "goal-20") || !strings.Contains(got, "Keep --- inside YAML intact") {
		t.Fatalf("incorrect limit/frontmatter: %s", got)
	}
}

func TestGoalContextSkipsAncestorSymlinks(t *testing.T) {
	for _, location := range []string{"self", "GOALS"} {
		t.Run(location, func(t *testing.T) {
			workspace := t.TempDir()
			outside := t.TempDir()
			dir := filepath.Join(outside, "GOALS", "private")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("---\nstatus: active\nsummary: SECRET\n---\n"), 0600); err != nil {
				t.Fatal(err)
			}
			link, target := filepath.Join(workspace, "self"), outside
			if location == "GOALS" {
				if err := os.MkdirAll(link, 0700); err != nil {
					t.Fatal(err)
				}
				link, target = filepath.Join(link, "GOALS"), filepath.Join(outside, "GOALS")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if got := GoalContext(workspace); got != "" {
				t.Fatalf("followed ancestor symlink: %s", got)
			}
		})
	}
}

func TestGoalContextOptInAndStatus(t *testing.T) {
	workspace := t.TempDir()
	if got := GoalContext(workspace); got != "" {
		t.Fatalf("expected off by default, got %q", got)
	}
	self := filepath.Join(workspace, "self")
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(self, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("GOALS/active/GOAL.md", "---\nstatus: active\nsummary: Finish the important work\n---\nprivate details")
	write("GOALS/done/GOAL.md", "---\nstatus: done\nsummary: Finished\n---\n")
	write("GOALS/blocked/GOAL.md", "---\nstatus: blocked\nsummary: Waiting for approval\n---\n")
	write("GOALS/invalid/GOAL.md", "no frontmatter")
	write("DREAM.md", "---\nstatus: active\nsummary: Explore a safer approach\n---\nprivate dream details")
	got := GoalContext(workspace)
	for _, want := range []string{"Finish the important work", "self/GOALS/active/GOAL.md", "Waiting for approval", "[blocked]", "does not itself authorize"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"Finished", "private details", "private dream details", "invalid", "self/DREAM.md", "Explore a safer approach"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("leaked %q: %s", forbidden, got)
		}
	}
}

func TestGoalContextIgnoresSymlinkAndBoundsSummary(t *testing.T) {
	workspace := t.TempDir()
	self := filepath.Join(workspace, "self")
	if err := os.MkdirAll(filepath.Join(self, "GOALS", "a"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\nstatus: active\nsummary: SECRET\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(self, "GOALS", "a", "GOAL.md")); err != nil {
		t.Fatal(err)
	}
	if got := GoalContext(workspace); got != "" {
		t.Fatalf("followed symlink: %q", got)
	}
	long := strings.Repeat("x", 300)
	if err := os.Remove(filepath.Join(self, "GOALS", "a", "GOAL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(self, "GOALS", "a", "GOAL.md"), []byte("---\nstatus: active\nsummary: "+long+"\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := GoalContext(workspace)
	if strings.Contains(got, long) || !strings.Contains(got, "…") {
		t.Fatalf("summary not bounded: %q", got)
	}
}
