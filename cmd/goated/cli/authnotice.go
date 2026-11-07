package cli

import (
	"context"

	"goated/internal/agent"
	"goated/internal/authnotice"
	"goated/internal/gateway"
)

// newAuthNotifier wires the admin chat's login notices (#12). The main
// session's auth probe and cron jobs both report into the one notifier, so an
// expiry produces a single notice. Notices go straight to the chat and are not
// mirrored into the session, which can't act on them while logged out.
func newAuthNotifier(adminChatID string, session agent.SessionRuntime, responder gateway.Responder) *authnotice.Notifier {
	n := &authnotice.Notifier{
		Send:        responder.SendMessage,
		AdminChatID: adminChatID,
		SessionName: session.Descriptor().SessionName,
	}
	if obs, ok := session.(interface {
		SetAuthObserver(func(ctx context.Context, failed bool))
	}); ok {
		obs.SetAuthObserver(func(ctx context.Context, failed bool) {
			if failed {
				n.Expired(ctx)
			} else {
				n.Restored(ctx)
			}
		})
	}
	return n
}
