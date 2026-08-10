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

func handoffManager(t *testing.T) *session.Manager {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	t.Cleanup(func() { mgr.CloseAll(context.Background()) })
	return mgr
}

// A session with an ID appears in a device's list iff that device may see it.
func ownsInList(mgr *session.Manager, deviceID, sessionID string) bool {
	for _, s := range mgr.ListFor(deviceID) {
		if s.ID == sessionID {
			return true
		}
	}
	return false
}

// TestReleaseOwnershipTransitions: only the owner may release; release clears
// ownership and records the target (MADR 0078 D2).
func TestReleaseOwnershipTransitions(t *testing.T) {
	mgr := handoffManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "s"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}

	// A non-owner cannot release.
	if _, err := mgr.Release(meta.ID, "device-b", ""); !errors.Is(err, session.ErrForbidden) {
		t.Fatalf("non-owner release: %v, want ErrForbidden", err)
	}

	released, err := mgr.Release(meta.ID, "device-a", "device-b")
	if err != nil {
		t.Fatalf("owner release: %v", err)
	}
	if released.OwnerDeviceID != "" {
		t.Fatalf("released owner=%q, want empty", released.OwnerDeviceID)
	}
	if released.PendingHandoffTo != "device-b" {
		t.Fatalf("PendingHandoffTo=%q, want device-b", released.PendingHandoffTo)
	}
	// The releasing device no longer sees it; a session it does not own and
	// is not the handoff target for is invisible.
	if ownsInList(mgr, "device-a", meta.ID) {
		t.Fatal("releaser still sees the released session")
	}
}

// TestTargetedReleaseVisibility: a targeted release is visible ONLY to the
// target, not to a third device (D2's narrowing of open-release visibility).
func TestTargetedReleaseVisibility(t *testing.T) {
	mgr := handoffManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "s"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Release(meta.ID, "device-a", "device-b"); err != nil {
		t.Fatal(err)
	}
	if !ownsInList(mgr, "device-b", meta.ID) {
		t.Fatal("target device-b does not see the targeted release")
	}
	if ownsInList(mgr, "device-c", meta.ID) {
		t.Fatal("third device-c sees a targeted release it is not the target of")
	}
}

// TestOpenReleaseVisibleToAll: an open release (no target) is visible to any
// paired device, exactly like a legacy unowned session.
func TestOpenReleaseVisibleToAll(t *testing.T) {
	mgr := handoffManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "s"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Release(meta.ID, "device-a", ""); err != nil {
		t.Fatal(err)
	}
	for _, dev := range []string{"device-b", "device-c"} {
		if !ownsInList(mgr, dev, meta.ID) {
			t.Fatalf("%s does not see the open release", dev)
		}
	}
}

// TestClaimTransitions: a released session is claimable by the target; a
// not-released session is not; a non-target cannot claim a targeted release.
func TestClaimTransitions(t *testing.T) {
	mgr := handoffManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "s"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}

	// Claim before release → not released.
	if _, err := mgr.Claim(meta.ID, "device-b"); !errors.Is(err, session.ErrNotReleased) {
		t.Fatalf("claim of owned session: %v, want ErrNotReleased", err)
	}

	if _, err := mgr.Release(meta.ID, "device-a", "device-b"); err != nil {
		t.Fatal(err)
	}

	// A non-target cannot claim a targeted release.
	if _, err := mgr.Claim(meta.ID, "device-c"); !errors.Is(err, session.ErrForbidden) {
		t.Fatalf("non-target claim: %v, want ErrForbidden", err)
	}

	claimed, err := mgr.Claim(meta.ID, "device-b")
	if err != nil {
		t.Fatalf("target claim: %v", err)
	}
	if claimed.OwnerDeviceID != "device-b" {
		t.Fatalf("claimed owner=%q, want device-b", claimed.OwnerDeviceID)
	}
	if claimed.PendingHandoffTo != "" {
		t.Fatalf("PendingHandoffTo=%q, want cleared after claim", claimed.PendingHandoffTo)
	}
	// Now device-b owns it; device-a no longer does.
	if !ownsInList(mgr, "device-b", meta.ID) || ownsInList(mgr, "device-a", meta.ID) {
		t.Fatal("ownership did not transfer to device-b")
	}
	// A second claim now fails — it has an owner again.
	if _, err := mgr.Claim(meta.ID, "device-c"); !errors.Is(err, session.ErrNotReleased) {
		t.Fatalf("re-claim of owned session: %v, want ErrNotReleased", err)
	}
}

// TestConcurrentOpenClaimSingleWinner: two devices racing to claim an open
// release — exactly one wins, the other gets a non-nil error.
func TestConcurrentOpenClaimSingleWinner(t *testing.T) {
	mgr := handoffManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "s"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Release(meta.ID, "device-a", ""); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	owners := make([]string, 2)
	devices := []string{"device-b", "device-c"}
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			m, err := mgr.Claim(meta.ID, devices[i])
			results[i] = err
			owners[i] = m.OwnerDeviceID
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i := 0; i < 2; i++ {
		if results[i] == nil {
			wins++
			if owners[i] != devices[i] {
				t.Fatalf("winner %d owner=%q want %q", i, owners[i], devices[i])
			}
		}
	}
	if wins != 1 {
		t.Fatalf("got %d winners, want exactly 1 (results=%v)", wins, results)
	}
}

// TestReleaseClaimPersist: PendingHandoffTo survives a fresh Manager over the
// same store dir (proving the new field round-trips through disk).
func TestReleaseClaimPersist(t *testing.T) {
	dir := t.TempDir()
	store, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, store, nil, nil)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "s"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Release(meta.ID, "device-a", "device-b"); err != nil {
		t.Fatal(err)
	}
	mgr.FlushPersist()
	mgr.CloseAll(ctx)

	// A fresh manager over the same dir sees the release from disk: the
	// session is visible to the target device only, as a released row.
	store2, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr2 := session.NewManager(reg, store2, nil, nil)
	t.Cleanup(func() { mgr2.CloseAll(ctx) })
	if !ownsInList(mgr2, "device-b", meta.ID) {
		t.Fatal("released-to target does not see the session after restart")
	}
	if ownsInList(mgr2, "device-c", meta.ID) {
		t.Fatal("a non-target sees the persisted targeted release after restart")
	}
}
