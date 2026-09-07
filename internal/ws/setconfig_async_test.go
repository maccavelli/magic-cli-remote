package ws_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
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

// slowConfigProvider's sessions block inside SetConfigOption until released,
// standing in for the engine call every ACP provider makes there.
type slowConfigProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *slowConfigProvider) ID() provider.ID { return "slowcfg" }
func (p *slowConfigProvider) Ready() bool     { return true }

func (p *slowConfigProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "s1"
	}
	s := &slowConfigSession{id: id, p: p, events: make(chan event.Event, 32)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id,
		Status: "idle", Timestamp: time.Now().UTC()}
	return s, nil
}

type slowConfigSession struct {
	id     string
	p      *slowConfigProvider
	events chan event.Event

	mu     sync.Mutex
	closed bool
}

func (s *slowConfigSession) ID() string                   { return s.id }
func (s *slowConfigSession) ProviderID() provider.ID      { return "slowcfg" }
func (s *slowConfigSession) AgentSessionID() string       { return s.id }
func (s *slowConfigSession) Cancel(context.Context) error { return nil }
func (s *slowConfigSession) Events() <-chan event.Event   { return s.events }

func (s *slowConfigSession) Prompt(_ context.Context, _ []provider.Content) error {
	s.events <- event.Event{Type: event.TypeTurnComplete, SessionID: s.id,
		Timestamp: time.Now().UTC(), StopReason: "end_turn"}
	return nil
}

func (s *slowConfigSession) SetConfigOption(ctx context.Context, optionID, kind, value string) error {
	select {
	case s.p.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *slowConfigSession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

// TestSlowSetConfigDoesNotStallTheConnection is MADR 0137 F4.
//
// session.set_config ran inline on the connection's read loop, so an engine
// call there stopped the daemon reading ANY further frame from that phone —
// the next prompt, a cancel, a ping. The symptom is indistinguishable from the
// engine being slow, which is what made it survive so long.
//
// The assertion is that a prompt sent while set_config is blocked is answered
// while it is still blocked. If the handler is inline, the prompt is not even
// read until the block clears.
func TestSlowSetConfigDoesNotStallTheConnection(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}

	p := &slowConfigProvider{
		entered: make(chan struct{}, 4),
		release: make(chan struct{}),
	}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store: store, Sessions: mgr, Registry: reg,
		RequireDeviceToken: true, Version: "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "a", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	createEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "c1",
		protocol.SessionCreatePayload{Provider: "slowcfg", SessionID: "s1", Name: "n"})
	writeEnv(ctx, t, conn, createEnv)
	for {
		if env := readEnv(ctx, t, conn); env.ID == "c1" {
			break
		}
	}

	// Block the connection in set_config.
	cfgEnv, _ := protocol.NewEnvelope(protocol.TypeSessionSetConfig, "cfg1",
		protocol.SessionSetConfigPayload{SessionID: "s1", OptionID: "opt", Kind: "boolean", Value: "true"})
	writeEnv(ctx, t, conn, cfgEnv)
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("set_config never reached the provider")
	}

	// Send a prompt while set_config is still blocked, and require an answer.
	promptEnv, _ := protocol.NewEnvelope(protocol.TypeSessionPrompt, "p1",
		protocol.SessionPromptPayload{SessionID: "s1", Text: "hi"})
	writeEnv(ctx, t, conn, promptEnv)

	answered := make(chan struct{})
	go func() {
		defer close(answered)
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			env := readEnv(ctx, t, conn)
			if env.ID == "p1" {
				return
			}
		}
	}()

	select {
	case <-answered:
	case <-time.After(8 * time.Second):
		close(p.release)
		t.Fatal("the prompt was not answered while set_config was blocked: " +
			"set_config is running on the read loop, so nothing else on this " +
			"connection is even read until it finishes")
	}
	close(p.release)
	_ = fmt.Sprint()
}
