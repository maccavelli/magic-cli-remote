package codex

import (
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MADR 0069 P2 (U5) — codex turn errors are classified, and a permission
// denial under a confining sandbox gets the mode-pointing hint (Seatbelt
// EPERM out of the workspace is the sandbox by design; FDA advice would be
// wrong twice). Under danger-full-access no hint is offered.

func drainForError(t *testing.T, s *session) event.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeError {
				return ev
			}
		case <-deadline:
			t.Fatal("no error event emitted")
		}
	}
}

func TestTurnErrorPermissionHintUnderConfiningSandbox(t *testing.T) {
	// Default policy seeds workspace-write (MADR 0047) — confining.
	s := newSession(nil, Config{}, provider.StartOptions{LocalSessionID: "s1"}, testLogger(t))
	s.emitTurnComplete("error", "stat /Users/x/outside: operation not permitted")

	ev := drainForError(t, s)
	if ev.ErrorKind != "permission" {
		t.Fatalf("ErrorKind = %q, want permission", ev.ErrorKind)
	}
	if !strings.Contains(ev.Error, "allow_full_access") {
		t.Fatalf("confining sandbox: no mode hint in %q", ev.Error)
	}
	if strings.Contains(ev.Error, "Full Disk Access") {
		t.Fatalf("sandbox denial must not point at FDA: %q", ev.Error)
	}
}

func TestTurnErrorPermissionNoHintWithoutConfinement(t *testing.T) {
	s := newSession(nil, Config{}, provider.StartOptions{LocalSessionID: "s1"}, testLogger(t))
	s.mu.Lock()
	s.sandboxMode = "danger-full-access"
	s.mu.Unlock()
	s.emitTurnComplete("error", "open /etc/x: operation not permitted")

	ev := drainForError(t, s)
	if ev.ErrorKind != "permission" {
		t.Fatalf("ErrorKind = %q, want permission", ev.ErrorKind)
	}
	if strings.Contains(ev.Error, "allow_full_access") {
		t.Fatalf("unconfined session must not get the mode hint: %q", ev.Error)
	}
}

// Bonus of wiring Classify (codex previously never called it): quota errors
// now classify like every other provider.
func TestTurnErrorQuotaClassified(t *testing.T) {
	s := newSession(nil, Config{}, provider.StartOptions{LocalSessionID: "s1"}, testLogger(t))
	s.emitTurnComplete("error", "quota exceeded for model")

	ev := drainForError(t, s)
	if ev.ErrorKind != "quota" {
		t.Fatalf("ErrorKind = %q, want quota", ev.ErrorKind)
	}
}
