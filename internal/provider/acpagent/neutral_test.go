package acpagent

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// providerNames are the agents and vendor wire prefixes acpagent's production
// code must not name, in code or in comments (MADR 0208 D2, as amended
// 2026-11-01): each agent's code lives in its own package.
var providerNames = regexp.MustCompile(`(?i)grok|opencode|kilo|codex|gobble|x\.ai|xai`)

// TestNoProviderIsNamedInAcpagent is 0208 PLAN P6c: acpagent's non-test files,
// its subpackages included, name no provider. A hit is reported with its file,
// line and text, so the failure says what to fix.
func TestNoProviderIsNamedInAcpagent(t *testing.T) {
	files := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for n := 1; sc.Scan(); n++ {
			if m := providerNames.FindString(sc.Text()); m != "" {
				t.Errorf("%s:%d names %q: %s", path, n, m, strings.TrimSpace(sc.Text()))
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 10 {
		t.Fatalf("scanned only %d files; the walk is not seeing the package", files)
	}
}
