package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// isolateConfig makes Load(LoadOptions{}) hermetic (MADR 0162 D2): every
// platform root points into a fresh temp dir, and every MCREMOTE_* variable
// inherited from the host is cleared. Without it such a Load reads the host's
// live config — on Windows through Known Folders, which ignore
// XDG_CONFIG_HOME — and a stray MCREMOTE_CONFIG overrides even that.
//
// Call it before the test's own t.Setenv lines, so they win. It returns the
// config directory Load searches, which does not exist until a test creates it.
func isolateConfig(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	roots := appdirs.Roots{
		Home:        d,
		ConfigHome:  filepath.Join(d, "config"),
		DataHome:    filepath.Join(d, "data"),
		StateHome:   filepath.Join(d, "state"),
		CacheHome:   filepath.Join(d, "cache"),
		RuntimeHome: filepath.Join(d, "run"),
		Temp:        filepath.Join(d, "tmp"),
		Logs:        filepath.Join(d, "logs"),
	}
	config.SetSystemRootsForTest(t, func(appdirs.Product) (appdirs.Roots, []appdirs.Diagnostic, error) {
		return roots, nil, nil
	})
	for _, kv := range os.Environ() {
		// Viper treats an empty value as unset (AllowEmptyEnv is off), and
		// Load ignores an empty MCREMOTE_CONFIG.
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "MCREMOTE_") {
			t.Setenv(name, "")
		}
	}
	dir := filepath.Join(d, "config", "mcremote")
	paths, err := appdirs.Resolve(appdirs.ProductMcremote, roots, "")
	if err != nil {
		t.Fatalf("resolve isolated roots: %v", err)
	}
	if paths.ConfigDir != dir {
		t.Fatalf("isolated ConfigDir = %q, want %q: the appdirs layout changed", paths.ConfigDir, dir)
	}
	return dir
}

// TestIsolatedLoadReadsOnlyTheIsolatedConfig proves the seam is wired (MADR
// 0162 D4): if Load stopped resolving roots through systemRoots, it would read
// the host's config instead of this one and fail here, rather than every other
// test quietly depending on the host again.
func TestIsolatedLoadReadsOnlyTheIsolatedConfig(t *testing.T) {
	dir := isolateConfig(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(want, []byte("display_name: isolated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisplayName != "isolated" {
		t.Fatalf("DisplayName = %q, want %q: Load did not read the isolated config", cfg.DisplayName, "isolated")
	}
	if cfg.ConfigFile != want {
		t.Fatalf("ConfigFile = %q, want %q", cfg.ConfigFile, want)
	}
}

// TestIsolatedLoadWithNoFileUsesDefaults is the empty case: an isolated Load
// finds no file and reports none.
func TestIsolatedLoadWithNoFileUsesDefaults(t *testing.T) {
	isolateConfig(t)
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigFile != "" {
		t.Fatalf("ConfigFile = %q, want none", cfg.ConfigFile)
	}
	if cfg.DisplayName != "" {
		t.Fatalf("DisplayName = %q, want empty", cfg.DisplayName)
	}
}
