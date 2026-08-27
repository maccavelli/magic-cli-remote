package appdirs

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSystemRootsDispatches proves the per-platform split introduced in MADR
// 0116 D3 produces a usable layout on whatever host runs the test, and that
// Logs is populated only where the platform has a service stdio base.
func TestSystemRootsDispatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by roots_windows_test.go with the Known Folder seam")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(k, "")
	}

	r, _, err := SystemRoots(ProductMcremote)
	if err != nil {
		t.Fatal(err)
	}
	if r.ConfigHome != filepath.Join(home, ".config") {
		t.Errorf("ConfigHome = %q", r.ConfigHome)
	}
	if r.ProductScoped {
		t.Error("XDG roots must not be product-scoped")
	}
	switch runtime.GOOS {
	case "darwin":
		if r.Logs == "" {
			t.Error("Logs is empty on darwin, where LaunchAgent stdio lives")
		}
	default:
		if r.Logs != "" {
			t.Errorf("Logs = %q; only darwin has a stdio base (MADR 0116 F4)", r.Logs)
		}
	}
}

// TestSystemPaths covers the convenience wrapper, which had no test.
func TestSystemPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Known Folder lookup is covered in roots_windows_test.go")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	p, _, err := SystemPaths(ProductMcremote, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.ConfigDir, "mcremote") {
		t.Errorf("ConfigDir = %q", p.ConfigDir)
	}
	if p.InstanceKey == "" {
		t.Error("InstanceKey is empty")
	}

	// An absolute override must be honoured and must re-key the instance.
	override := filepath.Join(home, "custom")
	p2, _, err := SystemPaths(ProductMcremote, override)
	if err != nil {
		t.Fatal(err)
	}
	if p2.DataDir != override {
		t.Errorf("DataDir = %q, want %q", p2.DataDir, override)
	}
	if p2.InstanceKey == p.InstanceKey {
		t.Error("instance key did not change with the data dir")
	}

	// A relative override must be refused, not silently resolved.
	if _, _, err := SystemPaths(ProductMcremote, "relative"); err == nil {
		t.Error("SystemPaths accepted a relative data dir override")
	}
}

// TestDefaultConfigFile covers the other wrapper.
func TestDefaultConfigFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Known Folder lookup is covered in roots_windows_test.go")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, _, err := DefaultConfigFile(ProductMcrelay)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "mcrelay", "config.yaml")
	if got != want {
		t.Errorf("DefaultConfigFile = %q, want %q", got, want)
	}
	if _, _, err := DefaultConfigFile(Product{}); err == nil {
		t.Error("DefaultConfigFile accepted an empty product")
	}
}
