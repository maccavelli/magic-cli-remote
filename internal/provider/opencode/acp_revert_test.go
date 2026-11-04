package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// revertHost is a session as undo, redo and the shell use it.
type revertHost struct {
	surfaceHost
	busy  bool
	state any

	mu      sync.Mutex
	emitted []event.Event
}

func (h *revertHost) Busy() bool     { return h.busy }
func (h *revertHost) SpecState() any { return h.state }
func (h *revertHost) Emit(ev event.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.emitted = append(h.emitted, ev)
}

func (h *revertHost) events() []event.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]event.Event(nil), h.emitted...)
}

// messageRow is a message as the list route answers it.
func messageRow(id, typ string, created int64) map[string]any {
	return map[string]any{"id": id, "type": typ, "time": map[string]any{"created": created}}
}

// revertRoutes answers the message list from users (newest first) and
// compactions, and records stage, clear and commit.
func revertRoutes(users, compactions []map[string]any) map[string]func(control.Request) control.Response {
	ok := func(control.Request) control.Response { return control.Response{Status: http.StatusNoContent} }
	return map[string]func(control.Request) control.Response{
		"/api/session/ses_1/message": func(r control.Request) control.Response {
			rows := users
			if r.Query.Get("type") == "compaction" {
				rows = compactions
			}
			var n int
			_ = json.Unmarshal([]byte(r.Query.Get("limit")), &n)
			if n < len(rows) {
				rows = rows[:n]
			}
			b, _ := json.Marshal(map[string]any{"data": rows, "cursor": map[string]any{}})
			return control.Response{Status: http.StatusOK, Body: b}
		},
		"/api/session/ses_1/revert/stage": jsonAnswer(map[string]any{"data": map[string]any{
			"messageID": "msg_x", "files": []map[string]any{{"file": "note.txt"}}}}),
		"/api/session/ses_1/revert":        ok,
		"/api/session/ses_1/revert/commit": ok,
	}
}

func newRevert(t *testing.T, routes map[string]func(control.Request) control.Response) (*acpSurfaces, *surfaceEngine, *revertHost) {
	t.Helper()
	s, e, base := newSurfaces(t, routes)
	return s, e, &revertHost{surfaceHost: base, state: newACPUndo()}
}

// staged is the message ids revert/stage was sent, in order.
func staged(e *surfaceEngine) []string {
	var out []string
	for _, r := range e.requests() {
		if r.Path == "/api/session/ses_1/revert/stage" {
			body, _ := r.Body.(map[string]any)
			out = append(out, body["messageID"].(string)+map[bool]string{true: "+files", false: ""}[body["files"] == true])
		}
	}
	return out
}

func sentPath(e *surfaceEngine, method, path string) bool {
	for _, r := range e.requests() {
		if r.Method == method && r.Path == path {
			return true
		}
	}
	return false
}

// TestACPUndoStagesTheNewestTurn: /undo stages a revert at the newest user
// message, files included; a second /undo reaches one turn further.
func TestACPUndoStagesTheNewestTurn(t *testing.T) {
	users := []map[string]any{messageRow("msg_u3", "user", 30), messageRow("msg_u2", "user", 20), messageRow("msg_u1", "user", 10)}
	s, e, h := newRevert(t, revertRoutes(users, nil))
	ctx := context.Background()
	text, err := s.undoLast(ctx, h)
	if err != nil || !strings.Contains(text, "/redo") || !strings.Contains(text, "last turn") ||
		!strings.Contains(text, "restored 1 file") {
		t.Fatalf("first undo: %q, %v", text, err)
	}
	text, err = s.undoLast(ctx, h)
	if err != nil || !strings.Contains(text, "last 2 turns") {
		t.Fatalf("second undo: %q, %v", text, err)
	}
	if got := staged(e); len(got) != 2 || got[0] != "msg_u3+files" || got[1] != "msg_u2+files" {
		t.Errorf("staged %v, want msg_u3 then msg_u2, with files", got)
	}
	if st := h.state.(*acpUndo); st.turns != 2 {
		t.Errorf("turns %d, want 2", st.turns)
	}
	for _, r := range e.requests() {
		if r.Directory != h.cwd {
			t.Errorf("%s %s in %q, want the session's directory", r.Method, r.Path, r.Directory)
		}
	}
}

// TestUndoTextSaysWhatWasRestored: OpenCode restores files only in a git
// repository, so an undo that restored none says so rather than claiming it.
func TestUndoTextSaysWhatWasRestored(t *testing.T) {
	for _, c := range []struct {
		turns, files int
		want         string
	}{
		{1, 0, "Undid the last turn and no file changes were recorded to restore (OpenCode records them only in a git repository)."},
		{1, 1, "Undid the last turn and restored 1 file."},
		{2, 3, "Undid the last 2 turns and restored 3 files."},
	} {
		if got := undoText(c.turns, c.files); !strings.HasPrefix(got, c.want) {
			t.Errorf("undoText(%d, %d) = %q, want it to start %q", c.turns, c.files, got, c.want)
		}
	}
}

// TestACPUndoRefusals: a running turn, no earlier turn, and a compaction after
// the turn are each refused, and nothing is staged.
func TestACPUndoRefusals(t *testing.T) {
	ctx := context.Background()
	users := []map[string]any{messageRow("msg_u1", "user", 10)}

	s, e, h := newRevert(t, revertRoutes(users, nil))
	h.busy = true
	if _, err := s.undoLast(ctx, h); !errors.Is(err, provider.ErrTurnBusy) || len(e.requests()) != 0 {
		t.Errorf("busy: %v, sent %v; want turn busy and nothing sent", err, e.paths())
	}

	s, e, h = newRevert(t, revertRoutes(nil, nil))
	if _, err := s.undoLast(ctx, h); err == nil || len(staged(e)) != 0 {
		t.Errorf("no turn: %v, staged %v", err, staged(e))
	}

	s, e, h = newRevert(t, revertRoutes(users, []map[string]any{messageRow("msg_c1", "compaction", 15)}))
	if _, err := s.undoLast(ctx, h); !errors.Is(err, errACPAcrossCompaction) || len(staged(e)) != 0 {
		t.Errorf("past a compaction: %v, staged %v", err, staged(e))
	}
	if h.state.(*acpUndo).turns != 0 {
		t.Error("a refused undo was counted")
	}

	// A compaction before the turn does not stand in the way.
	s, e, h = newRevert(t, revertRoutes(users, []map[string]any{messageRow("msg_c0", "compaction", 5)}))
	if _, err := s.undoLast(ctx, h); err != nil || len(staged(e)) != 1 {
		t.Errorf("compaction before the turn: %v, staged %v", err, staged(e))
	}
}

// TestACPRedoClears: /redo clears the staged revert and forgets the undo.
func TestACPRedoClears(t *testing.T) {
	s, e, h := newRevert(t, revertRoutes(nil, nil))
	h.state.(*acpUndo).turns = 2
	if err := s.unrevert(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if !sentPath(e, http.MethodDelete, "/api/session/ses_1/revert") || h.state.(*acpUndo).turns != 0 {
		t.Errorf("sent %v, turns %d; want DELETE …/revert and none", e.paths(), h.state.(*acpUndo).turns)
	}
	h.busy = true
	if err := s.unrevert(context.Background(), h); !errors.Is(err, provider.ErrTurnBusy) {
		t.Errorf("busy redo: %v", err)
	}
}

// TestACPBeforePromptMakesTheUndoPermanent: with an undo staged, the next
// prompt first commits it, then removes its turns; without one it sends
// nothing; a failed commit refuses the prompt and keeps the undo.
func TestACPBeforePromptMakesTheUndoPermanent(t *testing.T) {
	ctx := context.Background()
	s, e, h := newRevert(t, revertRoutes(nil, nil))
	if err := s.beforePrompt(ctx, h); err != nil || len(e.requests()) != 0 || len(h.events()) != 0 {
		t.Fatalf("no undo: %v, sent %v, emitted %v; want nothing", err, e.paths(), h.events())
	}

	h.state.(*acpUndo).turns = 2
	if err := s.beforePrompt(ctx, h); err != nil {
		t.Fatal(err)
	}
	evs := h.events()
	if !sentPath(e, http.MethodPost, "/api/session/ses_1/revert/commit") || len(evs) != 1 ||
		evs[0].Type != event.TypeTranscriptRemove || evs[0].RemoveTurns != 2 || h.state.(*acpUndo).turns != 0 {
		t.Errorf("sent %v, emitted %+v, turns %d; want the commit, a removal of 2 turns, and none left",
			e.paths(), evs, h.state.(*acpUndo).turns)
	}

	routes := revertRoutes(nil, nil)
	routes["/api/session/ses_1/revert/commit"] = func(control.Request) control.Response {
		return control.Response{Status: http.StatusConflict, Body: []byte(`{"_tag":"SessionBusyError"}`)}
	}
	s, _, h = newRevert(t, routes)
	h.state.(*acpUndo).turns = 1
	if err := s.beforePrompt(ctx, h); err == nil || !strings.Contains(err.Error(), "/redo") {
		t.Errorf("failed commit: %v, want the prompt refused, naming /redo", err)
	}
	if len(h.events()) != 0 || h.state.(*acpUndo).turns != 1 {
		t.Errorf("after a failed commit: emitted %v, turns %d; want nothing removed and the undo kept", h.events(), h.state.(*acpUndo).turns)
	}
}

// TestACPRevertAtAMessage: session.revert stages at the named prompt and
// counts the turns from it; a part, or a message that is not a recent prompt,
// is refused without staging.
func TestACPRevertAtAMessage(t *testing.T) {
	users := []map[string]any{messageRow("msg_u3", "user", 30), messageRow("msg_u2", "user", 20), messageRow("msg_u1", "user", 10)}
	ctx := context.Background()
	s, e, h := newRevert(t, revertRoutes(users, nil))
	if err := s.revert(ctx, h, "msg_u2", ""); err != nil {
		t.Fatal(err)
	}
	if got := staged(e); len(got) != 1 || got[0] != "msg_u2+files" || h.state.(*acpUndo).turns != 2 {
		t.Errorf("staged %v, turns %d; want msg_u2 and 2 turns", got, h.state.(*acpUndo).turns)
	}
	s, e, h = newRevert(t, revertRoutes(users, nil))
	if err := s.revert(ctx, h, "msg_unknown", ""); err == nil || len(staged(e)) != 0 {
		t.Errorf("unknown message: %v, staged %v", err, staged(e))
	}
	if err := s.revert(ctx, h, "msg_u2", "prt_1"); err == nil || len(e.requests()) > 1 {
		t.Errorf("a part: %v, sent %v", err, e.paths())
	}
}

// shellRoutes answers the shell route and the message it records.
func shellRoutes(post control.Response, msg any) map[string]func(control.Request) control.Response {
	b, _ := json.Marshal(map[string]any{"data": msg})
	return map[string]func(control.Request) control.Response{
		"/api/session/ses_1/shell": func(control.Request) control.Response { return post },
		"": func(r control.Request) control.Response {
			return control.Response{Status: http.StatusOK, Body: b}
		},
	}
}

// shellEngine routes any message read to the "" handler.
type shellEngine struct{ *surfaceEngine }

func (e shellEngine) Do(ctx context.Context, req control.Request) (control.Response, error) {
	if strings.HasPrefix(req.Path, "/api/session/ses_1/message/") {
		e.mu.Lock()
		e.sent = append(e.sent, req)
		h := e.routes[""]
		e.mu.Unlock()
		return h(req), nil
	}
	return e.surfaceEngine.Do(ctx, req)
}

func newShell(t *testing.T, post control.Response, msg any) (*acpSurfaces, *surfaceEngine, *revertHost) {
	t.Helper()
	e := &surfaceEngine{routes: shellRoutes(post, msg)}
	return &acpSurfaces{plane: shellEngine{e}}, e, &revertHost{surfaceHost: surfaceHost{cwd: t.TempDir(), sid: "ses_1"}, state: newACPUndo()}
}

// TestACPShell: the command runs with an id Magic makes and the shell's long
// bound, and shows as a bash tool call whose completion carries the output,
// failed for a non-zero exit, cut past 1 MiB.
func TestACPShell(t *testing.T) {
	ctx := context.Background()
	noContent := control.Response{Status: http.StatusNoContent}
	exit := func(code int, out string) any {
		return map[string]any{"type": "shell", "status": "exited", "exit": code, "output": map[string]any{"output": out, "truncated": false}}
	}

	s, e, h := newShell(t, noContent, exit(0, "alpha\n"))
	if err := s.shell(ctx, h, "cat a.txt"); err != nil {
		t.Fatal(err)
	}
	post := e.requests()[0]
	body, _ := post.Body.(map[string]any)
	if post.Path != "/api/session/ses_1/shell" || post.Timeout != shellTimeout || body["command"] != "cat a.txt" ||
		!strings.HasPrefix(body["id"].(string), "msg_") || post.Directory != h.cwd {
		t.Errorf("shell request %+v", post)
	}
	evs := h.events()
	if len(evs) != 2 || evs[0].Type != event.TypeToolCall || evs[0].ToolName != "bash" || evs[0].ToolKind != "execute" ||
		evs[0].Text != "cat a.txt" || evs[0].ToolID != body["id"] ||
		evs[1].Type != event.TypeToolUpdate || evs[1].Status != event.ToolStatusCompleted || evs[1].Text != "alpha\n" ||
		evs[1].ToolID != evs[0].ToolID {
		t.Errorf("events %+v", evs)
	}

	s, _, h = newShell(t, noContent, exit(2, "boom\n"))
	_ = s.shell(ctx, h, "false")
	if evs := h.events(); evs[1].Status != event.ToolStatusFailed || !strings.HasSuffix(evs[1].Text, "(exit 2)") {
		t.Errorf("a failing command: %+v", evs[1])
	}

	s, _, h = newShell(t, noContent, exit(0, strings.Repeat("x", acpShellOutputMax+10)))
	_ = s.shell(ctx, h, "seq 1 1000000")
	if evs := h.events(); len(evs[1].Text) > acpShellOutputMax+64 || !strings.Contains(evs[1].Text, "output cut") {
		t.Errorf("a long output: %d bytes", len(evs[1].Text))
	}

	s, _, h = newShell(t, control.Response{Status: http.StatusInternalServerError, Body: []byte(`{"_tag":"UnknownError"}`)}, nil)
	if err := s.shell(ctx, h, "ls"); err == nil {
		t.Error("a failed run reported success")
	}
	if evs := h.events(); len(evs) != 2 || evs[1].Status != event.ToolStatusFailed {
		t.Errorf("a failed run: %+v", evs)
	}
}

// TestACPShellRefusals: an invalid command or a running turn is refused, and
// nothing is sent or shown.
func TestACPShellRefusals(t *testing.T) {
	ctx := context.Background()
	s, e, h := newShell(t, control.Response{Status: http.StatusNoContent}, nil)
	if err := s.shell(ctx, h, "  "); !errors.Is(err, provider.ErrShellInvalid) {
		t.Errorf("empty command: %v", err)
	}
	h.busy = true
	if err := s.shell(ctx, h, "ls"); !errors.Is(err, provider.ErrTurnBusy) {
		t.Errorf("busy: %v", err)
	}
	if len(e.requests()) != 0 || len(h.events()) != 0 {
		t.Errorf("sent %v, shown %v; want nothing", e.paths(), h.events())
	}
}

// TestACPSpecWantsTheRealPath: OpenCode snapshots, and so undo's files, need
// the project's real path, so the Spec asks for it (0214 MADR, added at P6e).
func TestACPSpecWantsTheRealPath(t *testing.T) {
	if !acpSpec.RealCWD {
		t.Error("the OpenCode Spec does not ask for the real path; an undo in a symlinked project would restore no files")
	}
}

// TestACPProviderOffersUndoAndShell: newACP declares undo and revert, and the
// shell only when the operator permits it.
func TestACPProviderOffersUndoAndShell(t *testing.T) {
	replies := map[string]string{
		"initialize":  `{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.2.3"},"agentCapabilities":{"loadSession":true}}`,
		"session/new": `{"sessionId":"ses_1"}`,
	}
	for _, allow := range []bool{false, true} {
		p := newACP(acpagent.Config{AllowRemoteShell: allow}, nil, &surfaceEngine{})
		s, _ := acptest.Start(t, p, acptest.Options{Replies: replies})
		caps := s.Capabilities()
		if !caps.Has(provider.CapUndo) || !caps.Has(provider.CapRevert) || caps.Has(provider.CapShell) != allow {
			t.Errorf("allow shell %v: capabilities %+v", allow, caps)
		}
		// The session carries the Spec's undo state: /undo reaches the
		// control plane (which here answers nothing) rather than failing for
		// want of it.
		if _, err := s.(provider.UndoSession).UndoLast(context.Background()); err == nil || strings.Contains(err.Error(), "no undo state") {
			t.Errorf("/undo through the session: %v, want the control plane's answer, not a missing state", err)
		}
	}
}
