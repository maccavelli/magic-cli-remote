package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// acceptOK is the former Server.allowAccept, kept as a test-only shorthand
// after 0115 P1 removed the unused production wrapper: upgrade() calls
// allowRateRetry directly.
func acceptOK(s *Server, r *http.Request) bool {
	ok, _ := s.allowRateRetry(s.clientIP(r), rateBucketAccept, s.cfg.Limits.AcceptPerMinute)
	return ok
}

func TestAllowAcceptRateLimitAndPrune(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow:  []HostCredential{cred},
		Limits: Limits{AcceptPerMinute: 3},
	}, nil)
	// Same RemoteAddr → same bucket.
	mk := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/phone", nil)
		r.RemoteAddr = "203.0.113.9:12345"
		return r
	}
	a1, a2, a3 := acceptOK(srv, mk()), acceptOK(srv, mk()), acceptOK(srv, mk())
	if !a1 || !a2 || !a3 {
		t.Fatal("first three should pass")
	}
	if acceptOK(srv, mk()) {
		t.Fatal("fourth should rate-limit")
	}
	// Force window expired + prune.
	srv.rateMu.Lock()
	for _, w := range srv.rate {
		w.start = time.Now().Add(-rateWindowTTL - time.Second)
	}
	srv.pruneRateLocked(time.Now())
	if len(srv.rate) != 0 {
		t.Fatalf("prune left %d entries", len(srv.rate))
	}
	srv.rateMu.Unlock()
	if !acceptOK(srv, mk()) {
		t.Fatal("after prune should allow again")
	}
}

func TestAllowAcceptRateMapCap(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow:  []HostCredential{cred},
		Limits: Limits{AcceptPerMinute: 1000},
	}, nil)
	// Fill past rateMapMax with unique IPs.
	for i := range rateMapMax + 50 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0." + itoa((i/256)%256) + "." + itoa(i%256) + ":1"
		acceptOK(srv, r)
	}
	srv.rateMu.Lock()
	n := len(srv.rate)
	srv.rateMu.Unlock()
	if n > rateMapMax {
		t.Fatalf("rate map size %d > max %d", n, rateMapMax)
	}
}

func TestAllowRateHotPathDoesNotRequireFullPrune(t *testing.T) {
	// E3: stale entries can remain until background prune or hard cap;
	// the current key still expires by its own window start.
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow:  []HostCredential{cred},
		Limits: Limits{AcceptPerMinute: 2},
	}, nil)
	mk := func(ip string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = ip + ":1"
		return r
	}
	a1, a2 := acceptOK(srv, mk("203.0.113.1")), acceptOK(srv, mk("203.0.113.1"))
	if !a1 || !a2 {
		t.Fatal("fill window")
	}
	if acceptOK(srv, mk("203.0.113.1")) {
		t.Fatal("should limit")
	}
	// Expire only this key's window without calling pruneRateLocked.
	srv.rateMu.Lock()
	for _, w := range srv.rate {
		w.start = time.Now().Add(-time.Minute - time.Second)
	}
	// Leave a second stale key that would only drop on full prune.
	srv.rate[rateKey{bucket: "accept", id: "stale-other"}] = &rateWindow{start: time.Now().Add(-rateWindowTTL - time.Second), count: 1}
	nBefore := len(srv.rate)
	srv.rateMu.Unlock()
	if nBefore < 2 {
		t.Fatalf("expected stale extra entry, got %d", nBefore)
	}
	// Hot path allows again for same IP (own window expired) without full prune.
	if !acceptOK(srv, mk("203.0.113.1")) {
		t.Fatal("own window should reset without full map prune")
	}
	srv.rateMu.Lock()
	// Stale-other may still be present until prune.
	_, still := srv.rate[rateKey{bucket: "accept", id: "stale-other"}]
	srv.pruneRateLocked(time.Now())
	_, after := srv.rate[rateKey{bucket: "accept", id: "stale-other"}]
	srv.rateMu.Unlock()
	if !still {
		t.Fatal("expected stale-other to remain until explicit prune (E3)")
	}
	if after {
		t.Fatal("prune should drop stale-other")
	}
}

func TestAllowRateSeparateBuckets(t *testing.T) {
	// R16: join and accept buckets are independent.
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow: []HostCredential{cred},
		Limits: Limits{
			AcceptPerMinute: 100,
			JoinPerMinute:   2,
		},
	}, nil)
	ip := "198.51.100.7"
	j1, j2 := srv.allowRate(ip, rateBucketJoin, 2), srv.allowRate(ip, rateBucketJoin, 2)
	if !j1 || !j2 {
		t.Fatal("join first two")
	}
	if srv.allowRate(ip, rateBucketJoin, 2) {
		t.Fatal("join third should fail")
	}
	// Accept bucket still open for same IP.
	if !srv.allowRate(ip, rateBucketAccept, 100) {
		t.Fatal("accept should not share join counter")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestRateEvictionOldestFirst pins deterministic oldest-first eviction under
// capacity pressure (0115 P4, F4). The pre-fix code deleted whatever key map
// iteration yielded first, so no deterministic assertion could hold there:
// with 4096 windows the newest-and-hottest window survived only by luck. Now
// the single evicted key must be the unique oldest.
func TestRateEvictionOldestFirst(t *testing.T) {
	srv := New(Config{Limits: Limits{AcceptPerMinute: 100}}, nil)
	now := time.Now()
	// Fill to capacity with fresh (non-TTL-expired) windows whose starts are
	// staggered by 1ms so exactly one is oldest. All are inside the one-minute
	// window and inside the TTL, so the TTL prune removes none of them.
	oldest := rateKey{bucket: "accept", id: "victim-0"}
	for i := range rateMapMax {
		k := rateKey{bucket: "accept", id: fmt.Sprintf("victim-%d", i)}
		srv.rate[k] = &rateWindow{start: now.Add(-30*time.Second + time.Duration(i)*time.Millisecond), count: 3}
	}
	if len(srv.rate) != rateMapMax {
		t.Fatalf("setup: %d windows, want %d", len(srv.rate), rateMapMax)
	}
	hot := rateKey{bucket: "accept", id: fmt.Sprintf("victim-%d", rateMapMax-1)}

	// A brand-new client forces one eviction.
	if ok := srv.allowRate("newcomer", "accept", 100); !ok {
		t.Fatal("newcomer should be admitted")
	}
	if _, gone := srv.rate[oldest]; gone {
		t.Fatal("the oldest window must be the one evicted")
	}
	if w := srv.rate[hot]; w == nil || w.count != 3 {
		t.Fatalf("hot window disturbed: %+v", w)
	}
	if len(srv.rate) != rateMapMax {
		t.Fatalf("map size %d, want %d (one out, one in)", len(srv.rate), rateMapMax)
	}
}
