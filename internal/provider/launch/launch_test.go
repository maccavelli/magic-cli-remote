package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveFindsOnPath proves resolution still goes through PATH the way
// exec.LookPath did, on every platform.
func TestResolveFindsOnPath(t *testing.T) {
	name := "go"
	if runtime.GOOS == "windows" {
		name = "cmd"
	}
	r, err := Resolve(name)
	if err != nil {
		t.Skipf("%s not on PATH: %v", name, err)
	}
	if r.Path == "" {
		t.Error("Resolve returned an empty path")
	}
	if _, err := os.Stat(r.Path); err != nil {
		t.Errorf("resolved path does not exist: %v", err)
	}
}

// TestResolveMissingBinary proves a missing engine still reports not-found,
// which is what the provider availability checks report to the user.
func TestResolveMissingBinary(t *testing.T) {
	if _, err := Resolve("mcremote-definitely-not-a-real-binary"); err == nil {
		t.Fatal("Resolve found a binary that does not exist")
	}
}

// TestCommandNativeIsPassThrough pins that a native image is executed
// directly, with argv untouched — the Unix behaviour must be unchanged.
func TestCommandNativeIsPassThrough(t *testing.T) {
	r := Resolved{Path: filepath.Join("usr", "bin", "engine"), Kind: KindNative}
	cmd, err := Command(context.Background(), r, "--flag", "value with spaces")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != r.Path {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, r.Path)
	}
	want := []string{r.Path, "--flag", "value with spaces"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

// TestCommandRejectsOverlongCommandLine pins the CreateProcessW ceiling. It is
// enforced on every platform so a pathological argv fails identically.
func TestCommandRejectsOverlongCommandLine(t *testing.T) {
	r := Resolved{Path: "engine", Kind: KindNative}
	_, err := Command(context.Background(), r, strings.Repeat("a", maxCommandLine+1))
	if !errors.Is(err, ErrCommandLineTooLong) {
		t.Fatalf("err = %v, want ErrCommandLineTooLong", err)
	}
}

// TestKindString keeps the error text stable.
func TestKindString(t *testing.T) {
	if got := KindNative.String(); got != "native" {
		t.Errorf("KindNative = %q", got)
	}
	if got := KindBatch.String(); got != "batch" {
		t.Errorf("KindBatch = %q", got)
	}
}

// TestResolveMatchesLookPathOnUnix proves this package is a pass-through off
// Windows, so P6 cannot have changed Unix behaviour.
func TestResolveMatchesLookPathOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix pass-through check")
	}
	want, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go not on PATH: %v", err)
	}
	got, err := Resolve("go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want {
		t.Errorf("Resolve = %q, LookPath = %q", got.Path, want)
	}
	if got.Kind != KindNative {
		t.Errorf("Kind = %v, want native on Unix", got.Kind)
	}
}
