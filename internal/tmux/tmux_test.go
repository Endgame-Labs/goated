package tmux

import "testing"

const rule = "────────────────────────────────────────────────────────────"

func TestHasUnsubmittedInput(t *testing.T) {
	cases := []struct {
		name string
		pane string
		want bool
	}{
		{"empty prompt", "● done\n" + rule + "\n❯ \n" + rule + "\n  ⏵⏵ bypass permissions on\n", false},
		{"pasted text left in box", "● done\n" + rule + "\n❯   \"user_name\": \"Eric\",\n    \"message_id\": \"9920\",\n  }\n" + rule + "\n  ⏵⏵ bypass permissions on\n", true},
		{"continuation lines only", rule + "\n❯ \n  }\n" + rule + "\n", true},
		{"no input box", "some output\n❯ \n", false},
		{"not a prompt box", rule + "\n  menu item\n" + rule + "\n", false},
	}
	for _, tc := range cases {
		if got := hasUnsubmittedInput(tc.pane); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
