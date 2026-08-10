package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// pairDevice pairs a fresh device over its own client cert and returns an
// authenticated connection plus the device id — the building block for a
// two-device handoff test.
func pairDevice(ctx context.Context, t *testing.T, ts *httptest.Server, codes *auth.PairCodeStore, name string) (*websocket.Conn, string) {
	t.Helper()
	code, err := codes.Create(name, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := genClientCert(t)
	conn := dialWSS(ctx, t, ts, &cert)
	claim, _ := protocol.NewEnvelope(protocol.TypePairClaim, "pair-"+name, protocol.PairClaimPayload{Code: code.Display})
	writeEnv(ctx, t, conn, claim)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypePairOK {
		t.Fatalf("%s pair: want pair_ok got %s payload=%s", name, got.Type, string(got.Payload))
	}
	var ok protocol.PairOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	return conn, ok.DeviceID
}

// sessionListIDs sends session.list on an authed connection and returns the
// session ids it can see.
func sessionListIDs(ctx context.Context, t *testing.T, conn *websocket.Conn, reqID string) []string {
	t.Helper()
	list, _ := protocol.NewEnvelope(protocol.TypeSessionList, reqID, struct{}{})
	writeEnv(ctx, t, conn, list)
	// Drain until the matching list_result arrives (broadcasts may interleave).
	for i := 0; i < 20; i++ {
		env := readEnv(ctx, t, conn)
		if env.Type != protocol.TypeSessionListResult {
			continue
		}
		var res protocol.SessionListResultPayload
		if err := json.Unmarshal(env.Payload, &res); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(res.Sessions))
		for _, s := range res.Sessions {
			ids = append(ids, s.ID)
		}
		return ids
	}
	t.Fatal("no session.list_result")
	return nil
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// readReplyFor drains until the envelope with the given request id arrives.
func readReplyFor(ctx context.Context, t *testing.T, conn *websocket.Conn, reqID string) protocol.Envelope {
	t.Helper()
	for i := 0; i < 20; i++ {
		env := readEnv(ctx, t, conn)
		if env.ID == reqID {
			return env
		}
	}
	t.Fatalf("no reply for req %s", reqID)
	return protocol.Envelope{}
}

// TestSessionHandoffTargetedOverWire drives the full A→B transfer through
// real WebSocket frames (MADR 0078 P2): A creates + releases (targeted at B),
// B's list gains it, B claims, A's list loses it and an A prompt is forbidden,
// B can prompt.
func TestSessionHandoffTargetedOverWire(t *testing.T) {
	srv, _, codes := newKeyServer(t, true)
	ts := startTLS(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	connA, devA := pairDevice(ctx, t, ts, codes, "phone-a")
	connB, devB := pairDevice(ctx, t, ts, codes, "phone-b")

	// A creates a session on the fake provider.
	create, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "create-1", protocol.SessionCreatePayload{Provider: "fake"})
	writeEnv(ctx, t, connA, create)
	got := readReplyFor(ctx, t, connA, "create-1")
	if got.Type != protocol.TypeSessionCreated {
		t.Fatalf("create: %s %s", got.Type, string(got.Payload))
	}
	var meta struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got.Payload, &meta); err != nil {
		t.Fatal(err)
	}

	// Before release: only A sees it.
	if !contains(sessionListIDs(ctx, t, connA, "list-a1"), meta.ID) {
		t.Fatal("A does not see its own session")
	}
	if contains(sessionListIDs(ctx, t, connB, "list-b1"), meta.ID) {
		t.Fatal("B sees A's session before release")
	}

	// A releases, targeted at B.
	rel, _ := protocol.NewEnvelope(protocol.TypeSessionRelease, "rel-1", protocol.SessionReleasePayload{
		SessionID:  meta.ID,
		ToDeviceID: devB,
	})
	writeEnv(ctx, t, connA, rel)
	if got := readReplyFor(ctx, t, connA, "rel-1"); got.Type != protocol.TypeOK {
		t.Fatalf("release: %s %s", got.Type, string(got.Payload))
	}

	// After release: B sees it, A does not.
	if !contains(sessionListIDs(ctx, t, connB, "list-b2"), meta.ID) {
		t.Fatal("B does not see the targeted release")
	}
	if contains(sessionListIDs(ctx, t, connA, "list-a2"), meta.ID) {
		t.Fatal("A still sees the released session")
	}

	// B claims — reply is a session.created-shaped Meta owned by B.
	claim, _ := protocol.NewEnvelope(protocol.TypeSessionClaim, "claim-1", protocol.SessionIDPayload{SessionID: meta.ID})
	writeEnv(ctx, t, connB, claim)
	cgot := readReplyFor(ctx, t, connB, "claim-1")
	if cgot.Type != protocol.TypeSessionCreated {
		t.Fatalf("claim: %s %s", cgot.Type, string(cgot.Payload))
	}
	var claimed struct {
		OwnerDeviceID string `json:"owner_device_id"`
	}
	if err := json.Unmarshal(cgot.Payload, &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.OwnerDeviceID != devB {
		t.Fatalf("claimed owner=%q want %q", claimed.OwnerDeviceID, devB)
	}

	// A prompt from the old owner now fails; B owns it.
	prompt, _ := protocol.NewEnvelope(protocol.TypeSessionPrompt, "prompt-a", protocol.SessionPromptPayload{
		SessionID: meta.ID, Text: "hi",
	})
	writeEnv(ctx, t, connA, prompt)
	if got := readReplyFor(ctx, t, connA, "prompt-a"); got.Type != protocol.TypeError {
		t.Fatalf("A prompt after handoff: %s %s, want error", got.Type, string(got.Payload))
	}
	if !contains(sessionListIDs(ctx, t, connB, "list-b3"), meta.ID) {
		t.Fatal("B does not own the session after claim")
	}
	_ = devA
}

// TestSessionHandoffOpenRelease: an open release (no target) is claimable by a
// third device C.
func TestSessionHandoffOpenRelease(t *testing.T) {
	srv, _, codes := newKeyServer(t, true)
	ts := startTLS(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	connA, _ := pairDevice(ctx, t, ts, codes, "phone-a")
	connC, devC := pairDevice(ctx, t, ts, codes, "phone-c")

	create, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "create-1", protocol.SessionCreatePayload{Provider: "fake"})
	writeEnv(ctx, t, connA, create)
	got := readReplyFor(ctx, t, connA, "create-1")
	var meta struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got.Payload, &meta); err != nil {
		t.Fatal(err)
	}

	// Open release (no ToDeviceID).
	rel, _ := protocol.NewEnvelope(protocol.TypeSessionRelease, "rel-1", protocol.SessionReleasePayload{SessionID: meta.ID})
	writeEnv(ctx, t, connA, rel)
	if got := readReplyFor(ctx, t, connA, "rel-1"); got.Type != protocol.TypeOK {
		t.Fatalf("open release: %s", got.Type)
	}
	if !contains(sessionListIDs(ctx, t, connC, "list-c1"), meta.ID) {
		t.Fatal("C does not see the open release")
	}
	claim, _ := protocol.NewEnvelope(protocol.TypeSessionClaim, "claim-1", protocol.SessionIDPayload{SessionID: meta.ID})
	writeEnv(ctx, t, connC, claim)
	cgot := readReplyFor(ctx, t, connC, "claim-1")
	if cgot.Type != protocol.TypeSessionCreated {
		t.Fatalf("C claim: %s %s", cgot.Type, string(cgot.Payload))
	}
	var claimed struct {
		OwnerDeviceID string `json:"owner_device_id"`
	}
	_ = json.Unmarshal(cgot.Payload, &claimed)
	if claimed.OwnerDeviceID != devC {
		t.Fatalf("claimed owner=%q want %q", claimed.OwnerDeviceID, devC)
	}
}

// TestSessionClaimNotReleasedError: claiming a session that has not been
// released returns a typed error over the wire.
func TestSessionClaimNotReleasedError(t *testing.T) {
	srv, _, codes := newKeyServer(t, true)
	ts := startTLS(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	connA, _ := pairDevice(ctx, t, ts, codes, "phone-a")
	connB, _ := pairDevice(ctx, t, ts, codes, "phone-b")

	create, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "create-1", protocol.SessionCreatePayload{Provider: "fake"})
	writeEnv(ctx, t, connA, create)
	got := readReplyFor(ctx, t, connA, "create-1")
	var meta struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(got.Payload, &meta)

	// B tries to claim a session A never released.
	claim, _ := protocol.NewEnvelope(protocol.TypeSessionClaim, "claim-1", protocol.SessionIDPayload{SessionID: meta.ID})
	writeEnv(ctx, t, connB, claim)
	cgot := readReplyFor(ctx, t, connB, "claim-1")
	if cgot.Type != protocol.TypeError {
		t.Fatalf("claim of un-released session: %s, want error", cgot.Type)
	}
	var ep protocol.ErrorPayload
	if err := json.Unmarshal(cgot.Payload, &ep); err != nil {
		t.Fatal(err)
	}
	if ep.Code != "session_not_released" {
		t.Fatalf("error code=%q want session_not_released", ep.Code)
	}
}
