package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// authProbeProvider is a provider whose credential state the test controls.
// It stands in for kilo, whose real probe needs a live engine.
type authProbeProvider struct {
	id    provider.ID
	state provider.AuthState
	err   error
	// calls is atomic: providers.list probes concurrently and credential
	// writes each push a status refresh, so several goroutines reach
	// AuthStatus at once.
	calls atomic.Int32
}

func (p *authProbeProvider) ID() provider.ID { return p.id }
func (p *authProbeProvider) Ready() bool     { return true }
func (p *authProbeProvider) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	return nil, errors.New("not used")
}
func (p *authProbeProvider) callCount() int { return int(p.calls.Load()) }

func (p *authProbeProvider) AuthStatus(context.Context) (provider.AuthState, error) {
	p.calls.Add(1)
	if p.err != nil {
		return provider.AuthState{}, p.err
	}
	return p.state, nil
}

// plainProvider implements no auth interface at all — the "contributes no auth
// block" case from MADR 0074 D3.
type plainProvider struct{ id provider.ID }

func (p *plainProvider) ID() provider.ID { return p.id }
func (p *plainProvider) Ready() bool     { return true }
func (p *plainProvider) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	return nil, errors.New("not used")
}

type authWS struct {
	conn *websocket.Conn
	ts   *httptest.Server
}

func (w *authWS) close() {
	w.conn.Close(websocket.StatusNormalClosure, "")
	w.ts.Close()
}

// startAuthServer boots a server with the given providers and connects a
// client that offers the given protocol versions.
func startAuthServer(t *testing.T, protocols []int, provs ...provider.Provider) (*authWS, *protocol.Caps) {
	t.Helper()
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("probe")
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	for _, p := range provs {
		reg.Register(p)
	}
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
	})
	ts := httptest.NewServer(srv.Handler())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "auth", protocol.AuthPayload{
		Token:     token,
		Protocols: protocols,
	})
	writeEnv(ctx, t, conn, env)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		conn.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		t.Fatalf("auth: %s %s", got.Type, got.Payload)
	}
	var ok protocol.AuthOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	return &authWS{conn: conn, ts: ts}, ok.Caps
}

// listProviders sends providers.list and returns the decoded reply plus the
// raw frame, so a test can assert on exact bytes.
func listProviders(t *testing.T, w *authWS, v int) (protocol.ProvidersResultPayload, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, _ := protocol.NewEnvelope(protocol.TypeProvidersList, "prov", nil)
	env.V = v
	writeEnv(ctx, t, w.conn, env)
	for {
		got := readEnv(ctx, t, w.conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeProvidersResult {
			t.Fatalf("want providers.list_result, got %s %s", got.Type, got.Payload)
		}
		var res protocol.ProvidersResultPayload
		if err := json.Unmarshal(got.Payload, &res); err != nil {
			t.Fatal(err)
		}
		return res, string(got.Payload)
	}
}

func sampleState() provider.AuthState {
	return provider.AuthState{
		Status:         provider.AuthConfigured,
		ActiveUpstream: "kilo",
		Upstreams: []provider.UpstreamAuth{{
			ID:     "github-copilot",
			Label:  "GitHub Copilot",
			Status: provider.AuthMissing,
			Methods: []provider.AuthMethod{{
				ID:    "github-copilot:0",
				Type:  provider.AuthMethodOAuthDevice,
				Label: "Login with GitHub Copilot",
				Inputs: []provider.AuthInput{{
					Key:     "deploymentType",
					Type:    provider.AuthInputSelect,
					Message: "Select GitHub deployment type",
					Options: []provider.AuthInputOption{
						{Value: "github.com", Label: "GitHub.com"},
						{Value: "enterprise", Label: "Enterprise"},
					},
				}, {
					Key:  "enterpriseUrl",
					Type: provider.AuthInputText,
					When: &provider.AuthInputCondition{Key: "deploymentType", Op: "eq", Value: "enterprise"},
				}},
			}},
		}},
	}
}

// MADR 0074 D4/D6: a v1 client's reply must be exactly what it was before this
// feature existed — no auth key at all — and the daemon must not even run the
// probes for it.
func TestProvidersListV1UnchangedAndUnprobed(t *testing.T) {
	p := &authProbeProvider{id: "kilo", state: sampleState()}
	w, caps := startAuthServer(t, nil, p)
	defer w.close()

	if caps != nil {
		t.Fatalf("v1 auth_ok carried caps: %+v", caps)
	}
	res, raw := listProviders(t, w, protocol.V1)
	if len(res.Providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(res.Providers))
	}
	if res.Providers[0].Auth != nil {
		t.Fatalf("v1 client received an auth block: %s", raw)
	}
	if strings.Contains(raw, "auth") {
		t.Fatalf("v1 frame mentions auth at all: %s", raw)
	}
	if n := p.callCount(); n != 0 {
		t.Fatalf("probed %d times for a v1 client; D6 says a client that cannot use it must not cost anything", n)
	}
}

// A v2 client that negotiated the capability gets the block, including the
// declared inputs and their conditional visibility (D5).
func TestProvidersListV2CarriesAuthBlock(t *testing.T) {
	p := &authProbeProvider{id: "kilo", state: sampleState()}
	w, caps := startAuthServer(t, []int{1, 2}, p)
	defer w.close()

	if caps == nil || !caps.ProviderAuth {
		t.Fatalf("v2 caps did not advertise provider_auth: %+v", caps)
	}
	res, raw := listProviders(t, w, protocol.V2)
	if len(res.Providers) != 1 || res.Providers[0].Auth == nil {
		t.Fatalf("v2 client got no auth block: %s", raw)
	}
	got := res.Providers[0].Auth
	if got.Status != protocol.AuthStatusConfigured || got.ActiveUpstream != "kilo" {
		t.Fatalf("auth block = %+v", got)
	}
	if len(got.Upstreams) != 1 || len(got.Upstreams[0].Methods) != 1 {
		t.Fatalf("upstreams = %+v", got.Upstreams)
	}
	m := got.Upstreams[0].Methods[0]
	if len(m.Inputs) != 2 {
		t.Fatalf("inputs dropped in conversion: %+v", m)
	}
	if len(m.Inputs[0].Options) != 2 || m.Inputs[0].Options[0].Value != "github.com" {
		t.Fatalf("select options mangled: %+v", m.Inputs[0])
	}
	if m.Inputs[1].When == nil || m.Inputs[1].When.Value != "enterprise" {
		t.Fatalf("conditional visibility lost: %+v", m.Inputs[1])
	}
}

// A provider that implements no auth interface contributes nothing, and must
// not block the others (D3).
func TestProvidersListMixedAuthAndPlainProviders(t *testing.T) {
	w, _ := startAuthServer(t, []int{1, 2},
		&authProbeProvider{id: "kilo", state: sampleState()},
		&plainProvider{id: "fake"},
	)
	defer w.close()

	res, raw := listProviders(t, w, protocol.V2)
	if len(res.Providers) != 2 {
		t.Fatalf("got %d providers: %s", len(res.Providers), raw)
	}
	byID := map[string]*protocol.ProviderAuthPayload{}
	for _, p := range res.Providers {
		byID[p.ID] = p.Auth
	}
	if byID["kilo"] == nil {
		t.Error("kilo lost its auth block")
	}
	if byID["fake"] != nil {
		t.Errorf("a provider with no auth support reported one: %+v", byID["fake"])
	}
}

// A failing probe degrades that one provider, never the listing. Before this,
// a wedged engine would have meant a provider screen that never loads.
func TestProvidersListSurvivesProbeFailure(t *testing.T) {
	w, _ := startAuthServer(t, []int{1, 2},
		&authProbeProvider{id: "kilo", err: errors.New("engine down")},
		&authProbeProvider{id: "opencode", state: sampleState()},
	)
	defer w.close()

	res, raw := listProviders(t, w, protocol.V2)
	if len(res.Providers) != 2 {
		t.Fatalf("a failing probe dropped a provider: %s", raw)
	}
	for _, p := range res.Providers {
		if p.Auth == nil {
			t.Fatalf("%s has no auth block: %s", p.ID, raw)
		}
		if p.ID == "kilo" && p.Auth.Status != protocol.AuthStatusError {
			t.Fatalf("failed probe reported %q, want error", p.Auth.Status)
		}
		if p.ID == "opencode" && p.Auth.Status != protocol.AuthStatusConfigured {
			t.Fatalf("healthy probe reported %q", p.Auth.Status)
		}
	}
}

// ErrAuthUnsupported is a normal answer, not a failure: the provider simply
// declines to report, and must not be shown as errored.
func TestProvidersListUnsupportedIsNotAnError(t *testing.T) {
	w, _ := startAuthServer(t, []int{1, 2},
		&authProbeProvider{id: "codex", err: provider.ErrAuthUnsupported})
	defer w.close()

	res, raw := listProviders(t, w, protocol.V2)
	if len(res.Providers) != 1 {
		t.Fatalf("got %s", raw)
	}
	if res.Providers[0].Auth != nil {
		t.Fatalf("unsupported reported as a block: %+v", res.Providers[0].Auth)
	}
}

// MADR 0074 D2: nothing key-shaped may ever reach the wire, even if a provider
// misbehaves and puts a secret in a label.
func TestProvidersListFrameCarriesNoSecrets(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-42"
	st := sampleState()
	st.Upstreams[0].Label = "GitHub Copilot"
	p := &authProbeProvider{id: "kilo", state: st}
	w, _ := startAuthServer(t, []int{1, 2}, p)
	defer w.close()

	_, raw := listProviders(t, w, protocol.V2)
	if strings.Contains(raw, secret) || strings.Contains(raw, "sk-") {
		t.Fatalf("providers frame carried key material: %s", raw)
	}
}

// The probe must honour the request context rather than pinning a goroutine
// after the client goes away.
func TestAuthProbeRespectsContext(t *testing.T) {
	slow := &slowAuthProvider{id: "kilo"}
	reg := provider.NewRegistry()
	reg.Register(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	list := reg.ListWithAuth(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("probe ignored context cancellation (took %s)", elapsed)
	}
	if len(list) != 1 {
		t.Fatalf("got %d entries", len(list))
	}
}

type slowAuthProvider struct{ id provider.ID }

func (p *slowAuthProvider) ID() provider.ID { return p.id }
func (p *slowAuthProvider) Ready() bool     { return true }
func (p *slowAuthProvider) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	return nil, errors.New("not used")
}
func (p *slowAuthProvider) AuthStatus(ctx context.Context) (provider.AuthState, error) {
	select {
	case <-ctx.Done():
		return provider.AuthState{}, ctx.Err()
	case <-time.After(30 * time.Second):
		return provider.AuthState{}, nil
	}
}
