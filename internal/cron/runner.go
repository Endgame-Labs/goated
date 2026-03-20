package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"goated/internal/agent"
	"goated/internal/db"
	"goated/internal/subagent"
)

type Runner struct {
	Store        *db.Store
	WorkspaceDir string
	LogDir       string
	Notifier     Notifier
	Headless     agent.HeadlessRuntime
}

type Notifier interface {
	SendMessage(ctx context.Context, chatID, text string) error
}

type runRecord struct {
	Minute          string `json:"minute"`
	CronID          uint64 `json:"cron_id"`
	ChatID          string `json:"chat_id"`
	Schedule        string `json:"schedule"`
	Status          string `json:"status"`
	UserMessage     string `json:"user_message,omitempty"`
	JobLogPath      string `json:"job_log_path"`
	RuntimeProvider string `json:"runtime_provider,omitempty"`
	RuntimeMode     string `json:"runtime_mode,omitempty"`
	RuntimeVersion  string `json:"runtime_version,omitempty"`
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
	runtimeMeta := db.ExecutionRuntime{}
	if r.Headless != nil {
		version := r.Headless.Version(context.Background())
		runtimeMeta = db.ExecutionRuntime{
			Provider: string(r.Headless.Descriptor().Provider),
			Mode:     "headless_exec",
			Version:  version,
		}
	}

	if err := r.Store.RecordCronRun(job.ID, runMinute, "started", "", "", runtimeMeta); err != nil {
		return runRecord{}, fmt.Errorf("insert cron run: %w", err)
	}

	jobCtx, jobCancel := context.WithTimeout(ctx, cronJobTimeout)
	defer jobCancel()

	jobLog := filepath.Join(r.LogDir, "cron", "jobs", fmt.Sprintf("%s-cron-%d.log", nowMinute.Format("20060102-1504"), job.ID))

	var (
		status string
		result agent.HeadlessResult
	)
	if job.Type == "system" {
		status = r.runSystem(jobCtx, job, jobLog)
	} else {
		status, result = r.runSubagent(jobCtx, job, jobLog)
	}

	if result.RuntimeProvider != "" {
		runtimeMeta = db.ExecutionRuntime{
			Provider: result.RuntimeProvider,
			Mode:     result.RuntimeMode,
			Version:  result.RuntimeVersion,
		}
	}
	if err := r.Store.RecordCronRun(job.ID, runMinute, status, "", jobLog, runtimeMeta); err != nil {
		return runRecord{}, fmt.Errorf("update cron run: %w", err)
	}

	if status == "error" && r.Notifier != nil && job.ChatID != "" {
		errNotify := fmt.Sprintf("Cron job #%d failed. Check log: %s", job.ID, jobLog)
		_ = r.Notifier.SendMessage(ctx, job.ChatID, errNotify)
	}

	return runRecord{
		Minute:          runMinute,
		CronID:          job.ID,
		ChatID:          job.ChatID,
		Schedule:        job.Schedule,
		Status:          status,
		JobLogPath:      jobLog,
		RuntimeProvider: runtimeMeta.Provider,
		RuntimeMode:     runtimeMeta.Mode,
		RuntimeVersion:  runtimeMeta.Version,
	}, nil
}

func (r *Runner) runSubagent(ctx context.Context, job db.CronJob, jobLog string) (string, agent.HeadlessResult) {
	userPrompt := job.Prompt
	if job.PromptFile != "" {
		data, err := os.ReadFile(job.PromptFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cron #%d: read prompt file: %v\n", job.ID, err)
			return "error", agent.HeadlessResult{}
		}
		userPrompt = string(data)
	}
	if strings.TrimSpace(userPrompt) == "" {
		fmt.Fprintf(os.Stderr, "cron #%d: empty prompt\n", job.ID)
		return "error", agent.HeadlessResult{}
	}
	if r.Headless == nil {
		fmt.Fprintf(os.Stderr, "cron #%d: no headless runtime configured\n", job.ID)
		return "error", agent.HeadlessResult{}
	}

	promptChatID := job.ChatID
	if job.Silent {
		promptChatID = ""
	}
	prompt := subagent.BuildPrompt(subagent.BuildPreamble("Read CRON.md before executing."), userPrompt, promptChatID, "cron", jobLog)
	result, err := r.Headless.RunSync(ctx, r.Store, agent.HeadlessRequest{
		WorkspaceDir: r.WorkspaceDir,
		Prompt:       prompt,
		LogPath:      jobLog,
		Source:       "cron",
		CronID:       job.ID,
		ChatID:       job.ChatID,
		Silent:       job.Silent,
		LogCaller:    fmt.Sprintf("cron-%d", job.ID),
	})
	if err != nil {
		return "error", result
	}
	return "ok", result
}

func (r *Runner) runSystem(ctx context.Context, job db.CronJob, jobLog string) string {
	logFile, err := os.Create(jobLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cron #%d: create log: %v\n", job.ID, err)
		return "error"
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, "bash", "-c", job.Command)
	cmd.Dir = r.WorkspaceDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("LOG_CALLER=cron-%d", job.ID))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	fmt.Fprintf(logFile, "$ %s\n\n", job.Command)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(logFile, "\n[exit error: %v]\n", err)
		return "error"
	}
	return "ok"
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
