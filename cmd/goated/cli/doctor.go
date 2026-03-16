package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"goated/internal/agent"
	"goated/internal/app"
	"goated/internal/db"
)

// checkResult is a single diagnostic check result.
type checkResult struct {
	Name    string
	OK      bool
	Detail  string
	FixHint string // shown when !OK
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose common configuration and setup problems",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		checks := runDoctorChecks(cfg)
		printDoctorResults(checks)

		failures := 0
		for _, c := range checks {
			if !c.OK {
				failures++
			}
		}
		if failures > 0 {
			return fmt.Errorf("%d check(s) failed", failures)
		}
		return nil
	},
}

// runDoctorChecks runs all diagnostic checks and returns results.
func runDoctorChecks(cfg app.Config) []checkResult {
	var checks []checkResult

	// 1. .env exists
	checks = append(checks, checkDotEnv())

	// 2. Runtime value is valid
	checks = append(checks, checkRuntimeValue(cfg.AgentRuntime))

	// 3. Runtime binary on PATH
	checks = append(checks, checkRuntimeBinary(cfg.AgentRuntime))

	// 4. tmux (only for TUI runtimes)
	if isTUIRuntime(cfg.AgentRuntime) {
		checks = append(checks, checkTmux())
	}

	// 5. Workspace directory
	checks = append(checks, checkWorkspaceDir(cfg.WorkspaceDir))

	// 6. workspace/goat binary
	checks = append(checks, checkGoatBinary(cfg.WorkspaceDir))

	// 7. workspace/GOATED.md
	checks = append(checks, checkGoatedMD(cfg.WorkspaceDir))

	// 8. Database
	checks = append(checks, checkDatabase(cfg.DBPath))

	// 9. Gateway config
	checks = append(checks, checkGateway(cfg))

	return checks
}

func checkDotEnv() checkResult {
	if _, err := os.Stat(".env"); err != nil {
		return checkResult{
			Name:    ".env file",
			OK:      false,
			Detail:  "not found in current directory",
			FixHint: "Run: ./goated bootstrap",
		}
	}
	return checkResult{Name: ".env file", OK: true, Detail: "found"}
}

func checkRuntimeValue(runtime string) checkResult {
	switch agent.RuntimeProvider(runtime) {
	case "", agent.RuntimeClaude, agent.RuntimeClaudeTUI, agent.RuntimeCodexTUI:
		display := runtime
		if display == "" {
			display = "(empty, defaults to claude)"
		}
		return checkResult{
			Name:   "GOAT_AGENT_RUNTIME",
			OK:     true,
			Detail: display,
		}
	default:
		return checkResult{
			Name:    "GOAT_AGENT_RUNTIME",
			OK:      false,
			Detail:  fmt.Sprintf("unknown value %q", runtime),
			FixHint: "Valid values: claude, claude_tui, codex_tui. Update .env or run: ./goated bootstrap",
		}
	}
}

func checkRuntimeBinary(runtime string) checkResult {
	var binary string
	switch agent.RuntimeProvider(runtime) {
	case "", agent.RuntimeClaude, agent.RuntimeClaudeTUI:
		binary = "claude"
	case agent.RuntimeCodexTUI:
		binary = "codex"
	default:
		return checkResult{
			Name:   "runtime binary",
			OK:     false,
			Detail: fmt.Sprintf("cannot determine binary for runtime %q", runtime),
		}
	}

	path, err := exec.LookPath(binary)
	if err != nil {
		hint := fmt.Sprintf("Install %s and ensure it's on your PATH", binary)
		if binary == "claude" {
			hint = "Install Claude Code: npm install -g @anthropic-ai/claude-code"
		}
		return checkResult{
			Name:    fmt.Sprintf("%s binary", binary),
			OK:      false,
			Detail:  "not found on PATH",
			FixHint: hint,
		}
	}
	return checkResult{
		Name:   fmt.Sprintf("%s binary", binary),
		OK:     true,
		Detail: path,
	}
}

func checkTmux() checkResult {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return checkResult{
			Name:    "tmux",
			OK:      false,
			Detail:  "not found on PATH (required for TUI runtimes)",
			FixHint: "Install tmux: brew install tmux (macOS) or apt install tmux (Linux)",
		}
	}
	return checkResult{Name: "tmux", OK: true, Detail: path}
}

func checkWorkspaceDir(dir string) checkResult {
	info, err := os.Stat(dir)
	if err != nil {
		return checkResult{
			Name:    "workspace directory",
			OK:      false,
			Detail:  fmt.Sprintf("%s not found", dir),
			FixHint: "Run: ./build.sh",
		}
	}
	if !info.IsDir() {
		return checkResult{
			Name:    "workspace directory",
			OK:      false,
			Detail:  fmt.Sprintf("%s exists but is not a directory", dir),
			FixHint: "Remove the file and run: ./build.sh",
		}
	}
	return checkResult{Name: "workspace directory", OK: true, Detail: dir}
}

func checkGoatBinary(workspaceDir string) checkResult {
	goatPath := filepath.Join(workspaceDir, "goat")
	info, err := os.Stat(goatPath)
	if err != nil {
		return checkResult{
			Name:    "workspace/goat binary",
			OK:      false,
			Detail:  "not found",
			FixHint: "Run: ./build.sh",
		}
	}
	if info.Mode()&0o111 == 0 {
		return checkResult{
			Name:    "workspace/goat binary",
			OK:      false,
			Detail:  "exists but not executable",
			FixHint: fmt.Sprintf("Run: chmod +x %s", goatPath),
		}
	}
	return checkResult{Name: "workspace/goat binary", OK: true, Detail: goatPath}
}

func checkGoatedMD(workspaceDir string) checkResult {
	path := filepath.Join(workspaceDir, "GOATED.md")
	if _, err := os.Stat(path); err != nil {
		return checkResult{
			Name:    "workspace/GOATED.md",
			OK:      false,
			Detail:  "not found",
			FixHint: "Run: ./build.sh (or check that your workspace is set up correctly)",
		}
	}
	return checkResult{Name: "workspace/GOATED.md", OK: true, Detail: path}
}

func checkDatabase(dbPath string) checkResult {
	store, err := db.Open(dbPath)
	if err != nil {
		return checkResult{
			Name:    "database",
			OK:      false,
			Detail:  fmt.Sprintf("cannot open %s: %v", dbPath, err),
			FixHint: "Run: ./goated bootstrap",
		}
	}
	store.Close()
	return checkResult{Name: "database", OK: true, Detail: dbPath}
}

func checkGateway(cfg app.Config) checkResult {
	switch cfg.Gateway {
	case "slack":
		var missing []string
		if cfg.SlackBotToken == "" {
			missing = append(missing, "GOAT_SLACK_BOT_TOKEN")
		}
		if cfg.SlackAppToken == "" {
			missing = append(missing, "GOAT_SLACK_APP_TOKEN")
		}
		if cfg.SlackChannelID == "" {
			missing = append(missing, "GOAT_SLACK_CHANNEL_ID")
		}
		if len(missing) > 0 {
			return checkResult{
				Name:    "gateway (slack)",
				OK:      false,
				Detail:  fmt.Sprintf("missing: %s", strings.Join(missing, ", ")),
				FixHint: "Set the missing environment variables in .env",
			}
		}
		return checkResult{Name: "gateway (slack)", OK: true, Detail: "configured"}

	default: // telegram
		if cfg.TelegramBotToken == "" {
			return checkResult{
				Name:    "gateway (telegram)",
				OK:      false,
				Detail:  "GOAT_TELEGRAM_BOT_TOKEN not set",
				FixHint: "Set GOAT_TELEGRAM_BOT_TOKEN in .env (get one from @BotFather on Telegram)",
			}
		}
		return checkResult{Name: "gateway (telegram)", OK: true, Detail: "configured"}
	}
}

// printDoctorResults prints diagnostic results in a readable format.
func printDoctorResults(checks []checkResult) {
	fmt.Println("=== goated doctor ===")
	fmt.Println()

	passed, failed := 0, 0
	for _, c := range checks {
		if c.OK {
			fmt.Printf("  OK  %s: %s\n", c.Name, c.Detail)
			passed++
		} else {
			fmt.Printf("  FAIL  %s: %s\n", c.Name, c.Detail)
			if c.FixHint != "" {
				fmt.Printf("        fix: %s\n", c.FixHint)
			}
			failed++
		}
	}

	fmt.Println()
	if failed == 0 {
		fmt.Printf("All %d checks passed.\n", passed)
	} else {
		fmt.Printf("%d passed, %d failed.\n", passed, failed)
	}
}

// runPreflightChecks runs a subset of doctor checks suitable for daemon startup.
// Returns a list of failures (empty if all good).
func runPreflightChecks(cfg app.Config) []checkResult {
	all := runDoctorChecks(cfg)
	var failures []checkResult
	for _, c := range all {
		if !c.OK {
			failures = append(failures, c)
		}
	}
	return failures
}

func isTUIRuntime(runtime string) bool {
	switch agent.RuntimeProvider(runtime) {
	case agent.RuntimeClaudeTUI, agent.RuntimeCodexTUI:
		return true
	}
	return false
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
