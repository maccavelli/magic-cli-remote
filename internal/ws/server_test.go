package ws_test

import (
	"context"
	"encoding/json"
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
