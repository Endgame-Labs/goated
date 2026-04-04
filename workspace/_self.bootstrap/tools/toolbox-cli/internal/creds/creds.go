package creds

import (
	"fmt"
	"os/exec"
	"strings"

	"toolbox-example/internal/workspace"
)

// Get reads a credential via workspace/goat.
func Get(keyName string) (string, error) {
	goatPath, err := workspace.GoatPath()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(goatPath, "creds", "get", keyName).Output()
	if err != nil {
		return "", fmt.Errorf("get credential %s: %w", keyName, err)
	}
	return strings.TrimSpace(string(out)), nil
}
