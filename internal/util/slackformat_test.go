package util

import (
	"testing"
)

func TestMarkdownToSlackMrkdwn_Bold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple bold", "**hello**", "*hello*"},
		{"bold in sentence", "this is **bold** text", "this is *bold* text"},
		{"multiple bold", "**a** and **b**", "*a* and *b*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToSlackMrkdwn(tt.in)
			if got != tt.want {
				t.Errorf("MarkdownToSlackMrkdwn(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkdownToSlackMrkdwn_BoldItalic(t *testing.T) {
	got := MarkdownToSlackMrkdwn("***bold italic***")
	want := "*_bold italic_*"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToSlackMrkdwn_Strikethrough(t *testing.T) {
	got := MarkdownToSlackMrkdwn("~~deleted~~")
	want := "~deleted~"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToSlackMrkdwn_Headers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"h1", "# Title", "*Title*"},
		{"h2", "## Section", "*Section*"},
		{"h3", "### Subsection", "*Subsection*"},
		{"h6", "###### Deep", "*Deep*"},
		{"header with bold", "# **Already Bold**", "**Already Bold**"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToSlackMrkdwn(tt.in)
			if got != tt.want {
				t.Errorf("MarkdownToSlackMrkdwn(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkdownToSlackMrkdwn_CodeBlocks(t *testing.T) {
	in := "before\n```python\ndef foo():\n  pass\n```\nafter"
	got := MarkdownToSlackMrkdwn(in)
	// Code blocks should pass through unchanged
	if got != in {
		t.Errorf("code block was modified:\ngot:  %q\nwant: %q", got, in)
	}
}

func TestMarkdownToSlackMrkdwn_CodeBlockNoLang(t *testing.T) {
	in := "```\nsome code\n```"
	got := MarkdownToSlackMrkdwn(in)
	if got != in {
		t.Errorf("code block was modified:\ngot:  %q\nwant: %q", got, in)
	}
}

func TestMarkdownToSlackMrkdwn_InlineCode(t *testing.T) {
	// Inline code uses backticks in both formats, should pass through
	in := "use `fmt.Println` here"
	got := MarkdownToSlackMrkdwn(in)
	if got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestMarkdownToSlackMrkdwn_BackslashEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"escaped bang", `hello \!`, "hello !"},
		{"escaped dot", `1\. item`, "1. item"},
		{"escaped dash", `\- item`, "- item"},
		{"escaped parens", `\(a\)`, "(a)"},
		{"escaped hash", `\# not a header`, "# not a header"},
		{"escaped bracket", `\[link\]`, "[link]"},
		{"multiple escapes", `\! \. \-`, "! . -"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToSlackMrkdwn(tt.in)
			if got != tt.want {
				t.Errorf("MarkdownToSlackMrkdwn(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkdownToSlackMrkdwn_Mixed(t *testing.T) {
	in := "# Header\n\n**Bold** and ~~strike~~\n```\ncode\n```"
	got := MarkdownToSlackMrkdwn(in)
	want := "*Header*\n\n*Bold* and ~strike~\n```\ncode\n```"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownToSlackMrkdwn_BoldInsideCodeBlock(t *testing.T) {
	// Bold markers inside code blocks should NOT be converted
	in := "```\n**not bold**\n```"
	got := MarkdownToSlackMrkdwn(in)
	if got != in {
		t.Errorf("bold inside code block was converted:\ngot:  %q\nwant: %q", got, in)
	}
}

func TestMarkdownToSlackMrkdwn_EmptyInput(t *testing.T) {
	got := MarkdownToSlackMrkdwn("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
