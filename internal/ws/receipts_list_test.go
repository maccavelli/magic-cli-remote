package ws_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
)

// seedPermissionReceipt appends one signed permission-decision entry to
// deviceID's chain in the on-disk store rooted at dataDir (a second handle to
// the same directory the daemon's store uses).
func seedPermissionReceipt(t *testing.T, dataDir, deviceID, permissionID string) {
	t.Helper()
	rs, err := receipt.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := receipt.BuildPermissionDecisionStatement(
		"sess-x", permissionID, deviceID, "once", "bash", "echo hi",
		time.Unix(1, 0).UTC(), "device:"+deviceID, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := receipt.SignES256Compact(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Append(deviceID, compact); err != nil {
		t.Fatal(err)
	}
}

func listReceiptsOverWire(ctx context.Context, t *testing.T, conn *websocket.Conn) protocol.ReceiptsListResultPayload {
	t.Helper()
	req, _ := protocol.NewEnvelope(protocol.TypeReceiptsList, "rl", struct{}{})
	writeEnv(ctx, t, conn, req)
	env := readReplyFor(ctx, t, conn, "rl")
	if env.Type != protocol.TypeReceiptsListResult {
		t.Fatalf("want receipts.list_result, got %s %s", env.Type, string(env.Payload))
	}
	var res protocol.ReceiptsListResultPayload
	if err := json.Unmarshal(env.Payload, &res); err != nil {
		t.Fatal(err)
	}
	return res
}

// TestReceiptsListOwnChainOnly is the D8 security property: a device reads
// ITS OWN receipt chain and never another device's — the exact analog of a
// device listing only its own sessions.
func TestReceiptsListOwnChainOnly(t *testing.T) {
	dataDir := t.TempDir()
	srv, _, codes, _, _ := newReceiptE2EServer(t, dataDir)
	ts := startTLS(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	connA, devA := pairDevice(ctx, t, ts, codes, "phone-a")
	connB, devB := pairDevice(ctx, t, ts, codes, "phone-b")

	seedPermissionReceipt(t, dataDir, devA, "perm-a")
	seedPermissionReceipt(t, dataDir, devB, "perm-b")

	resA := listReceiptsOverWire(ctx, t, connA)
	if len(resA.Entries) != 1 || resA.Entries[0].Statement == nil {
		t.Fatalf("A sees %d entries, want 1 decoded", len(resA.Entries))
	}
	nameA := resA.Entries[0].Statement.Subject[0].Name
	if !strings.Contains(nameA, "perm-a") || strings.Contains(nameA, "perm-b") {
		t.Fatalf("A's entry subject %q is not A's own (or leaks B's)", nameA)
	}

	resB := listReceiptsOverWire(ctx, t, connB)
	if len(resB.Entries) != 1 || resB.Entries[0].Statement == nil {
		t.Fatalf("B sees %d entries, want 1 decoded", len(resB.Entries))
	}
	nameB := resB.Entries[0].Statement.Subject[0].Name
	if !strings.Contains(nameB, "perm-b") || strings.Contains(nameB, "perm-a") {
		t.Fatalf("B's entry subject %q is not B's own (or leaks A's)", nameB)
	}
}
