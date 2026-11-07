package tmux

import (
	"os"
	"path/filepath"
	"testing"
)

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

// TestHasPrompt runs on panes captured from Claude Code. The tall footer
// (status line plus update notice and background tasks) pushes the input box
// out of the last 5 lines, which made an idle session look stuck after Escape
// (#16).
func TestHasPrompt(t *testing.T) {
	tests := []struct {
		file string
		want bool
	}{
		{"idle_after_escape_tall_footer.txt", true},
		{"idle_suggestion_tall_footer.txt", true},
		{"permission_menu_tall_footer.txt", false},
		{"no_prompt.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			pane, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if got := HasPrompt(string(pane)); got != tt.want {
				t.Fatalf("HasPrompt() = %v, want %v", got, tt.want)
			}
		})
	}
}
