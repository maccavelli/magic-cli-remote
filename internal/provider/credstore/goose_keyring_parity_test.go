package credstore

import (
	"os"
	"path/filepath"
	"testing"
)

// These pin mcremote's reading of GOOSE_DISABLE_KEYRING to goose's own, taken
// from crates/goose/src/config/base.rs at the installed version 1.47.0
// (MADR 0110 F12). The two branches are deliberately asymmetric there, and
// mcremote must reproduce the asymmetry rather than tidy it up:
//
//	base.rs:206      env: env::var(...).is_ok()      -> presence alone disables
//	base.rs:299-301  yaml: keyring_disabled_value(v) -> only true / "true" / "1"

func cfgWith(t *testing.T, line string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "active_provider: openrouter\n"
	if line != "" {
		body = line + "\n" + body
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGooseKeyringDisabledMatchesGooseConfigRule covers the config branch.
func TestGooseKeyringDisabledMatchesGooseConfigRule(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		// keyring_disabled_value: as_bool, or the strings "true"/"1".
		{`GOOSE_DISABLE_KEYRING: true`, true},
		{`GOOSE_DISABLE_KEYRING: "true"`, true},
		{`GOOSE_DISABLE_KEYRING: "1"`, true},
		{`GOOSE_DISABLE_KEYRING: 1`, true},
		// Everything else leaves the keyring enabled.
		{`GOOSE_DISABLE_KEYRING: false`, false},
		{`GOOSE_DISABLE_KEYRING: "0"`, false},
		{`GOOSE_DISABLE_KEYRING: 0`, false},
		{`GOOSE_DISABLE_KEYRING: "no"`, false},
		{`GOOSE_DISABLE_KEYRING: "off"`, false},
		{`GOOSE_DISABLE_KEYRING: ""`, false},
		{``, false},
	}
	for _, tc := range cases {
		t.Setenv("GOOSE_DISABLE_KEYRING", "")
		os.Unsetenv("GOOSE_DISABLE_KEYRING")
		if got := GooseKeyringDisabled(cfgWith(t, tc.line)); got != tc.want {
			t.Errorf("config %q -> %v, want %v (goose base.rs:299-301)", tc.line, got, tc.want)
		}
	}
}

// TestGooseKeyringDisabledRejectsArbitraryStrings is one of the two cases where
// today's isFalsey handling disagrees with goose: it treats any non-falsey
// string as "disabled", while goose accepts only "true" and "1".
func TestGooseKeyringDisabledRejectsArbitraryStrings(t *testing.T) {
	os.Unsetenv("GOOSE_DISABLE_KEYRING")
	for _, v := range []string{"maybe", "yes", "on", "disabled", "TRUE", "True"} {
		if GooseKeyringDisabled(cfgWith(t, `GOOSE_DISABLE_KEYRING: "`+v+`"`)) {
			t.Errorf("config value %q read as disabled; goose reads it as ENABLED", v)
		}
	}
}

// TestGooseKeyringDisabledMatchesGooseEnvRule is the other disagreement: the
// environment branch is presence-only, so GOOSE_DISABLE_KEYRING=0 disables the
// keyring. mcremote must not "helpfully" interpret that as false.
func TestGooseKeyringDisabledMatchesGooseEnvRule(t *testing.T) {
	path := cfgWith(t, "")
	for _, v := range []string{"1", "true", "0", "false", "no", "off", "", "maybe"} {
		t.Setenv("GOOSE_DISABLE_KEYRING", v)
		if !GooseKeyringDisabled(path) {
			t.Errorf("env value %q read as enabled; goose disables on presence alone (base.rs:206)", v)
		}
	}
}
