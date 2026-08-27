package credstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSetGooseKeyringDisabledReplacesInPlace proves the rest of the operator's
// file survives untouched. They edit this file by hand, so a YAML round-trip
// that reordered keys or dropped comments would be a real loss.
func TestSetGooseKeyringDisabledReplacesInPlace(t *testing.T) {
	before := "# my notes\nGOOSE_DISABLE_KEYRING: false  " + GooseKeyringMarker + "\n" +
		"active_provider: openrouter\n\n# trailing note\nGOOSE_MODE: auto\n"
	path := writeCfg(t, before)

	changed, err := SetGooseKeyringDisabled(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("wroteChange = false, want true")
	}
	got := read(t, path)
	for _, keep := range []string{"# my notes", "active_provider: openrouter", "# trailing note", "GOOSE_MODE: auto"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("lost %q from the file:\n%s", keep, got)
		}
	}
	if strings.Count(got, "GOOSE_DISABLE_KEYRING") != 1 {
		t.Fatalf("key appears more than once:\n%s", got)
	}
	if !strings.Contains(got, "GOOSE_DISABLE_KEYRING: true") {
		t.Fatalf("value not updated:\n%s", got)
	}
}

// TestSetGooseKeyringDisabledPrependsWhenAbsent proves a new key cannot land
// inside another block's indented body.
func TestSetGooseKeyringDisabledPrependsWhenAbsent(t *testing.T) {
	path := writeCfg(t, "extensions:\n  developer:\n    enabled: true\n")
	if _, err := SetGooseKeyringDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.HasPrefix(got, "GOOSE_DISABLE_KEYRING: true") {
		t.Fatalf("key was not prepended:\n%s", got)
	}
	if !strings.Contains(got, "  developer:") {
		t.Fatalf("existing block damaged:\n%s", got)
	}
}

// TestSetGooseKeyringDisabledIgnoresIndentedKey proves a same-named key nested
// under another mapping is not mistaken for the top-level flag, matching how
// GooseKeyringDisabled already refuses nested keys.
func TestSetGooseKeyringDisabledIgnoresIndentedKey(t *testing.T) {
	path := writeCfg(t, "extensions:\n  weird:\n    GOOSE_DISABLE_KEYRING: true\n")
	if _, err := SetGooseKeyringDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.HasPrefix(got, "GOOSE_DISABLE_KEYRING: true") {
		t.Fatalf("top-level key not added:\n%s", got)
	}
	if !strings.Contains(got, "    GOOSE_DISABLE_KEYRING: true") {
		t.Fatalf("indented key was disturbed:\n%s", got)
	}
}

func TestSetGooseKeyringDisabledCreatesMissingFile(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := SetGooseKeyringDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "GOOSE_DISABLE_KEYRING: true") {
		t.Fatalf("file contents = %q", got)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %04o, want 0600", fi.Mode().Perm())
	}
}

// TestSetGooseKeyringDisabledIsNoOpWhenUnchanged proves a daemon restart does
// not churn a stable file.
func TestSetGooseKeyringDisabledIsNoOpWhenUnchanged(t *testing.T) {
	path := writeCfg(t, "GOOSE_DISABLE_KEYRING: true  "+GooseKeyringMarker+"\nactive_provider: x\n")
	before := read(t, path)

	changed, err := SetGooseKeyringDisabled(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("wroteChange = true for an already-correct file")
	}
	if read(t, path) != before {
		t.Fatal("file was rewritten despite matching")
	}
}

// TestSetGooseKeyringDisabledRemovesMarkedLine proves toggling off restores the
// file rather than leaving a residual `false`.
func TestSetGooseKeyringDisabledRemovesMarkedLine(t *testing.T) {
	original := "# notes\nactive_provider: openrouter\n"
	path := writeCfg(t, original)
	if _, err := SetGooseKeyringDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	if read(t, path) == original {
		t.Fatal("setup failed: key was not added")
	}

	changed, err := SetGooseKeyringDisabled(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("wroteChange = false, want true")
	}
	if got := read(t, path); got != original {
		t.Fatalf("round trip left residue:\nwant %q\ngot  %q", original, got)
	}
}

// TestSetGooseKeyringDisabledRefusesUnmarkedLine is the D10 gate: a setting the
// operator chose by hand is never silently deleted.
func TestSetGooseKeyringDisabledRefusesUnmarkedLine(t *testing.T) {
	original := "GOOSE_DISABLE_KEYRING: true\nactive_provider: x\n"
	path := writeCfg(t, original)

	_, err := SetGooseKeyringDisabled(path, false)
	if !errors.Is(err, ErrGooseKeyringOperatorOwned) {
		t.Fatalf("err = %v, want ErrGooseKeyringOperatorOwned", err)
	}
	if read(t, path) != original {
		t.Fatal("an operator-owned line was modified")
	}
}

// TestSetGooseKeyringDisabledMarkerSurvivesRead proves the marker comment does
// not corrupt the value for either reader. Goose strips it as YAML; mcremote
// strips it in splitYAMLScalar. If that stopped being true, selective removal
// would silently stop working.
func TestSetGooseKeyringDisabledMarkerSurvivesRead(t *testing.T) {
	path := writeCfg(t, "active_provider: x\n")
	if _, err := SetGooseKeyringDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	if !GooseKeyringDisabled(path) {
		t.Fatalf("mcremote could not read back its own marked line:\n%s", read(t, path))
	}
}

// TestSetGooseKeyringDisabledWritesGooseReadableLiterals checks the written
// value against a local reimplementation of Goose's own rule
// (base.rs:299-301), so a later edit cannot write something Goose reads as the
// opposite of what was intended.
func TestSetGooseKeyringDisabledWritesGooseReadableLiterals(t *testing.T) {
	// Goose: disabled only for bool true, "true", or "1".
	gooseReadsDisabled := func(v string) bool { return v == "true" || v == "1" }

	path := writeCfg(t, "active_provider: x\n")
	if _, err := SetGooseKeyringDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, l := range strings.Split(read(t, path), "\n") {
		if strings.HasPrefix(l, "GOOSE_DISABLE_KEYRING") {
			line = l
			break
		}
	}
	_, value, ok := splitYAMLScalar(strings.TrimSpace(line))
	if !ok {
		t.Fatalf("could not parse written line %q", line)
	}
	if !gooseReadsDisabled(value) {
		t.Fatalf("wrote %q, which goose reads as keyring-ENABLED", value)
	}
}
