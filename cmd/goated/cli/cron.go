package cli

import (
	"context"
	"fmt"
	"strconv"
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
	cronAddTimezone   string
	cronAddSilent     bool
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
			if cronAddChat == "" {
				return fmt.Errorf("--chat is required for subagent crons")
			}
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

		database, err := db.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		id, err := database.AddCron(cronAddType, cronAddChat, cronAddSchedule, cronAddPrompt, cronAddPromptFile, cronAddCommand, tz, cronAddSilent)
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
			if j.Silent {
				status += ",silent"
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
			fmt.Printf("#%d [%s] type=%s schedule=%q tz=%s chat=%s %s\n", j.ID, status, jobType, j.Schedule, tzDisplay, j.ChatID, detail)
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

var cronSetSilentCmd = &cobra.Command{
	Use:   "set-silent ID true|false",
	Short: "Set whether a cron job suppresses success notifications",
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

func init() {
	cronAddCmd.Flags().StringVar(&cronAddType, "type", "subagent", "Cron type: subagent or system")
	cronAddCmd.Flags().StringVar(&cronAddChat, "chat", "", "Chat ID for notifications")
	cronAddCmd.Flags().StringVar(&cronAddSchedule, "schedule", "", "Cron schedule (5-field)")
	cronAddCmd.Flags().StringVar(&cronAddPrompt, "prompt", "", "Inline prompt to execute (subagent)")
	cronAddCmd.Flags().StringVar(&cronAddPromptFile, "prompt-file", "", "Path to a prompt file (subagent)")
	cronAddCmd.Flags().StringVar(&cronAddCommand, "command", "", "Shell command to run (system)")
	cronAddCmd.Flags().StringVar(&cronAddTimezone, "timezone", "", "IANA timezone (e.g. UTC, America/Los_Angeles). Defaults to GOAT_DEFAULT_TIMEZONE.")
	cronAddCmd.Flags().BoolVar(&cronAddSilent, "silent", false, "Suppress success notifications to the main session")

	cronListCmd.Flags().StringVar(&cronListChat, "chat", "", "Filter by chat ID (optional)")

	cronCmd.AddCommand(cronRunCmd, cronAddCmd, cronListCmd, cronEnableCmd, cronDisableCmd, cronRemoveCmd, cronSetScheduleCmd, cronSetTimezoneCmd, cronSetSilentCmd)
	rootCmd.AddCommand(cronCmd)
}
