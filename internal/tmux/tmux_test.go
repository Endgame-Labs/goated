package tmux

import "testing"

const rule = "────────────────────────────────────────────────────────────"

const envelope = `{
  "message": "Hi",
  "user_name": "Eric",
  "message_id": "9920",
}`

func TestHasUnsubmittedInput(t *testing.T) {
	cases := []struct {
		name string
		pane string
		want bool
	}{
		{"empty prompt", "● done\n" + rule + "\n❯ \n" + rule + "\n  ⏵⏵ bypass permissions on\n", false},
		{"pasted text left in box", "● done\n" + rule + "\n❯   \"user_name\": \"Eric\",\n    \"message_id\": \"9920\",\n  }\n" + rule + "\n  ⏵⏵ bypass permissions on\n", true},
		{"first pasted line on prompt", rule + "\n❯ \"message\": \"Hi\",\n" + rule + "\n", true},
		{"dimmed prompt suggestion", rule + "\n❯ check the daemon delivered both messages\n" + rule + "\n", false},
		{"no input box", "some output\n❯ \n", false},
		{"not a prompt box", rule + "\n  \"message_id\": \"9920\",\n" + rule + "\n", false},
	}
	for _, tc := range cases {
		if got := hasUnsubmittedInput(tc.pane, envelope); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
