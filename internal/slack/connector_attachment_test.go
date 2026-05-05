package slack

import (
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func TestIsAllowedByMetadata(t *testing.T) {
	if !isAllowedByMetadata("photo.png", "image/png") {
		t.Fatal("expected png image to be allowed")
	}
	if !isAllowedByMetadata("sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet") {
		t.Fatal("expected xlsx to be allowed")
	}
	if !isAllowedByMetadata("archive.zip", "application/zip") {
		t.Fatal("expected zip to be allowed")
	}
}

func TestIsAllowedByContent(t *testing.T) {
	if !isAllowedByContent("report.pdf", "application/pdf", "application/pdf") {
		t.Fatal("expected pdf content to be allowed")
	}
	if !isAllowedByContent("sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/zip") {
		t.Fatal("expected zip-sniffed xlsx to be allowed")
	}
	if isAllowedByContent("doc.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/pdf") {
		t.Fatal("expected mismatched docx content to be rejected")
	}
}

func TestMessageDedupKey_UsesEventID(t *testing.T) {
	c := &Connector{}
	ev := &slackevents.MessageEvent{Channel: "C1", TimeStamp: "111.222"}
	k := c.messageDedupKey("T1", "Ev123", ev, nil)
	if k != "Ev123" {
		t.Fatalf("got %q, want EventID", k)
	}
}

func TestMessageDedupKey_FallbackComposite(t *testing.T) {
	c := &Connector{}
	ev := &slackevents.MessageEvent{Channel: "C1", TimeStamp: "111.222"}
	files := []slack.File{{ID: "F2"}, {ID: "F1"}}
	k := c.messageDedupKey("T1", "", ev, files)
	if k != "T1|C1|111.222|F1,F2" {
		t.Fatalf("unexpected fallback key %q", k)
	}
}

func TestIsSubPath(t *testing.T) {
	if !isSubPath("/tmp/root", "/tmp/root/file") {
		t.Fatal("expected file under root")
	}
	if isSubPath("/tmp/root", "/tmp/root2/file") {
		t.Fatal("expected path outside root to be rejected")
	}
}
