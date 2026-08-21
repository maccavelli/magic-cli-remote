package providerauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLoginScript writes a shell script that prints a device code, optionally
// writes a credential into the home named by FAKE_HOME, then exits.
func fakeLoginScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakelogin.sh")
	script := "#!/bin/sh\n" +
		"echo 'To sign in, visit https://example.test/device and enter CODE-1234'\n" +
		body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func ownedFixture(t *testing.T) (*Coordinator, *fakeAdapter) {
	t.Helper()
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	if err := c.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c, ad
}

// TestOwnedFlowPublishesValidatedCandidate is the happy path: the child writes
// into its isolated home, the candidate is staged, probed, and only then
// published.
func TestOwnedFlowPublishesValidatedCandidate(t *testing.T) {
	c, ad := ownedFixture(t)
	bin := fakeLoginScript(t, `printf '{"mode":"chatgpt","seq":7}' > "$FAKE_HOME/auth.json"; chmod 600 "$FAKE_HOME/auth.json"`)

	h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator: c,
		Bin:         bin,
		ScanTimeout: 10 * time.Second,
		EnvFor:      func(home string) []string { return []string{"FAKE_HOME=" + home} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Flow().UserCode == "" {
		t.Fatal("no user code was parsed")
	}
	if err := h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	live, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `"seq":7`) {
		t.Fatalf("live = %s, want the published candidate", live)
	}
	if ad.probes == 0 {
		t.Fatal("candidate was published without a provider probe")
	}
}

// TestOwnedFlowStartsWithAnEmptyHome is the F14 gate at the flow level: the
// child must find nothing to revoke.
func TestOwnedFlowStartsWithAnEmptyHome(t *testing.T) {
	c, _ := ownedFixture(t)
	// The script fails if it sees any pre-existing credential.
	bin := fakeLoginScript(t, `[ -e "$FAKE_HOME/auth.json" ] && exit 3; printf '{"mode":"chatgpt","seq":2}' > "$FAKE_HOME/auth.json"; chmod 600 "$FAKE_HOME/auth.json"`)

	h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator: c, Bin: bin, ScanTimeout: 10 * time.Second,
		EnvFor: func(home string) []string { return []string{"FAKE_HOME=" + home} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(context.Background()); err != nil {
		t.Fatalf("child saw a seeded credential in its pending home: %v", err)
	}
}

// TestOwnedFlowIncompleteOutcomesLeaveLiveIdentical covers cancel, failure, and
// a clean exit that produced nothing. Each must leave LIVE byte-identical.
func TestOwnedFlowIncompleteOutcomesLeaveLiveIdentical(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		cancel bool
	}{
		{"child fails", "exit 4", false},
		{"child writes nothing", "exit 0", false},
		{"child writes junk", `printf 'not json' > "$FAKE_HOME/auth.json"; chmod 600 "$FAKE_HOME/auth.json"`, false},
		{"cancelled", "sleep 120", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ad := ownedFixture(t)
			before, err := os.ReadFile(ad.live)
			if err != nil {
				t.Fatal(err)
			}
			bin := fakeLoginScript(t, tc.body)

			h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
				Coordinator: c, Bin: bin, ScanTimeout: 10 * time.Second,
				EnvFor: func(home string) []string { return []string{"FAKE_HOME=" + home} },
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.cancel {
				h.Cancel()
			}
			if err := h.Wait(context.Background()); err == nil {
				t.Fatal("incomplete flow reported success")
			}

			after, err := os.ReadFile(ad.live)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("an incomplete flow changed LIVE")
			}
			// The transaction slot must be free again.
			if _, err := c.Begin(context.Background(), SourceDeviceAuth); err != nil {
				t.Fatalf("transaction was not released: %v", err)
			}
		})
	}
}

// TestOwnedFlowCancelIsIdempotentAndOrderFree proves Cancel and Wait share one
// result whichever order they are called in (MADR 0074 D27).
func TestOwnedFlowCancelIsIdempotentAndOrderFree(t *testing.T) {
	c, _ := ownedFixture(t)
	bin := fakeLoginScript(t, "sleep 120")
	h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator: c, Bin: bin, ScanTimeout: 10 * time.Second,
		EnvFor: func(home string) []string { return []string{"FAKE_HOME=" + home} },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.Cancel()
	h.Cancel()

	first := h.Wait(context.Background())
	second := h.Wait(context.Background())
	if !errors.Is(first, ErrFlowCancelled) || !errors.Is(second, ErrFlowCancelled) {
		t.Fatalf("results = %v / %v, want ErrFlowCancelled from both", first, second)
	}
}

// TestOwnedFlowDefersActivationWhileBusy proves a validated candidate waits for
// an idle provider, reports ready_to_activate rather than failure, and then
// publishes without repeating OAuth (MADR 0074 D25/D28, P18 step 8).
func TestOwnedFlowDefersActivationWhileBusy(t *testing.T) {
	c, ad := ownedFixture(t)
	bin := fakeLoginScript(t, `printf '{"mode":"chatgpt","seq":3}' > "$FAKE_HOME/auth.json"; chmod 600 "$FAKE_HOME/auth.json"`)

	var busy atomic.Int64
	busy.Store(1)

	h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator: c, Bin: bin, ScanTimeout: 10 * time.Second,
		EnvFor:       func(home string) []string { return []string{"FAKE_HOME=" + home} },
		Busy:         func() int { return int(busy.Load()) },
		ActivateWait: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- h.Wait(context.Background()) }()

	// The flow must announce that it is waiting rather than failing.
	select {
	case st := <-h.Updates():
		if st != provDeviceAuthReadyToActivate {
			t.Fatalf("update = %q, want ready_to_activate", st)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no ready_to_activate update while the provider was busy")
	}

	select {
	case err := <-done:
		t.Fatalf("flow finished while busy: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	busy.Store(0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("flow never activated after the provider went idle")
	}

	live, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `"seq":3`) {
		t.Fatalf("live = %s, want the deferred candidate", live)
	}
}

// TestOwnedFlowActivationDeadlineAborts proves a deferred candidate does not
// wait forever and leaves LIVE untouched when it expires.
func TestOwnedFlowActivationDeadlineAborts(t *testing.T) {
	c, ad := ownedFixture(t)
	before, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	bin := fakeLoginScript(t, `printf '{"mode":"chatgpt","seq":4}' > "$FAKE_HOME/auth.json"; chmod 600 "$FAKE_HOME/auth.json"`)

	h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator: c, Bin: bin, ScanTimeout: 10 * time.Second,
		EnvFor:            func(home string) []string { return []string{"FAKE_HOME=" + home} },
		Busy:              func() int { return 1 }, // never goes idle
		ActivateWait:      10 * time.Millisecond,
		ActivationTimeout: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(context.Background()); err == nil {
		t.Fatal("expired activation reported success")
	}
	after, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("an expired activation changed LIVE")
	}
}

// TestOwnedFlowNoSecretsInErrors proves flow failures never echo credential
// bytes the child produced.
func TestOwnedFlowNoSecretsInErrors(t *testing.T) {
	const sentinel = "SENTINELtokenVALUE0123456789"
	c, _ := ownedFixture(t)
	bin := fakeLoginScript(t, `printf '{"tok":"`+sentinel+`"}' > "$FAKE_HOME/auth.json"; chmod 600 "$FAKE_HOME/auth.json"`)

	h, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator: c, Bin: bin, ScanTimeout: 10 * time.Second,
		EnvFor: func(home string) []string { return []string{"FAKE_HOME=" + home} },
	})
	if err != nil {
		t.Fatal(err)
	}
	waitErr := h.Wait(context.Background())
	if waitErr == nil {
		t.Fatal("an invalid candidate was accepted")
	}
	if strings.Contains(waitErr.Error(), sentinel) {
		t.Fatalf("error text leaked the credential: %v", waitErr)
	}
}

// provDeviceAuthReadyToActivate mirrors provider.DeviceAuthReadyToActivate
// without importing the provider package, which would be an import cycle.
const provDeviceAuthReadyToActivate = "ready_to_activate"
