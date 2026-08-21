package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// TestDetectCredentialStore proves mcremote only claims to protect the one
// backend it can actually observe. Codex 0.148.0 defaults to the file store,
// but keyring/auto/ephemeral put the credential somewhere this coordinator
// cannot snapshot, lock, or restore — so those return a typed unsupported
// result rather than a false promise (MADR 0074 D22, P18 step 2).
func TestDetectCredentialStore(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		want    CredentialStore
		wantErr error
	}{
		{"no config file at all", "", StoreFile, nil},
		{"empty config", "\n\n", StoreFile, nil},
		{"explicit file", "cli_auth_credentials_store = \"file\"\n", StoreFile, nil},
		{"keyring", "cli_auth_credentials_store = \"keyring\"\n", StoreKeyring, providerauth.ErrUnsupportedBackend},
		{"auto", "cli_auth_credentials_store = \"auto\"\n", StoreAuto, providerauth.ErrUnsupportedBackend},
		{"ephemeral", "cli_auth_credentials_store = \"ephemeral\"\n", StoreEphemeral, providerauth.ErrUnsupportedBackend},
		{"single quotes", "cli_auth_credentials_store = 'keyring'\n", StoreKeyring, providerauth.ErrUnsupportedBackend},
		{"whitespace and comment", "  cli_auth_credentials_store   =  \"keyring\"  # why\n", StoreKeyring, providerauth.ErrUnsupportedBackend},
		{"other keys ignored", "model = \"gpt-5.6-sol\"\napproval_policy = \"never\"\n", StoreFile, nil},
		{
			name: "key inside a table is not the top-level key",
			// A same-named key under a [table] header belongs to that table,
			// so it must not be read as the global setting.
			config: "[profiles.work]\ncli_auth_credentials_store = \"keyring\"\n",
			want:   StoreFile,
		},
		{
			name:   "top-level key before a table still counts",
			config: "cli_auth_credentials_store = \"keyring\"\n[profiles.work]\nmodel = \"x\"\n",
			want:   StoreKeyring, wantErr: providerauth.ErrUnsupportedBackend,
		},
		{
			name:   "unknown value is not silently treated as file",
			config: "cli_auth_credentials_store = \"vault\"\n",
			want:   StoreUnknown, wantErr: providerauth.ErrUnsupportedBackend,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got, err := DetectCredentialStore()
			if got != tc.want {
				t.Errorf("store = %q, want %q", got, tc.want)
			}
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestDetectCredentialStoreUsesEffectiveHome proves detection reads the same
// home the CLI would, not $HOME (MADR 0074 F7).
func TestDetectCredentialStoreUsesEffectiveHome(t *testing.T) {
	decoy := t.TempDir()
	t.Setenv("HOME", decoy)
	if err := os.WriteFile(filepath.Join(decoy, ".codex", "config.toml"), nil, 0o600); err == nil {
		t.Fatal("decoy should not be writable without mkdir; test setup is wrong")
	}

	real := t.TempDir()
	t.Setenv("CODEX_HOME", real)
	if err := os.WriteFile(filepath.Join(real, "config.toml"),
		[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DetectCredentialStore()
	if got != StoreKeyring || !errors.Is(err, providerauth.ErrUnsupportedBackend) {
		t.Fatalf("store = %q err = %v, want keyring/unsupported from CODEX_HOME", got, err)
	}
}
