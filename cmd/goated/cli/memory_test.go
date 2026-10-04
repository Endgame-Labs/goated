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
