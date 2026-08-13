package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The two fixtures are the real bodies from opencode 1.18.16, captured
// 2026-08-12 while resolving MADR 0074 D16. `provider-1.18.16.json` is
// trimmed to twelve vendors (the live answer carries 184 and 4.7 MB of model
// metadata); the auth-method catalog is kept verbatim because it is small.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// apiFrom serves the given path→body map and errors on anything else, so a
// probe that starts fetching an endpoint it should not fails loudly.
func apiFrom(t *testing.T, bodies map[string]string) func(context.Context, string, string, any, any) error {
	t.Helper()
	return func(_ context.Context, _, path string, _, out any) error {
		body, ok := bodies[path]
		if !ok {
			return fmt.Errorf("unexpected fetch of %s", path)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal([]byte(body), out)
	}
}

// engineAPI serves the endpoints the catalog path uses.
func engineAPI(t *testing.T) func(context.Context, string, string, any, any) error {
	t.Helper()
	return apiFrom(t, map[string]string{
		"/provider":      fixture(t, "provider-1.18.16.json"),
		"/provider/auth": fixture(t, "provider-auth-1.18.16.json"),
	})
}

// statusAPI serves what the *status* path uses. It deliberately does not serve
// `/provider`: status must never pull the multi-megabyte vendor catalog, and
// apiFrom fails the test if it tries.
func statusAPI(t *testing.T) func(context.Context, string, string, any, any) error {
	t.Helper()
	return apiFrom(t, map[string]string{
		"/config/providers": fixture(t, "config-providers-1.18.16.json"),
		"/provider/auth":    fixture(t, "provider-auth-1.18.16.json"),
	})
}

// The gap MADR 0074 D16 closes: before it, a phone could only ever re-key a
// vendor OpenCode already had a credential for. togetherai — a plain API-key
// vendor with no entry in /provider/auth — must be configurable.
func TestAuthCatalogListCoversKeyOnlyVendors(t *testing.T) {
	d := newDialect()
	cat, err := d.AuthCatalogList(context.Background(), engineAPI(t))
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	if cat.Source != provider.AuthCatalogSourceEngine {
		t.Errorf("source = %q, want engine", cat.Source)
	}
	byID := map[string]provider.UpstreamAuth{}
	for _, up := range cat.Upstreams {
		byID[up.ID] = up
	}
	for _, want := range []string{"togetherai", "deepseek", "anthropic", "groq"} {
		up, ok := byID[want]
		if !ok {
			t.Fatalf("catalog is missing %q; D16 exists to make exactly these reachable", want)
		}
		if len(up.Methods) != 1 || up.Methods[0].Type != provider.AuthMethodAPIKey {
			t.Errorf("%s methods = %+v, want a single api_key method", want, up.Methods)
		}
	}
	if got := byID["togetherai"].Label; got != "Together AI" {
		t.Errorf("togetherai label = %q, want the vendor's own display name", got)
	}
}

// Vendors whose auth is more than a key keep their typed methods and inputs in
// the catalog — the catalog must not flatten everything to "API key".
func TestAuthCatalogListKeepsTypedMethods(t *testing.T) {
	d := newDialect()
	cat, err := d.AuthCatalogList(context.Background(), engineAPI(t))
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	var copilot provider.UpstreamAuth
	for _, up := range cat.Upstreams {
		if up.ID == "github-copilot" {
			copilot = up
		}
	}
	if len(copilot.Methods) == 0 {
		t.Fatal("github-copilot has no methods")
	}
	if copilot.Methods[0].Type != provider.AuthMethodOAuthDevice {
		t.Errorf("copilot method type = %q, want oauth_device", copilot.Methods[0].Type)
	}
	keys := map[string]bool{}
	for _, in := range copilot.Methods[0].Inputs {
		keys[in.Key] = true
	}
	if !keys["deploymentType"] || !keys["enterpriseUrl"] {
		t.Errorf("copilot inputs = %v, want deploymentType and enterpriseUrl", keys)
	}
}

// OpenAI declares a browser flow and a headless one, and only the second can
// run without a loopback listener. Mixing them up would offer a sign-in button
// that fails the instant it is pressed (MADR 0074 D7's catalog-time hint).
func TestAuthCatalogSplitsBrowserFromDeviceOAuth(t *testing.T) {
	d := newDialect()
	cat, err := d.AuthCatalogList(context.Background(), engineAPI(t))
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	var types []string
	for _, up := range cat.Upstreams {
		if up.ID != "openai" {
			continue
		}
		for _, m := range up.Methods {
			types = append(types, m.Type)
		}
	}
	want := []string{provider.AuthMethodOAuthBrowser, provider.AuthMethodOAuthDevice, provider.AuthMethodAPIKey}
	if len(types) != len(want) {
		t.Fatalf("openai methods = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("openai methods = %v, want %v", types, want)
		}
	}
}

// Status is the small block that rides on every providers.list. It must report
// what is configured — including env-keyed vendors that never touch auth.json —
// and must not turn into the 184-entry catalog.
func TestAuthStatusFromEngineReportsConnected(t *testing.T) {
	// Clear the host's own env keys so a pass cannot come from the disk
	// fallback: this test is about the engine path.
	for env := range envUpstreams {
		t.Setenv(env, "")
	}
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), statusAPI(t))
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	got := map[string]string{}
	for _, up := range st.Upstreams {
		got[up.ID] = up.Status
	}
	// The fixture's connected set, including the two supplied through the
	// environment on the probed host.
	for _, id := range []string{"opencode-go", "openrouter", "huggingface"} {
		if got[id] != provider.AuthConfigured {
			t.Errorf("%s = %q, want configured", id, got[id])
		}
	}
	if _, present := got["togetherai"]; present {
		t.Error("status carried an unconfigured key-only vendor; that belongs in the catalog (D16)")
	}
	if st.Status != provider.AuthConfigured {
		t.Errorf("status = %q, want configured", st.Status)
	}
	// Labels come from the engine, not from the bare id — proof the engine
	// path ran rather than the on-disk fallback.
	for _, up := range st.Upstreams {
		if up.ID == "opencode-go" && up.Label == "opencode-go" {
			t.Error("opencode-go carries no engine-supplied label; the disk fallback ran")
		}
	}
}

// Status must not fetch the vendor catalog: it runs on every providers.list,
// and `GET /provider` is 4.7 MB on 1.18.16. apiFrom fails on any unexpected
// path, so serving only the two status endpoints is the assertion.
func TestAuthStatusDoesNotFetchTheVendorCatalog(t *testing.T) {
	d := newDialect()
	if _, err := d.AuthStatus(context.Background(), statusAPI(t)); err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
}

// No engine, no problem: chips still come from auth.json plus the env map.
func TestAuthStatusFallsBackToDiskWhenEngineDown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	// The env half of the fallback is exercised elsewhere; clear the vars this
	// developer host happens to export so the assertion is about auth.json.
	for env := range envUpstreams {
		t.Setenv(env, "")
	}
	dir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const key = "sk-must-never-appear"
	body := `{"opencode-go":{"type":"api","key":"` + key + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	down := func(context.Context, string, string, any, any) error {
		return fmt.Errorf("no engine")
	}
	d := newDialect()
	st, err := d.AuthStatus(context.Background(), down)
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	if len(st.Upstreams) != 1 || st.Upstreams[0].ID != "opencode-go" {
		t.Fatalf("upstreams = %+v, want the one entry from auth.json", st.Upstreams)
	}
	blob, _ := json.Marshal(st)
	if strings.Contains(string(blob), key) {
		t.Fatal("auth state carried key material (MADR 0074 D2)")
	}
}

// The engine write path is what makes a key-only vendor configurable without
// restarting anything (D9). Assert the exact call, and that the key rides in
// the body rather than the path.
func TestSetCredentialUsesEngineAPI(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody any
	api := func(_ context.Context, method, path string, body, _ any) error {
		gotMethod, gotPath, gotBody = method, path, body
		return nil
	}
	d := newDialect()
	if err := d.SetCredential(context.Background(), api, "togetherai", "togetherai:api", "sk-live", nil); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	if gotMethod != "PUT" || gotPath != "/auth/togetherai" {
		t.Fatalf("call = %s %s, want PUT /auth/togetherai", gotMethod, gotPath)
	}
	m, ok := gotBody.(map[string]any)
	if !ok || m["type"] != "api" || m["key"] != "sk-live" {
		t.Fatalf("body = %#v, want {type:api, key:…}", gotBody)
	}
	if _, has := m["metadata"]; has {
		t.Fatalf("no inputs given, but body carries metadata: %#v", gotBody)
	}
}

// MADR 0083 D2: typed prompt answers ride in ApiAuth metadata — before this
// the daemon dropped them and azure-class vendors silently mis-stored.
func TestSetCredentialCarriesInputsAsMetadata(t *testing.T) {
	var gotBody any
	api := func(_ context.Context, method, path string, body, out any) error {
		if method == "GET" && path == "/provider/auth" {
			return json.Unmarshal(
				[]byte(`{"azure":[{"type":"api","label":"API key","prompts":[{"key":"resourceName","message":"Enter Azure Resource Name","type":"text"}]}]}`),
				out,
			)
		}
		gotBody = body
		return nil
	}
	d := newDialect()
	err := d.SetCredential(context.Background(), api, "azure", "azure:0", "sk-live",
		map[string]string{"resourceName": "my-models"})
	if err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	m := gotBody.(map[string]any)
	md, ok := m["metadata"].(map[string]string)
	if !ok || md["resourceName"] != "my-models" {
		t.Fatalf("body = %#v, want metadata.resourceName", gotBody)
	}
}

// MADR 0083 D2: an oauth-typed method must refuse the key-write path instead
// of storing an api-shaped credential for it.
func TestSetCredentialRefusesOAuthMethod(t *testing.T) {
	api := func(_ context.Context, method, path string, _, out any) error {
		if method == "GET" && path == "/provider/auth" {
			return json.Unmarshal(
				[]byte(`{"anthropic":[{"type":"oauth","label":"Claude Pro/Max"},{"type":"api","label":"API key"}]}`),
				out,
			)
		}
		t.Fatalf("unexpected engine call %s %s", method, path)
		return nil
	}
	d := newDialect()
	err := d.SetCredential(context.Background(), api, "anthropic", "anthropic:0", "sk-live", nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("err = %v, want ErrAuthMethodUnsupported", err)
	}
	// The api-typed sibling at index 1 goes through.
	called := false
	api2 := func(_ context.Context, method, path string, _, out any) error {
		if method == "GET" && path == "/provider/auth" {
			return json.Unmarshal(
				[]byte(`{"anthropic":[{"type":"oauth","label":"Claude Pro/Max"},{"type":"api","label":"API key"}]}`),
				out,
			)
		}
		called = true
		return nil
	}
	if err := d.SetCredential(context.Background(), api2, "anthropic", "anthropic:1", "sk-live", nil); err != nil {
		t.Fatalf("api-typed method refused: %v", err)
	}
	if !called {
		t.Fatal("engine write never happened")
	}
}

// A method id from another upstream refuses before any engine call.
func TestSetCredentialRefusesForeignMethod(t *testing.T) {
	api := func(_ context.Context, method, path string, _, _ any) error {
		t.Fatalf("unexpected engine call %s %s", method, path)
		return nil
	}
	d := newDialect()
	err := d.SetCredential(context.Background(), api, "togetherai", "deepseek:0", "sk-live", nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("err = %v, want ErrAuthMethodUnsupported", err)
	}
}

func TestSetCredentialRejectsEmptySecretBeforeAnyCall(t *testing.T) {
	called := false
	api := func(context.Context, string, string, any, any) error {
		called = true
		return nil
	}
	d := newDialect()
	if err := d.SetCredential(context.Background(), api, "togetherai", "", "   ", nil); err == nil {
		t.Fatal("empty secret accepted")
	}
	if called {
		t.Fatal("engine was called with an invalid secret")
	}
}

func TestClearCredentialUsesEngineAPI(t *testing.T) {
	var gotMethod, gotPath string
	api := func(_ context.Context, method, path string, _, _ any) error {
		gotMethod, gotPath = method, path
		return nil
	}
	d := newDialect()
	if err := d.ClearCredential(context.Background(), api, "togetherai"); err != nil {
		t.Fatalf("ClearCredential: %v", err)
	}
	if gotMethod != "DELETE" || gotPath != "/auth/togetherai" {
		t.Fatalf("call = %s %s, want DELETE /auth/togetherai", gotMethod, gotPath)
	}
}
