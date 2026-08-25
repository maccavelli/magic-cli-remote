package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type transportFixture struct {
	client transport
	stop   func()
}

func TestTransportConformance(t *testing.T) {
	factories := map[string]func(*testing.T) transportFixture{
		"stdio": stdioTransportFixture,
		"tcp_ws": func(t *testing.T) transportFixture {
			return websocketTransportFixture(t, "tcp")
		},
		"unix_ws": func(t *testing.T) transportFixture {
			return websocketTransportFixture(t, "unix")
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			fixture := factory(t)
			defer fixture.stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			for _, frame := range [][]byte{
				[]byte(`{"id":1,"method":"thread/read"}`),
				[]byte(`{"id":"server-id","method":"item/tool/requestUserInput"}`),
				[]byte(`{"method":"warning","params":{"message":"hi"}}`),
				[]byte(`not-json`),
			} {
				if err := fixture.client.Send(ctx, frame); err != nil {
					t.Fatalf("send: %v", err)
				}
				got, err := fixture.client.Read(ctx)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if string(got) != string(frame) {
					t.Fatalf("round trip = %q, want %q", got, frame)
				}
			}

			var wg sync.WaitGroup
			readErr := make(chan error, 1)
			go func() {
				for i := 0; i < 8; i++ {
					if _, err := fixture.client.Read(ctx); err != nil {
						readErr <- err
						return
					}
				}
				readErr <- nil
			}()
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = fixture.client.Send(ctx, []byte(`{"method":"ping"}`))
				}()
			}
			wg.Wait()
			if err := <-readErr; err != nil {
				t.Fatalf("concurrent read: %v", err)
			}

			if err := fixture.client.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if err := fixture.client.Send(ctx, []byte(`{}`)); err == nil {
				t.Fatal("send after close succeeded")
			}
		})
		t.Run(name+"_cancellation", func(t *testing.T) {
			fixture := factory(t)
			defer fixture.stop()
			cn := newTransportConn(fixture.client, testLogger(t))
			go cn.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := cn.sendRequest(ctx, "thread/read", map[string]any{"threadId": "cancel"})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled request error = %v", err)
			}
			_ = fixture.client.Close()
		})
	}
}

func stdioTransportFixture(t *testing.T) transportFixture {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		reader := bufio.NewReader(server)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			_, _ = server.Write(line)
		}
	}()
	return transportFixture{
		client: newJSONLTransport(client, client),
		stop:   func() { _ = server.Close() },
	}
}

func websocketTransportFixture(t *testing.T, network string) transportFixture {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			typ, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), typ, data); err != nil {
				return
			}
		}
	})
	if network == "tcp" {
		server := httptest.NewServer(handler)
		url := "ws" + strings.TrimPrefix(server.URL, "http")
		tr, err := dialWebSocketTransport(context.Background(), url, nil)
		if err != nil {
			t.Fatal(err)
		}
		return transportFixture{client: tr, stop: server.Close}
	}
	socket := shortUnixSocketPath(t, "codex.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	tr, err := dialUnixWebSocketTransport(context.Background(), socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	return transportFixture{client: tr, stop: func() {
		_ = server.Close()
		_ = listener.Close()
	}}
}

// maxUnixSocketPathLen is the usable sockaddr_un.sun_path budget. Darwin and
// the BSDs allow 104 bytes including the NUL terminator; Linux allows 108. The
// smaller figure is used so the helper behaves the same on every platform.
const maxUnixSocketPathLen = 103

// shortUnixSocketPath returns a private directory plus socket name whose full
// path fits sun_path. t.TempDir() is unusable here because it embeds the
// subtest name: on macOS TMPDIR is already ~49 bytes, so the "unix_ws" subtest
// produced a 104-byte path and bind(2) rejected it with EINVAL. Trying /tmp
// second keeps the test working on hosts with an unusually long TMPDIR.
func shortUnixSocketPath(t *testing.T, name string) string {
	t.Helper()
	for _, base := range []string{"", "/tmp"} {
		dir, err := os.MkdirTemp(base, "cdx")
		if err != nil {
			continue
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			_ = os.RemoveAll(dir)
			t.Fatalf("chmod socket dir: %v", err)
		}
		socket := filepath.Join(dir, name)
		if len(socket) <= maxUnixSocketPathLen {
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			return socket
		}
		_ = os.RemoveAll(dir)
	}
	t.Fatalf("no temp dir yields a socket path within %d bytes", maxUnixSocketPathLen)
	return ""
}

func TestTransportFrameBoundsAndEOF(t *testing.T) {
	reader, writer := io.Pipe()
	transport := newJSONLTransport(io.Discard, reader)
	go func() {
		_, _ = writer.Write(append(make([]byte, maxTransportFrameBytes+1), '\n'))
		_ = writer.Close()
	}()
	if _, err := transport.Read(context.Background()); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err := transport.Read(context.Background()); err == nil {
		t.Fatal("EOF not reported")
	}
}
