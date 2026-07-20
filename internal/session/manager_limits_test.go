package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

func TestMaxLiveSessions(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManagerWithLimits(reg, nil, nil, nil, 2)
	ctx := context.Background()

	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "1"}, "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "2"}, "d"); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "3"}, "d")
	if !errors.Is(err, session.ErrLimitReached) {
		t.Fatalf("want ErrLimitReached, got %v", err)
	}
	if mgr.LiveCount() != 2 {
		t.Fatalf("live=%d", mgr.LiveCount())
	}
}

func TestMaxLiveSessionsReplaceDoesNotCountTwice(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManagerWithLimits(reg, nil, nil, nil, 1)
	ctx := context.Background()
	const id = "only"
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "1", LocalSessionID: id}, "d"); err != nil {
		t.Fatal(err)
	}
	// Replace same id should succeed under max=1.
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "2", LocalSessionID: id}, "d"); err != nil {
		t.Fatal(err)
	}
	if mgr.LiveCount() != 1 {
		t.Fatalf("live=%d", mgr.LiveCount())
	}
}
