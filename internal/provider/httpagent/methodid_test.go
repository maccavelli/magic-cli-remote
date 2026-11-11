package httpagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MADR 0074 D30 and D31 (PLAN P23 step 2): no label decides a flow, and a
// kilo method id names the method by its label's slug, not its position.

// The ChatGPT browser authorize response, captured live on kilo: the engine
// says method:"auto" for browser and device flows alike, and only the
// loopback redirect_uri tells them apart (MADR 0074 D7).
const loopbackAuthorize = `{
  "url": "https://auth.openai.com/oauth/authorize?response_type=code&client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&state=y",
  "method": "auto",
  "instructions": "Complete authorization in your browser. This window will close automatically."
}`

// The Kilo Gateway device response, captured live 2026-08-10 (MADR 0074 §7.1).
const deviceAuthorize = `{
  "url": "https://app.kilo.ai/device-auth?code=RX2Y-4H7X",
  "method": "auto",
  "instructions": "Open https://app.kilo.ai/device-auth?code=RX2Y-4H7X and enter code: RX2Y-4H7X"
}`

// catalogAPI serves GET /provider/auth from catalog and POST authorize from
// authorize, recording the method index the authorize body carried.
func catalogAPI(catalog, authorize string, gotMethod *int) API {
	return func(_ context.Context, method, path string, body, out any) error {
		switch {
		case method == "GET" && path == "/provider/auth":
			return json.Unmarshal([]byte(catalog), out)
		case method == "POST" && strings.Contains(path, "/oauth/authorize"):
			if m, ok := body.(map[string]any); ok && gotMethod != nil {
				idx, _ := m["method"].(int)
				*gotMethod = idx
			}
			return json.Unmarshal([]byte(authorize), out)
		}
		return fmt.Errorf("unexpected %s %s", method, path)
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestNoLabelDecidesTheFlow (MADR 0074 §16.5 item 1): a method whose label
// says "browser" is still listed oauth_device and startable; its loopback
// authorize URL is what refuses it, at start, with the host-browser message.
func TestNoLabelDecidesTheFlow(t *testing.T) {
	const catalog = `{"vendor":[{"type":"oauth","label":"Login with a browser"},{"type":"api","label":"API key"}]}`
	ctx := context.Background()

	methods, err := FetchAuthMethods(ctx, catalogAPI(catalog, loopbackAuthorize, nil))
	if err != nil {
		t.Fatal(err)
	}
	m := methods["vendor"][0]
	if m.Type != provider.AuthMethodOAuthDevice || m.Unavailable {
		t.Fatalf("a method labelled %q is listed %+v, want a startable oauth_device: the label is not a fact about the flow", m.Label, m)
	}
	if m.ID != "vendor:login-with-a-browser" {
		t.Fatalf("method id = %q, want vendor:login-with-a-browser", m.ID)
	}

	_, _, err = StartEngineDeviceFlow(ctx, catalogAPI(catalog, loopbackAuthorize, nil), quietLog(),
		"kilo", "vendor", m.ID, nil, nil, nil)
	if err == nil {
		t.Fatal("a loopback authorize URL was accepted as a device flow")
	}
	if !errors.Is(err, provider.ErrAuthUnsupported) || !strings.Contains(err.Error(), "signs in through a browser on the host") {
		t.Fatalf("start refused with %v, want ErrAuthUnsupported saying it signs in through a browser on the host", err)
	}
}

// TestKiloMethodIDsSurviveAReorder (MADR 0074 §16.5 item 2): the phone chose a
// method from one listing; the engine's array is in another order when the
// flow starts; the method the phone chose is the one started.
func TestKiloMethodIDsSurviveAReorder(t *testing.T) {
	const listed = `{"xai":[
	  {"type":"oauth","label":"xAI Grok OAuth (SuperGrok Subscription)"},
	  {"type":"oauth","label":"xAI Grok OAuth (Headless / Remote / VPS)"},
	  {"type":"api","label":"Manually enter API Key"}]}`
	const reordered = `{"xai":[
	  {"type":"oauth","label":"xAI Grok OAuth (Headless / Remote / VPS)"},
	  {"type":"oauth","label":"xAI Grok OAuth (SuperGrok Subscription)"},
	  {"type":"api","label":"Manually enter API Key"}]}`
	ctx := context.Background()

	methods, err := FetchAuthMethods(ctx, catalogAPI(listed, deviceAuthorize, nil))
	if err != nil {
		t.Fatal(err)
	}
	chosen := methods["xai"][1] // the phone picks Headless / Remote / VPS
	if chosen.ID != "xai:xai-grok-oauth-headless-remote-vps" {
		t.Fatalf("listed id = %q", chosen.ID)
	}

	var started int = -1
	_, _, err = StartEngineDeviceFlow(ctx, catalogAPI(reordered, deviceAuthorize, &started), quietLog(),
		"kilo", "xai", chosen.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("start after a reorder: %v", err)
	}
	if started != 0 {
		t.Fatalf("authorize was sent method %d, want 0, where Headless / Remote / VPS now sits", started)
	}
}

// TestAmbiguousSlugIsRefused: two methods whose labels slug alike cannot be
// told apart, so the id is refused rather than the first match started.
func TestAmbiguousSlugIsRefused(t *testing.T) {
	const catalog = `{"vendor":[{"type":"oauth","label":"Login"},{"type":"oauth","label":"login!"}]}`
	ctx := context.Background()
	methods, err := FetchAuthMethods(ctx, catalogAPI(catalog, deviceAuthorize, nil))
	if err != nil {
		t.Fatal(err)
	}
	if a, b := methods["vendor"][0].ID, methods["vendor"][1].ID; a != b || a != "vendor:login" {
		t.Fatalf("ids = %q, %q; the fixture needs them to collide on vendor:login", a, b)
	}
	_, err = ResolveEngineMethod(ctx, catalogAPI(catalog, deviceAuthorize, nil), "vendor", "vendor:login")
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("an ambiguous slug resolved: err=%v, want ErrAuthMethodUnsupported", err)
	}
	_, _, err = StartEngineDeviceFlow(ctx, catalogAPI(catalog, deviceAuthorize, nil), quietLog(),
		"kilo", "vendor", "vendor:login", nil, nil, nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("a start on an ambiguous slug was not refused: %v", err)
	}
}

// TestIndexMethodIDsAreRefused: the pre-D31 "<upstream>:<index>" shape no
// longer names anything.
func TestIndexMethodIDsAreRefused(t *testing.T) {
	const catalog = `{"vendor":[{"type":"oauth","label":"Login"},{"type":"api","label":"API key"}]}`
	ctx := context.Background()
	for _, id := range []string{"vendor:0", "vendor:1", "vendor:-1", "vendor:nope", "other:login"} {
		if _, err := ResolveEngineMethod(ctx, catalogAPI(catalog, deviceAuthorize, nil), "vendor", id); !errors.Is(err, provider.ErrAuthMethodUnsupported) {
			t.Errorf("%q resolved: err=%v, want ErrAuthMethodUnsupported", id, err)
		}
	}
	if idx, err := ResolveEngineMethod(ctx, catalogAPI(catalog, deviceAuthorize, nil), "vendor", ""); err != nil || idx != 0 {
		t.Errorf("an empty id = %d, %v; want the first method", idx, err)
	}
	if err := VerifyAPIKeyMethod(ctx, catalogAPI(catalog, deviceAuthorize, nil), "vendor", "vendor:api-key"); err != nil {
		t.Errorf("the api method's slug id was refused for a key write: %v", err)
	}
	if err := VerifyAPIKeyMethod(ctx, catalogAPI(catalog, deviceAuthorize, nil), "vendor", "vendor:login"); !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Errorf("an oauth method was accepted for a key write: %v", err)
	}
}

func TestMethodSlug(t *testing.T) {
	for _, tc := range []struct{ typ, label, want string }{
		{"oauth", "ChatGPT Pro/Plus (browser)", "chatgpt-pro-plus-browser"},
		{"oauth", "xAI Grok OAuth (Headless / Remote / VPS)", "xai-grok-oauth-headless-remote-vps"},
		{"oauth", "Kilo Gateway (Device Authorization)", "kilo-gateway-device-authorization"},
		{"api", "  Paste PAT or bearer token manually ", "paste-pat-or-bearer-token-manually"},
		{"api", "", "api"},
		{"oauth", "(-)", "oauth"},
	} {
		if got := methodSlug(tc.typ, tc.label); got != tc.want {
			t.Errorf("methodSlug(%q, %q) = %q, want %q", tc.typ, tc.label, got, tc.want)
		}
	}
}
