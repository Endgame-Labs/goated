package gateway

import "testing"

func TestPrivateGoalChat(t *testing.T) {
	cases := []struct {
		msg  IncomingMessage
		want bool
	}{
		{IncomingMessage{Channel: "slack", ChatID: "D123"}, true},
		{IncomingMessage{Channel: "slack", ChatID: "C123"}, false},
		{IncomingMessage{Channel: "slack", ChatID: "G123"}, false},
		{IncomingMessage{Channel: "telegram", ChatType: "private"}, true},
		{IncomingMessage{Channel: "telegram", ChatType: "group"}, false},
		{IncomingMessage{Channel: "slack", ChatID: "D123", ChatType: "group"}, false},
	}
	for _, tc := range cases {
		if got := privateGoalChat(tc.msg); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.msg, got, tc.want)
		}
	}
}
