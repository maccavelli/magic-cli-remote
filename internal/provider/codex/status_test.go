package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNoInProgressStatusInProductionCode guards the v1 vocabulary leak that
// MADR 0035 D2 fixes. The mobile reducer (chat_models.dart) keys off
// `toolStatus == 'running' || 'pending'` for the spinner; the v1 status
// `in_progress` would never trigger the spinner and would also be silently
// elided from the "still working" group on the mobile side.
func TestNoInProgressStatusInProductionCode(t *testing.T) {
	// items.go is the one legitimate home for the wire-format string
	// "inProgress" (it maps to "running" via codexToolStatus). All other
	// production files must speak only the daemon vocabulary.
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// items.go is the one legitimate home for the daemon's
		// "in_progress" return value (it is the codex v2 → daemon
		// mapping table). All other production files must speak only the
		// wire-format side of codexToolStatus.
		if name == "items.go" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"in_progress"`) {
			t.Errorf("%s: stray \"in_progress\" status (D2 violation)", name)
		}
	}
}

// TestToolStatusVocabulary pins the daemon contract: tool events use only
// pending | running | completed | failed. The codex-specific mapping
// happens in codexToolStatus; the emitted events themselves use the daemon
// vocabulary only.
func TestToolStatusVocabulary(t *testing.T) {
	allowed := map[string]bool{
		"pending":   true,
		"running":   true,
		"completed": true,
		"failed":    true,
	}
	// Each input is one the engine actually emits (or a pass-through
	// default). Unknown inputs intentionally fall through as-is — the
	// contract is "do not invent a fake status"; the reducer just
	// treats anything that is not running/pending as terminal.
	for _, in := range []string{"inProgress", "running", "completed", "failed", "declined", "pending"} {
		out := codexToolStatus(in)
		if !allowed[out] {
			t.Errorf("codexToolStatus(%q) = %q, not in daemon vocabulary", in, out)
		}
	}
}
