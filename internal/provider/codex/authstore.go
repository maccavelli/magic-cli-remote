package codex

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// CredentialStore is where Codex keeps its CLI credentials, as configured by
// the top-level `cli_auth_credentials_store` key in <CodexHome>/config.toml.
// Codex 0.148.0 names these `file`, `keyring`, `auto`, and `ephemeral`, and
// defaults to `file`.
type CredentialStore string

const (
	// StoreFile is CODEX_HOME/auth.json — the default, and the only backend
	// this repair can observe, snapshot, lock, and restore.
	StoreFile CredentialStore = "file"
	// StoreKeyring puts the credential in the OS keyring.
	StoreKeyring CredentialStore = "keyring"
	// StoreAuto uses the keyring when available and falls back to a file, so
	// the effective location is not knowable ahead of time.
	StoreAuto CredentialStore = "auto"
	// StoreEphemeral keeps the credential in the CLI process only.
	StoreEphemeral CredentialStore = "ephemeral"
	// StoreUnknown is a configured value this binary does not recognize.
	StoreUnknown CredentialStore = "unknown"
)

// credentialStoreKey is Codex's config key. The config file deliberately omits
// the `_mode` suffix the Rust type carries internally.
const credentialStoreKey = "cli_auth_credentials_store"

// DetectCredentialStore reports Codex's configured credential backend and,
// for anything other than the file store, a wrapped
// providerauth.ErrUnsupportedBackend.
//
// mcremote must not claim to protect a store it cannot see. Snapshotting,
// locking, fingerprinting, and restoring all assume a file at a known path; a
// keyring, auto, or ephemeral store breaks every one of those assumptions, so
// the honest answer is a typed refusal rather than a silent no-op
// (MADR 0074 D22, P18 step 2).
//
// Detection reads only the user config. A managed ConfigRequirements file can
// also force this value; when that is in force the CLI is authoritative and a
// caller that sees StoreFile here may still meet a keyring at runtime, which
// surfaces as a candidate that never appears.
func DetectCredentialStore() (CredentialStore, error) {
	home, err := credstore.CodexHome()
	if err != nil {
		return StoreFile, err
	}
	raw, err := readTopLevelTOMLString(filepath.Join(home, "config.toml"), credentialStoreKey)
	if err != nil {
		return StoreFile, err
	}
	switch CredentialStore(raw) {
	case "":
		// Unset means Codex's own default.
		return StoreFile, nil
	case StoreFile:
		return StoreFile, nil
	case StoreKeyring, StoreAuto, StoreEphemeral:
		s := CredentialStore(raw)
		return s, fmt.Errorf("%w: codex stores credentials in the %s backend, which mcremote cannot protect",
			providerauth.ErrUnsupportedBackend, s)
	default:
		return StoreUnknown, fmt.Errorf("%w: codex credential store %q is not recognized",
			providerauth.ErrUnsupportedBackend, raw)
	}
}

// readTopLevelTOMLString returns the value of a bare top-level string key.
//
// This is deliberately not a TOML parser. It reads only keys that appear
// before the first table header, because a same-named key under `[profiles.x]`
// belongs to that table and must never be mistaken for the global setting. A
// missing file is not an error: it means the default.
func readTopLevelTOMLString(path, key string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path derived from the effective codex home
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// First table header: every later key is scoped to a table.
			return "", nil
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		return unquoteTOMLScalar(value), nil
	}
	return "", sc.Err()
}

// unquoteTOMLScalar strips a trailing comment and surrounding quotes.
func unquoteTOMLScalar(v string) string {
	v = strings.TrimSpace(v)
	// A comment can only follow the value; quotes here never contain '#' for
	// the enum values this reads.
	if i := strings.Index(v, "#"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
