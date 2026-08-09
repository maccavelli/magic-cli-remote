package acphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// permFramer captures replies to agent permission requests and can be made to
// fail, so the retry contract is testable without an engine.
type permFramer struct {
	mu       sync.Mutex
	replies  int
	failWith error
}

func (f *permFramer) sendRequest(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *permFramer) sendResponse(context.Context, json.RawMessage, any, *rpcErrorBody) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.replies++
	return nil
}

func (f *permFramer) sendNotification(context.Context, string, any) error { return nil }

func (f *permFramer) replyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replies
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func permTestSession(t *testing.T) (*session, *permFramer) {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(discardWriter{}, nil))
	f := &permFramer{}
	p := &Provider{
		spec: Spec{ID: "goose", DefaultBin: "goose"},
		cfg:  Config{Bin: "goose"},
		log:  quiet,
		fr:   f,
	}
	s := &session{
		p:                 p,
		localID:           "local-1",
		agentID:           "agent-1",
		events:            make(chan event.Event, 32),
		done:              make(chan struct{}),
		log:               quiet,
		pendingPerms:      make(map[string]json.RawMessage),
		pendingPermTimers: make(map[string]*time.Timer),
	}
	t.Cleanup(func() { close(s.done) })
	return s, f
}

func drainPermEvents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func resolvedFor(evs []event.Event, id string) *event.Event {
	for i := range evs {
		if evs[i].Type == event.TypePermissionResolved && evs[i].PermissionID == id {
			return &evs[i]
		}
	}
	return nil
}

// TestRespondPermissionEmitsResolved: the answer path emitted nothing, so the
// daemon's history ring kept an unresolved permission_request forever and every
// reconnect or resync re-raised the sheet for a request the agent had already
// been told about. Answering the replay hits "unknown permission", which the
// client reports as "permission respond failed" and then re-presents — a loop.
func TestRespondPermissionEmitsResolved(t *testing.T) {
	s, f := permTestSession(t)
	s.pendingPerms["p1"] = json.RawMessage(`3`)

	if err := s.RespondPermission(context.Background(), "p1", "allow", false, "dev-1"); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	if f.replyCount() != 1 {
		t.Fatalf("replies to the agent = %d, want 1", f.replyCount())
	}
	ev := resolvedFor(drainPermEvents(s), "p1")
	if ev == nil {
		t.Fatal("no permission_resolved emitted; the client's sheet never clears")
	}
	if ev.Status != event.PermissionStatusResolved {
		t.Errorf("status = %q, want %q", ev.Status, event.PermissionStatusResolved)
	}
	// MADR 0077 §1: both the resolving device and its chosen option must
	// land on the event — previously dropped entirely.
	if ev.DeviceID != "dev-1" || ev.OptionID != "allow" {
		t.Errorf("device_id=%q option_id=%q, want dev-1/allow", ev.DeviceID, ev.OptionID)
	}
}

// TestPermissionTimeoutLeavesDeviceEmpty covers the PermissionTimeoutSeconds
// fail-safe auto-cancel path (distinct from RespondPermission's normal
// resolve/cancel path, tested above): no device answered in time, so
// DeviceID/OptionID must stay empty rather than looking like an oversight
// (MADR 0077 §1, PLAN P6 step 4) — the same reasoning as codex's
// auto-mode-arm sweep.
func TestPermissionTimeoutLeavesDeviceEmpty(t *testing.T) {
	s, _ := permTestSession(t)
	s.permTimeout = 20 * time.Millisecond

	reqJSON, err := json.Marshal(permissionRequestParams{
		SessionID: "agent-1",
		Options:   []acp.PermissionOption{{OptionId: "allow", Name: "Allow", Kind: "allow_once"}},
		ToolCall:  acp.ToolCallUpdate{ToolCallId: "tc1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.handlePermissionRequest(json.RawMessage(`5`), reqJSON)

	var ev *event.Event
	deadline := time.After(2 * time.Second)
	for ev == nil {
		select {
		case got := <-s.events:
			if got.Type == event.TypePermissionResolved {
				e := got
				ev = &e
			}
		case <-deadline:
			t.Fatal("no permission_resolved emitted after the timeout")
		}
	}
	if ev.Status != event.PermissionStatusCancelled || !ev.TimedOut {
		t.Fatalf("resolved event = %+v, want cancelled+timed_out", ev)
	}
	if ev.DeviceID != "" || ev.OptionID != "" {
		t.Fatalf("device_id=%q option_id=%q, want both empty (timeout fail-safe, no device)",
			ev.DeviceID, ev.OptionID)
	}
}

func TestRespondPermissionCancelledStatus(t *testing.T) {
	s, _ := permTestSession(t)
	s.pendingPerms["p1"] = json.RawMessage(`3`)
	if err := s.RespondPermission(context.Background(), "p1", "", true, "dev-1"); err != nil {
		t.Fatal(err)
	}
	ev := resolvedFor(drainPermEvents(s), "p1")
	if ev == nil || ev.Status != event.PermissionStatusCancelled {
		t.Fatalf("resolved event = %+v, want cancelled", ev)
	}
}

// TestRespondPermissionRestoresPendingOnWriteFailure: the entry was deleted
// before the reply was written, so a failed write stranded the request — the
// agent still waiting, every retry answering "unknown permission".
func TestRespondPermissionRestoresPendingOnWriteFailure(t *testing.T) {
	s, f := permTestSession(t)
	s.pendingPerms["p1"] = json.RawMessage(`3`)

	f.mu.Lock()
	f.failWith = fmt.Errorf("engine down")
	f.mu.Unlock()

	if err := s.RespondPermission(context.Background(), "p1", "allow", false, "dev-1"); err == nil {
		t.Fatal("expected the write failure to be reported")
	}
	if resolvedFor(drainPermEvents(s), "p1") != nil {
		t.Fatal("permission announced as resolved after a failed write")
	}

	f.mu.Lock()
	f.failWith = nil
	f.mu.Unlock()
	if err := s.RespondPermission(context.Background(), "p1", "allow", false, "dev-1"); err != nil {
		t.Fatalf("retry after a transient failure: %v", err)
	}
	if resolvedFor(drainPermEvents(s), "p1") == nil {
		t.Fatal("the retry did not resolve the request")
	}
}

// TestRespondPermissionTwiceIsNotAnError: the second answer is a client
// re-presenting a request it already answered. It must be a quiet no-op that
// re-announces the resolution, not an error that restarts the loop.
func TestRespondPermissionTwiceIsNotAnError(t *testing.T) {
	s, f := permTestSession(t)
	s.pendingPerms["p1"] = json.RawMessage(`3`)

	if err := s.RespondPermission(context.Background(), "p1", "allow", false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	drainPermEvents(s)
	if err := s.RespondPermission(context.Background(), "p1", "allow", false, "dev-1"); err != nil {
		t.Fatalf("second answer must not error: %v", err)
	}
	if f.replyCount() != 1 {
		t.Fatalf("replies = %d, want exactly one to reach the agent", f.replyCount())
	}
	if resolvedFor(drainPermEvents(s), "p1") == nil {
		t.Fatal("a repeat answer must re-announce the resolution so the client clears")
	}
}

func TestRespondPermissionUnknownIDStillErrors(t *testing.T) {
	s, _ := permTestSession(t)
	if err := s.RespondPermission(context.Background(), "never-existed", "allow", false, "dev-1"); err == nil {
		t.Fatal("an id this session never issued must still be an error")
	}
}

// TestAnsweredSetIsBounded keeps a long session from growing the map forever.
func TestAnsweredSetIsBounded(t *testing.T) {
	s, _ := permTestSession(t)
	for i := 0; i < maxAnsweredPerms+50; i++ {
		id := fmt.Sprintf("p%d", i)
		s.pendingPerms[id] = json.RawMessage(`3`)
		if err := s.RespondPermission(context.Background(), id, "allow", false, "dev-1"); err != nil {
			t.Fatal(err)
		}
		drainPermEvents(s)
	}
	s.pendingPermsMu.Lock()
	n, order := len(s.answeredPerms), len(s.answeredOrder)
	s.pendingPermsMu.Unlock()
	if n > maxAnsweredPerms || order > maxAnsweredPerms {
		t.Fatalf("answered set = %d ids / %d order entries, cap is %d", n, order, maxAnsweredPerms)
	}
}
