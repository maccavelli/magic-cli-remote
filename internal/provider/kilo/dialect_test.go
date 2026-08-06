package kilo

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func newTestDialect(pure bool) *httpDialect {
	return &httpDialect{log: slog.Default(), pure: pure}
}

func TestServeArgs(t *testing.T) {
	d := newTestDialect(false)
	got := strings.Join(d.ServeArgs(4321), " ")
	want := "serve --hostname 127.0.0.1 --port 4321"
	if got != want {
		t.Fatalf("ServeArgs = %q, want %q", got, want)
	}
	// --mdns must never appear: it rebinds the un-gated engine to 0.0.0.0
	// (MADR 0075 §2.3 / plan PD1).
	if strings.Contains(got, "mdns") {
		t.Fatalf("ServeArgs must not enable mDNS: %q", got)
	}
}

func TestServeArgsPure(t *testing.T) {
	d := newTestDialect(true)
	got := strings.Join(d.ServeArgs(9), " ")
	want := "serve --hostname 127.0.0.1 --port 9 --pure"
	if got != want {
		t.Fatalf("ServeArgs = %q, want %q", got, want)
	}
}

// Frames below are verbatim captures from the kilo 7.4.20 live spike
// (docs/kilo-spike-7.4.20/sse-samples.json), ids included.
const (
	frameDeltaWrapped  = `{"directory": "/Users/saxsmith/gitrepos/go/magic-cli-remote", "project": "9d6971a88a97de05c9d2896bdc517ecaa2e78ec6", "payload": {"id": "evt_fd7771c54001ldlmeGsnaoiAVl", "type": "message.part.delta", "properties": {"sessionID": "ses_02888f266ffeN1neSYGSrakhhu", "messageID": "msg_fd777134d001oQ1XMK5Fp8MCXj", "partID": "prt_fd7771c51001ZOra3OcxwTVC02", "field": "text", "delta": "The"}}}`
	frameStatusWrapped = `{"directory": "/Users/saxsmith/gitrepos/go/magic-cli-remote", "project": "9d6971a88a97de05c9d2896bdc517ecaa2e78ec6", "payload": {"id": "evt_fd777134c0011DdV4mVU6J0YtY", "type": "session.status", "properties": {"sessionID": "ses_02888f266ffeN1neSYGSrakhhu", "status": {"type": "busy"}}}}`
	frameHeartbeat     = `{"payload": {"id": "evt_fd7773549001mKQZ1yutS7fSy1", "type": "server.heartbeat", "properties": {}}}`
	// Bare per-directory /event form (no GlobalEvent wrapper).
	frameBare = `{"type": "session.idle", "properties": {"sessionID": "ses_02888f266ffeN1neSYGSrakhhu"}}`
)

func TestDecodeFrameWrapped(t *testing.T) {
	d := newTestDialect(false)
	typ, props, sid, ok := d.DecodeFrame([]byte(frameDeltaWrapped))
	if !ok || typ != "message.part.delta" {
		t.Fatalf("DecodeFrame = (%q, ok=%v), want message.part.delta", typ, ok)
	}
	if sid != "ses_02888f266ffeN1neSYGSrakhhu" {
		t.Fatalf("sessionID = %q", sid)
	}
	if !strings.Contains(string(props), `"delta"`) {
		t.Fatalf("properties lost: %s", props)
	}

	typ, _, sid, ok = d.DecodeFrame([]byte(frameStatusWrapped))
	if !ok || typ != "session.status" || sid != "ses_02888f266ffeN1neSYGSrakhhu" {
		t.Fatalf("status frame = (%q, %q, ok=%v)", typ, sid, ok)
	}
}

func TestDecodeFrameBare(t *testing.T) {
	d := newTestDialect(false)
	typ, _, sid, ok := d.DecodeFrame([]byte(frameBare))
	if !ok || typ != "session.idle" || sid != "ses_02888f266ffeN1neSYGSrakhhu" {
		t.Fatalf("bare frame = (%q, %q, ok=%v)", typ, sid, ok)
	}
}

func TestDecodeFrameHeartbeatHasNoSession(t *testing.T) {
	d := newTestDialect(false)
	typ, _, sid, ok := d.DecodeFrame([]byte(frameHeartbeat))
	if !ok || typ != "server.heartbeat" {
		t.Fatalf("heartbeat = (%q, ok=%v)", typ, ok)
	}
	if sid != "" {
		t.Fatalf("heartbeat sessionID = %q, want empty (transport drops it)", sid)
	}
}

func TestDecodeFrameRejectsNonJSON(t *testing.T) {
	d := newTestDialect(false)
	if _, _, _, ok := d.DecodeFrame([]byte("not json")); ok {
		t.Fatal("non-JSON frame decoded ok")
	}
}

func TestStaticModelsDefaultsToKiloAutoFree(t *testing.T) {
	d := newTestDialect(false)
	cat := d.StaticModels(Config{})
	if len(cat.DefaultIDs) != 1 || cat.DefaultIDs[0] != "kilo/kilo-auto/free" {
		t.Fatalf("defaults = %v, want [kilo/kilo-auto/free] (plan PD4)", cat.DefaultIDs)
	}
	if !cat.AllowCustom {
		t.Fatal("models catalog must allow custom ids (~vendor aliases, MADR 0075 Q11)")
	}
	found := false
	for _, o := range cat.Options {
		if o.ID == "openrouter/openrouter/free" {
			found = true
		}
	}
	if !found {
		t.Fatal("openrouter/openrouter/free seed missing")
	}
}

func TestStaticModelsHonorsConfigModel(t *testing.T) {
	d := newTestDialect(false)
	cat := d.StaticModels(Config{Model: "kilo/~anthropic/claude-sonnet"})
	if len(cat.DefaultIDs) != 1 || cat.DefaultIDs[0] != "kilo/~anthropic/claude-sonnet" {
		t.Fatalf("defaults = %v, want config model", cat.DefaultIDs)
	}
}

func TestStaticAgentsDefaultCode(t *testing.T) {
	d := newTestDialect(false)
	cat := d.StaticAgents(Config{})
	if len(cat.DefaultIDs) != 1 || cat.DefaultIDs[0] != "code" {
		t.Fatalf("default agent = %v, want [code] (kilo has no build)", cat.DefaultIDs)
	}
	for _, o := range cat.Options {
		switch o.ID {
		case "explore", "general", "compaction", "summary", "title":
			t.Fatalf("non-selectable agent %q must not be in the static catalog", o.ID)
		}
	}
}

func TestOnHealthyRecordsVersion(t *testing.T) {
	d := newTestDialect(false)
	if err := d.OnHealthy([]byte(`{"healthy":true,"version":"7.4.20"}`)); err != nil {
		t.Fatalf("OnHealthy: %v", err)
	}
	if v := d.EngineVersion(); v != "7.4.20" {
		t.Fatalf("EngineVersion = %q", v)
	}
	// Non-JSON body must not fail boot.
	if err := d.OnHealthy([]byte("ok")); err != nil {
		t.Fatalf("OnHealthy non-JSON: %v", err)
	}
}

func TestDialectIdentity(t *testing.T) {
	d := newTestDialect(false)
	if d.ID() != provider.IDKilo {
		t.Fatalf("ID = %q", d.ID())
	}
	if d.DefaultBin() != "kilo" {
		t.Fatalf("DefaultBin = %q", d.DefaultBin())
	}
	if d.HealthPath() != "/global/health" || d.EventsPath() != "/global/event" {
		t.Fatalf("paths = %q, %q", d.HealthPath(), d.EventsPath())
	}
}

func TestStubSessionFailsCleanly(t *testing.T) {
	d := newTestDialect(false)
	s := d.NewSession(nil)
	if _, err := s.Create(context.Background(), provider.StartOptions{}); !errors.Is(err, errSessionLoopPending) {
		t.Fatalf("Create err = %v, want errSessionLoopPending", err)
	}
	if err := s.Prompt(context.Background(), nil); !errors.Is(err, errSessionLoopPending) {
		t.Fatalf("Prompt err = %v", err)
	}
}
