package ws_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// e2ePermSession/e2ePermProvider — the fake provider package does not
// implement provider.PermissionSession at all, so this end-to-end test
// needs its own minimal double, matching every real provider's shape: emit
// permission_resolved carrying Status/DeviceID/OptionID on RespondPermission
// (MADR 0077 P9, using the same test-double approach as P7's
// manager_receipt_test.go, one package over — package boundaries mean it
// can't be reused directly).
type e2ePermSession struct {
	id     string
	events chan event.Event
}

func (s *e2ePermSession) ID() string                                       { return s.id }
func (s *e2ePermSession) ProviderID() provider.ID                          { return provider.ID("e2eperm") }
func (s *e2ePermSession) AgentSessionID() string                           { return s.id }
func (s *e2ePermSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *e2ePermSession) Cancel(context.Context) error                     { return nil }
func (s *e2ePermSession) Events() <-chan event.Event                       { return s.events }
func (s *e2ePermSession) Close(context.Context) error                      { close(s.events); return nil }

func (s *e2ePermSession) emitPermissionRequest(permissionID, toolName, detail string) {
	s.events <- event.Event{
		Type:         event.TypePermission,
		SessionID:    s.id,
		PermissionID: permissionID,
		ToolName:     toolName,
		Text:         detail,
	}
}

func (s *e2ePermSession) RespondPermission(_ context.Context, permissionID, optionID string, cancelled bool, deviceID string) error {
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

var _ provider.PermissionSession = (*e2ePermSession)(nil)

type e2ePermProvider struct {
	mu   sync.Mutex
	last *e2ePermSession
}

func (p *e2ePermProvider) ID() provider.ID { return provider.ID("e2eperm") }
func (p *e2ePermProvider) Ready() bool     { return true }

func (p *e2ePermProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &e2ePermSession{id: opts.LocalSessionID, events: make(chan event.Event, 32)}
	p.mu.Lock()
	p.last = s
	p.mu.Unlock()
	return s, nil
}

func (p *e2ePermProvider) lastSession() *e2ePermSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

// newReceiptE2EServer wires a real ws.Server + session.Manager with receipts
// enabled end to end — the same construction daemon.go performs, minus
// listener/TLS-file plumbing already covered by startTLS.
func newReceiptE2EServer(t *testing.T, dataDir string) (srv *ws.Server, store *auth.Store, codes *auth.PairCodeStore, prov *e2ePermProvider, daemonPriv *ecdsa.PrivateKey) {
	t.Helper()
	var err error
	store, err = auth.OpenStore(filepath.Join(dataDir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	codes, err = auth.OpenPairCodeStore(filepath.Join(dataDir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	rcptStore, err := receipt.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	daemonPriv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	prov = &e2ePermProvider{}
	reg := provider.NewRegistry()
	reg.Register(prov)
	// Bridges the same construction-order cycle daemon.go's eventHub solves:
	// Manager needs an onEvent callback at construction, but that callback
	// is *ws.Server.BroadcastEvent, and ws.Server needs the Manager to
	// construct. srvRef is nil for the brief window before ws.New returns
	// below, during which no session exists yet to emit anything.
	var srvRef *ws.Server
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		if srvRef != nil {
			srvRef.BroadcastEvent(ev)
		}
	})
	t.Cleanup(func() { mgr.CloseAll(context.Background()) })

	srv = ws.New(ws.Options{
		Store:              store,
		PairCodes:          codes,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		RequireClientKey:   true,
		Version:            "test",
	})
	srvRef = srv

	mgr.SetReceiptSupport(session.ReceiptSupport{
		Config:    config.ReceiptsConfig{Enabled: true, AllowPatterns: []string{"*"}, Handoffs: true},
		Store:     rcptStore,
		AuthStore: store,
		DaemonKey: daemonPriv,
		Transport: srv,
	})

	return srv, store, codes, prov, daemonPriv
}

// TestReceiptEndToEndOverRealWS is MADR 0077 P9's final acceptance gate:
// unlike every other test in this feature (which drives session.Manager
// directly with a fake ReceiptTransport, or the CLI with synthetic
// fixtures), this one goes through the *actual* WebSocket wire protocol —
// pair.claim, session.create, permission_request, permission.respond,
// permission.receipt_request, permission.receipt — then verifies the result
// with the real built `mcremote` binary as a subprocess, proving the CLI
// surface (P8) and the storage layer (P4) actually agree with what P7's
// live orchestration produced, not just with each other in-process.
func TestReceiptEndToEndOverRealWS(t *testing.T) {
	dataDir := t.TempDir()
	srv, _, codes, prov, _ := newReceiptE2EServer(t, dataDir)
	ts := startTLS(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Pair over TLS presenting the phone's real client cert — the same
	// key material this test will sign the receipt with.
	cert, _ := genClientCert(t)
	devicePriv, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("client cert key is %T, not ECDSA", cert.PrivateKey)
	}

	code, err := codes.Create("phone", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	conn := dialWSS(ctx, t, ts, &cert)

	claim, _ := protocol.NewEnvelope(protocol.TypePairClaim, "1", protocol.PairClaimPayload{Code: code.Display})
	writeEnv(ctx, t, conn, claim)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypePairOK {
		t.Fatalf("want pair_ok got %s payload=%s", got.Type, string(got.Payload))
	}
	var pairOK protocol.PairOKPayload
	if err := json.Unmarshal(got.Payload, &pairOK); err != nil {
		t.Fatal(err)
	}
	if pairOK.DeviceID == "" {
		t.Fatal("pair_ok carried no device_id")
	}

	// 2. Create a session on the permission-capable test provider — the
	// SAME connection is already authenticated (pair.claim calls setAuthed).
	create, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "2", protocol.SessionCreatePayload{
		Provider: "e2eperm",
	})
	writeEnv(ctx, t, conn, create)
	got = readUntilType(ctx, t, conn, protocol.TypeSessionCreated)
	var meta struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got.Payload, &meta); err != nil {
		t.Fatal(err)
	}

	sess := prov.lastSession()
	if sess == nil {
		t.Fatal("provider.Start was not called")
	}

	// 3. The provider raises a permission request — the phone sees it as a
	// live event, exactly like a real permission ask.
	sess.emitPermissionRequest("perm-e2e-1", "bash", "rm -rf ./build")
	permEnv := readUntilEventType(ctx, t, conn, event.TypePermission)
	var permEv event.Event
	var pep protocol.EventPayload
	if err := json.Unmarshal(permEnv.Payload, &pep); err != nil {
		t.Fatal(err)
	}
	permEv = pep.Event
	if permEv.PermissionID != "perm-e2e-1" {
		t.Fatalf("permission_request id = %q, want perm-e2e-1", permEv.PermissionID)
	}

	// 4. The phone answers — permission.respond.
	respond, _ := protocol.NewEnvelope(protocol.TypePermissionRespond, "3", protocol.PermissionRespondPayload{
		SessionID:    meta.ID,
		PermissionID: permEv.PermissionID,
		OptionID:     "once",
	})
	writeEnv(ctx, t, conn, respond)

	// 5. Two things now arrive on this connection, in either order: the
	// direct `ok` reply to permission.respond (envelope id "3"), and the
	// server-initiated `permission.receipt_request` push (no matching id).
	var receiptReqPayload protocol.PermissionReceiptRequestPayload
	var sawOK, sawReceiptRequest bool
	for i := 0; i < 4 && !(sawOK && sawReceiptRequest); i++ {
		env := readEnv(ctx, t, conn)
		switch {
		case env.ID == "3" && env.Type == protocol.TypeOK:
			sawOK = true
		case env.Type == protocol.TypePermissionReceiptRequest:
			if err := json.Unmarshal(env.Payload, &receiptReqPayload); err != nil {
				t.Fatal(err)
			}
			sawReceiptRequest = true
		case env.Type == protocol.TypeEvent:
			// permission_resolved or other transcript noise — ignore.
		default:
			t.Fatalf("unexpected envelope while waiting for ok+receipt_request: %s id=%s", env.Type, env.ID)
		}
	}
	if !sawOK {
		t.Fatal("never saw the ok response to permission.respond")
	}
	if !sawReceiptRequest {
		t.Fatal("never saw permission.receipt_request — the receipt round trip did not fire")
	}
	if receiptReqPayload.PermissionID != "perm-e2e-1" {
		t.Fatalf("receipt_request permission_id = %q, want perm-e2e-1", receiptReqPayload.PermissionID)
	}

	// 6. Sign the daemon-constructed Statement with this device's own key —
	// json.RawMessage means Statement arrives as the exact bytes the daemon
	// sent, no re-encoding needed (unlike the Dart client, which must decode
	// then re-encode since its JSON parser has no raw-passthrough mode; both
	// are safe — see jws.dart's doc comment for why).
	jws, err := receipt.SignES256Compact(devicePriv, receiptReqPayload.Statement)
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := protocol.NewEnvelope(protocol.TypePermissionReceipt, "4", protocol.PermissionReceiptPayload{
		SessionID:    receiptReqPayload.SessionID,
		PermissionID: receiptReqPayload.PermissionID,
		JWS:          jws,
	})
	writeEnv(ctx, t, conn, reply)
	got = readEnv(ctx, t, conn)
	if got.Type != protocol.TypeOK {
		t.Fatalf("want ok for permission.receipt, got %s payload=%s", got.Type, string(got.Payload))
	}

	// 7. Verify the resulting .jsonl entry through the real built CLI
	// binary as a subprocess — not ReceiptStore.Verify called in-process —
	// proving the CLI surface and the storage layer actually agree.
	waitForFile(t, filepath.Join(dataDir, "receipts", pairOK.DeviceID+".jsonl"), 5*time.Second)
	runMcremoteReceiptsVerify(t, dataDir, pairOK.DeviceID)
}

// readUntilType drains envelopes until it finds one of the given top-level
// envelope type, tolerating stray `event` broadcasts that can legitimately
// arrive interleaved with a direct request/response pair (e.g. session
// creation's async remote_commands advertisement racing session.created).
func readUntilType(ctx context.Context, t *testing.T, conn *websocket.Conn, want string) protocol.Envelope {
	t.Helper()
	for i := 0; i < 20; i++ {
		env := readEnv(ctx, t, conn)
		if env.Type == want {
			return env
		}
	}
	t.Fatalf("never saw an envelope of type %s", want)
	return protocol.Envelope{}
}

// readUntilEventType drains envelopes until it finds a live `event` envelope
// of the given type (skipping any other event types that arrive first,
// e.g. an initial mode/usage/status event some providers emit).
func readUntilEventType(ctx context.Context, t *testing.T, conn *websocket.Conn, want event.Type) protocol.Envelope {
	t.Helper()
	for i := 0; i < 20; i++ {
		env := readEnv(ctx, t, conn)
		if env.Type != protocol.TypeEvent {
			continue
		}
		var p protocol.EventPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.Event.Type == want {
			return env
		}
	}
	t.Fatalf("never saw an event of type %s", want)
	return protocol.Envelope{}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}

// runMcremoteReceiptsVerify builds the real mcremote binary once (cached
// across subtests via the module build cache) and shells out to it — the
// literal subprocess invocation this phase's Acceptance calls for.
func runMcremoteReceiptsVerify(t *testing.T, dataDir, deviceID string) {
	t.Helper()
	repoRoot := findRepoRoot(t)
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "mcremote-e2e-test")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer buildCancel()
	buildCmd := exec.CommandContext(buildCtx, "go", "build", "-o", binPath, "./cmd/mcremote")
	buildCmd.Dir = repoRoot
	var buildOut bytes.Buffer
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("go build ./cmd/mcremote: %v\n%s", err, buildOut.String())
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer runCancel()
	runCmd := exec.CommandContext(runCtx, binPath, "receipts", "verify", "--device", deviceID, "--data-dir", dataDir)
	var runOut bytes.Buffer
	runCmd.Stdout = &runOut
	runCmd.Stderr = &runOut
	err := runCmd.Run()
	out := runOut.String()
	if err != nil {
		t.Fatalf("mcremote receipts verify: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK") || !strings.Contains(out, "intact") {
		t.Fatalf("expected an OK/intact report from the real binary, got:\n%s", out)
	}
}

// findRepoRoot locates the module root by walking up from the working
// directory (internal/ws while running `go test`) looking for go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod) from " + dir)
		}
		dir = parent
	}
}
