package msglog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const currentSessionSeqFile = ".current_session_seq"

// SessionFileManager tracks per-session JSONL files named YYYY-MM-DD-NNN.
type SessionFileManager struct {
	dir        string // logs/message_logs/sessions/
	tz         *time.Location
	currentSeq string // e.g. "2026-03-16-003"
	sessionID  string
}

// NewSessionFileManager creates a manager and scans for the highest existing
// sequence number for today's date.
func NewSessionFileManager(dir string, tz *time.Location) *SessionFileManager {
	m := &SessionFileManager{dir: dir, tz: tz}
	m.currentSeq = m.readCurrentSeq()
	return m
}

// NewSession increments the session counter and returns the new sequence.
func (m *SessionFileManager) NewSession(sessionID string) string {
	m.sessionID = sessionID

	today := time.Now().In(m.tz).Format("2006-01-02")
	highest := m.highestSeqForDate(today)
	next := highest + 1

	m.currentSeq = fmt.Sprintf("%s-%03d", today, next)
	m.writeCurrentSeq(m.currentSeq)
	return m.currentSeq
}

// CurrentPath returns the full JSONL path for the active session.
func (m *SessionFileManager) CurrentPath() string {
	if m.currentSeq == "" {
		return ""
	}
	return filepath.Join(m.dir, m.currentSeq+".jsonl")
}

// CurrentSeq returns the current session sequence string (e.g. "2026-03-16-003").
func (m *SessionFileManager) CurrentSeq() string {
	return m.currentSeq
}

// SessionID returns the current runtime session/thread ID.
func (m *SessionFileManager) SessionID() string {
	return m.sessionID
}

// highestSeqForDate scans the sessions dir and finds the highest NNN for the given date.
func (m *SessionFileManager) highestSeqForDate(date string) int {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0
	}

	highest := 0
	prefix := date + "-"
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Extract NNN from "YYYY-MM-DD-NNN.jsonl"
		seqStr := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".jsonl")
		var n int
		if _, err := fmt.Sscanf(seqStr, "%d", &n); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

// readCurrentSeq reads the persisted current session seq from the marker file.
func (m *SessionFileManager) readCurrentSeq() string {
	data, err := os.ReadFile(filepath.Join(m.dir, currentSessionSeqFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeCurrentSeq persists the current session seq so the CLI can read it.
func (m *SessionFileManager) writeCurrentSeq(seq string) {
	_ = os.MkdirAll(m.dir, 0o755)
	_ = os.WriteFile(filepath.Join(m.dir, currentSessionSeqFile), []byte(seq+"\n"), 0o644)
}

// ListSessionFiles returns all session JSONL files sorted by name (chronological).
func ListSessionFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files
}
