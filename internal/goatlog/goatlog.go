package goatlog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Init opens (or creates) today's log file and appends a session header.
// logDir is the base directory for logs (e.g. "logs/goat").
// Returns a cleanup function that should be deferred.
func Init(logDir string) func() {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return func() {}
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(logDir, today+".log")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return func() {}
	}

	fmt.Fprintf(f, "\n=== goat invoked at %s ===\n", time.Now().Format(time.RFC3339))
	if caller := os.Getenv("LOG_CALLER"); caller != "" {
		fmt.Fprintf(f, "caller: %s\n", caller)
	}
	fmt.Fprintf(f, "args: %v\n", os.Args)

	return func() { f.Close() }
}
