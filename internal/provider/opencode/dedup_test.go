package opencode

import (
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// statuses returns the Status of every session_status event, in order.
func statuses(evs []event.Event) []string {
	var out []string
	for _, ev := range evs {
		if ev.Type == event.TypeSessionStatus {
			out = append(out, ev.Status)
		}
	}
	return out
}

func usageReports(evs []event.Event) []event.Usage {
	var out []event.Usage
	for _, ev := range evs {
		if ev.Type == event.TypeUsage && ev.Usage != nil {
			out = append(out, *ev.Usage)
		}
	}
	return out
}

const busyFrame = `{"sessionID":"parent1","status":{"type":"busy"}}`

// TestRepeatedBusyAnnouncesRunningOnce is the daemon half of the MADR 0024
// §1.1 fix: session_status is not batchable on the phone, so every repeat
// forced an immediate transcript commit and defeated the client's own window.
func TestRepeatedBusyAnnouncesRunningOnce(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	for range 5 {
		s.HandleEvent("session.status", json.RawMessage(busyFrame))
	}

	if got := statuses(h.events); !slices.Equal(got, []string{"running"}) {
		t.Errorf("statuses = %v, want exactly one running", got)
	}
}

// A latch that never re-opened would leave the phone showing idle for the
// whole of the next turn.
func TestBusyAfterIdleAnnouncesRunningAgain(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.status", json.RawMessage(busyFrame))
	s.HandleEvent("session.status", json.RawMessage(busyFrame))
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"parent1","status":{"type":"idle"}}`))
	h.turnOn = true // a second turn begins
	s.HandleEvent("session.status", json.RawMessage(busyFrame))
	s.HandleEvent("session.status", json.RawMessage(busyFrame))

	want := []string{"running", "idle", "running"}
	if got := statuses(h.events); !slices.Equal(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
}

// A turn that ends via the error path (EndTurn already returned false, so
// turnCleanup is skipped) must still re-open the latch, or the next turn is
// invisible. emitStatus clearing on any non-running status is what covers it.
func TestErrorStatusReopensTheLatch(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.status", json.RawMessage(busyFrame))
	h.turnOn = false // EndTurn will report no active turn: no turnCleanup
	s.HandleEvent("session.error", json.RawMessage(
		`{"sessionID":"parent1","error":{"name":"BoomError","data":{"message":"boom"}}}`))
	s.HandleEvent("session.status", json.RawMessage(busyFrame))

	got := statuses(h.events)
	if len(got) == 0 || got[len(got)-1] != "running" {
		t.Errorf("statuses = %v, want a running re-announced after the error", got)
	}
}

// TestUnchangedUsageIsNotResent covers the other type that bypasses the
// client's batch window. OpenCode ships message.updated repeatedly per turn,
// usually with identical counts.
func TestUnchangedUsageIsNotResent(t *testing.T) {
	h := newRecorder()
	d := &httpDialect{
		log:           slog.Default(),
		contextLimits: map[string]int{"google/gemini-2.5-flash": 1000000},
	}
	s := d.NewSession(h).(*httpSession)

	frame := func(input int) json.RawMessage {
		return json.RawMessage(`{"info":{
			"id":"msg_1","role":"assistant","providerID":"google","modelID":"gemini-2.5-flash",
			"tokens":{"input":` + strconv.Itoa(input) + `,"output":0,"reasoning":0,
			"cache":{"read":0,"write":0}}}}`)
	}

	for range 4 {
		s.HandleEvent("message.updated", frame(100))
	}
	if got := usageReports(h.events); len(got) != 1 {
		t.Fatalf("got %d usage reports for 4 identical frames, want 1: %+v", len(got), got)
	}

	// A changed count must still get through — the indicator has to move.
	s.HandleEvent("message.updated", frame(250))
	got := usageReports(h.events)
	if len(got) != 2 {
		t.Fatalf("got %d usage reports, want 2 after a change: %+v", len(got), got)
	}
	if got[1].Used != 250 {
		t.Errorf("second report Used = %d, want 250", got[1].Used)
	}
}

// Every turn reports usage at least once, even when the numbers have not moved
// since the previous turn — otherwise a resumed context indicator stays blank.
func TestUsageIsResentAfterTurnCleanup(t *testing.T) {
	h := newRecorder()
	d := &httpDialect{
		log:           slog.Default(),
		contextLimits: map[string]int{"google/gemini-2.5-flash": 1000000},
	}
	s := d.NewSession(h).(*httpSession)

	frame := json.RawMessage(`{"info":{
		"id":"msg_1","role":"assistant","providerID":"google","modelID":"gemini-2.5-flash",
		"tokens":{"input":100,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}}`)

	s.HandleEvent("message.updated", frame)
	s.HandleEvent("message.updated", frame)
	if got := usageReports(h.events); len(got) != 1 {
		t.Fatalf("got %d reports before cleanup, want 1", len(got))
	}

	s.turnCleanup()
	s.HandleEvent("message.updated", frame)
	if got := usageReports(h.events); len(got) != 2 {
		t.Errorf("got %d reports, want a fresh one after turnCleanup", len(got))
	}
}
