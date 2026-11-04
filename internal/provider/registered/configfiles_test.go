package registered_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider/registered"
)

// repoRoot is the repository root, from this package's directory.
var repoRoot = filepath.Join("..", "..", "..")

// shippedConfigs are the mcremote config files the repository ships: the
// examples, and the seed setup-service writes.
var shippedConfigs = []string{
	"configs/config.example.yaml",
	"configs/config.prod.example.yaml",
	"configs/config.mesh-grok.yaml",
	"internal/cli/service/defaults_mcremote.yaml",
}

// providerDocs document providers.* variables.
var providerDocs = []string{"docs/guides/config.md", "README.md"}

// clearMcremoteEnv unsets every MCREMOTE_* variable for the rest of t, so the
// host's own settings take no part.
func clearMcremoteEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "MCREMOTE_") {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
}

// load loads body as a private config file with the registered sections.
//
// The shipped configs carry relay.secret, so the load refuses a file another
// principal can read (MADR 0155). The directory is made private with the
// product's own primitive: on Windows a temp file inherits whatever the
// host's %TEMP% grants, whatever its mode.
func load(t *testing.T, body []byte) error {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		t.Fatalf("EnsurePrivateDir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(config.LoadOptions{ConfigFile: path, Providers: registered.Sections()})
	return err
}

// TestTheShippedConfigFilesLoadThroughTheSections is 0208 PLAN P8 step 2:
// every config file the repository ships loads through the registered
// sections, which refuse a key they do not have.
func TestTheShippedConfigFilesLoadThroughTheSections(t *testing.T) {
	for _, rel := range shippedConfigs {
		t.Run(rel, func(t *testing.T) {
			clearMcremoteEnv(t)
			body, err := os.ReadFile(filepath.Join(repoRoot, rel))
			if err != nil {
				t.Fatal(err)
			}
			if err := load(t, body); err != nil {
				t.Errorf("%s does not load: %v", rel, err)
			}
		})
	}
}

// documentedVar matches a providers.* variable named in full.
var documentedVar = regexp.MustCompile(`MCREMOTE_PROVIDERS_([A-Z0-9]+)_[A-Z0-9_]+`)

// TestEveryDocumentedProviderVariableNamesAKey is 0208 PLAN P8 step 2: each
// providers.* variable the docs name, for a registered provider, is one its
// section accepts.
func TestEveryDocumentedProviderVariableNamesAKey(t *testing.T) {
	var ids []string
	for _, d := range registered.Descriptors() {
		ids = append(ids, strings.ToUpper(string(d.ID)))
	}
	for _, rel := range providerDocs {
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range documentedVar.FindAllStringSubmatch(string(body), -1) {
			name, id := m[0], m[1]
			if !slices.Contains(ids, id) {
				continue
			}
			t.Run(rel+"/"+name, func(t *testing.T) {
				clearMcremoteEnv(t)
				// An empty value is unset to viper, so only the name is checked.
				t.Setenv(name, "")
				if err := load(t, nil); err != nil {
					t.Errorf("%s documents %s, which does not load: %v", rel, name, err)
				}
			})
		}
	}
}
