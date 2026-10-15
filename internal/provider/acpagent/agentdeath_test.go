package acpagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestAgentDeathMidTurnShowsOneError is PLAN 0184 B1 step 4 (E5, B19). When
// the agent process dies during a turn, two paths learn of it: the exit
// watcher, which calls signalDisconnected, and the turn, whose prompt fails
// because the connection ended. The phone must get one error for one death,
// whichever path is first.
func TestAgentDeathMidTurnShowsOneError(t *testing.T) {
	for _, watcherFirst := range []bool{true, false} {
		name := "turn first"
		if watcherFirst {
			name = "watcher first"
		}
		t.Run(name, func(t *testing.T) {
			s, a := newWireSession(t, `{}`)
			if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
				t.Fatal(err)
			}
			a.request("session/prompt", wireBudget)

			// The process dies: its stdout closes, and its exit watcher fires.
			if watcherFirst {
				s.signalDisconnected("the agent process exited")
			}
			_ = a.toConn.Close()
			waitTurnEnded(t, s)
			if !watcherFirst {
				s.signalDisconnected("the agent process exited")
			}

			var errs []string
			for len(s.events) > 0 {
				if ev := <-s.events; ev.Type == event.TypeError {
					errs = append(errs, ev.Error)
				}
			}
			if len(errs) != 1 {
				t.Fatalf("one agent death gave %d error events %q; want 1", len(errs), errs)
			}
			// Classified as a turn error, a dead agent read as a provider
			// outage ("The model provider had a server error").
			if strings.Contains(errs[0], "model provider") {
				t.Fatalf("an agent death was reported as %q", errs[0])
			}
		})
	}
}

// TestCloseEndsTheConnection is PLAN 0184 B1 step 4 (E5): Close ends the ACP
// connection itself, so the SDK's reader and writer stop and the agent's stdin
// closes when Close returns, not whenever the dying process's pipes do.
func TestCloseEndsTheConnection(t *testing.T) {
	s, _ := newWireSession(t, `{}`)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.conn.Done():
	case <-time.After(wireBudget):
		t.Fatal("the ACP connection was still open after Close returned")
	}
}

// waitTurnEnded waits for the turn goroutine to finish its cleanup.
func waitTurnEnded(t *testing.T, s *session) {
	t.Helper()
	deadline := time.Now().Add(wireBudget)
	for {
		s.mu.Lock()
		done := !s.prompting
		s.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn never ended after the agent's connection closed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
