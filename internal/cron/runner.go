package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"goated/internal/db"
	"goated/internal/subagent"
)

type Runner struct {
	Store        *db.Store
	WorkspaceDir string
	LogDir       string
	Notifier     Notifier
}

type Notifier interface {
	SendMessage(ctx context.Context, chatID, text string) error
}

type runRecord struct {
	Minute      string `json:"minute"`
	CronID      uint64 `json:"cron_id"`
	ChatID      string `json:"chat_id"`
	Schedule    string `json:"schedule"`
	Status      string `json:"status"`
	UserMessage string `json:"user_message,omitempty"`
	JobLogPath  string `json:"job_log_path"`
}

func (r *Runner) Run(ctx context.Context, now time.Time) error {
	nowMinute := now.UTC().Truncate(time.Minute)
	jobs, err := r.dueJobs(nowMinute)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(r.LogDir, "cron", "jobs"), 0o755); err != nil {
		return fmt.Errorf("mkdir cron jobs log dir: %w", err)
	}

	records := make([]runRecord, 0, len(jobs))
	for _, job := range jobs {
		// Skip if a previous run of this cron job is still in-flight
		if r.Store.CronJobRunning(job.ID) {
			fmt.Fprintf(os.Stderr, "[%s] cron #%d still running, skipping\n",
				time.Now().Format(time.RFC3339), job.ID)
			continue
		}
		rec, err := r.runOne(ctx, nowMinute, job)
		if err != nil {
			return err
		}
		records = append(records, rec)
	}
	return appendRunRecords(filepath.Join(r.LogDir, "cron", "runs.jsonl"), records)
}

func (r *Runner) dueJobs(nowMinute time.Time) ([]db.CronJob, error) {
	all, err := r.Store.ActiveCrons()
	if err != nil {
		return nil, fmt.Errorf("query crons: %w", err)
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	var due []db.CronJob
	for _, c := range all {
		loc, err := time.LoadLocation(c.Timezone)
		if err != nil {
			loc = time.Local
		}
		s, err := parser.Parse(c.Schedule)
		if err != nil {
			continue
		}
		nowInLoc := nowMinute.In(loc)
		prev := nowInLoc.Add(-time.Minute)
		next := s.Next(prev)
		if next.Equal(nowInLoc) {
			due = append(due, c)
		}
	}
	return due, nil
}

const cronJobTimeout = 1 * time.Hour

func (r *Runner) runOne(ctx context.Context, nowMinute time.Time, job db.CronJob) (runRecord, error) {
	runMinute := nowMinute.Format(time.RFC3339)

	if err := r.Store.RecordCronRun(job.ID, runMinute, "started", "", ""); err != nil {
		return runRecord{}, fmt.Errorf("insert cron run: %w", err)
	}

	// Enforce a per-job timeout so hung subagents don't block the cron ticker forever
	jobCtx, jobCancel := context.WithTimeout(ctx, cronJobTimeout)
	defer jobCancel()

	jobLog := filepath.Join(r.LogDir, "cron", "jobs", fmt.Sprintf("%s-cron-%d.log", nowMinute.Format("20060102-1504"), job.ID))

	userPrompt := job.Prompt
	if job.PromptFile != "" {
		data, err := os.ReadFile(job.PromptFile)
		if err != nil {
			return runRecord{}, fmt.Errorf("read prompt file %s: %w", job.PromptFile, err)
		}
		userPrompt = string(data)
	}
	if strings.TrimSpace(userPrompt) == "" {
		return runRecord{}, fmt.Errorf("cron #%d has empty prompt", job.ID)
	}

	prompt := subagent.BuildPrompt("Read CRON.md before executing.", userPrompt, job.ChatID, "cron", jobLog)
	_, err := subagent.RunSync(jobCtx, r.Store, subagent.RunOpts{
		WorkspaceDir: r.WorkspaceDir,
		Prompt:       prompt,
		LogPath:      jobLog,
		Source:       "cron",
		CronID:       job.ID,
		ChatID:       job.ChatID,
	})
	status := "ok"
	if err != nil {
		status = "error"
	}

	if err := r.Store.RecordCronRun(job.ID, runMinute, status, "", jobLog); err != nil {
		return runRecord{}, fmt.Errorf("update cron run: %w", err)
	}

	// On error, notify the user so they know something went wrong
	if status == "error" && r.Notifier != nil {
		errNotify := fmt.Sprintf("Cron job #%d failed. Check log: %s", job.ID, jobLog)
		_ = r.Notifier.SendMessage(ctx, job.ChatID, errNotify)
	}

	return runRecord{
		Minute:     runMinute,
		CronID:     job.ID,
		ChatID:     job.ChatID,
		Schedule:   job.Schedule,
		Status:     status,
		JobLogPath: jobLog,
	}, nil
}

func appendRunRecords(path string, records []runRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir runs jsonl dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open runs jsonl: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("write runs jsonl: %w", err)
		}
	}
	return nil
}

