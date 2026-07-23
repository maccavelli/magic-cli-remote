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
	if !srv.allowAccept(mk()) || !srv.allowAccept(mk()) || !srv.allowAccept(mk()) {
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
