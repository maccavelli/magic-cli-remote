//go:build unix

// The shipped examples deploy/systemd/<product>.user.service are a second copy
// of each embedded unit template. Nothing compared them until MADR 0240, and the
// mcrelay copy kept the two directives MADR 0099 F4a removed from the template,
// so the documented manual install crash-looped with 218/CAPABILITIES. These
// tests fail CI on that drift.

package service_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// allowedUnitDifferences are the directives a deploy user unit may leave out or
// set differently from its template, each with the decision that allows it.
// Widening this map is a reviewed change to a record, not a test fix.
var allowedUnitDifferences = map[string]string{
	"[Service] Type":            "the static examples stay Type=simple (0054-PLAN S8, 0207-PLAN D22, 0240-MADR D4)",
	"[Service] TimeoutStartSec": "only the template's Type=notify needs a start timeout (0240-MADR D4)",
}

// bannedUserUnitDirectives make a systemd user unit unstartable on every host.
var bannedUserUnitDirectives = []string{"PrivateDevices", "RestrictNamespaces"}

// unitDirectives parses systemd unit text into "[Section] Key" -> values. It
// skips blank lines and comments, and keys an Environment= line by its variable
// name, so each variable is compared on its own.
func unitDirectives(text string) map[string][]string {
	out := map[string][]string{}
	section := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "Environment" {
			name, _, _ := strings.Cut(value, "=")
			key = "Environment:" + strings.TrimSpace(name)
		}
		full := section + " " + key
		out[full] = append(out[full], strings.TrimSpace(value))
	}
	return out
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestDeployUserUnitsMatchTheTemplates(t *testing.T) {
	root := repoRoot(t)
	for _, product := range []string{"mcrelay", "mcremote"} {
		t.Run(product, func(t *testing.T) {
			rendered, err := service.RenderUnit(service.Options{
				Product:    product,
				UnitName:   product,
				Binary:     "/home/user/.local/bin/" + product,
				ConfigPath: "/home/user/.config/" + product + "/config.yaml",
				PrintOnly:  true,
			})
			if err != nil {
				t.Fatal(err)
			}
			rawTemplate, err := os.ReadFile(product + ".user.service.tmpl")
			if err != nil {
				t.Fatal(err)
			}
			deployPath := filepath.Join(root, "deploy", "systemd", product+".user.service")
			deployText, err := os.ReadFile(deployPath)
			if err != nil {
				t.Fatal(err)
			}
			tmpl := unitDirectives(rendered)
			literal := unitDirectives(string(rawTemplate))
			deploy := unitDirectives(string(deployText))
			rel := filepath.ToSlash(filepath.Join("deploy", "systemd", product+".user.service"))
			tmplName := product + ".user.service.tmpl"

			for _, key := range sortedKeys(deploy) {
				if _, ok := tmpl[key]; !ok {
					if _, allowed := allowedUnitDifferences[key]; !allowed {
						t.Errorf("%s sets %s, which %s does not", rel, key, tmplName)
					}
				}
			}
			for _, key := range sortedKeys(tmpl) {
				if _, ok := deploy[key]; !ok {
					if _, allowed := allowedUnitDifferences[key]; !allowed {
						t.Errorf("%s does not set %s, which %s does", rel, key, tmplName)
					}
				}
			}
			for _, key := range sortedKeys(literal) {
				want := literal[key]
				if strings.Contains(strings.Join(want, "\n"), "{{") {
					continue // a templated value; the key check covers it
				}
				if _, allowed := allowedUnitDifferences[key]; allowed {
					continue
				}
				got, ok := deploy[key]
				if !ok {
					continue // reported by the key check
				}
				if strings.Join(got, "\n") != strings.Join(want, "\n") {
					t.Errorf("%s: %s = %q, but %s has %q", rel, key, got, tmplName, want)
				}
			}

			for _, banned := range bannedUserUnitDirectives {
				key := "[Service] " + banned
				if _, ok := deploy[key]; ok {
					t.Errorf("%s sets %s: it makes a systemd user unit unstartable on every host "+
						"(218/CAPABILITIES; 0099-MADR-installer-service-state-verification.md F4a)", rel, banned)
				}
				if _, ok := tmpl[key]; ok {
					t.Errorf("%s sets %s (0099-MADR F4a)", tmplName, banned)
				}
			}
			for _, key := range sortedKeys(deploy) {
				for _, v := range deploy[key] {
					if strings.Contains(v, "/cache/") {
						t.Errorf("%s: %s = %q points under /cache/, which is not a standard path; "+
							"agents inherit it and their Go commands fail where it is not writable (0240-MADR F7)", rel, key, v)
					}
				}
			}
		})
	}
}

func TestDeployRelayUnitNamesEveryRelayVariable(t *testing.T) {
	root := repoRoot(t)
	sources, err := filepath.Glob(filepath.Join(root, "internal", "relay", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	literal := regexp.MustCompile(`"(MCRELAY_[A-Z0-9_]+)"`)
	names := map[string]bool{}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range literal.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
	}
	// A moved or renamed package must not let the test pass with nothing to check.
	if len(names) < 30 {
		t.Fatalf("found %d MCRELAY_* variables in internal/relay; want at least 30", len(names))
	}
	unit, err := os.ReadFile(filepath.Join(root, "deploy", "systemd", "mcrelay.user.service"))
	if err != nil {
		t.Fatal(err)
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		if !strings.Contains(string(unit), n+"=") {
			t.Errorf("deploy/systemd/mcrelay.user.service does not name %s; its comments list every "+
				"MCRELAY_* variable (0240-MADR D6)", n)
		}
	}
}
