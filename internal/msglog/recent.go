package msglog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"goated/internal/memory"
)

// RecentHistory returns the last four prior user messages and last four
// assistant responses for a chat. Daily logs are the durable source, so this
// works across daemon restarts; missing/expired logs simply yield less history.
func (l *Logger) RecentHistory(chatID, excludeRequestID string) memory.History {
	var history memory.History
	if l == nil {
		return history
	}
	paths, _ := filepath.Glob(filepath.Join(l.DailyDir(), "*.jsonl"))
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	var entries []LogEntry
	for _, path := range paths { // bound scanning by the existing 7-day retention window
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		var day []LogEntry
		for scanner.Scan() {
			var e LogEntry
			if json.Unmarshal(scanner.Bytes(), &e) != nil || e.RequestID == excludeRequestID {
				continue
			}
			if e.UserMessage != nil && e.UserMessage.ChatID == chatID && e.Type == EntryUserMessage {
				day = append(day, e)
			}
			if e.AgentResponse != nil && e.AgentResponse.ChatID == chatID && e.Type == EntryAgentResponse {
				day = append(day, e)
			}
		}
		_ = f.Close()
		for i := len(day) - 1; i >= 0; i-- {
			entries = append(entries, day[i])
		}
		if len(entries) >= 40 {
			break
		}
	}
	var older []string
	for _, e := range entries {
		if e.UserMessage != nil {
			t := clip(e.UserMessage.Text, 600)
			if len(history.Users) < 4 {
				history.Users = append(history.Users, t)
			} else if len(older) < 8 {
				older = append(older, "User: "+clip(t, 160))
			}
		}
		if e.AgentResponse != nil {
			t := clip(e.AgentResponse.Text, 400)
			if len(history.Assistants) < 4 {
				history.Assistants = append(history.Assistants, t)
			} else if len(older) < 8 {
				older = append(older, "Assistant: "+clip(t, 160))
			}
		}
	}
	reverse(history.Users)
	reverse(history.Assistants)
	reverse(older)
	// A bounded extractive summary, deliberately not presented as semantic truth.
	history.Summary = strings.Join(older, " | ")
	return history
}
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
