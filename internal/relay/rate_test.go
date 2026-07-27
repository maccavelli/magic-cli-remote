package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
	a1, a2, a3 := srv.allowAccept(mk()), srv.allowAccept(mk()), srv.allowAccept(mk())
	if !a1 || !a2 || !a3 {
		t.Fatal("first three should pass")
	}
	if srv.allowAccept(mk()) {
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
	if !srv.allowAccept(mk()) {
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
	for i := 0; i < rateMapMax+50; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0." + itoa((i/256)%256) + "." + itoa(i%256) + ":1"
		srv.allowAccept(r)
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
	a1, a2 := srv.allowAccept(mk("203.0.113.1")), srv.allowAccept(mk("203.0.113.1"))
	if !a1 || !a2 {
		t.Fatal("fill window")
	}
	if srv.allowAccept(mk("203.0.113.1")) {
		t.Fatal("should limit")
	}
	// Expire only this key's window without calling pruneRateLocked.
	srv.rateMu.Lock()
	for _, w := range srv.rate {
		w.start = time.Now().Add(-time.Minute - time.Second)
	}
	// Leave a second stale key that would only drop on full prune.
	srv.rate["accept\x00stale-other"] = &rateWindow{start: time.Now().Add(-rateWindowTTL - time.Second), count: 1}
	nBefore := len(srv.rate)
	srv.rateMu.Unlock()
	if nBefore < 2 {
		t.Fatalf("expected stale extra entry, got %d", nBefore)
	}
	// Hot path allows again for same IP (own window expired) without full prune.
	if !srv.allowAccept(mk("203.0.113.1")) {
		t.Fatal("own window should reset without full map prune")
	}
	srv.rateMu.Lock()
	// Stale-other may still be present until prune.
	_, still := srv.rate["accept\x00stale-other"]
	srv.pruneRateLocked(time.Now())
	_, after := srv.rate["accept\x00stale-other"]
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
