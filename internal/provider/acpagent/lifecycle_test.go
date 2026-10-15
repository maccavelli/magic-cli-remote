package acpagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// agentWithClose advertises session/close, as grok 1.0.13 does.
const agentWithClose = `{"sessionCapabilities":{"close":{}}}`

// TestCloseIsBoundedWhenTheAgentStopsReading is PLAN 0184 A2 step 8. Against
// an agent that has stopped reading, Close must still return, and go on to
// the process kill. v0.17.1 ignored a write's context (the SDK's B16), so
// Magic bounded session/close with its own timer; since acp-go-sdk2 v1.14.0 a
// send returns when its context ends, and closeTimeout's deadline alone is the
// bound this test holds (MADR 0184 amendment of 2026-10-15, E5).
func TestCloseIsBoundedWhenTheAgentStopsReading(t *testing.T) {
	s, a := newWireSession(t, agentWithClose)
	a.stopReading()

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- s.Close(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
		if elapsed := time.Since(start); elapsed > closeTimeout+time.Second {
			t.Fatalf("Close took %s, want at most about %s", elapsed, closeTimeout)
		}
	case <-time.After(closeTimeout + 5*time.Second):
		t.Fatalf("Close did not return within %s of an agent that stopped reading", closeTimeout+5*time.Second)
	}
}

// TestCloseSendsNothingWithAnEndedContext is PLAN 0184 A2 step 10. A close the
// caller abandoned must not reach the agent. v0.17.1 wrote the request anyway
// (its B9), so Close checked first; since acp-go-sdk2 v1.14.0 the SDK writes
// nothing for an ended context, and this test holds it to that (MADR 0184
// amendment of 2026-10-15, E5).
func TestCloseSendsNothingWithAnEndedContext(t *testing.T) {
	s, a := newWireSession(t, agentWithClose)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if m, wrote := a.quiet(500 * time.Millisecond); wrote {
		t.Fatalf("Close with an ended context put %q on the wire", m.Method)
	}
}

// TestCloseSendsSessionClose: with a live context, an agent that advertises
// close is sent one.
func TestCloseSendsSessionClose(t *testing.T) {
	s, a := newWireSession(t, agentWithClose)
	go func() { _ = s.Close(context.Background()) }()
	req := a.request("session/close", wireBudget)
	a.respond(req, map[string]any{})
}

// TestPromptSendsNothingWithAnEndedContext is step 10 for the turn path.
func TestPromptSendsNothingWithAnEndedContext(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.submitPrompt(ctx, []acp.ContentBlock{acp.TextBlock("hi")}); err == nil {
		t.Fatal("a prompt with an ended context returned no error")
	}
	if m, wrote := a.quiet(500 * time.Millisecond); wrote {
		t.Fatalf("a prompt with an ended context put %q on the wire", m.Method)
	}
}

// permissionRequestLine is one session/request_permission from the agent.
const permissionRequestLine = `{"jsonrpc":"2.0","id":30,"method":"session/request_permission","params":` +
	`{"sessionId":"agent-1","toolCall":{"toolCallId":"t1","title":"run"},` +
	`"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"}]}}`

// awaitPermissionCard waits for the phone's permission card.
func awaitPermissionCard(t *testing.T, s *session) string {
	t.Helper()
	deadline := time.After(wireBudget)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypePermission {
				return ev.PermissionID
			}
		case <-deadline:
			t.Fatal("no permission card was emitted")
		}
	}
}

// assertCardClosedAndAnswered checks the two outcomes of abandoning a pending
// permission request: the phone's card is closed as cancelled, and the agent's
// request is answered cancelled.
func assertCardClosedAndAnswered(t *testing.T, s *session, a *wireAgent, permID string) {
	t.Helper()
	deadline := time.After(wireBudget)
	for closed := false; !closed; {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypePermissionResolved && ev.PermissionID == permID {
				if ev.Status != event.PermissionStatusCancelled {
					t.Fatalf("the card was resolved %q, want cancelled", ev.Status)
				}
				closed = true
			}
		case <-deadline:
			t.Fatal("the phone's permission card was left open")
		}
	}
	m := a.answerTo("30", wireBudget)
	var res struct {
		Outcome struct {
			Outcome string `json:"outcome"`
		} `json:"outcome"`
	}
	if m.Error != nil || json.Unmarshal(m.Result, &res) != nil || res.Outcome.Outcome != "cancelled" {
		t.Fatalf("the agent's permission request was answered %+v %s, want outcome cancelled", m.Error, m.Result)
	}
}

// TestCancelClosesAPendingPermissionCard is PLAN 0184 A2 step 7 (MADR 0184 D8),
// over a real connection.
func TestCancelClosesAPendingPermissionCard(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	a.sendRaw(permissionRequestLine)
	permID := awaitPermissionCard(t, s)
	if err := s.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	assertCardClosedAndAnswered(t, s, a, permID)
}

// TestCloseClosesAPendingPermissionCard is PLAN 0184 A2 step 9. Magic's Close
// ends its pending permission requests and closes their phone cards. Since
// acp-go-sdk2 v1.14.0 the SDK also answers such a request "cancelled" on the
// wire (its B4), but the waiter and the card are Magic's own state, so this
// stays (MADR 0184 amendment of 2026-10-15, E5).
func TestCloseClosesAPendingPermissionCard(t *testing.T) {
	s, a := newWireSession(t, agentWithClose)
	a.sendRaw(permissionRequestLine)
	permID := awaitPermissionCard(t, s)
	go func() { _ = s.Close(context.Background()) }()
	assertCardClosedAndAnswered(t, s, a, permID)
}

// TestACancelledTurnEndsCancelledEitherWay: an acp-go-sdk2 agent answers a
// cancelled turn with stopReason "cancelled", where the Coder SDK's agents
// answered -32800 (MADR 0184 D8). Either way the phone's turn_complete must
// read cancelled.
func TestACancelledTurnEndsCancelledEitherWay(t *testing.T) {
	endings := map[string]func(context.Context) (acp.PromptResponse, error){
		"stopReason cancelled": func(context.Context) (acp.PromptResponse, error) {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		},
		"-32800": func(context.Context) (acp.PromptResponse, error) {
			return acp.PromptResponse{}, &acp.RequestError{Code: int(acp.ErrorCodeRequestCancelled), Message: "cancelled"}
		},
	}
	for name, end := range endings {
		t.Run(name, func(t *testing.T) {
			got := turnEvents(t, newQueueTestSession(), end)
			last := got[len(got)-1]
			if last.StopReason != "cancelled" || last.Status != "cancelled" {
				t.Fatalf("turn_complete = stop %q, status %q; want cancelled, cancelled", last.StopReason, last.Status)
			}
		})
	}
}
