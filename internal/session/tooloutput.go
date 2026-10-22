package session

import (
	"errors"
	"slices"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/output"
)

// ErrToolOutputNotFound is session.tool_output's answer for a tool whose
// output was never cut, or was evicted, or whose session is gone.
var ErrToolOutputNotFound = errors.New("tool output not found")

// ToolOutput returns the whole text and diffs of a tool event the manager cut
// for the phone. The device must own the session; reading does not claim it.
func (m *Manager) ToolOutput(sessionID, toolID, deviceID string) (string, []event.ToolDiff, error) {
	if err := m.Authorize(sessionID, deviceID, false); err != nil {
		return "", nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.sessions[sessionID]
	if e == nil {
		return "", nil, ErrToolOutputNotFound
	}
	out, ok := e.toolOutputs.get(toolID)
	if !ok {
		return "", nil, ErrToolOutputNotFound
	}
	return out.text, slices.Clone(out.diffs), nil
}

// cutToolEventLocked bounds a tool event to what the phone is shown, for every
// provider in one place (MADR 0208 D11). When it cuts the text or a diff, it
// keeps the whole of them for ToolOutput and marks the event truncated. A tool
// event with no tool id is cut but has nothing to fetch. An event carrying new
// content that needs no cut replaces any earlier cut of the same tool; a
// status-only update leaves it. Caller holds m.mu.
func (e *entry) cutToolEventLocked(ev *event.Event) {
	if ev.Type != event.TypeToolCall && ev.Type != event.TypeToolUpdate {
		return
	}
	whole := toolOutput{text: strings.TrimRight(ev.Text, " \t\r\n"), diffs: ev.Diffs}
	text, cut := output.Clip(ev.Text, output.MaxToolShown)
	var diffs []event.ToolDiff
	if len(ev.Diffs) > 0 {
		diffs = make([]event.ToolDiff, len(ev.Diffs))
		for i, d := range ev.Diffs {
			oldText, oldCut := cutDiffText(d.OldText)
			newText, newCut := cutDiffText(d.NewText)
			diffs[i] = event.ToolDiff{Path: d.Path, OldText: oldText, NewText: newText}
			cut = cut || oldCut || newCut
		}
	}
	hasContent := ev.Text != "" || len(ev.Diffs) > 0
	ev.Text, ev.Diffs = text, diffs
	switch {
	case cut && ev.ToolID != "":
		e.toolOutputs.put(ev.ToolID, whole)
		ev.Truncated = true
	case hasContent:
		e.toolOutputs.drop(ev.ToolID)
	}
}

// cutDiffText bounds one side of a diff. A file's text is left exactly as it
// is unless it is over the bound; only then is it cut, as Clip cuts.
func cutDiffText(s string) (string, bool) {
	if len(s) <= output.MaxToolShown {
		return s, false
	}
	return output.Clip(s, output.MaxToolShown)
}

// maxToolOutputBytes bounds the whole tool output one session keeps for
// session.tool_output: about 32 outputs at output.MaxToolText each.
const maxToolOutputBytes = 8 << 20

// toolOutputs keeps the whole text and diffs of a session's tool events, which
// the history ring holds only clipped to output.MaxToolShown (MADR 0208 D11).
// It is bounded by bytes, and evicts the oldest tool first. Guarded by m.mu,
// like every other entry field.
type toolOutputs struct {
	byID  map[string]toolOutput
	order []string // tool ids, oldest first
	bytes int
}

type toolOutput struct {
	text  string
	diffs []event.ToolDiff
}

func (o toolOutput) size() int {
	n := len(o.text)
	for _, d := range o.diffs {
		n += len(d.Path) + len(d.OldText) + len(d.NewText)
	}
	return n
}

// put records the whole output of tool id, replacing any earlier one, then
// evicts the oldest tools until the store is within budget. The output just
// put is never evicted, even when it alone is over budget.
func (s *toolOutputs) put(id string, out toolOutput) {
	s.drop(id)
	if s.byID == nil {
		s.byID = make(map[string]toolOutput)
	}
	s.byID[id] = out
	s.order = append(s.order, id)
	s.bytes += out.size()
	for s.bytes > maxToolOutputBytes && len(s.order) > 1 {
		s.drop(s.order[0])
	}
}

// drop forgets tool id.
func (s *toolOutputs) drop(id string) {
	out, ok := s.byID[id]
	if !ok {
		return
	}
	delete(s.byID, id)
	s.bytes -= out.size()
	for i, v := range s.order {
		if v == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// get returns the whole output of tool id.
func (s *toolOutputs) get(id string) (toolOutput, bool) {
	out, ok := s.byID[id]
	return out, ok
}
