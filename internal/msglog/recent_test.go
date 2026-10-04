package msglog

import (
	"path/filepath"
	"testing"
)

func TestRecentHistoryFourPriorTurns(t *testing.T) {
	root := t.TempDir()
	l, err := NewLogger(root, filepath.Join(root, "workspace"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		l.LogUserMessage(id, UserMessageData{ChatID: "chat", Text: "user" + id}, StatusPending)
		l.LogAgentResponse(id, AgentResponseData{ChatID: "chat", Text: "answer" + id}, StatusPending, "")
	}
	l.LogUserMessage("current", UserMessageData{ChatID: "chat", Text: "current"}, StatusPending)
	got := l.RecentHistory("chat", "current")
	if len(got.Users) != 4 || got.Users[0] != "userc" || got.Users[3] != "userf" {
		t.Fatalf("users %#v", got.Users)
	}
	if len(got.Assistants) != 4 || got.Assistants[0] != "answerc" || got.Assistants[3] != "answerf" {
		t.Fatalf("assistants %#v", got.Assistants)
	}
	if got.Summary == "" {
		t.Fatal("missing older extractive summary")
	}
}
