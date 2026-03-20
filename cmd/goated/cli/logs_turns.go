package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"goated/internal/app"
	"goated/internal/msglog"
)

type conversationMessage struct {
	TS     string
	TSUnix int64
	Speaker string
	ChatID string
	Text   string
}

type conversationTurn struct {
	User      *conversationMessage
	Assistant []conversationMessage
}

var logsTurnsCmd = &cobra.Command{
	Use:   "turns",
	Short: "Render the last N user/assistant turns from gateway message logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := app.LoadConfig()
		turnCount, _ := cmd.Flags().GetInt("turns")
		chatID, _ := cmd.Flags().GetString("chat")

		loc, err := time.LoadLocation(cfg.DefaultTimezone)
		if err != nil {
			loc = time.Local
		}

		msgs, err := readConversationMessages(filepath.Join(cfg.LogDir, "message_logs", "daily"), chatID)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			fmt.Println("No conversation messages found.")
			return nil
		}

		turns := buildConversationTurns(msgs)
		if len(turns) == 0 {
			fmt.Println("No turns found.")
			return nil
		}
		if turnCount > len(turns) {
			turnCount = len(turns)
		}
		turns = turns[len(turns)-turnCount:]

		for i, turn := range turns {
			fmt.Printf("## Turn %d\n\n", len(turns)-turnCount+i+1)
			if turn.User != nil {
				renderConversationMessage(*turn.User, loc)
			}
			for _, msg := range turn.Assistant {
				renderConversationMessage(msg, loc)
			}
			if i < len(turns)-1 {
				fmt.Println("---")
				fmt.Println()
			}
		}

		return nil
	},
}

func readConversationMessages(dailyDir, chatID string) ([]conversationMessage, error) {
	entries, err := os.ReadDir(dailyDir)
	if err != nil {
		return nil, fmt.Errorf("read daily logs: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var msgs []conversationMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dailyDir, entry.Name())
		fileMsgs, err := readConversationMessagesFromFile(path, chatID)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, fileMsgs...)
	}

	sort.SliceStable(msgs, func(i, j int) bool {
		if msgs[i].TSUnix == msgs[j].TSUnix {
			return msgs[i].TS < msgs[j].TS
		}
		return msgs[i].TSUnix < msgs[j].TSUnix
	})

	return msgs, nil
}

func readConversationMessagesFromFile(path, chatID string) ([]conversationMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var msgs []conversationMessage
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	for scanner.Scan() {
		var entry msglog.LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case msglog.EntryUserMessage:
			if entry.Status != msglog.StatusPending || entry.UserMessage == nil {
				continue
			}
			if chatID != "" && entry.UserMessage.ChatID != chatID {
				continue
			}
			msgs = append(msgs, conversationMessage{
				TS:      entry.TS,
				TSUnix:  entry.TSUnix,
				Speaker: "User",
				ChatID:  entry.UserMessage.ChatID,
				Text:    entry.UserMessage.Text,
			})
		case msglog.EntryAgentResponse:
			if entry.Status != msglog.StatusPending || entry.AgentResponse == nil {
				continue
			}
			if chatID != "" && entry.AgentResponse.ChatID != chatID {
				continue
			}
			msgs = append(msgs, conversationMessage{
				TS:      entry.TS,
				TSUnix:  entry.TSUnix,
				Speaker: "Assistant",
				ChatID:  entry.AgentResponse.ChatID,
				Text:    entry.AgentResponse.Text,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return msgs, nil
}

func buildConversationTurns(msgs []conversationMessage) []conversationTurn {
	var turns []conversationTurn
	for _, msg := range msgs {
		switch msg.Speaker {
		case "User":
			turns = append(turns, conversationTurn{User: &msg})
		case "Assistant":
			if len(turns) == 0 {
				turns = append(turns, conversationTurn{})
			}
			turns[len(turns)-1].Assistant = append(turns[len(turns)-1].Assistant, msg)
		}
	}
	return turns
}

func renderConversationMessage(msg conversationMessage, loc *time.Location) {
	ts := msg.TS
	if parsed, err := time.Parse(time.RFC3339, msg.TS); err == nil {
		ts = parsed.In(loc).Format("2006-01-02 15:04:05 MST")
	}

	fmt.Printf("[%s] %s\n", ts, msg.Speaker)
	fmt.Println(msg.Text)
	fmt.Println()
}

func init() {
	logsTurnsCmd.Flags().IntP("turns", "n", 10, "number of turns to render")
	logsTurnsCmd.Flags().String("chat", "", "filter by chat ID")
	logsCmd.AddCommand(logsTurnsCmd)
}
