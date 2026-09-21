package codex

import (
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// maxEarlyWarnings bounds the warnings held for a session that does not exist
// yet. Small on purpose: this buffer exists to carry the handful of warnings a
// starting engine raises — a bad config value, a world-writable directory, a
// deprecation — not to become an unbounded log of a provider nobody is using.
// When it overflows the OLDEST is dropped, because the newest warning is the one
// describing the state the session is about to run in.
const maxEarlyWarnings = 16

// codexWarning is one provider-level warning, independent of who receives it.
type codexWarning struct {
	method   string
	threadID string
	kind     string
	message  string
}

// emitCodexWarning is the single place a warning becomes an event, so a warning
// replayed from the early buffer is indistinguishable on the wire from one
// delivered live.
func (s *session) emitCodexWarning(w codexWarning) {
	s.emit(event.Event{
		Type:           event.TypeCodexWarning,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		AgentSessionID: s.agentID,
		Codex: &event.CodexPayload{
			Key:    "warning:" + w.method + ":" + w.threadID,
			Kind:   w.kind,
			Status: "completed",
			Title:  "Codex warning",
			Text:   w.message,
		},
	})
}

// noteEarlyWarning holds a provider-global warning that found no session.
func (p *Provider) noteEarlyWarning(w codexWarning) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.earlyWarnings = append(p.earlyWarnings, w)
	if len(p.earlyWarnings) > maxEarlyWarnings {
		p.earlyWarnings = p.earlyWarnings[len(p.earlyWarnings)-maxEarlyWarnings:]
	}
}

// drainEarlyWarnings removes and returns the held warnings.
//
// Draining rather than copying is what stops every new session replaying the same
// warning: the first session to register owns them. A warning that matters to
// every session would have to be re-raised by the engine, which is the engine's
// decision to make, not ours.
func (p *Provider) drainEarlyWarnings() []codexWarning {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	held := p.earlyWarnings
	p.earlyWarnings = nil
	return held
}
