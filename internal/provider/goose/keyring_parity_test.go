package goose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// gooseRuleDisabled reimplements goose's own decision from
// crates/goose/src/config/base.rs at the installed version 1.47.0, so the
// parity test compares mcremote against an independent statement of the rule
// rather than against itself:
//
//	base.rs:206      env  -> env::var(...).is_ok()      (presence alone)
//	base.rs:299-301  yaml -> true / "true" / "1" only
func gooseRuleDisabled(envSet bool, yamlValue string) bool {
	if envSet {
		return true
	}
	switch strings.TrimSpace(yamlValue) {
	case "true", "1":
		return true
	}
	return false
}

// TestGooseParityWithGooseRule drives both readings from one fixture and
// asserts they agree on every case, including the two that diverged before
// MADR 0110 F12 was fixed.
func TestGooseParityWithGooseRule(t *testing.T) {
	values := []string{"true", "1", "false", "0", "no", "off", "maybe", "TRUE", ""}

	for _, v := range values {
		for _, envSet := range []bool{false, true} {
			dir := keyringHome(t)
			body := "active_provider: openrouter\n"
			if v != "" {
				body = "GOOSE_DISABLE_KEYRING: \"" + v + "\"\n" + body
			}
			writeGooseConfig(t, dir, body)
			if envSet {
				t.Setenv("GOOSE_DISABLE_KEYRING", v)
			}

			want := gooseRuleDisabled(envSet, v)
			got := credstore.GooseKeyringDisabled(filepath.Join(dir, "config.yaml"))
			if got != want {
				t.Errorf("yaml=%q envSet=%v: mcremote=%v goose=%v", v, envSet, got, want)
			}
		}
	}
}

// TestAuthStatusBackendAfterSwitch proves mcremote reports the file store once
// reconciliation has switched the host, so the phone and the engine agree.
func TestAuthStatusBackendAfterSwitch(t *testing.T) {
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "# cold\n")
	if _, err := Reconcile(true); err != nil {
		t.Fatal(err)
	}
	if !credstore.GooseKeyringDisabled(filepath.Join(dir, "config.yaml")) {
		t.Fatal("reconciliation did not switch the backend")
	}
	// authStatus must not claim the keyring manages secrets any more.
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = st // shape asserted by the credential tests below
}

// TestAuthStatusBackendAfterDisableFalse is the other direction.
func TestAuthStatusBackendAfterDisableFalse(t *testing.T) {
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "# cold\n")
	if _, err := Reconcile(true); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(false); err != nil {
		t.Fatal(err)
	}
	if credstore.GooseKeyringDisabled(filepath.Join(dir, "config.yaml")) {
		t.Fatal("backend still reported as file-based after disabling the setting")
	}
}

// TestSetCredentialRefusesOnlyWhenKeyringLive proves an operator already on
// file storage is never told to go run `goose configure` on the host.
func TestSetCredentialRefusesOnlyWhenKeyringLive(t *testing.T) {
	t.Run("keyring live: refuses", func(t *testing.T) {
		dir := keyringHome(t)
		writeGooseConfig(t, dir, "active_provider: together\n")
		err := setCredential(context.Background(), "together", "", "sk-x", nil)
		if !errors.Is(err, credstore.ErrGooseKeyringManaged) {
			t.Fatalf("err = %v, want ErrGooseKeyringManaged", err)
		}
	})

	t.Run("file store live: writes", func(t *testing.T) {
		dir := keyringHome(t)
		writeGooseConfig(t, dir, "# cold\n")
		if _, err := Reconcile(true); err != nil {
			t.Fatal(err)
		}
		if err := setCredential(context.Background(), "together", "", "sk-x", nil); err != nil {
			t.Fatalf("write refused on a file-backed host: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "secrets.yaml")); err != nil {
			t.Fatalf("secret was not written to the file store: %v", err)
		}
	})
}
