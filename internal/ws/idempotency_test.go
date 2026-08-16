package ws

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestIdempotencyReplaySameFrame(t *testing.T) {
	l := newIdempotencyLedger()
	frame, wait, action := l.begin("dev", "req-1")
	if action != idemExecute || frame != nil || wait != nil {
		t.Fatalf("first begin: action=%v frame=%v wait=%v", action, frame, wait != nil)
	}
	payload := []byte(`{"v":1,"type":"ok","id":"req-1"}`)
	l.capture("dev", "req-1", payload)

	frame, wait, action = l.begin("dev", "req-1")
	if action != idemReplay {
		t.Fatalf("second begin action=%v, want replay", action)
	}
	if wait != nil {
		t.Fatal("replay must not wait")
	}
	if string(frame) != string(payload) {
		t.Fatalf("frame = %q, want %q", frame, payload)
	}
}

func TestIdempotencyWaitForInFlight(t *testing.T) {
	l := newIdempotencyLedger()
	_, _, action := l.begin("dev", "req-2")
	if action != idemExecute {
		t.Fatal("want execute")
	}

	var got []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, wait, action := l.begin("dev", "req-2")
		if action != idemWait || wait == nil {
			t.Errorf("want wait, got action=%v", action)
			return
		}
		got = wait(context.Background())
	}()

	time.Sleep(20 * time.Millisecond)
	payload := []byte(`{"type":"session.created","id":"req-2"}`)
	l.capture("dev", "req-2", payload)
	wg.Wait()
	if string(got) != string(payload) {
		t.Fatalf("waited frame = %q, want %q", got, payload)
	}
}

func TestIdempotencyFailedAllowsRetry(t *testing.T) {
	l := newIdempotencyLedger()
	_, _, _ = l.begin("dev", "req-3")
	l.fail("dev", "req-3")
	_, _, action := l.begin("dev", "req-3")
	if action != idemExecute {
		t.Fatalf("after fail, want execute, got %v", action)
	}
}

func TestIdempotencyCompleteNilStillReplaysSkip(t *testing.T) {
	l := newIdempotencyLedger()
	_, _, _ = l.begin("dev", "req-4")
	l.complete("dev", "req-4", nil)
	frame, _, action := l.begin("dev", "req-4")
	if action != idemReplay {
		t.Fatalf("want replay after nil complete, got %v", action)
	}
	if frame != nil {
		t.Fatalf("want nil frame, got %q", frame)
	}
}

// A waiter parked on an in-flight entry whose owner fails must not be left
// with nothing: the ledger drops the key so the RETRY may execute, and
// dispatchAsync must re-begin rather than return silently (MADR 0095 F5).
func TestIdempotencyFailReleasesWaiterForReexecute(t *testing.T) {
	l := newIdempotencyLedger()
	_, _, action := l.begin("dev", "req-fail")
	if action != idemExecute {
		t.Fatal("want execute")
	}
	var got []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, wait, a := l.begin("dev", "req-fail")
		if a != idemWait || wait == nil {
			t.Errorf("want wait, got %v", a)
			return
		}
		got = wait(context.Background())
	}()
	time.Sleep(20 * time.Millisecond)
	l.fail("dev", "req-fail")
	wg.Wait()
	if got != nil {
		t.Fatalf("waiter frame = %q, want nil", got)
	}
	// The contract dispatchAsync relies on: after the wait yields nothing,
	// a fresh begin must hand the caller the work rather than replay.
	if _, _, a := l.begin("dev", "req-fail"); a != idemExecute {
		t.Fatalf("after a failed original, want execute, got %v", a)
	}
}

// maxEntries must hold regardless of how many entries are in flight.
//
// purgeLocked samples ONE random entry per eviction pass and returns from
// the whole function if that entry happens to be unfinished, so the
// probability of an early bail scales with the in-flight fraction. With a
// single in-flight entry the overshoot is 1 and invisible; measured on the
// pre-fix code, 200 in-flight settled the ledger at 328 entries and 255
// in-flight at 518 — roughly 2x the cap, permanently (MADR 0095 F6).
//
// The ledger is shared across devices (keys are deviceID+requestID), so
// while maxAsyncPerClient caps one client at 8, enough concurrent clients
// reach the fractions above.
func TestIdempotencyPurgeEnforcesCapWithManyInFlight(t *testing.T) {
	l := newIdempotencyLedger()
	const inFlight = 200
	for i := 0; i < inFlight; i++ {
		l.begin("dev", fmt.Sprintf("inflight-%d", i))
	}
	worst := 0
	for i := 0; i < 2000; i++ {
		id := fmt.Sprintf("req-%d", i)
		l.begin("dev", id)
		l.complete("dev", id, []byte(`{"type":"ok"}`))
		l.mu.Lock()
		if n := len(l.entries); n > worst {
			worst = n
		}
		l.mu.Unlock()
	}
	l.mu.Lock()
	final := len(l.entries)
	inFlightKept := 0
	for _, e := range l.entries {
		if !e.finished {
			inFlightKept++
		}
	}
	l.mu.Unlock()
	// The cap cannot go below the in-flight count, which is never evicted.
	if final > l.maxEntries {
		t.Errorf("ledger settled at %d entries, want <= %d (worst seen %d)",
			final, l.maxEntries, worst)
	}
	if inFlightKept != inFlight {
		t.Errorf("in-flight entries kept = %d, want %d", inFlightKept, inFlight)
	}
}
