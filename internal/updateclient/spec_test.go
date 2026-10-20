package updateclient

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	yaml "go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
)

// frozenPlatforms is the release matrix: MADR 0116 D19 leaves out
// windows/arm64, and MADR 0120 leaves out darwin/amd64.
var frozenPlatforms = []selfupdate.Platform{
	{OS: "linux", Arch: "amd64"},
	{OS: "linux", Arch: "arm64"},
	{OS: "darwin", Arch: "arm64"},
	{OS: "windows", Arch: "amd64"},
}

// TestReleaseSpecServesEachProduct: the embedded spec parses, names each
// product with its identity command, gives each an asset selector, and holds
// the frozen platforms and the extras, so a spec this program cannot use
// fails here rather than in a release (MADR 0054 D8, S3, S4).
func TestReleaseSpecServesEachProduct(t *testing.T) {
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range []string{"mcremote", "mcrelay"} {
		p, err := spec.Product(product)
		if err != nil {
			t.Fatalf("spec.Product(%s): %v", product, err)
		}
		if p.Package != "./cmd/"+product || !slices.Equal(p.IdentityArgs, []string{"version"}) {
			t.Errorf("%s = %+v, want package ./cmd/%s and identity_args [version]", product, p, product)
		}
		if _, err := New(Options{Product: product}); err != nil {
			t.Errorf("New(%s): %v", product, err)
		}
	}
	if _, err := spec.AssetSelector(); err != nil {
		t.Fatalf("spec.AssetSelector: %v", err)
	}
	if got := spec.Targets(); !slices.Equal(got, frozenPlatforms) {
		t.Errorf("spec.Targets() = %v, want the frozen matrix %v", got, frozenPlatforms)
	}
	// The APK, then the installers the build workflow generates (MADR 0054
	// PLAN S7b): the release publishes them under the same names as before.
	wantExtras := []string{"magic-cli-remote-v1.2.3-arm64.apk", "install.sh", "install.ps1"}
	if got, err := spec.ExtraNames("v1.2.3"); err != nil || !slices.Equal(got, wantExtras) {
		t.Errorf("spec.ExtraNames(v1.2.3) = %q, %v; want %q", got, err, wantExtras)
	}
	if _, err := New(Options{Product: "mcother"}); err == nil {
		t.Error("New accepted a product the spec does not name")
	}
}

// TestReleaseSpecInstaller: the build workflow generates install.sh and
// install.ps1 from this block (MADR 0054 D8, D9, amendment S7 H1; PLAN S7b).
// Its name is the Windows folder setup-service migrates to (installerName in
// internal/cli/service/migrate.go), its prefix keeps the MCREMOTE_ variables,
// and each product's hook is the upgrade-aware --from-installer, never a plain
// setup-service, which refuses a definition with baked options on a second
// install. Each product's before_uninstall hook removes only its own services
// before the installer deletes its binary (MADR 0054 amendment S9, U1).
func TestReleaseSpecInstaller(t *testing.T) {
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Installer == nil {
		t.Fatal("the spec has no installer block, so the build workflow generates no installers")
	}
	if got := spec.InstallerName("magic-cli-remote"); got != "magic-cli-remote" {
		t.Errorf("installer name %q, want magic-cli-remote, the folder setup-service migrates to", got)
	}
	if got, err := spec.InstallerEnvPrefix("magic-cli-remote"); err != nil || got != "MCREMOTE" {
		t.Errorf("installer env prefix %q, %v; want MCREMOTE", got, err)
	}
	if got := spec.InstallerScripts(); !slices.Equal(got, []string{"install.sh", "install.ps1"}) {
		t.Errorf("installers %v, want install.sh and install.ps1", got)
	}
	var hooks []string
	for _, h := range spec.Installer.Hooks {
		hooks = append(hooks, h.When+" "+h.Product+" "+strings.Join(h.Args, " "))
	}
	want := []string{
		"after_install mcremote setup-service --from-installer",
		"after_install mcrelay setup-service --from-installer",
		"before_uninstall mcremote setup-service --uninstall --from-installer",
		"before_uninstall mcrelay setup-service --uninstall --from-installer",
	}
	if !slices.Equal(hooks, want) {
		t.Errorf("hooks %q, want %q", hooks, want)
	}
}

// TestReleaseSpecExtrasHaveTheirSources: the build job stages the APK from
// android-apk's artifact (MADR 0054 S4), so it has no path; the installers
// are generated, so no extra names a script in the repository (PLAN S7b).
func TestReleaseSpecExtrasHaveTheirSources(t *testing.T) {
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"magic-cli-remote-{tag}-arm64.apk": "",
	}
	if len(spec.Extras) != len(want) {
		t.Fatalf("spec has %d extras, want %d", len(spec.Extras), len(want))
	}
	for _, e := range spec.Extras {
		path, ok := want[e.Name]
		if !ok || e.Path != path {
			t.Errorf("extra %s has path %q, want %q", e.Name, e.Path, path)
			continue
		}
		if path == "" {
			continue
		}
		if fi, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(path))); err != nil || !fi.Mode().IsRegular() {
			t.Errorf("extra %s: %s is not a regular file in the repository: %v", e.Name, path, err)
		}
	}
}

// TestWorkflowPinsMatchGoMod: go.mod and both library workflow pins move
// together (MADR 0054 D1). go.mod requires the library with no replace, so the
// code built is the release it names; each pin is a full commit; and its
// comment names that release, so a move that changes one side alone fails
// here. That the commit is that release's tag is checked by hand, with git
// ls-remote, when the pin moves.
func TestWorkflowPinsMatchGoMod(t *testing.T) {
	const module = "github.com/maccavelli/go-selfupdate-lib"
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, r := range mod.Require {
		if r.Mod.Path == module {
			want = r.Mod.Version
		}
	}
	if want == "" {
		t.Fatalf("go.mod does not require %s", module)
	}
	for _, r := range mod.Replace {
		if r.Old.Path == module {
			t.Errorf("go.mod replaces %s with %s %s: the build would not be the release the pins name", module, r.New.Path, r.New.Version)
		}
	}

	ci, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(ci, &root); err != nil {
		t.Fatal(err)
	}
	const lib = "maccavelli/go-selfupdate-lib/.github/workflows/"
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	calls := 0
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Value == "uses" && v.Kind == yaml.ScalarNode && strings.HasPrefix(v.Value, lib) {
					calls++
					wf, ref, _ := strings.Cut(strings.TrimPrefix(v.Value, lib), "@")
					if !sha.MatchString(ref) {
						t.Errorf("%s is pinned to %q, want a full commit", wf, ref)
					}
					if c := strings.TrimSpace(v.LineComment); c != "# go-selfupdate-lib "+want {
						t.Errorf("%s@%s is commented %q; want \"# go-selfupdate-lib %s\", the release go.mod requires", wf, ref, c, want)
					}
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&root)
	if calls != 2 {
		t.Errorf("ci.yml calls %d library workflows, want the build and publish workflows", calls)
	}
}

// ciJob is the part of a ci.yml job these tests read.
type ciJob struct {
	If    string            `yaml:"if"`
	Needs []string          `yaml:"needs"`
	Uses  string            `yaml:"uses"`
	With  map[string]string `yaml:"with"`
	Steps []struct {
		Uses string            `yaml:"uses"`
		With map[string]string `yaml:"with"`
	} `yaml:"steps"`
}

// TestReleaseJobsReadTheSpec: the release is built from this package's
// embedded spec, and published with the inputs the build job derives from it,
// never with a copy written into ci.yml (MADR 0054 D8, S4). The build gets the
// APK from the artifact android-apk uploads; publish runs only on a tag and
// never for a rehearsal; and both library workflows are called at one pin.
func TestReleaseJobsReadTheSpec(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var ci struct {
		Jobs map[string]ciJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &ci); err != nil {
		t.Fatal(err)
	}
	build, publish, apk := ci.Jobs["build"], ci.Jobs["publish"], ci.Jobs["android-apk"]

	const lib = "maccavelli/go-selfupdate-lib/.github/workflows/"
	buildPin, ok := strings.CutPrefix(build.Uses, lib+"build-selfupdate-release.yml@")
	if !ok {
		t.Fatalf("build uses %q, want the library's build workflow", build.Uses)
	}
	if publishPin, ok := strings.CutPrefix(publish.Uses, lib+"publish-selfupdate-release.yml@"); !ok || publishPin != buildPin {
		t.Errorf("publish uses %q, want the library's publish workflow at the build's pin %s", publish.Uses, buildPin)
	}
	if got := build.With["spec-path"]; got != "internal/updateclient/selfupdate-release.json" {
		t.Errorf("build spec-path = %q, want the spec this package embeds", got)
	}
	if !slices.Contains(build.Needs, "android-apk") || build.If != apk.If {
		t.Errorf("build needs %v if %q; want android-apk, under its gate %q", build.Needs, build.If, apk.If)
	}
	uploaded := ""
	for _, s := range apk.Steps {
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
			uploaded = s.With["name"]
		}
	}
	if uploaded == "" || build.With["extras-artifact-name"] != uploaded {
		t.Errorf("build extras-artifact-name = %q, but android-apk uploads %q", build.With["extras-artifact-name"], uploaded)
	}

	if !slices.Equal(publish.Needs, []string{"build"}) {
		t.Errorf("publish needs %v, want [build]", publish.Needs)
	}
	if publish.If != "github.ref_type == 'tag' && needs.build.outputs.rehearsal == 'false'" {
		t.Errorf("publish if = %q, want it to run only on a tag that is not a rehearsal", publish.If)
	}
	inputs := []string{"artifact-name", "products-json", "platforms-json", "extra-assets-json", "prerelease-channels-json"}
	if len(publish.With) != len(inputs) {
		t.Errorf("publish has inputs %v, want exactly %v", publish.With, inputs)
	}
	for _, in := range inputs {
		if want := "${{ needs.build.outputs." + in + " }}"; publish.With[in] != want {
			t.Errorf("publish %s = %q, want %q", in, publish.With[in], want)
		}
	}
}
