package cli

import (
	"goated/internal/app"
	"goated/internal/memory"
	"testing"
)

func TestMemoryEngineDefaultsToFiles(t *testing.T) {
	engine := memoryEngine(app.Config{WorkspaceDir: t.TempDir()})
	if _, ok := engine.Searcher.(memory.FileSearcher); !ok {
		t.Fatalf("default searcher %T", engine.Searcher)
	}
}
func TestMemoryEngineUsesConfiguredCommand(t *testing.T) {
	engine := memoryEngine(app.Config{WorkspaceDir: t.TempDir(), MemorySearchCommand: []string{"custom-search", "{query}"}})
	if _, ok := engine.Searcher.(memory.CommandSearcher); !ok {
		t.Fatalf("override searcher %T", engine.Searcher)
	}
}
func TestMemoryEngineHooksDisabledByDefault(t *testing.T) {
	engine := memoryEngine(app.Config{WorkspaceDir: t.TempDir(), MemoryHooks: []app.MemoryHookConfig{{Enabled: false, Command: []string{"cat"}}}})
	if len(engine.Hooks) != 0 {
		t.Fatalf("unexpected hooks: %d", len(engine.Hooks))
	}
}
func TestMemoryEngineConfiguredHooks(t *testing.T) {
	engine := memoryEngine(app.Config{WorkspaceDir: t.TempDir(), MemoryHooks: []app.MemoryHookConfig{{Enabled: true, Command: []string{"cat"}}}})
	if len(engine.Hooks) != 1 {
		t.Fatalf("hooks: %d", len(engine.Hooks))
	}
}
