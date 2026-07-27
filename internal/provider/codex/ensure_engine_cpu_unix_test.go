//go:build unix

package codex

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// processCPU is the CPU time this test binary has consumed so far.
func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Skipf("getrusage unavailable: %v", err)
	}
	user := time.Duration(ru.Utime.Sec)*time.Second + time.Duration(ru.Utime.Usec)*time.Microsecond
	sys := time.Duration(ru.Stime.Sec)*time.Second + time.Duration(ru.Stime.Usec)*time.Microsecond
	return user + sys
}

// Waiting on someone else's in-flight start must cost no CPU. This is the
// regression that pinned two cores on a 2-core host and — because each waiter
// also holds a ws async slot — made every operation on the connection answer
// rate_limited.
func TestEnsureEngineWaiterDoesNotBurnCPU(t *testing.T) {
	p := New(Config{Bin: "codex"})
	p.mu.Lock()
	p.starting = true
	p.mu.Unlock()

	const wait = 500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	before := processCPU(t)
	started := time.Now()
	if _, err := p.ensureEngine(ctx); err == nil {
		t.Fatal("ensure engine returned without an engine")
	}
	used := processCPU(t) - before
	elapsed := time.Since(started)

	// A parked waiter costs microseconds; a spin costs a full core-second per
	// second waited. Anything past a quarter of the wall clock is the spin.
	if used > elapsed/4 {
		t.Fatalf("waiter burned %s of CPU while parked for %s (busy-wait regression)", used, elapsed)
	}
}
