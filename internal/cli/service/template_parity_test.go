package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// MADR 0069 P0 (U1) — the setup template must never silently drift from
// the documented example config. The 0069 incident: the template omitted
// codex's `allow_full_access`, so provisioned hosts hid the lever and the
// phone's mode menu diverged between hosts for months before anyone could
// tell why. Every provider key must exist in both files with the same
// default, in both directions.
func TestTemplateProviderKeysMatchExample(t *testing.T) {
	tmpl := parseProviders(t, defaultConfigMcremote)
	exampleBytes, err := os.ReadFile(
		filepath.Join("..", "..", "..", "configs", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	example := parseProviders(t, exampleBytes)

	for prov, tKeys := range tmpl {
		eKeys, ok := example[prov]
		if !ok {
			t.Errorf("provider %q in template but not in example", prov)
			continue
		}
		for key, tVal := range tKeys {
			eVal, present := eKeys[key]
			if !present {
				t.Errorf("%s.%s shipped by template but undocumented in example", prov, key)
				continue
			}
			if !reflect.DeepEqual(tVal, eVal) {
				t.Errorf("%s.%s default differs: template=%#v example=%#v",
					prov, key, tVal, eVal)
			}
		}
		// The reverse direction is the 0069 F1 drift shape: a lever the
		// example documents that provisioned hosts never see.
		for key := range eKeys {
			if _, present := tKeys[key]; !present {
				t.Errorf("%s.%s documented in example but missing from template (0069 F1)",
					prov, key)
			}
		}
	}
	for prov := range example {
		if _, ok := tmpl[prov]; !ok {
			t.Errorf("provider %q in example but not in template", prov)
		}
	}
}

func parseProviders(t *testing.T, b []byte) map[string]map[string]any {
	t.Helper()
	var doc struct {
		Providers map[string]map[string]any `yaml:"providers"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Providers) == 0 {
		t.Fatal("no providers block parsed")
	}
	return doc.Providers
}
