package util

import (
	"testing"
)

func TestMarkdownToTelegramHTML_Bold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple bold", "**hello**", "<b>hello</b>"},
		{"bold in sentence", "this is **bold** text", "this is <b>bold</b> text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToTelegramHTML(tt.in)
			if got != tt.want {
				t.Errorf("MarkdownToTelegramHTML(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_Italic(t *testing.T) {
	got := MarkdownToTelegramHTML("*italic*")
	want := "<i>italic</i>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_BoldItalic(t *testing.T) {
	got := MarkdownToTelegramHTML("***bold italic***")
	want := "<b><i>bold italic</i></b>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_Strikethrough(t *testing.T) {
	got := MarkdownToTelegramHTML("~~deleted~~")
	want := "<s>deleted</s>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_InlineCode(t *testing.T) {
	got := MarkdownToTelegramHTML("use `fmt.Println` here")
	want := "use <code>fmt.Println</code> here"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_Headers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"h1", "# Title", "<b>Title</b>"},
		{"h2", "## Section", "<b>Section</b>"},
		{"h3", "### Sub", "<b>Sub</b>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToTelegramHTML(tt.in)
			if got != tt.want {
				t.Errorf("MarkdownToTelegramHTML(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_CodeBlockWithLang(t *testing.T) {
	in := "```python\ndef foo():\n  pass\n```"
	got := MarkdownToTelegramHTML(in)
	want := "<pre><code class=\"language-python\">\ndef foo():\n  pass\n</code></pre>"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownToTelegramHTML_CodeBlockNoLang(t *testing.T) {
	in := "```\nsome code\n```"
	got := MarkdownToTelegramHTML(in)
	want := "<pre><code>\nsome code\n</code></pre>"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownToTelegramHTML_Blockquote(t *testing.T) {
	got := MarkdownToTelegramHTML("> quoted text")
	want := "<blockquote>quoted text</blockquote>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_UnorderedList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"dash list", "- item one", "• item one"},
		{"star list", "* item one", "• item one"},
		{"indented dash", "  - nested", "• nested"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToTelegramHTML(tt.in)
			if got != tt.want {
				t.Errorf("MarkdownToTelegramHTML(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_HTMLEscaping(t *testing.T) {
	got := MarkdownToTelegramHTML("a < b & c > d")
	want := "a &lt; b &amp; c &gt; d"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_HTMLEscapingInCodeBlock(t *testing.T) {
	in := "```\nif a < b && c > d {}\n```"
	got := MarkdownToTelegramHTML(in)
	want := "<pre><code>\nif a &lt; b &amp;&amp; c &gt; d {}\n</code></pre>"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownToTelegramHTML_UnclosedCodeBlock(t *testing.T) {
	// Unclosed code block should auto-close
	in := "```\nunclosed"
	got := MarkdownToTelegramHTML(in)
	want := "<pre><code>\nunclosed\n</code></pre>"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownToTelegramHTML_EmptyInput(t *testing.T) {
	got := MarkdownToTelegramHTML("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestMarkdownToTelegramHTML_Mixed(t *testing.T) {
	in := "# Header\n\n**Bold** and *italic*\n> quote\n- list item"
	got := MarkdownToTelegramHTML(in)
	want := "<b>Header</b>\n\n<b>Bold</b> and <i>italic</i>\n<blockquote>quote</blockquote>\n• list item"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
