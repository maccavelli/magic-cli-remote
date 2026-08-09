package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// captureHost records emitted events for dialect unit tests.
type captureHost struct {
	mu       sync.Mutex
	events   []event.Event
	model    string
	agent    string
	api      httpagent.API
	endTurns int
	// pending/autoApprove/done/ds back the permission bookkeeping the real
	// session owns (MADR 0044). ds is the dialect session this host was handed
	// to; set it when a test needs RespondPermission to reach the engine.
	pending     map[string]struct{}
	autoApprove bool
	done        chan struct{}
	ds          httpagent.DialectSession
}

func (h *captureHost) ID() string               { return "local" }
func (h *captureHost) AgentSessionID() string   { return "ses_test" }
func (h *captureHost) CWD() string              { return "/tmp" }
func (h *captureHost) Config() httpagent.Config { return httpagent.Config{} }

// Agent/SetAgent and Model/RecordModel mirror the real session's locked pairs so
// mode and in-place model switches are observable in dialect tests.
func (h *captureHost) Agent() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agent
}

func (h *captureHost) Model() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.model
}

func (h *captureHost) RecordModel(model string) {
	h.mu.Lock()
	h.model = model
	h.mu.Unlock()
}

func (h *captureHost) SetAgent(name string) {
	h.mu.Lock()
	h.agent = name
	h.mu.Unlock()
}
func (h *captureHost) Log() *slog.Logger { return slog.Default() }
func (h *captureHost) API() httpagent.API {
	if h.api != nil {
		return h.api
	}
	return func(context.Context, string, string, any, any) error { return nil }
}
func (h *captureHost) Emit(ev event.Event) {
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.mu.Unlock()
}
func (h *captureHost) EmitReplay(ev event.Event) { h.Emit(ev) }
func (h *captureHost) EndTurn() bool {
	h.mu.Lock()
	h.endTurns++
	h.mu.Unlock()
	return true
}
func (h *captureHost) BindChildAlias(string)                       {}
func (h *captureHost) UnbindChildAlias(string)                     {}
func (h *captureHost) NoteNodeStatus(string, httpagent.NodeStatus) {}
func (h *captureHost) TryEndTurnIfTreeIdle() bool                  { return h.EndTurn() }
func (h *captureHost) EventAgentSessionID() string                 { return "" }
func (h *captureHost) TrackPermissionOrigin(string, string)        {}
func (h *captureHost) PermissionOrigin(string) string              { return h.AgentSessionID() }
func (h *captureHost) endTurnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.endTurns
}
func (h *captureHost) TrackQuestion(string)            {}
func (h *captureHost) TakeQuestionPending(string) bool { return true }

// The permission half of the fake mirrors the real session's bookkeeping rather
// than stubbing it: MADR 0044's auto-approve path is *defined* by that
// bookkeeping (claim-once, disarm-the-expiry, resolve-once), so a fake that
// always says "yes, pending" would let the bugs it guards against pass.

func (h *captureHost) TrackPermission(id string) {
	h.mu.Lock()
	if h.pending == nil {
		h.pending = map[string]struct{}{}
	}
	h.pending[id] = struct{}{}
	h.mu.Unlock()
}

func (h *captureHost) TakePending(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.pending[id]
	delete(h.pending, id)
	return ok
}

func (h *captureHost) PendingPermissions() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.pending))
	for id := range h.pending {
		ids = append(ids, id)
	}
	return ids
}

func (h *captureHost) AutoApprove() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.autoApprove
}

func (h *captureHost) SetAutoApprove(on bool) {
	h.mu.Lock()
	h.autoApprove = on
	h.mu.Unlock()
}

func (h *captureHost) Done() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done == nil {
		h.done = make(chan struct{})
	}
	return h.done
}

// RespondPermission mirrors httpagent.session.RespondPermission: claim first,
// delegate, restore the claim on failure, then emit permission_resolved.
func (h *captureHost) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool, deviceID string) error {
	if !h.TakePending(permissionID) {
		return fmt.Errorf("%w %q", httpagent.ErrPermissionNotPending, permissionID)
	}
	h.mu.Lock()
	ds := h.ds
	h.mu.Unlock()
	if ds != nil {
		if err := ds.RespondPermission(ctx, permissionID, optionID, cancelled); err != nil {
			h.mu.Lock()
			h.pending[permissionID] = struct{}{}
			h.mu.Unlock()
			return err
		}
	}
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	h.Emit(event.Event{
		Type:         event.TypePermissionResolved,
		PermissionID: permissionID,
		Status:       status,
		DeviceID:     deviceID,
		OptionID:     optionID,
	})
	return nil
}

// Ensure captureHost implements httpagent.Host.
var _ httpagent.Host = (*captureHost)(nil)

func (h *captureHost) texts(t event.Type) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b string
	for _, e := range h.events {
		if e.Type == t {
			b += e.Text
		}
	}
	return b
}

func newTestSession(h *captureHost) *httpSession {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "kilo", defaultModelID: defaultModelID}
	return d.NewSession(h).(*httpSession)
}

func TestPartDeltaStreamsAssistantText(t *testing.T) {
	h := &captureHost{}
	s := newTestSession(h)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_1","role":"assistant"}}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_r","messageID":"msg_1","type":"reasoning","text":""}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_r","field":"text","delta":"think"
	}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":""}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_t","field":"text","delta":"PO"
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_t","field":"text","delta":"NG"
	}`))
	// Final snapshot must not double-emit already-streamed text.
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":"PONG"}
	}`))
	if got := h.texts(event.TypeAssistantChunk); got != "PONG" {
		t.Fatalf("assistant text=%q want PONG", got)
	}
	if got := h.texts(event.TypeThoughtChunk); got != "think" {
		t.Fatalf("thought text=%q want think", got)
	}
}

// Kilo delta (MADR 0075 §2.4): parts marked metadata.kilocode.lifecycle
// "transient" are engine chrome ("Initializing snapshot…") and must never
// render as assistant chat — neither their snapshots nor later deltas.
func TestTransientLifecyclePartsFiltered(t *testing.T) {
	h := &captureHost{}
	s := newTestSession(h)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_1","role":"assistant"}}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_x","messageID":"msg_1","type":"text",
			"text":"Initializing snapshot…",
			"metadata":{"kilocode":{"lifecycle":"transient"}}}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_x","field":"text","delta":" still initializing"
	}`))
	if got := h.texts(event.TypeAssistantChunk); got != "" {
		t.Fatalf("transient part leaked into chat: %q", got)
	}
	// A normal part on the same message still streams.
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":"real"}
	}`))
	if got := h.texts(event.TypeAssistantChunk); got != "real" {
		t.Fatalf("normal part blocked: %q", got)
	}
}

// Kilo delta (MADR 0075 §2.4): session.turn.close is an explicit turn boundary
// alongside session.idle; both may arrive and the turn must end exactly once.
func TestTurnCloseEndsTurnOnce(t *testing.T) {
	h := &captureHost{}
	s := newTestSession(h)
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"ses_test","status":{"type":"busy"}}`))
	s.HandleEvent("session.turn.close", json.RawMessage(`{"sessionID":"ses_test"}`))
	if h.endTurnCount() != 1 {
		t.Fatalf("endTurns=%d want 1", h.endTurnCount())
	}
	var completes int
	h.mu.Lock()
	for _, e := range h.events {
		if e.Type == event.TypeTurnComplete {
			completes++
		}
	}
	h.mu.Unlock()
	if completes != 1 {
		t.Fatalf("turn_complete count=%d want 1", completes)
	}
}

// Kilo extras that must be ignored: none of these may emit transcript events.
func TestIgnoredKiloEventsEmitNothing(t *testing.T) {
	h := &captureHost{}
	s := newTestSession(h)
	for _, typ := range []string{
		"sync", "file.watcher.updated", "message.part.removed",
		"session.next.agent.switched", "session.next.model.switched",
		"session.turn.open", "server.heartbeat",
	} {
		s.HandleEvent(typ, json.RawMessage(`{"sessionID":"ses_test"}`))
	}
	h.mu.Lock()
	n := len(h.events)
	h.mu.Unlock()
	if n != 0 {
		t.Fatalf("ignored events emitted %d daemon events", n)
	}
}

// session.error must classify through agenterr and end the turn.
func TestSessionErrorEmitsClassifiedError(t *testing.T) {
	h := &captureHost{}
	s := newTestSession(h)
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"ses_test","status":{"type":"busy"}}`))
	s.HandleEvent("session.error", json.RawMessage(`{
		"sessionID":"ses_test",
		"error":{"name":"UnknownError","data":{"message":"Model not found: kilo/nope"}}
	}`))
	var sawErr bool
	h.mu.Lock()
	for _, e := range h.events {
		if e.Type == event.TypeError && strings.Contains(e.Error, "Model not found") {
			sawErr = true
		}
	}
	h.mu.Unlock()
	if !sawErr {
		t.Fatal("session.error did not surface an error event")
	}
	if h.endTurnCount() != 1 {
		t.Fatalf("endTurns=%d want 1", h.endTurnCount())
	}
}

// frameLivePermissionAsk is the verbatim permission.asked frame captured from
// kilo 7.4.20 on 2026-08-06 (docs/kilo-spike-7.4.20/sse-permission.raw, path
// anonymized) — the PD6 fixture that resolves MADR 0075 Q10.
const frameLivePermissionAsk = `{"directory": "/work/project", "project": "global", "payload": {"id": "evt_fd7f683de002ISDK4Nlpt0nACW", "type": "permission.asked", "properties": {"id": "per_fd7f683de00185EH7qTcT5q0vk", "sessionID": "ses_0280995bbffef2wMjAO32CCPXs", "permission": "bash", "patterns": ["echo fixture-ok"], "metadata": {"command": "echo fixture-ok", "description": "Run echo fixture-ok"}, "always": ["echo *"], "tool": {"messageID": "msg_fd7f66f9d001GMQGBvoRaQ1d39", "callID": "call-c2ca8d85-5fe3-4fa2-922a-2d2628e33a91"}}}}`

// TestPermissionRepliedResyncLeavesDeviceEmpty covers kilo's resync
// emission site (session.go's "permission.replied"/"permission.v2.replied"
// SSE handler) — distinct from httpagent's shared RespondPermission path
// (tested in the httpagent package). This path fires when the engine
// reports a permission was answered by some means other than mcremote's own
// RespondPermission call (e.g. resolved via kilo's own CLI/TUI, or caught
// on reconnect resync) — there is no device to attribute it to, so
// DeviceID/OptionID must stay empty rather than looking like an oversight
// (MADR 0077 §1, PLAN P6 step 4).
func TestPermissionRepliedResyncLeavesDeviceEmpty(t *testing.T) {
	h := &captureHost{}
	h.TrackPermission("perm-resync")
	s := newTestSession(h)

	s.HandleEvent("permission.replied", json.RawMessage(`{"id":"perm-resync"}`))

	var found bool
	for _, ev := range h.events {
		if ev.Type != event.TypePermissionResolved || ev.PermissionID != "perm-resync" {
			continue
		}
		found = true
		if ev.Status != event.PermissionStatusResolved {
			t.Fatalf("status=%q want resolved", ev.Status)
		}
		if ev.DeviceID != "" || ev.OptionID != "" {
			t.Fatalf("device_id=%q option_id=%q, want both empty (resync path, no device)",
				ev.DeviceID, ev.OptionID)
		}
	}
	if !found {
		t.Fatal("expected a permission_resolved event from the permission.replied resync path")
	}
}

// TestLivePermissionAskFixtureDecodes pins the whole decode path against the
// captured frame: envelope unwrap, session demux, and the ask normalization
// that feeds emitPermissionAsk.
func TestLivePermissionAskFixtureDecodes(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	typ, props, sid, ok := d.DecodeFrame([]byte(frameLivePermissionAsk))
	if !ok || typ != "permission.asked" {
		t.Fatalf("DecodeFrame = (%q, ok=%v)", typ, ok)
	}
	if sid != "ses_0280995bbffef2wMjAO32CCPXs" {
		t.Fatalf("sessionID = %q", sid)
	}
	p, ok := normalizePermissionAsk(props)
	if !ok {
		t.Fatal("normalizePermissionAsk rejected the live frame")
	}
	if p.ID != "per_fd7f683de00185EH7qTcT5q0vk" {
		t.Fatalf("permission id = %q", p.ID)
	}
	if p.Name != "bash" {
		t.Fatalf("permission name = %q", p.Name)
	}
	if len(p.Patterns) != 1 || p.Patterns[0] != "echo fixture-ok" {
		t.Fatalf("patterns = %v", p.Patterns)
	}
}

// The full permission emit path: ask fixture in → permission_request event out
// with once/always/reject options (matching the live CLI's option set).
func TestLivePermissionAskFixtureEmits(t *testing.T) {
	h := &captureHost{}
	s := newTestSession(h)
	d := s.d
	_, props, _, _ := d.DecodeFrame([]byte(frameLivePermissionAsk))
	s.HandleEvent("permission.asked", props)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.events {
		if e.Type == event.TypePermission {
			if e.PermissionID != "per_fd7f683de00185EH7qTcT5q0vk" {
				t.Fatalf("emitted permission id = %q", e.PermissionID)
			}
			if len(e.Options) < 3 {
				t.Fatalf("expected >=3 options (once/always/reject), got %+v", e.Options)
			}
			return
		}
	}
	t.Fatal("no permission_request emitted for live ask fixture")
}
