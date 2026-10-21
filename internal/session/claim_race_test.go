package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// claimRace runs two concurrent first-touch claims on id and checks that
// exactly one wins, the other is forbidden, and the stored owner is the winner.
func claimRace(t *testing.T, mgr *session.Manager, store *session.Store, id string) {
	t.Helper()
	devs := [2]string{"dev-a", "dev-b"}
	var errs [2]error
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 2 {
		wg.Go(func() {
			<-start
			errs[i] = mgr.Authorize(id, devs[i], true)
		})
	}
	close(start)
	wg.Wait()

	winner := ""
	for i, err := range errs {
		switch {
		case err == nil:
			if winner != "" {
				t.Fatalf("%s: both claimers were told they own the session", id)
			}
			winner = devs[i]
		case errors.Is(err, session.ErrForbidden):
		default:
			t.Fatalf("%s: claim by %s: %v", id, devs[i], err)
		}
	}
	if winner == "" {
		t.Fatalf("%s: neither claim won: %v", id, errs)
	}
	rec, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.OwnerDeviceID != winner {
		t.Fatalf("%s: stored owner %q, but %q was told it won", id, rec.OwnerDeviceID, winner)
	}
	if owner, ok := mgr.OwnerOf(id); !ok || owner != winner {
		t.Fatalf("%s: in-memory owner %q, but %q was told it won", id, owner, winner)
	}
}

// TestOnlyOneConcurrentClaimWins is MADR 0201 D13 (0073 F-S3). Two devices
// claiming one unowned session could both be told they own it, while disk kept
// the last writer and memory the first. Live sessions and stored-only ones are
// both covered; 200 rounds each, under -race.
func TestOnlyOneConcurrentClaimWins(t *testing.T) {
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, store, nil, nil)
	ctx := context.Background()

	t.Run("live", func(t *testing.T) {
		for range 200 {
			meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{}, "")
			if err != nil {
				t.Fatal(err)
			}
			claimRace(t, mgr, store, meta.ID)
		}
	})
	t.Run("stored", func(t *testing.T) {
		for range 200 {
			meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := mgr.Close(ctx, meta.ID, ""); err != nil {
				t.Fatal(err)
			}
			claimRace(t, mgr, store, meta.ID)
		}
	})
}
