package updateclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/releasespec"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// The release fixture is the program in testdata/fixture. The update's guards
// refuse a release binary that is not an executable image for this platform,
// or that does not run and report the tag being installed (MADR 0207 D16), so
// the tests serve a real program, and TestMain builds it once.
const (
	// fixtureVersionEnv, when set, is the tag the fixture reports instead of
	// v0.2.0, the release the tests install.
	fixtureVersionEnv = "MCREMOTE_UPDATECLIENT_FIXTURE_VERSION"
	// fixtureExitEnv, when set, is the fixture's exit status.
	fixtureExitEnv = "MCREMOTE_UPDATECLIENT_FIXTURE_EXIT"
	// fixtureFailNamedEnv, when set, makes the fixture exit 1 when its own
	// file has that name: an update stages it under another name, so it
	// fails only once installed.
	fixtureFailNamedEnv = "MCREMOTE_UPDATECLIENT_FIXTURE_FAIL_NAMED"
	// fixtureBuildTimeout bounds the fixture's one build.
	fixtureBuildTimeout = 2 * time.Minute
)

// fixtureImage is the built fixture: an executable image for the host
// platform, set once by TestMain, from the file at fixturePath.
var (
	fixtureImage []byte
	fixturePath  string
)

// hostPlatform is the platform the update installs for.
var hostPlatform = selfupdate.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}

func TestMain(m *testing.M) {
	code, err := runWithFixture(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "updateclient: release fixture:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

// runWithFixture builds the fixture into a temporary directory, runs the
// tests, and removes the directory.
func runWithFixture(m *testing.M) (int, error) {
	spec, err := releaseSpec()
	if err != nil {
		return 0, err
	}
	product, err := spec.Product("mcremote")
	if err != nil {
		return 0, err
	}
	// The fixture answers only "version", so a spec that probes with
	// anything else must fail here, not as a puzzling refusal in each test.
	if !slices.Equal(product.IdentityArgs, []string{"version"}) {
		return 0, fmt.Errorf("the spec probes mcremote with %q; testdata/fixture answers only [version]", product.IdentityArgs)
	}
	dir, err := os.MkdirTemp("", "updateclient-fixture-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if fixturePath, err = buildFixture(dir); err != nil {
		return 0, err
	}
	if fixtureImage, err = os.ReadFile(fixturePath); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// buildFixture builds testdata/fixture for the host into dir and returns its
// path. The build is hermetic, as go-selfupdate-lib builds its own fixtures:
// the fixture's own module, no workspace, no cgo, and no GOFLAGS, so it is
// the same program under -race, -cover or any flags the tests run with.
func buildFixture(dir string) (string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("the go command is required to build the fixture: %w", err)
	}
	out := filepath.Join(dir, "fixture")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), fixtureBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", out, ".") //nolint:gosec // the go command, building a test fixture
	cmd.Dir = filepath.Join("testdata", "fixture")
	cmd.Env = append(os.Environ(),
		"GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build in %s: %w\n%s", cmd.Dir, err, b)
	}
	return out, nil
}

// TestFixtureAnswersOnlyItsIdentityArguments pins the contract the refusal
// tests rely on: the fixture reports a release of v0.2.0 for "version", the
// tag and exit status the tests set, and fails any other run.
func TestFixtureAnswersOnlyItsIdentityArguments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		env      []string
		stdout   string
		exitCode int
	}{
		{"version", []string{"version"}, nil, "v0.2.0 (release)\n", 0},
		{"another tag", []string{"version"}, []string{fixtureVersionEnv + "=v0.1.9"}, "v0.1.9 (release)\n", 0},
		{"an exit status", []string{"version"}, []string{fixtureExitEnv + "=3"}, "v0.2.0 (release)\n", 3},
		{"an exit status that is no number", []string{"version"}, []string{fixtureExitEnv + "=x"}, "v0.2.0 (release)\n", 2},
		{"no arguments", nil, nil, "", 2},
		{"other arguments", []string{"--version"}, nil, "", 2},
		{"extra arguments", []string{"version", "--json"}, nil, "", 2},
		{"run under the name it must fail as", []string{"version"},
			[]string{fixtureFailNamedEnv + "=" + filepath.Base(fixturePath)}, "", 1},
		{"run under another name than the one it must fail as", []string{"version"},
			[]string{fixtureFailNamedEnv + "=mcremote-installed"}, "v0.2.0 (release)\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(fixturePath, tc.args...) //nolint:gosec // the fixture TestMain built
			cmd.Env = append(os.Environ(), tc.env...)
			out, err := cmd.Output()
			code := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.stdout || code != tc.exitCode {
				t.Errorf("stdout %q, exit %d; want %q, exit %d", out, code, tc.stdout, tc.exitCode)
			}
		})
	}
}

func TestNewRequiresProduct(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected an error without a product")
	}
}

func TestNewBuildsStandaloneAndManaged(t *testing.T) {
	for _, managed := range []bool{false, true} {
		up, err := New(Options{Product: "mcremote", Identity: buildinfo.Info{Version: "v0.15.3", Kind: buildinfo.KindRelease}, Managed: managed})
		if err != nil {
			t.Fatalf("managed=%v: New = %v", managed, err)
		}
		if up == nil {
			t.Fatalf("managed=%v: nil updater", managed)
		}
	}
}

// TestNewUsesTheLibraryHTTPClient is MADR 0207 D16: with no client given, the
// update uses the library's, which has no Client.Timeout. cli.Run's context
// bounds the whole update, and a client timeout would cut off a slow but
// healthy download. A given client is used as it is.
func TestNewUsesTheLibraryHTTPClient(t *testing.T) {
	c := httpClient(nil)
	if c == nil {
		t.Fatal("no client")
	}
	if c.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want none", c.Timeout)
	}
	if c == http.DefaultClient || c.Transport == nil {
		t.Errorf("client = %+v, want the library's own, with its own transport", c)
	}
	given := &http.Client{}
	if got := httpClient(given); got != given {
		t.Error("a given client was replaced")
	}
}

// TestNewProbesTheStagedBinary is MADR 0207 D16: the staged binary is run
// with the spec's identity arguments, after its signature on macOS with a
// signing identity, and a product the spec gives no identity arguments is
// refused rather than left unprobed.
func TestNewProbesTheStagedBinary(t *testing.T) {
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mcremote", "mcrelay"} {
		product, err := spec.Product(name)
		if err != nil {
			t.Fatal(err)
		}
		probes, version, err := newProbes(product, "")
		if err != nil || len(probes) != 1 || version == nil {
			t.Errorf("%s: newProbes = %d probes, version %v, %v; want the version probe", name, len(probes), version, err)
		}
	}
	if _, _, err := newProbes(releasespec.Product{Name: "mcother"}, ""); err == nil {
		t.Error("a product with no identity_args was given no probe and no error")
	}
	product, err := spec.Product("mcremote")
	if err != nil {
		t.Fatal(err)
	}
	want := 1
	if runtime.GOOS == "darwin" {
		want = 2
	}
	probes, version, err := newProbes(product, "Developer ID Application: Example (TEAMID)")
	if err != nil || len(probes) != want || version == nil {
		t.Errorf("with a signing identity: newProbes = %d probes, version %v, %v; want %d and the version probe", len(probes), version, err, want)
	}
}

// TestNewCommandFlagSurface: the command carries the library's canonical
// flags (MADR 0054 S3, draft amendment 2).
func TestNewCommandFlagSurface(t *testing.T) {
	cmd := NewCommand("mcremote", service.HealthOptions{})
	for _, name := range []string{"check", "yes", "force", "dry-run", "json", "version", "channel"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	if f := cmd.Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
		t.Error("missing -y shorthand for --yes")
	}
}

// TestNewCommandRejectsPositionalArgs keeps a typo from being read as an
// argument the command silently ignores.
func TestNewCommandRejectsPositionalArgs(t *testing.T) {
	cmd := NewCommand("mcremote", service.HealthOptions{})
	if err := cmd.Args(cmd, []string{"stray"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}
