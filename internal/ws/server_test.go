package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

func TestWSAuthAndFakeSession(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())

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
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// healthz unauthenticated
	res, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("healthz status %d", res.StatusCode)
	}

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// auth
	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(t, ctx, conn, authEnv)
	got := readEnv(t, ctx, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s", got.Type)
	}

	// create session (may interleave with pushed session_status events)
	createEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "2", protocol.SessionCreatePayload{
		Provider: "fake",
		Name:     "demo",
	})
	writeEnv(t, ctx, conn, createEnv)
	var meta session.Meta
	for {
		got = readEnv(t, ctx, conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeSessionCreated {
			t.Fatalf("want session.created got %s payload=%s", got.Type, string(got.Payload))
		}
		if err := json.Unmarshal(got.Payload, &meta); err != nil {
			t.Fatal(err)
		}
		break
	}

	// prompt (may interleave with pushed events)
	promptEnv, _ := protocol.NewEnvelope(protocol.TypeSessionPrompt, "3", protocol.SessionPromptPayload{
		SessionID: meta.ID,
		Text:      "hi",
	})
	writeEnv(t, ctx, conn, promptEnv)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	var sawOK, sawChunk bool
	for !sawOK || !sawChunk {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			break
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case protocol.TypeOK:
			if env.ID == "3" {
				sawOK = true
			}
		case protocol.TypeEvent:
			var ep protocol.EventPayload
			_ = json.Unmarshal(env.Payload, &ep)
			if ep.Event.Type == event.TypeAssistantChunk {
				sawChunk = true
			}
		}
	}
	if !sawOK {
		t.Fatal("expected ok for prompt")
	}
	if !sawChunk {
		t.Fatal("expected assistant_message_chunk event")
	}
}

func TestWSPairClaim(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	codes, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := codes.Create("phone", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		PairCodes:          codes,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	claim, _ := protocol.NewEnvelope(protocol.TypePairClaim, "1", protocol.PairClaimPayload{
		Code: info.Display,
	})
	writeEnv(t, ctx, conn, claim)
	got := readEnv(t, ctx, conn)
	if got.Type != protocol.TypePairOK {
		t.Fatalf("want pair_ok got %s payload=%s", got.Type, string(got.Payload))
	}
	var ok protocol.PairOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.Token == "" || ok.DeviceID == "" {
		t.Fatalf("empty token/device: %+v", ok)
	}

	// Socket is authed — session.list should work without a second auth.
	listEnv, _ := protocol.NewEnvelope(protocol.TypeSessionList, "2", map[string]any{})
	writeEnv(t, ctx, conn, listEnv)
	got = readEnv(t, ctx, conn)
	if got.Type != protocol.TypeSessionListResult {
		t.Fatalf("want session.list_result got %s", got.Type)
	}

	// Code is one-shot.
	conn2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")
	writeEnv(t, ctx, conn2, claim)
	got = readEnv(t, ctx, conn2)
	if got.Type != protocol.TypePairError {
		t.Fatalf("want pair_error on reuse, got %s", got.Type)
	}
}

func writeEnv(t *testing.T, ctx context.Context, conn *websocket.Conn, env protocol.Envelope) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func readEnv(t *testing.T, ctx context.Context, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestHTTPEndpointAuth(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}

	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           session.NewManager(provider.NewRegistry(), nil, nil, nil),
		Registry:           provider.NewRegistry(),
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "100.64.0.1:7531",
		HeadscaleURL:       "https://headscale.example",
	})
	srv.SetTLSStatus("letsencrypt", true)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	get := func(t *testing.T, path, token string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body
	}

	t.Run("hello requires a device token", func(t *testing.T) {
		for _, tok := range []string{"", "mcr_not-a-real-token"} {
			code, body := get(t, "/v1/hello", tok)
			if code != http.StatusUnauthorized {
				t.Fatalf("token %q: status %d want 401", tok, code)
			}
			for _, leak := range []string{"headscale_control_url", "listen", "version", "protocol", "tls_mode"} {
				if _, ok := body[leak]; ok {
					t.Fatalf("401 body discloses %q: %v", leak, body)
				}
			}
		}
	})

	t.Run("hello with a valid token returns the payload", func(t *testing.T) {
		code, body := get(t, "/v1/hello", token)
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if body["headscale_control_url"] != "https://headscale.example" {
			t.Fatalf("body=%v", body)
		}
		if body["listen"] != "100.64.0.1:7531" || body["version"] != "test" {
			t.Fatalf("body=%v", body)
		}
		if body["tls_mode"] != "letsencrypt" || body["tls_fell_back"] != true {
			t.Fatalf("tls status not surfaced: %v", body)
		}
	})

	t.Run("healthz is unauthenticated liveness only", func(t *testing.T) {
		code, body := get(t, "/healthz", "")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if body["ok"] != true {
			t.Fatalf("body=%v", body)
		}
		if len(body) != 1 {
			t.Fatalf("healthz discloses more than liveness: %v", body)
		}
	})
}
