package providerauth

import (
	"context"
	"errors"
	"os"
	"testing"
)

// TestUnparseableLiveIsUnmanagedNotAmbiguous is a regression for a lockout
// observed in production on 2026-08-21.
//
// The host's ~/.codex/auth.json was the three-byte stub `{}` — Codex keeps that
// account's real session elsewhere. Seeding could not parse it, escalated to
// recovery_required, and recorded no generation. The operator was then locked
// out of the one action that would have fixed it.
//
// A credential we cannot parse is not ambiguity. Ambiguity means two plausible
// truths and something worth protecting; here there is nothing retained and
// nothing to lose, so the honest state is unmanaged.
func TestUnparseableLiveIsUnmanagedNotAmbiguous(t *testing.T) {
	for _, body := range []string{`{}`, `not json at all`, ``} {
		t.Run(body, func(t *testing.T) {
			c, ad, _ := newFixture(t)
			if err := os.WriteFile(ad.live, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := c.Seed(ctx); err != nil {
				t.Fatal(err)
			}

			m, err := loadManifest(c.store.manifestPath())
			if err != nil {
				t.Fatal(err)
			}
			if m.State == StateRecoveryRequired {
				t.Fatal("an unparseable credential escalated to recovery_required with nothing to recover")
			}
			if len(m.Generations) != 0 {
				t.Fatal("an unparseable credential was seeded as known-good")
			}

			st, err := c.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if st.BackupState != BackupUnmanaged {
				t.Fatalf("backup state = %s, want unmanaged", st.BackupState)
			}
			// The whole point: a fresh sign-in must still be possible.
			if _, err := c.Begin(ctx, SourceDeviceAuth); err != nil {
				t.Fatalf("sign-in blocked on an unmanaged provider: %v", err)
			}
		})
	}
}

// TestSignInIsAllowedFromRecoveryRequired proves a deliberate login is never
// blocked by an ambiguous manifest.
//
// A login publishes a brand-new credential produced in an isolated home. It
// restores nothing and depends on no retained generation, so the reasoning that
// makes automatic restores unsafe does not apply to it. Blocking it turns a
// recoverable state into a dead end (production lockout, 2026-08-21).
func TestSignInIsAllowedFromRecoveryRequired(t *testing.T) {
	c, ad, _ := newFixture(t)
	ctx := context.Background()
	writeLive(t, ad, "chatgpt", 5)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	// Force the ambiguous state.
	writeLive(t, ad, "chatgpt", 2)
	if st, err := c.Recover(ctx); err != nil || st != StateRecoveryRequired {
		t.Fatalf("state = %s err = %v, want recovery_required", st, err)
	}

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatalf("sign-in blocked while recovery was pending: %v", err)
	}

	// Completing it resolves the ambiguity rather than compounding it.
	cand := []byte(`{"mode":"chatgpt","seq":9}`)
	if err := os.WriteFile(txn.Home()+"/auth.json", cand, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.StageCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}

	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.State != StateIdle {
		t.Fatalf("state = %s, want idle after a successful sign-in", m.State)
	}
	if m.byLabel(LabelCurrent).Fingerprint != FingerprintOf(cand) {
		t.Fatal("the new credential did not become CURRENT")
	}
}

// TestAutomaticMutationStillBlockedInRecoveryRequired proves the fix above did
// not open the door to the thing recovery_required actually exists to prevent:
// mcremote silently changing a credential it does not understand.
func TestAutomaticMutationStillBlockedInRecoveryRequired(t *testing.T) {
	c, ad, _ := newFixture(t)
	ctx := context.Background()
	writeLive(t, ad, "chatgpt", 5)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	older := writeLive(t, ad, "chatgpt", 2)
	if st, _ := c.Recover(ctx); st != StateRecoveryRequired {
		t.Fatalf("state = %s, want recovery_required", st)
	}

	// Reconciliation must not adopt anything while ambiguous.
	if err := c.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := loadManifest(c.store.manifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if m.State != StateRecoveryRequired {
		t.Fatal("reconciliation cleared an ambiguous state on its own")
	}
	got, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(older) {
		t.Fatal("reconciliation mutated LIVE while ambiguous")
	}
	// And a restore still requires an explicit operator choice.
	if err := c.ResolveRecovery(ctx, ChooseLive); err != nil {
		t.Fatalf("explicit operator choice failed: %v", err)
	}
}

// TestRecoveryRequiredWithNothingRetainedIsNotADeadEnd pins the exact shape of
// the production manifest: recovery_required with no generations at all.
func TestRecoveryRequiredWithNothingRetainedIsNotADeadEnd(t *testing.T) {
	c, ad, _ := newFixture(t)
	ctx := context.Background()
	if err := os.WriteFile(ad.live, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Force the observed production state directly.
	m := newManifest("fake")
	m.State = StateRecoveryRequired
	if err := saveManifest(c.store.manifestPath(), m); err != nil {
		t.Fatal(err)
	}

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.RecoveryAvailable {
		t.Fatal("recovery was advertised with no retained generation")
	}
	if st.BackupState != BackupReauthRequired {
		t.Fatalf("backup state = %s, want reauth_required when nothing can be restored", st.BackupState)
	}
	// A restore genuinely cannot work here, and must say so rather than hang.
	if err := c.ResolveRecovery(ctx, ChooseCurrent); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("err = %v, want ErrInvalidCandidate", err)
	}
	// But re-authentication — the only thing that can actually help — works.
	if _, err := c.Begin(ctx, SourceDeviceAuth); err != nil {
		t.Fatalf("re-authentication blocked with nothing to recover: %v", err)
	}
}
