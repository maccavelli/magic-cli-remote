package acphttp

import (
	"context"
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

	conn := newACPConn(srv.URL, Config{})
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
	if err := server.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		t.Fatalf("server write: %v", err)
	}

	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read %d-byte frame: %v (read limit not raised?)", len(payload), err)
	}
	if len(data) != len(payload) {
		t.Fatalf("frame truncated: want %d bytes, got %d", len(payload), len(data))
	}
}
