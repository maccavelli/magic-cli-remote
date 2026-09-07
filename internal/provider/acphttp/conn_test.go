package acphttp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestDialWSReadLimitAdmitsLargeFrames pins the regression where the
// coder/websocket default read limit (32 KiB, read.go defaultReadLimit=32768)
// closed the engine connection as soon as a tool_call update carried a file
// read or command output past it — "engine lost" for every session. A frame
// the size of a realistic tool payload must survive.
func TestDialWSReadLimitAdmitsLargeFrames(t *testing.T) {
	upgraded := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		upgraded <- c
	}))
	t.Cleanup(srv.Close)

	conn := newACPConn(srv.URL, Config{}, nil)
	conn.connID = "test-conn"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := conn.dialWS(ctx)
	if err != nil {
		t.Fatalf("dialWS: %v", err)
	}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "test done") })

	server := <-upgraded
	// 1 MiB: far past the 32 KiB default, far under the 8 MiB limit.
	payload := strings.Repeat("tool-output-", 1<<20/12)
	// Concurrent with the read: a sequential 1 MiB write silently depends
	// on the frame fitting in loopback socket buffers, which fails on a
	// loaded host. The regression under test is the READ limit, not buffer
	// sizes.
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- server.Write(ctx, websocket.MessageText, []byte(payload))
	}()

	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read %d-byte frame: %v (read limit not raised?)", len(payload), err)
	}
	if len(data) != len(payload) {
		t.Fatalf("frame truncated: want %d bytes, got %d", len(payload), len(data))
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("server write: %v", err)
	}
}

func TestSendNotificationOmitsJSONRPCID(t *testing.T) {
	upgraded := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		upgraded <- c
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := newACPConn(srv.URL, Config{}, nil)
	conn.connID = "test-conn"
	ws, err := conn.dialWS(ctx)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "test done") })

	fr := newWSFramer(ws, slog.Default())
	if err := fr.sendNotification(ctx, "session/cancel", map[string]any{"sessionId": "s1"}); err != nil {
		t.Fatalf("send notification: %v", err)
	}

	server := <-upgraded
	t.Cleanup(func() { server.Close(websocket.StatusNormalClosure, "test done") })
	_, data, err := server.Read(ctx)
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatalf("decode notification: %v", err)
	}
	if _, ok := message["id"]; ok {
		t.Fatalf("notification has JSON-RPC id: %s", data)
	}
	if got := string(message["method"]); got != `"session/cancel"` {
		t.Fatalf("method = %s, want session/cancel", got)
	}
}

func TestOriginForBaseURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://127.0.0.1:1234", "http://127.0.0.1:1234"},
		{"http://localhost:5678", "http://localhost:5678"},
		{"http://[::1]:9090", "http://[::1]:9090"},
		{"http://127.0.0.1:1234/acp", "http://127.0.0.1:1234"},
		{"https://goose.example.com:8443/acp", "https://goose.example.com:8443"},
	}
	for _, tc := range tests {
		if got := originForBaseURL(tc.input); got != tc.want {
			t.Errorf("originForBaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDialWSSendsOriginHeader(t *testing.T) {
	var gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.Close(websocket.StatusNormalClosure, "done")
	}))
	t.Cleanup(srv.Close)

	conn := newACPConn(srv.URL, Config{}, nil)
	conn.connID = "test-conn"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := conn.dialWS(ctx)
	if err != nil {
		t.Fatalf("dialWS: %v", err)
	}
	_ = ws.Close(websocket.StatusNormalClosure, "done")

	if gotOrigin != srv.URL {
		t.Fatalf("got Origin header %q, want %q", gotOrigin, srv.URL)
	}
}
