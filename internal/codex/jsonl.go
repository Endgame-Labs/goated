package codex

import (
	"bufio"
	"encoding/json"
	"strings"
)

type jsonlEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

func parseThreadID(jsonlOutput string) string {
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	for scanner.Scan() {
		var evt jsonlEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Type == "thread.started" && evt.ThreadID != "" {
			return evt.ThreadID
		}
	}
	return ""
}

func parseLastAgentMessage(jsonlOutput string) string {
	var last string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	for scanner.Scan() {
		var evt jsonlEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Type == "item.completed" && evt.Item != nil && evt.Item.Type == "agent_message" && evt.Item.Text != "" {
			last = evt.Item.Text
		}
	}
	return last
}
