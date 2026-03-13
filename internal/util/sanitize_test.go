package util

import (
	"testing"
)

func TestSafeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "default"},
		{"simple", "hello", "hello"},
		{"with spaces", "hello world", "hello_world"},
		{"with special chars", "foo@bar.com", "foo_bar_com"},
		{"preserves dashes", "my-name", "my-name"},
		{"preserves underscores", "my_name", "my_name"},
		{"preserves digits", "abc123", "abc123"},
		{"all special", "!@#$%", "_"},
		{"mixed", "Hello, World! 123", "Hello_World_123"},
		{"unicode", "caf\u00e9", "caf_"},
		{"leading special", ".hidden", "_hidden"},
		{"consecutive special", "a//b", "a_b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeName(tt.in)
			if got != tt.want {
				t.Errorf("SafeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
