package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"goated/internal/app"
	"goated/internal/memory"
)

func memorySearcher(cfg app.Config) memory.Searcher {
	if len(cfg.MemorySearchCommand) > 0 {
		return memory.CommandSearcher{Args: cfg.MemorySearchCommand, Dir: cfg.WorkspaceDir, Timeout: 3 * time.Second}
	}
	return memory.FileSearcher{Workspace: cfg.WorkspaceDir}
}
func memoryEngine(cfg app.Config) *memory.Engine {
	engine := &memory.Engine{Searcher: memorySearcher(cfg), MaxChunks: cfg.MemoryMaxChunks}
	for _, hook := range cfg.MemoryHooks {
		if hook.Enabled && len(hook.Command) > 0 {
			engine.Hooks = append(engine.Hooks, memory.CommandHook{Args: hook.Command, Dir: cfg.WorkspaceDir, Timeout: 5 * time.Second})
		}
	}
	return engine
}

var memoryCmd = &cobra.Command{Use: "memory", Short: "Memory search and hook operations"}
var memorySearchCmd = &cobra.Command{Use: "search QUERY", Args: cobra.ExactArgs(1), Short: "Search memory and emit a validated JSON result array", RunE: func(cmd *cobra.Command, args []string) error {
	cfg := app.LoadConfig()
	chunks, err := memorySearcher(cfg).Search(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	if chunks == nil {
		chunks = []memory.Chunk{}
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(chunks)
}}
var memoryHookCmd = &cobra.Command{Use: "hook", Short: "Array-in/array-out memory hooks"}
var memoryJevCmd = &cobra.Command{Use: "jev", Args: cobra.NoArgs, Short: "Filter a JSON result array with Jev (opt-in hook command)", RunE: func(cmd *cobra.Command, _ []string) error {
	cfg := app.LoadConfig()
	key := memory.KeyFromFile(cfg.WorkspaceDir)
	input, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	if err != nil {
		return err
	}
	chunks, err := memory.ParseResults(input)
	if err != nil {
		return err
	}
	var state memory.HookContext
	if raw := os.Getenv("GOAT_MEMORY_HOOK_CONTEXT"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return fmt.Errorf("decode hook context: %w", err)
		}
	}
	if key == "" || state.Query == "" { // still satisfy the hook's array-out contract
		return json.NewEncoder(cmd.OutOrStdout()).Encode(chunks)
	}
	model, _ := cmd.Flags().GetString("model")
	url, _ := cmd.Flags().GetString("url")
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	hook := memory.JevHook{Judge: memory.JevJudge{APIKey: key, Model: model, URL: url, Threshold: threshold}, Parallel: 4, Timeout: 5 * time.Second}
	out, err := hook.Apply(cmd.Context(), chunks, state)
	if err != nil {
		return err
	}
	if out == nil {
		out = []memory.Chunk{}
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
}}

func init() {
	memoryJevCmd.Flags().String("model", "jev-latest", "TypeSafe model")
	memoryJevCmd.Flags().String("url", "https://api.typesafe.ai/v1/systemone", "TypeSafe API endpoint")
	memoryJevCmd.Flags().Float64("threshold", 0.5, "Minimum useful probability")
	memoryHookCmd.AddCommand(memoryJevCmd)
	memoryCmd.AddCommand(memorySearchCmd, memoryHookCmd)
	rootCmd.AddCommand(memoryCmd)
}
