package session_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// handoffReceiptFixture sets up two enrolled devices (A + B) each with its
// own signing key, a real receipt store, and a manager whose transport signs
// with whichever device it is asked to sign for — the shape a real
// release+claim exercises (releaser signs into A's chain, claimer into B's).
type handoffReceiptFixture struct {
	devA, devB string
	keyA, keyB *ecdsa.PrivateKey
	daemonPriv *ecdsa.PrivateKey
	rcptStore  *receipt.Store
	mgr        *session.Manager
	prov       *permProvider
	// transportBehavior can be overridden per test (e.g. to drop B's reply).
	behavior func(deviceID string, statement json.RawMessage) (string, error)
}

func enrollDeviceKey(t *testing.T, as *auth.Store, name string) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	dev, _, err := as.CreateWithClientKey(name, "fp-"+name, spki)
	if err != nil {
		t.Fatal(err)
	}
	return dev.ID, key
}

func newHandoffReceiptFixture(t *testing.T, handoffs bool) *handoffReceiptFixture {
	t.Helper()
	dir := t.TempDir()

	as, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &handoffReceiptFixture{}
	f.devA, f.keyA = enrollDeviceKey(t, as, "phone-a")
	f.devB, f.keyB = enrollDeviceKey(t, as, "phone-b")

	f.daemonPriv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.rcptStore, err = receipt.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Default: sign the daemon-sent statement with the requested device's key.
	f.behavior = func(deviceID string, statement json.RawMessage) (string, error) {
		key := f.keyA
		if deviceID == f.devB {
			key = f.keyB
		}
		return receipt.SignES256Compact(key, statement)
	}
	transport := &fakeReceiptTransport{
		behavior: func(_ context.Context, deviceID, _, _ string, statement json.RawMessage) (string, error) {
			return f.behavior(deviceID, statement)
		},
	}

	f.prov = &permProvider{}
	reg := provider.NewRegistry()
	reg.Register(f.prov)
	f.mgr = session.NewManager(reg, nil, nil, nil)
	t.Cleanup(func() { f.mgr.CloseAll(context.Background()) })

	f.mgr.SetReceiptSupport(session.ReceiptSupport{
		Config:    config.ReceiptsConfig{Enabled: true, AllowPatterns: []string{"*"}, Handoffs: handoffs},
		Store:     f.rcptStore,
		AuthStore: as,
		DaemonKey: f.daemonPriv,
		Transport: transport,
	})
	return f
}

// createOwnedByA creates a session owned by device A.
func (f *handoffReceiptFixture) createOwnedByA(t *testing.T) string {
	t.Helper()
	meta, err := f.mgr.Create(context.Background(), provider.ID("permtest"), provider.StartOptions{}, f.devA)
	if err != nil {
		t.Fatal(err)
	}
	return meta.ID
}

func lastPredicateType(t *testing.T, rs *receipt.Store, deviceID string) string {
	t.Helper()
	lines, err := rs.Lines(deviceID)
	if err != nil || len(lines) == 0 {
		return ""
	}
	payload, err := receipt.DecodePayloadUnverified(lines[len(lines)-1])
	if err != nil {
		t.Fatal(err)
	}
	var s receipt.Statement
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatal(err)
	}
	return s.PredicateType
}

// TestHandoffReceiptsDualChains: a release writes a verifiable
// session-handoff-release entry to A's chain; a claim writes a
// session-handoff-claim entry to B's chain (MADR 0078 D4).
func TestHandoffReceiptsDualChains(t *testing.T) {
	f := newHandoffReceiptFixture(t, true)
	sid := f.createOwnedByA(t)

	if _, err := f.mgr.Release(sid, f.devA, f.devB); err != nil {
		t.Fatal(err)
	}
	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.devA)
		return err == nil && ok
	})
	if pt := lastPredicateType(t, f.rcptStore, f.devA); pt != receipt.PredicateTypeSessionHandoffRelease {
		t.Fatalf("A's last entry predicateType=%q, want handoff-release", pt)
	}

	if _, err := f.mgr.Claim(sid, f.devB); err != nil {
		t.Fatal(err)
	}
	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.devB)
		return err == nil && ok
	})
	if pt := lastPredicateType(t, f.rcptStore, f.devB); pt != receipt.PredicateTypeSessionHandoffClaim {
		t.Fatalf("B's last entry predicateType=%q, want handoff-claim", pt)
	}

	// Both chains verify against their signer's key.
	if broken, err := f.rcptStore.Verify(f.devA, &f.keyA.PublicKey, &f.daemonPriv.PublicKey); err != nil || broken != -1 {
		t.Fatalf("A chain: broken=%d err=%v", broken, err)
	}
	if broken, err := f.rcptStore.Verify(f.devB, &f.keyB.PublicKey, &f.daemonPriv.PublicKey); err != nil || broken != -1 {
		t.Fatalf("B chain: broken=%d err=%v", broken, err)
	}
}

// TestHandoffReceiptTimeoutWritesMarker: if the claiming device never signs,
// a daemon-signed receipt-unavailable marker lands in B's chain, keeping it
// intact (the same fallback permission receipts use).
func TestHandoffReceiptTimeoutWritesMarker(t *testing.T) {
	f := newHandoffReceiptFixture(t, true)
	sid := f.createOwnedByA(t)
	if _, err := f.mgr.Release(sid, f.devA, f.devB); err != nil {
		t.Fatal(err)
	}
	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.devA)
		return err == nil && ok
	})

	// B's signing fails (device silent / signature error).
	f.behavior = func(deviceID string, statement json.RawMessage) (string, error) {
		if deviceID == f.devB {
			return "", errors.New("device never replied")
		}
		return receipt.SignES256Compact(f.keyA, statement)
	}
	if _, err := f.mgr.Claim(sid, f.devB); err != nil {
		t.Fatal(err)
	}
	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.devB)
		return err == nil && ok
	})
	if pt := lastPredicateType(t, f.rcptStore, f.devB); pt != receipt.PredicateTypeReceiptUnavailable {
		t.Fatalf("B's last entry predicateType=%q, want receipt-unavailable marker", pt)
	}
	if broken, err := f.rcptStore.Verify(f.devB, &f.keyB.PublicKey, &f.daemonPriv.PublicKey); err != nil || broken != -1 {
		t.Fatalf("B chain with marker: broken=%d err=%v", broken, err)
	}
}

// TestHandoffReceiptsDisabledIsNoOp: with receipts.handoffs=false, the
// transfer still succeeds but nothing is written (MADR 0078 D6).
func TestHandoffReceiptsDisabledIsNoOp(t *testing.T) {
	f := newHandoffReceiptFixture(t, false)
	f.behavior = func(string, json.RawMessage) (string, error) {
		t.Fatal("transport must not be called when handoffs are disabled")
		return "", nil
	}
	sid := f.createOwnedByA(t)
	if _, err := f.mgr.Release(sid, f.devA, f.devB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mgr.Claim(sid, f.devB); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok, _ := f.rcptStore.LastHash(f.devA); ok {
		t.Fatal("A chain has an entry with handoffs disabled")
	}
	if _, ok, _ := f.rcptStore.LastHash(f.devB); ok {
		t.Fatal("B chain has an entry with handoffs disabled")
	}
}
