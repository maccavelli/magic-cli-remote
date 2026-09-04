package acpagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// This file consumes the standard ACP `sessionCapabilities` block that grok has
// been advertising and mcremote has never read.
//
// MADR 0138 F10 measured it: grok offers `list`, `resume` and `close`; goose
// offers `list`, `delete` and `close`; mcremote referenced **none** of them.
// F7's table then mapped grok's vendor `x.ai/session/*` methods onto the same
// gaps — but where the standard surface exists it is the better answer, because
// it is gated on what the agent advertised rather than on which vendor it is.
//
// This package is grok only. goose speaks ACP over a websocket through
// `internal/provider/acphttp`, which already lists and purges sessions by its
// own route; what changes here is that grok stops being the ACP provider with
// neither.
//
// Everything here is gated on what the agent advertised at `initialize`. An
// agent that offers nothing gets asked for nothing, and the corresponding
// optional interface reports itself unsupported the way it always did.

// extCallTimeout bounds one session-management extension or capability call.
// These are metadata operations against a local process; a slow one is a
// wedged agent, not a long computation.
const extCallTimeout = 20 * time.Second

// supportsList reports whether the agent advertised `session/list`.
func (s *session) supportsList() bool { return s.agentCaps.SessionCapabilities.List != nil }

// supportsDelete reports whether the agent advertised `session/delete`.
func (s *session) supportsDelete() bool { return s.agentCaps.SessionCapabilities.Delete != nil }

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

	const maxPages = 20
	out := make([]provider.AgentSessionMeta, 0, 32)
	var cursor *string
	for page := 0; page < maxPages; page++ {
		res, err := conn.ListSessions(callCtx, acp.ListSessionsRequest{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("session/list: %w", err)
		}
		for _, si := range res.Sessions {
			out = append(out, provider.AgentSessionMeta{
				ID:        string(si.SessionId),
				CWD:       si.Cwd,
				Title:     derefString(si.Title),
				UpdatedAt: parseACPTime(si.UpdatedAt),
			})
		}
		if res.NextCursor == nil || strings.TrimSpace(*res.NextCursor) == "" {
			return out, nil
		}
		// A cursor that does not move is an agent looping, not a long list.
		if cursor != nil && *res.NextCursor == *cursor {
			s.log.Warn("session/list returned a repeating cursor; stopping",
				slog.Int("sessions", len(out)))
			return out, nil
		}
		cursor = res.NextCursor
	}
	s.log.Warn("session/list did not terminate within the page bound",
		slog.Int("pages", maxPages), slog.Int("sessions", len(out)))
	return out, nil
}

// Purge implements provider.PurgeSession over `session/delete` where the agent
// advertises it, falling back to an ordinary close where it does not.
//
// The fallback matters and is not a workaround: Close is what Purge did for
// every ACP provider before this, so an agent without `delete` is no worse off,
// and one with it now actually removes its native conversation instead of
// leaving it behind. grok advertises `list`/`resume`/`close` and no `delete`,
// so today this closes; a later grok that adds `delete` starts deleting with no
// change here, because the branch reads the wire rather than the vendor's name.
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
	_, err := conn.UnstableDeleteSession(callCtx, acp.UnstableDeleteSessionRequest{
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

// callAgentExtension issues one grok `x.ai/*` extension request and decodes its
// result into out.
//
// Outbound, client to agent — the mirror of HandleExtensionMethod, which serves
// the requests grok makes of *us*. It adds the `_` the wire requires (grok's
// own dispatch table spells these without it) and delegates the transport to
// rawRequest, so there is one JSON-RPC path in this package rather than two.
//
// grok publishes no list of its seventy ext methods, so "supported" is answered
// the only way the wire allows: call it, and read a JSON-RPC method-not-found
// as "this build does not have it" rather than as a failure worth showing
// anyone.
func callAgentExtension(ctx context.Context, s *session, method string, params any, out any) error {
	if s.connOrNil() == nil {
		return errors.New("session is not connected")
	}
	callCtx, cancel := context.WithTimeout(ctx, extCallTimeout)
	defer cancel()

	if err := s.rawRequest(callCtx, "_"+method, params, out); err != nil {
		if isMethodNotFound(err) {
			return fmt.Errorf("%w: agent has no %s", provider.ErrNotImplemented, method)
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// isMethodNotFound reports whether err is JSON-RPC -32601.
func isMethodNotFound(err error) bool {
	var re *acp.RequestError
	if errors.As(err, &re) {
		return re.Code == -32601
	}
	return false
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// parseACPTime reads an optional RFC3339 timestamp. An unparseable or absent
// value yields the zero time, which AgentSessionMeta serialises away — the
// alternative is a session dated to the year 1, which the picker once rendered
// as an age of about two thousand years (MADR 0046 L-13).
func parseACPTime(p *string) time.Time {
	if p == nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*p))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// CurrentModel implements provider.ModelReporter.
//
// It answers with what the agent is actually running, which is not the same
// question as "what did the client ask for". MADR 0137's eighth amendment made
// "providers run on their own default model" the norm, so on the normal path
// the client asks for nothing — and every one of the seven grok turn-latency
// records in MADR 0138 carried no model as a result.
//
// Preference order: the model grok applied to this session (harvested from
// `x.ai/sessionDetail` on session/new|load), then the model it reported at
// `initialize`. Empty when it said neither, rather than guessed.
func (s *session) CurrentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := strings.TrimSpace(s.currentModelID); id != "" {
		return id
	}
	return strings.TrimSpace(s.engineModelID)
}

// Compact implements provider.CompactSession over grok's
// `x.ai/compact_conversation`.
//
// One of the seventy ext methods grok routes and mcremote did not call
// (MADR 0138 F7). Before this, `/compact` on a grok session answered "this
// agent can't compact its conversation" — which was mcremote's limitation, not
// grok's.
func (s *session) Compact(ctx context.Context) error {
	agentID := s.AgentSessionID()
	if agentID == "" {
		return errors.New("session has no agent session id")
	}
	return callAgentExtension(ctx, s, "x.ai/compact_conversation", map[string]any{
		"sessionId": agentID,
	}, nil)
}

// Rename implements provider.RenameSession over grok's `x.ai/session/rename`.
func (s *session) Rename(ctx context.Context, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("title is empty")
	}
	agentID := s.AgentSessionID()
	if agentID == "" {
		return errors.New("session has no agent session id")
	}
	return callAgentExtension(ctx, s, "x.ai/session/rename", map[string]any{
		"sessionId": agentID,
		"title":     title,
	}, nil)
}
