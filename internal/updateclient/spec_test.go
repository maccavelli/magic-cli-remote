package updateclient

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
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
// the frozen platforms and the APK extra, so a spec this program cannot use
// fails here rather than in a release (MADR 0054 D8, S3).
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
	if got, err := spec.ExtraNames("v1.2.3"); err != nil || !slices.Equal(got, []string{"magic-cli-remote-v1.2.3-arm64.apk"}) {
		t.Errorf("spec.ExtraNames(v1.2.3) = %q, %v; want the APK only until S4 and S7", got, err)
	}
	if _, err := New(Options{Product: "mcother"}); err == nil {
		t.Error("New accepted a product the spec does not name")
	}
}

// TestReleaseSpecMatchesThePublishJob: until the library's build workflow
// reads the spec (MADR 0054 PLAN S4), the publish job's products and platforms
// are written out in ci.yml, and must agree with the spec the client selects
// assets from.
func TestReleaseSpecMatchesThePublishJob(t *testing.T) {
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	ci, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ input, want string }{
		{"products-json", spec.ProductsJSON()},
		{"platforms-json", spec.PlatformsJSON()},
	} {
		m := regexp.MustCompile(`(?m)^\s+` + tc.input + `: '(.*)'$`).FindSubmatch(ci)
		if m == nil {
			t.Fatalf("ci.yml has no %s input", tc.input)
		}
		var got, want any
		if err := json.Unmarshal(m[1], &got); err != nil {
			t.Fatalf("ci.yml %s: %v", tc.input, err)
		}
		if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ci.yml %s = %s, but the spec gives %s", tc.input, m[1], tc.want)
		}
	}
}
