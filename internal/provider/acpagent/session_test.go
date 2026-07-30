package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestAutoAllowPrefersAllowKind(t *testing.T) {
	resp := autoAllow(acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "deny", Name: "Deny", Kind: "reject_once"},
			{OptionId: "allow", Name: "Allow", Kind: "allow_once"},
		},
	})
	if resp.Outcome.Selected == nil {
		t.Fatal("expected selected")
	}
	if string(resp.Outcome.Selected.OptionId) != "allow" {
		t.Fatalf("got %s", resp.Outcome.Selected.OptionId)
	}
}

func TestContentText(t *testing.T) {
	if contentText(acp.TextBlock("hi")) != "hi" {
		t.Fatal("expected hi")
	}
	if contentText(acp.ContentBlock{}) != "" {
		t.Fatal("expected empty")
	}
}

// A Plan update must emit a single plan event carrying the mapped entries,
// with status/priority normalised to the fixed vocabulary.
func TestSessionUpdatePlanEmitsMappedEntries(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			Plan: &acp.SessionUpdatePlan{
				Entries: []acp.PlanEntry{
					{Content: "step one", Status: acp.PlanEntryStatusInProgress, Priority: acp.PlanEntryPriorityHigh},
					{Content: "step two", Status: acp.PlanEntryStatusPending, Priority: acp.PlanEntryPriorityLow},
					// Empty priority ("") is unknown and must fall back to medium.
					{Content: "step three", Status: acp.PlanEntryStatusCompleted},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePlan {
		t.Fatalf("want plan event, got %s", ev.Type)
	}
	if ev.SessionID != "local-1" {
		t.Fatalf("session id = %q", ev.SessionID)
	}
	want := []event.PlanEntry{
		{Content: "step one", Status: event.PlanStatusInProgress, Priority: event.PlanPriorityHigh},
		{Content: "step two", Status: event.PlanStatusPending, Priority: event.PlanPriorityLow},
		{Content: "step three", Status: event.PlanStatusCompleted, Priority: event.PlanPriorityMedium},
	}
	if len(ev.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(ev.Entries), len(want), ev.Entries)
	}
	for i, w := range want {
		if ev.Entries[i] != w {
			t.Fatalf("entry %d = %+v, want %+v", i, ev.Entries[i], w)
		}
	}
}

// PlanRemoved must emit a plan event whose entries are an empty (non-nil) list:
// the client interprets this as clearing the current plan (replace-semantics).
func TestSessionUpdatePlanRemovedEmitsEmptyPlan(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			PlanRemoved: &acp.SessionUpdatePlanRemoved{Id: "plan-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePlan {
		t.Fatalf("want plan event, got %s", ev.Type)
	}
	if ev.Entries == nil {
		t.Fatal("expected non-nil empty entries")
	}
	if len(ev.Entries) != 0 {
		t.Fatalf("expected empty entries, got %+v", ev.Entries)
	}
}

func recvEvent(t *testing.T, ch <-chan event.Event) event.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return event.Event{}
	}
}

// UserMessageChunk must not emit user_message: Prompt already does, and ACP
// echoes the same prompt (often in chunks), which duplicated UI bubbles.
func TestSessionUpdateIgnoresUserMessageChunk(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
				Content: acp.TextBlock("hello twice?"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-s.events:
		t.Fatalf("expected no event, got %+v", ev)
	case <-time.After(20 * time.Millisecond):
		// ok
	}
}

// Close with a pending permission must unblock the waiter and emit
// permission_resolved (Phase 2.5 / B.2).
func TestCloseUnblocksPendingPermission(t *testing.T) {
	s := &session{
		localID:    "l",
		agentID:    "a",
		events:     make(chan event.Event, 16),
		done:       make(chan struct{}),
		log:        slog.Default(),
		pending:    make(map[string]*permWaiter),
		procExited: true, // skip process kill
	}
	ch := make(chan permResult, 1)
	s.pending["p1"] = &permWaiter{ch: ch}

	// Close emits permission_resolved into events (cap 16) and unblocks ch.
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case res := <-ch:
		if !res.cancelled {
			t.Fatal("want cancelled result on waiter")
		}
	default:
		t.Fatal("permission waiter not unblocked by Close")
	}
	// Client-facing cancellation notice must land in the event stream.
	select {
	case ev := <-s.events:
		if ev.Type != event.TypePermissionResolved || ev.Status != event.PermissionStatusCancelled {
			t.Fatalf("event=%+v", ev)
		}
	default:
		t.Fatal("expected permission_resolved event on close")
	}
}

// A remote permission request that is never answered must time out and resolve
// as cancelled (fail safe) so the agent stops waiting, with a notice explaining
// why.
func TestPermissionTimeoutCancels(t *testing.T) {
	s := &session{
		localID: "l",
		events:  make(chan event.Event, 16),
		log:     slog.Default(),
		pending: make(map[string]*permWaiter),
		cfg:     Config{PermissionTimeout: 40 * time.Millisecond},
	}
	title := "Bash"
	resp, err := s.RequestPermission(t.Context(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "allow", Name: "Allow", Kind: "allow_once"},
		},
		ToolCall: acp.ToolCallUpdate{Title: &title},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Outcome.Cancelled == nil {
		t.Fatalf("expected cancelled outcome, got %+v", resp.Outcome)
	}

	var sawReq, sawNotice, sawResolved bool
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypePermission:
				sawReq = true
				if ev.Text != "Bash" {
					t.Fatalf("want detail fallback to title, got %q", ev.Text)
				}
			case event.TypeNotice:
				if strings.Contains(ev.Text, "timed out") {
					sawNotice = true
				}
			case event.TypePermissionResolved:
				if ev.Status == event.PermissionStatusCancelled {
					sawResolved = true
				}
			}
			if sawReq && sawNotice && sawResolved {
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if !sawReq || !sawNotice || !sawResolved {
		t.Fatalf("req=%v notice=%v resolved=%v", sawReq, sawNotice, sawResolved)
	}
}

// Control events must not be dropped when the event buffer is full of chunks,
// and buffered text must flush intact ahead of the boundary (MADR 0057).
func TestControlEventNotDroppedWhenBufferFull(t *testing.T) {
	// Positive window so the second chunk is held in chunkbuf (not dropped when
	// the 1-slot channel is full). Kill-switch (0) does not Unflush.
	win := time.Hour
	s := &session{
		localID:  "local-1",
		agentID:  "agent-1",
		events:   make(chan event.Event, 1),
		log:      slog.Default(),
		attached: true,
		done:     make(chan struct{}),
		cfg:      Config{StreamCoalesce: &win},
	}
	s.emit(event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "local-1",
		Text:      "fill",
	})
	// Second chunk is buffered in chunkbuf (leading edge already used the slot).
	s.emit(event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "local-1",
		Text:      "held",
	})

	done := make(chan struct{})
	go func() {
		s.emit(event.Event{
			Type:         event.TypePermissionResolved,
			SessionID:    "local-1",
			PermissionID: "p1",
			Status:       event.PermissionStatusResolved,
		})
		close(done)
	}()

	var assistant strings.Builder
	var received []event.Event
	var sawResolved bool
	deadline := time.After(2 * time.Second)
loop:
	for {
		select {
		case ev := <-s.events:
			received = append(received, ev)
			switch ev.Type {
			case event.TypeAssistantChunk:
				assistant.WriteString(ev.Text)
			case event.TypePermissionResolved:
				if ev.PermissionID != "p1" {
					t.Fatalf("want permission id p1, got %+v", ev)
				}
				sawResolved = true
				break loop
			}
		case <-deadline:
			t.Fatalf("control emit still blocked / dropped; received=%+v", received)
		}
	}

	<-done
	if !sawResolved {
		t.Fatalf("permission_resolved was never delivered; received=%+v", received)
	}
	if got := assistant.String(); !strings.Contains(got, "held") {
		t.Fatalf("chunk text was dropped: assistant=%q; delivery order=%+v", got, received)
	}
}

// Chunk text must not be dropped under a slow consumer: Unflush retries until
// the pump drains (MADR 0024 / 0057).
func TestChunksCoalesceUnderBackpressure(t *testing.T) {
	win := 5 * time.Millisecond
	s := &session{
		localID:  "local-1",
		agentID:  "agent-1",
		events:   make(chan event.Event, 2),
		log:      slog.Default(),
		attached: true,
		done:     make(chan struct{}),
		cfg:      Config{StreamCoalesce: &win},
	}
	var want strings.Builder
	for i := 0; i < 50; i++ {
		text := string(rune('a'+i%26)) + "x"
		want.WriteString(text)
		s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: "local-1", Text: text})
	}

	var got strings.Builder
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeAssistantChunk {
				got.WriteString(ev.Text)
			}
		default:
			if got.String() == want.String() {
				return
			}
			time.Sleep(2 * win)
		}
	}
	if got.String() != want.String() {
		t.Fatalf("text lost under backpressure\n got %q\nwant %q", got.String(), want.String())
	}
}

// Concurrency invariant for the permission resolve/cancel race (M14): whenever
// RespondPermission reports success, the waiter's outcome MUST be Selected, and
// whenever the outcome is Selected the answer MUST have been accepted — the two
// are mutually exclusive with a timeout cancellation. The pre-fix code read
// pending and then sent to the buffered channel as separate steps, so an answer
// landing as the timeout fired could return success while the waiter had already
// cancelled (the false success). The resolved latch closes that window.
//
// NOTE: this asserts the invariant under real contention; it is not a reliable
// reproducer of the original bug (its window is a few ns and not a data race, so
// neither iteration count nor -race forces it). It guards against a regression
// that widens the window materially, and documents the contract.
func TestRespondPermissionCancelRaceConsistency(t *testing.T) {
	for i := 0; i < 400; i++ {
		s := &session{
			localID:  "l",
			agentID:  "a",
			events:   make(chan event.Event, 64),
			done:     make(chan struct{}),
			log:      slog.Default(),
			pending:  make(map[string]*permWaiter),
			attached: true,
			cfg:      Config{PermissionTimeout: 1 * time.Millisecond},
		}

		permIDCh := make(chan string, 1)
		go func() {
			for {
				select {
				case ev := <-s.events:
					if ev.Type == event.TypePermission {
						select {
						case permIDCh <- ev.PermissionID:
						default:
						}
					}
				case <-s.done:
					return
				}
			}
		}()

		outcomeCh := make(chan acp.RequestPermissionResponse, 1)
		go func() {
			resp, _ := s.RequestPermission(context.Background(), acp.RequestPermissionRequest{
				Options: []acp.PermissionOption{{OptionId: "allow", Name: "Allow", Kind: "allow_once"}},
			})
			outcomeCh <- resp
		}()

		permID := <-permIDCh
		respErr := s.RespondPermission(context.Background(), permID, "allow", false)
		resp := <-outcomeCh

		selected := resp.Outcome.Selected != nil
		if respErr == nil && !selected {
			t.Fatalf("iter %d: RespondPermission succeeded but outcome was cancelled (false success)", i)
		}
		if selected && respErr != nil {
			t.Fatalf("iter %d: outcome Selected but RespondPermission errored: %v", i, respErr)
		}
		close(s.done)
	}
}

// fakeConn stands in for the ACP connection so watchConnClose can be driven
// deterministically (real conn death needs a subprocess + pipe EOF).
type fakeConn struct{ done chan struct{} }

func (f *fakeConn) Done() <-chan struct{} { return f.done }

func newWatchSession() *session {
	return &session{
		providerID: "grok",
		localID:    "l1",
		events:     make(chan event.Event, 8),
		done:       make(chan struct{}),
		attached:   true,
		pending:    map[string]*permWaiter{},
		log:        slog.Default(),
	}
}

func awaitDisconnected(t *testing.T, s *session, want bool, wait time.Duration) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeSessionStatus && ev.Status == "disconnected" {
				if !want {
					t.Fatal("unexpected disconnected status emitted")
				}
				return
			}
		case <-deadline:
			if want {
				t.Fatal("no disconnected status emitted")
			}
			return
		}
	}
}

// M11: when the SDK tears the connection down while the agent process is still
// alive (its notification queue overflowed behind our blocked handler),
// conn.Done fires but cmd.Wait does not. watchConnClose must still drive a
// clean disconnected teardown so the session cannot zombie.
func TestWatchConnCloseEmitsDisconnected(t *testing.T) {
	s := newWatchSession()
	fc := &fakeConn{done: make(chan struct{})}
	go s.watchConnClose(fc)
	close(fc.done) // SDK tore the connection down
	awaitDisconnected(t, s, true, 2*time.Second)
}

// A normal Close (done closed, session marked closed) fires conn.Done too, but
// must NOT produce a spurious disconnected event.
func TestWatchConnCloseSilentOnNormalClose(t *testing.T) {
	s := newWatchSession()
	fc := &fakeConn{done: make(chan struct{})}
	go s.watchConnClose(fc)
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	close(s.done)
	close(fc.done)
	awaitDisconnected(t, s, false, 200*time.Millisecond)
}

// The process-exit and connection-death watchers can both fire (e.g. an agent
// crash closes both); the disconnected pair must be emitted exactly once.
func TestSignalDisconnectedEmitsOnce(t *testing.T) {
	s := newWatchSession()
	s.signalDisconnected("first")
	s.signalDisconnected("second")

	var errs, statuses int
	drain := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypeError:
				errs++
			case event.TypeSessionStatus:
				if ev.Status == "disconnected" {
					statuses++
				}
			}
		case <-drain:
			if errs != 1 || statuses != 1 {
				t.Fatalf("want exactly one error+disconnected pair, got errs=%d statuses=%d", errs, statuses)
			}
			return
		}
	}
}

// M3: every agent fs callback is surfaced as a tool event so the access is
// visible/auditable, and unrestricted access (no FSRoots) is still allowed.
func TestFSAccessEmitsAuditEvent(t *testing.T) {
	s := &session{
		localID:  "l1",
		events:   make(chan event.Event, 8),
		done:     make(chan struct{}),
		attached: true,
		log:      slog.Default(),
	}
	if err := s.auditFSAccess("read_file", "/etc/hosts"); err != nil {
		t.Fatalf("unrestricted access must be allowed: %v", err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeToolCall || ev.ToolKind != "fs" || ev.ToolName != "read_file" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
	if ev.Status != "completed" || ev.Text != "/etc/hosts" {
		t.Fatalf("status=%q text=%q", ev.Status, ev.Text)
	}
}

// M3: with FSRoots set, a path inside the cwd/root is allowed and one outside
// is refused — and the refusal is still recorded as a failed audit event.
func TestFSConfinementEnforcesRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	s := &session{
		localID:  "l1",
		cwd:      root,
		events:   make(chan event.Event, 16),
		done:     make(chan struct{}),
		attached: true,
		log:      slog.Default(),
		cfg:      Config{FSRoots: []string{root}},
	}
	if err := s.auditFSAccess("read_file", filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("within-root read must be allowed: %v", err)
	}
	outPath := filepath.Join(outside, "secret.txt")
	if err := s.auditFSAccess("read_file", outPath); err == nil {
		t.Fatal("out-of-root read must be refused")
	}
	var sawFailed bool
	for drain := true; drain; {
		select {
		case ev := <-s.events:
			if ev.Text == outPath && ev.Status == "failed" {
				sawFailed = true
			}
		default:
			drain = false
		}
	}
	if !sawFailed {
		t.Fatal("expected a failed audit event for the refused path")
	}
}

// M3: a symlink inside a root that points outside it must not be treated as
// within the root (real-path check), while a not-yet-created file under the
// root is allowed.
func TestPathWithinRootsResolvesSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if pathWithinRoots(filepath.Join(link, "secret.txt"), []string{root}) {
		t.Fatal("a symlink escaping the root must not be treated as within it")
	}
	if !pathWithinRoots(filepath.Join(root, "sub", "new.txt"), []string{root}) {
		t.Fatal("a new file under the root must be allowed")
	}
}

func TestSetModelClosedSessionReturnsError(t *testing.T) {
	s := &session{closed: true}
	err := s.SetModel(context.Background(), "grok-4.5")
	if err == nil || !strings.Contains(err.Error(), "session closed") {
		t.Fatalf("expected session closed error, got: %v", err)
	}
}

func TestMCPStatusAndDiagnostics(t *testing.T) {
	s := &session{}
	HandleMCPStatus(t.Context(), s, json.RawMessage(`{"name":"server1","status":"ready"}`))

	status, err := s.MCPStatus(t.Context())
	if err != nil {
		t.Fatalf("MCPStatus: %v", err)
	}
	if len(status) != 1 || status[0].Name != "server1" || status[0].State != "ready" {
		t.Fatalf("MCPStatus = %+v, want server1 ready", status)
	}

	diag, err := s.Diagnostics(t.Context())
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diag.MCP) != 1 || diag.MCP[0].Name != "server1" {
		t.Fatalf("Diagnostics = %+v", diag)
	}
}
