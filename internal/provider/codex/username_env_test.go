package codex

import (
	"runtime"
	"strings"
	"testing"
)

// TestWithResolvedUserName is acceptance criterion A16 of PLAN 0163.
//
// codex 0.149.1's Windows sandbox derived the ACL grant principal from
// %USERNAME%, falling back to the literal "Administrators" when the variable was
// absent — and a relay-spawned app-server is exactly the filtered-environment
// launcher that hit that fallback (MADR 0163 F28). 0.155.1 reads the OS token
// instead, so this is defence in depth against a downgrade.
func TestWithResolvedUserName(t *testing.T) {
	if runtime.GOOS != "windows" {
		// The helper is a no-op elsewhere, and that is the assertion.
		if got := withResolvedUserName([]string{"PATH=/usr/bin"}); len(got) != 1 {
			t.Errorf("non-Windows env was modified: %v", got)
		}
		return
	}

	userName := func(env []string) (string, bool) {
		for _, entry := range env {
			if name, value, ok := strings.Cut(entry, "="); ok && strings.EqualFold(name, "USERNAME") {
				return value, true
			}
		}
		return "", false
	}

	// Absent: added, and never as a bare domain-qualified name, because the
	// sandbox wants the account name %USERNAME% normally holds.
	got, ok := userName(withResolvedUserName([]string{`PATH=C:\Windows`}))
	if !ok || got == "" {
		t.Fatalf("USERNAME was not added to an environment that lacked it")
	}
	if strings.Contains(got, `\`) {
		t.Errorf("USERNAME = %q, want the bare account name without a domain prefix", got)
	}

	// Present and non-empty: left exactly as the caller set it.
	got, ok = userName(withResolvedUserName([]string{"USERNAME=someone-else"}))
	if !ok || got != "someone-else" {
		t.Errorf("USERNAME = %q, want the caller's value preserved", got)
	}

	// Present but empty is the case that actually bit: Codex's own fallback
	// triggers on an unset variable, and an empty one is just as useless.
	//
	// It must be REPLACED, not shadowed by a second entry. Windows builds the
	// child's environment block from this slice, and which of two entries with the
	// same name wins is not something to rely on — the first implementation
	// appended, and this assertion is what caught it.
	filled := withResolvedUserName([]string{"PATH=x", "USERNAME=", "TEMP=y"})
	got, ok = userName(filled)
	if !ok || got == "" {
		t.Errorf("an empty USERNAME was left empty (%q); it must be filled", got)
	}
	if n := countUserName(filled); n != 1 {
		t.Errorf("USERNAME appears %d times in %v; want exactly one entry", n, filled)
	}
	if len(filled) != 3 {
		t.Errorf("env grew to %d entries (%v); replacing must not add one", len(filled), filled)
	}
}

func countUserName(env []string) int {
	n := 0
	for _, entry := range env {
		if name, _, ok := strings.Cut(entry, "="); ok && strings.EqualFold(name, "USERNAME") {
			n++
		}
	}
	return n
}
