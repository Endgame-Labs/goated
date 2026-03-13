package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	WorkspaceDir                 string
	DBPath                       string
	LogDir                       string
	TelegramBotToken             string
	Gateway                      string
	TelegramMode                 string
	TelegramWebhookURL           string
	TelegramWebhookAddr          string
	TelegramWebhookPath          string
	SlackBotToken                string
	SlackAppToken                string
	SlackChannelID               string
	SlackAttachmentsRoot         string
	SlackAttachmentMaxBytes      int64
	SlackAttachmentMaxTotalBytes int64
	SlackAttachmentMaxParallel   int
	DefaultTimezone              string
	AdminChatID                  string
}

func LoadConfig() Config {
	loadDotEnv(".env")
	// Also check next to the executable (e.g. workspace/goat → ../  .env)
	if exe, err := os.Executable(); err == nil {
		loadDotEnv(filepath.Join(filepath.Dir(exe), ".env"))
		loadDotEnv(filepath.Join(filepath.Dir(exe), "..", ".env"))
	}

	cwd, _ := os.Getwd()
	workspace := getenvDefault("GOAT_WORKSPACE_DIR", cwd)
	db := getenvDefault("GOAT_DB_PATH", filepath.Join(cwd, "goated.db"))
	logDir := getenvDefault("GOAT_LOG_DIR", filepath.Join(cwd, "logs"))
	tz := getenvDefault("GOAT_DEFAULT_TIMEZONE", "America/Los_Angeles")
	slackAttachmentMaxBytes := getenvInt64Default("GOAT_SLACK_ATTACHMENT_MAX_BYTES", 25*1024*1024)
	slackAttachmentMaxTotalBytes := getenvInt64Default("GOAT_SLACK_ATTACHMENT_MAX_TOTAL_BYTES", 251*1024*1024)
	slackAttachmentMaxParallel := getenvIntDefault("GOAT_SLACK_ATTACHMENT_MAX_PARALLEL", 3)

	return Config{
		WorkspaceDir:                 workspace,
		DBPath:                       db,
		LogDir:                       logDir,
		TelegramBotToken:             os.Getenv("GOAT_TELEGRAM_BOT_TOKEN"),
		Gateway:                      getenvDefault("GOAT_GATEWAY", "telegram"),
		TelegramMode:                 getenvDefault("GOAT_TELEGRAM_MODE", "polling"),
		TelegramWebhookURL:           os.Getenv("GOAT_TELEGRAM_WEBHOOK_URL"),
		TelegramWebhookAddr:          getenvDefault("GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR", ":8080"),
		TelegramWebhookPath:          getenvDefault("GOAT_TELEGRAM_WEBHOOK_PATH", "/telegram/webhook"),
		SlackBotToken:                os.Getenv("GOAT_SLACK_BOT_TOKEN"),
		SlackAppToken:                os.Getenv("GOAT_SLACK_APP_TOKEN"),
		SlackChannelID:               os.Getenv("GOAT_SLACK_CHANNEL_ID"),
		SlackAttachmentsRoot:         getenvDefault("GOAT_SLACK_ATTACHMENTS_ROOT", filepath.Join(workspace, "tmp", "slack", "attachments")),
		SlackAttachmentMaxBytes:      slackAttachmentMaxBytes,
		SlackAttachmentMaxTotalBytes: slackAttachmentMaxTotalBytes,
		SlackAttachmentMaxParallel:   slackAttachmentMaxParallel,
		DefaultTimezone:              tz,
		AdminChatID:                  os.Getenv("GOAT_ADMIN_CHAT_ID"),
	}
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		// Environment variables already present should win over .env values.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func getenvDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func getenvInt64Default(k string, fallback int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "invalid %s=%q, using default %d\n", k, v, fallback)
		return fallback
	}
	return n
}

func getenvIntDefault(k string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "invalid %s=%q, using default %d\n", k, v, fallback)
		return fallback
	}
	return n
}
