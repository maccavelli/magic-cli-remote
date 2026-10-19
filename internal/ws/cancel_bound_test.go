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

// blockingCancelProvider is the fake provider with one change: its sessions'
// Cancel ignores the context and waits until the test releases it — the shape
// of an engine whose cancel hangs.
type blockingCancelProvider struct {
	*fake.Provider
	release chan struct{}
}

func (p blockingCancelProvider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	s, err := p.Provider.Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	return blockingCancelSession{Session: s, release: p.release}, nil
}

type blockingCancelSession struct {
	provider.Session
	release chan struct{}
}

func (s blockingCancelSession) Cancel(context.Context) error {
	<-s.release
	return nil
}

// TestABlockedCancelDoesNotHoldTheReadLoop is MADR 0201 D10 (the 0073 F-T3
// residual). session.cancel stays on the read loop on purpose, so it can reach
// work queued behind the async workers; but a provider whose Cancel hangs must
// not stall every frame behind it. The cancel gets a typed timeout, and the
// connection keeps answering.
func TestABlockedCancelDoesNotHoldTheReadLoop(t *testing.T) {
	defer ws.SetCancelBoundForTest(100 * time.Millisecond)()
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("canceller")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	reg := provider.NewRegistry()
	reg.Register(blockingCancelProvider{Provider: fake.New(), release: release})
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "a1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	create, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "c1", protocol.SessionCreatePayload{Provider: "fake"})
	writeEnv(ctx, t, conn, create)
	created := readReplyFor(ctx, t, conn, "c1")
	if created.Type != protocol.TypeSessionCreated {
		t.Fatalf("create: %s %s", created.Type, string(created.Payload))
	}
	var meta struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Payload, &meta); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	cancelEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCancel, "x1", protocol.SessionIDPayload{SessionID: meta.ID})
	writeEnv(ctx, t, conn, cancelEnv)
	ping, _ := protocol.NewEnvelope(protocol.TypePing, "p1", nil)
	writeEnv(ctx, t, conn, ping)

	got := readReplyFor(ctx, t, conn, "x1")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the cancel answered after %v, want within the bound", elapsed)
	}
	var p protocol.ErrorPayload
	_ = json.Unmarshal(got.Payload, &p)
	if got.Type != protocol.TypeError || p.Code != protocol.ErrSessionCancelTimeout {
		t.Fatalf("cancel reply: %s %s, want error %s", got.Type, string(got.Payload), protocol.ErrSessionCancelTimeout)
	}
	if pong := readReplyFor(ctx, t, conn, "p1"); pong.Type != protocol.TypePong {
		t.Fatalf("ping after a blocked cancel: %s, want pong", pong.Type)
	}
}
