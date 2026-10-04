package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"goated/internal/app"
	"goated/internal/memory"
)

func memoryEngine(cfg app.Config) *memory.Engine {
	var searcher memory.Searcher = memory.FileSearcher{Workspace: cfg.WorkspaceDir}
	if len(cfg.MemorySearchCommand) > 0 {
		searcher = memory.CommandSearcher{Args: cfg.MemorySearchCommand, Dir: cfg.WorkspaceDir, Timeout: 3 * time.Second}
	}
	engine := &memory.Engine{Searcher: searcher, MaxChunks: cfg.MemoryMaxChunks, Parallel: 4, Timeout: 5 * time.Second}
	if cfg.MemoryJevEnabled {
		if key := memory.KeyFromFile(cfg.WorkspaceDir); key != "" {
			engine.Judge = memory.JevJudge{APIKey: key, URL: cfg.MemoryJevURL, Model: cfg.MemoryJevModel, Threshold: cfg.MemoryJevThreshold}
		} else {
			fmt.Fprintln(os.Stderr, "[memory] Jev enabled but TYPESAFE_API_KEY missing; returning unfiltered search results")
		}
	}
	return engine
}

var memoryCmd = &cobra.Command{Use: "memory", Short: "Memory search operations"}
var memorySearchCmd = &cobra.Command{Use: "search QUERY", Args: cobra.ExactArgs(1), Short: "Search the configured memory backend; output source-linked JSON chunks", RunE: func(cmd *cobra.Command, args []string) error {
	cfg := app.LoadConfig()
	var searcher memory.Searcher = memory.FileSearcher{Workspace: cfg.WorkspaceDir}
	if len(cfg.MemorySearchCommand) > 0 {
		searcher = memory.CommandSearcher{Args: cfg.MemorySearchCommand, Dir: cfg.WorkspaceDir, Timeout: 5 * time.Second}
	}
	chunks, err := searcher.Search(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(chunks)
}}

func init() { memoryCmd.AddCommand(memorySearchCmd); rootCmd.AddCommand(memoryCmd) }
