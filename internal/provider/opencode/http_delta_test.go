package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// captureHost records emitted events for dialect unit tests.
type captureHost struct {
	cwd                string
	startThinkingLevel string
	// logger overrides the default so a test can assert on what was logged.
	logger   *slog.Logger
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

func (h *captureHost) ID() string             { return "local" }
func (h *captureHost) AgentSessionID() string { return "ses_test" }

// CWD is overridable so workspace tests can root a session at a real temp
// directory and exercise the filesystem symlink checks.
func (h *captureHost) CWD() string {
	if h.cwd != "" {
		return h.cwd
	}
	return "/tmp"
}
func (h *captureHost) Config() httpagent.Config { return httpagent.Config{} }

// Agent/SetAgent and Model/RecordModel mirror the real session's locked pairs so
// mode and in-place model switches are observable in dialect tests.
func (h *captureHost) Agent() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agent
}

// StartThinkingLevel implements httpagent.Host. Tests that exercise start-time
// rung precedence set startThinkingLevel directly.
func (h *captureHost) StartThinkingLevel() string { return h.startThinkingLevel }

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
func (h *captureHost) Log() *slog.Logger {
	if h.logger != nil {
		return h.logger
	}
	return slog.Default()
}
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

// texts reduces the captured stream the way a correct consumer does: append
// deltas accumulate per native part, and a Replace snapshot supersedes whatever
// that part held (MADR 0112 A3). Parts are concatenated in first-seen order.
//
// Reducing rather than blindly concatenating is the whole point of P4: a
// snapshot repeats text already streamed, so a consumer that appends it renders
// the reply twice.
func (h *captureHost) texts(t event.Type) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var order []string
	byPart := map[string]string{}
	for _, e := range h.events {
		if e.Type != t {
			continue
		}
		key := e.NativeMessageID + "\x00" + e.NativePartID
		if _, seen := byPart[key]; !seen {
			order = append(order, key)
		}
		if e.Replace {
			byPart[key] = e.Text
			continue
		}
		byPart[key] += e.Text
	}
	var b string
	for _, k := range order {
		b += byPart[k]
	}
	return b
}

func TestPartDeltaStreamsAssistantText(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)

	// Role registration (assistant).
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_1","role":"assistant"}}`))
	// Announce reasoning part type, then stream deltas.
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_r","messageID":"msg_1","type":"reasoning","text":""}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_r","field":"text","delta":"think"
	}`))
	// Text part deltas (the primary streaming path on OpenCode 1.18).
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":""}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_t","field":"text","delta":"Hel"
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_t","field":"text","delta":"lo"
	}`))
	// Final snapshot must not double-emit already-streamed text.
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":"Hello"}
	}`))

	if got := h.texts(event.TypeAssistantChunk); got != "Hello" {
		t.Fatalf("assistant text=%q want Hello", got)
	}
	if got := h.texts(event.TypeThoughtChunk); got != "think" {
		t.Fatalf("thought text=%q want think", got)
	}
}

func TestUserDeltasAreDropped(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_u","role":"user"}}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_u","partID":"prt_u","field":"text","delta":"hi"
	}`))
	if got := h.texts(event.TypeAssistantChunk); got != "" {
		t.Fatalf("user delta leaked: %q", got)
	}
}

// M9: a delta whose message.updated (role) frame was missed must NOT default to
// assistant — that echoed the user's own prompt back as an assistant bubble. The
// authoritative part.updated snapshot re-delivers assistant text once role lands.
func TestRolelessDeltasAreDropped(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	// No message.updated for msg_x: role is unknown.
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_x","partID":"prt_x","field":"text","delta":"echo"
	}`))
	if got := h.texts(event.TypeAssistantChunk); got != "" {
		t.Fatalf("roleless delta leaked as assistant: %q", got)
	}
}

// M8: when a delta is dropped, the authoritative snapshot diverges from what we
// streamed. The pre-fix code tracked a byte cursor and sliced the snapshot at
// that offset — which, on divergence, cut through a multi-byte rune and emitted
// invalid UTF-8. Tracking the real text lets us re-align at a rune boundary.
func TestSnapshotDivergenceStaysValidUTF8(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"m","role":"assistant"}}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{"part":{"id":"p","messageID":"m","type":"text","text":""}}`))
	// Two ASCII deltas accumulate 2 bytes; the snapshot diverges at byte 1 with a
	// 3-byte rune (€). A byte cursor of 2 would slice mid-rune.
	s.HandleEvent("message.part.delta", json.RawMessage(`{"messageID":"m","partID":"p","field":"text","delta":"a"}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{"messageID":"m","partID":"p","field":"text","delta":"b"}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{"part":{"id":"p","messageID":"m","type":"text","text":"a€cd"}}`))
	got := h.texts(event.TypeAssistantChunk)
	if !utf8.ValidString(got) {
		t.Fatalf("emitted assistant text is not valid UTF-8: %q (% x)", got, got)
	}
	if !strings.Contains(got, "€cd") {
		t.Fatalf("expected rune-aligned tail %q in %q", "€cd", got)
	}
}

func TestCommonPrefixLenRuneSafe(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "abc", 0},
		{"abc", "abc", 3},
		{"abc", "abd", 2},
		{"a€", "a€b", len("a€")},          // full common prefix, on a boundary
		{"ab", "a€cd", 1},                 // diverge at byte 1 (a rune start in b)
		{"a\xe2\x82", "a\xe2\x82\xac", 1}, // a ends mid-€: back off to "a"
	}
	for _, c := range cases {
		if got := commonPrefixLen(c.a, c.b); got != c.want {
			t.Errorf("commonPrefixLen(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
		// The result must never index into the middle of a rune in b.
		if n := commonPrefixLen(c.a, c.b); n < len(c.b) && !utf8.RuneStart(c.b[n]) {
			t.Errorf("commonPrefixLen(%q,%q)=%d lands mid-rune in b", c.a, c.b, n)
		}
	}
}

// seenTools dedups first-sighting (tool_call) from update (tool_call_update)
// WITHIN a turn. Left untouched it accumulates every tool id ever seen for the
// life of the session; turn end (session.idle) must drop it so it stays bounded.
func TestSeenToolsClearedOnIdle(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	// Drive several distinct tool calls to populate seenTools.
	for _, id := range []string{"call_a", "call_b", "call_c"} {
		s.HandleEvent("message.part.updated", json.RawMessage(`{
			"part":{"id":"prt","messageID":"m","type":"tool","callID":"`+id+`","state":{"status":"completed"}}
		}`))
	}
	s.mu.Lock()
	n := len(s.seenTools)
	s.mu.Unlock()
	if n != 3 {
		t.Fatalf("seenTools populated with %d ids, want 3", n)
	}

	// The turn ends: the accumulated tool ids are no longer needed and must be
	// dropped so the map cannot grow without bound over a long session.
	s.HandleEvent("session.idle", json.RawMessage(`{}`))
	s.mu.Lock()
	n = len(s.seenTools)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("seenTools=%d after session.idle, want 0 (turn over)", n)
	}
}

func TestSeededFallbackModel(t *testing.T) {
	// NewHTTP must seed a usable free fast model without waiting on AfterBoot.
	_ = NewHTTP(Config{})
	h := &captureHost{model: ""}
	d := &httpDialect{
		log:                  slog.Default(),
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
	}
	s := d.NewSession(h).(*httpSession)
	mp, mid := s.resolveModel()
	if mp != "opencode" || mid != zenDefaultModel {
		t.Fatalf("resolveModel=%s/%s want opencode/%s", mp, mid, zenDefaultModel)
	}
	// The seed is the engine's own default on the assessed release, not a
	// hand-picked latency winner. deepseek-v4-flash-free and
	// north-mini-code-free were both gone from the 1.18.21 Zen catalog, so the
	// old seed pointed at a model that no longer existed (MADR 0112 D6).
	if zenDefaultModel != "big-pickle" {
		t.Fatalf("zenDefaultModel=%q; expected the engine default big-pickle", zenDefaultModel)
	}
	_ = provider.Content{}
}

// The offline catalog is a picker convenience, not a preference ranking: the
// live engine's default replaces it as soon as AfterBoot returns (MADR 0112 D6).
func TestStaticCatalogOffersTheSeed(t *testing.T) {
	opts := staticModelOptions()
	if len(opts) == 0 {
		t.Fatal("static catalog is empty")
	}
	var ids []string
	for _, o := range opts {
		ids = append(ids, o.ID)
	}
	if opts[0].ID != "opencode/"+zenDefaultModel {
		t.Errorf("static catalog leads with %q, want the seed %q", opts[0].ID, "opencode/"+zenDefaultModel)
	}
	for _, gone := range []string{"opencode/deepseek-v4-flash-free", "opencode/north-mini-code-free"} {
		if slices.Contains(ids, gone) {
			t.Errorf("static catalog still offers %q, which 1.18.21 no longer lists", gone)
		}
	}
}

func TestServeArgsPure(t *testing.T) {
	dDefault := &httpDialect{}
	argsDefault := dDefault.ServeArgs(1234)
	wantDefault := []string{"serve", "--hostname", "127.0.0.1", "--port", "1234"}
	if !slices.Equal(argsDefault, wantDefault) {
		t.Fatalf("ServeArgs default = %v, want %v", argsDefault, wantDefault)
	}

	dPure := &httpDialect{pure: true}
	argsPure := dPure.ServeArgs(1234)
	wantPure := []string{"serve", "--hostname", "127.0.0.1", "--port", "1234", "--pure"}
	if !slices.Equal(argsPure, wantPure) {
		t.Fatalf("ServeArgs pure = %v, want %v", argsPure, wantPure)
	}
}

func TestToolOutputVisibilityAndClipBlock(t *testing.T) {
	// 1. clipBlock preserves newlines & handles rune boundaries.
	multiLine := "line 1\nline 2\nline 3"
	if got := clipBlock(multiLine, 100); got != multiLine {
		t.Errorf("clipBlock short multi-line = %q, want %q", got, multiLine)
	}

	// 2. clipBlock rune-safety test.
	// "é" is 2 bytes (0xC3, 0xA9).
	strWithRune := "abc" + strings.Repeat("é", 10)
	// If max is 5, "abc" is 3 bytes, plus 1 byte of "é" = index 4 is mid-rune.
	// clipBlock should back off to 3 bytes ("abc") and append "\n…[truncated]".
	clippedRune := clipBlock(strWithRune, 4)
	if !strings.HasPrefix(clippedRune, "abc\n…[truncated]") {
		t.Errorf("clipBlock failed rune boundary backoff, got %q", clippedRune)
	}
	if !utf8.ValidString(clippedRune) {
		t.Errorf("clipBlock output is invalid UTF-8: %q", clippedRune)
	}

	// 3. Tool frame carrying output emits output as Text.
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	makeToolFrame := func(output, errStr, title string) json.RawMessage {
		return json.RawMessage(`{
			"part":{
				"id":"p1",
				"type":"tool",
				"callID":"c1",
				"tool":"bash",
				"state":{
					"status":"running",
					"title":"` + title + `",
					"output":` + strconv.Quote(output) + `,
					"error":` + strconv.Quote(errStr) + `
				}
			}
		}`)
	}

	s.HandleEvent("message.part.updated", makeToolFrame("hello stdout\nline2", "", "bash"))
	if len(h.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(h.events))
	}
	if h.events[0].Text != "hello stdout\nline2" {
		t.Errorf("event text = %q, want output text", h.events[0].Text)
	}

	// 4. Output longer than maxToolOutputChars (8000) is clipped with truncation marker.
	h.events = nil
	s.turnCleanup()
	longOut := strings.Repeat("x\n", 5000) // 10000 chars
	s.HandleEvent("message.part.updated", makeToolFrame(longOut, "", "bash"))
	if len(h.events) != 1 {
		t.Fatalf("expected 1 event for long output, got %d", len(h.events))
	}
	if !strings.HasSuffix(h.events[0].Text, "\n…[truncated]") {
		t.Errorf("long output missing truncation marker: %q", h.events[0].Text[len(h.events[0].Text)-30:])
	}
	if len(h.events[0].Text) > maxToolOutputChars+30 {
		t.Errorf("output length %d exceeds max cap + marker allowance", len(h.events[0].Text))
	}

	// 5. Error takes precedence over output.
	h.events = nil
	s.turnCleanup()
	s.HandleEvent("message.part.updated", makeToolFrame("some output", "fatal error", "bash"))
	if len(h.events) != 1 {
		t.Fatalf("expected 1 event for error precedence, got %d", len(h.events))
	}
	if h.events[0].Text != "fatal error" {
		t.Errorf("event text = %q, want error text 'fatal error'", h.events[0].Text)
	}

	// 6. Empty output falls back to title.
	h.events = nil
	s.turnCleanup()
	s.HandleEvent("message.part.updated", makeToolFrame("", "", "my bash title"))
	if len(h.events) != 1 {
		t.Fatalf("expected 1 event for title fallback, got %d", len(h.events))
	}
	if h.events[0].Text != "my bash title" {
		t.Errorf("event text = %q, want title fallback", h.events[0].Text)
	}
}
