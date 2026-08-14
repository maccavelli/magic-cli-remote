package kilo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// authFixture is the real GET /provider/auth body from kilo 7.4.20, captured
// 2026-08-10. It contains no credentials — the catalog describes how to
// authenticate, never what the host already holds.
func authFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "provider-auth-7.4.20.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// apiFrom serves the given path→body map and errors on anything else, so a
// test fails loudly if the probe starts fetching an endpoint it should not.
func apiFrom(t *testing.T, bodies map[string]string) func(context.Context, string, string, any, any) error {
	t.Helper()
	return func(_ context.Context, _, path string, _, out any) error {
		body, ok := bodies[path]
		if !ok {
			return fmt.Errorf("unexpected fetch of %s", path)
		}
		return json.Unmarshal([]byte(body), out)
	}
}

// The catalog shape MADR 0074 §7.3 depends on: 13 upstreams, and the eight
// methods that declare prompt inputs must survive into AuthMethod.Inputs. A
// protocol that models a method as {type,label} silently drops all of these.
func TestAuthStatusMapsCatalogInputs(t *testing.T) {
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), apiFrom(t, map[string]string{
		"/provider/auth":    authFixture(t),
		"/config/providers": `{"providers":[{"id":"opencode-go"}]}`,
	}))
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	if len(st.Upstreams) < 13 {
		t.Fatalf("got %d upstreams, want the catalog's 13", len(st.Upstreams))
	}

	withInputs := map[string][]string{}
	for _, up := range st.Upstreams {
		for _, m := range up.Methods {
			if len(m.Inputs) == 0 {
				continue
			}
			keys := make([]string, 0, len(m.Inputs))
			for _, in := range m.Inputs {
				keys = append(keys, in.Key)
			}
			withInputs[up.ID] = append(withInputs[up.ID], keys...)
		}
	}
	// Every upstream the MADR tabulated as needing a form must still need one.
	for _, want := range []struct {
		upstream string
		key      string
	}{
		{"github-copilot", "deploymentType"},
		{"gitlab", "instanceUrl"},
		{"azure", "resourceName"},
		{"snowflake-cortex", "account"},
		{"cloudflare-ai-gateway", "gatewayId"},
		{"cloudflare-workers-ai", "accountId"},
	} {
		keys, ok := withInputs[want.upstream]
		if !ok {
			t.Errorf("%s declares no inputs; MADR 0074 §7.3 says it must", want.upstream)
			continue
		}
		found := false
		for _, k := range keys {
			if k == want.key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s inputs %v missing %q", want.upstream, keys, want.key)
		}
	}
}

// A select prompt must arrive as a select with its options, not as free text —
// the phone renders a dropdown off exactly this.
func TestAuthStatusPreservesSelectOptions(t *testing.T) {
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), apiFrom(t, map[string]string{
		"/provider/auth":    authFixture(t),
		"/config/providers": `{"providers":[]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var checked bool
	for _, up := range st.Upstreams {
		if up.ID != "github-copilot" {
			continue
		}
		for _, m := range up.Methods {
			for _, in := range m.Inputs {
				if in.Key != "deploymentType" {
					continue
				}
				checked = true
				if in.Type != "select" {
					t.Errorf("deploymentType is %q, want select", in.Type)
				}
				if len(in.Options) == 0 {
					t.Error("deploymentType lost its options")
				}
			}
		}
	}
	if !checked {
		t.Fatal("github-copilot deploymentType not present in the mapped catalog")
	}
}

// Catalog classification is a hint, not MADR 0074 D7 — but the hint still has
// to be right about the case the MADR called out, or the phone offers a
// "sign in" button that cannot work headlessly. OpenAI publishes both a
// browser and a headless ChatGPT method; they must not classify alike.
func TestAuthStatusSplitsBrowserFromDeviceOAuth(t *testing.T) {
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), apiFrom(t, map[string]string{
		"/provider/auth":    authFixture(t),
		"/config/providers": `{"providers":[]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var browser, device int
	for _, up := range st.Upstreams {
		if up.ID != "openai" {
			continue
		}
		for _, m := range up.Methods {
			switch m.Type {
			case "oauth_browser":
				browser++
				if !strings.Contains(strings.ToLower(m.Label), "browser") {
					t.Errorf("classified %q as browser without a browser marker", m.Label)
				}
			case "oauth_device":
				device++
			}
		}
	}
	if browser == 0 || device == 0 {
		t.Fatalf("openai methods classified browser=%d device=%d, want both present", browser, device)
	}
}

// SECURITY (MADR 0074 D2): GET /config/providers returns the plaintext API key
// for every connected provider. The probe reads only ids from it, so no key
// may appear anywhere in the returned state — this is the regression guard for
// a well-meaning "just decode the whole response" refactor.
func TestAuthStatusNeverCarriesKeyMaterial(t *testing.T) {
	const secret = "sk-DO-NOT-LEAK-0123456789"
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), apiFrom(t, map[string]string{
		"/provider/auth": `{"opencode-go":[{"type":"api","label":"API key"}]}`,
		"/config/providers": fmt.Sprintf(
			`{"providers":[{"id":"opencode-go","name":"OpenCode Go","source":"api","key":%q}]}`, secret),
	}))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), secret) {
		t.Fatalf("auth state carried key material: %s", blob)
	}
	if strings.Contains(string(blob), "sk-") {
		t.Fatalf("auth state carried something key-shaped: %s", blob)
	}
}

// The engine being down is the normal cold-host case, not an error: the phone
// still needs to know which upstreams have credentials, which auth.json knows.
func TestAuthStatusFallsBackToDiskWhenEngineDown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	dir := filepath.Join(home, ".local", "share", "kilo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"),
		[]byte(`{"kilo":{"type":"oauth"},"opencode-go":{"type":"api","key":"sk-secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	d := newDialect()
	down := func(context.Context, string, string, any, any) error {
		return fmt.Errorf("kilo server not running")
	}
	st, err := d.AuthStatus(context.Background(), down)
	if err != nil {
		t.Fatalf("a cold host must not fail the listing: %v", err)
	}
	if st.Status != "configured" {
		t.Fatalf("status = %q, want configured from the on-disk store", st.Status)
	}
	got := map[string]string{}
	for _, up := range st.Upstreams {
		got[up.ID] = up.Status
	}
	for _, id := range []string{"kilo", "opencode-go"} {
		if got[id] != "configured" {
			t.Errorf("%s = %q, want configured", id, got[id])
		}
	}
	blob, _ := json.Marshal(st)
	if strings.Contains(string(blob), "sk-secret") {
		t.Fatalf("disk fallback leaked a key: %s", blob)
	}
}

// An upstream in the catalog with no credential is "missing" — the state the
// setup sheet exists to resolve.
func TestAuthStatusMarksUnconfiguredUpstreamsMissing(t *testing.T) {
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), apiFrom(t, map[string]string{
		"/provider/auth":    `{"anthropic":[{"type":"api","label":"API key"}]}`,
		"/config/providers": `{"providers":[]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Upstreams) != 1 || st.Upstreams[0].Status != "missing" {
		t.Fatalf("got %+v, want one missing upstream", st.Upstreams)
	}
	if st.Status != "missing" {
		t.Fatalf("aggregate status = %q, want missing", st.Status)
	}
}

// MADR 0074 D16: the status block answers "what is configured", the catalog
// answers "what could be". Before D16 kilo offered only the 13 upstreams in
// GET /provider/auth, so the ~170 key-only vendors — togetherai, deepseek,
// anthropic — could not be given a key from the phone at all.
func TestAuthCatalogListCoversKeyOnlyVendors(t *testing.T) {
	d := newDialect()
	cat, err := d.AuthCatalogList(context.Background(), apiFrom(t, map[string]string{
		"/provider":      providerFixture(t),
		"/provider/auth": authFixture(t),
	}))
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	byID := map[string]int{}
	for _, up := range cat.Upstreams {
		byID[up.ID] = len(up.Methods)
	}
	for _, want := range []string{"togetherai", "deepseek", "anthropic"} {
		if byID[want] == 0 {
			t.Errorf("catalog is missing %q, the class of vendor D16 exists for", want)
		}
	}
	// And the typed methods from /provider/auth must survive the merge.
	var copilotInputs int
	for _, up := range cat.Upstreams {
		if up.ID == "github-copilot" {
			for _, m := range up.Methods {
				copilotInputs += len(m.Inputs)
			}
		}
	}
	if copilotInputs == 0 {
		t.Error("github-copilot lost its declared inputs in the catalog merge")
	}
}

// providerFixture is the real GET /provider body from kilo 7.4.21 (2026-08-12),
// trimmed to ten vendors: the live answer carries 185 and several megabytes of
// model metadata.
func providerFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "provider-7.4.21.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// MADR 0083 D2: typed prompt answers ride in ApiAuth metadata, and a method
// that is not an API key refuses the key-write path.
func TestSetCredentialCarriesInputsAndGuardsMethod(t *testing.T) {
	var gotBody any
	api := func(_ context.Context, method, path string, body, out any) error {
		if method == "GET" && path == "/provider/auth" {
			return json.Unmarshal(
				[]byte(`{"azure":[{"type":"api","label":"API key","prompts":[{"key":"resourceName","message":"m","type":"text"}]}],"kilo":[{"type":"oauth","label":"Sign in"}]}`),
				out,
			)
		}
		gotBody = body
		return nil
	}
	d := &httpDialect{log: slog.Default()}
	err := d.SetCredential(context.Background(), api, "azure", "azure:0", "sk-live",
		map[string]string{"resourceName": "my-models"})
	if err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	md, ok := gotBody.(map[string]any)["metadata"].(map[string]string)
	if !ok || md["resourceName"] != "my-models" {
		t.Fatalf("body = %#v, want metadata.resourceName", gotBody)
	}

	err = d.SetCredential(context.Background(), api, "kilo", "kilo:0", "sk-live", nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("oauth method err = %v, want ErrAuthMethodUnsupported", err)
	}
	err = d.SetCredential(context.Background(), api, "azure", "deepseek:0", "sk-live", nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("foreign method err = %v, want ErrAuthMethodUnsupported", err)
	}
}

func TestSetCredentialFileWritesKiloAuthJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	d := newDialect()
	if err := d.SetCredentialFile("togetherai", "togetherai:api", "sk-live", nil); err != nil {
		t.Fatalf("SetCredentialFile: %v", err)
	}
	path := filepath.Join(home, ".local", "share", "kilo", "auth.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"togetherai"`) {
		t.Fatalf("auth.json missing togetherai: %s", b)
	}
	if err := d.ClearCredentialFile("togetherai"); err != nil {
		t.Fatal(err)
	}
}
