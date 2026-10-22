// Package output bounds an agent's text for an event without rewriting it.
//
// Every provider adapter clips the same way here, so what the phone receives
// does not depend on which agent produced it (MADR 0208 D11). Agent text keeps
// its line breaks, indentation and escape bytes; only its length is bounded.
package output

import (
	"strings"
	"unicode/utf8"
)

// MaxToolText bounds the text an adapter hands the daemon for one tool event,
// in bytes. The session manager keeps it whole for session.tool_output, so it
// must fit one WebSocket message (1 MiB) even if JSON escaping quadruples it
// (PLAN 0208 P2 Deviation 1).
const MaxToolText = 256 << 10

// MaxToolShown bounds what a tool event carries to the phone, in bytes. The
// session manager clips every provider's tool text to it in one place (MADR
// 0208 D11) and marks the event truncated; the phone fetches the rest. It is
// the cap opencode, kilo and the phone's expanded tool detail
// (kMaxExpandedDetailChars) already shared. It is not the phone's
// kMaxItemTextChars (100,000), because shipping text the phone never renders
// is waste (MADR 0034 D2).
const MaxToolShown = 8000

// Truncated ends text Clip has cut.
const Truncated = "\n…[truncated]"

// Clip bounds s to max bytes, keeping its line structure, and reports whether
// it cut. Trailing whitespace goes; nothing inside the text changes.
//
// The cut falls on the last line break at or before max, so no line is left
// half shown. When that break would discard more than half the budget (one
// very long line), the cut falls on the last rune boundary at or before max
// instead. Truncated is appended after the cut, beyond max.
func Clip(s string, max int) (string, bool) {
	s = strings.TrimRight(s, " \t\r\n")
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := strings.LastIndexByte(s[:max+1], '\n')
	if cut < max/2 {
		cut = max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
	}
	return strings.TrimRight(s[:cut], " \t\r\n") + Truncated, true
}

// Line is the one-line form of s, for a title, a row or a metadata field: its
// first non-empty line, trimmed, at most max bytes, cut on a rune boundary.
// It ends in "…" when it was cut or when more lines followed. Spacing inside
// the line is kept.
func Line(s string, max int) string {
	first, rest, _ := strings.Cut(strings.TrimLeft(s, " \t\r\n"), "\n")
	first = strings.TrimSpace(first)
	more := strings.TrimSpace(rest) != ""
	if max > 0 && len(first) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(first[cut]) {
			cut--
		}
		return strings.TrimRight(first[:cut], " \t") + "…"
	}
	if more {
		return first + "…"
	}
	return first
}
