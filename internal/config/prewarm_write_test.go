package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

const prewarmFixture = `# keep this comment
listen:
  port: 7531
providers:
  kilo:
    enabled: true
    # sibling comment
    bin: "kilo"
    prewarm: true
  grok:
    enabled: true
    prewarm: false
`

func TestSetProviderPrewarmFileFlipsAndKeepsComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(prewarmFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetProviderPrewarmFile(path, "kilo", false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "keep this comment") {
		t.Fatalf("lost file comment:\n%s", got)
	}
	if !strings.Contains(got, "sibling comment") {
		t.Fatalf("lost sibling comment:\n%s", got)
	}
	if !strings.Contains(got, "bin:") {
		t.Fatalf("lost sibling key:\n%s", got)
	}
	kilo, grok := mustLoadPrewarm(t, raw)
	if kilo {
		t.Fatal("kilo prewarm still true")
	}
	if grok {
		t.Fatal("grok prewarm changed")
	}
}

func TestSetProviderPrewarmFileInsertsMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	src := "providers:\n  kilo:\n    enabled: true\n  grok:\n    prewarm: true\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetProviderPrewarmFile(path, "kilo", true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	kilo, grok := mustLoadPrewarm(t, raw)
	if !kilo {
		t.Fatalf("kilo prewarm not inserted:\n%s", raw)
	}
	if !grok {
		t.Fatal("grok prewarm changed")
	}
}

// A toggle must never conjure a config file: a daemon running on defaults has
// nothing to edit, and writing one here would materialise a file the operator
// never asked for (and that later `serve` runs would then honour).
func TestSetProviderPrewarmFileMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	err := SetProviderPrewarmFile(path, "kilo", true)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("a failed write created %s", path)
	}
}

// The no-file-path case: `serve` booted without a config file, so ConfigFile is
// empty. internal/ws classifies this by sentinel to answer config_write_failed.
func TestSetProviderPrewarmFileEmptyPath(t *testing.T) {
	if err := SetProviderPrewarmFile("", "kilo", true); !errors.Is(err, ErrNoConfigFile) {
		t.Fatalf("err = %v, want ErrNoConfigFile", err)
	}
}

func TestSetProviderPrewarmFileEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetProviderPrewarmFile(path, "kilo", true); !errors.Is(err, ErrNoConfigFile) {
		t.Fatalf("err = %v, want ErrNoConfigFile", err)
	}
}

func TestSetProviderPrewarmFileUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(prewarmFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SetProviderPrewarmFile(path, "nope", true)
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
}

// The phone's prewarm toggle reaches gobble's section (MADR 0179 D11, PLAN P4
// step 2).
func TestSetProviderPrewarmFileGobble(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(prewarmFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetProviderPrewarmFile(path, "gobble", true); err != nil {
		t.Fatalf("SetProviderPrewarmFile(gobble): %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Providers struct {
			Gobble struct {
				Prewarm bool `yaml:"prewarm"`
			} `yaml:"gobble"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Providers.Gobble.Prewarm {
		t.Fatalf("providers.gobble.prewarm not set:\n%s", raw)
	}
}

func TestSetProviderPrewarmFileConcurrentValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(prewarmFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = SetProviderPrewarmFile(path, "kilo", i%2 == 0)
		}(i)
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("concurrent write left invalid YAML: %v\n%s", err, raw)
	}
}

func TestLiveSetPrewarmUpdatesMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(prewarmFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Defaults()
	sec := &agentSection{}
	sec.Prewarm = true
	cfg.Providers.Sections = ProviderSections{"kilo": sec}
	live := &Live{Path: path, Cfg: &cfg}
	if err := live.SetPrewarm("kilo", false); err != nil {
		t.Fatal(err)
	}
	if sec.Prewarm {
		t.Fatal("in-memory kilo prewarm still true")
	}
	got, ok := live.GetPrewarm("kilo")
	if !ok || got {
		t.Fatalf("GetPrewarm = %v %v", got, ok)
	}
}

// TestLiveSetPrewarmRefusesBeforeWriting: an id the running config has no
// agent section for fails before the file changes (0208 PLAN P8).
func TestLiveSetPrewarmRefusesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(prewarmFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Defaults()
	live := &Live{Path: path, Cfg: &cfg}
	if err := live.SetPrewarm("kilo", false); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("SetPrewarm = %v, want ErrUnknownProvider", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != prewarmFixture {
		t.Fatalf("the file changed although the id was refused:\n%s", raw)
	}
}

func mustLoadPrewarm(t *testing.T, raw []byte) (kilo, grok bool) {
	t.Helper()
	var doc struct {
		Providers struct {
			Kilo struct {
				Prewarm bool `yaml:"prewarm"`
			} `yaml:"kilo"`
			Grok struct {
				Prewarm bool `yaml:"prewarm"`
			} `yaml:"grok"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	return doc.Providers.Kilo.Prewarm, doc.Providers.Grok.Prewarm
}

// agentSection is a described agent's section, as a provider's package defines
// one (0208 MADR D6).
type agentSection struct {
	AgentCommon `mapstructure:",squash"`
}

func (s *agentSection) Validate() error { return nil }

// TestPrewarmTogglesADescribedAgentsSection is 0208 PLAN P8: the phone's
// prewarm toggle reaches a provider whose section its own package owns.
func TestPrewarmTogglesADescribedAgentsSection(t *testing.T) {
	sec := &agentSection{}
	cfg := Defaults()
	cfg.Providers.Sections = ProviderSections{"grok": sec}
	if err := cfg.SetProviderPrewarm("grok", true); err != nil {
		t.Fatal(err)
	}
	if !sec.Prewarm {
		t.Fatal("SetProviderPrewarm did not reach the section")
	}
	if got, ok := cfg.ProviderPrewarm("grok"); !ok || !got {
		t.Fatalf("ProviderPrewarm = %v %v, want true true", got, ok)
	}
}
