package acpagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// purgeInBackground runs Purge, which waits on the agent's answers, so the test
// can play the agent meanwhile.
func purgeInBackground(s *session) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.Purge(context.Background()) }()
	return done
}

// purgeResult is Purge's result, or a failure if it has not returned within
// the wire budget.
func purgeResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(wireBudget):
		t.Fatal("Purge did not return")
		return nil
	}
}

// answerClose answers the session/close the session sends as it closes, and
// fails if none comes.
func answerClose(a *wireAgent) {
	a.t.Helper()
	a.respond(a.request("session/close", wireBudget), map[string]any{})
}

// TestPurgeDeletesWhenTheAgentAdvertisesDelete: an agent that advertises
// session/delete is asked to delete the agent's own session, and the session
// then closes (MADR 0184 amendment of 2026-10-29). No engine Magic runs
// advertises delete yet, so this is the only place the request runs.
func TestPurgeDeletesWhenTheAgentAdvertisesDelete(t *testing.T) {
	s, a := newWireSession(t, `{"sessionCapabilities":{"delete":{},"close":{}}}`)
	done := purgeInBackground(s)

	m := a.next(wireBudget)
	if m.Method != "session/delete" {
		t.Fatalf("first request = %q, want session/delete", m.Method)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "agent-1" {
		t.Fatalf("session/delete named %q, want the agent's session, agent-1", p.SessionID)
	}
	a.respond(m, map[string]any{})
	answerClose(a)

	if err := purgeResult(t, done); err != nil {
		t.Fatalf("Purge = %v, want nil", err)
	}
	if s.connOrNil() != nil {
		t.Fatal("the session is still open after Purge")
	}
}

// TestPurgeClosesWhenDeleteIsNotAdvertised: without delete, Purge is an
// ordinary close, which is what grok gets today.
func TestPurgeClosesWhenDeleteIsNotAdvertised(t *testing.T) {
	s, a := newWireSession(t, `{"sessionCapabilities":{"close":{}}}`)
	done := purgeInBackground(s)

	m := a.next(wireBudget)
	if m.Method != "session/close" {
		t.Fatalf("first request = %q, want session/close and no session/delete", m.Method)
	}
	a.respond(m, map[string]any{})

	if err := purgeResult(t, done); err != nil {
		t.Fatalf("Purge = %v, want nil", err)
	}
	if s.connOrNil() != nil {
		t.Fatal("the session is still open after Purge")
	}
}

// TestPurgeClosesAfterAFailedDelete: a failed native delete must not leave the
// session open, and the failure is still reported.
func TestPurgeClosesAfterAFailedDelete(t *testing.T) {
	s, a := newWireSession(t, `{"sessionCapabilities":{"delete":{},"close":{}}}`)
	done := purgeInBackground(s)

	m := a.request("session/delete", wireBudget)
	a.send(map[string]any{"jsonrpc": "2.0", "id": m.ID,
		"error": map[string]any{"code": -32603, "message": "the agent could not delete"}})
	answerClose(a)

	err := purgeResult(t, done)
	if err == nil || !strings.Contains(err.Error(), "session/delete") {
		t.Fatalf("Purge = %v, want the session/delete failure", err)
	}
	if s.connOrNil() != nil {
		t.Fatal("the session is still open after a failed delete")
	}
}
