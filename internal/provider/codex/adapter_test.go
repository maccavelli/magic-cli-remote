package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

func TestCodexAdapterPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("HOME", "/tmp/decoy")

	ad := NewCredentialAdapter("codex")
	if ad.ProviderID() != "codex" {
		t.Fatalf("provider id = %q", ad.ProviderID())
	}
	live, err := ad.LivePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "auth.json"); live != want {
		t.Fatalf("live = %q, want %q", live, want)
	}
	lock, err := ad.NativeLockPath()
	if err != nil {
		t.Fatal(err)
	}
	// MADR 0133: the base path WithLock derives auth.json.lock from, not the
	// lock file itself. credstore.TestAuthLockPathsFlockTheFileTheProviderHonors
	// asserts which file is actually flocked.
	if lock != live {
		t.Fatalf("lock = %q, want %q — WithLock appends the .lock suffix", lock, live)
	}
	if ad.CandidateName() != "auth.json" {
		t.Fatalf("candidate name = %q", ad.CandidateName())
	}
	if env := ad.PendingEnv("/pending"); len(env) != 1 || env[0] != "CODEX_HOME=/pending" {
		t.Fatalf("pending env = %v", env)
	}
}

// TestCodexAdapterValidate proves the adapter reports Codex's own auth mode and
// the revocability that decides whether a logout is a point of no return.
func TestCodexAdapterValidate(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantMode      string
		wantRevocable bool
		wantErr       bool
	}{
		{
			name:          "chatgpt tokens are revocable",
			body:          `{"OPENAI_API_KEY":null,"tokens":{"access_token":"a","refresh_token":"r","account_id":"acct"},"last_refresh":"2026-08-21T00:00:00Z"}`,
			wantMode:      authModeChatGPT,
			wantRevocable: true,
		},
		{
			name:          "api key is not revocable",
			body:          `{"OPENAI_API_KEY":"sk-test","tokens":null}`,
			wantMode:      authModeAPIKey,
			wantRevocable: false,
		},
		{
			name:          "tokens win when both are present",
			body:          `{"OPENAI_API_KEY":"sk-test","tokens":{"access_token":"a","refresh_token":"r"}}`,
			wantMode:      authModeChatGPT,
			wantRevocable: true,
		},
		{"not json", `nope`, "", false, true},
		{"empty object has no credential", `{}`, "", false, true},
		{"null tokens and blank key", `{"OPENAI_API_KEY":"","tokens":null}`, "", false, true},
	}

	ad := NewCredentialAdapter("codex")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := ad.Validate(context.Background(), []byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				if strings.Contains(err.Error(), "sk-test") {
					t.Fatalf("error leaked the key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if meta.Mode != tc.wantMode {
				t.Errorf("mode = %q, want %q", meta.Mode, tc.wantMode)
			}
			if meta.Revocable != tc.wantRevocable {
				t.Errorf("revocable = %v, want %v", meta.Revocable, tc.wantRevocable)
			}
		})
	}
}

// TestCodexAdapterFreshnessUsesRefreshTime proves an autonomous refresh is
// ordered by Codex's own last_refresh rather than a timestamp mcremote invents,
// and that credentials of different modes are never compared (MADR 0074 D24).
func TestCodexAdapterFreshnessUsesRefreshTime(t *testing.T) {
	ad := NewCredentialAdapter("codex")
	ctx := context.Background()

	older, err := ad.Validate(ctx, []byte(`{"tokens":{"access_token":"a","refresh_token":"r"},"last_refresh":"2026-08-20T00:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := ad.Validate(ctx, []byte(`{"tokens":{"access_token":"a","refresh_token":"r"},"last_refresh":"2026-08-21T00:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	key, err := ad.Validate(ctx, []byte(`{"OPENAI_API_KEY":"sk-test"}`))
	if err != nil {
		t.Fatal(err)
	}

	if !newer.Fresher(older) {
		t.Error("a later last_refresh must be fresher")
	}
	if older.Fresher(newer) {
		t.Error("an earlier last_refresh must never win")
	}
	if newer.Fresher(newer) {
		t.Error("an identical credential is not fresher than itself")
	}
	if key.Fresher(newer) || newer.Fresher(key) {
		t.Error("credentials of different modes must never be compared")
	}
}

// TestCodexAdapterRefusesUnsupportedBackend proves the adapter will not open a
// transaction against a store it cannot observe (MADR 0074 D22).
func TestCodexAdapterRefusesUnsupportedBackend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"),
		[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ad := NewCredentialAdapter("codex")
	if err := ad.CheckBackend(); !errors.Is(err, providerauth.ErrUnsupportedBackend) {
		t.Fatalf("err = %v, want ErrUnsupportedBackend", err)
	}
}

// TestCodexAdapterMaxBytesIsBounded proves a runaway candidate is refused
// before it is read into memory.
func TestCodexAdapterMaxBytesIsBounded(t *testing.T) {
	ad := NewCredentialAdapter("codex")
	if n := ad.MaxCandidateBytes(); n <= 0 || n > 1<<20 {
		t.Fatalf("max candidate bytes = %d, want a small positive bound", n)
	}
}

// TestCodexAdapterSatisfiesInterface pins the adapter to the coordinator's
// contract so a signature drift is a compile error, not a runtime surprise.
func TestCodexAdapterSatisfiesInterface(t *testing.T) {
	var _ providerauth.Adapter = NewCredentialAdapter("codex")
}
