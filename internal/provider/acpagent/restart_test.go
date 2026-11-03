package acpagent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// heldPrompts is an agent that answers the handshake and holds each prompt
// until the test answers it.
type heldPrompts struct {
	mu      sync.Mutex
	prompts []scriptRequest
}

func (h *heldPrompts) handle(r scriptRequest, _ func(string)) {
	switch r.Method {
	case "initialize":
		r.Answer(`{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}`)
	case "session/new":
		r.Answer(`{"sessionId":"agent-1"}`)
	case "session/prompt":
		h.mu.Lock()
		h.prompts = append(h.prompts, r)
		h.mu.Unlock()
	}
}

func (h *heldPrompts) answerNext(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		h.mu.Lock()
		if len(h.prompts) > 0 {
			r := h.prompts[0]
			h.prompts = h.prompts[1:]
			h.mu.Unlock()
			r.Answer(`{"stopReason":"end_turn"}`)
			return
		}
		h.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("no prompt reached the agent")
		}
	}
}

// restarted reports the notice and whether the session reported itself
// disconnected, from what it emitted since the last call.
func restarted(s provider.Session) (notice string, disconnected bool) {
	for _, ev := range startEvents(s) {
		switch {
		case ev.Type == event.TypeNotice:
			notice += ev.Text
		case ev.Type == event.TypeSessionStatus && ev.Status == "disconnected":
			disconnected = true
		case ev.Type == event.TypeError:
			notice += "ERROR:" + ev.Error
		}
	}
	return notice, disconnected
}

// TestRestartDiscardsTheSpare: the warm spare read the same state as the
// sessions at its start, so a restart discards it, and a spare that began
// warming before the restart is not kept when it lands.
func TestRestartDiscardsTheSpare(t *testing.T) {
	p := &Provider{log: slog.New(slog.DiscardHandler)}
	spare := &session{done: make(chan struct{}), events: make(chan event.Event, 1)}
	p.warm = spare
	before := p.warmGen
	p.RestartSessionsWhenIdle("restart")
	if p.warm != nil {
		t.Error("the spare survived the restart")
	}
	spare.mu.Lock()
	closed := spare.closed
	spare.mu.Unlock()
	if !closed {
		t.Error("the discarded spare was not closed")
	}
	if p.warmGen == before {
		t.Error("the restart left the warm generation as it was; a spare warming across it would be kept")
	}
}

// TestRestartSessionsWhenIdle pins 0214 PLAN P6a's restart: each live session
// of the provider ends its agent with the caller's notice and a disconnected
// status, and no error, so the resume path reattaches it; an idle one at once,
// a busy one when its turn ends, and a closed one not at all.
func TestRestartSessionsWhenIdle(t *testing.T) {
	p := New(Spec{ID: "vendor"}, Config{})
	idleAgent, busyAgent, closedAgent := &heldPrompts{}, &heldPrompts{}, &heldPrompts{}
	idle, err := startOnScript(t, p, provider.StartOptions{}, idleAgent.handle)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := startOnScript(t, p, provider.StartOptions{}, busyAgent.handle)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := startOnScript(t, p, provider.StartOptions{}, closedAgent.handle)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []provider.Session{idle, busy, closed} {
		startEvents(s)
	}
	_ = closed.Close(context.Background())
	if err := busy.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	startEvents(busy)

	p.RestartSessionsWhenIdle("A credential changed; the agent restarts to apply it.")

	if n, d := restarted(idle); !d || !strings.Contains(n, "A credential changed") || strings.Contains(n, "ERROR:") {
		t.Errorf("idle session: notice %q, disconnected %v; want the notice, disconnected, no error", n, d)
	}
	if n, d := restarted(busy); d {
		t.Errorf("busy session restarted under its turn (notice %q)", n)
	}
	busyAgent.answerNext(t)
	if n, d := restarted(busy); !d || !strings.Contains(n, "A credential changed") || strings.Contains(n, "ERROR:") {
		t.Errorf("busy session after its turn: notice %q, disconnected %v; want the notice, disconnected, no error", n, d)
	}
	if n, d := restarted(closed); d || n != "" {
		t.Errorf("closed session: notice %q, disconnected %v; want nothing", n, d)
	}
	p.liveMu.Lock()
	n := len(p.live)
	p.liveMu.Unlock()
	if n != 2 {
		t.Errorf("the provider tracks %d live sessions, want the 2 not closed", n)
	}
}
