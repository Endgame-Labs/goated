// Package email provides a shared email sending interface for toolbox commands.
// Currently backed by AgentMail, but the interface is backend-agnostic so a
// future Gmail/SMTP implementation can be swapped in by changing this file.
package email

import (
	"fmt"

	"toolbox-example/internal/creds"
	"toolbox-example/internal/httputil"
)

// Attachment represents a file attachment on an outbound email.
type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`      // base64-encoded
	ContentType string `json:"content_type"` // MIME type
}

// Message describes an outbound email.
type Message struct {
	From        string
	To          []string
	Subject     string
	Text        string
	Attachments []Attachment
}

// SendResult contains the response from the email backend.
type SendResult struct {
	MessageID string `json:"message_id"`
	ThreadID  string `json:"thread_id"`
}

// Send dispatches a message through the configured email backend.
// Currently uses AgentMail; swap this function body for a different
// provider without changing any callers.
func Send(msg Message) (*SendResult, error) {
	apiKey, err := creds.Get("AGENTMAIL_API_KEY")
	if err != nil {
		return nil, fmt.Errorf("no email API key (AGENTMAIL_API_KEY): %w", err)
	}

	url := fmt.Sprintf("https://api.agentmail.to/v0/inboxes/%s/messages/send", msg.From)

	payload := map[string]any{
		"to":      msg.To,
		"subject": msg.Subject,
		"text":    msg.Text,
	}
	if len(msg.Attachments) > 0 {
		atts := make([]map[string]any, len(msg.Attachments))
		for i, a := range msg.Attachments {
			atts[i] = map[string]any{
				"filename":     a.Filename,
				"content":      a.Content,
				"content_type": a.ContentType,
			}
		}
		payload["attachments"] = atts
	}

	var result SendResult
	status, err := httputil.PostJSON(url, payload, httputil.BearerAuth(apiKey), &result)
	if err != nil {
		return nil, fmt.Errorf("send email (status %d): %w", status, err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("send email failed with status %d", status)
	}
	return &result, nil
}
