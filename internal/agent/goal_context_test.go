package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	write("GOALS/done/GOAL.md", "---\nstatus: complete\nsummary: Finished\n---\n")
	write("GOALS/invalid/GOAL.md", "no frontmatter")
	write("DREAM.md", "---\nstatus: active\nsummary: Explore a safer approach\n---\nprivate dream details")
	got := GoalContext(workspace)
	for _, want := range []string{"Finish the important work", "self/GOALS/active/GOAL.md", "Explore a safer approach", "self/DREAM.md", "does not itself authorize"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"Finished", "private details", "private dream details", "invalid"} {
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
