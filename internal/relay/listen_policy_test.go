package relay

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testCred(t *testing.T) HostCredential {
	t.Helper()
	c, err := ParseAllowFlag("h1:sixteen-chars-min-sec")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMaxHeaderBytesRejectsOversized(t *testing.T) {
	srv := New(Config{
		ListenAddr: "127.0.0.1:0",
		Allow:      []HostCredential{testCred(t)},
		Limits:     DefaultLimits(),
	}, nil)
	if srv.http.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("MaxHeaderBytes=%d want %d", srv.http.MaxHeaderBytes, maxHeaderBytes)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, ln) }()
	time.Sleep(20 * time.Millisecond)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	pad := strings.Repeat("a", 64<<10)
	if _, err := fmt.Fprintf(conn, "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\nX-Pad: %s\r\n\r\n", pad); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		// Server closed the connection on an oversized header.
		cancel()
		<-errCh
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 431 or 400", resp.StatusCode)
	}
	cancel()
	<-errCh
}

func TestListenAndServeRejectsPlaintextNonLoopback(t *testing.T) {
	srv := New(Config{
		ListenAddr: "0.0.0.0:18443",
		Allow:      []HostCredential{testCred(t)},
		Limits:     DefaultLimits(),
	}, nil)
	err := srv.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("want error for plaintext non-loopback")
	}
	if !strings.Contains(err.Error(), "plaintext") || !strings.Contains(err.Error(), "--allow-plaintext") {
		t.Fatalf("error not actionable: %v", err)
	}
}

func TestListenAndServeAllowsPlaintextLoopback(t *testing.T) {
	srv := New(Config{
		ListenAddr: "127.0.0.1:0",
		Allow:      []HostCredential{testCred(t)},
		Limits:     DefaultLimits(),
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("loopback plaintext should start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestListenAndServeAllowPlaintextOverride(t *testing.T) {
	srv := New(Config{
		ListenAddr:     "0.0.0.0:0",
		Allow:          []HostCredential{testCred(t)},
		Limits:         DefaultLimits(),
		AllowPlaintext: true,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("--allow-plaintext should start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestListenHostIsLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8443", "[::1]:8443", "localhost:1", "127.0.0.9:9"} {
		if !listenHostIsLoopback(addr) {
			t.Errorf("%s should be loopback", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8443", ":8443", "[::]:443", "192.168.1.1:80", "example.com:443"} {
		if listenHostIsLoopback(addr) {
			t.Errorf("%s should not be loopback", addr)
		}
	}
}

func TestStripHTTP2(t *testing.T) {
	cfg := &tls.Config{NextProtos: []string{"h2", "http/1.1", "acme-tls/1"}}
	stripHTTP2(cfg)
	for _, p := range cfg.NextProtos {
		if p == "h2" || p == "h2c" {
			t.Fatalf("h2 still present: %v", cfg.NextProtos)
		}
	}
	if len(cfg.NextProtos) != 2 {
		t.Fatalf("NextProtos=%v", cfg.NextProtos)
	}
	stripHTTP2(nil)
	stripHTTP2(&tls.Config{})
}

func TestListenAndServeStripsH2FromManagedTLS(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeTestCert(t, dir)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
	}
	srv := New(Config{
		ListenAddr: "127.0.0.1:0",
		TLSConfig:  tlsCfg,
		Allow:      []HostCredential{testCred(t)},
		Limits:     DefaultLimits(),
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-errCh
	if got := strings.Join(tlsCfg.NextProtos, ","); got != "h2,http/1.1" {
		t.Fatalf("caller TLSConfig mutated: %v", tlsCfg.NextProtos)
	}
}
