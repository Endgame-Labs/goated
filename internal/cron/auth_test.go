package cron

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"goated/internal/agent"
	"goated/internal/authnotice"
	"goated/internal/db"
)

// fakeHeadless returns a canned result for every AI job.
type fakeHeadless struct {
	agent.HeadlessRuntime
	output string
	err    error
}

func (f *fakeHeadless) Descriptor() agent.RuntimeDescriptor { return agent.RuntimeDescriptor{} }
func (f *fakeHeadless) Version(context.Context) string      { return "" }
func (f *fakeHeadless) RunSync(context.Context, *db.Store, agent.HeadlessRequest) (agent.HeadlessResult, error) {
	return agent.HeadlessResult{Output: []byte(f.output)}, f.err
}

type chatLog struct {
	mu   sync.Mutex
	msgs map[string][]string
}

func (c *chatLog) SendMessage(_ context.Context, chatID, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.msgs == nil {
		c.msgs = map[string][]string{}
	}
	c.msgs[chatID] = append(c.msgs[chatID], text)
	return nil
}

func (c *chatLog) to(chatID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.msgs[chatID]
}

func addAIJob(t *testing.T, store *db.Store) db.CronJob {
	t.Helper()
	id, err := store.AddCronWithNotifications("subagent", "user", "* * * * *", "summarize the news", "", "", "UTC", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.GetCron(id)
	if err != nil {
		t.Fatal(err)
	}
	return *job
}

func TestAuthExpiredJobsAreRecordedAndReportedOnce(t *testing.T) {
	r, store := testRunner(t)
	chat := &chatLog{}
	r.Notifier = chat
	r.Auth = &authnotice.Notifier{Send: chat.SendMessage, AdminChatID: "admin", SessionName: "goat_claude_main"}
	r.Headless = &fakeHeadless{
		output: `Please run /login · API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."},"request_id":"req_123"}`,
		err:    errors.New("exit status 1"),
	}
	job := addAIJob(t, store)
	ctx := context.Background()
	minute := time.Date(2026, 4, 6, 17, 0, 0, 0, time.UTC)

	for i := 0; i < 2; i++ {
		rec, err := r.runOne(ctx, minute.Add(time.Duration(i)*time.Minute), job)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != "auth_expired" {
			t.Fatalf("status = %q, want auth_expired", rec.Status)
		}
	}

	if admin := chat.to("admin"); len(admin) != 1 {
		t.Fatalf("admin notices = %q, want exactly 1", admin)
	}
	user := chat.to("user")
	if len(user) != 2 {
		t.Fatalf("user notices = %q, want one per run", user)
	}
	for _, msg := range append(user, chat.to("admin")...) {
		for _, raw := range []string{"401", "request_id", "req_123", "authentication_error"} {
			if strings.Contains(msg, raw) {
				t.Errorf("notice %q leaks raw provider text %q", msg, raw)
			}
		}
	}

	// The next successful AI job means the login works again.
	r.Headless = &fakeHeadless{output: "done"}
	if rec, err := r.runOne(ctx, minute.Add(10*time.Minute), job); err != nil || rec.Status != "ok" {
		t.Fatalf("runOne = %+v, %v", rec, err)
	}
	admin := chat.to("admin")
	if len(admin) != 2 || !strings.Contains(admin[1], "works again") {
		t.Fatalf("admin notices = %q, want a restored notice", admin)
	}
}

func TestOrdinaryJobFailureIsNotAuthExpired(t *testing.T) {
	r, store := testRunner(t)
	chat := &chatLog{}
	r.Notifier = chat
	r.Auth = &authnotice.Notifier{Send: chat.SendMessage, AdminChatID: "admin"}
	r.Headless = &fakeHeadless{output: "API Error: 500 Internal server error", err: errors.New("exit status 1")}
	job := addAIJob(t, store)

	rec, err := r.runOne(context.Background(), time.Now().Truncate(time.Minute), job)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "error" {
		t.Fatalf("status = %q, want error", rec.Status)
	}
	if admin := chat.to("admin"); len(admin) != 0 {
		t.Fatalf("admin notices = %q, want none", admin)
	}
}
