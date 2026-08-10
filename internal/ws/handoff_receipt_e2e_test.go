package ws_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
)

// signingConn is a paired device that behaves like the real phone for the
// receipt round trip (MADR 0077 D2): a background pump reads the socket,
// auto-signs any server-pushed permission.receipt_request with the device's
// enrolled key, and forwards every other envelope to an inbound channel the
// test reads from. This is what lets a handoff's release/claim receipts
// actually get signed over the real wire.
type signingConn struct {
	conn    *websocket.Conn
	key     *ecdsa.PrivateKey
	device  string
	writeMu sync.Mutex
	inbound chan protocol.Envelope
}

func (sc *signingConn) write(ctx context.Context, env protocol.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()
	return sc.conn.Write(ctx, websocket.MessageText, b)
}

func (sc *signingConn) pump(ctx context.Context) {
	for {
		_, data, err := sc.conn.Read(ctx)
		if err != nil {
			return
		}
		var env protocol.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if env.Type == protocol.TypePermissionReceiptRequest {
			var p protocol.PermissionReceiptRequestPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			jws, serr := receipt.SignES256Compact(sc.key, p.Statement)
			if serr != nil {
				continue
			}
			reply, _ := protocol.NewEnvelope(protocol.TypePermissionReceipt, "rcpt-reply", protocol.PermissionReceiptPayload{
				SessionID:    p.SessionID,
				PermissionID: p.PermissionID,
				JWS:          jws,
			})
			_ = sc.write(ctx, reply)
			continue
		}
		select {
		case sc.inbound <- env:
		case <-ctx.Done():
			return
		}
	}
}

// reply drains the inbound channel until the envelope with the given request
// id arrives (skipping broadcasts/events that interleave).
func (sc *signingConn) reply(ctx context.Context, t *testing.T, reqID string) protocol.Envelope {
	t.Helper()
	for {
		select {
		case env := <-sc.inbound:
			if env.ID == reqID {
				return env
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for reply id=%s", reqID)
		}
	}
}

// pairDeviceSigning pairs a fresh device and starts its signing pump, keeping
// the enrolled key so the pump can sign receipt requests.
func pairDeviceSigning(ctx context.Context, t *testing.T, ts *httptest.Server, codes *auth.PairCodeStore, name string) *signingConn {
	t.Helper()
	code, err := codes.Create(name, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := genClientCert(t)
	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("client cert key is not ecdsa")
	}
	conn := dialWSS(ctx, t, ts, &cert)
	claim, _ := protocol.NewEnvelope(protocol.TypePairClaim, "pair-"+name, protocol.PairClaimPayload{Code: code.Display})
	writeEnv(ctx, t, conn, claim)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypePairOK {
		t.Fatalf("%s pair: want pair_ok got %s payload=%s", name, got.Type, string(got.Payload))
	}
	var okp protocol.PairOKPayload
	if err := json.Unmarshal(got.Payload, &okp); err != nil {
		t.Fatal(err)
	}
	sc := &signingConn{
		conn:    conn,
		key:     key,
		device:  okp.DeviceID,
		inbound: make(chan protocol.Envelope, 32),
	}
	go sc.pump(ctx)
	return sc
}

// waitForChainEntry polls deviceID's on-disk chain (a second store handle to
// dataDir) until it has at least wantLen entries, or fails.
func waitForChainEntry(t *testing.T, dataDir, deviceID string, wantLen int) []string {
	t.Helper()
	rs, err := receipt.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		lines, err := rs.Lines(deviceID)
		if err == nil && len(lines) >= wantLen {
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("device %s chain never reached %d entries (have %d)", deviceID, wantLen, len(lines))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func decodeStatement(t *testing.T, jwsCompact string) receipt.Statement {
	t.Helper()
	payload, err := receipt.DecodePayloadUnverified(jwsCompact)
	if err != nil {
		t.Fatal(err)
	}
	var s receipt.Statement
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSessionHandoffReceiptsEndToEnd is MADR 0078 P7's acceptance gate: a real
// A→B handoff over the actual WebSocket wire — A creates and releases (targeted
// at B), B claims — produces TWO independently-verifiable receipts in two
// different device chains, linked by a shared handoff subject, and BOTH chains
// verify via the real `mcremote receipts verify` subprocess (not an in-process
// Store.Verify call).
func TestSessionHandoffReceiptsEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	srv, _, codes, _, _ := newReceiptE2EServer(t, dataDir)
	ts := startTLS(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := pairDeviceSigning(ctx, t, ts, codes, "phone-a")
	b := pairDeviceSigning(ctx, t, ts, codes, "phone-b")

	// A creates a session.
	create, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "create-1", protocol.SessionCreatePayload{Provider: "e2eperm"})
	if err := a.write(ctx, create); err != nil {
		t.Fatal(err)
	}
	got := a.reply(ctx, t, "create-1")
	if got.Type != protocol.TypeSessionCreated {
		t.Fatalf("create: %s %s", got.Type, string(got.Payload))
	}
	var meta struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got.Payload, &meta); err != nil {
		t.Fatal(err)
	}

	// A releases, targeted at B.
	rel, _ := protocol.NewEnvelope(protocol.TypeSessionRelease, "rel-1", protocol.SessionReleasePayload{
		SessionID:  meta.ID,
		ToDeviceID: b.device,
	})
	if err := a.write(ctx, rel); err != nil {
		t.Fatal(err)
	}
	if r := a.reply(ctx, t, "rel-1"); r.Type != protocol.TypeOK {
		t.Fatalf("release: %s %s", r.Type, string(r.Payload))
	}

	// A's release receipt lands in A's chain (its pump signs it).
	aLines := waitForChainEntry(t, dataDir, a.device, 1)
	relStmt := decodeStatement(t, aLines[len(aLines)-1])
	if relStmt.PredicateType != receipt.PredicateTypeSessionHandoffRelease {
		t.Fatalf("A's last entry predicateType=%q, want handoff-release", relStmt.PredicateType)
	}

	// B claims.
	claim, _ := protocol.NewEnvelope(protocol.TypeSessionClaim, "claim-1", protocol.SessionIDPayload{SessionID: meta.ID})
	if err := b.write(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if c := b.reply(ctx, t, "claim-1"); c.Type != protocol.TypeSessionCreated {
		t.Fatalf("claim: %s %s", c.Type, string(c.Payload))
	}

	// B's claim receipt lands in B's chain.
	bLines := waitForChainEntry(t, dataDir, b.device, 1)
	claimStmt := decodeStatement(t, bLines[len(bLines)-1])
	if claimStmt.PredicateType != receipt.PredicateTypeSessionHandoffClaim {
		t.Fatalf("B's last entry predicateType=%q, want handoff-claim", claimStmt.PredicateType)
	}

	// The two halves share one handoff subject — the cross-chain link (D4).
	if len(relStmt.Subject) == 0 || len(claimStmt.Subject) == 0 ||
		relStmt.Subject[0].Name == "" || relStmt.Subject[0].Name != claimStmt.Subject[0].Name {
		t.Fatalf("release/claim subjects do not match: %+v vs %+v", relStmt.Subject, claimStmt.Subject)
	}

	// Both chains verify via the REAL binary, independently.
	runMcremoteReceiptsVerify(t, dataDir, a.device)
	runMcremoteReceiptsVerify(t, dataDir, b.device)
}
