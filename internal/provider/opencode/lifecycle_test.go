package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// treeHost is a Host that records tree operations and EndTurn for PR2 tests.
type treeHost struct {
	captureHost
	mu         sync.Mutex
	nodes      map[string]httpagent.NodeStatus
	bound      []string
	unbound    []string
	turnOn     bool
	endViaTree int
	// eventAgentID is the origin the transport stamps on the frame currently
	// being handled; see EventAgentSessionID / asChild.
	eventAgentID string
}

func newTreeHost() *treeHost {
	h := &treeHost{
		nodes:  map[string]httpagent.NodeStatus{},
		turnOn: true,
	}
	h.model = ""
	return h
}

func (h *treeHost) AgentSessionID() string { return "parent1" }

func (h *treeHost) EndTurn() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.turnOn {
		return false
	}
	h.turnOn = false
	h.endTurns++
	return true
}

func (h *treeHost) BindChildAlias(id string) {
	h.mu.Lock()
	h.bound = append(h.bound, id)
	h.mu.Unlock()
}

func (h *treeHost) UnbindChildAlias(id string) {
	h.mu.Lock()
	h.unbound = append(h.unbound, id)
	h.mu.Unlock()
}

func (h *treeHost) NoteNodeStatus(id string, st httpagent.NodeStatus) {
	h.mu.Lock()
	if id == "" {
		id = h.AgentSessionID()
	}
	h.nodes[id] = st
	h.mu.Unlock()
}

func (h *treeHost) TryEndTurnIfTreeIdle() bool {
	h.mu.Lock()
	for _, st := range h.nodes {
		if httpagent.NodeBusyForEndTurn(st) {
			h.mu.Unlock()
			return false
		}
	}
	if !h.turnOn {
		h.mu.Unlock()
		return false
	}
	h.turnOn = false
	h.endTurns++
	h.endViaTree++
	h.mu.Unlock()
	return true
}

// EventAgentSessionID mirrors what httpagent's dispatch stamps for the frame
// being handled: empty for a parent frame, the child's id for a child frame.
// Tests that exercise child-origin suppression set eventAgentID first.
func (h *treeHost) EventAgentSessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.eventAgentID
}

// asChild runs fn with the frame origin stamped as sid, the way dispatch does.
func (h *treeHost) asChild(sid string, fn func()) {
	h.mu.Lock()
	h.eventAgentID = sid
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.eventAgentID = ""
		h.mu.Unlock()
	}()
	fn()
}

func TestSessionCreatedBindsChildAndCard(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.created", json.RawMessage(`{
		"info":{"id":"child1","parentID":"parent1","title":"Explore","agent":"general"}
	}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bound) != 1 || h.bound[0] != "child1" {
		t.Fatalf("bound=%v", h.bound)
	}
	if h.nodes["child1"] != httpagent.NodeBusy {
		t.Fatalf("child status=%q", h.nodes["child1"])
	}
	// Status goes to the panel, not the transcript: a subagents set, not a
	// synthetic tool card (MADR 0051 D8/D10).
	for _, ev := range h.events {
		if ev.Type == event.TypeToolCall || ev.Type == event.TypeToolUpdate {
			t.Fatalf("sub-agent status must not be a transcript tool card; got %+v", ev)
		}
	}
	var set []event.SubagentInfo
	for _, ev := range h.events {
		if ev.Type == event.TypeSubagents {
			set = ev.Subagents
		}
	}
	if len(set) != 1 {
		t.Fatalf("subagents=%v, want exactly one entry", set)
	}
	if set[0].ID != "child1" || set[0].Status != event.SubagentStatusRunning {
		t.Fatalf("entry=%+v, want child1/running", set[0])
	}
	// `agent` names the role; `title` is the task. Reading only the title (as
	// this did before) discarded the agent's identity entirely.
	if set[0].Name != "general" || set[0].Task != "Explore" {
		t.Fatalf("entry=%+v, want name=general task=Explore", set[0])
	}
}

func TestSessionIdleParentOnlyEndsTurn(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"parent1"}`))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endViaTree != 1 {
		t.Fatalf("endViaTree=%d want 1", h.endViaTree)
	}
	if h.turnOn {
		t.Fatal("turn still active")
	}
}

func TestSessionIdleBlockedByBusyChild(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeIdle
	h.nodes["child1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"parent1"}`))
	h.mu.Lock()
	if h.endViaTree != 0 || !h.turnOn {
		t.Fatalf("should not end turn: end=%d turnOn=%v", h.endViaTree, h.turnOn)
	}
	h.mu.Unlock()

	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"child1"}`))
	h.mu.Lock()
	if h.endViaTree != 1 {
		t.Fatalf("after child idle endViaTree=%d", h.endViaTree)
	}
	h.mu.Unlock()
}

func TestSessionDeletedCompletesCard(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("session.created", json.RawMessage(`{"info":{"id":"c1","parentID":"parent1","title":"T"}}`))
	s.HandleEvent("session.deleted", json.RawMessage(`{"info":{"id":"c1"}}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.unbound) != 1 || h.unbound[0] != "c1" {
		t.Fatalf("unbound=%v", h.unbound)
	}
	var last []event.SubagentInfo
	for _, ev := range h.events {
		if ev.Type == event.TypeSubagents {
			last = ev.Subagents
		}
	}
	if len(last) != 1 || last[0].ID != "c1" ||
		last[0].Status != event.SubagentStatusCompleted {
		t.Fatalf("subagents=%v, want c1 completed", last)
	}
}

func TestSessionStatusRetryNotice(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("session.status", json.RawMessage(`{
		"sessionID":"parent1",
		"status":{"type":"retry","attempt":2,"message":"rate limited","next":5}
	}`))
	found := false
	for _, ev := range h.events {
		if ev.Type == event.TypeNotice && strings.Contains(ev.Text, "Retry") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected retry notice")
	}
	if h.nodes["parent1"] != httpagent.NodeRetry {
		t.Fatalf("status=%q", h.nodes["parent1"])
	}
}

// Long next delay / quota message must end the turn with a classified limit
// card instead of leaving the phone on "running" while OpenCode sleeps.
func TestSessionStatusRetryHardLimitEndsTurn(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("session.status", json.RawMessage(`{
		"sessionID":"parent1",
		"status":{"type":"retry","attempt":1,"message":"Weekly usage limit reached. Resets in 4 days.","next":3600000}
	}`))
	var sawError, sawComplete bool
	for _, ev := range h.events {
		switch ev.Type {
		case event.TypeError:
			sawError = true
			if ev.ErrorKind != "quota" {
				t.Fatalf("ErrorKind=%q want quota (msg %q)", ev.ErrorKind, ev.Error)
			}
		case event.TypeTurnComplete:
			sawComplete = true
			if ev.StopReason != "error" {
				t.Fatalf("stopReason=%q", ev.StopReason)
			}
		}
	}
	if !sawError || !sawComplete {
		t.Fatalf("want TypeError+turn_complete, events=%+v", h.events)
	}
	if h.turnOn {
		t.Fatal("turn should have ended")
	}
}

func TestConfirmTreeIdleFiltersGlobalStatus(t *testing.T) {
	// Global status has foreign session busy; our tree is idle.
	var hits []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "/children"):
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/session/status"):
			_, _ = w.Write([]byte(`{
				"parent1":{"type":"idle"},
				"other-session":{"type":"busy"}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	h := newTreeHost()
	h.api = func(ctx context.Context, method, path string, body, out any) error {
		req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, nil)
		if err != nil {
			return err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if out != nil {
			return json.NewDecoder(res.Body).Decode(out)
		}
		return nil
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	busy, disc, err := s.ConfirmTreeIdle(context.Background(), "parent1", []string{"parent1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 0 {
		t.Fatalf("stillBusy=%v (foreign busy must be ignored)", busy)
	}
	_ = disc
}

func TestConfirmTreeIdleReportsBusyChild(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(strings.Split(r.URL.Path, "?")[0], "/children"):
			_, _ = w.Write([]byte(`[{"id":"child1"}]`))
		case strings.Contains(r.URL.Path, "/session/status"):
			_, _ = w.Write([]byte(`{
				"parent1":{"type":"idle"},
				"child1":{"type":"busy"}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	h := newTreeHost()
	h.api = func(ctx context.Context, method, path string, body, out any) error {
		req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, nil)
		if err != nil {
			return err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if out != nil {
			return json.NewDecoder(res.Body).Decode(out)
		}
		return nil
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	busy, disc, err := s.ConfirmTreeIdle(context.Background(), "parent1", []string{"parent1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(disc) != 1 || disc[0] != "child1" {
		t.Fatalf("discovered=%v", disc)
	}
	if len(busy) != 1 || busy[0] != "child1" {
		t.Fatalf("stillBusy=%v", busy)
	}
}
