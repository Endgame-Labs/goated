package util

import (
	"regexp"
	"strconv"
	"strings"
)

var userMessageRe = regexp.MustCompile(`(?s):START_USER_MESSAGE:\s*(.*?)\s*:END_USER_MESSAGE:`)
var oldUserMessageRe = regexp.MustCompile(`(?s)<<<START_USER_MESSAGE>>>\s*(.*?)\s*<<<END_USER_(?:M|)ESSAGE>>>`)
var legacyBlockRe = regexp.MustCompile(`(?s)<<>>\s*(.*?)\s*<<>>`)
var cursorForwardRe = regexp.MustCompile(`\x1b\[([0-9]*)C`)
var ansiCsiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
var ansiSingleRe = regexp.MustCompile(`\x1b[@-Z\\-_]`)
var ansiOscRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
var controlExceptNewlineTabRe = regexp.MustCompile(`[\x00-\x08\x0B-\x1F\x7F]`)

// tuiBulletRe strips Claude Code's TUI bullet prefixes from capture-pane lines.
var tuiBulletRe = regexp.MustCompile(`(?m)^[●✽✢•▸▹▪]\ ?`)

func ExtractUserMessage(s string) string {
	// Strip ANSI and TUI bullet prefixes before matching delimiters.
	clean := CleanTerminalText(s)

	matches := userMessageRe.FindAllStringSubmatch(clean, -1)
	if len(matches) == 0 {
		matches = oldUserMessageRe.FindAllStringSubmatch(clean, -1)
	}
	if len(matches) == 0 {
		legacy := legacyBlockRe.FindAllStringSubmatch(clean, -1)
		if len(legacy) == 0 {
			return ""
		}
		msg := normalizeWhitespace(legacy[len(legacy)-1][1])
		if isPlaceholderOnly(msg) {
			return ""
		}
		return msg
	}
	msg := normalizeWhitespace(matches[len(matches)-1][1])
	if isPlaceholderOnly(msg) {
		return ""
	}
	return msg
}

func CleanTerminalText(s string) string {
	clean := stripAnsi(s)
	clean = tuiBulletRe.ReplaceAllString(clean, "")
	return strings.TrimSpace(clean)
}

func stripAnsi(s string) string {
	// Replace cursor-forward sequences with spaces first
	s = cursorForwardRe.ReplaceAllStringFunc(s, func(seq string) string {
		m := cursorForwardRe.FindStringSubmatch(seq)
		if len(m) < 2 || m[1] == "" {
			return " "
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return " "
		}
		return strings.Repeat(" ", n)
	})
	s = ansiOscRe.ReplaceAllString(s, "")
	s = ansiCsiRe.ReplaceAllString(s, "")
	s = ansiSingleRe.ReplaceAllString(s, "")
	s = controlExceptNewlineTabRe.ReplaceAllString(s, "")
	return s
}

// excessiveNewlines collapses 3+ consecutive newlines to 2.
var excessiveNewlines = regexp.MustCompile(`\n{3,}`)

func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	result := strings.Join(lines, "\n")
	result = excessiveNewlines.ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

func isPlaceholderOnly(msg string) bool {
	candidate := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(msg), " ", ""))
	return candidate == "(yourresponse)" || candidate == "yourresponse"
}
