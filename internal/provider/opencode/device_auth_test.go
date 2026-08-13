package opencode

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

// The same engine family answers both agents; the fixture mirrors kilo's
// live-captured shapes (MADR 0074 §7.1) with opencode's connected-provider
// document (MADR 0083 D3).
const deviceAuthorize = `{
  "url": "https://app.example.ai/device-auth?code=AB3C-9XYZ",
  "method": "auto",
  "instructions": "Open https://app.example.ai/device-auth and enter code: AB3C-9XYZ"
}`

const browserAuthorize = `{
  "url": "https://auth.openai.com/oauth/authorize?response_type=code&client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&state=y",
  "method": "auto",
  "instructions": "Complete authorization in your browser. This window will close automatically."
}`

func deviceAPI(authorize, upstream string, connectAfter int32) func(context.Context, string, string, any, any) error {
	var polls int32
	return func(_ context.Context, method, path string, _, out any) error {
		switch {
		case method == "POST" && strings.Contains(path, "/oauth/authorize"):
			return json.Unmarshal([]byte(authorize), out)
		case path == "/config/providers":
			n := atomic.AddInt32(&polls, 1)
			if n >= connectAfter {
				return json.Unmarshal(
					[]byte(`{"providers":[{"id":"`+upstream+`"}],"default":{}}`), out)
			}
			return json.Unmarshal([]byte(`{"providers":[],"default":{}}`), out)
		}
		return fmt.Errorf("unexpected %s %s", method, path)
	}
}

// The gap MADR 0083 A2 records: this used to be ErrAuthUnsupported for every
// opencode vendor. Now a device flow yields a code the phone can display.
func TestStartDeviceAuthReturnsCode(t *testing.T) {
	api := deviceAPI(deviceAuthorize, "anthropic", 1)
	d := newDialect()

	flow, wait, err := d.StartDeviceAuth(context.Background(), api, "anthropic", "anthropic:0", nil, false)
	if err != nil {
		t.Fatalf("StartDeviceAuth: %v", err)
	}
	if flow.UserCode != "AB3C-9XYZ" {
		t.Errorf("user code = %q", flow.UserCode)
	}
	if wait == nil {
		t.Fatal("no wait function returned")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := wait(ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

// MADR 0074 D7 at the provider boundary: a browser flow refuses rather than
// rendering a device sheet with an empty code.
func TestStartDeviceAuthRefusesBrowserFlow(t *testing.T) {
	api := deviceAPI(browserAuthorize, "openai", 1)
	d := newDialect()

	_, _, err := d.StartDeviceAuth(context.Background(), api, "openai", "openai:0", nil, false)
	if !errors.Is(err, provider.ErrAuthUnsupported) {
		t.Fatalf("err = %v, want ErrAuthUnsupported", err)
	}
}
