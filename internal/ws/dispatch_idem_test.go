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
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// A retry whose ledger entry completed without a captured frame must get a
// typed error, not silence. Before MADR 0095 D6 dispatchAsync returned
// without writing anything and the client burned its whole timeout — the
// failure shape MADR 0093 was written about.
func TestDispatchAsyncEmptyReplayAnswersWithRetryNoResult(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	dev, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
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
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	// The original attempt finished but captured no response frame.
	const reqID = "retry-me"
	srv.SeedIdemCompleted(dev.ID, reqID)

	// The client retries a mutating op with that same id.
	delEnv, _ := protocol.NewEnvelope(protocol.TypeSessionDelete, reqID,
		protocol.SessionIDPayload{SessionID: "does-not-matter"})
	writeEnv(ctx, t, conn, delEnv)

	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeError {
		t.Fatalf("want an error frame, got %s payload=%s", got.Type, string(got.Payload))
	}
	var p protocol.ErrorPayload
	if err := json.Unmarshal(got.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != protocol.ErrRetryNoResult {
		t.Fatalf("code = %q, want %q", p.Code, protocol.ErrRetryNoResult)
	}
	if got.ID != reqID {
		t.Fatalf("reply id = %q, want %q", got.ID, reqID)
	}
}
