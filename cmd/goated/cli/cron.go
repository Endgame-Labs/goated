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

		runner := &cronpkg.Runner{
			Store:        database,
			WorkspaceDir: cfg.WorkspaceDir,
			LogDir:       cfg.LogDir,
		}

		now := time.Now()
		fmt.Printf("Running cron check for %s\n", now.Format(time.RFC3339))
		return runner.Run(context.Background(), now)
	},
}

var (
	cronAddChat       string
	cronAddSchedule   string
	cronAddPrompt     string
	cronAddPromptFile string
	cronAddTimezone   string
)

var cronAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new cron job",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cronAddChat == "" || cronAddSchedule == "" {
			return fmt.Errorf("--chat and --schedule are required")
		}
		if cronAddPrompt == "" && cronAddPromptFile == "" {
			return fmt.Errorf("either --prompt or --prompt-file is required")
		}
		if cronAddPrompt != "" && cronAddPromptFile != "" {
			return fmt.Errorf("--prompt and --prompt-file are mutually exclusive")
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

		id, err := database.AddCron(cronAddChat, cronAddSchedule, cronAddPrompt, cronAddPromptFile, tz)
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
			promptDisplay := fmt.Sprintf("prompt=%q", j.Prompt)
			if j.PromptFile != "" {
				promptDisplay = fmt.Sprintf("prompt-file=%q", j.PromptFile)
			}
			tzDisplay := j.Timezone
			if tzDisplay == "" {
				tzDisplay = "UTC"
			}
			fmt.Printf("#%d [%s] schedule=%q tz=%s chat=%s %s\n", j.ID, status, j.Schedule, tzDisplay, j.ChatID, promptDisplay)
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

func init() {
	cronAddCmd.Flags().StringVar(&cronAddChat, "chat", "", "Chat ID for notifications")
	cronAddCmd.Flags().StringVar(&cronAddSchedule, "schedule", "", "Cron schedule (5-field)")
	cronAddCmd.Flags().StringVar(&cronAddPrompt, "prompt", "", "Inline prompt to execute")
	cronAddCmd.Flags().StringVar(&cronAddPromptFile, "prompt-file", "", "Path to a prompt file (read at execution time)")
	cronAddCmd.Flags().StringVar(&cronAddTimezone, "timezone", "", "IANA timezone (e.g. UTC, America/Los_Angeles). Defaults to GOAT_DEFAULT_TIMEZONE.")

	cronListCmd.Flags().StringVar(&cronListChat, "chat", "", "Filter by chat ID (optional)")

	cronCmd.AddCommand(cronRunCmd, cronAddCmd, cronListCmd, cronEnableCmd, cronDisableCmd, cronRemoveCmd, cronSetScheduleCmd, cronSetTimezoneCmd)
	rootCmd.AddCommand(cronCmd)
}
