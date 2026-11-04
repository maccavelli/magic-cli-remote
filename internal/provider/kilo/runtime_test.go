package kilo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestKiloImplementsRuntimeDialect(t *testing.T) {
	var d any = &httpDialect{}
	if _, ok := d.(httpagent.RuntimeDialect); !ok {
		t.Fatal("kilo's dialect does not implement httpagent.RuntimeDialect")
	}
}

// TestStatusPassesKiloProviderUsageThrough is 0208-PLAN P5 step 3 for kilo
// `/status` (Deviation 5): kilo 7.8.18's answer to GET /kilocode/provider-usage,
// byte for byte, named by the request that got it, and nothing else asked.
func TestStatusPassesKiloProviderUsageThrough(t *testing.T) {
	captured, err := os.ReadFile("testdata/wire/7.8.18/provider-usage.json")
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	captured = bytes.TrimSpace(captured)
	var calls []string
	api := func(_ context.Context, method, path string, _, out any) error {
		calls = append(calls, method+" "+path)
		return json.Unmarshal(captured, out)
	}
	res, err := (&httpDialect{}).RuntimeStatus(context.Background(), api)
	if err != nil {
		t.Fatalf("RuntimeStatus: %v", err)
	}
	if len(calls) != 1 || calls[0] != "GET /kilocode/provider-usage" {
		t.Fatalf("engine calls = %v, want exactly [GET /kilocode/provider-usage]", calls)
	}
	if !bytes.Equal(res.Body, captured) {
		t.Errorf("body = %s\nwant kilo's answer unchanged: %s", res.Body, captured)
	}
	if res.Method != "GET /kilocode/provider-usage" || res.Text != "" {
		t.Errorf("method = %q, text = %q; want the request that answered and no text", res.Method, res.Text)
	}
}

// TestStatusReturnsTheEnginesError leaves the text to httpagent, which shows a
// failed read rather than nothing (its TestStatusReportsADialectFailureAsText).
func TestStatusReturnsTheEnginesError(t *testing.T) {
	want := errors.New("engine refused: 503")
	api := func(context.Context, string, string, any, any) error { return want }
	if _, err := (&httpDialect{}).RuntimeStatus(context.Background(), api); !errors.Is(err, want) {
		t.Fatalf("err = %v, want the engine's error", err)
	}
}
