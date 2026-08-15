package service

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
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

// Keys that exist on the Go struct but are not operator-facing YAML
// (retired, or squash leftovers that the daemon ignores). Templates
// must omit these; requiring them would re-document dead levers.
var omittedConfigKeys = map[string]struct{}{
	"providers.opencode.transport": {},
	"providers.goose.args":         {},
	"providers.goose.fs_roots":     {},
}

func exampleConfigBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "configs", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parseYAMLMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) == 0 {
		t.Fatal("empty yaml document")
	}
	return doc
}

func flattenYAML(m map[string]any, prefix string, out map[string]struct{}) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		child, ok := v.(map[string]any)
		if ok {
			flattenYAML(child, path, out)
			continue
		}
		out[path] = struct{}{}
	}
}

// requiredConfigKeys walks Config's mapstructure tags and returns every
// operator-facing key path. Slice/map element fields (mcp_servers.*)
// are recorded only as the collection key.
func requiredConfigKeys() []string {
	var out []string
	walkConfigKeys(reflect.TypeOf(config.Config{}), "", &out)
	return out
}

func walkConfigKeys(t reflect.Type, prefix string, out *[]string) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		name, squash := parseMapstructure(tag)
		if squash {
			walkConfigKeys(f.Type, prefix, out)
			continue
		}
		if name == "" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if _, skip := omittedConfigKeys[path]; skip {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Struct:
			walkConfigKeys(ft, path, out)
		default:
			*out = append(*out, path)
		}
	}
}

func parseMapstructure(tag string) (name string, squash bool) {
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "squash" {
			squash = true
		}
	}
	return name, squash
}

// MADR 0090 — 0069's provider-only parity test did not see top-level
// sections. Receipts (0077), pair, and limits.tcp_keepalive therefore
// shipped in config.example.yaml and never reached the setup-service
// seed. This compares every flattened key in both directions.
func TestTemplateTopLevelKeysMatchExample(t *testing.T) {
	tmpl := map[string]struct{}{}
	example := map[string]struct{}{}
	flattenYAML(parseYAMLMap(t, defaultConfigMcremote), "", tmpl)
	flattenYAML(parseYAMLMap(t, exampleConfigBytes(t)), "", example)

	for key := range tmpl {
		if _, ok := example[key]; !ok {
			t.Errorf("template has %s but example does not", key)
		}
	}
	for key := range example {
		if _, ok := tmpl[key]; !ok {
			t.Errorf("example has %s but template does not (0090 / 0069 F1)", key)
		}
	}
}

// Every mapstructure key on config.Config must appear in the setup-service
// seed and in the documented example. Missing a key is the 0077 receipts
// failure mode: the feature works via Defaults() but operators never see
// the lever in the file they were given.
func TestTemplatesSpellEveryConfigKey(t *testing.T) {
	files := []struct {
		name string
		body []byte
	}{
		{"defaults_mcremote.yaml", defaultConfigMcremote},
		{"config.example.yaml", exampleConfigBytes(t)},
	}
	prod, err := os.ReadFile(filepath.Join("..", "..", "..", "configs", "config.prod.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mesh, err := os.ReadFile(filepath.Join("..", "..", "..", "configs", "config.mesh-grok.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files,
		struct {
			name string
			body []byte
		}{"config.prod.example.yaml", prod},
		struct {
			name string
			body []byte
		}{"config.mesh-grok.yaml", mesh},
	)

	required := requiredConfigKeys()
	if len(required) == 0 {
		t.Fatal("requiredConfigKeys returned nothing")
	}
	for _, f := range files {
		have := map[string]struct{}{}
		flattenYAML(parseYAMLMap(t, f.body), "", have)
		for _, key := range required {
			if _, ok := have[key]; !ok {
				t.Errorf("%s missing %s", f.name, key)
			}
		}
	}
}

// YAML "" overwrites Defaults() (MADR 0073 finding 3 / 0050 D3). The
// seed and the example must ship the pinned value, not an empty string.
func TestTemplateGrokPermissionModeIsDefault(t *testing.T) {
	want := config.Defaults().Providers.Grok.PermissionMode
	if want != "default" {
		t.Fatalf("Defaults().permission_mode = %q, want default", want)
	}
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"defaults_mcremote.yaml", defaultConfigMcremote},
		{"config.example.yaml", exampleConfigBytes(t)},
	} {
		provs := parseProviders(t, tc.body)
		got, ok := provs["grok"]["permission_mode"]
		if !ok {
			t.Errorf("%s: grok.permission_mode missing", tc.name)
			continue
		}
		if got != want {
			t.Errorf("%s: grok.permission_mode = %#v, want %q", tc.name, got, want)
		}
	}
}
