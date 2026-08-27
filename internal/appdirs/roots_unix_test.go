//go:build unix

// XDG resolution is a Unix layout by definition: Windows uses Known Folders
// (MADR 0116 D3), covered by roots_windows_test.go.

package appdirs

import (
	"path/filepath"
	"testing"
)

func TestSystemRootsRelativeXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	roots, diags, err := SystemRoots(ProductMcremote)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range diags {
		if d.Code == "xdg_relative_ignored" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected relative diagnostic, got %#v", diags)
	}
	if roots.ConfigHome != filepath.Join(home, ".config") {
		t.Errorf("ConfigHome = %q, want fallback", roots.ConfigHome)
	}
}

func TestSystemRootsAbsoluteXDG(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, "cfg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "rt"))

	roots, diags, err := SystemRoots(ProductMcrelay)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Code == "xdg_relative_ignored" {
			t.Errorf("unexpected diag: %+v", d)
		}
	}
	if roots.ConfigHome != cfg {
		t.Errorf("ConfigHome = %q", roots.ConfigHome)
	}
	p, err := Resolve(ProductMcrelay, roots, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigDir != filepath.Join(cfg, "mcrelay") {
		t.Errorf("ConfigDir = %q", p.ConfigDir)
	}
}
