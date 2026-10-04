package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSearcherDefaultAndSecretExclusion(t *testing.T) {
	root := t.TempDir()
	must := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(root, "self", "vault", "project.md"), "The Surabaya conference presentation is Friday.")
	must(filepath.Join(root, "creds", "TYPESAFE_API_KEY.txt"), "Surabaya secret key")
	must(filepath.Join(root, "self", "state", "secret.md"), "Surabaya secret state")
	got, err := (FileSearcher{Workspace: root}).Search(context.Background(), "Surabaya presentation")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Source != "self/vault/project.md" || strings.Contains(got[0].Text, "secret") {
		t.Fatalf("unexpected results %#v", got)
	}
}
func TestFileSearcherFallsBackToWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("Goated memory retrieval"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := (FileSearcher{Workspace: root}).Search(context.Background(), "memory retrieval")
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
func TestWordIndexDoesNotMatchInsideOtherWord(t *testing.T) {
	if wordIndex("unconference plan", "conference") >= 0 {
		t.Fatal("matched suffix inside a different word")
	}
	if wordIndex("conference plan", "conference") != 0 {
		t.Fatal("missed whole word")
	}
}
