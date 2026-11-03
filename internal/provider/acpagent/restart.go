package acpagent

import (
	"log/slog"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// A provider restarts its sessions' agents when something they loaded at
// start changes under them, such as a credential the agent read once (0214
// MADR D8). An agent runs as one process per session here, so the restart is
// per session: at its next idle point, the session posts the caller's notice,
// reports itself disconnected without an error, and ends its agent. The
// manager then closes it as it does any ended agent, and the resume path
// reattaches it with session/load, keeping the conversation (0214 PLAN P6a).

// track adds s to the provider's live sessions.
func (p *Provider) track(s *session) {
	p.liveMu.Lock()
	defer p.liveMu.Unlock()
	if p.live == nil {
		p.live = make(map[*session]struct{})
	}
	p.live[s] = struct{}{}
}

// untrack removes s from the provider's live sessions.
func (p *Provider) untrack(s *session) {
	p.liveMu.Lock()
	defer p.liveMu.Unlock()
	delete(p.live, s)
}

// RestartSessionsWhenIdle restarts every live session's agent at its next
// idle point, posting notice to each: at once when idle, otherwise when its
// turn and any queued prompt have ended.
func (p *Provider) RestartSessionsWhenIdle(notice string) {
	// The spare read the same state at its start: discard it, and any spare
	// warming now. The next EnsureWarm makes a fresh one.
	p.warmMu.Lock()
	spare := p.warm
	p.warm = nil
	p.warmGen++
	p.warmMu.Unlock()
	if spare != nil {
		spare.markClosedAndKill()
	}

	p.liveMu.Lock()
	sessions := make([]*session, 0, len(p.live))
	for s := range p.live {
		sessions = append(sessions, s)
	}
	p.liveMu.Unlock()
	for _, s := range sessions {
		s.restartWhenIdle(notice)
	}
}

// restartWhenIdle restarts s now when idle, or marks it for the end of its
// turn.
func (s *session) restartWhenIdle(notice string) {
	s.mu.Lock()
	if s.closed || s.disconnected {
		s.mu.Unlock()
		return
	}
	if s.prompting {
		s.restartNotice = notice
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.restartAgent(notice)
}

// restartIfPending runs a restart marked during the turn that just ended,
// unless another turn has already begun.
func (s *session) restartIfPending() {
	s.mu.Lock()
	notice := s.restartNotice
	if notice == "" || s.prompting {
		s.mu.Unlock()
		return
	}
	s.restartNotice = ""
	s.mu.Unlock()
	s.restartAgent(notice)
}

// restartAgent posts notice, reports the session disconnected, without the
// error an agent's own exit carries, and ends the agent.
func (s *session) restartAgent(notice string) {
	s.mu.Lock()
	if s.closed || s.disconnected {
		s.mu.Unlock()
		return
	}
	s.disconnected = true
	s.mu.Unlock()
	s.log.Info("acp: restarting the agent", slog.String("session_id", s.localID))
	s.Notice(notice)
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "disconnected",
	})
	s.killProc()
}
