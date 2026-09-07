package relay

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLimitedConnCloseOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	limited := limitListener(ln, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
		_ = c.Close()
	}()
	c, err := limited.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	_ = c.Close() // second close must not double-release the semaphore
	<-done
}

func TestLimitListenerNoCap(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if got := limitListener(ln, 0); got != ln {
		t.Fatal("n<=0 must return the original listener")
	}
}

func startPlainListenAndServe(t *testing.T, srv *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	srv.cfg.ListenAddr = addr
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 20*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ListenAndServe did not become reachable")
	return ""
}

func TestHealthzIdleTimeout(t *testing.T) {
	prev := setHTTPIdleTimeout(50 * time.Millisecond)
	t.Cleanup(func() { setHTTPIdleTimeout(prev) })

	cred, err := ParseAllowFlag("h1:sixteen-chars-min-sec")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		ListenAddr:     "127.0.0.1:0",
		Allow:          []HostCredential{cred},
		Limits:         DefaultLimits(),
		AllowPlaintext: true,
	}, nil)
	addr := startPlainListenAndServe(t, srv)

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(conn, "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: keep-alive\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first status=%d", resp.StatusCode)
	}

	time.Sleep(150 * time.Millisecond)
	if _, err := fmt.Fprintf(conn, "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); err != nil {
		return // peer already closed the keep-alive; that is the idle timeout.
	}
	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet}); err == nil {
		t.Fatal("second request on idle keep-alive succeeded; want IdleTimeout close")
	}
}

func TestMaxConnsBlocks(t *testing.T) {
	cred, err := ParseAllowFlag("h1:sixteen-chars-min-sec")
	if err != nil {
		t.Fatal(err)
	}
	lim := DefaultLimits()
	lim.MaxConns = 1
	srv := New(Config{
		ListenAddr:     "127.0.0.1:0",
		Allow:          []HostCredential{cred},
		Limits:         lim,
		AllowPlaintext: true,
	}, nil)
	addr := startPlainListenAndServe(t, srv)

	hold, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	_ = hold.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(hold, "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: keep-alive\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(hold)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	c2, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return // kernel did not complete the handshake; accept is blocked.
	}
	defer c2.Close()
	// Accept is blocked, so the HTTP request cannot complete while `hold` is live.
	_ = c2.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := fmt.Fprintf(c2, "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); err != nil {
		return
	}
	br2 := bufio.NewReader(c2)
	if _, err := http.ReadResponse(br2, &http.Request{Method: http.MethodGet}); err == nil {
		t.Fatal("second HTTP request completed at MaxConns=1; want accept blocked")
	}
}
