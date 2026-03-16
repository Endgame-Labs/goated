package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"goated/internal/goatlog"
)

var rootCmd = &cobra.Command{
	Use:   "goated",
	Short: "Goated – Claude-powered agent gateway",
}

func Execute() {
	// Daily-rotating log for all goat/goated CLI invocations.
	// Log dir is relative to the binary's parent directory.
	exe, _ := os.Executable()
	logDir := filepath.Join(filepath.Dir(exe), "logs", "goat")
	cleanup := goatlog.Init(logDir)
	defer cleanup()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
