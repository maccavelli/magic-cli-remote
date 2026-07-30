package ws

import (
	"context"
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
