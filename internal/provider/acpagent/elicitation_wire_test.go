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
