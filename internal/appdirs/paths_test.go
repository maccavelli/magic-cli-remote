package appdirs

import (
	"path/filepath"
	"runtime"
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

// TestResolveDoesNotDoubleJoinProductLeaf pins the joinProduct behaviour that
// lets a platform whose roots are already product-scoped (Windows Known
// Folders, MADR 0116 D3) share one pure Resolve with XDG. It runs on every
// host precisely because Roots is injected.
func TestResolveDoesNotDoubleJoinProductLeaf(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, "Local", "mcremote")
	r := Roots{
		Home:          home,
		ConfigHome:    filepath.Join(home, "Roaming"),
		DataHome:      filepath.Join(home, "Local"),
		StateHome:     filepath.Join(base, "State"),
		CacheHome:     filepath.Join(base, "Cache"),
		RuntimeHome:   filepath.Join(base, "Runtime"),
		Temp:          filepath.Join(home, "tmp"),
		ProductScoped: true,
	}
	p, err := Resolve(ProductMcremote, r, "")
	if err != nil {
		t.Fatal(err)
	}
	// Roots that do NOT end in the product name still get the leaf joined.
	if want := filepath.Join(r.ConfigHome, "mcremote"); p.ConfigDir != want {
		t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, want)
	}
	if want := filepath.Join(r.DataHome, "mcremote"); p.DataDir != want {
		t.Errorf("DataDir = %q, want %q", p.DataDir, want)
	}
	// Roots that are already product-scoped must NOT be joined again.
	if p.StateDir != r.StateHome {
		t.Errorf("StateDir = %q, want %q (no second leaf)", p.StateDir, r.StateHome)
	}
	if p.CacheDir != r.CacheHome {
		t.Errorf("CacheDir = %q, want %q (no second leaf)", p.CacheDir, r.CacheHome)
	}
	if p.RuntimeBase != r.RuntimeHome {
		t.Errorf("RuntimeBase = %q, want %q (no second leaf)", p.RuntimeBase, r.RuntimeHome)
	}
}

// TestJoinProductHonoursProductScoped is the other half of the contract: the
// platform declares the shape, Resolve does not infer it from the path (MADR
// 0116 D3 amendment).
func TestJoinProductHonoursProductScoped(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"tmp", "state")
	unscoped := Roots{}
	if got, want := unscoped.joinProduct(root, "mcremote"), filepath.Join(root, "mcremote"); got != want {
		t.Errorf("unscoped joinProduct(%q) = %q, want %q", root, got, want)
	}
	scoped := Roots{ProductScoped: true}
	if got := scoped.joinProduct(root, "mcremote"); got != root {
		t.Errorf("scoped joinProduct(%q) = %q, want unchanged", root, got)
	}
	// The ancestor case the Base-only predicate got wrong.
	deep := filepath.Join(root, "mcremote", "State")
	if got := scoped.joinProduct(deep, "mcremote"); got != deep {
		t.Errorf("scoped joinProduct(%q) = %q, want unchanged", deep, got)
	}
}

// TestInstanceKeyCaseFolding pins MADR 0116 D3: NTFS is case-insensitive, so
// two spellings of one directory must key one instance. On Unix, case is
// significant and the keys must differ.
func TestInstanceKeyCaseFolding(t *testing.T) {
	upper, err := InstanceKey(filepath.Join(string(filepath.Separator)+"Users", "X", "Data"))
	if err != nil {
		t.Fatal(err)
	}
	lower, err := InstanceKey(filepath.Join(string(filepath.Separator)+"users", "x", "data"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if upper != lower {
			t.Errorf("case-insensitive filesystem: keys differ (%q vs %q)", upper, lower)
		}
		return
	}
	if upper == lower {
		t.Errorf("case-sensitive filesystem: keys collide (%q)", upper)
	}
}

// TestResolveLeavesLogDirEmptyWithoutLogsRoot proves Paths.LogDir follows the
// root rather than being synthesised, which is what lets Linux have none
// (MADR 0116 F4).
func TestResolveLeavesLogDirEmptyWithoutLogsRoot(t *testing.T) {
	r := testRoots(t)
	r.Logs = ""
	p, err := Resolve(ProductMcremote, r, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.LogDir != "" {
		t.Errorf("LogDir = %q, want empty", p.LogDir)
	}
}
