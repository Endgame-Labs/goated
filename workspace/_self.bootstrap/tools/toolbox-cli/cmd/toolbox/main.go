package main

import (
	"fmt"
	"os"

	"toolbox-example/internal/agentlog"
	"toolbox-example/internal/workspace"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "toolbox",
		Short: "Generalized personal CLI for a Goated self repo",
		Long:  "Reusable Cobra CLI template for a Goated self repo.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			selfDir, err := workspace.SelfDirFromExecutable()
			if err != nil {
				return err
			}
			if err := os.Chdir(selfDir); err != nil {
				return fmt.Errorf("chdir to self dir: %w", err)
			}
			return nil
		},
	}

	cleanup, err := agentlog.Init("toolbox")
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: log init failed:", err)
	} else {
		defer cleanup()
	}

	rootCmd.AddCommand(credsCmd())
	rootCmd.AddCommand(rememberCmd())
	rootCmd.AddCommand(notesCmd())
	rootCmd.AddCommand(browserCmd())
	rootCmd.AddCommand(voiceCmd())
	rootCmd.AddCommand(emailCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
