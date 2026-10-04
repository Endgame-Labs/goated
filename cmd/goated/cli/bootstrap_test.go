package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"goated/internal/db"
)

func TestDefaultSelfCronsDreaming(t *testing.T) {
	for _, scenario := range []string{"fresh", "legacy", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			workspace := t.TempDir()
			store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			write := func(rel string) string {
				t.Helper()
				path := filepath.Join(workspace, "self", rel)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("custom private instructions"), 0600); err != nil {
					t.Fatal(err)
				}
				return path
			}
			write("HEARTBEAT.md")
			var legacyID uint64
			var before *db.CronJob
			if scenario != "missing" {
				write("prompts/dreaming.md")
			}
			if scenario == "legacy" {
				path := write("prompts/knowledge_extraction.md")
				legacyID, err = store.AddCronWithNotifications("subagent", "original-chat", "0 3 * * *", "", path, "", "America/Los_Angeles", "", true, false)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.SetCronActive(legacyID, false); err != nil {
					t.Fatal(err)
				}
				before, err = store.GetCron(legacyID)
				if err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				if err := ensureDefaultSelfCrons(store, workspace, "UTC"); err != nil {
					t.Fatal(err)
				}
			}
			jobs, err := store.AllCrons()
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if scenario == "missing" {
				want = 1
			}
			if len(jobs) != want {
				t.Fatalf("got %d jobs, want %d", len(jobs), want)
			}
			if scenario == "legacy" {
				after, err := store.GetCron(legacyID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("legacy job changed: before=%+v after=%+v", before, after)
				}
				data, err := os.ReadFile(after.PromptFile)
				if err != nil || string(data) != "custom private instructions" {
					t.Fatalf("private prompt changed: %q, %v", data, err)
				}
			}
			if scenario == "fresh" {
				found := false
				for _, job := range jobs {
					if filepath.Base(job.PromptFile) != "dreaming.md" {
						continue
					}
					found = true
					if job.Schedule != "0 */8 * * *" || job.Timezone != "UTC" || job.EffectiveNotifyUser() || !job.EffectiveNotifyMainSession() {
						t.Fatalf("incorrect dreaming defaults: %+v", job)
					}
				}
				if !found {
					t.Fatal("dreaming job missing")
				}
			}
		})
	}
}
