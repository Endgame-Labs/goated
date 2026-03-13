package slack

import (
	"fmt"
	"os"
	"strings"
)

const ThinkingFile = "/tmp/goated-slack-thinking"

// WriteThinkingTS atomically writes the thinking indicator timestamp.
func WriteThinkingTS(ts string) error {
	tmp := ThinkingFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(ts), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, ThinkingFile)
}

// ClaimThinkingTS atomically reads and removes the thinking indicator timestamp.
// Returns "" if no indicator exists or another process already claimed it.
func ClaimThinkingTS() string {
	// Rename to a unique temp name to atomically claim ownership
	claimed := ThinkingFile + ".claimed." + fmt.Sprintf("%d", os.Getpid())
	if err := os.Rename(ThinkingFile, claimed); err != nil {
		return "" // file doesn't exist or already claimed
	}
	data, err := os.ReadFile(claimed)
	os.Remove(claimed)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
