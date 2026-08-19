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

func TestGrokAuthMethodsUsableSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Upstreams) != 1 {
		t.Fatalf("upstreams = %d", len(st.Upstreams))
	}
	ms := st.Upstreams[0].Methods
	if len(ms) != 3 {
		t.Fatalf("methods = %d, want 3: %+v", len(ms), ms)
	}
	byID := map[string]provider.AuthMethod{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	api, ok := byID["xai:api"]
	if !ok || api.Type != provider.AuthMethodAPIKey {
		t.Fatalf("xai:api = %+v, want api_key", api)
	}
	dev, ok := byID["xai:device"]
	if !ok || dev.Type != provider.AuthMethodOAuthDevice {
		t.Fatalf("xai:device = %+v, want oauth_device (0107 D1)", dev)
	}
	br, ok := byID["xai:browser"]
	if !ok || br.Type != provider.AuthMethodOAuthBrowser {
		t.Fatalf("xai:browser = %+v, want oauth_browser (0107 D3 host-only)", br)
	}
}

func TestAuthStatusIncludesBrowserMethod(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Upstreams) != 1 {
		t.Fatalf("upstreams = %d", len(st.Upstreams))
	}
	ms := st.Upstreams[0].Methods
	if len(ms) != 3 {
		t.Fatalf("methods = %d, want 3: %+v", len(ms), ms)
	}
	want := []struct{ id, typ string }{
		{"xai:api", provider.AuthMethodAPIKey},
		{"xai:device", provider.AuthMethodOAuthDevice},
		{"xai:browser", provider.AuthMethodOAuthBrowser},
	}
	for i, w := range want {
		if ms[i].ID != w.id || ms[i].Type != w.typ {
			t.Fatalf("method[%d] = {%s %s}, want {%s %s}", i, ms[i].ID, ms[i].Type, w.id, w.typ)
		}
		if ms[i].Unavailable {
			t.Fatalf("method %s must leave Unavailable to the transport annotator", w.id)
		}
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
