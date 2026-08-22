package goose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// isolate points every goose path at a temp HOME and starts from a host whose
// keyring is disabled — the configuration in which mcremote can write.
func isolate(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GOOSE_DISABLE_KEYRING", "1")
	if err := os.MkdirAll(filepath.Join(home, ".config", "goose"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

// The point of D16 for goose: the phone can reach every vendor goose supports,
// not only the four or five a host's config.yaml happens to name.
func TestCatalogCoversGooseRegistry(t *testing.T) {
	isolate(t)
	cat, err := authCatalogList(context.Background())
	if err != nil {
		t.Fatalf("authCatalogList: %v", err)
	}
	if cat.Source != provider.AuthCatalogSourceStatic {
		t.Errorf("source = %q, want static — goose has no live catalog to read", cat.Source)
	}
	if len(cat.Upstreams) < 60 {
		t.Fatalf("catalog has %d upstreams; goose 1.46 registers ~73", len(cat.Upstreams))
	}
	byID := map[string]provider.UpstreamAuth{}
	for _, up := range cat.Upstreams {
		byID[up.ID] = up
	}
	// One from each source that feeds the table: declarative definitions,
	// coded providers, and the vendors this project actually uses.
	for id, label := range map[string]string{
		"together":    "Together AI",
		"xai":         "xAI",
		"anthropic":   "Anthropic",
		"opencode_go": "OpenCode Go",
	} {
		up, ok := byID[id]
		if !ok {
			t.Errorf("catalog is missing %q", id)
			continue
		}
		if up.Label != label {
			t.Errorf("%s label = %q, want %q", id, up.Label, label)
		}
		if len(up.Methods) != 1 || up.Methods[0].Type != provider.AuthMethodAPIKey {
			t.Errorf("%s methods = %+v, want one api_key method", id, up.Methods)
		}
	}
	// Subscription-backed vendors have no key to paste; offering a key field
	// would be a dead end, so they must be typed as host-side sign-in.
	for _, id := range []string{"chatgpt_codex", "gemini_oauth", "xai_oauth", "github_copilot"} {
		up, ok := byID[id]
		if !ok {
			t.Errorf("catalog is missing %q", id)
			continue
		}
		if len(up.Methods) != 1 || up.Methods[0].Type != provider.AuthMethodOAuthBrowser {
			t.Errorf("%s methods = %+v, want a host-side sign-in method", id, up.Methods)
		}
	}
}

// A key written from the phone must land under the exact name goose reads it
// by, and the file must not be readable by anyone else.
func TestSetCredentialWritesGooseSecretStore(t *testing.T) {
	home := isolate(t)
	if err := setCredential(context.Background(), "together", "together:api", "sk-together-live", nil); err != nil {
		t.Fatalf("setCredential: %v", err)
	}
	path := filepath.Join(home, ".config", "goose", "secrets.yaml")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("secrets.yaml mode = %v, want 0600", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "TOGETHER_API_KEY") {
		t.Fatalf("secrets.yaml does not name TOGETHER_API_KEY:\n%s", b)
	}

	// And it must read back as configured, even though config.yaml has never
	// heard of this vendor.
	st2, err := authStatus(context.Background())
	if err != nil {
		t.Fatalf("authStatus: %v", err)
	}
	found := false
	for _, up := range st2.Upstreams {
		if up.ID == "together" && up.Status == provider.AuthConfigured {
			found = true
		}
	}
	if !found {
		t.Fatalf("together not reported configured after a write: %+v", st2.Upstreams)
	}
}

// Merging, not clobbering: a second vendor's key must not erase the first.
func TestSetCredentialPreservesOtherSecrets(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".config", "goose", "secrets.yaml")
	if err := os.WriteFile(path, []byte("EXISTING_KEY: keep-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setCredential(context.Background(), "deepseek", "", "sk-deepseek", nil); err != nil {
		// custom_deepseek is the declarative id; deepseek may not exist. Fall
		// back to the id the table actually carries.
		if err2 := setCredential(context.Background(), "custom_deepseek", "", "sk-deepseek", nil); err2 != nil {
			t.Fatalf("setCredential: %v / %v", err, err2)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "EXISTING_KEY") {
		t.Fatalf("unrelated secret lost:\n%s", b)
	}
}

// Clearing removes only the vendor's own key.
func TestClearCredentialRemovesOnlyThatKey(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".config", "goose", "secrets.yaml")
	if err := credstore.SetGooseSecret(path, "TOGETHER_API_KEY", "sk-a"); err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGooseSecret(path, "XAI_API_KEY", "sk-b"); err != nil {
		t.Fatal(err)
	}
	if err := clearCredential(context.Background(), "together"); err != nil {
		t.Fatalf("clearCredential: %v", err)
	}
	names, err := credstore.ReadGooseSecretNames(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "XAI_API_KEY" {
		t.Fatalf("names = %v, want only XAI_API_KEY", names)
	}
}

// On a keyring-backed host the write cannot work, and saying so beats writing
// a file goose will never read (MADR 0074 D18).
func TestSetCredentialRefusesWhenKeyringManaged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// Unset, not empty. goose's env branch is `env::var(...).is_ok()`
	// (base.rs:206), and a set-but-empty variable is still Ok(""), so setting
	// it to "" would DISABLE the keyring — the opposite of what this test
	// needs. t.Setenv first so the original value is restored on cleanup.
	t.Setenv("GOOSE_DISABLE_KEYRING", "")
	os.Unsetenv("GOOSE_DISABLE_KEYRING")
	err := setCredential(context.Background(), "together", "", "sk-x", nil)
	if !errors.Is(err, credstore.ErrGooseKeyringManaged) {
		t.Fatalf("err = %v, want ErrGooseKeyringManaged", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".config", "goose", "secrets.yaml")); statErr == nil {
		t.Fatal("a secrets file was written on a keyring-managed host")
	}
}

// A vendor goose authenticates through another CLI's session has no key to
// take; accepting one would store a secret nothing reads.
func TestSetCredentialRefusesKeylessVendor(t *testing.T) {
	isolate(t)
	if err := setCredential(context.Background(), "chatgpt_codex", "", "sk-x", nil); err == nil {
		t.Fatal("a key was accepted for a subscription-backed vendor")
	}
}

// The switch that fixes MADR 0073 must reach a vendor whose credential arrived
// from the phone, not only ones goose's own configure flow has named.
func TestSetActiveUpstreamAcceptsPhoneConfiguredVendor(t *testing.T) {
	home := isolate(t)
	cfg := filepath.Join(home, ".config", "goose", "config.yaml")
	if err := os.WriteFile(cfg, []byte("active_provider: opencode_go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setCredential(context.Background(), "together", "", "sk-together", nil); err != nil {
		t.Fatalf("setCredential: %v", err)
	}
	if err := setActiveUpstream(context.Background(), "together"); err != nil {
		t.Fatalf("setActiveUpstream: %v", err)
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "active_provider: together") {
		t.Fatalf("active_provider not switched:\n%s", b)
	}
}

func TestSetActiveUpstreamStillRefusesUnknownVendor(t *testing.T) {
	isolate(t)
	if err := setActiveUpstream(context.Background(), "nope-not-a-provider"); err == nil {
		t.Fatal("switch to an unconfigured vendor was accepted")
	}
}

// MADR 0083 D2: a method id that is not this vendor's own api method refuses
// instead of silently writing the key anyway.
func TestSetCredentialRefusesForeignMethod(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GOOSE_DISABLE_KEYRING", "1")
	for _, m := range []string{"together:host", "openrouter:api", "bogus"} {
		if err := setCredential(context.Background(), "together", m, "sk-x", nil); !errors.Is(err, provider.ErrAuthMethodUnsupported) {
			t.Fatalf("method %q: err = %v, want ErrAuthMethodUnsupported", m, err)
		}
	}
	if err := setCredential(context.Background(), "together", "together:api", "sk-x", nil); err != nil {
		t.Fatalf("own api method refused: %v", err)
	}
}

// MADR 0083 D4: on a keyring-managed host the catalog says up front that key
// methods cannot be stored, instead of the write refusing after secret entry.
func TestCatalogMarksKeyringManagedMethods(t *testing.T) {
	cat := authCatalog(map[string]struct{}{}, true)
	checked := 0
	for _, up := range cat.Upstreams {
		for _, m := range up.Methods {
			if m.Type != provider.AuthMethodAPIKey {
				continue
			}
			checked++
			if !m.Unavailable || m.Reason != "keyring_managed" {
				t.Fatalf("%s %s: Unavailable=%v Reason=%q, want keyring_managed",
					up.ID, m.ID, m.Unavailable, m.Reason)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no api-key methods checked")
	}

	// A file-store host keeps them available.
	for _, up := range authCatalog(map[string]struct{}{}, false).Upstreams {
		for _, m := range up.Methods {
			if m.Type == provider.AuthMethodAPIKey && m.Unavailable {
				t.Fatalf("%s %s marked unavailable on a file-store host", up.ID, m.ID)
			}
		}
	}
}
