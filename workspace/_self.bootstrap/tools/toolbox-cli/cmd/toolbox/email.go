package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"toolbox-example/internal/archive"
	"toolbox-example/internal/creds"
	"toolbox-example/internal/httputil"

	"github.com/spf13/cobra"
)

func emailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "email",
		Short: "AgentMail operations for one configured @agentmail.to inbox",
		Long: `This example assumes one primary AgentMail inbox stored in goat creds as
AGENTMAIL_INBOX.`,
	}
	cmd.AddCommand(emailCheckCmd())
	cmd.AddCommand(emailSendCmd())
	return cmd
}

func emailSendCmd() *cobra.Command {
	var to, subject, body, inbox string
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send an email via AgentMail",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedInbox, err := resolveInbox(inbox)
			if err != nil {
				return err
			}
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			if subject == "" {
				return fmt.Errorf("--subject is required")
			}
			if body == "" {
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					data, err := os.ReadFile("/dev/stdin")
					if err != nil {
						return fmt.Errorf("reading stdin: %w", err)
					}
					body = strings.TrimSpace(string(data))
				}
			}
			if body == "" {
				return fmt.Errorf("--body is required (or pipe content via stdin)")
			}

			apiKey, err := creds.Get("AGENTMAIL_API_KEY")
			if err != nil {
				return fmt.Errorf("get AGENTMAIL_API_KEY via goat creds: %w", err)
			}
			url := fmt.Sprintf("https://api.agentmail.to/v0/inboxes/%s/messages/send", resolvedInbox)
			payload := map[string]any{
				"to":      []string{to},
				"subject": subject,
				"text":    body,
			}
			var result struct {
				MessageID string `json:"message_id"`
				ThreadID  string `json:"thread_id"`
			}
			status, err := httputil.PostJSON(url, payload, httputil.BearerAuth(apiKey), &result)
			if err != nil {
				return fmt.Errorf("send email (status %d): %w", status, err)
			}
			if status >= 400 {
				return fmt.Errorf("send email failed with status %d", status)
			}
			fmt.Printf("Email sent from %s to %s\n", resolvedInbox, to)
			fmt.Printf("Subject: %s\n", subject)
			fmt.Printf("Message ID: %s\n", result.MessageID)
			fmt.Printf("Thread ID: %s\n", result.ThreadID)
			return nil
		},
	}
	cmd.Flags().StringVar(&inbox, "inbox", "", "Override configured AgentMail inbox")
	cmd.Flags().StringVar(&to, "to", "", "Recipient email address")
	cmd.Flags().StringVar(&subject, "subject", "", "Email subject")
	cmd.Flags().StringVar(&body, "body", "", "Email body (or pipe via stdin)")
	return cmd
}

func emailCheckCmd() *cobra.Command {
	var inbox string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check the configured AgentMail inbox for new messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedInbox, err := resolveInbox(inbox)
			if err != nil {
				return err
			}
			return checkInbox(resolvedInbox)
		},
	}
	cmd.Flags().StringVar(&inbox, "inbox", "", "Override configured AgentMail inbox")
	return cmd
}

func resolveInbox(inbox string) (string, error) {
	if inbox == "" {
		var err error
		inbox, err = creds.Get("AGENTMAIL_INBOX")
		if err != nil {
			return "", fmt.Errorf("get AGENTMAIL_INBOX via goat creds: %w", err)
		}
	}
	if inbox == "" {
		return "", fmt.Errorf("no inbox configured; set AGENTMAIL_INBOX with goat creds or pass --inbox")
	}
	if !strings.HasSuffix(strings.ToLower(inbox), "@agentmail.to") {
		return "", fmt.Errorf("configured inbox must be an @agentmail.to address: %s", inbox)
	}
	return inbox, nil
}

func checkInbox(inbox string) error {
	apiKey, err := creds.Get("AGENTMAIL_API_KEY")
	if err != nil {
		return fmt.Errorf("get AGENTMAIL_API_KEY via goat creds: %w", err)
	}

	stateFile := "email-state-" + strings.NewReplacer("@", "_", ".", "_").Replace(inbox) + ".json"
	var state struct {
		LastSeenTimestamp string   `json:"last_seen_timestamp"`
		SeenIDs           []string `json:"seen_ids"`
		LastCheck         string   `json:"last_check"`
	}
	if data, err := archive.ReadState(stateFile); err == nil && data != "" {
		_ = json.Unmarshal([]byte(data), &state)
	}

	seenSet := map[string]bool{}
	for _, id := range state.SeenIDs {
		seenSet[id] = true
	}

	url := fmt.Sprintf("https://api.agentmail.to/v0/inboxes/%s/messages?limit=100", inbox)
	var result struct {
		Messages []struct {
			MessageID string      `json:"message_id"`
			From      interface{} `json:"from"`
			Subject   string      `json:"subject"`
			Preview   string      `json:"preview"`
			CreatedAt string      `json:"created_at"`
			Labels    []string    `json:"labels"`
		} `json:"messages"`
	}
	if _, err := httputil.GetJSON(url, httputil.BearerAuth(apiKey), &result); err != nil {
		return fmt.Errorf("check inbox: %w", err)
	}

	type newMessage struct {
		From    string
		Subject string
		Body    string
	}
	var newMessages []newMessage

	for _, msg := range result.Messages {
		if seenSet[msg.MessageID] {
			continue
		}
		if hasLabel(msg.Labels, "sent") {
			continue
		}
		if state.LastSeenTimestamp != "" && msg.CreatedAt <= state.LastSeenTimestamp {
			continue
		}

		sender := senderAddress(msg.From)
		body := ""
		msgURL := fmt.Sprintf("https://api.agentmail.to/v0/inboxes/%s/messages/%s", inbox, msg.MessageID)
		var fullMsg struct {
			Text string `json:"text"`
			HTML string `json:"html"`
		}
		if _, err := httputil.GetJSON(msgURL, httputil.BearerAuth(apiKey), &fullMsg); err == nil {
			body = fullMsg.Text
			if body == "" {
				body = fullMsg.HTML
			}
		}

		newMessages = append(newMessages, newMessage{
			From:    sender,
			Subject: msg.Subject,
			Body:    body,
		})
		state.SeenIDs = append(state.SeenIDs, msg.MessageID)
	}

	for _, m := range result.Messages {
		if m.CreatedAt > state.LastSeenTimestamp {
			state.LastSeenTimestamp = m.CreatedAt
		}
	}
	state.LastCheck = time.Now().UTC().Format(time.RFC3339)
	if len(state.SeenIDs) > 500 {
		state.SeenIDs = state.SeenIDs[len(state.SeenIDs)-500:]
	}

	data, _ := json.MarshalIndent(state, "", "  ")
	if err := archive.WriteState(stateFile, string(data)); err != nil {
		return err
	}

	if len(newMessages) == 0 {
		fmt.Printf("No new emails in %s\n", inbox)
		return nil
	}

	fmt.Printf("%d NEW EMAIL(S) in %s:\n\n", len(newMessages), inbox)
	for _, msg := range newMessages {
		fmt.Printf("From: %s\n", msg.From)
		fmt.Printf("Subject: %s\n", msg.Subject)
		if msg.Body != "" {
			fmt.Printf("Body:\n%s\n", msg.Body)
		}
		fmt.Println()
	}
	return nil
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func senderAddress(from any) string {
	switch v := from.(type) {
	case map[string]any:
		if addr, ok := v["address"].(string); ok {
			return addr
		}
	case string:
		return v
	}
	return "unknown"
}
