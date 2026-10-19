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
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// TestBearerUpgradeReceivesAuthOK is MADR 0201 D9 (0073 F-T9). The protocol
// guide documents `Authorization: Bearer` on the upgrade as an optional way to
// authenticate. It must tell the client it worked: an auth_ok for protocol 1,
// since a Bearer upgrade offers no protocols list.
func TestBearerUpgradeReceivesAuthOK(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	dev, token, err := store.Create("bearer")
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           session.NewManager(reg, nil, nil, nil),
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, raw, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("no frame after a Bearer upgrade: %v", err)
	}
	var got protocol.Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("first frame after a Bearer upgrade: %s, want auth_ok", got.Type)
	}
	var ok protocol.AuthOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.DeviceID != dev.ID || ok.Protocol != 0 || ok.Caps != nil {
		t.Fatalf("auth_ok = %+v; want device %s, protocol 1 (field omitted), no caps", ok, dev.ID)
	}

	list, _ := protocol.NewEnvelope(protocol.TypeSessionList, "l1", nil)
	writeEnv(ctx, t, conn, list)
	if res := readEnv(ctx, t, conn); res.Type == protocol.TypeError || res.Type == protocol.TypeAuthError {
		t.Fatalf("session.list after a Bearer upgrade: %s %s", res.Type, string(res.Payload))
	}
}
