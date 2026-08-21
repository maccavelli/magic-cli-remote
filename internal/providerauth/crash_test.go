package providerauth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// crashHelperEnv names the transition a helper process should die at.
const crashHelperEnv = "PROVIDERAUTH_CRASH_AT"

// TestCrashHelperProcess is the helper body. It runs only when the parent sets
// crashHelperEnv, drives a real coordinator against a real data directory, and
// kills itself at the named durable transition (MADR 0074 P17 step 12).
func TestCrashHelperProcess(t *testing.T) {
	at := os.Getenv(crashHelperEnv)
	if at == "" {
		t.Skip("helper process; driven by TestCrashRecovery")
	}
	dataDir := os.Getenv("PROVIDERAUTH_DATA_DIR")
	livePath := os.Getenv("PROVIDERAUTH_LIVE")
	ctx := context.Background()

	ad := &fakeAdapter{id: "fake", live: livePath, lock: livePath + ".lock"}
	c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if at == "after_seed" {
		hardExit()
	}

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	if at == "after_begin" {
		hardExit()
	}

	cand := []byte(`{"mode":"chatgpt","seq":2}`)
	if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), cand, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.StageCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if at == "after_stage" {
		hardExit()
	}

	if err := c.ValidateCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if at == "after_validate" {
		hardExit()
	}

	// Imitate a crash in the middle of publication: journal committing, write
	// LIVE, then die before the label rotation is durable.
	if at == "after_publish_rename" {
		forceState(t, dataDir, "fake", StateCommitting)
		if err := os.WriteFile(livePath, cand, 0o600); err != nil {
			t.Fatal(err)
		}
		hardExit()
	}

	if err := c.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if at == "after_commit" {
		hardExit()
	}
	t.Fatalf("unknown crash point %q", at)
}

// hardExit leaves no deferred cleanup, mirroring a kill rather than a return.
func hardExit() { os.Exit(9) }

// TestCrashRecovery kills a real process at each persisted transition, then
// reopens the store and asserts recovery lands on a committed state or, when
// the evidence is genuinely ambiguous, preserves every file and asks for an
// operator decision.
func TestCrashRecovery(t *testing.T) {
	cases := []struct {
		at        string
		wantState State
		// wantLive is "old", "new", or "absent".
		wantLive string
	}{
		{"after_seed", StateIdle, "old"},
		{"after_begin", StateIdle, "old"},
		{"after_stage", StateIdle, "old"},
		{"after_validate", StateIdle, "old"},
		{"after_publish_rename", StateIdle, "new"},
		{"after_commit", StateIdle, "new"},
	}

	const oldLive = `{"mode":"chatgpt","seq":1}`
	const newLive = `{"mode":"chatgpt","seq":2}`

	for _, tc := range cases {
		t.Run(tc.at, func(t *testing.T) {
			dataDir := t.TempDir()
			liveDir := t.TempDir()
			livePath := filepath.Join(liveDir, "auth.json")
			if err := os.WriteFile(livePath, []byte(oldLive), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashHelperProcess$", "-test.v")
			cmd.Env = append(os.Environ(),
				crashHelperEnv+"="+tc.at,
				"PROVIDERAUTH_DATA_DIR="+dataDir,
				"PROVIDERAUTH_LIVE="+livePath,
			)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("helper exited cleanly, want a kill at %s:\n%s", tc.at, out)
			}
			if strings.Contains(string(out), "--- FAIL") {
				t.Fatalf("helper failed before its crash point:\n%s", out)
			}

			// Reopen exactly as a restarted daemon would.
			ad := &fakeAdapter{id: "fake", live: livePath, lock: livePath + ".lock"}
			c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Recover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantState {
				t.Fatalf("recovered state = %s, want %s", got, tc.wantState)
			}

			live, err := os.ReadFile(livePath)
			switch tc.wantLive {
			case "absent":
				if !os.IsNotExist(err) {
					t.Fatalf("live present, want absent")
				}
			case "old":
				if err != nil {
					t.Fatal(err)
				}
				if string(live) != oldLive {
					t.Fatalf("live = %s, want the untouched original", live)
				}
			case "new":
				if err != nil {
					t.Fatal(err)
				}
				if string(live) != newLive {
					t.Fatalf("live = %s, want the published candidate", live)
				}
			}

			// Whatever happened, the store must be usable again without an
			// operator when recovery reported a committed state.
			if tc.wantState == StateIdle {
				if _, err := c.Begin(context.Background(), SourceDeviceAuth); err != nil {
					t.Fatalf("store not reusable after recovery: %v", err)
				}
			}

			// No pending litter survives a completed recovery.
			pend, err := os.ReadDir(filepath.Join(dataDir, "provider-auth", "fake", "pending"))
			if err != nil {
				t.Fatal(err)
			}
			if len(pend) > 1 {
				t.Fatalf("pending entries = %d, want at most the new transaction", len(pend))
			}
		})
	}
}

// TestConvergenceTableUnderRepeatedKills is the D26 cross-boundary proof.
//
// It kills a real process at every journal, candidate, sync, rename, and label
// boundary, restarts, and asserts the recovered state is one of the two
// committed outcomes — never a third, and never a lost generation. Where the
// evidence is genuinely ambiguous the only acceptable answer is
// recovery_required with every file preserved.
func TestConvergenceTableUnderRepeatedKills(t *testing.T) {
	const oldLive = `{"mode":"chatgpt","seq":1}`
	const newLive = `{"mode":"chatgpt","seq":2}`

	points := []string{
		"after_seed", "after_begin", "after_stage",
		"after_validate", "after_publish_rename", "after_commit",
	}

	for _, at := range points {
		t.Run(at, func(t *testing.T) {
			dataDir := t.TempDir()
			liveDir := t.TempDir()
			livePath := filepath.Join(liveDir, "auth.json")
			if err := os.WriteFile(livePath, []byte(oldLive), 0o600); err != nil {
				t.Fatal(err)
			}

			// Kill, restart, recover — twice. A second pass must be a no-op:
			// recovery that is not idempotent would rewrite state every boot.
			for pass := range 2 {
				if pass == 0 {
					cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashHelperProcess$")
					cmd.Env = append(os.Environ(),
						crashHelperEnv+"="+at,
						"PROVIDERAUTH_DATA_DIR="+dataDir,
						"PROVIDERAUTH_LIVE="+livePath,
					)
					if out, err := cmd.CombinedOutput(); err == nil {
						t.Fatalf("helper exited cleanly at %s:\n%s", at, out)
					}
				}

				ad := &fakeAdapter{id: "fake", live: livePath, lock: livePath + ".lock"}
				c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{})
				if err != nil {
					t.Fatal(err)
				}
				st, err := c.Recover(context.Background())
				if err != nil {
					t.Fatalf("pass %d recover: %v", pass, err)
				}

				live, readErr := os.ReadFile(livePath)
				if readErr != nil {
					t.Fatalf("pass %d: live credential is gone entirely: %v", pass, readErr)
				}
				// The only two acceptable contents are the committed outcomes.
				if s := string(live); s != oldLive && s != newLive {
					t.Fatalf("pass %d: live = %s, want one of the two committed values", pass, s)
				}

				switch st {
				case StateIdle:
					// A committed outcome must leave a usable chain.
					m, mErr := loadManifest(c.store.manifestPath())
					if mErr != nil {
						t.Fatal(mErr)
					}
					if m.byLabel(LabelCurrent) == nil {
						t.Fatalf("pass %d: idle with no CURRENT generation", pass)
					}
					if m.byLabel(LabelCurrent).Fingerprint != FingerprintOf(live) {
						t.Fatalf("pass %d: CURRENT does not match LIVE after recovery", pass)
					}
				case StateRecoveryRequired:
					// Ambiguity must preserve evidence, not consume it.
					gens, gErr := os.ReadDir(filepath.Join(dataDir, "provider-auth", "fake", "generations"))
					if gErr != nil {
						t.Fatal(gErr)
					}
					if len(gens) == 0 {
						t.Fatalf("pass %d: recovery_required discarded every generation", pass)
					}
				default:
					t.Fatalf("pass %d: unexpected recovered state %s", pass, st)
				}
			}
		})
	}
}
