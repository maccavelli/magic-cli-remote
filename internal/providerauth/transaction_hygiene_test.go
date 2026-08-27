package providerauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// TestNoSecretReachesDisk walks everything the coordinator persists and proves
// a sentinel token value never appears outside the immutable generation
// payloads, which are the one place it is meant to live (MADR 0074 D23/D29).
func TestNoSecretReachesDisk(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	const sentinel = "SENTINELtokenVALUE0123456789"
	c, ad, dataDir := newFixture(t)
	if err := os.WriteFile(ad.live, []byte(`{"mode":"chatgpt","seq":1,"tok":"`+sentinel+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	cand := `{"mode":"chatgpt","seq":2,"tok":"` + sentinel + `"}`
	if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), []byte(cand), 0o600); err != nil {
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

	genDir := filepath.Join(dataDir, "provider-auth", ad.id, "generations")
	root := filepath.Join(dataDir, "provider-auth")
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Dir(path) == genDir {
			return nil // payloads legitimately hold the credential
		}
		b, readErr := os.ReadFile(path) //nolint:gosec // test walk of a temp dir
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(b), sentinel) {
			t.Errorf("%s contains the credential sentinel", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestErrorsNeverCarrySecrets proves failure text names the provider and the
// condition, never bytes, fingerprints, or credential paths (D29/P17 step 10).
func TestErrorsNeverCarrySecrets(t *testing.T) {
	const sentinel = "SENTINELtokenVALUE0123456789"
	c, ad, _ := newFixture(t)
	if err := os.WriteFile(ad.live, []byte(`{"mode":"chatgpt","seq":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	// A candidate the provider rejects, carrying a secret in its bytes.
	if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()),
		[]byte(`{"tok":"`+sentinel+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stageErr := c.StageCandidate(ctx, txn)
	if !errors.Is(stageErr, ErrInvalidCandidate) {
		t.Fatalf("err = %v, want ErrInvalidCandidate", stageErr)
	}
	msg := stageErr.Error()
	if strings.Contains(msg, sentinel) {
		t.Fatalf("error text leaked the credential: %s", msg)
	}
	if strings.Contains(msg, ad.live) {
		t.Fatalf("error text leaked the live credential path: %s", msg)
	}
}

// TestFingerprintShortIsNotAConfirmationOracle proves the logging projection is
// truncated and that absence stays a distinct value (D29).
func TestFingerprintShortIsNotAConfirmationOracle(t *testing.T) {
	full := FingerprintOf([]byte("something"))
	if got := full.Short(); len(got) != 8 || !strings.HasPrefix(string(full), got) {
		t.Fatalf("Short() = %q, want an 8-char prefix", got)
	}
	if FingerprintAbsent.Short() != "absent" {
		t.Fatalf("absent must stay distinguishable in logs")
	}
	if FingerprintOf(nil) == FingerprintAbsent {
		t.Fatal("the hash of empty bytes must not equal absence")
	}
	if !full.Valid() || FingerprintAbsent.Valid() != true {
		t.Fatal("valid fingerprints must validate")
	}
	if Fingerprint("nothex").Valid() {
		t.Fatal("a malformed fingerprint must not validate")
	}
}

// TestLockContentionIsBounded proves a second coordinator against the same
// store waits for the lock rather than corrupting state, and gives up within
// its configured bound (D21 lock discipline).
func TestLockContentionIsBounded(t *testing.T) {
	dataDir := t.TempDir()
	liveDir := t.TempDir()
	live := filepath.Join(liveDir, "auth.json")
	if err := os.WriteFile(live, []byte(`{"mode":"chatgpt","seq":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mk := func(timeout time.Duration) *Coordinator {
		ad := &fakeAdapter{id: "fake", live: live, lock: live + ".lock"}
		c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{LockTimeout: timeout})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ctx := context.Background()
	if err := mk(time.Second).Seed(ctx); err != nil {
		t.Fatal(err)
	}

	// Hold the on-disk provider lock from another "process" view, then prove a
	// short-timeout coordinator reports a bounded failure instead of hanging.
	held := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		other := mk(5 * time.Second)
		_ = other.withProviderLock(ctx, func(*Manifest) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	start := time.Now()
	_, err := mk(200*time.Millisecond).Begin(ctx, SourceDeviceAuth)
	elapsed := time.Since(start)
	close(release)
	wg.Wait()

	if err == nil {
		t.Fatal("Begin succeeded while the provider lock was held")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Begin waited %s, want a bounded failure", elapsed)
	}
}

// TestConcurrentBeginAdmitsOne proves only one transaction is admitted when
// several goroutines race, and the rest get a typed busy error.
func TestConcurrentBeginAdmitsOne(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok int
	var busy int
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Begin(ctx, SourceDeviceAuth)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrTransactionBusy):
				busy++
			}
		}()
	}
	wg.Wait()

	if ok != 1 {
		t.Fatalf("admitted %d transactions, want exactly 1", ok)
	}
	if ok+busy != n {
		t.Fatalf("unclassified outcomes: ok=%d busy=%d of %d", ok, busy, n)
	}
}

// TestContextCancellationIsRespected proves coordinator entry points honour a
// cancelled context rather than proceeding with a mutation.
func TestContextCancellationIsRespected(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Begin(ctx, SourceDeviceAuth); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestUnknownSourceRejected proves the journal cannot record an unclassified
// mutation origin.
func TestUnknownSourceRejected(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	if _, err := c.Begin(context.Background(), Source("mystery")); err == nil {
		t.Fatal("Begin accepted an unknown source")
	}
}
