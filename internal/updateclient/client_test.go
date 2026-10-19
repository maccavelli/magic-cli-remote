package updateclient

import (
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
)

func TestNewRequiresProduct(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected an error without a product")
	}
}

func TestNewBuildsStandaloneAndManaged(t *testing.T) {
	for _, managed := range []bool{false, true} {
		up, err := New(Options{Product: "mcremote", Identity: buildinfo.Info{Version: "v0.15.3", Kind: buildinfo.KindRelease}, Managed: managed})
		if err != nil {
			t.Fatalf("managed=%v: New = %v", managed, err)
		}
		if up == nil {
			t.Fatalf("managed=%v: nil updater", managed)
		}
	}
}

// TestNewCommandFlagSurface: the command carries the library's canonical
// flags (MADR 0054 S3, draft amendment 2).
func TestNewCommandFlagSurface(t *testing.T) {
	cmd := NewCommand("mcremote")
	for _, name := range []string{"check", "yes", "force", "dry-run", "json", "version", "channel"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	if f := cmd.Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
		t.Error("missing -y shorthand for --yes")
	}
}

// TestNewCommandRejectsPositionalArgs keeps a typo from being read as an
// argument the command silently ignores.
func TestNewCommandRejectsPositionalArgs(t *testing.T) {
	cmd := NewCommand("mcremote")
	if err := cmd.Args(cmd, []string{"stray"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}
