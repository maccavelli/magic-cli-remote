package acpagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// This file consumes the standard ACP `sessionCapabilities` block (MADR 0138
// F10). Where the standard surface exists it is the better answer than a
// vendor's own methods, because it is gated on what the agent advertised rather
// than on which vendor it is.
//
// Everything here is gated on what the agent advertised at `initialize`. An
// agent that offers nothing gets asked for nothing, and the corresponding
// optional interface reports itself unsupported the way it always did.

// extCallTimeout bounds one session-management extension or capability call.
// These are metadata operations against a local process; a slow one is a
// wedged agent, not a long computation.
const extCallTimeout = 20 * time.Second

// negotiation returns what initialize agreed on this session's connection. It
// is the SDK's own record, the one its gates check every message against, so
// reading it here cannot disagree with what the SDK will let through
// (MADR 0184 D4). False before initialize and on a closed session.
func (s *session) negotiation() (acp.Negotiation, bool) {
	conn := s.connOrNil()
	if conn == nil {
		return acp.Negotiation{}, false
	}
	return conn.Negotiated()
}

// allows reports whether the negotiated capabilities let this client send
// method.
func (s *session) allows(method string) bool {
	n, ok := s.negotiation()
	return ok && n.Allows(method) == nil
}

// supportsList reports whether the agent advertised `session/list`.
func (s *session) supportsList() bool { return s.allows(acp.AgentMethodSessionList) }

// supportsDelete reports whether the agent advertised `session/delete`.
func (s *session) supportsDelete() bool { return s.allows(acp.AgentMethodSessionDelete) }

// ListAgentSessions implements provider.AgentSessionLister over `session/list`.
//
// It pages to exhaustion via the cursor the response carries, bounded, so an
// agent that returns a cursor forever cannot pin the caller.
func (s *session) ListAgentSessions(ctx context.Context) ([]provider.AgentSessionMeta, error) {
	if !s.supportsList() {
		return nil, fmt.Errorf("%w: agent does not advertise session/list", provider.ErrNotImplemented)
	}
	conn := s.connOrNil()
	if conn == nil {
		return nil, errors.New("session is not connected")
	}

	callCtx, cancel := context.WithTimeout(ctx, extCallTimeout)
	defer cancel()

	// acp.AllSessions follows the cursor, and stops at any cursor the agent has
	// sent before, which is an agent looping, not a long list; extCallTimeout
	// bounds an agent that answers new cursors forever (H-6).
	out := make([]provider.AgentSessionMeta, 0, 32)
	for si, err := range acp.AllSessions(callCtx, conn, acp.ListSessionsRequest{}) {
		if errors.Is(err, acp.ErrRepeatedCursor) {
			s.log.Warn("session/list returned a repeating cursor; stopping",
				slog.Int("sessions", len(out)))
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("session/list: %w", err)
		}
		out = append(out, agentSessionMeta(si))
	}
	return out, nil
}

// agentSessionMeta is one listed session as Magic carries it. An updatedAt that
// is absent or not RFC 3339 is the zero time, which AgentSessionMeta serialises
// away: the alternative is a session dated to the year 1, which the picker once
// rendered as an age of about two thousand years (MADR 0046 L-13).
func agentSessionMeta(si acp.SessionInfo) provider.AgentSessionMeta {
	m := provider.AgentSessionMeta{ID: string(si.SessionId), CWD: si.Cwd, Title: derefString(si.Title)}
	if t, ok := si.Updated(); ok {
		m.UpdatedAt = t.UTC()
	}
	return m
}

// Purge implements provider.PurgeSession over `session/delete` where the agent
// advertises it, falling back to an ordinary close where it does not.
//
// The fallback matters and is not a workaround: Close is what Purge did for
// every ACP provider before this, so an agent without `delete` is no worse off,
// and one with it now actually removes its native conversation instead of
// leaving it behind. An agent that adds `delete` starts deleting with no change
// here, because the branch reads the wire rather than the vendor's name.
func (s *session) Purge(ctx context.Context) error {
	conn := s.connOrNil()
	if conn == nil || !s.supportsDelete() {
		if conn != nil {
			s.log.Debug("agent does not advertise session/delete; closing instead")
		}
		return s.Close(ctx)
	}

	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), extCallTimeout)
	agentID := s.agentID
	_, err := conn.DeleteSession(callCtx, acp.DeleteSessionRequest{
		SessionId: acp.SessionId(agentID),
	})
	cancel()
	if err != nil {
		// The local session still has to go: a failed native delete must not
		// leave a live process behind. Report the failure, close anyway.
		s.log.Warn("session/delete failed; closing the local session regardless",
			slog.String("agent_session_id", agentID),
			slog.String("err", err.Error()))
		if closeErr := s.Close(ctx); closeErr != nil {
			return closeErr
		}
		return fmt.Errorf("session/delete: %w", err)
	}
	return s.Close(ctx)
}

// connOrNil returns the live connection, or nil once the session is closed.
func (s *session) connOrNil() *acp.ClientSideConnection {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.conn
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// CurrentModel implements provider.ModelReporter.
//
// It answers with what the agent is actually running, which is not the same
// question as "what did the client ask for". MADR 0137's eighth amendment made
// "providers run on their own default model" the norm, so on the normal path
// the client asks for nothing — and every one of the seven turn-latency
// records in MADR 0138 carried no model as a result.
//
// Preference order: the model the agent applied to this session (harvested
// from session/new|load `_meta`), then the model it reported at `initialize`.
// Empty when it said neither, rather than guessed.
func (s *session) CurrentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := strings.TrimSpace(s.currentModelID); id != "" {
		return id
	}
	return strings.TrimSpace(s.engineModelID)
}

// Compact implements provider.CompactSession through the Spec's Ops.Compact
// (MADR 0138 F7; MADR 0208 D2).
func (s *session) Compact(ctx context.Context) error {
	if s.ops.Compact == nil {
		return errNotOffered("compaction")
	}
	return s.ops.Compact(ctx, s)
}

// Rename implements provider.RenameSession through the Spec's Ops.Rename.
func (s *session) Rename(ctx context.Context, title string) error {
	if s.ops.Rename == nil {
		return errNotOffered("rename")
	}
	return s.ops.Rename(ctx, s, title)
}

// shellOffered reports a direct shell: the Spec runs one and the operator
// permits it, as httpagent's does (MADR 0112 A9; 0214 MADR, added at P6e).
func (s *session) shellOffered() bool {
	return s.ops.Shell != nil && s.cfg.AllowRemoteShell
}

// Capabilities implements provider.Session: the operations the Spec's Ops
// offer, and a model or thinking switch where the agent has the config option
// or the Spec a fallback (MADR 0208 D1). It changes when the agent's config
// options do.
func (s *session) Capabilities() provider.Capabilities {
	spec := s.spec()
	s.mu.Lock()
	_, _, modelOption := selectOption(s.configOptions, acp.SessionConfigOptionCategoryModel)
	_, _, thinkingOption := selectOption(s.configOptions, acp.SessionConfigOptionCategoryThoughtLevel)
	s.mu.Unlock()
	return provider.Capabilities{
		provider.CapCompact:          s.ops.Compact != nil,
		provider.CapFork:             s.ops.Fork != nil,
		provider.CapRename:           s.ops.Rename != nil,
		provider.CapUndo:             s.ops.UndoLast != nil,
		provider.CapUsage:            s.ops.RuntimeUsage != nil,
		provider.CapStatus:           s.ops.RuntimeStatus != nil,
		provider.CapSetModel:         modelOption || spec.SetModelFallback != nil,
		provider.CapSetThinkingLevel: thinkingOption || spec.SetThinkingFallback != nil,
		provider.CapWorkspace:        s.ops.workspaceOffered(),
		provider.CapShell:            s.shellOffered(),
		provider.CapRevert:           s.ops.revertOffered(),
		provider.CapDiff:             s.ops.Diff != nil,
		provider.CapSkillRefresh:     s.ops.RefreshSkills != nil,
	}
}
