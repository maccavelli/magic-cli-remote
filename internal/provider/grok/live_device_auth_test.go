//go:build live_grok

package grok_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

func hashOrAbsent(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func grokVersion(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("grok", "--version").CombinedOutput()
	if err != nil {
		t.Skipf("grok not runnable: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestLiveGrokEffectiveHomeAndLock pins the two facts the coordinator depends
// on: GROK_HOME wins over $HOME, and the native sibling lock is the one grok's
// own writer honours (MADR 0074 F10/F12).
func TestLiveGrokEffectiveHomeAndLock(t *testing.T) {
	t.Logf("grok version under test: %s", grokVersion(t))

	// A decoy $HOME proves resolution does not silently fall back to it.
	isolated := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GROK_HOME", isolated)

	auth, err := credstore.GrokAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(isolated, "auth.json"); auth != want {
		t.Fatalf("effective auth path = %q, want %q", auth, want)
	}
	lock, err := credstore.GrokAuthLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if lock != auth+".lock" {
		t.Fatalf("lock path = %q, want the sibling grok itself uses", lock)
	}
	cfg, err := credstore.GrokConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(cfg) != isolated {
		t.Fatalf("config path %q escaped the effective home", cfg)
	}
}

// TestLiveGrokIsolatedFlowLeavesHostCredentialIdentical starts and cancels a
// real device login in an isolated home and proves the operator's credential is
// untouched. It never completes an authorization and never logs the code.
func TestLiveGrokIsolatedFlowLeavesHostCredentialIdentical(t *testing.T) {
	t.Logf("grok version under test: %s", grokVersion(t))

	realPath, err := credstore.GrokAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	realBefore := hashOrAbsent(t, realPath)

	isolated := t.TempDir()
	t.Setenv("GROK_HOME", isolated)

	coord, err := providerauth.NewCoordinator(t.TempDir(),
		grok.NewCredentialAdapter("grok"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}

	c := grok.NewCoordinated(grok.New(grok.Config{Bin: "grok"}), "grok", nil, coord, nil)
	h, err := c.StartOwnedDeviceAuth(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("start device auth: %v", err)
	}
	if h.Flow().UserCode == "" {
		t.Error("no device code was parsed from a live grok login")
	}
	t.Log("device code received (value deliberately not logged)")

	h.Cancel()
	if err := h.Wait(context.Background()); err == nil {
		t.Error("a cancelled live flow reported success")
	}

	if got := hashOrAbsent(t, realPath); got != realBefore {
		t.Fatal("the real host credential changed during an isolated flow")
	}
	// No browser stub may survive in the system temp directory: the owned flow
	// roots them inside its transaction (MADR 0107 D6 under 0074 D27).
	stubs, _ := filepath.Glob(filepath.Join(os.TempDir(), "mcremote-grok-open-*"))
	for _, d := range stubs {
		t.Errorf("browser stub outlived the flow: %s", d)
	}
}
