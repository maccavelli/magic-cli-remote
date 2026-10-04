package protocol_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// protocolDoc is the wire contract these guards check against.
const protocolDoc = "../../docs/guides/protocol-v1.md"

// eventTypeListPrefix marks the canonical enumeration a client implements
// against. The enumeration is the paragraph that starts with this prefix and
// runs to the next blank line, so the list can be reflowed without breaking
// the guard (MADR 0180 D8).
const eventTypeListPrefix = "Event `type` values:"

// readProtocolDoc loads the spec relative to this package so the guards work
// from any working directory.
func readProtocolDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(protocolDoc))
	if err != nil {
		t.Fatalf("read %s: %v — these guards are pinned to the protocol spec; "+
			"if it moved, update protocolDoc", protocolDoc, err)
	}
	return string(b)
}

// TestEventTypesAreDocumented asserts every event.Type the daemon can emit
// appears in the spec's canonical event-type enumeration (MADR 0036 D6).
//
// This is the guard for the failure that motivated it: `session_title` was
// emitted live, was a control event that must not be dropped, and had zero
// mentions in the spec. Nothing caught it.
//
// Scope: this proves *presence*, not correctness. A type listed with a wrong
// description still passes. That is the honest limit of a mechanical check —
// it cannot replace reading the spec against the code.
func TestEventTypesAreDocumented(t *testing.T) {
	doc := readProtocolDoc(t)

	var paragraph []string
	for line := range strings.SplitSeq(doc, "\n") {
		if paragraph == nil {
			if strings.HasPrefix(strings.TrimSpace(line), eventTypeListPrefix) {
				paragraph = []string{line}
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		paragraph = append(paragraph, line)
	}
	if paragraph == nil {
		t.Fatalf("no line starting %q in %s — the enumeration this guard checks "+
			"is gone or was reworded", eventTypeListPrefix, protocolDoc)
	}
	list := strings.Join(paragraph, "\n")

	for _, typ := range event.Types() {
		if !strings.Contains(list, "`"+string(typ)+"`") {
			t.Errorf("event type %q is emitted but missing from the %q paragraph in %s",
				typ, eventTypeListPrefix, protocolDoc)
		}
	}
}

// TestErrorCodesAreDocumented asserts every registered protocol error code is
// mentioned somewhere in the spec (MADR 0036 D6).
//
// Paired with TestWSErrorCodesAreRegistered in internal/ws: that one proves the
// registry covers what the server emits, this one proves the spec covers the
// registry. Together they close the loop from emit site to documentation.
func TestErrorCodesAreDocumented(t *testing.T) {
	doc := readProtocolDoc(t)
	for _, code := range protocol.ErrorCodes() {
		if !strings.Contains(doc, code) {
			t.Errorf("error code %q is registered but not documented in %s",
				code, protocolDoc)
		}
	}
}
