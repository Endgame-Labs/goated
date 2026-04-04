package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"toolbox-example/internal/workspace"
)

func selfDir() (string, error) {
	return workspace.SelfDirFromExecutable()
}

// Slugify converts text into a filesystem-friendly slug.
func Slugify(text string, maxLen int) string {
	s := strings.ToLower(strings.TrimSpace(text))
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func Today() string {
	return time.Now().UTC().Format("2006-01-02")
}

// SavePost writes a file under self/posts/<platform>/.
func SavePost(platform, filename, content string) (string, error) {
	root, err := selfDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "posts", platform)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return filepath.ToSlash(filepath.Join("posts", platform, filename)), nil
}

func ensureStateDir() (string, error) {
	root, err := selfDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "state")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
}

// ReadState reads a state file. Missing files return an empty string.
func ReadState(filename string) (string, error) {
	dir, err := ensureStateDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteState writes a state file.
func WriteState(filename, content string) error {
	dir, err := ensureStateDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
}
