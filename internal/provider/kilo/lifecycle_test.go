package kilo

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// statusTestHost adds NoteNodeStatus tracking on top of captureHost — enough
// for the quota/retry lifecycle tests (MADR 0076 M4 #2) without building the
// full tree-scoped host apparatus (bound/unbound/TryEndTurnIfTreeIdle/
// ConfirmTreeIdle) those tests don't exercise; the tree-scoped half of
// opencode's lifecycle_test.go stays deferred with session_tree (MADR 0075
// PD2/Q7, MADR 0076 non-goals).
type statusTestHost struct {
	captureHost
	nodes map[string]httpagent.NodeStatus
}

func newStatusTestHost() *statusTestHost {
	return &statusTestHost{nodes: map[string]httpagent.NodeStatus{}}
}

func (h *statusTestHost) NoteNodeStatus(id string, st httpagent.NodeStatus) {
	h.mu.Lock()
	if id == "" {
		id = h.AgentSessionID()
	}
	h.nodes[id] = st
	h.mu.Unlock()
}

func TestSessionStatusRetryNotice(t *testing.T) {
	h := newStatusTestHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("session.status", json.RawMessage(`{
		"sessionID":"ses_test",
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
	if h.nodes["ses_test"] != httpagent.NodeRetry {
		t.Fatalf("status=%q", h.nodes["ses_test"])
	}
}

// Long next delay / quota message must end the turn with a classified limit
// card instead of leaving the phone on "running" while Kilo sleeps (parity
// with goose/codex/grok — MADR 0073).
func TestSessionStatusRetryHardLimitEndsTurn(t *testing.T) {
	h := newStatusTestHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("session.status", json.RawMessage(`{
		"sessionID":"ses_test",
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
	if h.endTurnCount() != 1 {
		t.Fatalf("EndTurn calls = %d, want 1", h.endTurnCount())
	}
}
