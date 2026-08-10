package kilo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The Kilo Gateway response captured live on 2026-08-10 (MADR 0074 §7.1).
const gatewayAuthorize = `{
  "url": "https://app.kilo.ai/device-auth?code=RX2Y-4H7X",
  "method": "auto",
  "instructions": "Open https://app.kilo.ai/device-auth?code=RX2Y-4H7X and enter code: RX2Y-4H7X"
}`

// The ChatGPT browser response, same engine, same "auto" method value.
const browserAuthorize = `{
  "url": "https://auth.openai.com/oauth/authorize?response_type=code&client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&state=y",
  "method": "auto",
  "instructions": "Complete authorization in your browser. This window will close automatically."
}`

// deviceAPI serves authorize once, then answers /config/providers from a
// counter so the poll can be made to succeed after N attempts.
func deviceAPI(authorize string, connectAfter int32) (func(context.Context, string, string, any, any) error, *int32) {
	var polls int32
	fn := func(_ context.Context, method, path string, _, out any) error {
		switch {
		case method == "POST" && strings.Contains(path, "/oauth/authorize"):
			return json.Unmarshal([]byte(authorize), out)
		case path == "/config/providers":
			n := atomic.AddInt32(&polls, 1)
			if n >= connectAfter {
				return json.Unmarshal([]byte(`{"providers":[{"id":"kilo"}]}`), out)
			}
			return json.Unmarshal([]byte(`{"providers":[]}`), out)
		}
		return fmt.Errorf("unexpected %s %s", method, path)
	}
	return fn, &polls
}

// The happy path: a device flow yields a code the phone can display, and the
// verification link does not carry that code (see the classifier's rationale).
func TestStartDeviceAuthReturnsCode(t *testing.T) {
	api, _ := deviceAPI(gatewayAuthorize, 1)
	d := newDialect()

	flow, wait, err := d.StartDeviceAuth(context.Background(), api, "kilo", "kilo:0", nil, false)
	if err != nil {
		t.Fatalf("StartDeviceAuth: %v", err)
	}
	if flow.UserCode != "RX2Y-4H7X" {
		t.Errorf("user code = %q", flow.UserCode)
	}
	if strings.Contains(flow.VerificationURI, "RX2Y-4H7X") {
		t.Errorf("verification URI leaks the code: %s", flow.VerificationURI)
	}
	if wait == nil {
		t.Fatal("no wait function returned")
	}
}

// MADR 0074 D7 enforced at the provider boundary: a browser flow must be
// refused rather than displayed as a device flow with an empty code. Both
// responses say method:"auto", so only the URL tells them apart.
func TestStartDeviceAuthRefusesBrowserFlow(t *testing.T) {
	api, _ := deviceAPI(browserAuthorize, 1)
	d := newDialect()

	_, _, err := d.StartDeviceAuth(context.Background(), api, "openai", "openai:0", nil, false)
	if err == nil {
		t.Fatal("a browser loopback flow was accepted as a device flow")
	}
	if !errors.Is(err, provider.ErrAuthUnsupported) {
		t.Fatalf("want ErrAuthUnsupported so the phone can explain it, got %v", err)
	}
}

// Polling stops as soon as the credential appears.
func TestAwaitCredentialCompletesOnceConfigured(t *testing.T) {
	api, polls := deviceAPI(gatewayAuthorize, 2)
	d := newDialect()
	_, wait, err := d.StartDeviceAuth(context.Background(), api, "kilo", "kilo:0", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- wait(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("wait never completed")
	}
	if atomic.LoadInt32(polls) < 2 {
		t.Errorf("completed after %d polls; expected it to wait for the second", *polls)
	}
}

// Cancelling the flow must stop the poll loop — otherwise a cancelled sign-in
// keeps hitting the engine until its code expires.
func TestAwaitCredentialStopsOnCancel(t *testing.T) {
	// connectAfter is unreachable, so only cancellation can end this.
	api, _ := deviceAPI(gatewayAuthorize, 1<<30)
	d := newDialect()
	_, wait, err := d.StartDeviceAuth(context.Background(), api, "kilo", "kilo:0", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- wait(ctx) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait ended with %v, want context.Canceled", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancel did not stop the poll loop")
	}
}

// An engine that dies mid-flow must not fail the sign-in: the transport
// respawns it, and the vendor-side flow is unaffected.
func TestAwaitCredentialToleratesEngineRestart(t *testing.T) {
	var polls int32
	api := func(_ context.Context, method, path string, _, out any) error {
		if method == "POST" && strings.Contains(path, "/oauth/authorize") {
			return json.Unmarshal([]byte(gatewayAuthorize), out)
		}
		if path == "/config/providers" {
			if atomic.AddInt32(&polls, 1) < 3 {
				return fmt.Errorf("kilo server not running")
			}
			return json.Unmarshal([]byte(`{"providers":[{"id":"kilo"}]}`), out)
		}
		return fmt.Errorf("unexpected %s %s", method, path)
	}
	d := newDialect()
	_, wait, err := d.StartDeviceAuth(context.Background(), api, "kilo", "kilo:0", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- wait(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an engine blip failed the flow: %v", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("wait never completed across the engine blip")
	}
}

func TestMethodIndexOf(t *testing.T) {
	for _, tc := range []struct {
		upstream, method string
		want             int
		wantErr          bool
	}{
		{"kilo", "kilo:0", 0, false},
		{"openai", "openai:1", 1, false},
		{"kilo", "", 0, false}, // empty means the first method
		{"kilo", "openai:0", 0, true},
		{"kilo", "kilo:abc", 0, true},
		{"kilo", "kilo:-1", 0, true},
	} {
		got, err := methodIndexOf(tc.upstream, tc.method)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s/%s: expected an error", tc.upstream, tc.method)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s: %v", tc.upstream, tc.method, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s/%s = %d, want %d", tc.upstream, tc.method, got, tc.want)
		}
	}
}

func TestStartDeviceAuthRequiresUpstream(t *testing.T) {
	api, _ := deviceAPI(gatewayAuthorize, 1)
	if _, _, err := newDialect().StartDeviceAuth(context.Background(), api, "  ", "", nil, false); err == nil {
		t.Fatal("accepted a blank upstream")
	}
}
