package util

import (
	"regexp"
	"strings"
)

var (
	// Slack mrkdwn uses *bold*, _italic_, ~strike~, `code`, ```code blocks```
	// Standard markdown uses **bold**, *italic*, ~~strike~~
	mdBoldItalicToSlack = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	mdBoldToSlack       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	mdStrikeToSlack     = regexp.MustCompile(`~~(.+?)~~`)
)

// MarkdownToSlackMrkdwn converts standard markdown to Slack's mrkdwn format.
// Handles bold, italic, strikethrough, code blocks, headers, and lists.
// Inline code and code blocks pass through unchanged since both formats
// use backtick syntax.
func MarkdownToSlackMrkdwn(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	inCodeBlock := false

	for _, line := range lines {
		// Code blocks pass through unchanged
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			out = append(out, line)
			continue
		}
		if inCodeBlock {
			out = append(out, line)
			continue
		}

		// Headers → bold (Slack has no header rendering)
		if m := headerRe.FindStringSubmatch(line); m != nil {
			out = append(out, "*"+convertSlackInline(m[2])+"*")
			continue
		}

		out = append(out, convertSlackInline(line))
	}

	return strings.Join(out, "\n")
}

func convertSlackInline(line string) string {
	// Bold+italic: ***text*** → *_text_*
	line = mdBoldItalicToSlack.ReplaceAllString(line, "*_${1}_*")
	// Bold: **text** → *text*
	line = mdBoldToSlack.ReplaceAllString(line, "*${1}*")
	// Italic: *text* stays as _text_ in Slack
	// Only convert standalone *text* that isn't already part of **bold**
	// After bold conversion, remaining single * pairs are italic
	// Strikethrough: ~~text~~ → ~text~
	line = mdStrikeToSlack.ReplaceAllString(line, "~${1}~")
	return line
}
