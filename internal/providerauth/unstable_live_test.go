package providerauth

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// TestUnstableLiveDefersInsteadOfWedging covers the deferral branch: a LIVE
// that never settles inside StableReadDeadline decides nothing (MADR 0133).
//
// Be precise about what this does and does not establish. It was run against
// the pre-fix baseline (commit 1c84b3f) and PASSED there, so it is not a
// regression test for the original wedge: the old single-read path usually
// caught one of the churn's small complete writes, found it valid and fresher,
// and adopted it. What it does prove is that the new deferral branch is
// reachable and that reaching it leaves every generation untouched — without
// it, an unsettled read would fall through to the escalation below.
//
// The wedge that IS reproduced by a failing test is the equal-ordering one
// below. A torn write that stays torn across a full StableReadInterval is
// still escalated, deliberately: two invalid reads 100 ms apart are evidence of
// corruption, not of a bad instant. Phase 4's re-evaluation of an existing
// recovery_required is what stops any such escalation from being permanent.
func TestUnstableLiveDefersInsteadOfWedging(t *testing.T) {
	ctx := context.Background()
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	before := readManifest(t, dataDir, "fake")

	// Churn LIVE faster than the stable read can ever see two matching values,
	// for longer than StableReadDeadline, so the observation never settles.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 100; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.WriteFile(ad.live, []byte(`{"mode":"chatgpt","seq":`+itoa(i)+`}`), 0o600)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	st, err := c.Recover(ctx)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}

	// KNOWN INTERMITTENT FAILURE, and it is the code that is wrong, not this
	// test (2026-09-03).
	//
	// os.WriteFile truncates before writing, so a read landing in that window
	// sees a ZERO-LENGTH file. That read is "settled and invalid", not
	// unstable: its fingerprint is the hash of empty bytes, which is stable, so
	// two consecutive reads that both land in a truncate window agree and
	// stableObservation reports stable+invalid — which escalates. Rare per run,
	// reproducible over many.
	//
	// The property asserted here is the correct one, so the assertion stays.
	// See the MADR 0133 plan's 2026-09-03 deviation for the resolution options;
	// 0133 Phase 4 already stops such an escalation from being permanent, which
	// is why this is a gap rather than a regression.
	if st == StateRecoveryRequired {
		t.Fatal("a live credential being rewritten was escalated to recovery_required: " +
			"that state is terminal, so one bad instant costs a sign-in and every restart after it")
	}
	after := readManifest(t, dataDir, "fake")
	if len(after.Generations) != len(before.Generations) {
		t.Errorf("generations changed on an untrustworthy read: %d -> %d",
			len(before.Generations), len(after.Generations))
	}
	if cur := after.byLabel(LabelCurrent); cur == nil ||
		cur.Fingerprint != before.byLabel(LabelCurrent).Fingerprint {
		t.Error("CURRENT was moved on an untrustworthy read")
	}

	// And once the file settles, the very same credential is adopted normally —
	// deferring is not refusing.
	settled := writeLive(t, ad, "chatgpt", 500)
	st, err = c.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st != StateIdle {
		t.Fatalf("state = %s, want idle once LIVE settled", st)
	}
	if cur := readManifest(t, dataDir, "fake").byLabel(LabelCurrent); cur == nil ||
		cur.Fingerprint != FingerprintOf(settled) {
		t.Fatal("the settled credential was not adopted")
	}
}

// TestEqualOrderingIsAdoptedNotEscalated covers the second trapdoor: a provider
// that rewrites its credential without advancing its own ordering signal.
//
// `Fresher` demanded strictly-greater, so equal-but-different was "not
// fresher", which via the terminal escalation meant the same wedge. Equality is
// not the backward direction D24 forbids, so it is adopted (MADR 0133).
func TestEqualOrderingIsAdoptedNotEscalated(t *testing.T) {
	ctx := context.Background()
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 7)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	// Same mode, same sequence, different bytes — the fake adapter ignores the
	// extra field exactly as codex's adapter ignores fields it does not read.
	rewritten := []byte(`{"mode":"chatgpt","seq":7,"rewritten":true}`)
	if err := os.WriteFile(ad.live, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := c.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st != StateIdle {
		t.Fatalf("state = %s, want idle: an equal-ordering rewrite has not gone backward", st)
	}
	if cur := readManifest(t, dataDir, "fake").byLabel(LabelCurrent); cur == nil ||
		cur.Fingerprint != FingerprintOf(rewritten) {
		t.Fatal("the rewritten credential was not adopted")
	}
}

// TestRecoverIdleDefersOnAnUnstableObservation drives the deferral branch
// directly, because the churn test above cannot produce an unstable
// observation on demand.
//
// stableObservation only reports unstable when the file never settles inside
// StableReadDeadline, and whether a background writer achieves that depends on
// scheduling: under a loaded full-suite run the churn can settle long enough
// for two matching reads, the credential is adopted, and the churn test's
// manifest-unchanged assertion fails. That made it flaky rather than wrong.
// Calling recoverIdle with the observation the branch is about removes the
// race entirely.
func TestRecoverIdleDefersOnAnUnstableObservation(t *testing.T) {
	ctx := context.Background()
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	before := readManifest(t, dataDir, "fake")

	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	// A fingerprint that differs from CURRENT, marked unstable: the exact shape
	// stableObservation returns when it gives up at the deadline.
	unstable := observation{fp: FingerprintOf([]byte("mid-write")), valid: false, stable: false}

	st, err := c.recoverIdle(ctx, m, unstable, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st == StateRecoveryRequired {
		t.Fatal("an unstable observation was escalated: that state is terminal, " +
			"so one bad instant would cost a sign-in and every restart after it")
	}
	after := readManifest(t, dataDir, "fake")
	if len(after.Generations) != len(before.Generations) {
		t.Errorf("generations changed on an untrustworthy read: %d -> %d",
			len(before.Generations), len(after.Generations))
	}
	if cur := after.byLabel(LabelCurrent); cur == nil ||
		cur.Fingerprint != before.byLabel(LabelCurrent).Fingerprint {
		t.Error("CURRENT was moved on an untrustworthy read")
	}
}

// TestZeroLengthDefersButCorruptEscalates pins the boundary the 2026-09-03
// amendment draws (MADR 0133).
//
// Both inputs are "settled and unusable" to a naive reader. They must be
// treated differently: an empty file is what a read looks like when it lands
// inside another process's truncate-then-write, so it is evidence of nothing;
// a file with content that does not parse is corruption, and escalating it is
// the behaviour the transition table documents.
func TestZeroLengthDefersButCorruptEscalates(t *testing.T) {
	ctx := context.Background()

	t.Run("zero length defers", func(t *testing.T) {
		c, ad, dataDir := newFixture(t)
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		before := readManifest(t, dataDir, "fake")
		if err := os.WriteFile(ad.live, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st == StateRecoveryRequired {
			t.Fatal("an empty credential file was escalated: it is what a read " +
				"landing inside another writer's truncate looks like, not corruption")
		}
		after := readManifest(t, dataDir, "fake")
		if cur := after.byLabel(LabelCurrent); cur == nil ||
			cur.Fingerprint != before.byLabel(LabelCurrent).Fingerprint {
			t.Error("CURRENT was moved on an empty read")
		}
	})

	t.Run("corrupt content still escalates", func(t *testing.T) {
		c, ad, _ := newFixture(t)
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ad.live, []byte("this is not a credential"), 0o600); err != nil {
			t.Fatal(err)
		}

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required: a file with content that "+
				"does not parse is corruption and must not be silently deferred", st)
		}
	})
}
