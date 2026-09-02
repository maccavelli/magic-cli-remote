package updateclient

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maccavelli/mcplib/selfupdate"
	"github.com/spf13/cobra"
)

func TestNewRequiresProduct(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected an error without a product")
	}
}

func TestNewBuildsStandaloneAndManaged(t *testing.T) {
	for _, managed := range []bool{false, true} {
		up, err := New(Options{Product: "mcremote", RawVersion: "v0.15.3", RawBuildKind: "release", Managed: managed})
		if err != nil {
			t.Fatalf("managed=%v: New = %v", managed, err)
		}
		if up == nil {
			t.Fatalf("managed=%v: nil updater", managed)
		}
	}
}

// TestRequestNormalizesLegacyIdentity proves the BASE.N bridge is applied at
// the one place it belongs, on the way into the shared request.
func TestRequestNormalizesLegacyIdentity(t *testing.T) {
	opts := Options{Product: "mcremote", RawVersion: "0.15.3.7", RawBuildKind: "release"}
	req := opts.Request("", true, false, false)
	if req.CurrentVersion != "v0.15.3" {
		t.Errorf("CurrentVersion = %q, want v0.15.3", req.CurrentVersion)
	}
	if req.CurrentBuild != selfupdate.ReleaseBuild {
		t.Errorf("CurrentBuild = %v, want ReleaseBuild", req.CurrentBuild)
	}
	if !req.CheckOnly || req.Force || req.Yes {
		t.Errorf("flags not carried through: %+v", req)
	}
}

func TestRequestLocalBuild(t *testing.T) {
	opts := Options{Product: "mcrelay", RawVersion: "0.15.3.g1234567", RawBuildKind: "local"}
	req := opts.Request("v0.16.0", false, true, true)
	if req.CurrentBuild != selfupdate.LocalBuild {
		t.Errorf("CurrentBuild = %v, want LocalBuild", req.CurrentBuild)
	}
	if req.TargetVersion != "v0.16.0" || !req.Force || !req.Yes {
		t.Errorf("request = %+v", req)
	}
}

func TestPlatformsAreTheFrozenMatrix(t *testing.T) {
	want := map[string]bool{"linux/amd64": true, "linux/arm64": true, "darwin/arm64": true, "windows/amd64": true}
	if len(Platforms) != len(want) {
		t.Fatalf("Platforms = %v", Platforms)
	}
	for _, p := range Platforms {
		if !want[p.OS+"/"+p.Arch] {
			t.Errorf("unexpected platform %v", p)
		}
	}
}

// TestContextDerivesFromCaller is the fix for building the operation context
// from context.Background: cancelling the caller must cancel the update.
func TestContextDerivesFromCaller(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, done := Context(parent)
	defer done()
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancelling the caller did not cancel the operation context")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Error("operation context has no deadline")
	}
}

func TestNewCommandFlagSurface(t *testing.T) {
	cmd := NewCommand("mcremote", func() (string, string) { return "v0.15.3", "release" })
	for _, name := range []string{"check", "yes", "force", "version"} {
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
	cmd := NewCommand("mcremote", func() (string, string) { return "v0.15.3", "release" })
	if err := cmd.Args(cmd, []string{"stray"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}

// TestNewCommandRejectsContradictoryFlags proves the contradiction is caught
// before any network call, by the shared request validator.
func TestNewCommandRejectsContradictoryFlags(t *testing.T) {
	cases := []struct{ name, flag string }{
		{"check and yes", "--yes"},
		{"check and force", "--force"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewCommand("mcremote", func() (string, string) { return "v0.15.3", "release" })
			var out, errBuf bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errBuf)
			cmd.SetIn(strings.NewReader(""))
			cmd.SetArgs([]string{"--check", tc.flag})
			err := cmd.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("expected contradictory flags to be rejected")
			}
			if errors.Is(err, selfupdate.ErrUpdateAvailable) {
				t.Fatal("contradiction was not detected before evaluation")
			}
		})
	}
}

// TestNewCommandUsesCallerContext guards against a reintroduced
// context.Background: a cancelled caller must abort before any network work.
func TestNewCommandUsesCallerContext(t *testing.T) {
	cmd := NewCommand("mcremote", func() (string, string) { return "v0.15.3", "release" })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--check"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cmd.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("expected the cancelled context to abort the command")
	}
}

var _ = cobra.Command{}
