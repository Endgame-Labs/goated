package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetenvDefault(t *testing.T) {
	// Unset key should return fallback
	os.Unsetenv("TEST_GETENV_DEFAULT_KEY")
	got := getenvDefault("TEST_GETENV_DEFAULT_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}

	// Set key should return its value
	os.Setenv("TEST_GETENV_DEFAULT_KEY", "actual")
	defer os.Unsetenv("TEST_GETENV_DEFAULT_KEY")
	got = getenvDefault("TEST_GETENV_DEFAULT_KEY", "fallback")
	if got != "actual" {
		t.Errorf("got %q, want actual", got)
	}
}

func TestGetenvDefault_EmptyValue(t *testing.T) {
	// Empty string should return fallback (matches the implementation)
	os.Setenv("TEST_GETENV_EMPTY", "")
	defer os.Unsetenv("TEST_GETENV_EMPTY")
	got := getenvDefault("TEST_GETENV_EMPTY", "default")
	if got != "default" {
		t.Errorf("got %q, want default (empty env value should use fallback)", got)
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := `# comment line
FOO_TEST_DOTENV=bar
BAZ_TEST_DOTENV="quoted value"
SINGLE_QUOTED='single'
EMPTY_LINE_BEFORE=value

SPACED_KEY = spaced_value
`
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Clear any existing values
	os.Unsetenv("FOO_TEST_DOTENV")
	os.Unsetenv("BAZ_TEST_DOTENV")
	os.Unsetenv("SINGLE_QUOTED")
	os.Unsetenv("EMPTY_LINE_BEFORE")
	os.Unsetenv("SPACED_KEY")
	defer func() {
		os.Unsetenv("FOO_TEST_DOTENV")
		os.Unsetenv("BAZ_TEST_DOTENV")
		os.Unsetenv("SINGLE_QUOTED")
		os.Unsetenv("EMPTY_LINE_BEFORE")
		os.Unsetenv("SPACED_KEY")
	}()

	loadDotEnv(envFile)

	tests := []struct {
		key  string
		want string
	}{
		{"FOO_TEST_DOTENV", "bar"},
		{"BAZ_TEST_DOTENV", "quoted value"},
		{"SINGLE_QUOTED", "single"},
		{"EMPTY_LINE_BEFORE", "value"},
		{"SPACED_KEY", "spaced_value"},
	}

	for _, tt := range tests {
		got := os.Getenv(tt.key)
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestLoadDotEnv_ExistingEnvWins(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	os.WriteFile(envFile, []byte("EXISTING_KEY_TEST=from_file\n"), 0644)

	// Pre-set the env var
	os.Setenv("EXISTING_KEY_TEST", "from_env")
	defer os.Unsetenv("EXISTING_KEY_TEST")

	loadDotEnv(envFile)

	got := os.Getenv("EXISTING_KEY_TEST")
	if got != "from_env" {
		t.Errorf("got %q, want from_env (existing env should win)", got)
	}
}

func TestLoadDotEnv_NonExistentFile(t *testing.T) {
	// Should not panic or error
	loadDotEnv("/nonexistent/path/.env")
}

func TestLoadDotEnv_SkipsInvalidLines(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := `no_equals_sign
=empty_key
VALID_KEY_TEST=valid
`
	os.WriteFile(envFile, []byte(content), 0644)
	os.Unsetenv("VALID_KEY_TEST")
	defer os.Unsetenv("VALID_KEY_TEST")

	loadDotEnv(envFile)

	if got := os.Getenv("VALID_KEY_TEST"); got != "valid" {
		t.Errorf("VALID_KEY_TEST = %q, want valid", got)
	}
}
