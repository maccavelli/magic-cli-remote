package acpagent

import (
	"fmt"
	"strings"
)

// annotateLimit appends the structured summary to a classified error message.
func annotateLimit(message, summary string) string {
	if summary == "" {
		return message
	}
	return fmt.Sprintf("%s (plan usage: %s)", strings.TrimRight(message, " ."), summary)
}
