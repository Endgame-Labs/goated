package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"goated/internal/db"
	"goated/internal/subagent"
)

var subagentFinishCmd = &cobra.Command{
	Use:    "subagent-finish",
	Short:  "Record subagent completion and notify the main session",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		status, _ := cmd.Flags().GetString("status")
		source, _ := cmd.Flags().GetString("source")
		logPath, _ := cmd.Flags().GetString("log")
		sessionName, _ := cmd.Flags().GetString("session")
		chatID, _ := cmd.Flags().GetString("chat")
		dbPath, _ := cmd.Flags().GetString("db")
		runtimeProvider, _ := cmd.Flags().GetString("runtime-provider")
		runtimeMode, _ := cmd.Flags().GetString("runtime-mode")
		runtimeVersion, _ := cmd.Flags().GetString("runtime-version")
		runID, _ := cmd.Flags().GetUint64("run-id")
		cronID, _ := cmd.Flags().GetUint64("cron-id")

		if status == "" {
			return fmt.Errorf("--status is required")
		}
		if source == "" {
			source = "subagent"
		}

		var store *db.Store
		if dbPath != "" {
			var err error
			store, err = db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer store.Close()
		}

		var runErr error
		if status != "ok" {
			runErr = fmt.Errorf("subagent exited with status %s", status)
		}

		subagent.HandleCompletion(store, runID, runErr, subagent.RunOpts{
			Source:      source,
			CronID:      cronID,
			ChatID:      chatID,
			LogPath:     logPath,
			SessionName: sessionName,
			Runtime: db.ExecutionRuntime{
				Provider: runtimeProvider,
				Mode:     runtimeMode,
				Version:  runtimeVersion,
			},
		})
		return nil
	},
}

func init() {
	subagentFinishCmd.Flags().String("status", "", "Final subagent status (ok/error)")
	subagentFinishCmd.Flags().String("source", "", "Completion source label")
	subagentFinishCmd.Flags().String("log", "", "Subagent log path")
	subagentFinishCmd.Flags().String("session", "", "Main session tmux target")
	subagentFinishCmd.Flags().String("chat", "", "Associated chat ID")
	subagentFinishCmd.Flags().String("db", "", "Database path for status updates")
	subagentFinishCmd.Flags().String("runtime-provider", "", "Runtime provider")
	subagentFinishCmd.Flags().String("runtime-mode", "", "Runtime mode")
	subagentFinishCmd.Flags().String("runtime-version", "", "Runtime version")
	subagentFinishCmd.Flags().Uint64("run-id", 0, "Tracked subagent run ID")
	subagentFinishCmd.Flags().Uint64("cron-id", 0, "Associated cron ID")
	rootCmd.AddCommand(subagentFinishCmd)
}
