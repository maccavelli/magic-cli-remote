package kilo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0088 D7–D11: the 7.4.22 pin does not grow a worktree/sandbox/PTY client,
// does not call allow-everything, and does not treat session.next.text.delta
// as assistant transcript. A later MADR can lift these; this file is the
// tripwire so they cannot land inside the pin unnoticed.
func TestPinDoesNotTakeDeferredSurfaces(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		if strings.Contains(src, "/permission/allow-everything") {
			t.Errorf("%s calls allow-everything (0088 D10)", name)
		}
		if strings.Contains(src, "/experimental/worktree") {
			t.Errorf("%s talks to experimental/worktree (0088 D7)", name)
		}
		if strings.Contains(src, "sandbox/toggle") {
			t.Errorf("%s toggles sandbox (0088 D7)", name)
		}
		if strings.Contains(src, "/pty") {
			t.Errorf("%s uses /pty (0088 D8)", name)
		}
		// A comment may name the event; a case arm would start mapping it.
		if strings.Contains(src, `case "session.next.text.delta"`) {
			t.Errorf("%s handles session.next.text.delta (0088 D3)", name)
		}
	}
}
