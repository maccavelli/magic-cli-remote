package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// 0068 P6: refusals carry a usable retry hint.

func TestAllowRateRetryWindowRemainder(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow:  []HostCredential{cred},
		Limits: Limits{JoinPerMinute: 1},
	}, nil)

	if ok, _ := srv.allowRateRetry("ip", rateBucketJoin, 1); !ok {
		t.Fatal("first attempt should pass")
	}
	ok, retry := srv.allowRateRetry("ip", rateBucketJoin, 1)
	if ok {
		t.Fatal("second attempt should be limited")
	}
	// The window just opened, so the remainder is essentially the whole
	// minute — and never more than it.
	if retry <= 55*time.Second || retry > time.Minute {
		t.Fatalf("retry remainder = %v, want ≈1m", retry)
	}

	// Age the window: the remainder must shrink to match.
	srv.rateMu.Lock()
	for _, w := range srv.rate {
		w.start = time.Now().Add(-45 * time.Second)
	}
	srv.rateMu.Unlock()
	if _, retry = srv.allowRateRetry("ip", rateBucketJoin, 1); retry > 15*time.Second {
		t.Fatalf("aged-window remainder = %v, want ≤15s", retry)
	}
}

func TestUpgradeRateLimitSetsRetryAfterHeader(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow:  []HostCredential{cred},
		Limits: Limits{AcceptPerMinute: 1},
	}, nil)
	mk := func() (*httptest.ResponseRecorder, *http.Request) {
		r := httptest.NewRequest(http.MethodGet, "/v1/phone", nil)
		r.RemoteAddr = "203.0.113.9:12345"
		return httptest.NewRecorder(), r
	}
	w, r := mk()
	_, _ = srv.upgrade(w, r) // consumes the window (fails upgrade, fine)
	w, r = mk()
	if _, err := srv.upgrade(w, r); err == nil {
		t.Fatal("second upgrade should be rate-limited")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	secs, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("Retry-After header not an integer: %q", w.Header().Get("Retry-After"))
	}
	if secs < 1 || secs > 60 {
		t.Fatalf("Retry-After = %ds, want within (0, 60]", secs)
	}
}
