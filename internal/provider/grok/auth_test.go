package grok

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// MADR 0083 D2: the key write serves exactly one method; anything else
// refuses rather than writing the config key for a device sign-in.
func TestSetCredentialGuardsMethodID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, m := range []string{"xai:device", "openai:api"} {
		if err := setCredential(context.Background(), "xai", m, "sk-x", nil); !errors.Is(err, provider.ErrAuthMethodUnsupported) {
			t.Fatalf("method %q: err = %v, want ErrAuthMethodUnsupported", m, err)
		}
	}
}

func TestSetCredentialRequiresModelInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := setCredential(context.Background(), "xai", "xai:api", "sk-x", nil); err == nil {
		t.Fatal("empty model accepted")
	}
	path, err := credstore.GrokConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if credstore.HasGrokConfigAPIKey(path) {
		t.Fatal("empty model must not write a key")
	}
}

func TestSetCredentialWritesQuotedDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := setCredential(context.Background(), "xai", "xai:api", "sk-x", map[string]string{"model": "grok-4.5"}); err != nil {
		t.Fatal(err)
	}
	path, err := credstore.GrokConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `[model."grok-4.5"]`) {
		t.Fatalf("quoted table missing: %s", b)
	}
	if strings.Contains(got, "[auth]") {
		t.Fatalf("must not write [auth]: %s", b)
	}
}

func TestResolveCredentialModelOrder(t *testing.T) {
	cfgWins := func(context.Context) (picker.Catalog, error) {
		return picker.Catalog{DefaultIDs: []string{"from-default"}, Options: []picker.Option{{ID: "from-opt"}}}, nil
	}
	id, err := resolveCredentialModel(context.Background(), cfgWins, "from-cfg")
	if err != nil || id != "from-cfg" {
		t.Fatalf("cfg pin: id=%q err=%v", id, err)
	}
	id, err = resolveCredentialModel(context.Background(), cfgWins, "")
	if err != nil || id != "from-default" {
		t.Fatalf("DefaultIDs: id=%q err=%v", id, err)
	}
	optsOnly := func(context.Context) (picker.Catalog, error) {
		return picker.Catalog{Options: []picker.Option{{ID: "from-opt"}}}, nil
	}
	id, err = resolveCredentialModel(context.Background(), optsOnly, "")
	if err != nil || id != "from-opt" {
		t.Fatalf("Options: id=%q err=%v", id, err)
	}
	empty := func(context.Context) (picker.Catalog, error) {
		return picker.Catalog{}, nil
	}
	if _, err := resolveCredentialModel(context.Background(), empty, ""); err == nil {
		t.Fatal("empty catalog must error")
	}
}

func TestAuthStatusSeesConfigKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XAI_API_KEY", "")
	_ = os.Unsetenv("XAI_API_KEY")
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := credstore.GrokConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGrokModelAPIKey(path, "grok-4.6", "sk-x"); err != nil {
		t.Fatal(err)
	}
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != provider.AuthConfigured {
		t.Fatalf("status = %q, want configured", st.Status)
	}
}

func TestAuthStatusSeesAuthJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XAI_API_KEY", "")
	_ = os.Unsetenv("XAI_API_KEY")
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}
	auth, err := credstore.GrokAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != provider.AuthConfigured {
		t.Fatalf("status = %q, want configured", st.Status)
	}
}

func TestGrokHasAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XAI_API_KEY", "")
	_ = os.Unsetenv("XAI_API_KEY")
	if grokHasAPIKey() {
		t.Fatal("empty home must not report a key")
	}
	t.Setenv("XAI_API_KEY", "sk-from-env")
	if !grokHasAPIKey() {
		t.Fatal("env key must count")
	}
	_ = os.Unsetenv("XAI_API_KEY")
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := credstore.GrokConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGrokModelAPIKey(path, "grok-4.6", "sk-x"); err != nil {
		t.Fatal(err)
	}
	if !grokHasAPIKey() {
		t.Fatal("quoted config key must count")
	}
}

func TestAuthStatusSeesEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XAI_API_KEY", "sk-from-env")
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != provider.AuthConfigured {
		t.Fatalf("status = %q, want configured", st.Status)
	}
}
