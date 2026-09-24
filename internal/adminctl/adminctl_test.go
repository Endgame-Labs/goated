package adminctl

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in      string
		wantSub string
		wantOK  bool
	}{
		{"/admin restart", "restart", true},
		{"  /admin   status  ", "status", true},
		{"/admin RESTART", "restart", true},          // case-folded
		{"/admin", "help", true},                     // bare -> help
		{"/admin status extra args", "status", true}, // extra args ignored
		{"hello /admin restart", "", false},          // not a prefix
		{"/administrate", "", false},                 // not the /admin token
		{"restart", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		gotSub, gotOK := Parse(tc.in)
		if gotSub != tc.wantSub || gotOK != tc.wantOK {
			t.Errorf("Parse(%q) = (%q, %v), want (%q, %v)", tc.in, gotSub, gotOK, tc.wantSub, tc.wantOK)
		}
	}
}

func TestExecuteUnknown(t *testing.T) {
	res := Execute("bogus")
	if res.After != nil {
		t.Errorf("unknown command should not carry an After action")
	}
	if res.Reply == "" {
		t.Errorf("unknown command should still reply")
	}
}

func TestExecuteRestartHasDeferredAction(t *testing.T) {
	res := Execute("restart")
	if res.After == nil {
		t.Errorf("restart must defer its action so the reply is sent first")
	}
}
