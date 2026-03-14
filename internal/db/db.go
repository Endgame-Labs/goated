package db

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	cronsBucket        = []byte("crons")
	cronRunsBucket     = []byte("cron_runs")
	subagentRunsBucket = []byte("subagent_runs")
	metaBucket         = []byte("meta")
	channelsBucket     = []byte("channels")

	allBuckets = [][]byte{cronsBucket, cronRunsBucket, subagentRunsBucket, metaBucket, channelsBucket}
)

type Store struct {
	path string
}

type CronJob struct {
	ID         uint64 `json:"id"`
	Type       string `json:"type"` // "subagent" (default) or "system"
	ChatID     string `json:"chat_id"`
	Schedule   string `json:"schedule"`
	Prompt     string `json:"prompt,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	Command    string `json:"command,omitempty"` // shell command for type="system"
	Timezone   string `json:"timezone"`
	Silent     bool   `json:"silent"`
	Active     bool   `json:"active"`
	CreatedAt  string `json:"created_at"`
}

type CronRun struct {
	ID              uint64 `json:"id"`
	CronID          uint64 `json:"cron_id"`
	RunMinute       string `json:"run_minute"`
	Status          string `json:"status"`
	UserMsg         string `json:"user_message,omitempty"`
	JobLogPath      string `json:"job_log_path,omitempty"`
	RuntimeProvider string `json:"runtime_provider,omitempty"`
	RuntimeMode     string `json:"runtime_mode,omitempty"`
	RuntimeVersion  string `json:"runtime_version,omitempty"`
}

type ExecutionRuntime struct {
	Provider string
	Mode     string
	Version  string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	// Open once to ensure buckets exist, then close immediately.
	bdb, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bolt: %w", err)
	}
	err = bdb.Update(func(tx *bolt.Tx) error {
		for _, bucket := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = bdb.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}
	if err := bdb.Close(); err != nil {
		return nil, fmt.Errorf("close after init: %w", err)
	}
	return &Store{path: path}, nil
}

// Close is a no-op since we no longer hold a persistent handle.
func (s *Store) Close() error {
	return nil
}

// open acquires the DB for a short-lived operation.
func (s *Store) open() (*bolt.DB, error) {
	return bolt.Open(s.path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
}

// view opens the DB, runs a read-only transaction, and closes the DB.
func (s *Store) view(fn func(tx *bolt.Tx) error) error {
	bdb, err := s.open()
	if err != nil {
		return fmt.Errorf("open bolt: %w", err)
	}
	defer bdb.Close()
	return bdb.View(fn)
}

// update opens the DB, runs a read-write transaction, and closes the DB.
func (s *Store) update(fn func(tx *bolt.Tx) error) error {
	bdb, err := s.open()
	if err != nil {
		return fmt.Errorf("open bolt: %w", err)
	}
	defer bdb.Close()
	return bdb.Update(fn)
}

func (s *Store) AddCron(cronType, chatID, schedule, prompt, promptFile, command, timezone string, silent bool) (uint64, error) {
	if cronType == "" {
		cronType = "subagent"
	}
	switch cronType {
	case "system":
		if command == "" {
			return 0, fmt.Errorf("system crons require --command")
		}
	case "subagent":
		if prompt == "" && promptFile == "" {
			return 0, fmt.Errorf("subagent crons require --prompt or --prompt-file")
		}
	default:
		return 0, fmt.Errorf("unknown cron type %q (use subagent or system)", cronType)
	}
	var id uint64
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		seq, _ := b.NextSequence()
		id = seq
		job := CronJob{
			ID:         id,
			Type:       cronType,
			ChatID:     chatID,
			Schedule:   schedule,
			Prompt:     prompt,
			PromptFile: promptFile,
			Command:    command,
			Timezone:   timezone,
			Silent:     silent,
			Active:     true,
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		data, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return b.Put(itob(id), data)
	})
	return id, err
}

func (s *Store) ActiveCrons() ([]CronJob, error) {
	var jobs []CronJob
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		return b.ForEach(func(k, v []byte) error {
			var job CronJob
			if err := json.Unmarshal(v, &job); err != nil {
				return nil // skip corrupt entries
			}
			if job.Active {
				jobs = append(jobs, job)
			}
			return nil
		})
	})
	return jobs, err
}

func (s *Store) RecordCronRun(cronID uint64, runMinute, status, userMsg, jobLogPath string, runtime ExecutionRuntime) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronRunsBucket)
		// Dedup: check if this cron+minute already recorded
		dedupKey := fmt.Sprintf("%d:%s", cronID, runMinute)
		if existing := b.Get([]byte(dedupKey)); existing != nil {
			// Update existing
			var run CronRun
			if err := json.Unmarshal(existing, &run); err == nil {
				run.Status = status
				run.UserMsg = userMsg
				run.JobLogPath = jobLogPath
				run.RuntimeProvider = runtime.Provider
				run.RuntimeMode = runtime.Mode
				run.RuntimeVersion = runtime.Version
				data, err := json.Marshal(run)
				if err != nil {
					return err
				}
				return b.Put([]byte(dedupKey), data)
			}
		}
		seq, _ := b.NextSequence()
		run := CronRun{
			ID:              seq,
			CronID:          cronID,
			RunMinute:       runMinute,
			Status:          status,
			UserMsg:         userMsg,
			JobLogPath:      jobLogPath,
			RuntimeProvider: runtime.Provider,
			RuntimeMode:     runtime.Mode,
			RuntimeVersion:  runtime.Version,
		}
		data, err := json.Marshal(run)
		if err != nil {
			return err
		}
		return b.Put([]byte(dedupKey), data)
	})
}

func (s *Store) AllCrons() ([]CronJob, error) {
	var jobs []CronJob
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		return b.ForEach(func(k, v []byte) error {
			var job CronJob
			if err := json.Unmarshal(v, &job); err != nil {
				return nil
			}
			jobs = append(jobs, job)
			return nil
		})
	})
	return jobs, err
}

func (s *Store) GetCron(id uint64) (*CronJob, error) {
	var job CronJob
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		data := b.Get(itob(id))
		if data == nil {
			return fmt.Errorf("cron %d not found", id)
		}
		return json.Unmarshal(data, &job)
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Store) SetCronActive(id uint64, active bool) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		key := itob(id)
		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("cron %d not found", id)
		}
		var job CronJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		job.Active = active
		updated, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return b.Put(key, updated)
	})
}

func (s *Store) SetCronSilent(id uint64, silent bool) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		key := itob(id)
		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("cron %d not found", id)
		}
		var job CronJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		job.Silent = silent
		updated, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return b.Put(key, updated)
	})
}

func (s *Store) SetCronSchedule(id uint64, schedule string) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		key := itob(id)
		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("cron %d not found", id)
		}
		var job CronJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		job.Schedule = schedule
		updated, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return b.Put(key, updated)
	})
}

func (s *Store) SetCronTimezone(id uint64, timezone string) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		key := itob(id)
		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("cron %d not found", id)
		}
		var job CronJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		job.Timezone = timezone
		updated, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return b.Put(key, updated)
	})
}

func (s *Store) DeleteCron(id uint64) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cronsBucket)
		key := itob(id)
		if b.Get(key) == nil {
			return fmt.Errorf("cron %d not found", id)
		}
		return b.Delete(key)
	})
}

// SubagentRun tracks a running or completed subagent process.
type SubagentRun struct {
	ID              uint64 `json:"id"`
	PID             int    `json:"pid"`
	Source          string `json:"source"` // "cron", "cli", "gateway"
	CronID          uint64 `json:"cron_id,omitempty"`
	ChatID          string `json:"chat_id,omitempty"`
	Prompt          string `json:"prompt"`
	Status          string `json:"status"` // "running", "ok", "error"
	LogPath         string `json:"log_path"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at,omitempty"`
	RuntimeProvider string `json:"runtime_provider,omitempty"`
	RuntimeMode     string `json:"runtime_mode,omitempty"`
	RuntimeVersion  string `json:"runtime_version,omitempty"`
}

func (s *Store) RecordSubagentStart(pid int, source string, cronID uint64, chatID, prompt, logPath string, runtime ExecutionRuntime) (uint64, error) {
	var id uint64
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subagentRunsBucket)
		seq, _ := b.NextSequence()
		id = seq
		run := SubagentRun{
			ID:              id,
			PID:             pid,
			Source:          source,
			CronID:          cronID,
			ChatID:          chatID,
			Prompt:          prompt,
			Status:          "running",
			LogPath:         logPath,
			StartedAt:       time.Now().UTC().Format(time.RFC3339),
			RuntimeProvider: runtime.Provider,
			RuntimeMode:     runtime.Mode,
			RuntimeVersion:  runtime.Version,
		}
		data, err := json.Marshal(run)
		if err != nil {
			return err
		}
		return b.Put(itob(id), data)
	})
	return id, err
}

func (s *Store) RecordSubagentFinish(id uint64, status string) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subagentRunsBucket)
		key := itob(id)
		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("subagent run %d not found", id)
		}
		var run SubagentRun
		if err := json.Unmarshal(data, &run); err != nil {
			return err
		}
		run.Status = status
		run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		updated, err := json.Marshal(run)
		if err != nil {
			return err
		}
		return b.Put(key, updated)
	})
}

func (s *Store) RunningSubagents() ([]SubagentRun, error) {
	var runs []SubagentRun
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(subagentRunsBucket)
		return b.ForEach(func(k, v []byte) error {
			var run SubagentRun
			if err := json.Unmarshal(v, &run); err != nil {
				return nil
			}
			if run.Status == "running" {
				runs = append(runs, run)
			}
			return nil
		})
	})
	return runs, err
}

// CronJobRunning returns true if there is a subagent_run with the given
// cronID that is still in status "running".
func (s *Store) CronJobRunning(cronID uint64) bool {
	var running bool
	_ = s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(subagentRunsBucket)
		return b.ForEach(func(k, v []byte) error {
			var run SubagentRun
			if err := json.Unmarshal(v, &run); err != nil {
				return nil
			}
			if run.CronID == cronID && run.Status == "running" {
				running = true
			}
			return nil
		})
	})
	return running
}

// GetMeta returns a string value for the given key, or "" if not set.
func (s *Store) GetMeta(key string) string {
	var val string
	_ = s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(metaBucket)
		data := b.Get([]byte(key))
		if data != nil {
			val = string(data)
		}
		return nil
	})
	return val
}

// SetMeta stores a string value for the given key.
func (s *Store) SetMeta(key, value string) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(metaBucket)
		return b.Put([]byte(key), []byte(value))
	})
}

// Channel represents a configured messaging gateway (telegram or slack).
type Channel struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"` // "telegram" or "slack"
	Config    map[string]string `json:"config"`
	CreatedAt string            `json:"created_at"`
}

func (s *Store) AddChannel(ch Channel) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(channelsBucket)
		if b.Get([]byte(ch.Name)) != nil {
			return fmt.Errorf("channel %q already exists", ch.Name)
		}
		if ch.CreatedAt == "" {
			ch.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		data, err := json.Marshal(ch)
		if err != nil {
			return err
		}
		return b.Put([]byte(ch.Name), data)
	})
}

func (s *Store) GetChannel(name string) (*Channel, error) {
	var ch Channel
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(channelsBucket)
		data := b.Get([]byte(name))
		if data == nil {
			return fmt.Errorf("channel %q not found", name)
		}
		return json.Unmarshal(data, &ch)
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *Store) AllChannels() ([]Channel, error) {
	var channels []Channel
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(channelsBucket)
		return b.ForEach(func(k, v []byte) error {
			var ch Channel
			if err := json.Unmarshal(v, &ch); err != nil {
				return nil
			}
			channels = append(channels, ch)
			return nil
		})
	})
	return channels, err
}

func (s *Store) DeleteChannel(name string) error {
	return s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(channelsBucket)
		if b.Get([]byte(name)) == nil {
			return fmt.Errorf("channel %q not found", name)
		}
		return b.Delete([]byte(name))
	})
}

func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
