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

func TestFileSearcherSkipsSymlinks(t *testing.T) {
	for _, location := range []string{"file", "root", "self"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			secretDir := t.TempDir()
			secret := filepath.Join(secretDir, "secret.md")
			if err := os.WriteFile(secret, []byte("Surabaya private credential"), 0600); err != nil {
				t.Fatal(err)
			}
			var link, target string
			switch location {
			case "file":
				link, target = filepath.Join(root, "self", "vault", "note.md"), secret
			case "root":
				link, target = filepath.Join(root, "self", "vault"), secretDir
			case "self":
				link, target = filepath.Join(root, "self"), secretDir
			}
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			got, err := (FileSearcher{Workspace: root}).Search(context.Background(), "Surabaya")
			if err != nil || len(got) != 0 {
				t.Fatalf("got=%#v err=%v", got, err)
			}
		})
	}
}

func TestFileSearchRootsUniqueOnBothFilesystems(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"vault", "VAULT", "missions", "MISSIONS"} {
		if err := os.MkdirAll(filepath.Join(root, "self", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	roots := fileSearchRoots(root)
	for i, path := range roots {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, previous := range roots[:i] {
			prior, err := os.Stat(previous)
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(info, prior) {
				t.Fatalf("duplicate roots: %s and %s", previous, path)
			}
		}
	}
	// Distinct case variants on Linux must remain searchable.
	lower, _ := os.Stat(filepath.Join(root, "self", "vault"))
	upper, _ := os.Stat(filepath.Join(root, "self", "VAULT"))
	want := 4
	if os.SameFile(lower, upper) {
		want = 2
	}
	if len(roots) != want {
		t.Fatalf("got %d roots, want %d", len(roots), want)
	}
}
