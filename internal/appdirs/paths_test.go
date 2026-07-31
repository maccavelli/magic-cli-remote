package appdirs

import (
	"path/filepath"
	"strings"
	"testing"
)

func testRoots(t *testing.T) Roots {
	t.Helper()
	home := t.TempDir()
	return Roots{
		Home:        home,
		ConfigHome:  filepath.Join(home, ".config"),
		DataHome:    filepath.Join(home, ".local", "share"),
		StateHome:   filepath.Join(home, ".local", "state"),
		CacheHome:   filepath.Join(home, ".cache"),
		RuntimeHome: filepath.Join(home, "run"),
		Temp:        filepath.Join(home, "tmp"),
		Logs:        filepath.Join(home, "Library", "Logs"),
	}
}

func TestResolveDefaults(t *testing.T) {
	r := testRoots(t)
	p, err := Resolve(ProductMcremote, r, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigDir != filepath.Join(r.ConfigHome, "mcremote") {
		t.Errorf("ConfigDir = %q", p.ConfigDir)
	}
	if p.ConfigFile != filepath.Join(p.ConfigDir, "config.yaml") {
		t.Errorf("ConfigFile = %q", p.ConfigFile)
	}
	if p.DataDir != filepath.Join(r.DataHome, "mcremote") {
		t.Errorf("DataDir = %q", p.DataDir)
	}
	if p.StateDir != filepath.Join(r.StateHome, "mcremote") {
		t.Errorf("StateDir = %q", p.StateDir)
	}
	if p.CacheDir != filepath.Join(r.CacheHome, "mcremote") {
		t.Errorf("CacheDir = %q", p.CacheDir)
	}
	if p.RuntimeBase != filepath.Join(r.RuntimeHome, "mcremote") {
		t.Errorf("RuntimeBase = %q", p.RuntimeBase)
	}
	if p.InstanceKey == "" || len(p.InstanceKey) != 16 {
		t.Errorf("InstanceKey = %q", p.InstanceKey)
	}
	if !strings.HasPrefix(p.RuntimeDir, p.RuntimeBase+string(filepath.Separator)) {
		t.Errorf("RuntimeDir = %q", p.RuntimeDir)
	}
	if p.AdminSocket != filepath.Join(p.RuntimeDir, "admin.sock") {
		t.Errorf("AdminSocket = %q", p.AdminSocket)
	}
	if !strings.Contains(p.EngineRegistryDir, p.InstanceKey) {
		t.Errorf("EngineRegistryDir = %q", p.EngineRegistryDir)
	}
}

func TestResolveDataDirOverride(t *testing.T) {
	r := testRoots(t)
	custom := filepath.Join(r.Home, "custom-data")
	p, err := Resolve(ProductMcremote, r, custom)
	if err != nil {
		t.Fatal(err)
	}
	if p.DataDir != custom {
		t.Errorf("DataDir = %q want %q", p.DataDir, custom)
	}
	key, err := InstanceKey(custom)
	if err != nil {
		t.Fatal(err)
	}
	if p.InstanceKey != key {
		t.Errorf("InstanceKey = %q want %q", p.InstanceKey, key)
	}
	p2, err := WithDataDir(p, filepath.Join(r.Home, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if p2.InstanceKey == p.InstanceKey {
		t.Error("expected different instance key for different data dir")
	}
}

func TestResolveRejectsRelativeOverride(t *testing.T) {
	r := testRoots(t)
	if _, err := Resolve(ProductMcremote, r, "relative"); err == nil {
		t.Fatal("expected error for relative data dir")
	}
}

func TestInstanceKeyStable(t *testing.T) {
	dir := "/var/lib/mcremote"
	a, err := InstanceKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := InstanceKey(dir + "/")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("InstanceKey not stable under Clean: %q vs %q", a, b)
	}
}

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

func TestProductByName(t *testing.T) {
	p, ok := ProductByName("mcremote")
	if !ok || p.Name != "mcremote" {
		t.Fatalf("ProductByName mcremote: %+v %v", p, ok)
	}
	if _, ok := ProductByName("nope"); ok {
		t.Fatal("expected unknown product")
	}
}
