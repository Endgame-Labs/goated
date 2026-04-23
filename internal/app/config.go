package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// validClaudeModels lists the model IDs and aliases accepted by the claude CLI's --model flag.
var validClaudeModels = map[string]bool{
	// Aliases (resolve to latest)
	"sonnet": true, "opus": true, "haiku": true,
	"sonnet[1m]": true, "opus[1m]": true,
	"opusplan": true,

	// Current models
	"claude-opus-4-6": true, "claude-sonnet-4-6": true,
	"claude-haiku-4-5-20251001": true, "claude-haiku-4-5": true,
	"claude-opus-4-6[1m]": true, "claude-sonnet-4-6[1m]": true,

	// Recent models
	"claude-sonnet-4-5-20250929": true, "claude-sonnet-4-5": true,
	"claude-opus-4-5-20251101": true, "claude-opus-4-5": true,
	"claude-opus-4-1-20250805": true, "claude-opus-4-1": true,
	"claude-sonnet-4-20250514": true, "claude-sonnet-4-0": true,
	"claude-opus-4-20250514": true, "claude-opus-4-0": true,
}

// ValidateClaudeModel returns an error if the model ID is not recognized.
// Empty string is valid (means use default).
func ValidateClaudeModel(model string) error {
	if model == "" {
		return nil
	}
	if !validClaudeModels[model] {
		var valid []string
		for k := range validClaudeModels {
			valid = append(valid, k)
		}
		return fmt.Errorf("invalid claude model %q; valid models: sonnet, opus, haiku, claude-opus-4-6, claude-sonnet-4-6, etc.", model)
	}
	return nil
}

type Config struct {
	WorkspaceDir                    string
	DBPath                          string
	LogDir                          string
	AgentRuntime                    string
	Model                           string // claude CLI --model value (e.g. "sonnet", "opus", "claude-opus-4-6")
	TelegramBotToken                string
	Gateway                         string
	TelegramMode                    string
	TelegramWebhookURL              string
	TelegramWebhookAddr             string
	TelegramWebhookPath             string
	TelegramAttachmentsRoot         string
	TelegramAttachmentMaxBytes      int64
	TelegramAttachmentMaxTotalBytes int64
	TelegramAllowedChatIDs          []string
	TelegramGroupRespondAll         bool
	SlackBotToken                   string
	SlackAppToken                   string
	SlackChannelID                  string
	SlackAttachmentsRoot            string
	SlackAttachmentMaxBytes         int64
	SlackAttachmentMaxTotalBytes    int64
	SlackAttachmentMaxParallel      int
	DefaultTimezone                 string
	AdminChatID                     string
	PiProvider                      string // pi --provider value, e.g. "fireworks"
	PiModel                         string // pi --model value, e.g. "accounts/fireworks/models/kimi-k2p5"
	LogRetentionDays                int
	LogRetentionMaxFiles            int
	LogRetentionCompress            bool
}

// ParsedTelegramAllowedChatIDs parses TelegramAllowedChatIDs into int64 values.
// Returns an error if any entry is not a valid int64.
func (c Config) ParsedTelegramAllowedChatIDs() ([]int64, error) {
	out := make([]int64, 0, len(c.TelegramAllowedChatIDs))
	for _, s := range c.TelegramAllowedChatIDs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram.allowed_chat_ids entry %q: %w", s, err)
		}
		out = append(out, id)
	}
	return out, nil
}

func LoadConfig() Config {
	ensureLocalBinPaths()

	v := viper.New()
	v.SetConfigName("goated")
	v.SetConfigType("json")

	// Search paths: cwd, exe dir, exe parent dir (same as old .env search)
	v.AddConfigPath(".")
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
		v.AddConfigPath(exeDir)
		v.AddConfigPath(filepath.Join(exeDir, ".."))
	}

	// Defaults for all settings keys
	v.SetDefault("gateway", "telegram")
	v.SetDefault("agent_runtime", "claude")
	v.SetDefault("model", "")
	v.SetDefault("default_timezone", "America/Los_Angeles")
	v.SetDefault("workspace_dir", "")
	v.SetDefault("db_path", "")
	v.SetDefault("log_dir", "")
	v.SetDefault("telegram.mode", "polling")
	v.SetDefault("telegram.webhook_addr", ":8080")
	v.SetDefault("telegram.webhook_path", "/telegram/webhook")
	v.SetDefault("telegram.attachments_root", "")
	v.SetDefault("telegram.attachment_max_bytes", int64(25*1024*1024))
	v.SetDefault("telegram.attachment_max_total_bytes", int64(251*1024*1024))
	v.SetDefault("telegram.group_respond_all", false)
	v.SetDefault("slack.attachments_root", "")
	v.SetDefault("slack.channel_id", "")
	v.SetDefault("slack.attachment_max_bytes", int64(25*1024*1024))
	v.SetDefault("slack.attachment_max_total_bytes", int64(251*1024*1024))
	v.SetDefault("slack.attachment_max_parallel", 3)
	v.SetDefault("pi.provider", "")
	v.SetDefault("pi.model", "")
	v.SetDefault("log_retention_days", 7)
	v.SetDefault("log_retention_max_files", 1000)
	v.SetDefault("log_retention_compress", false)

	// Bind env vars so they override config file values
	v.BindEnv("gateway", "GOAT_GATEWAY")
	v.BindEnv("agent_runtime", "GOAT_AGENT_RUNTIME")
	v.BindEnv("model", "GOAT_MODEL")
	v.BindEnv("default_timezone", "GOAT_DEFAULT_TIMEZONE")
	v.BindEnv("workspace_dir", "GOAT_WORKSPACE_DIR")
	v.BindEnv("db_path", "GOAT_DB_PATH")
	v.BindEnv("log_dir", "GOAT_LOG_DIR")
	v.BindEnv("telegram.mode", "GOAT_TELEGRAM_MODE")
	v.BindEnv("telegram.webhook_addr", "GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR")
	v.BindEnv("telegram.webhook_path", "GOAT_TELEGRAM_WEBHOOK_PATH")
	v.BindEnv("telegram.attachments_root", "GOAT_TELEGRAM_ATTACHMENTS_ROOT")
	v.BindEnv("telegram.attachment_max_bytes", "GOAT_TELEGRAM_ATTACHMENT_MAX_BYTES")
	v.BindEnv("telegram.attachment_max_total_bytes", "GOAT_TELEGRAM_ATTACHMENT_MAX_TOTAL_BYTES")
	v.BindEnv("telegram.group_respond_all", "GOAT_TELEGRAM_GROUP_RESPOND_ALL")
	v.BindEnv("slack.attachments_root", "GOAT_SLACK_ATTACHMENTS_ROOT")
	v.BindEnv("slack.channel_id", "GOAT_SLACK_CHANNEL_ID")
	v.BindEnv("slack.attachment_max_bytes", "GOAT_SLACK_ATTACHMENT_MAX_BYTES")
	v.BindEnv("slack.attachment_max_total_bytes", "GOAT_SLACK_ATTACHMENT_MAX_TOTAL_BYTES")
	v.BindEnv("slack.attachment_max_parallel", "GOAT_SLACK_ATTACHMENT_MAX_PARALLEL")
	v.BindEnv("pi.provider", "GOAT_PI_PROVIDER")
	v.BindEnv("pi.model", "GOAT_PI_MODEL")
	v.BindEnv("log_retention_days", "GOAT_LOG_RETENTION_DAYS")
	v.BindEnv("log_retention_max_files", "GOAT_LOG_RETENTION_MAX_FILES")
	v.BindEnv("log_retention_compress", "GOAT_LOG_RETENTION_COMPRESS")

	// Read config file (ignore not-found — defaults + env vars are fine)
	readErr := v.ReadInConfig()

	// Auto-migrate: if no goated.json found but .env exists, migrate in place
	if readErr != nil {
		envPath := findEnvFile(".", exeDir)
		if envPath != "" {
			if err := autoMigrateEnv(envPath); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: auto-migrate .env failed: %v\n", err)
			} else {
				// Re-read now that goated.json exists
				_ = v.ReadInConfig()
			}
		}
	}

	cwd, _ := os.Getwd()
	baseDir := defaultBaseDir(cwd, exeDir)
	configDir := ""
	if used := v.ConfigFileUsed(); used != "" {
		configDir = filepath.Dir(used)
	}

	// Resolve workspace
	workspace := v.GetString("workspace_dir")
	if workspace == "" {
		workspace = defaultWorkspaceDir(cwd, exeDir)
	} else if configDir != "" && !filepath.IsAbs(workspace) {
		workspace = filepath.Join(configDir, workspace)
	}

	// Resolve db path
	dbPath := v.GetString("db_path")
	if dbPath == "" {
		dbPath = filepath.Join(baseDir, "goated.db")
	} else if configDir != "" && !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(configDir, dbPath)
	}

	// Resolve log dir
	logDir := v.GetString("log_dir")
	if logDir == "" {
		logDir = filepath.Join(baseDir, "logs")
	} else if configDir != "" && !filepath.IsAbs(logDir) {
		logDir = filepath.Join(configDir, logDir)
	}

	// Resolve slack attachments root
	telegramAttRoot := v.GetString("telegram.attachments_root")
	if telegramAttRoot == "" {
		telegramAttRoot = filepath.Join(workspace, "tmp", "telegram", "attachments")
	} else if configDir != "" && !filepath.IsAbs(telegramAttRoot) {
		telegramAttRoot = filepath.Join(configDir, telegramAttRoot)
	}

	// Resolve slack attachments root
	slackAttRoot := v.GetString("slack.attachments_root")
	if slackAttRoot == "" {
		slackAttRoot = filepath.Join(workspace, "tmp", "slack", "attachments")
	} else if configDir != "" && !filepath.IsAbs(slackAttRoot) {
		slackAttRoot = filepath.Join(configDir, slackAttRoot)
	}

	// Validate model if set
	model := v.GetString("model")
	if err := ValidateClaudeModel(model); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		model = "" // fall back to default
	}

	// Resolve creds directory for secrets
	credsDir := filepath.Join(workspace, "creds")
	slackChannelID := v.GetString("slack.channel_id")
	if slackChannelID == "" {
		legacyChannelID := loadCredFile(credsDir, "GOAT_SLACK_CHANNEL_ID")
		if legacyChannelID != "" {
			slackChannelID = legacyChannelID
			if used := v.ConfigFileUsed(); used != "" {
				if err := migrateLegacySlackChannelID(used, legacyChannelID, credsDir); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: migrate legacy GOAT_SLACK_CHANNEL_ID: %v\n", err)
				}
			}
		}
	}

	return Config{
		WorkspaceDir:                    workspace,
		DBPath:                          dbPath,
		LogDir:                          logDir,
		AgentRuntime:                    v.GetString("agent_runtime"),
		Model:                           model,
		TelegramBotToken:                loadCred(credsDir, "GOAT_TELEGRAM_BOT_TOKEN"),
		Gateway:                         v.GetString("gateway"),
		TelegramMode:                    v.GetString("telegram.mode"),
		TelegramWebhookURL:              loadCred(credsDir, "GOAT_TELEGRAM_WEBHOOK_URL"),
		TelegramWebhookAddr:             v.GetString("telegram.webhook_addr"),
		TelegramWebhookPath:             v.GetString("telegram.webhook_path"),
		TelegramAttachmentsRoot:         telegramAttRoot,
		TelegramAttachmentMaxBytes:      v.GetInt64("telegram.attachment_max_bytes"),
		TelegramAttachmentMaxTotalBytes: v.GetInt64("telegram.attachment_max_total_bytes"),
		TelegramAllowedChatIDs:          v.GetStringSlice("telegram.allowed_chat_ids"),
		TelegramGroupRespondAll:         v.GetBool("telegram.group_respond_all"),
		SlackBotToken:                   loadCred(credsDir, "GOAT_SLACK_BOT_TOKEN"),
		SlackAppToken:                   loadCred(credsDir, "GOAT_SLACK_APP_TOKEN"),
		SlackChannelID:                  slackChannelID,
		SlackAttachmentsRoot:            slackAttRoot,
		SlackAttachmentMaxBytes:         v.GetInt64("slack.attachment_max_bytes"),
		SlackAttachmentMaxTotalBytes:    v.GetInt64("slack.attachment_max_total_bytes"),
		SlackAttachmentMaxParallel:      v.GetInt("slack.attachment_max_parallel"),
		DefaultTimezone:                 v.GetString("default_timezone"),
		AdminChatID:                     loadCred(credsDir, "GOAT_ADMIN_CHAT_ID"),
		PiProvider:                      v.GetString("pi.provider"),
		PiModel:                         v.GetString("pi.model"),
		LogRetentionDays:                v.GetInt("log_retention_days"),
		LogRetentionMaxFiles:            v.GetInt("log_retention_max_files"),
		LogRetentionCompress:            v.GetBool("log_retention_compress"),
	}
}

func ensureLocalBinPaths() {
	const pathKey = "PATH"

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}

	candidates := []string{
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".local", "goated-go", "bin"),
	}

	current := filepath.SplitList(os.Getenv(pathKey))
	seen := make(map[string]struct{}, len(current))
	var cleaned []string
	for _, dir := range current {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		cleaned = append(cleaned, dir)
	}

	var additions []string
	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		additions = append(additions, dir)
	}

	if len(additions) == 0 {
		return
	}

	parts := append(additions, cleaned...)
	_ = os.Setenv(pathKey, strings.Join(parts, string(os.PathListSeparator)))
}

// loadCred reads a secret from workspace/creds/KEY.txt, falling back to the
// environment variable of the same name. Env vars always win.
func loadCred(credsDir, key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return loadCredFile(credsDir, key)
}

func loadCredFile(credsDir, key string) string {
	path := filepath.Join(credsDir, key+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// RemoveCred removes workspace/creds/KEY.txt if it exists.
func RemoveCred(credsDir, key string) error {
	path := filepath.Join(credsDir, key+".txt")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadConfigJSON reads a goated.json config file into a generic map.
func ReadConfigJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// WriteConfigJSON writes a config map to a JSON file atomically.
func WriteConfigJSON(path string, data map[string]any) error {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	content = append(content, '\n')

	// Atomic write: write to tmp file, then rename
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

// WriteCred writes a single credential to workspace/creds/KEY.txt.
func WriteCred(credsDir, key, value string) error {
	if err := os.MkdirAll(credsDir, 0o700); err != nil {
		return fmt.Errorf("mkdir creds: %w", err)
	}
	path := filepath.Join(credsDir, key+".txt")
	return os.WriteFile(path, []byte(value+"\n"), 0o600)
}

func envExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findEnvFile looks for a .env file to auto-migrate. Migration is destructive
// (renames .env → .env.bak and writes goated.json next to it), so we only
// consider .env in the current working directory — never walk up or use the
// binary's install path, which would risk mutating unrelated files in parent
// directories.
func findEnvFile(cwd, _ string) string {
	p := filepath.Join(cwd, ".env")
	if envExists(p) {
		return p
	}
	return ""
}

// autoMigrateEnv reads a .env file, writes goated.json + creds files, and
// renames .env → .env.bak. Called automatically by LoadConfig when goated.json
// is missing but .env exists.
func autoMigrateEnv(envPath string) error {
	env := parseEnvFileForMigration(envPath)
	if len(env) == 0 {
		return nil
	}

	// Build goated.json
	configMap := make(map[string]any)
	setIfPresent := func(jsonKey, envKey string) {
		if v := env[envKey]; v != "" {
			configMap[jsonKey] = v
		}
	}
	setIfPresent("gateway", "GOAT_GATEWAY")
	setIfPresent("agent_runtime", "GOAT_AGENT_RUNTIME")
	setIfPresent("default_timezone", "GOAT_DEFAULT_TIMEZONE")
	setIfPresent("workspace_dir", "GOAT_WORKSPACE_DIR")
	setIfPresent("db_path", "GOAT_DB_PATH")
	setIfPresent("log_dir", "GOAT_LOG_DIR")

	telegram := make(map[string]any)
	if v := env["GOAT_TELEGRAM_MODE"]; v != "" {
		telegram["mode"] = v
	}
	if v := env["GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR"]; v != "" {
		telegram["webhook_addr"] = v
	}
	if v := env["GOAT_TELEGRAM_WEBHOOK_PATH"]; v != "" {
		telegram["webhook_path"] = v
	}
	if len(telegram) > 0 {
		configMap["telegram"] = telegram
	}

	slack := make(map[string]any)
	setSlackInt := func(jsonKey, envKey string) {
		if v := env[envKey]; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				slack[jsonKey] = n
			}
		}
	}
	if v := env["GOAT_SLACK_ATTACHMENTS_ROOT"]; v != "" {
		slack["attachments_root"] = v
	}
	if v := env["GOAT_SLACK_CHANNEL_ID"]; v != "" {
		slack["channel_id"] = v
	}
	setSlackInt("attachment_max_bytes", "GOAT_SLACK_ATTACHMENT_MAX_BYTES")
	setSlackInt("attachment_max_total_bytes", "GOAT_SLACK_ATTACHMENT_MAX_TOTAL_BYTES")
	setSlackInt("attachment_max_parallel", "GOAT_SLACK_ATTACHMENT_MAX_PARALLEL")
	if len(slack) > 0 {
		configMap["slack"] = slack
	}

	// Write goated.json next to the .env
	dir := filepath.Dir(envPath)
	configPath := filepath.Join(dir, "goated.json")
	if err := WriteConfigJSON(configPath, configMap); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Auto-migrated %s → %s\n", envPath, configPath)

	// Write secrets to creds files
	workspace := env["GOAT_WORKSPACE_DIR"]
	if workspace == "" {
		workspace = "workspace"
	}
	if !filepath.IsAbs(workspace) {
		workspace = filepath.Join(dir, workspace)
	}
	credsDir := filepath.Join(workspace, "creds")

	secrets := []struct{ envKey string }{
		{"GOAT_TELEGRAM_BOT_TOKEN"},
		{"GOAT_TELEGRAM_WEBHOOK_URL"},
		{"GOAT_SLACK_BOT_TOKEN"},
		{"GOAT_SLACK_APP_TOKEN"},
		{"GOAT_ADMIN_CHAT_ID"},
	}
	for _, s := range secrets {
		if val := env[s.envKey]; val != "" {
			if err := WriteCred(credsDir, s.envKey, val); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not write cred %s: %v\n", s.envKey, err)
			} else {
				fmt.Fprintf(os.Stderr, "  Wrote secret %s\n", s.envKey)
			}
		}
	}

	// Rename .env → .env.bak
	bakPath := envPath + ".bak"
	if err := os.Rename(envPath, bakPath); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not rename %s → %s: %v\n", envPath, bakPath, err)
	} else {
		fmt.Fprintf(os.Stderr, "  Renamed %s → %s\n", envPath, bakPath)
	}

	return nil
}

// parseEnvFileForMigration reads a .env file into a key-value map.
func parseEnvFileForMigration(path string) map[string]string {
	m := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return m
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
		m[key] = value
	}
	return m
}

func migrateLegacySlackChannelID(configPath, channelID, credsDir string) error {
	cfg, err := ReadConfigJSON(configPath)
	if err != nil {
		return err
	}

	slack, _ := cfg["slack"].(map[string]any)
	if slack == nil {
		slack = make(map[string]any)
	}
	if existing, _ := slack["channel_id"].(string); existing == "" {
		slack["channel_id"] = channelID
	}
	cfg["slack"] = slack

	if err := WriteConfigJSON(configPath, cfg); err != nil {
		return err
	}
	if err := RemoveCred(credsDir, "GOAT_SLACK_CHANNEL_ID"); err != nil {
		return err
	}
	return nil
}

func defaultBaseDir(cwd, exeDir string) string {
	if cwd != "" && hasWorkspaceDir(cwd) {
		return cwd
	}
	if filepath.Base(cwd) == "workspace" {
		return filepath.Dir(cwd)
	}
	if filepath.Base(exeDir) == "workspace" {
		return filepath.Dir(exeDir)
	}
	return cwd
}

func defaultWorkspaceDir(cwd, exeDir string) string {
	if cwd != "" && hasWorkspaceContract(cwd) {
		return cwd
	}
	if hasWorkspaceDir(cwd) {
		return filepath.Join(cwd, "workspace")
	}
	if filepath.Base(exeDir) == "workspace" {
		return exeDir
	}
	return cwd
}

func hasWorkspaceDir(root string) bool {
	return fileExists(filepath.Join(root, "workspace", "goat"))
}

func hasWorkspaceContract(dir string) bool {
	return fileExists(filepath.Join(dir, "goat")) &&
		(fileExists(filepath.Join(dir, "GOATED.md")) || fileExists(filepath.Join(dir, "CLAUDE.md")))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
