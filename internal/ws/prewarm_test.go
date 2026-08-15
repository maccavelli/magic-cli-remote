package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

type prewarmEngine struct {
	ensures   int
	shutdowns int
}

func (p *prewarmEngine) ID() provider.ID { return provider.IDKilo }
func (p *prewarmEngine) Ready() bool     { return true }
func (p *prewarmEngine) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	return nil, provider.ErrNotImplemented
}
func (p *prewarmEngine) EnsureServer() { p.ensures++ }
func (p *prewarmEngine) Shutdown()     { p.shutdowns++ }

func startPrewarmServer(t *testing.T, liveCount func(provider.ID) int) (*authWS, *prewarmEngine, *provider.Controller) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("providers:\n  kilo:\n    prewarm: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("probe")
	if err != nil {
		t.Fatal(err)
	}
	eng := &prewarmEngine{}
	reg := provider.NewRegistry()
	reg.Register(eng)
	cfg := config.Defaults()
	live := &config.Live{Path: cfgPath, Cfg: &cfg}
	if liveCount == nil {
		liveCount = func(provider.ID) int { return 0 }
	}
	ctrl := provider.NewController(live, reg, liveCount)
	var srv *ws.Server
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		if srv != nil {
			srv.BroadcastEvent(ev)
		}
	})
	srv = ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		Prewarm:            ctrl,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "auth", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s %s", got.Type, got.Payload)
	}
	return &authWS{conn: conn, ts: ts}, eng, ctrl
}

func TestSetPrewarmTrueListsAndEnsures(t *testing.T) {
	w, eng, _ := startPrewarmServer(t, nil)
	defer w.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env, _ := protocol.NewEnvelope(protocol.TypeProvidersSetPrewarm, "sp", protocol.ProvidersSetPrewarmPayload{
		ProviderID: "kilo",
		Prewarm:    true,
	})
	writeEnv(ctx, t, w.conn, env)
	got := readUntil(ctx, t, w.conn, protocol.TypeOK)
	var body protocol.ProvidersPrewarmPayload
	if err := json.Unmarshal(got.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Engine != protocol.EngineRunning || !body.Prewarm {
		t.Fatalf("ok body = %+v", body)
	}
	if eng.ensures != 1 {
		t.Fatalf("ensures = %d", eng.ensures)
	}

	// The setter also pushes providers.prewarm to every client, the requester
	// included (D7), so a second phone tracks the change. Consume and check it
	// here — leaving it queued would desync every later read on this socket.
	push := readUntil(ctx, t, w.conn, protocol.TypeProvidersPrewarm)
	var pushBody protocol.ProvidersPrewarmPayload
	if err := json.Unmarshal(push.Payload, &pushBody); err != nil {
		t.Fatal(err)
	}
	if pushBody != body {
		t.Fatalf("push body = %+v, want %+v", pushBody, body)
	}

	res, _ := listProviders(t, w, protocol.V1)
	var saw bool
	for _, p := range res.Providers {
		if p.ID == "kilo" {
			saw = true
			if !p.Prewarm {
				t.Fatal("list still shows prewarm false")
			}
		}
	}
	if !saw {
		t.Fatal("kilo missing from list")
	}
}

func TestSetPrewarmFalseWhileLiveLatches(t *testing.T) {
	w, eng, _ := startPrewarmServer(t, func(provider.ID) int { return 1 })
	defer w.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env, _ := protocol.NewEnvelope(protocol.TypeProvidersSetPrewarm, "sp", protocol.ProvidersSetPrewarmPayload{
		ProviderID: "kilo",
		Prewarm:    false,
	})
	writeEnv(ctx, t, w.conn, env)
	got := readUntil(ctx, t, w.conn, protocol.TypeOK)
	var body protocol.ProvidersPrewarmPayload
	if err := json.Unmarshal(got.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Engine != protocol.EngineStoppingWhenIdle {
		t.Fatalf("engine = %q", body.Engine)
	}
	if eng.shutdowns != 0 {
		t.Fatalf("shutdowns = %d", eng.shutdowns)
	}
}

func TestSetPrewarmUnknownProvider(t *testing.T) {
	w, _, _ := startPrewarmServer(t, nil)
	defer w.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, _ := protocol.NewEnvelope(protocol.TypeProvidersSetPrewarm, "sp", protocol.ProvidersSetPrewarmPayload{
		ProviderID: "nope",
		Prewarm:    true,
	})
	writeEnv(ctx, t, w.conn, env)
	got := readUntil(ctx, t, w.conn, protocol.TypeError)
	var ep protocol.ErrorPayload
	if err := json.Unmarshal(got.Payload, &ep); err != nil {
		t.Fatal(err)
	}
	if ep.Code != "unknown_provider" {
		t.Fatalf("code = %q", ep.Code)
	}
}

func TestSetPrewarmMissingConfig(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("probe")
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(&prewarmEngine{})
	cfg := config.Defaults()
	live := &config.Live{Path: "", Cfg: &cfg}
	ctrl := provider.NewController(live, reg, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		Registry:           reg,
		Prewarm:            ctrl,
		RequireDeviceToken: true,
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "auth", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}
	env, _ = protocol.NewEnvelope(protocol.TypeProvidersSetPrewarm, "sp", protocol.ProvidersSetPrewarmPayload{
		ProviderID: "kilo",
		Prewarm:    true,
	})
	writeEnv(ctx, t, conn, env)
	got := readUntil(ctx, t, conn, protocol.TypeError)
	var ep protocol.ErrorPayload
	if err := json.Unmarshal(got.Payload, &ep); err != nil {
		t.Fatal(err)
	}
	if ep.Code != "config_write_failed" {
		t.Fatalf("code = %q msg=%q", ep.Code, ep.Message)
	}
}

func readUntil(ctx context.Context, t *testing.T, conn *websocket.Conn, typ string) protocol.Envelope {
	t.Helper()
	for {
		got := readEnv(ctx, t, conn)
		if got.Type == protocol.TypeEvent || got.Type == protocol.TypeProvidersPrewarm {
			if got.Type == typ {
				return got
			}
			continue
		}
		if got.Type != typ {
			t.Fatalf("want %s, got %s %s", typ, got.Type, got.Payload)
		}
		return got
	}
}
