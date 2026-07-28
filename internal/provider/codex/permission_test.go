package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// permFramer captures what the daemon writes back to codex for a server
// request, and can be made to fail so the retry contract is testable.
type permFramer struct {
	mu        sync.Mutex
	responses []map[string]any
	failWith  error
}

func (f *permFramer) sendResponse(_ context.Context, id json.RawMessage, result any, rpcErr *rpcErrorBody) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.responses = append(f.responses, map[string]any{"id": string(id), "result": result, "error": rpcErr})
	return nil
}

func (f *permFramer) decisions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.responses))
	for _, r := range f.responses {
		m, _ := r["result"].(map[string]any)
		if m == nil {
			out = append(out, "<error>")
			continue
		}
		d, _ := m["decision"].(string)
		out = append(out, d)
	}
	return out
}

func permSession(t *testing.T) (*session, *permFramer) {
	t.Helper()
	f := &permFramer{}
	s := &session{
		localID:      "local-1",
		agentID:      "thread-1",
		events:       make(chan event.Event, 32),
		done:         make(chan struct{}),
		log:          silentLogger(),
		pendingPerms: make(map[string]json.RawMessage),
		respond:      f.sendResponse,
	}
	t.Cleanup(func() { close(s.done) })
	return s, f
}

func drainEvents(s *session) []event.Event {
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

func firstPermissionID(t *testing.T, evs []event.Event) string {
	t.Helper()
	for _, ev := range evs {
		if ev.Type == event.TypePermission {
			return ev.PermissionID
		}
	}
	t.Fatalf("no permission_request in %+v", evs)
	return ""
}

// TestRespondPermissionEmitsResolved is the bug behind the reported loop.
//
// Every other provider emits permission_resolved when the user answers; codex
// emitted it only from its timeout path. Without it the daemon's history ring
// keeps an unresolved permission_request forever, so any reconnect, resync or
// history hydrate re-raises the sheet for a request the engine has already been
// told about. Answering it a second time hits "unknown permission", the client
// reports "Permission respond failed", forgets it presented the request, and
// re-presents it — an inescapable modal loop.
func TestRespondPermissionEmitsResolved(t *testing.T) {
	s, f := permSession(t)
	s.handleApprovalRequest("item/commandExecution/requestApproval",
		json.RawMessage(`7`), json.RawMessage(`{"command":"ls -la"}`))
	permID := firstPermissionID(t, drainEvents(s))

	if err := s.RespondPermission(context.Background(), permID, "accept", false); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	if got := f.decisions(); len(got) != 1 || got[0] != "accept" {
		t.Fatalf("decisions sent to codex = %v, want [accept]", got)
	}

	evs := drainEvents(s)
	var resolved *event.Event
	for i := range evs {
		if evs[i].Type == event.TypePermissionResolved {
			resolved = &evs[i]
		}
	}
	if resolved == nil {
		t.Fatalf("no permission_resolved emitted; every other provider emits one "+
			"and without it the request never clears. events=%+v", evs)
	}
	if resolved.PermissionID != permID {
		t.Errorf("resolved id = %q, want %q", resolved.PermissionID, permID)
	}
	if resolved.Status != event.PermissionStatusResolved {
		t.Errorf("status = %q, want %q", resolved.Status, event.PermissionStatusResolved)
	}
}

func TestRespondPermissionCancelledEmitsCancelledStatus(t *testing.T) {
	s, f := permSession(t)
	s.handleApprovalRequest("item/fileChange/requestApproval",
		json.RawMessage(`8`), json.RawMessage(`{"filePath":"/tmp/x"}`))
	permID := firstPermissionID(t, drainEvents(s))

	if err := s.RespondPermission(context.Background(), permID, "", true); err != nil {
		t.Fatal(err)
	}
	if got := f.decisions(); len(got) != 1 || got[0] != "cancel" {
		t.Fatalf("decisions = %v, want [cancel]", got)
	}
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypePermissionResolved {
			if ev.Status != event.PermissionStatusCancelled {
				t.Errorf("status = %q, want %q", ev.Status, event.PermissionStatusCancelled)
			}
			return
		}
	}
	t.Fatal("no permission_resolved emitted")
}

// TestRespondPermissionRestoresPendingOnWriteFailure: the entry used to be
// deleted before the write, so a failed write lost the request entirely —
// codex still waiting, and every retry answering "unknown permission".
func TestRespondPermissionRestoresPendingOnWriteFailure(t *testing.T) {
	s, f := permSession(t)
	s.handleApprovalRequest("item/commandExecution/requestApproval",
		json.RawMessage(`9`), json.RawMessage(`{"command":"rm -rf /"}`))
	permID := firstPermissionID(t, drainEvents(s))

	f.mu.Lock()
	f.failWith = fmt.Errorf("broken pipe")
	f.mu.Unlock()

	if err := s.RespondPermission(context.Background(), permID, "accept", false); err == nil {
		t.Fatal("expected the write failure to be reported")
	}
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypePermissionResolved {
			t.Fatal("permission announced as resolved after a failed write")
		}
	}

	f.mu.Lock()
	f.failWith = nil
	f.mu.Unlock()
	if err := s.RespondPermission(context.Background(), permID, "accept", false); err != nil {
		t.Fatalf("retry after a transient write failure: %v", err)
	}
	if got := f.decisions(); len(got) != 1 || got[0] != "accept" {
		t.Fatalf("decisions = %v, want the retry to have landed", got)
	}
}

// TestRespondPermissionTwiceIsNotAnError: the second answer is the client
// re-presenting a request it already answered (a resync replay, or two
// devices). It must be a quiet no-op, not an error the client turns into
// "respond failed" and then a re-present loop.
func TestRespondPermissionTwiceIsNotAnError(t *testing.T) {
	s, f := permSession(t)
	s.handleApprovalRequest("item/commandExecution/requestApproval",
		json.RawMessage(`10`), json.RawMessage(`{"command":"ls"}`))
	permID := firstPermissionID(t, drainEvents(s))

	if err := s.RespondPermission(context.Background(), permID, "accept", false); err != nil {
		t.Fatal(err)
	}
	drainEvents(s)
	if err := s.RespondPermission(context.Background(), permID, "accept", false); err != nil {
		t.Fatalf("second answer must not error: %v", err)
	}
	if got := f.decisions(); len(got) != 1 {
		t.Fatalf("decisions = %v, want exactly one answer to reach codex", got)
	}
	found := false
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypePermissionResolved && ev.PermissionID == permID {
			found = true
		}
	}
	if !found {
		t.Fatal("a repeat answer must re-announce the resolution so the client clears")
	}
}

// TestApprovalRequestCarriesDescription: `item/permissions/requestApproval`
// fell through the switch with no text and no tool name, so its sheet had
// nothing to describe what was being approved.
func TestApprovalRequestCarriesDescription(t *testing.T) {
	for _, tc := range []struct {
		method   string
		params   string
		wantTool string
	}{
		{"item/commandExecution/requestApproval", `{"command":"ls -la"}`, "command"},
		{"item/fileChange/requestApproval", `{"filePath":"/tmp/x"}`, "file"},
		{"item/permissions/requestApproval", `{"reason":"needs network access"}`, "permission"},
	} {
		s, _ := permSession(t)
		s.handleApprovalRequest(tc.method, json.RawMessage(`1`), json.RawMessage(tc.params))
		var req *event.Event
		evs := drainEvents(s)
		for i := range evs {
			if evs[i].Type == event.TypePermission {
				req = &evs[i]
			}
		}
		if req == nil {
			t.Fatalf("%s: no permission_request", tc.method)
		}
		if req.ToolName != tc.wantTool {
			t.Errorf("%s: tool = %q, want %q", tc.method, req.ToolName, tc.wantTool)
		}
		if req.Text == "" {
			t.Errorf("%s: permission sheet would describe nothing", tc.method)
		}
	}
}

// TestApprovalOptionsMatchCodexDecisions pins the option ids against the
// engine's own enums (spike inventory, codex-cli 0.145.0): sending a decision
// codex does not accept would fail the approval.
func TestApprovalOptionsMatchCodexDecisions(t *testing.T) {
	valid := map[string]bool{
		"accept": true, "acceptForSession": true, "decline": true, "cancel": true,
		"acceptWithExecpolicyAmendment": true, "applyNetworkPolicyAmendment": true,
	}
	for _, method := range []string{
		"item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
	} {
		s, _ := permSession(t)
		s.handleApprovalRequest(method, json.RawMessage(`1`), json.RawMessage(`{}`))
		for _, ev := range drainEvents(s) {
			if ev.Type != event.TypePermission {
				continue
			}
			if len(ev.Options) == 0 {
				t.Errorf("%s offered no options", method)
			}
			for _, o := range ev.Options {
				if !valid[o.OptionID] {
					t.Errorf("%s offers %q, which codex does not accept", method, o.OptionID)
				}
			}
		}
	}
}

// TestCloseResolvesOutstandingPermissions: a closed session's sheets must not
// be left up on the phone waiting for an answer nothing will ever read.
func TestCloseResolvesOutstandingPermissions(t *testing.T) {
	s, _ := permSession(t)
	s.handleApprovalRequest("item/commandExecution/requestApproval",
		json.RawMessage(`11`), json.RawMessage(`{"command":"ls"}`))
	permID := firstPermissionID(t, drainEvents(s))

	s.cancelPendingPermissions("session closed")

	found := false
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypePermissionResolved && ev.PermissionID == permID {
			found = true
			if ev.Status != event.PermissionStatusCancelled {
				t.Errorf("status = %q, want cancelled", ev.Status)
			}
		}
	}
	if !found {
		t.Fatal("outstanding permission was not cancelled; its sheet stays up forever")
	}
	if _, still := s.pendingPerms[permID]; still {
		t.Error("pending entry survived the cancel sweep")
	}
}

func TestRespondPermissionUnknownIDStillReported(t *testing.T) {
	s, _ := permSession(t)
	err := s.RespondPermission(context.Background(), "never-existed", "accept", false)
	if err == nil {
		t.Fatal("expected an error for an id this session never issued")
	}
	if !strings.Contains(err.Error(), "never-existed") {
		t.Errorf("error %q should name the id", err)
	}
}
