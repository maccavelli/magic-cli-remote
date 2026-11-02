package acpagent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestWire is the agent end of a wire session, for the external test package:
// a Spec there is held to what acpagent exports (MADR 0208, amendment of
// 2026-11-01).
type TestWire struct{ a *wireAgent }

// NewTestWire builds a session for spec on a real connection whose peer is a
// wire agent, initialized as production initializes it.
func NewTestWire(t *testing.T, spec Spec) (provider.Session, *TestWire) {
	t.Helper()
	s, a := newWireSessionFor(t, "{}", &Provider{spec: spec})
	return s, &TestWire{a: a}
}

// Send writes one line to the client, exactly as given.
func (w *TestWire) Send(line string) { w.a.sendRaw(line) }

// AnswerTo returns the client's answer to the request with id: its result, or
// its error code.
func (w *TestWire) AnswerTo(id string) (result json.RawMessage, code int) {
	m := w.a.answerTo(id, wireBudget)
	if m.Error != nil {
		return nil, m.Error.Code
	}
	return m.Result, 0
}

// AnswerPrompt waits for the client's session/prompt and answers it with
// result, JSON.
func (w *TestWire) AnswerPrompt(result string) {
	req := w.a.request("session/prompt", wireBudget)
	var v map[string]any
	if err := json.NewDecoder(strings.NewReader(result)).Decode(&v); err != nil {
		w.a.t.Fatal(err)
	}
	w.a.respond(req, v)
}

// AwaitPrompt waits for the client's session/prompt and returns its id,
// leaving it unanswered.
func (w *TestWire) AwaitPrompt() json.RawMessage {
	return w.a.request("session/prompt", wireBudget).ID
}
