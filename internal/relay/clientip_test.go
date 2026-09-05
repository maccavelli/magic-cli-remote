package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPDefaultIgnoresXFF(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}}, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.50:9999"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := srv.clientIP(r); got != "203.0.113.50" {
		t.Fatalf("got %q want RemoteAddr host (XFF ignored)", got)
	}
}

func TestClientIPTrustedProxyUsesXFF(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"10.0.0.0/8", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}, TrustedProxies: nets}, nil)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:443"
	// client, intermediate-proxy(trusted)
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.9.9.9")
	if got := srv.clientIP(r); got != "198.51.100.7" {
		t.Fatalf("got %q want original client", got)
	}
}

func TestClientIPUntrustedRemoteIgnoresXFF(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}, TrustedProxies: nets}, nil)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// Attacker not in trusted set tries to spoof XFF.
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := srv.clientIP(r); got != "203.0.113.9" {
		t.Fatalf("got %q; untrusted hop must not use XFF", got)
	}
}

func TestClientIPXRealIPFallback(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}, TrustedProxies: nets}, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:80"
	r.Header.Set("X-Real-IP", "198.51.100.20")
	if got := srv.clientIP(r); got != "198.51.100.20" {
		t.Fatalf("got %q", got)
	}
}

func TestRateIPKeyIPv4Mapped(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}}, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[::ffff:203.0.113.50]:9999"
	if got := srv.clientIP(r); got != "203.0.113.50" {
		t.Fatalf("mapped IPv4 got %q want 203.0.113.50", got)
	}
	r.RemoteAddr = "203.0.113.50:9999"
	if got := srv.clientIP(r); got != "203.0.113.50" {
		t.Fatalf("IPv4 got %q want 203.0.113.50", got)
	}
}

func TestRateIPKeyIPv6SameSlash64(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}}, nil)
	a := httptest.NewRequest(http.MethodGet, "/", nil)
	a.RemoteAddr = "[2001:db8:1::1]:1"
	b := httptest.NewRequest(http.MethodGet, "/", nil)
	b.RemoteAddr = "[2001:db8:1::2]:2"
	ka, kb := srv.clientIP(a), srv.clientIP(b)
	if ka != kb {
		t.Fatalf("same /64 keys %q vs %q", ka, kb)
	}
}

func TestRateIPKeyIPv6DifferentSlash64(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{Allow: []HostCredential{cred}}, nil)
	a := httptest.NewRequest(http.MethodGet, "/", nil)
	a.RemoteAddr = "[2001:db8:1::1]:1"
	b := httptest.NewRequest(http.MethodGet, "/", nil)
	b.RemoteAddr = "[2001:db8:2::1]:1"
	if srv.clientIP(a) == srv.clientIP(b) {
		t.Fatal("different /64 must not share a rate key")
	}
}

func TestParseTrustedProxiesBareAndCIDR(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "  ", "::1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 3 {
		t.Fatalf("len=%d", len(nets))
	}
	if _, err := ParseTrustedProxies([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAllowAcceptUsesForwardedClientWhenTrusted(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	srv := New(Config{
		Allow:          []HostCredential{cred},
		TrustedProxies: nets,
		Limits:         Limits{AcceptPerMinute: 2},
	}, nil)
	mk := func(xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:1"
		r.Header.Set("X-Forwarded-For", xff)
		return r
	}
	a1, a2 := acceptOK(srv, mk("198.51.100.1")), acceptOK(srv, mk("198.51.100.1"))
	if !a1 || !a2 {
		t.Fatal("same client should use two slots")
	}
	if acceptOK(srv, mk("198.51.100.1")) {
		t.Fatal("third from same XFF client should limit")
	}
	// Different client through same proxy still allowed.
	if !acceptOK(srv, mk("198.51.100.2")) {
		t.Fatal("different XFF client should have own bucket")
	}
}
