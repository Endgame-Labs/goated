package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"goated/internal/adminctl"
)

func adminUpdate(chatID, senderID int64, chatType, text string) tgbotapi.Update {
	return tgbotapi.Update{
		UpdateID: 99,
		Message: &tgbotapi.Message{
			Text: text,
			Chat: &tgbotapi.Chat{ID: chatID, Type: chatType},
			From: &tgbotapi.User{ID: senderID},
		},
	}
}

func TestAdminCommandRequiresPrivateOwnerIdentity(t *testing.T) {
	tests := []struct {
		name     string
		update   tgbotapi.Update
		consumed bool
	}{
		{"private owner", adminUpdate(42, 42, "private", "/admin status"), true},
		{"configured group cannot authorize members", adminUpdate(42, 7, "supergroup", "/admin restart"), false},
		{"private chat sender must match owner", adminUpdate(42, 7, "private", "/admin restart"), false},
		{"different private chat rejected", adminUpdate(7, 7, "private", "/admin restart"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			c := &Connector{
				adminChatID: 42,
				adminSend:   func(context.Context, string, string) error { return nil },
				adminExecute: func(string) adminctl.Result {
					executed = true
					return adminctl.Result{Reply: "ok"}
				},
			}
			if got := c.tryHandleAdminCommand(context.Background(), tt.update); got != tt.consumed {
				t.Fatalf("tryHandleAdminCommand() = %v, want %v", got, tt.consumed)
			}
			if executed != tt.consumed {
				t.Fatalf("execute called = %v, want %v", executed, tt.consumed)
			}
		})
	}
}

func TestAdminRestartRunsOnlyAfterSuccessfulReceipt(t *testing.T) {
	tests := []struct {
		name      string
		sendError error
		wantAfter bool
	}{
		{"receipt sent", nil, true},
		{"receipt failed", errors.New("telegram unavailable"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			afterCalled := false
			c := &Connector{
				adminChatID: 42,
				adminSend:   func(context.Context, string, string) error { return tt.sendError },
				adminExecute: func(string) adminctl.Result {
					return adminctl.Result{Reply: "restarting", After: func() { afterCalled = true }}
				},
			}
			if !c.tryHandleAdminCommand(context.Background(), adminUpdate(42, 42, "private", "/admin restart")) {
				t.Fatal("owner admin command was not consumed")
			}
			if afterCalled != tt.wantAfter {
				t.Fatalf("After called = %v, want %v", afterCalled, tt.wantAfter)
			}
		})
	}
}

func TestDispatchDoesNotBlockWhenNormalQueueIsFull(t *testing.T) {
	work := make(chan tgbotapi.Update, 1)
	work <- tgbotapi.Update{UpdateID: 1}
	c := &Connector{}
	done := make(chan bool, 1)
	go func() {
		done <- c.dispatch(context.Background(), work, tgbotapi.Update{UpdateID: 2})
	}()
	select {
	case queued := <-done:
		if queued {
			t.Fatal("full queue reported update as queued")
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch blocked on a full normal-work queue")
	}
}

func TestDispatchHandlesAdminWhileNormalQueueIsFull(t *testing.T) {
	work := make(chan tgbotapi.Update, 1)
	work <- tgbotapi.Update{UpdateID: 1}
	afterCalled := false
	c := &Connector{
		adminChatID: 42,
		adminSend:   func(context.Context, string, string) error { return nil },
		adminExecute: func(string) adminctl.Result {
			return adminctl.Result{Reply: "restarting", After: func() { afterCalled = true }}
		},
	}
	if !c.dispatch(context.Background(), work, adminUpdate(42, 42, "private", "/admin restart")) {
		t.Fatal("admin command was not handled with full normal queue")
	}
	if !afterCalled {
		t.Fatal("admin action did not run")
	}
}
