package ws_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// writableProvider records what reached the credential writer.
type writableProvider struct {
	authProbeProvider
	mu       sync.Mutex
	gotKey   string
	gotUp    string
	gotIn    map[string]string
	cleared  string
	setErr   error
	clearErr error
	writes   int
}

func (p *writableProvider) SetCredential(_ context.Context, upstreamID, _, secret string, inputs map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes++
	if p.setErr != nil {
		return p.setErr
	}
	p.gotKey, p.gotUp, p.gotIn = secret, upstreamID, inputs
	return nil
}

func (p *writableProvider) ClearCredential(_ context.Context, upstreamID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clearErr != nil {
		return p.clearErr
	}
	p.cleared = upstreamID
	return nil
}

func (p *writableProvider) snapshot() (key, up, cleared string, writes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gotKey, p.gotUp, p.cleared, p.writes
}

// sendAndAwait writes a request and returns the first non-event reply.
func sendAndAwait(t *testing.T, w *authWS, typ string, payload any) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, err := protocol.NewEnvelope(typ, "req-"+typ, payload)
	if err != nil {
		t.Fatal(err)
	}
	env.V = protocol.V2
	writeEnv(ctx, t, w.conn, env)
	for {
		got := readEnv(ctx, t, w.conn)
		switch got.Type {
		case protocol.TypeEvent, protocol.TypeProviderAuthStatus:
			continue // status pushes are expected alongside the ack
		default:
			return got
		}
	}
}

// The happy path: the key reaches the provider, and the declared inputs travel
// with it (MADR 0074 D5).
func TestSetCredentialReachesProvider(t *testing.T) {
	p := &writableProvider{authProbeProvider: authProbeProvider{id: "opencode", state: sampleState()}}
	w, _ := startAuthServer(t, []int{1, 2}, p)
	defer w.close()

	got := sendAndAwait(t, w, protocol.TypeProviderSetCredential, protocol.SetCredentialPayload{
		ProviderID: "opencode",
		UpstreamID: "opencode-go",
		MethodID:   "opencode-go:api",
		Secret:     "sk-live-value",
		Inputs:     map[string]string{"accountId": "acct-1"},
	})
	if got.Type != protocol.TypeOK {
		t.Fatalf("want ok, got %s %s", got.Type, got.Payload)
	}
	key, up, _, _ := p.snapshot()
	if key != "sk-live-value" {
		t.Errorf("provider received key %q", key)
	}
	if up != "opencode-go" {
		t.Errorf("provider received upstream %q", up)
	}
	p.mu.Lock()
	in := p.gotIn["accountId"]
	p.mu.Unlock()
	if in != "acct-1" {
		t.Errorf("declared inputs did not reach the provider: %v", p.gotIn)
	}
}

// MADR 0074 D9: a credential change that needs a restart must not kill a live
// turn. The client is told to retry, with a transient code.
func TestSetCredentialBusyIsTransient(t *testing.T) {
	p := &writableProvider{
		authProbeProvider: authProbeProvider{id: "goose", state: sampleState()},
		setErr:            provider.ErrAuthBusy,
	}
	w, _ := startAuthServer(t, []int{1, 2}, p)
	defer w.close()

	got := sendAndAwait(t, w, protocol.TypeProviderSetCredential, protocol.SetCredentialPayload{
		ProviderID: "goose", UpstreamID: "opencode_go", Secret: "sk-x",
	})
	if got.Type != protocol.TypeError {
		t.Fatalf("want error, got %s", got.Type)
	}
	if !strings.Contains(string(got.Payload), protocol.ErrProviderBusy) {
		t.Fatalf("busy not reported as %s: %s", protocol.ErrProviderBusy, got.Payload)
	}
}

// A provider with no writer must say so rather than silently accepting.
func TestSetCredentialUnsupportedProvider(t *testing.T) {
	w, _ := startAuthServer(t, []int{1, 2},
		&authProbeProvider{id: "readonly", state: sampleState()})
	defer w.close()

	got := sendAndAwait(t, w, protocol.TypeProviderSetCredential, protocol.SetCredentialPayload{
		ProviderID: "readonly", UpstreamID: "x", Secret: "sk-x",
	})
	if got.Type != protocol.TypeError || !strings.Contains(string(got.Payload), "read-only") {
		t.Fatalf("want read-only error, got %s %s", got.Type, got.Payload)
	}
}

// MADR 0074 D6: the verbs are gated too, not just the status block. A v1
// client must not be able to write credentials.
func TestSetCredentialRefusedWithoutCapability(t *testing.T) {
	p := &writableProvider{authProbeProvider: authProbeProvider{id: "opencode"}}
	w, _ := startAuthServer(t, nil, p)
	defer w.close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, _ := protocol.NewEnvelope(protocol.TypeProviderSetCredential, "req", protocol.SetCredentialPayload{
		ProviderID: "opencode", UpstreamID: "opencode-go", Secret: "sk-x",
	})
	env.V = protocol.V1
	writeEnv(ctx, t, w.conn, env)
	got := readEnv(ctx, t, w.conn)
	if got.Type != protocol.TypeError {
		t.Fatalf("a v1 client wrote a credential: %s %s", got.Type, got.Payload)
	}
	if _, _, _, writes := p.snapshot(); writes != 0 {
		t.Fatalf("provider was called %d times for an ungated client", writes)
	}
}

// MADR 0074 D2/D11: the secret must not appear in any daemon log line, at any
// level, on success or on failure.
func TestCredentialWriteNeverLogsSecret(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-777"
	var buf lockedBuffer
	restore := swapDefaultLogger(&buf)
	defer restore()

	p := &writableProvider{authProbeProvider: authProbeProvider{id: "opencode", state: sampleState()}}
	w, _ := startAuthServer(t, []int{1, 2}, p)
	defer w.close()

	if got := sendAndAwait(t, w, protocol.TypeProviderSetCredential, protocol.SetCredentialPayload{
		ProviderID: "opencode", UpstreamID: "opencode-go", Secret: secret,
	}); got.Type != protocol.TypeOK {
		t.Fatalf("write failed: %s %s", got.Type, got.Payload)
	}
	// And again against a provider that fails, since error paths are where
	// payloads most often get formatted into messages.
	p2 := &writableProvider{
		authProbeProvider: authProbeProvider{id: "grok"},
		setErr:            errors.New("upstream rejected the key"),
	}
	w2, _ := startAuthServer(t, []int{1, 2}, p2)
	defer w2.close()
	_ = sendAndAwait(t, w2, protocol.TypeProviderSetCredential, protocol.SetCredentialPayload{
		ProviderID: "grok", UpstreamID: "xai", Secret: secret,
	})

	if logged := buf.String(); strings.Contains(logged, secret) {
		t.Fatalf("secret reached the logs:\n%s", logged)
	}
}

// Two devices writing at once is a supported race whose outcome is one of the
// two values, never a wedged handler (D10).
func TestConcurrentCredentialWrites(t *testing.T) {
	p := &writableProvider{authProbeProvider: authProbeProvider{id: "opencode", state: sampleState()}}
	w1, _ := startAuthServer(t, []int{1, 2}, p)
	defer w1.close()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			env, _ := protocol.NewEnvelope(protocol.TypeProviderSetCredential, "c", protocol.SetCredentialPayload{
				ProviderID: "opencode", UpstreamID: "opencode-go", Secret: "sk-concurrent",
			})
			env.V = protocol.V2
			writeEnv(ctx, t, w1.conn, env)
		}(i)
	}
	wg.Wait()

	// Drain until the writes have all been observed.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, _, writes := p.snapshot(); writes >= 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	key, _, _, writes := p.snapshot()
	if writes < 4 {
		t.Fatalf("only %d of 4 concurrent writes landed", writes)
	}
	if key != "sk-concurrent" {
		t.Fatalf("last writer did not win: %q", key)
	}
}

func TestClearCredential(t *testing.T) {
	p := &writableProvider{authProbeProvider: authProbeProvider{id: "opencode", state: sampleState()}}
	w, _ := startAuthServer(t, []int{1, 2}, p)
	defer w.close()

	got := sendAndAwait(t, w, protocol.TypeProviderClearCredential, protocol.ClearCredentialPayload{
		ProviderID: "opencode", UpstreamID: "opencode-go",
	})
	if got.Type != protocol.TypeOK {
		t.Fatalf("want ok, got %s %s", got.Type, got.Payload)
	}
	if _, _, cleared, _ := p.snapshot(); cleared != "opencode-go" {
		t.Fatalf("cleared %q", cleared)
	}
}

// lockedBuffer is a race-free sink for the test logger.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func swapDefaultLogger(w *lockedBuffer) func() {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(prev) }
}
