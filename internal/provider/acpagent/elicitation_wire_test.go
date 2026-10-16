package acpagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestWireElicitationFormIsAnswered is PLAN 0191 L4: once Magic advertises
// elicitation.form, an agent's form sent over a real connection, through the
// SDK's capability gate as production configures it, reaches the phone as a
// question form, and the answer goes back as typed content (MADR 0191 D6).
// Without the advertisement the SDK refuses the form before the handler runs.
func TestWireElicitationFormIsAnswered(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	a.send(map[string]any{"jsonrpc": "2.0", "id": 41, "method": "elicitation/create",
		"params": acp.CreateElicitationRequest{Form: &acp.CreateElicitationForm{
			Message: "Which name?", Mode: "form",
			RequestedSchema: acp.ElicitationSchema{Type: "object", Required: []string{"name"},
				Properties: map[string]acp.ElicitationPropertySchema{
					"name": {String: &acp.ElicitationPropertySchemaString{Type: "string"}},
				}},
			Session: &acp.ElicitationSessionScope{SessionId: "agent-1"},
		}}})

	var req event.Event
	deadline := time.After(wireBudget)
	for req.QuestionID == "" {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeQuestion {
				req = ev
			}
		case <-deadline:
			m, _ := a.quiet(10 * time.Millisecond)
			t.Fatalf("no question_request within %s; the client wrote %+v", wireBudget, m)
		}
	}
	if err := s.RespondQuestion(context.Background(), req.QuestionID,
		provider.QuestionAnswers{"name": {"Ada"}}, false); err != nil {
		t.Fatal(err)
	}
	m := a.answerTo("41", wireBudget)
	if m.Error != nil {
		t.Fatalf("the form was answered with an error: %+v", m.Error)
	}
	var got map[string]any
	if err := json.Unmarshal(m.Result, &got); err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(map[string]any)
	if got["action"] != "accept" || content["name"] != "Ada" {
		t.Fatalf("result = %s, want accept with name Ada", m.Result)
	}
}

// TestWireElicitationFormKeepsTheAgentsOrder is PLAN 0191 L7: a form written
// over the wire with its fields out of name order, and its one required field
// last, reaches the phone in the order the agent wrote (MADR 0191, amendment
// of 2026-10-16). The params are raw JSON so the order on the wire is the
// test's, not an encoder's.
func TestWireElicitationFormKeepsTheAgentsOrder(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	a.send(map[string]any{"jsonrpc": "2.0", "id": 42, "method": "elicitation/create",
		"params": json.RawMessage(`{"mode":"form","message":"Order?",` +
			`"requestedSchema":{"type":"object","required":["mid"],"properties":{` +
			`"zeta":{"type":"string"},"alpha":{"type":"string"},"mid":{"type":"string"}}},` +
			`"sessionId":"agent-1"}`)})

	var req event.Event
	deadline := time.After(wireBudget)
	for req.QuestionID == "" {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeQuestion {
				req = ev
			}
		case <-deadline:
			m, _ := a.quiet(10 * time.Millisecond)
			t.Fatalf("no question_request within %s; the client wrote %+v", wireBudget, m)
		}
	}
	var order []string
	for _, it := range req.Questions {
		order = append(order, it.ID)
	}
	if want := []string{"zeta", "alpha", "mid"}; len(order) != len(want) ||
		order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("order = %v, want the wire order %v", order, want)
	}
	if err := s.RespondQuestion(context.Background(), req.QuestionID, nil, true); err != nil {
		t.Fatal(err)
	}
	if m := a.answerTo("42", wireBudget); m.Error != nil {
		t.Fatalf("the cancelled form was answered with an error: %+v", m.Error)
	}
}
