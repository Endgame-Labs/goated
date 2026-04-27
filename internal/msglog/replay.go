package msglog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"goated/internal/agent"
)

// StuckMessage represents a user message that was sent to the agent but
// never received an agent_received confirmation.
type StuckMessage struct {
	RequestID string
	Entry     LogEntry
}

// FilterRecentStuckMessages keeps only stuck messages newer than maxAge.
// Older entries are returned separately so callers can log or inspect them.
func FilterRecentStuckMessages(stuck []StuckMessage, now time.Time, maxAge time.Duration) (recent []StuckMessage, stale []StuckMessage) {
	if maxAge <= 0 {
		return stuck, nil
	}

	for _, sm := range stuck {
		age := now.Sub(time.Unix(sm.Entry.TSUnix, 0))
		if age > maxAge {
			stale = append(stale, sm)
			continue
		}
		recent = append(recent, sm)
	}

	return recent, stale
}

// FindStuckMessages scans today's (and yesterday's) daily logs for messages
// stuck in sent_to_agent status.
func FindStuckMessages(logger *Logger) ([]StuckMessage, error) {
	now := time.Now().In(logger.tz)
	today := now.Format("2006-01-02")
	yesterday := now.Add(-24 * time.Hour).Format("2006-01-02")

	// Build request_id → latest status map
	statusMap := make(map[string]MessageStatus)
	entryMap := make(map[string]LogEntry) // request_id → original user_message entry

	for _, date := range []string{yesterday, today} {
		path := fmt.Sprintf("%s/daily/%s.jsonl", logger.baseDir, date)
		entries, err := readEntries(path)
		if err != nil {
			continue // file may not exist
		}
		for _, e := range entries {
			if e.RequestID == "" {
				continue
			}
			if e.Status != "" {
				statusMap[e.RequestID] = e.Status
			}
			if e.Type == EntryUserMessage && e.UserMessage != nil {
				entryMap[e.RequestID] = e
			}
		}
	}

	// Collect entries where latest status is sent_to_agent
	var stuck []StuckMessage
	for reqID, status := range statusMap {
		if status != StatusSentToAgent {
			continue
		}
		if entry, ok := entryMap[reqID]; ok {
			stuck = append(stuck, StuckMessage{RequestID: reqID, Entry: entry})
		}
	}

	// Sort by timestamp (oldest first)
	sort.Slice(stuck, func(i, j int) bool {
		return stuck[i].Entry.TSUnix < stuck[j].Entry.TSUnix
	})

	return stuck, nil
}

// ReplayStuckMessages replays stuck messages to the agent session with
// exponential backoff on failures.
func ReplayStuckMessages(ctx context.Context, logger *Logger, session agent.SessionRuntime, stuck []StuckMessage) {
	for _, sm := range stuck {
		select {
		case <-ctx.Done():
			return
		default:
		}

		logger.LogEvent(sm.RequestID, EventData{
			Name:   "replay_attempt",
			Detail: fmt.Sprintf("replaying stuck message from %s", sm.Entry.TS),
		})

		msg := sm.Entry.UserMessage
		if msg == nil {
			continue
		}

		backoff := time.Second
		const maxBackoff = 30 * time.Second
		const maxAttempts = 5

		replayCtx := WithRequestID(ctx, sm.RequestID)
		for attempt := 0; attempt < maxAttempts; attempt++ {
			err := session.SendUserPrompt(replayCtx, msg.Channel, msg.ChatID, msg.Text, nil, msg.MessageID, msg.ThreadID, nil)
			if err != nil {
				logger.LogEvent(sm.RequestID, EventData{
					Name:   "replay_send_failed",
					Detail: err.Error(),
				})
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			state, err := session.WaitForAwaitingInput(ctx, 5*time.Minute)
			if err != nil {
				logger.LogEvent(sm.RequestID, EventData{
					Name:   "replay_wait_timeout",
					Detail: err.Error(),
				})
				break // move to next message
			}

			if state.SafeIdle() {
				logger.UpdateStatus(sm.RequestID, EntryUserMessage, StatusAgentReceived)
				fmt.Fprintf(os.Stderr, "[%s] replayed stuck message %s successfully\n",
					time.Now().Format(time.RFC3339), sm.RequestID)
				break
			}

			logger.LogEvent(sm.RequestID, EventData{
				Name:   "replay_unexpected_state",
				Detail: state.Summary,
			})
			break
		}
	}
}

// readEntries reads all JSONL entries from a file.
func readEntries(path string) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	// Allow longer lines (default 64KB may be too small for large messages)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
