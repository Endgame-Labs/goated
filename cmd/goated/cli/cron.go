package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/app"
	cronpkg "goated/internal/cron"
	"goated/internal/db"
	runtimepkg "goated/internal/runtime"
)

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Cron job management",
}

var cronRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute due cron jobs for the current minute",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()

		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		runtime, err := runtimepkg.New(cfg)
		if err != nil {
			return err
		}

		runner := &cronpkg.Runner{
			Store:        database,
			WorkspaceDir: cfg.WorkspaceDir,
			LogDir:       cfg.LogDir,
			Headless:     runtime.Headless(),
		}

		now := time.Now()
		fmt.Printf("Running cron check for %s\n", now.Format(time.RFC3339))
		return runner.Run(context.Background(), now)
	},
}

var (
	cronAddType       string
	cronAddChat       string
	cronAddSchedule   string
	cronAddPrompt     string
	cronAddPromptFile string
	cronAddCommand    string
	cronAddModel      string
	cronAddTimezone   string
	cronAddSilent     bool
	cronAddNotifyUser bool
	cronAddNotifyMain bool
)

var cronAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new cron job",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cronAddSchedule == "" {
			return fmt.Errorf("--schedule is required")
		}
		if cronAddType == "system" {
			if cronAddCommand == "" {
				return fmt.Errorf("--command is required for system crons")
			}
		} else {
			if cronAddPrompt == "" && cronAddPromptFile == "" {
				return fmt.Errorf("either --prompt or --prompt-file is required")
			}
			if cronAddPrompt != "" && cronAddPromptFile != "" {
				return fmt.Errorf("--prompt and --prompt-file are mutually exclusive")
			}
		}
		cfg := app.LoadConfig()

		tz := cronAddTimezone
		if tz == "" {
			tz = cfg.DefaultTimezone
		}
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid IANA timezone %q: %w", tz, err)
		}
		notifyUserSet := cmd.Flags().Lookup("notify-user").Changed
		notifyMainSessionSet := cmd.Flags().Lookup("notify-main-session").Changed
		chatID := strings.TrimSpace(cronAddChat)
		notifyUser := cronAddNotifyUser
		if !notifyUserSet {
			notifyUser = chatID != "" && !cronAddSilent
		}
		if notifyUser && chatID == "" {
			return fmt.Errorf("--chat is required when --notify-user is true")
		}
		notifyMainSession := cronAddNotifyMain
		if !notifyMainSessionSet {
			notifyMainSession = !cronAddSilent
		}

		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		id, err := database.AddCronWithNotifications(cronAddType, chatID, cronAddSchedule, cronAddPrompt, cronAddPromptFile, cronAddCommand, tz, strings.TrimSpace(cronAddModel), notifyUser, notifyMainSession)
		if err != nil {
			return err
		}
		fmt.Printf("Created cron %d\n", id)
		return nil
	},
}

var cronListChat string

var cronListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cron jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		jobs, err := database.AllCrons()
		if err != nil {
			return err
		}

		if cronListChat != "" {
			var filtered []db.CronJob
			for _, j := range jobs {
				if j.ChatID == cronListChat {
					filtered = append(filtered, j)
				}
			}
			jobs = filtered
		}

		if len(jobs) == 0 {
			fmt.Println("(no cron jobs)")
			return nil
		}
		for _, j := range jobs {
			status := "active"
			if !j.Active {
				status = "disabled"
			}
			jobType := j.Type
			if jobType == "" {
				jobType = "subagent"
			}
			tzDisplay := j.Timezone
			if tzDisplay == "" {
				tzDisplay = "UTC"
			}
			var detail string
			if jobType == "system" {
				detail = fmt.Sprintf("command=%q", j.Command)
			} else if j.PromptFile != "" {
				detail = fmt.Sprintf("prompt-file=%q", j.PromptFile)
			} else {
				detail = fmt.Sprintf("prompt=%q", j.Prompt)
			}
			modelDisplay := ""
			if j.Model != "" {
				modelDisplay = fmt.Sprintf(" model=%s", j.Model)
			}
			fmt.Printf("#%d [%s] type=%s schedule=%q tz=%s notify_user=%t chat=%s notify_main_session=%t%s %s\n",
				j.ID,
				status,
				jobType,
				j.Schedule,
				tzDisplay,
				j.EffectiveNotifyUser(),
				j.ChatID,
				j.EffectiveNotifyMainSession(),
				modelDisplay,
				detail,
			)
		}
		return nil
	},
}

var cronEnableCmd = &cobra.Command{
	Use:   "enable ID",
	Short: "Enable a cron job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronActive(id, true); err != nil {
			return err
		}
		fmt.Printf("Enabled cron %d\n", id)
		return nil
	},
}

var cronDisableCmd = &cobra.Command{
	Use:   "disable ID",
	Short: "Disable a cron job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronActive(id, false); err != nil {
			return err
		}
		fmt.Printf("Disabled cron %d\n", id)
		return nil
	},
}

var cronRemoveCmd = &cobra.Command{
	Use:   "remove ID",
	Short: "Remove a cron job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.DeleteCron(id); err != nil {
			return err
		}
		fmt.Printf("Removed cron %d\n", id)
		return nil
	},
}

var cronSetScheduleCmd = &cobra.Command{
	Use:   "set-schedule ID SCHEDULE",
	Short: "Set the cron schedule (5-field cron expression)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		schedule := args[1]
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronSchedule(id, schedule); err != nil {
			return err
		}
		fmt.Printf("Set cron %d schedule to %s\n", id, schedule)
		return nil
	},
}

var cronSetTimezoneCmd = &cobra.Command{
	Use:   "set-timezone ID TIMEZONE",
	Short: "Set the IANA timezone for a cron job (e.g. UTC, America/Los_Angeles)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		tz := args[1]
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid IANA timezone %q: %w", tz, err)
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronTimezone(id, tz); err != nil {
			return err
		}
		fmt.Printf("Set cron %d timezone to %s\n", id, tz)
		return nil
	},
}

var cronSetModelCmd = &cobra.Command{
	Use:   "set-model ID [MODEL]",
	Short: "Set or clear the model a cron runs with; omit MODEL (or pass \"\") to use the runtime default",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 || len(args) > 2 {
			return fmt.Errorf("accepts ID [MODEL]")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		model := ""
		if len(args) == 2 {
			model = args[1]
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronModel(id, model); err != nil {
			return err
		}
		if strings.TrimSpace(model) == "" {
			fmt.Printf("Cleared model for cron %d (using runtime default)\n", id)
		} else {
			fmt.Printf("Set cron %d model to %s\n", id, model)
		}
		return nil
	},
}

var cronSetSilentCmd = &cobra.Command{
	Use:   "set-silent ID true|false",
	Short: "Deprecated: map legacy silent semantics onto notify_user/notify_main_session",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		silent, err := strconv.ParseBool(args[1])
		if err != nil {
			return fmt.Errorf("invalid bool %q: use true or false", args[1])
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronSilent(id, silent); err != nil {
			return err
		}
		fmt.Printf("Set cron %d silent=%v\n", id, silent)
		return nil
	},
}

var cronSetNotifyUserCmd = &cobra.Command{
	Use:   "set-notify-user ID true|false [CHAT_ID]",
	Short: "Set whether a cron notifies the user; CHAT_ID is required when enabling",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("accepts ID true|false [CHAT_ID]")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		notifyUser, err := strconv.ParseBool(args[1])
		if err != nil {
			return fmt.Errorf("invalid bool %q: use true or false", args[1])
		}
		chatID := ""
		if len(args) == 3 {
			chatID = args[2]
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronNotifyUser(id, notifyUser, chatID); err != nil {
			return err
		}
		fmt.Printf("Set cron %d notify_user=%v chat=%s\n", id, notifyUser, chatID)
		return nil
	},
}

var cronSetNotifyMainSessionCmd = &cobra.Command{
	Use:   "set-notify-main-session ID true|false",
	Short: "Set whether a cron sends internal notices to the main session",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
		notifyMainSession, err := strconv.ParseBool(args[1])
		if err != nil {
			return fmt.Errorf("invalid bool %q: use true or false", args[1])
		}
		cfg := app.LoadConfig()
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.SetCronNotifyMainSession(id, notifyMainSession); err != nil {
			return err
		}
		fmt.Printf("Set cron %d notify_main_session=%v\n", id, notifyMainSession)
		return nil
	},
}

func init() {
	cronAddCmd.Flags().StringVar(&cronAddType, "type", "subagent", "Cron type: subagent or system")
	cronAddCmd.Flags().StringVar(&cronAddChat, "chat", "", "Chat ID for user notifications; leave blank if not notifying the user")
	cronAddCmd.Flags().StringVar(&cronAddSchedule, "schedule", "", "Cron schedule (5-field)")
	cronAddCmd.Flags().StringVar(&cronAddPrompt, "prompt", "", "Inline prompt to execute (subagent)")
	cronAddCmd.Flags().StringVar(&cronAddPromptFile, "prompt-file", "", "Path to a prompt file (subagent)")
	cronAddCmd.Flags().StringVar(&cronAddCommand, "command", "", "Shell command to run (system)")
	cronAddCmd.Flags().StringVar(&cronAddModel, "model", "", "Optional model to run this cron with (e.g. claude-haiku-4-5). Empty uses the runtime default.")
	cronAddCmd.Flags().StringVar(&cronAddTimezone, "timezone", "", "IANA timezone (e.g. UTC, America/Los_Angeles). Defaults to GOAT_DEFAULT_TIMEZONE.")
	cronAddCmd.Flags().BoolVar(&cronAddSilent, "silent", false, "Deprecated legacy shorthand: disable both user and main-session success notifications")
	cronAddCmd.Flags().BoolVar(&cronAddNotifyUser, "notify-user", false, "Notify the user directly for this cron")
	cronAddCmd.Flags().BoolVar(&cronAddNotifyMain, "notify-main-session", true, "Send internal notices to the main interactive session")

	cronListCmd.Flags().StringVar(&cronListChat, "chat", "", "Filter by chat ID (optional)")

	cronCmd.AddCommand(cronRunCmd, cronAddCmd, cronListCmd, cronEnableCmd, cronDisableCmd, cronRemoveCmd, cronSetScheduleCmd, cronSetTimezoneCmd, cronSetModelCmd, cronSetSilentCmd, cronSetNotifyUserCmd, cronSetNotifyMainSessionCmd)
	rootCmd.AddCommand(cronCmd)
}
