package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"toolbox-example/internal/workspace"

	"github.com/spf13/cobra"
)

func notesCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "notes",
		Short:              "Proxy to the bundled notesmd CLI",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			selfDir, err := workspace.SelfDirFromExecutable()
			if err != nil {
				return err
			}
			notesPath := filepath.Join(selfDir, "tools", "notesmd")
			info, err := os.Stat(notesPath)
			if err != nil {
				return fmt.Errorf(
					"notesmd binary not found at %s\nbuild it by running %s\nor place a notesmd binary at %s",
					notesPath,
					filepath.Join(selfDir, "build_clis.sh"),
					notesPath,
				)
			}
			if info.IsDir() {
				return fmt.Errorf("expected notesmd binary at %s, but found a directory; place the built notesmd binary there", notesPath)
			}
			if info.Mode()&0111 == 0 {
				return fmt.Errorf("notesmd exists at %s but is not executable; run chmod +x %s", notesPath, notesPath)
			}

			proxy := exec.Command(notesPath, args...)
			proxy.Stdin = os.Stdin
			proxy.Stdout = os.Stdout
			proxy.Stderr = os.Stderr
			return proxy.Run()
		},
	}
}
