package httpagent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// treeDialect returns fixed sid/props from DecodeFrame and implements ChildFrame.
type treeDialect struct {
	fakeDialect
	// decode is set per test to control streamOnce inputs when testing demux
	// helpers directly; DecodeFrame is unused for unit demux tests that call
	// lookupSessionLocked.
	parentIDFn func(json.RawMessage) string
}

func (d *treeDialect) ParentIDFromProps(props json.RawMessage) string {
	if d.parentIDFn != nil {
		return d.parentIDFn(props)
	}
	var probe struct {
		Info struct {
			ParentID string `json:"parentID"`
		} `json:"info"`
	}
	_ = json.Unmarshal(props, &probe)
	return probe.Info.ParentID
}

func TestLookupSessionBootstrapChild(t *testing.T) {
	d := &treeDialect{fakeDialect: fakeDialect{id: "test"}}
	p := NewWithLogger(d, Config{}, nil)
	parent, _ := newTestSession(p)
	parent.agentID = "parent1"
	if err := p.register(parent); err != nil {
		t.Fatal(err)
	}

	props := json.RawMessage(`{"info":{"id":"child1","parentID":"parent1"}}`)
	p.mu.Lock()
	got := p.lookupSessionLocked("child1", props)
	p.mu.Unlock()
	if got != parent {
		t.Fatalf("lookup = %v, want parent session", got)
	}
	p.mu.Lock()
	owner := p.childAliases["child1"]
	p.mu.Unlock()
	if owner != parent {
		t.Fatal("child1 not aliased to parent")
	}
	parent.mu.Lock()
	st := parent.treeNodes["child1"]
	parent.mu.Unlock()
	if st != NodeBusy {
		t.Fatalf("treeNodes[child1]=%q want busy", st)
	}
}

func TestLookupSessionBootstrapGrandchild(t *testing.T) {
	d := &treeDialect{fakeDialect: fakeDialect{id: "test"}}
	p := NewWithLogger(d, Config{}, nil)
	parent, _ := newTestSession(p)
	parent.agentID = "parent1"
	if err := p.register(parent); err != nil {
		t.Fatal(err)
	}
	// Pre-bind mid-level child as alias (simulates prior create).
	p.mu.Lock()
	p.childAliases["child1"] = parent
	p.mu.Unlock()
	parent.noteChildBound("child1")

	props := json.RawMessage(`{"info":{"id":"g1","parentID":"child1"}}`)
	p.mu.Lock()
	got := p.lookupSessionLocked("g1", props)
	p.mu.Unlock()
	if got != parent {
		t.Fatal("grandchild must fan into parent local session")
	}
	p.mu.Lock()
	if p.childAliases["g1"] != parent {
		t.Fatal("g1 alias missing")
	}
	p.mu.Unlock()
}

func TestLookupSessionUnknownNoParent(t *testing.T) {
	d := &treeDialect{fakeDialect: fakeDialect{id: "test"}}
	p := NewWithLogger(d, Config{}, nil)
	props := json.RawMessage(`{"info":{"id":"orphan","parentID":"missing"}}`)
	p.mu.Lock()
	got := p.lookupSessionLocked("orphan", props)
	p.mu.Unlock()
	if got != nil {
		t.Fatal("expected nil for unknown parent")
	}
}

func TestTryEndTurnIfTreeIdleParentOnly(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.treeNodes = map[string]NodeStatus{s.agentID: NodeIdle}
	s.mu.Unlock()
	if !s.TryEndTurnIfTreeIdle() {
		t.Fatal("parent-only idle should EndTurn")
	}
	s.mu.Lock()
	active := s.turnActive
	s.mu.Unlock()
	if active {
		t.Fatal("turn still active after tree idle")
	}
}

func TestTryEndTurnIfTreeIdleBlocksOnBusyChild(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.treeNodes = map[string]NodeStatus{
		s.agentID: NodeIdle,
		"child1":  NodeBusy,
	}
	s.mu.Unlock()
	if s.TryEndTurnIfTreeIdle() {
		t.Fatal("must not EndTurn while child busy")
	}
	s.mu.Lock()
	if !s.turnActive {
		t.Fatal("turn cleared while child busy")
	}
	s.mu.Unlock()
}

func TestTryEndTurnIfTreeIdleAfterChildIdle(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.treeNodes = map[string]NodeStatus{
		s.agentID: NodeIdle,
		"child1":  NodeBusy,
	}
	s.mu.Unlock()
	s.NoteNodeStatus("child1", NodeIdle)
	if !s.TryEndTurnIfTreeIdle() {
		t.Fatal("want EndTurn when all nodes idle")
	}
}

// confirmDialectSession optionally implements TreeIdleConfirmer.
type confirmDialectSession struct {
	fakeDialectSession
	mu         sync.Mutex
	stillBusy  []string
	discovered []string
	err        error
	calls      int
	lastKnown  []string
	lastParent string
}

func (c *confirmDialectSession) ConfirmTreeIdle(_ context.Context, parentID string, known []string) ([]string, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.lastParent = parentID
	c.lastKnown = append([]string(nil), known...)
	return c.stillBusy, c.discovered, c.err
}

func TestTryEndTurnIfTreeIdleConfirmBusy(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	conf := &confirmDialectSession{stillBusy: []string{"child-remote"}}
	s.ds = conf
	conf.h = s
	s.mu.Lock()
	s.turnActive = true
	s.treeNodes = map[string]NodeStatus{s.agentID: NodeIdle}
	s.mu.Unlock()
	if s.TryEndTurnIfTreeIdle() {
		t.Fatal("confirm reported busy child — must not EndTurn")
	}
	s.mu.Lock()
	if !s.turnActive {
		t.Fatal("turn cleared despite confirm busy")
	}
	if s.treeNodes["child-remote"] != NodeBusy {
		t.Fatalf("expected child-remote marked busy, got %q", s.treeNodes["child-remote"])
	}
	s.mu.Unlock()
	conf.mu.Lock()
	if conf.calls != 1 {
		t.Fatalf("confirm calls=%d", conf.calls)
	}
	conf.mu.Unlock()
}

func TestTryEndTurnIfTreeIdleConfirmOk(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	conf := &confirmDialectSession{}
	s.ds = conf
	conf.h = s
	s.mu.Lock()
	s.turnActive = true
	s.treeNodes = map[string]NodeStatus{s.agentID: NodeIdle}
	s.mu.Unlock()
	if !s.TryEndTurnIfTreeIdle() {
		t.Fatal("confirm idle should EndTurn")
	}
}

func TestBindChildAliasIdempotent(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	if err := p.register(s); err != nil {
		t.Fatal(err)
	}
	s.BindChildAlias("c1")
	s.BindChildAlias("c1")
	kids := p.childrenOf(s)
	if len(kids) != 1 || kids[0] != "c1" {
		t.Fatalf("children=%v", kids)
	}
	s.UnbindChildAlias("c1")
	if len(p.childrenOf(s)) != 0 {
		t.Fatal("expected no children after unbind")
	}
}

func TestUnregisterClearsChildAliases(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	if err := p.register(s); err != nil {
		t.Fatal(err)
	}
	s.BindChildAlias("c1")
	_ = s.Close(context.Background())
	p.mu.Lock()
	_, ok := p.childAliases["c1"]
	p.mu.Unlock()
	if ok {
		t.Fatal("child alias survived parent Close/unregister")
	}
}

func TestDispatchSetsEventAgentSessionID(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	var seen string
	s.ds = &handleHook{
		DialectSession: &fakeDialectSession{h: s},
		host:           s,
		on: func(h Host) {
			seen = h.EventAgentSessionID()
		},
	}
	s.dispatch("session.idle", json.RawMessage(`{}`), "child-x")
	if seen != "child-x" {
		t.Fatalf("EventAgentSessionID during HandleEvent = %q", seen)
	}
	if s.EventAgentSessionID() != "" {
		t.Fatal("EventAgentSessionID must clear after HandleEvent")
	}
}

// handleHook wraps a DialectSession and observes Host during HandleEvent.
type handleHook struct {
	DialectSession
	host Host
	on   func(Host)
}

func (h *handleHook) HandleEvent(typ string, props json.RawMessage) {
	if h.on != nil {
		h.on(h.host)
	}
	h.DialectSession.HandleEvent(typ, props)
}
