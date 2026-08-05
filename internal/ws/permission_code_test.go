package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// MADR 0069 P1 (U4) — an OS permission denial surviving a provider error
// chain maps to the stable permission_denied code at the WS boundary, and
// unrelated stat errors keep the caller's fallback code.
func TestWriteSessionErrMapsPermissionDenied(t *testing.T) {
	s := New(Options{Version: "test", ListenAddr: "127.0.0.1:0"})

	frameFor := func(err error) protocol.ErrorPayload {
		t.Helper()
		c := &client{out: make(chan []byte, 1), closed: make(chan struct{})}
		if werr := s.writeSessionErr(context.Background(), c, "req1", "session_create_failed", err); werr != nil {
			t.Fatal(werr)
		}
		var env protocol.Envelope
		if uerr := json.Unmarshal(<-c.out, &env); uerr != nil {
			t.Fatal(uerr)
		}
		var p protocol.ErrorPayload
		if uerr := json.Unmarshal(env.Payload, &p); uerr != nil {
			t.Fatal(uerr)
		}
		return p
	}

	eperm := fmt.Errorf("cwd %q: %w", "/Users/x/Documents",
		&os.PathError{Op: "stat", Path: "/Users/x/Documents", Err: syscall.EPERM})
	if p := frameFor(eperm); p.Code != protocol.ErrPermissionDenied {
		t.Fatalf("EPERM chain code = %q, want %q", p.Code, protocol.ErrPermissionDenied)
	}

	enoent := fmt.Errorf("cwd %q does not exist: %w", "/nope",
		&os.PathError{Op: "stat", Path: "/nope", Err: syscall.ENOENT})
	if p := frameFor(enoent); p.Code != "session_create_failed" {
		t.Fatalf("ENOENT chain code = %q, want fallback", p.Code)
	}
}
