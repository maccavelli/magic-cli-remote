package codex

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// These are acceptance criterion A27 of PLAN 0163, added by the 2026-09-21
// deviation. Before it, a provider-level warning raised while no session was
// registered was discarded with no log and no trace — which covers engine start,
// initialize and the first resume, and is why a live deprecationNotice went
// unobserved and left A13 passing whether or not the migration had happened.

// TestEarlyProviderWarningIsHeldForTheNextSession proves the warning survives
// having nobody to deliver it to.
func TestEarlyProviderWarningIsHeldForTheNextSession(t *testing.T) {
	p := New(Config{})
	p.handleProviderNotification("deprecationNotice", json.RawMessage(
		`{"summary":"Full-history hydration is deprecated for paginated threads","details":null}`))

	held := p.drainEarlyWarnings()
	if len(held) != 1 {
		t.Fatalf("held %d warnings, want 1", len(held))
	}
	if held[0].kind != "deprecation" {
		t.Errorf("kind = %q, want deprecation", held[0].kind)
	}
	if held[0].message == "" {
		t.Error("the held warning lost its message, so the session would show an empty card")
	}

	// Draining is once-only: otherwise every new session replays the same warning
	// for the life of the process.
	if again := p.drainEarlyWarnings(); len(again) != 0 {
		t.Errorf("a second drain returned %d warnings, want 0", len(again))
	}
}

// TestThreadScopedWarningIsNotHeldForAnotherThread is the limit on the fix.
// Replaying a warning about thread A to whichever session registers next would
// attribute it to thread B, which is a worse outcome than losing it.
func TestThreadScopedWarningIsNotHeldForAnotherThread(t *testing.T) {
	p := New(Config{})
	p.handleProviderNotification("configWarning", json.RawMessage(
		`{"summary":"model_provider is unknown","threadId":"thread-nobody-has"}`))
	if held := p.drainEarlyWarnings(); len(held) != 0 {
		t.Errorf("held %d thread-scoped warnings, want 0: %+v", len(held), held)
	}
}

// TestEarlyWarningsAreBounded keeps a provider nobody is using from accumulating
// warnings without limit, and keeps the NEWEST, which describe the state the
// next session is about to run in.
func TestEarlyWarningsAreBounded(t *testing.T) {
	p := New(Config{})
	for i := 0; i < maxEarlyWarnings*2; i++ {
		p.handleProviderNotification("warning", json.RawMessage(
			fmt.Sprintf(`{"summary":"warning number %d"}`, i)))
	}
	held := p.drainEarlyWarnings()
	if len(held) != maxEarlyWarnings {
		t.Fatalf("held %d warnings, want the bound of %d", len(held), maxEarlyWarnings)
	}
	last := fmt.Sprintf("warning number %d", maxEarlyWarnings*2-1)
	if held[len(held)-1].message != last {
		t.Errorf("newest held warning = %q, want %q; the buffer must drop the oldest",
			held[len(held)-1].message, last)
	}
}

// TestLiveWarningIsNotAlsoBuffered: a warning that reached a session must not be
// held as well, or the next session shows it a second time.
func TestLiveWarningIsNotAlsoBuffered(t *testing.T) {
	p := New(Config{})
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		localID: "local-1",
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)
	p.mu.Lock()
	p.sessions["thread-x"] = s
	p.mu.Unlock()

	p.handleProviderNotification("guardianWarning", json.RawMessage(`{"summary":"guardian blocked a command"}`))

	var delivered int
	for _, ev := range drain(s) {
		if ev.Type == event.TypeCodexWarning {
			delivered++
		}
	}
	if delivered != 1 {
		t.Errorf("session received %d warning events, want 1", delivered)
	}
	if held := p.drainEarlyWarnings(); len(held) != 0 {
		t.Errorf("a delivered warning was also buffered (%d held), so it would be shown twice", len(held))
	}
}
