package db

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// --- Cron CRUD ---

func TestCronAddAndGet(t *testing.T) {
	s := newTestStore(t)

	id, err := s.AddCron("subagent", "chat1", "* * * * *", "do something", "", "", "UTC", false)
	if err != nil {
		t.Fatalf("AddCron: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	job, err := s.GetCron(id)
	if err != nil {
		t.Fatalf("GetCron: %v", err)
	}
	if job.ChatID != "chat1" {
		t.Errorf("ChatID = %q, want chat1", job.ChatID)
	}
	if job.Schedule != "* * * * *" {
		t.Errorf("Schedule = %q", job.Schedule)
	}
	if job.Prompt != "do something" {
		t.Errorf("Prompt = %q", job.Prompt)
	}
	if !job.Active {
		t.Error("expected Active=true")
	}
	if job.Type != "subagent" {
		t.Errorf("Type = %q, want subagent", job.Type)
	}
}

func TestCronDefaultType(t *testing.T) {
	s := newTestStore(t)
	id, err := s.AddCron("", "chat1", "* * * * *", "test", "", "", "UTC", false)
	if err != nil {
		t.Fatalf("AddCron: %v", err)
	}
	job, _ := s.GetCron(id)
	if job.Type != "subagent" {
		t.Errorf("default type = %q, want subagent", job.Type)
	}
}

func TestCronSystemRequiresCommand(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddCron("system", "chat1", "* * * * *", "", "", "", "UTC", false)
	if err == nil {
		t.Fatal("expected error for system cron without command")
	}
}

func TestCronSubagentRequiresPrompt(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddCron("subagent", "chat1", "* * * * *", "", "", "", "UTC", false)
	if err == nil {
		t.Fatal("expected error for subagent cron without prompt")
	}
}

func TestCronUnknownType(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddCron("invalid", "chat1", "* * * * *", "test", "", "", "UTC", false)
	if err == nil {
		t.Fatal("expected error for unknown cron type")
	}
}

func TestCronList(t *testing.T) {
	s := newTestStore(t)

	s.AddCron("subagent", "c1", "* * * * *", "p1", "", "", "UTC", false)
	s.AddCron("subagent", "c2", "0 * * * *", "p2", "", "", "UTC", false)

	all, err := s.AllCrons()
	if err != nil {
		t.Fatalf("AllCrons: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}

	active, err := s.ActiveCrons()
	if err != nil {
		t.Fatalf("ActiveCrons: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active len = %d, want 2", len(active))
	}
}

func TestCronSetActive(t *testing.T) {
	s := newTestStore(t)

	id, _ := s.AddCron("subagent", "c1", "* * * * *", "p1", "", "", "UTC", false)
	if err := s.SetCronActive(id, false); err != nil {
		t.Fatalf("SetCronActive: %v", err)
	}

	active, _ := s.ActiveCrons()
	if len(active) != 0 {
		t.Fatalf("expected 0 active crons, got %d", len(active))
	}

	job, _ := s.GetCron(id)
	if job.Active {
		t.Error("expected Active=false after deactivation")
	}
}

func TestCronSetSilent(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.AddCron("subagent", "c1", "* * * * *", "p1", "", "", "UTC", false)

	if err := s.SetCronSilent(id, true); err != nil {
		t.Fatalf("SetCronSilent: %v", err)
	}
	job, _ := s.GetCron(id)
	if !job.Silent {
		t.Error("expected Silent=true")
	}
}

func TestCronSetSchedule(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.AddCron("subagent", "c1", "* * * * *", "p1", "", "", "UTC", false)

	if err := s.SetCronSchedule(id, "0 9 * * *"); err != nil {
		t.Fatalf("SetCronSchedule: %v", err)
	}
	job, _ := s.GetCron(id)
	if job.Schedule != "0 9 * * *" {
		t.Errorf("Schedule = %q, want 0 9 * * *", job.Schedule)
	}
}

func TestCronSetTimezone(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.AddCron("subagent", "c1", "* * * * *", "p1", "", "", "UTC", false)

	if err := s.SetCronTimezone(id, "America/New_York"); err != nil {
		t.Fatalf("SetCronTimezone: %v", err)
	}
	job, _ := s.GetCron(id)
	if job.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q", job.Timezone)
	}
}

func TestCronDelete(t *testing.T) {
	s := newTestStore(t)

	id, _ := s.AddCron("subagent", "c1", "* * * * *", "p1", "", "", "UTC", false)
	if err := s.DeleteCron(id); err != nil {
		t.Fatalf("DeleteCron: %v", err)
	}

	_, err := s.GetCron(id)
	if err == nil {
		t.Fatal("expected error getting deleted cron")
	}

	all, _ := s.AllCrons()
	if len(all) != 0 {
		t.Fatalf("expected 0 crons after delete, got %d", len(all))
	}
}

func TestCronDeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteCron(99999)
	if err == nil {
		t.Fatal("expected error deleting non-existent cron")
	}
}

func TestGetCronNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetCron(99999)
	if err == nil {
		t.Fatal("expected error for non-existent cron")
	}
}

// --- Subagent Runs ---

func TestSubagentRunLifecycle(t *testing.T) {
	s := newTestStore(t)

	id, err := s.RecordSubagentStart(1234, "cron", 1, "chat1", "do work", "/tmp/log")
	if err != nil {
		t.Fatalf("RecordSubagentStart: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero run ID")
	}

	running, err := s.RunningSubagents()
	if err != nil {
		t.Fatalf("RunningSubagents: %v", err)
	}
	if len(running) != 1 {
		t.Fatalf("expected 1 running, got %d", len(running))
	}
	if running[0].PID != 1234 {
		t.Errorf("PID = %d, want 1234", running[0].PID)
	}
	if running[0].Status != "running" {
		t.Errorf("Status = %q, want running", running[0].Status)
	}

	if err := s.RecordSubagentFinish(id, "ok"); err != nil {
		t.Fatalf("RecordSubagentFinish: %v", err)
	}

	running, _ = s.RunningSubagents()
	if len(running) != 0 {
		t.Fatalf("expected 0 running after finish, got %d", len(running))
	}
}

func TestCronJobRunning(t *testing.T) {
	s := newTestStore(t)

	if s.CronJobRunning(1) {
		t.Fatal("expected false for no runs")
	}

	id, _ := s.RecordSubagentStart(100, "cron", 1, "chat1", "test", "/tmp/log")
	if !s.CronJobRunning(1) {
		t.Fatal("expected true while running")
	}

	s.RecordSubagentFinish(id, "ok")
	if s.CronJobRunning(1) {
		t.Fatal("expected false after finish")
	}
}

// --- Cron Run Dedup ---

func TestRecordCronRunDedup(t *testing.T) {
	s := newTestStore(t)

	// First record
	err := s.RecordCronRun(1, "2025-01-01T00:00", "started", "", "")
	if err != nil {
		t.Fatalf("first RecordCronRun: %v", err)
	}

	// Same cron+minute should update, not create duplicate
	err = s.RecordCronRun(1, "2025-01-01T00:00", "ok", "done", "/log")
	if err != nil {
		t.Fatalf("second RecordCronRun: %v", err)
	}

	// Different minute should be a separate entry
	err = s.RecordCronRun(1, "2025-01-01T00:01", "started", "", "")
	if err != nil {
		t.Fatalf("third RecordCronRun: %v", err)
	}
}

// --- Meta ---

func TestMetaGetSet(t *testing.T) {
	s := newTestStore(t)

	// Get non-existent key returns empty
	if got := s.GetMeta("foo"); got != "" {
		t.Errorf("GetMeta(foo) = %q, want empty", got)
	}

	if err := s.SetMeta("foo", "bar"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if got := s.GetMeta("foo"); got != "bar" {
		t.Errorf("GetMeta(foo) = %q, want bar", got)
	}

	// Overwrite
	if err := s.SetMeta("foo", "baz"); err != nil {
		t.Fatalf("SetMeta overwrite: %v", err)
	}
	if got := s.GetMeta("foo"); got != "baz" {
		t.Errorf("GetMeta(foo) = %q, want baz", got)
	}
}

// --- Channels ---

func TestChannelCRUD(t *testing.T) {
	s := newTestStore(t)

	ch := Channel{
		Name:   "my-slack",
		Type:   "slack",
		Config: map[string]string{"token": "xoxb-123"},
	}

	if err := s.AddChannel(ch); err != nil {
		t.Fatalf("AddChannel: %v", err)
	}

	got, err := s.GetChannel("my-slack")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.Type != "slack" {
		t.Errorf("Type = %q, want slack", got.Type)
	}
	if got.Config["token"] != "xoxb-123" {
		t.Errorf("Config token = %q", got.Config["token"])
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt should be set automatically")
	}

	// List
	all, err := s.AllChannels()
	if err != nil {
		t.Fatalf("AllChannels: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}

	// Delete
	if err := s.DeleteChannel("my-slack"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	_, err = s.GetChannel("my-slack")
	if err == nil {
		t.Fatal("expected error getting deleted channel")
	}
}

func TestChannelDuplicateName(t *testing.T) {
	s := newTestStore(t)

	ch := Channel{Name: "dup", Type: "slack", Config: map[string]string{}}
	s.AddChannel(ch)

	err := s.AddChannel(ch)
	if err == nil {
		t.Fatal("expected error adding duplicate channel name")
	}
}

func TestChannelGetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetChannel("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent channel")
	}
}

func TestChannelDeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteChannel("nonexistent")
	if err == nil {
		t.Fatal("expected error deleting non-existent channel")
	}
}

// --- Store Open/Close ---

func TestOpenCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "test.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store.Close()

	if _, err := os.Stat(filepath.Join(dir, "sub", "dir")); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}
