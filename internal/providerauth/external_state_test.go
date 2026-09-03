package providerauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// realityAdapter is a fakeAdapter that also implements RealityReporter, so the
// optional capability can be exercised without importing a provider.
type realityAdapter struct {
	*fakeAdapter
	external bool
	err      error
	probes   int
}

func (r *realityAdapter) CredentialIsExternal(_ context.Context) (bool, error) {
	r.probes++
	return r.external, r.err
}

// newRealityFixture mirrors newFixture but with the reality-aware adapter.
func newRealityFixture(t *testing.T) (*Coordinator, *realityAdapter, string) {
	t.Helper()
	dataDir := t.TempDir()
	liveDir := t.TempDir()
	ad := &realityAdapter{fakeAdapter: &fakeAdapter{
		id:   "fake",
		live: filepath.Join(liveDir, "auth.json"),
		lock: filepath.Join(liveDir, "auth.json"),
	}}
	c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return c, ad, dataDir
}

// writeStub puts the exact shape codex-cli 0.152.1 leaves behind: well-formed
// JSON, no auth material (MADR 0134).
func writeStub(t *testing.T, ad *realityAdapter) []byte {
	t.Helper()
	b := []byte(`{}`)
	if err := os.WriteFile(ad.live, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestExternalCredentialIsNotAnAmbiguity is the reason MADR 0134 exists.
//
// A settled, well-formed, credential-free LIVE plus a provider CLI that is
// signed in from elsewhere is not something an operator can resolve: every
// `mcremote auth-recovery choose` answer is destructive there — adopt the stub,
// write a possibly-revoked stale credential into a file nothing reads, or
// tombstone a live session. So it must not be reported as a decision.
func TestExternalCredentialIsNotAnAmbiguity(t *testing.T) {
	ctx := context.Background()

	t.Run("signed in elsewhere is external, not recovery_required", func(t *testing.T) {
		c, ad, dataDir := newRealityFixture(t)
		writeLive(t, ad.fakeAdapter, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		before := readManifest(t, dataDir, "fake")
		ad.external = true
		stub := writeStub(t, ad)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateExternal {
			t.Fatalf("state = %s, want external", st)
		}
		if ad.probes == 0 {
			t.Error("the reality probe was never consulted")
		}
		after := readManifest(t, dataDir, "fake")
		if len(after.Generations) != len(before.Generations) {
			t.Errorf("generations changed: %d -> %d",
				len(before.Generations), len(after.Generations))
		}
		got, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(stub) {
			t.Error("recovery mutated LIVE")
		}
		// The projection the phone reads must agree with the manifest.
		status, err := c.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if status.BackupState != BackupUnsupported || status.RecoveryAvailable {
			t.Errorf("status = %+v, want unsupported and not recoverable", status)
		}
	})

	t.Run("not signed in elsewhere still escalates", func(t *testing.T) {
		c, ad, _ := newRealityFixture(t)
		writeLive(t, ad.fakeAdapter, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		ad.external = false
		writeStub(t, ad)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required: an unusable file with no "+
				"credential anywhere is exactly the corruption MADR 0133 preserved", st)
		}
	})

	t.Run("a probe error still escalates", func(t *testing.T) {
		c, ad, _ := newRealityFixture(t)
		writeLive(t, ad.fakeAdapter, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		ad.external = true // would say external, but the probe fails
		ad.err = errors.New("cli unreachable")
		writeStub(t, ad)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateRecoveryRequired {
			t.Fatalf("state = %s, want recovery_required: an unreachable CLI must "+
				"never invent a healthy state", st)
		}
	})

	t.Run("an adoptable live is never probed", func(t *testing.T) {
		c, ad, _ := newRealityFixture(t)
		writeLive(t, ad.fakeAdapter, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		ad.external = true
		writeLive(t, ad.fakeAdapter, "chatgpt", 2)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
		if ad.probes != 0 {
			t.Fatalf("probed %d times on a healthy host: a working credential must "+
				"never cost a process spawn", ad.probes)
		}
	})

	t.Run("external is left automatically when the file becomes usable", func(t *testing.T) {
		c, ad, dataDir := newRealityFixture(t)
		writeLive(t, ad.fakeAdapter, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		ad.external = true
		writeStub(t, ad)
		if st, err := c.Recover(ctx); err != nil || st != StateExternal {
			t.Fatalf("setup: state = %s err = %v", st, err)
		}

		// The provider writes a real credential back into the file.
		ad.external = false
		restored := writeLive(t, ad.fakeAdapter, "chatgpt", 9)

		st, err := c.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st != StateIdle {
			t.Fatalf("state = %s, want idle: external caches an environment fact "+
				"and must not outlive it", st)
		}
		if cur := readManifest(t, dataDir, "fake").byLabel(LabelCurrent); cur == nil ||
			cur.Fingerprint != FingerprintOf(restored) {
			t.Fatal("the restored credential was not adopted")
		}
	})
}

// TestCommitInExternalIsUnsupportedNotAmbiguous pins the error a caller sees.
// Publishing into a file the provider is not reading would report success and
// change nothing a session can use.
func TestCommitInExternalIsUnsupportedNotAmbiguous(t *testing.T) {
	ctx := context.Background()
	c, ad, dataDir := newRealityFixture(t)
	writeLive(t, ad.fakeAdapter, "chatgpt", 1)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	txn := stage(t, c, ad.fakeAdapter, "chatgpt", 2)
	forceState(t, dataDir, "fake", StateExternal)

	err := c.Commit(ctx, txn)
	if !errors.Is(err, ErrUnsupportedBackend) {
		t.Fatalf("commit err = %v, want ErrUnsupportedBackend", err)
	}
	if errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("commit must not send the operator to a prompt whose every answer is destructive")
	}
}

// TestExternalStateManifestIsTolerated covers the FORWARD half of the durable
// format: a manifest carrying the new state loads in this binary and is
// re-evaluated rather than rejected.
//
// The backward half is deliberately not asserted here, because it is false.
// Verified 2026-09-03 against a pre-0134 worktree: `loadManifest` validates the
// state, so an older binary rejects such a manifest outright with
// `unknown manifest state "external"` and every coordinator call for that
// provider then fails. `DisallowUnknownFields` makes any additive manifest
// change one-way in the same manner — MADR 0133's operator_choice field already
// did it. Downgrade is an operator procedure, recorded in the 0134 plan's
// Rollback section, not a fallback a test could pin.
func TestExternalStateManifestIsTolerated(t *testing.T) {
	ctx := context.Background()
	c, ad, dataDir := newRealityFixture(t)
	writeLive(t, ad.fakeAdapter, "chatgpt", 1)
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dataDir, "provider-auth", "fake", "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	obj["state"] = "external"
	patched, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, patched, 0o600); err != nil {
		t.Fatal(err)
	}

	// LIVE still matches CURRENT, so any binary — new or old — should resolve
	// this to idle rather than erroring on an unknown state string.
	st, err := c.Recover(ctx)
	if err != nil {
		t.Fatalf("a manifest with state=external must load, got: %v", err)
	}
	if st != StateIdle {
		t.Fatalf("state = %s, want idle", st)
	}
}
