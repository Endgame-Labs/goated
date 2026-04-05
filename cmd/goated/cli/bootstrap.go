package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/db"
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Initialize database, workspace, and configure your first channel",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("=== goated bootstrap ===")
		fmt.Println()

		// Load existing config if present
		configPath := "goated.json"
		existing, _ := app.ReadConfigJSON(configPath)

		// Prompt for common settings
		tz := prompt(reader, "Default timezone", withDefault(strFromMap(existing, "default_timezone"), "America/Los_Angeles"))
		runtime := prompt(reader, "Agent runtime (claude/codex/pi/claude_tui/codex_tui)", withDefault(strFromMap(existing, "agent_runtime"), "claude_tui"))
		if runtime != "claude" && runtime != "codex" && runtime != "pi" && runtime != "claude_tui" && runtime != "codex_tui" {
			return fmt.Errorf("agent runtime must be claude, codex, pi, claude_tui, or codex_tui")
		}
		if runtime == "pi" {
			fmt.Println()
			fmt.Println("Pi runtime notes:")
			fmt.Println("- Pi auth and custom provider config live under ~/.pi/agent/")
			fmt.Println("- Bootstrap will create a Goated-managed Pi session and warm it up from GOATED.md")
			fmt.Println("- The Pi runtime is headless-only and still considered in development")
		}

		cfg := app.LoadConfig()
		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer store.Close()

		if err := maybeResetBootstrapChannels(reader, store); err != nil {
			return err
		}

		// Interactive channel setup
		fmt.Println()
		ch, err := promptChannel(reader)
		if err != nil {
			return err
		}

		// Build config map
		configMap := make(map[string]any)
		configMap["gateway"] = ch.Type
		configMap["agent_runtime"] = runtime
		configMap["default_timezone"] = tz
		configMap["workspace_dir"] = withDefault(strFromMap(existing, "workspace_dir"), "workspace")
		if v := strFromMap(existing, "db_path"); v != "" {
			configMap["db_path"] = v
		}
		if v := strFromMap(existing, "log_dir"); v != "" {
			configMap["log_dir"] = v
		}

		// Write goated.json
		if err := app.WriteConfigJSON(configPath, configMap); err != nil {
			return fmt.Errorf("write goated.json: %w", err)
		}
		fmt.Println("Wrote goated.json")

		// Write channel secrets to creds files
		workspace := withDefault(strFromMap(existing, "workspace_dir"), "workspace")
		credsDir := filepath.Join(workspace, "creds")
		if err := writeChannelCreds(credsDir, ch); err != nil {
			return fmt.Errorf("write creds: %w", err)
		}

		fmt.Println()
		fmt.Println("Database initialized at", cfg.DBPath)

		// Ensure workspace dir exists
		if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
			return fmt.Errorf("mkdir workspace: %w", err)
		}
		fmt.Println("Workspace directory:", cfg.WorkspaceDir)
		fmt.Println()
		if err := ensureSeededSelfRepo(cfg.WorkspaceDir, os.Stdout); err != nil {
			return err
		}

		// Save channel to DB
		if err := store.AddChannel(*ch); err != nil {
			return err
		}
		if err := store.SetMeta("active_channel", ch.Name); err != nil {
			return err
		}
		fmt.Printf("Channel %q (%s) added and activated.\n", ch.Name, ch.Type)

		// Write final goated.json with channel config active
		if err := writeChannelConfig(ch); err != nil {
			return fmt.Errorf("write goated.json: %w", err)
		}

		// Add hourly self-sync system cron
		syncCmd := "./goat sync_self_to_github"
		_, err = store.AddCron("system", "", "0 * * * *", "", "", syncCmd, tz, true)
		if err != nil {
			fmt.Printf("Warning: could not create sync cron: %v\n", err)
		} else {
			fmt.Println("Added hourly sync_self_to_github system cron.")
		}
		if err := ensureDefaultSelfCrons(store, cfg.WorkspaceDir, tz); err != nil {
			return err
		}

		fmt.Println()
		repoRoot, _ := os.Getwd()
		if err := runBootstrapPostSetup(repoRoot, runtime); err != nil {
			return err
		}
		fmt.Println()
		fmt.Println("Bootstrap complete.")
		return nil
	},
}

func ensureDefaultSelfCrons(store *db.Store, workspaceDir, timezone string) error {
	selfDir := filepath.Join(workspaceDir, "self")

	defaults := []struct {
		schedule          string
		promptFile        string
		label             string
		notifyMainSession bool
	}{
		{
			schedule:          "0 * * * *",
			promptFile:        filepath.Join(selfDir, "HEARTBEAT.md"),
			label:             "hourly heartbeat",
			notifyMainSession: true,
		},
		{
			schedule:          "0 */8 * * *",
			promptFile:        filepath.Join(selfDir, "prompts", "knowledge_extraction.md"),
			label:             "knowledge extraction",
			notifyMainSession: true,
		},
	}

	existing, err := store.AllCrons()
	if err != nil {
		return fmt.Errorf("list crons: %w", err)
	}

	for _, def := range defaults {
		found := false
		for _, job := range existing {
			if job.Type == "subagent" && job.PromptFile == def.promptFile {
				found = true
				break
			}
		}
		if found {
			fmt.Printf("Default %s cron already exists.\n", def.label)
			continue
		}
		if _, err := store.AddCronWithNotifications("subagent", "", def.schedule, "", def.promptFile, "", timezone, false, def.notifyMainSession); err != nil {
			return fmt.Errorf("add default %s cron: %w", def.label, err)
		}
		fmt.Printf("Added default %s cron.\n", def.label)
	}

	return nil
}

func shouldInstallSystemDeps(repoRoot string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	cmd := exec.Command(filepath.Join(repoRoot, "scripts", "setup_machine.sh"), "can-install-system")
	cmd.Dir = repoRoot
	return cmd.Run() == nil
}

func runBootstrapPostSetup(repoRoot, agentRuntime string) error {
	cfg := app.LoadConfig()

	fmt.Println("Running post-bootstrap setup automatically so this workspace is immediately usable.")

	fmt.Println()
	fmt.Println("[1/6] Installing system dependencies with scripts/setup_machine.sh install-system")
	fmt.Println("Reason: bootstrap should leave behind a usable machine baseline, including tools like tmux and crontab used by Goated workflows.")
	if shouldInstallSystemDeps(repoRoot) {
		systemCmd := exec.Command(filepath.Join(repoRoot, "scripts", "setup_machine.sh"), "install-system")
		systemCmd.Dir = repoRoot
		systemCmd.Stdout = os.Stdout
		systemCmd.Stderr = os.Stderr
		if err := systemCmd.Run(); err != nil {
			return fmt.Errorf("run setup_machine.sh install-system: %w", err)
		}
	} else {
		fmt.Println("Skipping install-system on this machine because scripts/setup_machine.sh only supports Ubuntu/Debian right now.")
		fmt.Println("Next step: install tmux, cron/crontab, git, curl, and build tools manually (for example via Homebrew on macOS), then rerun scripts/setup_machine.sh doctor if needed.")
	}

	fmt.Println()
	fmt.Println("[2/6] Installing the managed Go toolchain with scripts/setup_machine.sh install-go")
	fmt.Println("Reason: bootstrap and self repo setup both build Go binaries, so the expected Go version needs to exist before any build step runs.")
	setupCmd := exec.Command(filepath.Join(repoRoot, "scripts", "setup_machine.sh"), "install-go")
	setupCmd.Dir = repoRoot
	setupCmd.Stdout = os.Stdout
	setupCmd.Stderr = os.Stderr
	if err := setupCmd.Run(); err != nil {
		return fmt.Errorf("run setup_machine.sh install-go: %w", err)
	}

	fmt.Println()
	fmt.Println("[3/6] Building binaries with ./build.sh")
	fmt.Println("Reason: bootstrap writes config and seeds self/, but the workspace is not actually runnable until goated and workspace/goat are built.")
	buildCmd := exec.Command(filepath.Join(repoRoot, "build.sh"))
	buildCmd.Dir = repoRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("run build.sh: %w", err)
	}

	fmt.Println()
	fmt.Println("[4/6] Stopping any existing goated daemon")
	fmt.Println("Reason: bootstrap should leave the freshly built daemon running, not fail because an older daemon instance is still holding the pid file.")
	pidPath := filepath.Join(cfg.LogDir, "goated_daemon.pid")
	if oldPID, err := stopDaemon(pidPath); err != nil {
		return fmt.Errorf("stop existing daemon: %w", err)
	} else if oldPID > 0 {
		fmt.Printf("Stopped existing daemon (pid=%d)\n", oldPID)
	} else {
		fmt.Println("No running daemon found.")
	}

	fmt.Println()
	fmt.Println("[5/6] Starting the goated daemon")
	fmt.Println("Reason: a successful bootstrap should leave the gateway running instead of requiring a separate manual start step.")
	goatedBin := filepath.Join(repoRoot, "goated")
	startCmd := exec.Command(goatedBin, "daemon", "run")
	startCmd.Dir = repoRoot
	startCmd.Stdout = os.Stdout
	startCmd.Stderr = os.Stderr
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	if agentRuntime == "pi" {
		fmt.Println()
		fmt.Println("[6/7] Initializing the Pi runtime session")
		fmt.Println("Reason: Pi uses a Goated-managed persisted session; bootstrap should warm it up from GOATED.md so the first real user message lands in an initialized session.")
		runtimeCmd := exec.Command(goatedBin, "runtime", "status")
		runtimeCmd.Dir = repoRoot
		runtimeCmd.Stdout = os.Stdout
		runtimeCmd.Stderr = os.Stderr
		if err := runtimeCmd.Run(); err != nil {
			return fmt.Errorf("initialize pi runtime session: %w", err)
		}
		fmt.Println()
		fmt.Println("[7/7] Installing the watchdog cron")
	} else {
		fmt.Println()
		fmt.Println("[6/6] Installing the watchdog cron")
	}
	fmt.Println("Reason: the watchdog restarts the daemon if it dies, so a fresh bootstrap should also make the service resilient.")
	if err := installWatchdogCron(repoRoot); err != nil {
		return err
	}
	fmt.Println("Watchdog cron installed or already present.")
	return nil
}

func installWatchdogCron(repoRoot string) error {
	watchdogLine := fmt.Sprintf("*/2 * * * * %s/scripts/watchdog.sh", repoRoot)

	listCmd := exec.Command("crontab", "-l")
	listOut, err := listCmd.Output()
	current := ""
	if err != nil {
		var exitErr *exec.ExitError
		if !os.IsNotExist(err) && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
			return fmt.Errorf("read current crontab: %w", err)
		}
	} else {
		current = string(listOut)
	}

	if strings.Contains(current, watchdogLine) {
		return nil
	}

	var buf bytes.Buffer
	current = strings.TrimRight(current, "\n")
	if current != "" {
		buf.WriteString(current)
		buf.WriteByte('\n')
	}
	buf.WriteString(watchdogLine)
	buf.WriteByte('\n')

	installCmd := exec.Command("crontab", "-")
	installCmd.Stdin = &buf
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("install watchdog crontab entry: %w", err)
	}
	return nil
}

func maybeResetBootstrapChannels(reader *bufio.Reader, store *db.Store) error {
	channels, err := store.AllChannels()
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return nil
	}

	activeChannel := store.GetMeta("active_channel")

	fmt.Println()
	fmt.Println("Configured channels already exist:")
	for _, ch := range channels {
		marker := "  "
		if ch.Name == activeChannel {
			marker = "* "
		}
		fmt.Printf("%s%s (%s)\n", marker, ch.Name, ch.Type)
	}
	fmt.Println()
	fmt.Println("You can keep them, delete some, or delete all before creating the replacement channel.")

	deleteInput := prompt(reader, "Channels to delete before starting over (comma-separated names, 'all', or blank to keep)", "")
	deleteInput = strings.TrimSpace(deleteInput)
	if deleteInput == "" {
		return nil
	}

	selected, deleteAll, err := parseChannelDeleteSelection(deleteInput, channels)
	if err != nil {
		return err
	}

	deletedActive := false
	for _, name := range selected {
		if err := store.DeleteChannel(name); err != nil {
			return err
		}
		if name == activeChannel {
			deletedActive = true
		}
		fmt.Printf("Deleted channel %q.\n", name)
	}

	if deleteAll || deletedActive {
		if err := store.SetMeta("active_channel", ""); err != nil {
			return err
		}
	}

	return nil
}

func parseChannelDeleteSelection(input string, channels []db.Channel) ([]string, bool, error) {
	if strings.EqualFold(input, "all") {
		names := make([]string, 0, len(channels))
		for _, ch := range channels {
			names = append(names, ch.Name)
		}
		return names, true, nil
	}

	available := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		available[ch.Name] = struct{}{}
	}

	selected := make([]string, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, raw := range strings.Split(input, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := available[name]; !ok {
			return nil, false, fmt.Errorf("channel %q not found", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, name)
	}

	if len(selected) == 0 {
		return nil, false, fmt.Errorf("no valid channels selected")
	}

	return selected, false, nil
}

// strFromMap safely extracts a string value from a map[string]any.
func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}
