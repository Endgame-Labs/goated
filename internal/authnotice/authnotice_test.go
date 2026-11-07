package authnotice

import (
	"context"
	"strings"
	"testing"
)

type sent struct{ chatID, text string }

func newTestNotifier(out *[]sent) *Notifier {
	return &Notifier{
		AdminChatID: "admin",
		SessionName: "goat_claude_main",
		Send: func(_ context.Context, chatID, text string) error {
			*out = append(*out, sent{chatID, text})
			return nil
		},
	}
}

func TestExpiredOnceUntilRestored(t *testing.T) {
	var out []sent
	n := newTestNotifier(&out)
	ctx := context.Background()

	n.Restored(ctx) // nothing reported yet: no notice
	n.Expired(ctx)
	n.Expired(ctx)
	if len(out) != 1 {
		t.Fatalf("got %d notices after two expiries, want 1: %+v", len(out), out)
	}
	msg := out[0].text
	for _, want := range []string{"tmux attach -t goat_claude_main", "/login", "won't be replayed", "next scheduled time"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expired notice %q is missing %q", msg, want)
		}
	}
	if out[0].chatID != "admin" {
		t.Errorf("notice went to %q, want admin", out[0].chatID)
	}

	n.Restored(ctx)
	n.Restored(ctx)
	if len(out) != 2 || !strings.Contains(out[1].text, "works again") {
		t.Fatalf("want one restored notice, got %+v", out)
	}

	n.Expired(ctx) // a new expiry is reported again
	if len(out) != 3 {
		t.Fatalf("want a notice for the second expiry, got %+v", out)
	}
}

func TestNoAdminChatOrNilNotifierSendsNothing(t *testing.T) {
	var out []sent
	n := newTestNotifier(&out)
	n.AdminChatID = ""
	n.Expired(context.Background())
	if len(out) != 0 {
		t.Fatalf("sent %+v without an admin chat", out)
	}
	var none *Notifier
	none.Expired(context.Background())
	none.Restored(context.Background())
}

func TestMatchIgnoresOrdinaryErrors(t *testing.T) {
	if Match("API Error: 500 Internal server error") != "" {
		t.Fatal("a 500 is not an auth failure")
	}
	if Match(`API Error: 401 {"type":"error","error":{"type":"authentication_error"}}`) == "" {
		t.Fatal("a 401 should match")
	}
}
