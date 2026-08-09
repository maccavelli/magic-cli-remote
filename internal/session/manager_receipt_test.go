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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// permProvider/permSession is a minimal provider.Session +
// provider.PermissionSession test double — the fake provider package does
// not implement PermissionSession at all (confirmed via
// `grep -rln RespondPermission internal/provider/fake/`), so this phase's
// tests need their own, matching how every real provider's RespondPermission
// resolves: send a permission_resolved event carrying Status/DeviceID/
// OptionID, exactly what internal/session.Manager's event pump (and this
// phase's receipt hook) observes.
type permSession struct {
	id     string
	events chan event.Event
}

func (s *permSession) ID() string                                       { return s.id }
func (s *permSession) ProviderID() provider.ID                          { return provider.ID("permtest") }
func (s *permSession) AgentSessionID() string                           { return s.id }
func (s *permSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *permSession) Cancel(context.Context) error                     { return nil }
func (s *permSession) Events() <-chan event.Event                       { return s.events }
func (s *permSession) Close(context.Context) error                      { close(s.events); return nil }

func (s *permSession) emitPermissionRequest(permissionID, toolName, detail string) {
	s.events <- event.Event{
		Type:         event.TypePermission,
		SessionID:    s.id,
		PermissionID: permissionID,
		ToolName:     toolName,
		Text:         detail,
	}
}

// RespondPermission implements provider.PermissionSession, mirroring every
// real provider's shape: emit permission_resolved carrying the resolving
// device and its chosen option (MADR 0077 §1).
func (s *permSession) RespondPermission(_ context.Context, permissionID, optionID string, cancelled bool, deviceID string) error {
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.events <- event.Event{
		Type:         event.TypePermissionResolved,
		SessionID:    s.id,
		PermissionID: permissionID,
		Status:       status,
		DeviceID:     deviceID,
		OptionID:     optionID,
	}
	return nil
}

var _ provider.PermissionSession = (*permSession)(nil)

type permProvider struct {
	mu   sync.Mutex
	last *permSession
}

func (p *permProvider) ID() provider.ID { return provider.ID("permtest") }
func (p *permProvider) Ready() bool     { return true }

func (p *permProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &permSession{id: opts.LocalSessionID, events: make(chan event.Event, 32)}
	p.mu.Lock()
	p.last = s
	p.mu.Unlock()
	return s, nil
}

func (p *permProvider) lastSession() *permSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

// fakeReceiptTransport stands in for the phone over the wire (this phase's
// "fake WS client", per the PLAN's Acceptance section) — behavior is set per
// test to simulate a signing device, a silent one, or one replying with a
// tampered signature.
type fakeReceiptTransport struct {
	calls    int32
	behavior func(ctx context.Context, deviceID, sessionID, permissionID string, statement json.RawMessage) (string, error)
}

func (f *fakeReceiptTransport) RequestPermissionReceipt(ctx context.Context, deviceID, sessionID, permissionID string, statement json.RawMessage) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.behavior(ctx, deviceID, sessionID, permissionID, statement)
}

func (f *fakeReceiptTransport) callCount() int32 { return atomic.LoadInt32(&f.calls) }

// receiptTestFixture bundles one device's real keypair (registered in a real
// auth.Store, matching PublicKeyFor's real code path), a real receipt.Store,
// and a daemon signing key.
type receiptTestFixture struct {
	t          *testing.T
	deviceID   string
	devicePriv *ecdsa.PrivateKey
	daemonPriv *ecdsa.PrivateKey
	authStore  *auth.Store
	rcptStore  *receipt.Store
	transport  *fakeReceiptTransport
	mgr        *session.Manager
	prov       *permProvider
}

func newReceiptTestFixture(t *testing.T, allow []string, transportBehavior func(ctx context.Context, deviceID, sessionID, permissionID string, statement json.RawMessage) (string, error)) *receiptTestFixture {
	t.Helper()
	dir := t.TempDir()

	devicePriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&devicePriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	daemonPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	authStore, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	dev, _, err := authStore.CreateWithClientKey("phone", "fp-1", spki)
	if err != nil {
		t.Fatal(err)
	}

	rcptStore, err := receipt.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	transport := &fakeReceiptTransport{behavior: transportBehavior}

	prov := &permProvider{}
	reg := provider.NewRegistry()
	reg.Register(prov)
	mgr := session.NewManager(reg, nil, nil, nil)
	t.Cleanup(func() { mgr.CloseAll(context.Background()) })

	mgr.SetReceiptSupport(session.ReceiptSupport{
		Config: config.ReceiptsConfig{
			Enabled:       true,
			AllowPatterns: allow,
		},
		Store:     rcptStore,
		AuthStore: authStore,
		DaemonKey: daemonPriv,
		Transport: transport,
	})

	return &receiptTestFixture{
		t: t, deviceID: dev.ID, devicePriv: devicePriv, daemonPriv: daemonPriv,
		authStore: authStore, rcptStore: rcptStore, transport: transport,
		mgr: mgr, prov: prov,
	}
}

// resolveOnePermission creates a session, raises one permission_request, and
// resolves it as deviceID/optionID — the same path RespondPermission drives
// in production, exercised end to end through the real event pump.
func (f *receiptTestFixture) resolveOnePermission(t *testing.T, toolName, detail, optionID string) {
	t.Helper()
	ctx := context.Background()
	meta, err := f.mgr.Create(ctx, provider.ID("permtest"), provider.StartOptions{}, f.deviceID)
	if err != nil {
		t.Fatal(err)
	}
	sess := f.prov.lastSession()
	if sess == nil {
		t.Fatal("provider.Start was not called")
	}
	sess.emitPermissionRequest("perm-1", toolName, detail)
	// The pump processes TypePermission asynchronously; give it a moment to
	// record pendingPermissions before resolving, matching real timing (the
	// phone always sees the request before it can answer it).
	time.Sleep(20 * time.Millisecond)
	if err := f.mgr.RespondPermission(ctx, meta.ID, "perm-1", optionID, false, f.deviceID); err != nil {
		t.Fatal(err)
	}
}

// waitForReceipt polls check every 10ms until it returns true or timeout
// elapses (named distinctly from commands_test.go's waitFor, a different
// signature in the same test package).
func waitForReceipt(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// TestReceiptRoundTripSuccess: a matching decision, a device that signs
// correctly → exactly one new chained, verifiable line.
func TestReceiptRoundTripSuccess(t *testing.T) {
	var f *receiptTestFixture
	f = newReceiptTestFixture(t, []string{"*rm -rf*"}, func(_ context.Context, deviceID, _, _ string, statement json.RawMessage) (string, error) {
		return receipt.SignES256Compact(f.devicePriv, statement)
	})

	f.resolveOnePermission(t, "bash", "rm -rf ./build", "once")

	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.deviceID)
		return err == nil && ok
	})

	broken, err := f.rcptStore.Verify(f.deviceID, &f.devicePriv.PublicKey, &f.daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != -1 {
		t.Fatalf("chain broken at line %d, want intact", broken)
	}
}

// TestReceiptTimeoutWritesUnavailableMarker: the device never signs (any
// transport failure) → a daemon-signed receipt-unavailable marker, reason
// "timeout", verifiable against the daemon's own key.
func TestReceiptTimeoutWritesUnavailableMarker(t *testing.T) {
	f := newReceiptTestFixture(t, []string{"*"}, func(context.Context, string, string, string, json.RawMessage) (string, error) {
		return "", errors.New("device never replied")
	})

	f.resolveOnePermission(t, "bash", "echo hi", "once")

	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.deviceID)
		return err == nil && ok
	})

	broken, err := f.rcptStore.Verify(f.deviceID, &f.devicePriv.PublicKey, &f.daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != -1 {
		t.Fatalf("chain broken at line %d, want intact (daemon-signed marker)", broken)
	}
}

// TestReceiptTamperedSignatureWritesUnavailableMarker: the device replies,
// but with a signature that does not verify → falls through to the same
// receipt-unavailable path, not a crash and not a false "success".
func TestReceiptTamperedSignatureWritesUnavailableMarker(t *testing.T) {
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := newReceiptTestFixture(t, []string{"*"}, func(_ context.Context, _, _, _ string, statement json.RawMessage) (string, error) {
		// Signed by the WRONG key — verification against the enrolled
		// device key must fail.
		return receipt.SignES256Compact(other, statement)
	})

	f.resolveOnePermission(t, "bash", "echo hi", "once")

	waitForReceipt(t, 2*time.Second, func() bool {
		_, ok, err := f.rcptStore.LastHash(f.deviceID)
		return err == nil && ok
	})

	broken, err := f.rcptStore.Verify(f.deviceID, &f.devicePriv.PublicKey, &f.daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != -1 {
		t.Fatalf("chain broken at line %d, want intact (daemon-signed marker)", broken)
	}
}

// TestReceiptDisabledIsNoOp is the core invariant (MADR 0077 D4): with
// receipts.enabled: false, none of this phase's code path executes at
// all — ShouldReceipt short-circuits before the transport is ever touched.
func TestReceiptDisabledIsNoOp(t *testing.T) {
	f := newReceiptTestFixture(t, []string{"*"}, func(context.Context, string, string, string, json.RawMessage) (string, error) {
		t.Fatal("transport must never be called when receipts are disabled")
		return "", nil
	})
	f.mgr.SetReceiptSupport(session.ReceiptSupport{
		Config:    config.ReceiptsConfig{Enabled: false, AllowPatterns: []string{"*"}},
		Store:     f.rcptStore,
		AuthStore: f.authStore,
		DaemonKey: f.daemonPriv,
		Transport: f.transport,
	})

	f.resolveOnePermission(t, "bash", "echo hi", "once")
	time.Sleep(100 * time.Millisecond)

	if got := f.transport.callCount(); got != 0 {
		t.Fatalf("transport called %d times, want 0", got)
	}
	if _, ok, err := f.rcptStore.LastHash(f.deviceID); err != nil || ok {
		t.Fatalf("LastHash ok=%v err=%v, want no entries", ok, err)
	}
}

// TestRespondPermissionReturnsBeforeReceiptRoundTrip: the receipt round trip
// must never delay RespondPermission's own return (D8) — asserted by making
// the transport artificially slow and confirming RespondPermission still
// returns promptly.
func TestRespondPermissionReturnsBeforeReceiptRoundTrip(t *testing.T) {
	release := make(chan struct{})
	var fx *receiptTestFixture
	fx = newReceiptTestFixture(t, []string{"*"}, func(_ context.Context, _, _, _ string, statement json.RawMessage) (string, error) {
		<-release
		return receipt.SignES256Compact(fx.devicePriv, statement)
	})
	defer close(release)

	ctx := context.Background()
	meta, err := fx.mgr.Create(ctx, provider.ID("permtest"), provider.StartOptions{}, fx.deviceID)
	if err != nil {
		t.Fatal(err)
	}
	sess := fx.prov.lastSession()
	sess.emitPermissionRequest("perm-1", "bash", "echo hi")
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	if err := fx.mgr.RespondPermission(ctx, meta.ID, "perm-1", "once", false, fx.deviceID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("RespondPermission took %s — the receipt round trip must run in the background, not block it", elapsed)
	}
}
