package session_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// misnamingProvider is the fake provider with one defect: its sessions report
// an id other than the LocalSessionID they were asked for.
type misnamingProvider struct {
	*fake.Provider
	closed *atomic.Int32
}

func (p misnamingProvider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	s, err := p.Provider.Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	return misnamedSession{Session: s, closed: p.closed}, nil
}

type misnamedSession struct {
	provider.Session
	closed *atomic.Int32
}

func (misnamedSession) ID() string { return "other" }

func (s misnamedSession) Close(ctx context.Context) error {
	s.closed.Add(1)
	return s.Session.Close(ctx)
}

// TestCreateRefusesAMismatchedSessionID is MADR 0201 D4 (0073 F-T1, second
// half). Create keyed the session by whatever id the provider reported, so a
// provider that ignored LocalSessionID would orphan the owner record the
// transport looks up. Create now refuses it, and closes the process.
func TestCreateRefusesAMismatchedSessionID(t *testing.T) {
	var closed atomic.Int32
	reg := provider.NewRegistry()
	reg.Register(misnamingProvider{Provider: fake.New(), closed: &closed})
	mgr := session.NewManager(reg, nil, nil, nil)

	_, err := mgr.Create(context.Background(), provider.IDFake,
		provider.StartOptions{LocalSessionID: "want"}, "dev-1")
	if !errors.Is(err, session.ErrSessionIDMismatch) {
		t.Fatalf("Create = %v, want ErrSessionIDMismatch", err)
	}
	if closed.Load() == 0 {
		t.Fatal("the mismatched provider session was not closed")
	}
	for _, id := range []string{"want", "other"} {
		if _, ok := mgr.OwnerOf(id); ok {
			t.Fatalf("an entry exists under %q after a refused create", id)
		}
	}
}
