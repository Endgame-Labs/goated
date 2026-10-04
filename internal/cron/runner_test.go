package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goated/internal/agent"
	"goated/internal/db"
)

func testRunner(t *testing.T) (*Runner, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &Runner{Store: store, WorkspaceDir: dir, LogDir: dir}, store
}

type blockingVersionRuntime struct {
	agent.HeadlessRuntime
	started chan context.Context
	release chan struct{}
}

func (r *blockingVersionRuntime) Descriptor() agent.RuntimeDescriptor {
	return agent.RuntimeDescriptor{}
}

func (r *blockingVersionRuntime) Version(ctx context.Context) string {
	r.started <- ctx
	select {
	case <-ctx.Done():
	case <-r.release:
	}
	return ""
}

func TestCancellationDrainsRuntimeVersionProbe(t *testing.T) {
	r, store := testRunner(t)
	runtime := &blockingVersionRuntime{started: make(chan context.Context, 1), release: make(chan struct{})}
	r.Headless = runtime
	if _, err := store.AddCronWithNotifications("system", "", "* * * * *", "", "", "true", "UTC", "", false, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); close(runtime.release); r.Wait() }()
	if err := r.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var probeCtx context.Context
	select {
	case probeCtx = <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("version probe never started")
	}
	deadline, ok := probeCtx.Deadline()
	if !ok || time.Until(deadline) > runtimeVersionTimeout {
		t.Error("version probe must have a bounded deadline")
	}
	cancel()
	done := make(chan struct{})
	go func() { r.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime version probe blocked shutdown after cancellation")
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestRunDispatchesOtherJobsAndMinutesWhileOneRuns(t *testing.T) {
	r, store := testRunner(t)
	gate := filepath.Join(r.WorkspaceDir, "gate")
	started := filepath.Join(r.WorkspaceDir, "started")
	fast := filepath.Join(r.WorkspaceDir, "fast")
	command := fmt.Sprintf("echo started >> %q; while [ ! -f %q ]; do sleep 0.01; done", started, gate)
	if _, err := store.AddCronWithNotifications("system", "", "* * * * *", "", "", command, "UTC", "", false, false); err != nil {
		t.Fatal(err)
	}
	fastID, err := store.AddCronWithNotifications("system", "", "* * * * *", "", "", fmt.Sprintf("echo done > %q", fast), "UTC", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { os.WriteFile(gate, nil, 0o600); r.Wait() }()
	minute := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	returned := make(chan error, 1)
	go func() { returned <- r.Run(ctx, minute) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run blocked on a long job")
	}
	waitForFile(t, started)
	waitForFile(t, fast)
	// The output file is written before run records and worker cleanup.
	// Advance the simulated clock only once the fast worker has fully exited.
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		running := r.running[fastID]
		r.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fast worker did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := r.Run(ctx, minute.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The slow system job must not overlap itself, even on the next tick.
	data, err := os.ReadFile(started)
	if err != nil || strings.Count(string(data), "started") != 1 {
		t.Fatalf("slow job overlapped: %q, %v", data, err)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.Wait()
	runs, err := os.ReadFile(filepath.Join(r.LogDir, "cron", "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(runs)), "\n") {
		var rec runRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.Status != "ok" {
			t.Fatalf("bad run record: %q: %v", line, err)
		}
	}
	if got := len(strings.Split(strings.TrimSpace(string(runs)), "\n")); got != 3 {
		t.Fatalf("got %d records, want slow once and fast twice", got)
	}
}

func TestRunBoundsConcurrentJobs(t *testing.T) {
	r, store := testRunner(t)
	gate := filepath.Join(r.WorkspaceDir, "gate")
	command := fmt.Sprintf("while [ ! -f %q ]; do sleep 0.01; done", gate)
	for i := 0; i < maxConcurrentPerType+1; i++ {
		if _, err := store.AddCronWithNotifications("system", "", "* * * * *", "", "", command, "UTC", "", false, false); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { os.WriteFile(gate, nil, 0o600); r.Wait() }()
	if err := r.Run(context.Background(), time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	count := len(r.running)
	r.mu.Unlock()
	if count != maxConcurrentPerType+1 {
		t.Fatalf("scheduled = %d, want %d (the extra job must queue)", count, maxConcurrentPerType+1)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(r.systemSlots) != maxConcurrentPerType && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(r.systemSlots); got != maxConcurrentPerType {
		t.Fatalf("active system jobs = %d, want %d", got, maxConcurrentPerType)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.Wait()
	runs, err := os.ReadFile(filepath.Join(r.LogDir, "cron", "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimSpace(string(runs)), "\n")); got != maxConcurrentPerType+1 {
		t.Fatalf("completed jobs = %d, want %d", got, maxConcurrentPerType+1)
	}
}

func TestWaitDrainsCanceledJob(t *testing.T) {
	r, store := testRunner(t)
	started := filepath.Join(r.WorkspaceDir, "started")
	command := fmt.Sprintf("echo started > %q; exec sleep 30", started)
	if _, err := store.AddCronWithNotifications("system", "", "* * * * *", "", "", command, "UTC", "", false, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Run(ctx, time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, started)
	cancel()
	done := make(chan struct{})
	go func() { r.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not drain canceled job")
	}
	runs, err := os.ReadFile(filepath.Join(r.LogDir, "cron", "runs.jsonl"))
	if err != nil || !strings.Contains(string(runs), `"status":"error"`) {
		t.Fatalf("missing canceled run record: %q, %v", runs, err)
	}
}

func TestSystemJobRunsWhileAgentSlotsAreFull(t *testing.T) {
	r, store := testRunner(t)
	r.systemSlots = make(chan struct{}, maxConcurrentPerType)
	r.subagentSlots = make(chan struct{}, maxConcurrentPerType)
	for i := 0; i < maxConcurrentPerType; i++ {
		r.subagentSlots <- struct{}{}
	}
	result := filepath.Join(r.WorkspaceDir, "system-ran")
	if _, err := store.AddCronWithNotifications("system", "", "* * * * *", "", "",
		fmt.Sprintf("echo yes > %q", result), "UTC", "", false, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, result)
	r.Wait()
}
