package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// probeSection is a providers.probe section with one key of each shape a
// provider's section has: a scalar, a table of further keys, and a
// config-file-only list.
type probeSection struct {
	Enabled bool   `mapstructure:"enabled"`
	Bin     string `mapstructure:"bin"`
	Limits  struct {
		Max int `mapstructure:"max"`
	} `mapstructure:"limits"`
	Roots []string `mapstructure:"roots" env:"-"`
}

func (s *probeSection) IsEnabled() bool { return s.Enabled }

func (s *probeSection) Validate() error {
	if s.Limits.Max < 0 {
		return errors.New("limits.max must be 0 or more")
	}
	return nil
}

var probeSpecs = []config.ProviderSectionSpec{{
	ID:  "probe",
	New: func() config.ProviderSection { return &probeSection{Bin: "probe-bin"} },
}}

// loadProbe loads body as the config file, with the probe section registered.
func loadProbe(t *testing.T, body string) (config.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.Load(config.LoadOptions{ConfigFile: path, Providers: probeSpecs})
}

// TestASectionLoadsFromItsDefaultsFileAndEnv is 0208 PLAN P8: a provider's
// section is decoded from its own defaults, the file and its variables, with
// no key of it listed in this package.
func TestASectionLoadsFromItsDefaultsFileAndEnv(t *testing.T) {
	isolateConfig(t)
	t.Setenv("MCREMOTE_PROVIDERS_PROBE_ENABLED", "true")
	t.Setenv("MCREMOTE_PROVIDERS_PROBE_LIMITS_MAX", "7")
	cfg, err := loadProbe(t, "providers:\n  probe:\n    roots: [/a, /b]\n")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := cfg.Providers.Sections["probe"].(*probeSection)
	if !ok {
		t.Fatalf("sections = %#v, want a *probeSection under probe", cfg.Providers.Sections)
	}
	if !got.Enabled || got.Bin != "probe-bin" || got.Limits.Max != 7 || !slices.Equal(got.Roots, []string{"/a", "/b"}) {
		t.Errorf("section = %+v; want enabled and max 7 from env, bin from defaults, roots from the file", got)
	}
}

// TestASectionRefusesWhatItDoesNotHave is 0208 MADR D6 as amended on
// 2026-11-04: an unknown key, in the file or in a variable, fails the load and
// is named, instead of being ignored.
func TestASectionRefusesWhatItDoesNotHave(t *testing.T) {
	cases := []struct {
		name string
		body string
		env  [2]string
		want string
	}{
		{"a key the section lacks", "providers:\n  probe:\n    bogus: 1\n", [2]string{},
			"providers.probe has no key bogus"},
		{"a key in a table the section lacks", "providers:\n  probe:\n    limits:\n      typo: 1\n", [2]string{},
			"providers.probe has no key limits.typo"},
		{"a variable naming no key", "", [2]string{"MCREMOTE_PROVIDERS_PROBE_BOGUS", "1"},
			"MCREMOTE_PROVIDERS_PROBE_BOGUS names no providers.probe key"},
		{"a variable for a config-file-only key", "", [2]string{"MCREMOTE_PROVIDERS_PROBE_ROOTS", "/a"},
			"MCREMOTE_PROVIDERS_PROBE_ROOTS sets providers.probe.roots, which is config-file only"},
		{"the section's own validation", "providers:\n  probe:\n    limits:\n      max: -1\n", [2]string{},
			"providers.probe: limits.max must be 0 or more"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			if tc.env[0] != "" {
				t.Setenv(tc.env[0], tc.env[1])
			}
			_, err := loadProbe(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
