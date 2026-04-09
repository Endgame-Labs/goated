package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"toolbox-example/internal/creds"
	"toolbox-example/internal/email"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func calendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Calendar operations (invites, etc.)",
	}
	cmd.AddCommand(calendarInviteCmd())
	return cmd
}

func calendarInviteCmd() *cobra.Command {
	var (
		to          string
		subject     string
		description string
		start       string
		duration    string
		location    string
		inbox       string
	)

	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Send a calendar invite via email",
		Long: `Generate an iCalendar (.ics) invite and send it via the configured email backend.
Start time is interpreted as Pacific if no timezone given.

Examples:
  toolbox calendar invite --to "user@example.com" --subject "Weekly sync" --start "2026-04-10T15:00"
  toolbox calendar invite --to "a@x.com,b@x.com" --subject "Team mtg" --start "2026-04-10T10:00" --duration 1h --location Zoom`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			if subject == "" {
				return fmt.Errorf("--subject is required")
			}
			if start == "" {
				return fmt.Errorf("--start is required")
			}

			// Resolve sender inbox
			from, err := resolveCalendarInbox(inbox)
			if err != nil {
				return err
			}

			// Parse duration
			dur, err := time.ParseDuration(duration)
			if err != nil {
				return fmt.Errorf("invalid --duration %q: %w", duration, err)
			}

			// Parse start time in Pacific if no timezone offset
			pac, err := time.LoadLocation("America/Los_Angeles")
			if err != nil {
				return fmt.Errorf("loading Pacific timezone: %w", err)
			}
			var startTime time.Time
			for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00"} {
				if t, err := time.Parse(layout, start); err == nil {
					startTime = t
					break
				}
			}
			if startTime.IsZero() {
				for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
					if t, err := time.ParseInLocation(layout, start, pac); err == nil {
						startTime = t
						break
					}
				}
			}
			if startTime.IsZero() {
				return fmt.Errorf("could not parse --start %q (use ISO format like 2026-04-10T15:00)", start)
			}

			endTime := startTime.Add(dur)
			recipients := strings.Split(to, ",")
			for i := range recipients {
				recipients[i] = strings.TrimSpace(recipients[i])
			}

			// Generate ICS
			uid := uuid.New().String()
			now := time.Now().UTC()
			ics := generateICS(uid, subject, description, location, from, recipients, startTime, endTime, now)
			icsB64 := base64.StdEncoding.EncodeToString([]byte(ics))

			// Send via shared email package
			result, err := email.Send(email.Message{
				From:    from,
				To:      recipients,
				Subject: subject,
				Text:    fmt.Sprintf("You have been invited to: %s\n\nWhen: %s\nDuration: %s", subject, startTime.Format("Mon, Jan 2 2006 at 3:04 PM MST"), dur),
				Attachments: []email.Attachment{
					{
						Filename:    "invite.ics",
						Content:     icsB64,
						ContentType: "text/calendar; method=REQUEST",
					},
				},
			})
			if err != nil {
				return fmt.Errorf("send invite: %w", err)
			}

			fmt.Printf("Calendar invite sent!\n")
			fmt.Printf("  From: %s\n", from)
			fmt.Printf("  To: %s\n", strings.Join(recipients, ", "))
			fmt.Printf("  Subject: %s\n", subject)
			fmt.Printf("  When: %s\n", startTime.Format("Mon, Jan 2 2006 at 3:04 PM MST"))
			fmt.Printf("  Duration: %s\n", dur)
			if location != "" {
				fmt.Printf("  Location: %s\n", location)
			}
			fmt.Printf("  Message ID: %s\n", result.MessageID)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "Recipient email(s), comma-separated (required)")
	cmd.Flags().StringVar(&subject, "subject", "", "Invite subject/title (required)")
	cmd.Flags().StringVar(&description, "description", "", "Event description")
	cmd.Flags().StringVar(&start, "start", "", "Start time, ISO format e.g. 2026-04-10T15:00 (required, Pacific if no TZ)")
	cmd.Flags().StringVar(&duration, "duration", "30m", "Duration (Go duration, e.g. 30m, 1h)")
	cmd.Flags().StringVar(&location, "location", "", "Event location (optional)")
	cmd.Flags().StringVar(&inbox, "inbox", "", "Override configured sender inbox")

	return cmd
}

func resolveCalendarInbox(inbox string) (string, error) {
	if inbox != "" {
		return inbox, nil
	}
	inbox, err := creds.Get("AGENTMAIL_INBOX")
	if err != nil || inbox == "" {
		return "", fmt.Errorf("no inbox configured; set AGENTMAIL_INBOX or pass --inbox")
	}
	return inbox, nil
}

func generateICS(uid, summary, description, location, organizer string, attendees []string, start, end, stamp time.Time) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//Goated Toolbox//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	b.WriteString("METHOD:REQUEST\r\n")
	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&b, "UID:%s\r\n", uid)
	fmt.Fprintf(&b, "DTSTAMP:%s\r\n", stamp.UTC().Format("20060102T150405Z"))
	fmt.Fprintf(&b, "DTSTART:%s\r\n", start.UTC().Format("20060102T150405Z"))
	fmt.Fprintf(&b, "DTEND:%s\r\n", end.UTC().Format("20060102T150405Z"))
	fmt.Fprintf(&b, "SUMMARY:%s\r\n", escapeICSValue(summary))
	if description != "" {
		fmt.Fprintf(&b, "DESCRIPTION:%s\r\n", escapeICSValue(description))
	}
	if location != "" {
		fmt.Fprintf(&b, "LOCATION:%s\r\n", escapeICSValue(location))
	}
	fmt.Fprintf(&b, "ORGANIZER;CN=%s:mailto:%s\r\n", organizer, organizer)
	for _, att := range attendees {
		fmt.Fprintf(&b, "ATTENDEE;RSVP=TRUE;PARTSTAT=NEEDS-ACTION;ROLE=REQ-PARTICIPANT:mailto:%s\r\n", att)
	}
	b.WriteString("STATUS:CONFIRMED\r\n")
	b.WriteString("SEQUENCE:0\r\n")
	b.WriteString("END:VEVENT\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func escapeICSValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
