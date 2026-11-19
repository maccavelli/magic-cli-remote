package relayhost

import (
	"net/http"
	"sync"
	"testing"
	"time"
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

type notATransport struct{}

func (notATransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

// http.DefaultTransport replaced by a RoundTripper that is not a *http.Transport
// is a copy with the standard library's defaults, not a panic.
func TestCloneTransportFallsBackToDefaultsForAForeignRoundTripper(t *testing.T) {
	tr := cloneTransport(notATransport{})
	if tr == nil || tr.Proxy == nil || !tr.ForceAttemptHTTP2 || tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("cloneTransport(foreign) = %+v, want the standard library's default settings", tr)
	}
}

func TestCloneTransportCopiesATransport(t *testing.T) {
	base := &http.Transport{MaxIdleConns: 7}
	tr := cloneTransport(base)
	if tr == base || tr.MaxIdleConns != 7 {
		t.Fatalf("cloneTransport did not return a copy carrying the base's settings: %+v", tr)
	}
}
