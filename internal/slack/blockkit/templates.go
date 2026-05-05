package blockkit

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

// TaskItem represents a single task in a daily prep message.
type TaskItem struct {
	Number   int
	ID       string // e.g. "GTM-295"
	Title    string
	Duration string
	Slot     string
	Priority string
	Status   string
}

// AlertItem represents a single alert entry for health alert messages.
type AlertItem struct {
	OrgName string
	Summary string
	Detail  string
}

// DailyPrepMessage builds a Block Kit message for a daily prep briefing.
// Returns the blocks and a plain-text fallback string.
func DailyPrepMessage(heading string, schedule string, tasks []TaskItem, commands string) ([]slack.Block, string) {
	blocks := []slack.Block{
		Header(heading),
	}

	if schedule != "" {
		blocks = append(blocks, Section(schedule))
	}

	blocks = append(blocks, Divider())

	for _, t := range tasks {
		taskText := fmt.Sprintf("*%d. %s — %s*\n%s | %s | %s",
			t.Number, t.ID, t.Title, t.Duration, t.Slot, t.Priority)
		if t.Status != "" {
			taskText += " | " + t.Status
		}
		blocks = append(blocks, Section(taskText))
	}

	// Actions block with approve-all button and per-task skip buttons
	buttons := []*slack.ButtonBlockElement{
		PrimaryButton("daily_prep_approve_all", "Approve All", "approve_all"),
	}
	for _, t := range tasks {
		buttons = append(buttons,
			Button(
				fmt.Sprintf("daily_prep_skip_%d", t.Number),
				fmt.Sprintf("Skip %d", t.Number),
				fmt.Sprintf("skip_%d", t.Number),
			),
		)
	}
	blocks = append(blocks, Actions("daily_prep_actions", buttons...))

	if commands != "" {
		blocks = append(blocks, Divider())
		blocks = append(blocks, Context("daily_prep_help", commands))
	}

	// Build fallback text
	var fb strings.Builder
	fb.WriteString(heading)
	fb.WriteString("\n")
	if schedule != "" {
		fb.WriteString(schedule)
		fb.WriteString("\n")
	}
	for _, t := range tasks {
		fmt.Fprintf(&fb, "%d. %s — %s (%s, %s, %s)\n",
			t.Number, t.ID, t.Title, t.Duration, t.Slot, t.Priority)
	}

	return blocks, fb.String()
}

// HealthAlertMessage builds a Block Kit message for health/status alerts.
// Items are grouped by severity: urgent, watch, and fyi.
// Returns the blocks and a plain-text fallback string.
func HealthAlertMessage(date string, urgent, watch, fyi []AlertItem) ([]slack.Block, string) {
	blocks := []slack.Block{
		Header(fmt.Sprintf("Health Alerts — %s", date)),
	}

	var fb strings.Builder
	fmt.Fprintf(&fb, "Health Alerts — %s\n", date)

	addSection := func(label string, emoji string, items []AlertItem) {
		if len(items) == 0 {
			return
		}
		blocks = append(blocks, Divider())
		blocks = append(blocks, Section(fmt.Sprintf("%s *%s* (%d)", emoji, label, len(items))))
		for _, item := range items {
			text := fmt.Sprintf("*%s* — %s", item.OrgName, item.Summary)
			if item.Detail != "" {
				text += "\n" + item.Detail
			}
			blocks = append(blocks, Section(text))
			fmt.Fprintf(&fb, "[%s] %s — %s\n", label, item.OrgName, item.Summary)
		}
	}

	addSection("Urgent", ":red_circle:", urgent)
	addSection("Watch", ":large_yellow_circle:", watch)
	addSection("FYI", ":large_blue_circle:", fyi)

	total := len(urgent) + len(watch) + len(fyi)
	blocks = append(blocks, Divider())
	blocks = append(blocks,
		Context("health_alert_footer",
			fmt.Sprintf("%d alert(s) total | Generated %s", total, date),
		),
	)

	return blocks, fb.String()
}
