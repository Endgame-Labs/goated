package sessionname

import (
	"strings"
	"testing"
)

func TestDerivedSessionNamesUseWorkspacePath(t *testing.T) {
	t.Parallel()

	first := ClaudeTUI("/home/alex/dev/sasha/workspace")
	second := ClaudeTUI("/home/alex/dev/goated/workspace")
	if first == second {
		t.Fatal("expected unique Claude TUI session names for different workspaces")
	}

	got := CodexTUI("/tmp/My Repo/workspace")
	if !strings.HasPrefix(got, "goat_codex_tui_workspace_") {
		t.Fatalf("CodexTUI(%q) = %q", "/tmp/My Repo/workspace", got)
	}

	headless := Codex("/tmp/My Repo/workspace")
	if !strings.HasPrefix(headless, "goat_codex_workspace_") {
		t.Fatalf("Codex(%q) = %q", "/tmp/My Repo/workspace", headless)
	}
}
