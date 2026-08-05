package ws

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"testing"
)

// MADR 0069 P3 (U7) — every error frame the phone sees leaves a daemon log
// line at the default level, carrying the same code and message.
func TestWriteErrorLogsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	s := New(Options{
		Version:    "test",
		ListenAddr: "127.0.0.1:0",
		Log:        slog.New(slog.NewTextHandler(&buf, nil)),
	})
	c := &client{out: make(chan []byte, 1), closed: make(chan struct{})}

	err := fmt.Errorf("cwd %q: %w", "/x",
		&os.PathError{Op: "stat", Path: "/x", Err: syscall.EPERM})
	if werr := s.writeSessionErr(context.Background(), c, "req9", "session_create_failed", err); werr != nil {
		t.Fatal(werr)
	}
	<-c.out

	logged := buf.String()
	for _, want := range []string{"ws error frame", "permission_denied", "req9"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q: %s", want, logged)
		}
	}
	// One line per frame, not one per layer (writeSessionErr → writeError).
	if n := strings.Count(logged, "ws error frame"); n != 1 {
		t.Fatalf("error frame logged %d times, want 1", n)
	}
}
