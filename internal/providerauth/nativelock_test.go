package providerauth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// TestCommitBlocksOnTheProviderNativeLockFile proves a publication serializes
// against the file the provider's own writer locks, not merely against some
// file (MADR 0074 D25, MADR 0133).
//
// The two cases exist together on purpose. Every pre-0133 assertion compared
// the STRING NativeLockPath returns, which cannot tell auth.json.lock from
// auth.json.lock.lock — and the daemon flocked the latter while grok's refresh
// held the former. The "pre-0133" case below reproduces that: the same external
// writer, the same Commit, and no contention at all. It is what makes the first
// case evidence rather than decoration.
func TestCommitBlocksOnTheProviderNativeLockFile(t *testing.T) {
	cases := []struct {
		name string
		// lockFor maps the live path to what NativeLockPath returns.
		lockFor func(live string) string
		// blocked is whether Commit must be stopped by the external writer.
		blocked bool
	}{
		{
			// The corrected convention: return the base path and let
			// fsutil.WithLock derive auth.json.lock, the file grok honors.
			name:    "base path derives the provider's lock file",
			lockFor: func(live string) string { return live },
			blocked: true,
		},
		{
			// The defect, pinned so it cannot come back unnoticed: a path that
			// already ends in .lock makes WithLock flock auth.json.lock.lock,
			// which no provider takes, so nothing serializes.
			name:    "pre-0133 doubled suffix locks a file nobody else takes",
			lockFor: func(live string) string { return live + ".lock" },
			blocked: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			liveDir := t.TempDir()
			live := filepath.Join(liveDir, "auth.json")
			const before = `{"mode":"chatgpt","seq":1}`
			if err := os.WriteFile(live, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			ad := &fakeAdapter{id: "fake", live: live, lock: tc.lockFor(live)}
			c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{
				LockTimeout: 200 * time.Millisecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := c.Seed(ctx); err != nil {
				t.Fatal(err)
			}
			txn := stage(t, c, ad, "chatgpt", 2)

			// The external writer holds <liveDir>/auth.json.lock, which is what
			// a provider refresh does. WithLock derives that name from the base
			// path, so naming the base path here names that file.
			held := make(chan struct{})
			release := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = fsutil.WithLock(live, 5*time.Second, func() error {
					close(held)
					<-release
					return nil
				})
			}()
			<-held

			commitErr := c.Commit(ctx, txn)
			close(release)
			wg.Wait()

			if tc.blocked {
				if commitErr == nil {
					t.Fatalf("Commit succeeded while %s.lock was held: it locked some other file", live)
				}
				if !strings.Contains(commitErr.Error(), "flock") {
					t.Fatalf("Commit failed for the wrong reason: %v", commitErr)
				}
				if got, err := os.ReadFile(live); err != nil {
					t.Fatal(err)
				} else if string(got) != before {
					t.Errorf("LIVE was modified without the native lock: %s", got)
				}
				if _, err := os.Stat(live + ".lock.lock"); err == nil {
					t.Errorf("a doubled lock file %s.lock.lock exists: the suffix was applied twice", live)
				}
				return
			}

			if commitErr != nil {
				t.Fatalf("the doubled-suffix case is supposed to sail past the "+
					"external writer; it failed instead: %v", commitErr)
			}
			if _, err := os.Stat(live + ".lock.lock"); err != nil {
				t.Errorf("expected the doubled lock file %s.lock.lock: %v", live, err)
			}
		})
	}
}
