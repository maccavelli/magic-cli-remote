package gobble

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// keepWirecapHostKey points wirecap at this host's own host key before a test
// swaps HOME, so a capture taken under a scratch HOME still records its capture
// host (MADR 0151 D12; 0179 PLAN, P3 deviation of 2026-11-10). It returns the
// tag such a capture must record, or "" when this host has no key. It fails when
// the path it sets does not give the tag wirecap's own lookup gives, so a key
// path that drifts from wirecap's fails here instead of leaving a capture
// untagged.
func keepWirecapHostKey(t testing.TB) string {
	t.Helper()
	before, err := wirecap.HostTag(false)
	if err != nil {
		t.Fatalf("wirecap host tag: %v", err)
	}
	if strings.TrimSpace(os.Getenv(wirecap.EnvHostKey)) == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("config dir: %v", err)
		}
		t.Setenv(wirecap.EnvHostKey, filepath.Join(dir, "mcremote", "wirecap-host-key"))
	}
	after, err := wirecap.HostTag(false)
	if err != nil {
		t.Fatalf("wirecap host tag through %s: %v", wirecap.EnvHostKey, err)
	}
	if after != before {
		t.Fatalf("%s gives tag %q where wirecap's own lookup gives %q: the key path no longer matches wirecap's",
			wirecap.EnvHostKey, short(after), short(before))
	}
	return before
}

// captureHostMismatch says why the capture in dir does not record want as its
// capture host, or returns nil when it does. On a host with no key (want ""),
// the capture records none.
func captureHostMismatch(dir, want string) error {
	b, err := os.ReadFile(filepath.Join(dir, "capture_host"))
	if want == "" {
		switch {
		case err == nil:
			return fmt.Errorf("capture_host is %q on a host with no wirecap key", short(strings.TrimSpace(string(b))))
		case errors.Is(err, os.ErrNotExist):
			return nil
		default:
			return err
		}
	}
	if err != nil {
		return fmt.Errorf("the capture recorded no capture_host, want %s: %w", short(want), err)
	}
	if got := strings.TrimSpace(string(b)); got != want {
		return fmt.Errorf("capture_host is %s, want %s", short(got), short(want))
	}
	return nil
}

// short is a tag's first eight digits, which is how records cite one.
func short(tag string) string {
	if len(tag) > 8 {
		return tag[:8] + "…"
	}
	return tag
}

// isolateConfigDir gives the test a config directory of its own, through every
// variable os.UserConfigDir reads, with no host key override.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv(wirecap.EnvHostKey, "")
}

// capture opens and closes one wirecap capture for gobble in a new directory,
// and returns the directory wirecap writes gobble's files to.
func capture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(wirecap.EnvDir, dir)
	id := string(provider.IDGobble)
	c := wirecap.For(id)
	if c == nil {
		t.Fatal("wirecap.For returned nil with a capture directory set")
	}
	c.Close()
	return filepath.Join(dir, id)
}

// A capture under a swapped HOME records its host only when the key was kept
// first, which is the gap the live suite's fixture fell into (0179 PLAN, P3
// deviation of 2026-11-10).
func TestWirecapHostKeySurvivesAHomeSwap(t *testing.T) {
	t.Run("kept", func(t *testing.T) {
		isolateConfigDir(t)
		want, err := wirecap.HostTag(true)
		if err != nil || want == "" {
			t.Fatalf("HostTag(true) = %q, %v", want, err)
		}
		if got := keepWirecapHostKey(t); got != want {
			t.Fatalf("keepWirecapHostKey = %s, want %s", short(got), short(want))
		}
		t.Setenv("HOME", t.TempDir())
		t.Setenv("AppData", t.TempDir())
		if err := captureHostMismatch(capture(t), want); err != nil {
			t.Fatalf("after a HOME swap with the key kept: %v", err)
		}
	})
	t.Run("not kept", func(t *testing.T) {
		isolateConfigDir(t)
		want, err := wirecap.HostTag(true)
		if err != nil || want == "" {
			t.Fatalf("HostTag(true) = %q, %v", want, err)
		}
		t.Setenv("HOME", t.TempDir())
		t.Setenv("AppData", t.TempDir())
		if err := captureHostMismatch(capture(t), want); err == nil {
			t.Fatal("a capture under a swapped HOME, with the key not kept, recorded this host's tag; the gap this guards is gone, so this test no longer shows the helper is needed")
		}
	})
	t.Run("no key on this host", func(t *testing.T) {
		isolateConfigDir(t)
		if got := keepWirecapHostKey(t); got != "" {
			t.Fatalf("keepWirecapHostKey = %s on a host with no key, want none", short(got))
		}
		if err := captureHostMismatch(capture(t), ""); err != nil {
			t.Fatalf("no key: %v", err)
		}
	})
}

func TestCaptureHostMismatch(t *testing.T) {
	const tag = "06e8a1e84e90cc8b6d6c1b30fe70fd82f7a6ae3456012f61cf9576cad628c0ba"
	write := func(t *testing.T, body string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "capture_host"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for _, c := range []struct {
		name, dir, want string
		ok              bool
	}{
		{"the tag, with wirecap's newline", write(t, tag+"\n"), tag, true},
		{"another tag", write(t, strings.Repeat("a", 64)+"\n"), tag, false},
		{"no file", t.TempDir(), tag, false},
		{"no key, no file", t.TempDir(), "", true},
		{"no key, but a file", write(t, tag+"\n"), "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := captureHostMismatch(c.dir, c.want)
			if (err == nil) != c.ok {
				t.Fatalf("captureHostMismatch = %v, want ok %v", err, c.ok)
			}
		})
	}
}
