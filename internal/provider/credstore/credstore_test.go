package credstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// MADR 0074 D2: the reader drops key material at the parse boundary, so no
// caller can leak what it never received. This is the guard against a
// convenience refactor that returns the whole decoded object.
func TestReadJSONAuthNeverReturnsKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	const secret = "sk-live-DO-NOT-LEAK-9999"
	body := `{
	  "opencode":    {"type":"api","key":"` + secret + `"},
	  "opencode-go": {"type":"api","key":"sk-another-secret"},
	  "kilo":        {"type":"oauth","refresh":"rt-secret","access":"at-secret"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := credstore.ReadJSONAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(entries), entries)
	}
	blob, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{secret, "sk-another-secret", "rt-secret", "at-secret", "sk-"} {
		if strings.Contains(string(blob), bad) {
			t.Fatalf("entries carried %q: %s", bad, blob)
		}
	}
	// Sorted, so the phone's list does not reshuffle between polls.
	if entries[0].ID != "kilo" || entries[1].ID != "opencode" || entries[2].ID != "opencode-go" {
		t.Fatalf("entries not sorted by id: %+v", entries)
	}
	if entries[0].Type != "oauth" || entries[1].Type != "api" {
		t.Fatalf("types not preserved: %+v", entries)
	}
}

// A cold host has no store at all. That is a normal state — no credentials —
// and must not surface as an error that degrades the whole provider listing.
func TestReadJSONAuthMissingFileIsNotAnError(t *testing.T) {
	entries, err := credstore.ReadJSONAuth(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing store should be empty, not an error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %+v, want none", entries)
	}
}

func TestReadJSONAuthMalformedIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := credstore.ReadJSONAuth(path); err == nil {
		t.Fatal("malformed store parsed without error")
	}
}

// Paths must follow the agents' own XDG conventions, not mcremote's layout,
// or a host with XDG_DATA_HOME set would silently read the wrong store.
func TestStorePathsRespectXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	oc, err := credstore.OpenCodeAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "share", "opencode", "auth.json"); oc != want {
		t.Errorf("opencode path = %s, want %s", oc, want)
	}

	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "custom-data"))
	oc2, err := credstore.OpenCodeAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "custom-data", "opencode", "auth.json"); oc2 != want {
		t.Errorf("XDG_DATA_HOME ignored: got %s, want %s", oc2, want)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	if credstore.FileExists(filepath.Join(dir, "nope")) {
		t.Error("reported a missing file as present")
	}
	p := filepath.Join(dir, "yes")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !credstore.FileExists(p) {
		t.Error("reported a present file as missing")
	}
	if credstore.FileExists(dir) {
		t.Error("a directory is not a credential file")
	}
}
