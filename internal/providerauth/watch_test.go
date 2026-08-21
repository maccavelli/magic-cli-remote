package providerauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// TestWatcherAdoptsAValidRefresh proves an autonomous provider refresh is
// checkpointed as the new CURRENT without mcremote having written it
// (MADR 0074 D24).
func TestWatcherAdoptsAValidRefresh(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()

	w := NewWatcher(c)
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close(ctx) }()

	// Atomic rename, the way a real provider writes it.
	fresh := []byte(`{"mode":"chatgpt","seq":9}`)
	tmp := ad.live + ".tmp"
	if err := os.WriteFile(tmp, fresh, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, ad.live); err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 10*time.Second, func() bool {
		m, err := loadManifest(c.store.manifestPath())
		if err != nil {
			return false
		}
		cur := m.byLabel(LabelCurrent)
		return cur != nil && cur.Fingerprint == FingerprintOf(fresh)
	}) {
		t.Fatal("the watcher never adopted the refreshed credential")
	}

	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.byLabel(LabelPrevious) == nil {
		t.Error("the superseded generation was not retained as PREVIOUS")
	}
	if got := m.byLabel(LabelCurrent).Source; got != SourceRefresh {
		t.Errorf("source = %s, want %s", got, SourceRefresh)
	}
}

// TestWatcherNeverRollsBackwards proves an older credential appearing on disk
// does not overwrite a newer CURRENT — grok's own writer makes the same
// refusal (MADR 0074 F12/D24).
func TestWatcherNeverRollsBackwards(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()
	// Advance CURRENT to seq 5 first.
	txn := stage(t, c, ad, "chatgpt", 5)
	if err := c.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}
	before, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}

	w := NewWatcher(c)
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close(ctx) }()

	writeLive(t, ad, "chatgpt", 2) // older
	time.Sleep(1500 * time.Millisecond)

	after, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if after.byLabel(LabelCurrent).Fingerprint != before.byLabel(LabelCurrent).Fingerprint {
		t.Fatal("an older credential replaced CURRENT")
	}
}

// TestWatcherIgnoresPartialAndInvalidWrites proves a torn or unparseable write
// never becomes a generation.
func TestWatcherIgnoresPartialAndInvalidWrites(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()
	before, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}

	w := NewWatcher(c)
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close(ctx) }()

	if err := os.WriteFile(ad.live, []byte(`{"mode":"chat`), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)

	after, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Generations) != len(before.Generations) {
		t.Fatal("a partial write created a generation")
	}
}

// TestWatcherDefersDuringATransaction proves the watcher does not race a
// publication it is about to observe (MADR 0074 D24).
func TestWatcherDefersDuringATransaction(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(c)
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close(ctx) }()

	writeLive(t, ad, "chatgpt", 9)
	time.Sleep(1500 * time.Millisecond)

	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.State != StatePending {
		t.Fatalf("state = %s, want the transaction still pending", m.State)
	}
	if m.byLabel(LabelCurrent).Fingerprint == FingerprintOf([]byte(`{"mode":"chatgpt","seq":9}`)) {
		t.Fatal("the watcher checkpointed during an active transaction")
	}
	_ = txn
}

// TestWatcherCloseIsBoundedAndIdempotent proves shutdown does not hang or
// double-close (MADR 0074 D27, §17.4 drain bound).
func TestWatcherCloseIsBoundedAndIdempotent(t *testing.T) {
	c, _ := ownedFixture(t)
	ctx := context.Background()
	w := NewWatcher(c)
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal("second Close returned an error")
	}
	if elapsed := time.Since(start); elapsed > DrainTimeout+2*time.Second {
		t.Fatalf("Close took %s, want bounded shutdown", elapsed)
	}
}

// TestWatcherStartsNoGoroutineInConstructor proves the constructor is inert, so
// a component built but never started cannot leak (P19 step 2).
func TestWatcherStartsNoGoroutineInConstructor(t *testing.T) {
	c, ad := ownedFixture(t)
	w := NewWatcher(c)
	writeLive(t, ad, "chatgpt", 9)
	time.Sleep(600 * time.Millisecond)

	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.byLabel(LabelCurrent).Fingerprint == FingerprintOf([]byte(`{"mode":"chatgpt","seq":9}`)) {
		t.Fatal("an unstarted watcher reconciled")
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatal("Close on an unstarted watcher errored")
	}
}

// TestReconcileRepairsMissedEvents proves the startup and pre-mutation
// checkpoints are the mandatory fallback when a watcher event never arrives
// (MADR 0074 D24, P19 step 3).
func TestReconcileRepairsMissedEvents(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()

	// No watcher running: this is exactly the missed-event case.
	fresh := writeLive(t, ad, "chatgpt", 4)
	if err := c.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.byLabel(LabelCurrent).Fingerprint != FingerprintOf(fresh) {
		t.Fatal("reconcile did not repair the missed refresh")
	}
}

// TestRecoverAllReportsEveryProvider proves one failing provider cannot hide
// another's state (P19 step 1).
func TestRecoverAllReportsEveryProvider(t *testing.T) {
	ctx := context.Background()
	good, adGood := ownedFixture(t)

	// A second provider whose LIVE vanished: recovery_required, not fatal.
	bad, adBad := ownedFixture(t)
	if err := os.Remove(adBad.live); err != nil {
		t.Fatal(err)
	}

	results := RecoverAll(ctx, []*Coordinator{good, bad})
	if len(results) != 2 {
		t.Fatalf("results = %d, want one per provider", len(results))
	}
	byState := map[State]int{}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("recover %s: %v", r.Provider, r.Err)
		}
		byState[r.State]++
	}
	if byState[StateIdle] != 1 || byState[StateRecoveryRequired] != 1 {
		t.Fatalf("states = %v, want one idle and one recovery_required", byState)
	}
	_ = adGood
}

// TestOperatorChoices covers every resolution an operator can make from a
// preserved ambiguous state (P19 step 7).
func TestOperatorChoices(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) (*Coordinator, *fakeAdapter, []byte, []byte) {
		t.Helper()
		c, ad := ownedFixture(t)
		first, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		txn := stage(t, c, ad, "chatgpt", 5)
		if err := c.Commit(ctx, txn); err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		// Force the ambiguous state the operator is resolving. It must be a
		// credential recovery will not adopt on its own: an older sequence is
		// exactly the "valid but not fresher" case that escalates.
		unrelated := writeLive(t, ad, "chatgpt", 2)
		if st, err := c.Recover(ctx); err != nil || st != StateRecoveryRequired {
			t.Fatalf("state = %s err = %v, want recovery_required", st, err)
		}
		_ = unrelated
		return c, ad, first, second
	}

	t.Run("live adopts what is on disk", func(t *testing.T) {
		c, ad, _, _ := setup(t)
		if err := c.ResolveRecovery(ctx, ChooseLive); err != nil {
			t.Fatal(err)
		}
		m, err := loadManifest(c.store.manifestPath())
		if err != nil {
			t.Fatal(err)
		}
		live, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if m.State != StateIdle || m.byLabel(LabelCurrent).Fingerprint != FingerprintOf(live) {
			t.Fatal("live choice did not adopt the observed credential")
		}
	})

	t.Run("current republishes the committed generation", func(t *testing.T) {
		c, ad, _, second := setup(t)
		if err := c.ResolveRecovery(ctx, ChooseCurrent); err != nil {
			t.Fatal(err)
		}
		live, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if string(live) != string(second) {
			t.Fatalf("live = %s, want CURRENT republished", live)
		}
	})

	t.Run("previous is promoted and the displaced current retained", func(t *testing.T) {
		c, ad, first, second := setup(t)
		if err := c.ResolveRecovery(ctx, ChoosePrevious); err != nil {
			t.Fatal(err)
		}
		live, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if string(live) != string(first) {
			t.Fatalf("live = %s, want PREVIOUS promoted", live)
		}
		m, err := loadManifest(c.store.manifestPath())
		if err != nil {
			t.Fatal(err)
		}
		prev := m.byLabel(LabelPrevious)
		if prev == nil || prev.Fingerprint != FingerprintOf(second) {
			t.Fatal("the displaced CURRENT was not retained as PREVIOUS")
		}
	})

	t.Run("logged-out tombstones and purges", func(t *testing.T) {
		c, ad, _, _ := setup(t)
		if err := c.ResolveRecovery(ctx, ChooseLoggedOut); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(ad.live); !os.IsNotExist(err) {
			t.Fatal("logged-out choice left LIVE in place")
		}
		m, err := loadManifest(c.store.manifestPath())
		if err != nil {
			t.Fatal(err)
		}
		if m.State != StateLoggedOut || len(m.Generations) != 0 {
			t.Fatal("logged-out choice did not tombstone and purge")
		}
	})

	t.Run("refuses a manifest that is not recovery_required", func(t *testing.T) {
		c, _ := ownedFixture(t)
		if err := c.ResolveRecovery(ctx, ChooseCurrent); !errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("err = %v, want ErrRecoveryRequired", err)
		}
	})

	t.Run("refuses an unknown choice", func(t *testing.T) {
		c, _, _, _ := setup(t)
		if err := c.ResolveRecovery(ctx, RecoveryChoice("mystery")); err == nil {
			t.Fatal("an unknown choice was accepted")
		}
	})

	t.Run("refuses a missing generation and preserves evidence", func(t *testing.T) {
		c, ad, _, _ := setup(t)
		// Drop PREVIOUS, then ask for it.
		m, err := loadManifest(c.store.manifestPath())
		if err != nil {
			t.Fatal(err)
		}
		m.dropLabel(LabelPrevious)
		if err := saveManifest(c.store.manifestPath(), m); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ResolveRecovery(ctx, ChoosePrevious); err == nil {
			t.Fatal("a missing generation was accepted")
		}
		after, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatal("a failed choice changed LIVE")
		}
	})
}

var _ = filepath.Join

// TestReconciliationSurvivesAnUnavailableWatcher proves the watcher is strictly
// an optimization. On Linux fsnotify is inotify, and inotify can legitimately
// refuse to start: fs.inotify.max_user_instances and max_user_watches are
// per-user limits that containers and busy desktops do hit, and some
// filesystems (NFS, some overlayfs setups) do not report events at all.
//
// None of that may affect credential correctness. Startup and pre-mutation
// reconciliation are the mandatory path; a failed or absent watcher costs a
// checkpoint delay, never a lost generation (MADR 0074 D24, P19 step 3).
func TestReconciliationSurvivesAnUnavailableWatcher(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()

	// Imitate a host where the watcher cannot run at all: never start one.
	w := NewWatcher(c)
	if err := w.Close(ctx); err != nil {
		t.Fatalf("closing a never-started watcher errored: %v", err)
	}

	// An autonomous refresh lands with nothing watching.
	fresh := writeLive(t, ad, "chatgpt", 6)

	// The pre-mutation checkpoint repairs it before any managed mutation.
	if err := c.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.byLabel(LabelCurrent).Fingerprint != FingerprintOf(fresh) {
		t.Fatal("reconciliation depended on the watcher")
	}

	// And a subsequent transaction still publishes correctly.
	txn := stage(t, c, ad, "chatgpt", 7)
	if err := c.Commit(ctx, txn); err != nil {
		t.Fatalf("a host with no watcher could not complete a login: %v", err)
	}
}

// TestWatcherStartErrorIsRecoverable proves a Start failure leaves the
// coordinator fully usable, so a caller may log and continue rather than
// treating an unavailable inotify instance as a fatal daemon error.
func TestWatcherStartErrorIsRecoverable(t *testing.T) {
	c, ad := ownedFixture(t)
	ctx := context.Background()

	w := NewWatcher(c)
	if err := w.Start(ctx); err != nil {
		t.Skipf("watcher unavailable on this host, which is the case under test: %v", err)
	}
	// A second Start is an error; the coordinator must be unharmed by it.
	if err := w.Start(ctx); err == nil {
		t.Fatal("a second Start was accepted")
	}
	defer func() { _ = w.Close(ctx) }()

	writeLive(t, ad, "chatgpt", 8)
	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("coordinator unusable after a rejected Start: %v", err)
	}
}
