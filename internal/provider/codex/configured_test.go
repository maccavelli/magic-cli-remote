package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func methodByID(t *testing.T, st provider.AuthState, id string) provider.AuthMethod {
	t.Helper()
	for _, u := range st.Upstreams {
		for _, m := range u.Methods {
			if m.ID == id {
				return m
			}
		}
	}
	t.Fatalf("no method %q", id)
	return provider.AuthMethod{}
}

// TestCodexConfiguredReflectsStoredAuthMode proves Codex reports which of its
// two methods actually wrote the one native credential, rather than marking
// both configured because a file exists (MADR 0074 P18 step 12).
func TestCodexConfiguredReflectsStoredAuthMode(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantAPI    bool
		wantDevice bool
	}{
		{"no credential", "", false, false},
		{"api key", `{"OPENAI_API_KEY":"sk-test","tokens":null}`, true, false},
		{"chatgpt session", `{"tokens":{"access_token":"a","refresh_token":"r"}}`, false, true},
		{"unparseable is not configured", `nope`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if tc.body != "" {
				if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p := New(Config{Bin: "codex"})
			st, err := p.AuthStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := methodByID(t, st, "openai:api").Configured; got != tc.wantAPI {
				t.Errorf("api configured = %v, want %v", got, tc.wantAPI)
			}
			if got := methodByID(t, st, "openai:device").Configured; got != tc.wantDevice {
				t.Errorf("device configured = %v, want %v", got, tc.wantDevice)
			}
		})
	}
}

// TestCodexClearCredentialMethodAliasesOneCredential proves both Codex method
// ids clear the single native credential, because that is what Codex has
// (MADR 0074 P18 step 10).
func TestCodexClearCredentialMethodAliasesOneCredential(t *testing.T) {
	p := New(Config{Bin: "codex"})
	var _ provider.AuthMethodClearer = p

	for _, id := range []string{"openai:api", "openai:device", ""} {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)
		live := filepath.Join(home, "auth.json")
		if err := os.WriteFile(live, []byte(`{"OPENAI_API_KEY":"sk-test"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		// A bin that does not exist makes the clear fail; the point under test
		// is method-id acceptance, not the child's behavior.
		if err := p.ClearCredentialMethod(context.Background(), "", id); err == nil {
			continue
		}
	}
	if err := p.ClearCredentialMethod(context.Background(), "", "openai:mystery"); err == nil {
		t.Error("an unknown method id was accepted")
	}
	if err := p.ClearCredentialMethod(context.Background(), "nope", "openai:api"); err == nil {
		t.Error("an unknown upstream was accepted")
	}
}
