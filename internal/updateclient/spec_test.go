package updateclient

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	yaml "go.yaml.in/yaml/v3"
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
	wantExtras := []string{"install.sh", "install.ps1", "magic-cli-remote-v1.2.3-arm64.apk"}
	if got, err := spec.ExtraNames("v1.2.3"); err != nil || !slices.Equal(got, wantExtras) {
		t.Errorf("spec.ExtraNames(v1.2.3) = %q, %v; want %q until S7 generates the installers", got, err, wantExtras)
	}
	if _, err := New(Options{Product: "mcother"}); err == nil {
		t.Error("New accepted a product the spec does not name")
	}
}

// TestReleaseSpecExtrasHaveTheirSources: the build job stages the installers
// from their paths and the APK from android-apk's artifact (MADR 0054 S4), so
// each path must name a file in the repository, and the APK must have none.
func TestReleaseSpecExtrasHaveTheirSources(t *testing.T) {
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"install.sh":                       "scripts/install.sh",
		"install.ps1":                      "scripts/install.ps1",
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
