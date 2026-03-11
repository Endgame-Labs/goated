package cli

import (
	"bufio"
	"fmt"
	"strings"
)

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func withDefault(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}
