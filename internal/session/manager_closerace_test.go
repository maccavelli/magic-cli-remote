package session_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// gateSession holds Prompt and Cancel open until the test releases them, so a
// test can close the session while a call is inside the provider. That is the
// window MADR 0218 F5 describes, made certain rather than hoped for.
type gateSession struct {
	id      string
	events  chan event.Event
	entered chan string // the call that has reached the provider
	release chan struct{}
	closed  atomic.Bool
	closeN  atomic.Int32
}

func (s *gateSession) ID() string                 { return s.id }
func (s *gateSession) ProviderID() provider.ID    { return "gate" }
func (s *gateSession) AgentSessionID() string     { return s.id }
func (s *gateSession) Events() <-chan event.Event { return s.events }

func (s *gateSession) Prompt(context.Context, []provider.Content) error { return s.gate("prompt") }
func (s *gateSession) Cancel(context.Context) error                     { return s.gate("cancel") }

// gate reports the call, waits for the release, and then answers as a real
// provider does: a plain "session closed" once Close has run (MADR 0218 F4).
func (s *gateSession) gate(call string) error {
	s.entered <- call
	<-s.release
	if s.closed.Load() {
		return errors.New("session closed")
	}
	return nil
}

func (s *gateSession) Close(context.Context) error {
	if s.closed.Swap(true) {
		return nil
	}
	s.closeN.Add(1)
	close(s.events)
	return nil
}

type gateProvider struct {
	mu   sync.Mutex
	byID map[string]*gateSession
}

func (p *gateProvider) ID() provider.ID { return "gate" }
func (p *gateProvider) Ready() bool     { return true }

func (p *gateProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &gateSession{
		id:      opts.LocalSessionID,
		events:  make(chan event.Event, 8),
		entered: make(chan string, 1),
		release: make(chan struct{}),
	}
	p.mu.Lock()
	p.byID[s.id] = s
	p.mu.Unlock()
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, Timestamp: time.Now().UTC(), Status: "idle"}
	return s, nil
}

func (p *gateProvider) get(id string) *gateSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byID[id]
}

// TestCallThatLosesTheCloseRaceIsNotLive pins MADR 0218 D2 for the two calls
// every provider implements. The call reaches the provider, the provider
// disconnects, the auto-close removes the entry and closes the session, and
// only then does the provider answer, with its own "session closed". The
// manager must report ErrNotLive, which the phone turns into its "no longer
// running" sentence, and must keep the provider's error inside it.
func TestCallThatLosesTheCloseRaceIsNotLive(t *testing.T) {
	for _, call := range []string{"prompt", "cancel"} {
		t.Run(call, func(t *testing.T) {
			gp := &gateProvider{byID: map[string]*gateSession{}}
			reg := provider.NewRegistry()
			reg.Register(gp)
			mgr := session.NewManager(reg, nil, nil, nil)
			ctx := context.Background()

			meta, err := mgr.Create(ctx, "gate", provider.StartOptions{Name: "t", LocalSessionID: "sess-race"}, "dev-a")
			if err != nil {
				t.Fatal(err)
			}
			s := gp.get(meta.ID)
			if s == nil {
				t.Fatal("missing gate session")
			}

			done := make(chan error, 1)
			go func() {
				if call == "prompt" {
					done <- mgr.Prompt(ctx, meta.ID, "hi", nil, "dev-a")
				} else {
					done <- mgr.Cancel(ctx, meta.ID, "dev-a")
				}
			}()

			select {
			case got := <-s.entered:
				if got != call {
					t.Fatalf("the provider saw %s, want %s", got, call)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s did not reach the provider within 2s", call)
			}

			s.events <- event.Event{
				Type: event.TypeSessionStatus, SessionID: s.id, Timestamp: time.Now().UTC(),
				Status: session.StatusDisconnected,
			}
			// The auto-close deletes the entry before it calls Close (MADR 0218
			// F2), so once Close has run the entry is gone.
			deadline := time.Now().Add(2 * time.Second)
			for s.closeN.Load() == 0 {
				if time.Now().After(deadline) {
					t.Fatal("the auto-close did not close the session within 2s")
				}
				time.Sleep(time.Millisecond)
			}
			close(s.release)

			select {
			case err = <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("%s did not return within 2s of its release", call)
			}
			if !errors.Is(err, session.ErrNotLive) {
				t.Fatalf("%s returned %v, want ErrNotLive", call, err)
			}
			if !strings.Contains(err.Error(), "session closed") {
				t.Fatalf("%s returned %q, which lost the provider's error", call, err)
			}
		})
	}
}
