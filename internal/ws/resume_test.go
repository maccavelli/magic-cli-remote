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

// MADR 0068 P4 (U4) — the resume fast path riding auth (R1 shape).

type resumeHarness struct {
	url   string
	token string
}

func newResumeHarness(t *testing.T, window time.Duration) resumeHarness {
	t.Helper()
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
		ResumeWindow:       window,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return resumeHarness{url: "ws" + ts.URL[len("http"):] + "/v1/ws", token: token}
}

// authV2 authenticates offering v2 with an optional resume payload and
// returns the decoded auth_ok.
func authV2(ctx context.Context, t *testing.T, conn *websocket.Conn, token string, resume *protocol.AuthResumePayload, windowMS int64) protocol.AuthOKPayload {
	t.Helper()
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{
		Token:          token,
		Protocols:      []int{1, 2},
		Resume:         resume,
		ResumeWindowMS: windowMS,
	})
	writeEnv(ctx, t, conn, env)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s %s", got.Type, string(got.Payload))
	}
	var ok protocol.AuthOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestResumeIssueRotateAndWindowClamp(t *testing.T) {
	h := newResumeHarness(t, 120*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c1 := dialNegotiation(ctx, t, h.url)
	ok1 := authV2(ctx, t, c1, h.token, nil, 0)
	if ok1.ResumeToken == "" {
		t.Fatal("v2 auth_ok missing resume_token")
	}
	if ok1.Caps == nil || ok1.Caps.Resume == nil || ok1.Caps.Resume.WindowMS != 120_000 {
		t.Fatalf("caps.resume = %+v, want 120000ms window", ok1.Caps)
	}
	if ok1.Resumed != nil || ok1.ResumeFailed {
		t.Fatal("no resume was attempted; neither resumed nor resume_failed may appear")
	}

	// Client-requested narrower window is granted; wider is clamped.
	c2 := dialNegotiation(ctx, t, h.url)
	ok2 := authV2(ctx, t, c2, h.token, nil, 30_000)
	if ok2.Caps.Resume.WindowMS != 30_000 {
		t.Fatalf("requested 30s window, got %dms", ok2.Caps.Resume.WindowMS)
	}
	if ok2.ResumeToken == ok1.ResumeToken {
		t.Fatal("token must rotate on every v2 auth")
	}
	c3 := dialNegotiation(ctx, t, h.url)
	ok3 := authV2(ctx, t, c3, h.token, nil, 600_000)
	if ok3.Caps.Resume.WindowMS != 120_000 {
		t.Fatalf("requested 10min window, got %dms — must clamp to server ceiling", ok3.Caps.Resume.WindowMS)
	}

	// Rotation invalidates elder tokens: ok1's token is two rotations old.
	c4 := dialNegotiation(ctx, t, h.url)
	ok4 := authV2(ctx, t, c4, h.token, &protocol.AuthResumePayload{Token: ok1.ResumeToken}, 0)
	if !ok4.ResumeFailed {
		t.Fatal("a rotated-away token must fail resume")
	}
}

func TestResumeReturnsOwnedSessionBounds(t *testing.T) {
	h := newResumeHarness(t, 120*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First connection: auth, create a session, generate some events.
	c1 := dialNegotiation(ctx, t, h.url)
	ok1 := authV2(ctx, t, c1, h.token, nil, 0)
	createEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "2", protocol.SessionCreatePayload{
		Provider: "fake", Name: "demo",
	})
	writeEnv(ctx, t, c1, createEnv)
	var meta session.Meta
	for {
		got := readEnv(ctx, t, c1)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeSessionCreated {
			t.Fatalf("want session.created got %s %s", got.Type, string(got.Payload))
		}
		if err := json.Unmarshal(got.Payload, &meta); err != nil {
			t.Fatal(err)
		}
		break
	}
	_ = c1.Close(websocket.StatusNormalClosure, "backgrounded")

	// Second connection resumes with the token and the session's seq claim.
	c2 := dialNegotiation(ctx, t, h.url)
	ok2 := authV2(ctx, t, c2, h.token, &protocol.AuthResumePayload{
		Token:    ok1.ResumeToken,
		Sessions: map[string]uint64{meta.ID: 0, "unknown-session": 3},
	}, 0)
	if ok2.ResumeFailed {
		t.Fatal("valid token within window must not fail")
	}
	if ok2.Resumed == nil {
		t.Fatal("resumed block missing")
	}
	bounds, okB := ok2.Resumed.Sessions[meta.ID]
	if !okB || bounds.LatestSeq == 0 {
		t.Fatalf("owned session bounds missing/empty: %+v", ok2.Resumed.Sessions)
	}
	if _, present := ok2.Resumed.Sessions["unknown-session"]; present {
		t.Fatal("unknown session must be absent from resumed — client reconciles it normally")
	}
}

func TestResumeExpiredWindowFails(t *testing.T) {
	h := newResumeHarness(t, 120*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c1 := dialNegotiation(ctx, t, h.url)
	// Request a 1ms window: the token is stale before the next dial.
	ok1 := authV2(ctx, t, c1, h.token, nil, 1)
	time.Sleep(20 * time.Millisecond)

	c2 := dialNegotiation(ctx, t, h.url)
	ok2 := authV2(ctx, t, c2, h.token, &protocol.AuthResumePayload{Token: ok1.ResumeToken}, 0)
	if !ok2.ResumeFailed {
		t.Fatal("expired token must set resume_failed")
	}
	if ok2.ResumeToken == "" {
		t.Fatal("a fresh token is still issued after a failed resume")
	}
}
