package relay

import (
	"sync"
	"testing"
)

// A pool whose New returns the wrong type: poolBuf must answer with a fresh
// buffer of the requested size, not panic.
func TestPoolBufFallsBackToAFreshBufferForAForeignValue(t *testing.T) {
	pool := &sync.Pool{New: func() any { return "not a *[]byte" }}
	b := poolBuf(pool, 128)
	if b == nil || len(*b) != 128 {
		t.Fatalf("poolBuf returned %v, want a pointer to a 128-byte slice", b)
	}
}

func TestPoolBufReturnsThePoolsOwnBuffer(t *testing.T) {
	want := make([]byte, 16)
	pool := &sync.Pool{New: func() any { return &want }}
	if got := poolBuf(pool, 128); got != &want {
		t.Fatalf("poolBuf returned a different buffer than the pool's own")
	}
}
