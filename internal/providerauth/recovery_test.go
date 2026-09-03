package providerauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecoveryTransitionTable walks the exhaustive P17 step 9 table. Each case
// builds a durable manifest and an observed LIVE, then asserts the recovered
// state and that nothing was destroyed on an ambiguous input.
func TestRecoveryTransitionTable(t *testing.T) {
	ctx := context.Background()

	// arrange builds a coordinator with a seeded CURRENT at seq 1 and returns
	// the committed CURRENT fingerprint.
	arrange := func(t *testing.T) (*Coordinator, *fakeAdapter, string, Fingerprint) {
		t.Helper()
		c, ad, dataDir := newFixture(t)
		cur := writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		return c, ad, dataDir, FingerprintOf(cur)
	}

	t.Run("idle/live equals current stays idle", func(t *testing.T) {
		c, _, dataDir, _ := arrange(t)
		// A pending directory nothing labels is stale litter and may go.
		stale := filepath.Join(dataDir, "provider-auth", "fake", "pending", "orphan")
		if err := os.MkdirAll(stale, 0o700); err != nil {
			t.Fatal(err)
		}
		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Error("stale pending directory survived recovery")
		}
	})

	t.Run("idle/live fresher reconciles as autonomous refresh", func(t *testing.T) {
		c, ad, dataDir, oldFP := arrange(t)
		fresh := writeLive(t, ad, "chatgpt", 2)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		m := readManifest(t, dataDir, "fake")
		if got := m.byLabel(LabelCurrent); got == nil || got.Fingerprint != FingerprintOf(fresh) {
			t.Fatal("CURRENT was not advanced to the fresher live credential")
		}
		if got := m.byLabel(LabelPrevious); got == nil || got.Fingerprint != oldFP {
			t.Fatal("the superseded CURRENT was not kept as PREVIOUS")
		}
	})

	t.Run("idle/live older is never rolled back", func(t *testing.T) {
		c, ad, dataDir, _ := arrange(t)
		// Commit seq 5 so CURRENT is ahead, then present an older live file.
		txn := stage(t, c, ad, "chatgpt", 5)
		if err := c.Commit(ctx, txn); err != nil {
			t.Fatal(err)
		}
		older := writeLive(t, ad, "chatgpt", 2)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required", st)
		}
		got, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(older) {
			t.Fatal("recovery must not overwrite an unexpected live file")
		}
		if len(readManifest(t, dataDir, "fake").Generations) != 2 {
			t.Fatal("recovery discarded retained generations")
		}
	})

	t.Run("idle/live absent requires recovery", func(t *testing.T) {
		c, ad, _, _ := arrange(t)
		if err := os.Remove(ad.live); err != nil {
			t.Fatal(err)
		}
		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required", st)
		}
	})

	t.Run("idle/live invalid requires recovery", func(t *testing.T) {
		c, ad, _, _ := arrange(t)
		if err := os.WriteFile(ad.live, []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required", st)
		}
	})

	t.Run("pending/live equals expected aborts to idle", func(t *testing.T) {
		c, ad, dataDir, _ := arrange(t)
		txn := stage(t, c, ad, "chatgpt", 2)
		home := txn.Home()

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Error("recovery left the abandoned pending home behind")
		}
		m := readManifest(t, dataDir, "fake")
		if m.Transaction != nil || m.byLabel(LabelPending) != nil {
			t.Fatal("a restarted daemon must not keep an ownerless candidate")
		}
		if m.byLabel(LabelCurrent) == nil {
			t.Fatal("recovery destroyed CURRENT")
		}
	})

	t.Run("pending/live changed requires recovery", func(t *testing.T) {
		c, ad, _, _ := arrange(t)
		_ = stage(t, c, ad, "chatgpt", 2)
		writeLive(t, ad, "chatgpt", 77) // something else moved LIVE meanwhile

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required", st)
		}
	})

	t.Run("committing/live equals candidate finishes rotation", func(t *testing.T) {
		c, ad, dataDir, oldFP := arrange(t)
		txn := stage(t, c, ad, "chatgpt", 2)
		cand := []byte(`{"mode":"chatgpt","seq":2}`)

		// Simulate a crash after the rename but before the label rotation.
		forceState(t, dataDir, "fake", StateCommitting)
		if err := os.WriteFile(ad.live, cand, 0o600); err != nil {
			t.Fatal(err)
		}

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		m := readManifest(t, dataDir, "fake")
		if got := m.byLabel(LabelCurrent); got == nil || got.Fingerprint != FingerprintOf(cand) {
			t.Fatal("committing recovery did not promote the published candidate")
		}
		if got := m.byLabel(LabelPrevious); got == nil || got.Fingerprint != oldFP {
			t.Fatal("committing recovery lost the prior CURRENT")
		}
		if m.Transaction != nil {
			t.Fatal("transaction fields were not cleared")
		}
		_ = txn
	})

	t.Run("committing/live equals old current discards the candidate", func(t *testing.T) {
		c, ad, dataDir, oldFP := arrange(t)
		txn := stage(t, c, ad, "chatgpt", 2)
		forceState(t, dataDir, "fake", StateCommitting)
		// LIVE never changed: the crash landed before the rename.

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		m := readManifest(t, dataDir, "fake")
		if got := m.byLabel(LabelCurrent); got == nil || got.Fingerprint != oldFP {
			t.Fatal("CURRENT should be unchanged when the publish never happened")
		}
		if m.byLabel(LabelPending) != nil {
			t.Fatal("the uncommitted candidate should have been discarded")
		}
		if _, err := os.Stat(txn.Home()); !os.IsNotExist(err) {
			t.Error("pending data survived an aborted commit")
		}
	})

	t.Run("committing/live is a third value requires recovery", func(t *testing.T) {
		c, ad, dataDir, _ := arrange(t)
		_ = stage(t, c, ad, "chatgpt", 2)
		forceState(t, dataDir, "fake", StateCommitting)
		writeLive(t, ad, "chatgpt", 404)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required", st)
		}
		m := readManifest(t, dataDir, "fake")
		if m.byLabel(LabelPending) == nil || m.byLabel(LabelCurrent) == nil {
			t.Fatal("ambiguous committing state must preserve every generation")
		}
	})

	t.Run("logged_out/live absent stays logged out", func(t *testing.T) {
		c, _, dataDir, _ := arrange(t)
		if err := c.RecordLogout(ctx); err != nil {
			t.Fatal(err)
		}
		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateLoggedOut {
			t.Fatalf("state = %s, want logged_out", st)
		}
		gens, err := os.ReadDir(filepath.Join(dataDir, "provider-auth", "fake", "generations"))
		if err != nil {
			t.Fatal(err)
		}
		if len(gens) != 0 {
			t.Fatal("logged out state retained credential payloads")
		}
	})

	t.Run("logged_out/live matches tombstone finishes removal", func(t *testing.T) {
		c, ad, _, _ := arrange(t)
		if err := c.RecordLogout(ctx); err != nil {
			t.Fatal(err)
		}
		// A crash between tombstone and unlink leaves the expected file back.
		restoreTombstoned(t, c, ad)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateLoggedOut {
			t.Fatalf("state = %s, want logged_out", st)
		}
		if _, err := os.Stat(ad.live); !os.IsNotExist(err) {
			t.Fatal("recovery did not finish the journalled removal")
		}
	})

	t.Run("logged_out/live is a new credential requires recovery", func(t *testing.T) {
		c, ad, _, _ := arrange(t)
		if err := c.RecordLogout(ctx); err != nil {
			t.Fatal(err)
		}
		// Someone signed in again outside mcremote.
		fresh := writeLive(t, ad, "chatgpt", 9)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required", st)
		}
		got, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(fresh) {
			t.Fatal("recovery must never delete an external credential")
		}
	})

	// AMENDED by MADR 0133. This row used to read "recovery_required makes no
	// automatic mutation" and assert the state was still recovery_required
	// afterwards. That made one ambiguous observation permanent — reconciliation
	// skips the state too, so nothing automatic could ever leave it and the only
	// exits were a CLI command or another sign-in.
	//
	// The invariant the row actually protects is unchanged and is asserted in
	// every case below: recovery must never mutate LIVE. What changed is that
	// the state is re-evaluated against fresh evidence rather than assumed to
	// still hold, because a successful ResolveRecovery always leaves the state,
	// so being in it means no operator decision is in effect.
	t.Run("recovery_required is re-evaluated, never mutates live", func(t *testing.T) {
		t.Run("live matching current clears the state", func(t *testing.T) {
			c, ad, dataDir, _ := arrange(t)
			forceState(t, dataDir, "fake", StateRecoveryRequired)
			before := writeLive(t, ad, "chatgpt", 1)

			st, err := c.Recover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if st != StateIdle {
				t.Fatalf("state = %s, want idle: LIVE matches CURRENT, so nothing is ambiguous", st)
			}
			assertLiveUnchanged(t, ad, before)
		})

		t.Run("an older live keeps the state", func(t *testing.T) {
			c, ad, dataDir, _ := arrange(t)
			txn := stage(t, c, ad, "chatgpt", 5)
			if err := c.Commit(ctx, txn); err != nil {
				t.Fatal(err)
			}
			forceState(t, dataDir, "fake", StateRecoveryRequired)
			before := writeLive(t, ad, "chatgpt", 2)

			st, err := c.Recover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if st != StateRecoveryRequired {
				t.Fatalf("state = %s, want recovery_required: an older LIVE is still ambiguous", st)
			}
			assertLiveUnchanged(t, ad, before)
		})

		t.Run("a recorded operator attempt keeps the state", func(t *testing.T) {
			c, ad, dataDir, _ := arrange(t)
			forceState(t, dataDir, "fake", StateRecoveryRequired)
			forceOperatorChoice(t, dataDir, "fake", ChoosePrevious)
			// LIVE is adoptable, so only the recorded attempt can hold the
			// state: a resolution that failed must not be second-guessed.
			before := writeLive(t, ad, "chatgpt", 1)

			st, err := c.Recover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if st != StateRecoveryRequired {
				t.Fatalf("state = %s, want recovery_required: an operator already ruled here", st)
			}
			assertLiveUnchanged(t, ad, before)
		})
	})

	t.Run("no manifest seeds a valid live credential", func(t *testing.T) {
		c, ad, dataDir := newFixture(t)
		want := writeLive(t, ad, "chatgpt", 1)
		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		m := readManifest(t, dataDir, "fake")
		if got := m.byLabel(LabelCurrent); got == nil || got.Fingerprint != FingerprintOf(want) {
			t.Fatal("a first sighting of a valid LIVE should seed CURRENT")
		}
	})

	t.Run("no manifest and no live stays unmanaged", func(t *testing.T) {
		c, _, dataDir := newFixture(t)
		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		m := readManifest(t, dataDir, "fake")
		if len(m.Generations) != 0 {
			t.Fatal("a cold host must not gain an artificial generation")
		}
		if m.LoggedOutAt != (m.LoggedOutAt.UTC()) || m.State == StateLoggedOut {
			t.Fatal("a cold host must not gain a logout tombstone")
		}
	})
}

// forceState rewrites only the durable state field, imitating a crash that left
// the journal at that transition.
// assertLiveUnchanged is the invariant every recovery_required case shares:
// recovery may change the manifest, never the credential.
func assertLiveUnchanged(t *testing.T, ad *fakeAdapter, want []byte) {
	t.Helper()
	got, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("recovery mutated LIVE")
	}
}

// forceOperatorChoice imitates a ResolveRecovery that recorded its attempt and
// then failed, leaving the manifest in recovery_required (MADR 0133).
func forceOperatorChoice(t *testing.T, dataDir, provider string, ch RecoveryChoice) {
	t.Helper()
	path := filepath.Join(dataDir, "provider-auth", provider, "manifest.json")
	m, err := loadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	m.OperatorChoice, m.OperatorChoiceAt = ch, time.Now().UTC()
	if err := saveManifest(path, m); err != nil {
		t.Fatal(err)
	}
}

func forceState(t *testing.T, dataDir, provider string, s State) {
	t.Helper()
	path := filepath.Join(dataDir, "provider-auth", provider, "manifest.json")
	m, err := loadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	m.State = s
	if err := saveManifest(path, m); err != nil {
		t.Fatal(err)
	}
}

// restoreTombstoned puts back exactly the bytes a logout tombstone expects to
// remove, imitating a crash between the durable tombstone and the unlink.
func restoreTombstoned(t *testing.T, c *Coordinator, ad *fakeAdapter) {
	t.Helper()
	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq < 50; seq++ {
		b := []byte(`{"mode":"chatgpt","seq":` + itoa(seq) + `}`)
		if FingerprintOf(b) == m.LoggedOutExpected {
			if err := os.WriteFile(ad.live, b, 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("could not reconstruct the tombstoned credential")
}
