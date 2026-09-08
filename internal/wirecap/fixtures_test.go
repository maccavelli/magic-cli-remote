package wirecap

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The committed wire fixtures are the artefact this package's redaction exists
// to protect, and every miss so far was found by looking at one rather than at
// the rule: the stripped form in MADR 0137 Phase 1, the host separator in
// 0144's amendment, JSON escaping in 0151. This test makes that a check.
//
// It guards the two shapes known to have reached a fixture, and it is honest
// about the rest: it cannot know what identifier an engine will send next
// (0151 F8), so a green run here means "neither known shape is present", not
// "this fixture carries nothing identifying".

// addressRE matches an email address rather than a bare "@".
//
// MADR 0151 open question 2 chose the strict bare-@ form, and executing this
// phase showed why that is wrong: grok's help text uses @ as file-reference
// syntax ("@src/main.rs", "@!.github/workflows"), which put ten @ on ten
// legitimate lines of the very fixture this guard exists for. A guard that
// fires on those gets weakened or deleted, which is worse than one that is
// narrower on purpose.
var addressRE = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// docDomains are reserved for documentation by RFC 2606, so an address in one
// is a deliberate placeholder rather than a leak. The scrubbed grok fixture
// uses example.com, and a guard that rejected its own placeholder would be
// unusable.
var docDomains = []string{"@example.com", "@example.org", "@example.net"}

// driveAbsolute matches a Windows absolute path in both the raw form and the
// JSON-escaped form that is what actually reaches a fixture (0151 F1). Any
// occurrence means a capture was taken on a Windows host and the home was not
// redacted — which is the exact defect 0151 P1 fixed, seen from the other end.
var driveAbsolute = regexp.MustCompile(`[A-Za-z]:\\\\?[A-Za-z]`)

func TestCommittedFixturesCarryNoIdentifiers(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "provider", "*", "testdata", "wire", "*", "frames.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// A glob that silently stops matching turns this guard into a test that
	// asserts nothing and still passes — the failure mode MADR 0147 D11 pins.
	// Five fixtures exist today; the floor is a floor, not a count, so adding a
	// provider does not break it.
	if len(paths) < 5 {
		t.Fatalf("glob matched %d fixtures, want at least 5: the guard is not reading what it claims to", len(paths))
	}

	for _, path := range paths {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			for i, line := range strings.Split(string(b), "\n") {
				for _, hit := range addressRE.FindAllString(line, -1) {
					if isDocAddress(hit) {
						continue
					}
					t.Errorf("line %d: email address %q — redaction covers the home directory only (MADR 0151 F8)", i+1, hit)
				}
				if m := driveAbsolute.FindString(line); m != "" {
					t.Errorf("line %d: unredacted Windows path starting %q (MADR 0151 F1)", i+1, m)
				}
			}
		})
	}
}

func isDocAddress(addr string) bool {
	lower := strings.ToLower(addr)
	for _, d := range docDomains {
		if strings.HasSuffix(lower, d) {
			return true
		}
	}
	return false
}
