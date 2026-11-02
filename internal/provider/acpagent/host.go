package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	acp "github.com/maccavelli/acp-go-sdk2"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Host is a session as a Spec's hooks see it: every hook a Spec registers
// receives one, so a Spec's package acts on its sessions without reaching into
// this one (MADR 0208, amendment of 2026-11-01).
//
// Every method is agent-neutral. One that names a single agent's concept would
// put back the coupling Host exists to remove, so a method is added only with a
// stated reason.
//
// A hook that runs on the SDK's notification loop keeps SessionUpdate's rule
// under OverflowBlock: it does not wait for a request or a $/cancel_request the
// agent sends later (MADR 0224 D5). AwaitPermission and AwaitAnswers wait for
// the user, so they are for extension-request handlers, which the SDK runs on
// their own goroutines.
type Host interface {
	// AgentSessionID is the agent's id for this session; "" before
	// session/new or session/load has answered.
	AgentSessionID() string
	// CWD is the session's working directory.
	CWD() string
	// Log is the session's logger.
	Log() *slog.Logger
	// Config is the provider's configuration.
	Config() Config
	// Loading reports whether session/load is replaying the conversation.
	Loading() bool
	// SpecState is the value the Spec's NewSessionState made for this
	// session, or nil when the Spec has none.
	SpecState() any

	// RawRequest sends one JSON-RPC request as given and decodes its result
	// into out. The SDK does not check what is sent this way against
	// initialize, so only a Spec's own vendor methods use it.
	RawRequest(ctx context.Context, method string, params, out any) error
	// CallExtension is RawRequest for an extension method, named in full as
	// the wire carries it. It is bounded by a timeout, and an agent that does
	// not have the method answers [provider.ErrNotImplemented].
	CallExtension(ctx context.Context, method string, params, out any) error

	// Emit publishes ev; an empty SessionID or Timestamp is filled in.
	Emit(ev event.Event)
	// Decline names an update Magic received and does not map, once per
	// session, at Debug.
	Decline(variant string)
	// Notice posts a notice to the conversation.
	Notice(text string)
	// NoteSubagent records one sub-agent in the panel and republishes it. A
	// finished one never reverts to running.
	NoteSubagent(id, name, task, status string)
	// ReportUsage publishes a usage report. A live one (replay false) marks
	// this turn as reported, so the prompt answer's usage is not counted
	// beside it.
	ReportUsage(u *event.Usage, replay bool)
	// UpdateCompaction folds one compaction's state, as ACP's
	// compaction_update does, and publishes it. A nil summary leaves the
	// summary as it was; errText is kept only for a failed one.
	UpdateCompaction(id string, status acp.CompactionStatus, summary *string, errText string)
	// NoteLimitText is an engine line that may report a provider limit: when
	// it classifies as one and a turn is running, the turn ends with the
	// limit card.
	NoteLimitText(line string)
	// EndTurnWithLimit ends the running turn with the limit card for
	// message, retrying at retryAt when the message itself names no time.
	// With no turn running it does nothing.
	EndTurnWithLimit(message string, retryAt time.Time)
	// UpdateMCPStatus replaces the session's MCP server list with what update
	// returns for the current one. update runs under the list's lock.
	UpdateMCPStatus(update func([]provider.MCPServerStatus) []provider.MCPServerStatus)
	// SetModelCatalog replaces the provider's model catalog.
	SetModelCatalog(cat picker.Catalog)

	// AwaitPermission shows req as a permission request and waits for the
	// user's choice: the option id, or cancelled when nobody chose.
	AwaitPermission(ctx context.Context, req event.Event) (optionID string, cancelled bool)
	// AwaitAnswers shows req as a question form and waits for the user's
	// answers, one label list per question in order, or cancelled.
	AwaitAnswers(ctx context.Context, req event.Event) (answers [][]string, cancelled bool)
}

var _ Host = (*session)(nil)

// ExtensionRequestHandler answers an extension request a Spec registered. Its
// result is sent as the answer; an error that is an *acp.RequestError is sent
// as it is.
type ExtensionRequestHandler func(ctx context.Context, h Host, params json.RawMessage) (any, error)

// Ops are the session operations a Spec offers beyond standard ACP. A nil
// field means the operation is not offered (MADR 0208 D2).
type Ops struct {
	Fork          func(ctx context.Context, h Host, opts provider.ForkOptions) (provider.ForkResult, error)
	Compact       func(ctx context.Context, h Host) error
	Rename        func(ctx context.Context, h Host, title string) error
	UndoLast      func(ctx context.Context, h Host) (string, error)
	RuntimeUsage  func(ctx context.Context, h Host) (provider.Result, error)
	RuntimeStatus func(ctx context.Context, h Host) (provider.Result, error)
	// ConfirmLimit checks a limit classified from prose against the agent's
	// own account data, on the turn goroutine. summary is appended to the
	// limit card; confirmed reports whether the data agreed.
	ConfirmLimit func(ctx context.Context, h Host, kind agenterr.Kind) (summary string, confirmed bool)
}

// InitializeInfo is what a Spec reads from initialize's `_meta`. Empty fields
// report nothing.
type InitializeInfo struct {
	// Version is the engine version, used when initialize sends no agentInfo.
	Version string
	// CurrentModel is the model the engine says it runs, before any session.
	CurrentModel string
	// DefaultAuthMethodID is logged at Debug.
	DefaultAuthMethodID string
	// Models, when non-nil, is the engine's model catalog.
	Models *picker.Catalog
}

// errNotOffered is the error an operation answers when the Spec offers none.
func errNotOffered(op string) error {
	return fmt.Errorf("%w: this agent offers no %s", provider.ErrNotImplemented, op)
}

// bindSpec gives s what its Spec registers, as Start does before the
// connection is built.
func (p *Provider) bindSpec(s *session) {
	s.extNotificationHandlers = p.spec.ExtensionNotifications
	s.metaHandlers = p.spec.MetaHandlers
	s.extRequestHandlers = p.spec.ExtensionRequests
	s.ops = p.spec.Ops
	if p.spec.NewSessionState != nil {
		s.specState = p.spec.NewSessionState()
	}
}

// Log implements [Host].
func (s *session) Log() *slog.Logger { return s.log }

// Config implements [Host].
func (s *session) Config() Config { return s.cfg }

// Loading implements [Host].
func (s *session) Loading() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loading
}

// SpecState implements [Host].
func (s *session) SpecState() any { return s.specState }

// CallExtension implements [Host].
//
// An agent publishes no list of its extension methods, so "supported" is
// answered the only way the wire allows: call it, and read a JSON-RPC
// method-not-found as "this build does not have it" rather than as a failure
// worth showing anyone.
func (s *session) CallExtension(ctx context.Context, method string, params, out any) error {
	if s.connOrNil() == nil {
		return errors.New("session is not connected")
	}
	callCtx, cancel := context.WithTimeout(ctx, extCallTimeout)
	defer cancel()
	if err := s.rawRequest(callCtx, method, params, out); err != nil {
		if acp.IsMethodNotFound(err) {
			return fmt.Errorf("%w: agent has no %s", provider.ErrNotImplemented, method)
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// Emit implements [Host].
func (s *session) Emit(ev event.Event) { s.emit(ev) }

// Decline implements [Host].
func (s *session) Decline(variant string) { s.declineUpdate(variant) }

// Notice implements [Host].
func (s *session) Notice(text string) {
	s.emit(event.Event{Type: event.TypeNotice, Text: text})
}

// NoteSubagent implements [Host].
func (s *session) NoteSubagent(id, name, task, status string) {
	s.noteSubagent(id, name, task, status)
}

// ReportUsage implements [Host].
func (s *session) ReportUsage(u *event.Usage, replay bool) {
	if u == nil {
		return
	}
	if !replay {
		s.mu.Lock()
		s.turnUsageReported = true
		s.mu.Unlock()
	}
	s.emit(event.Event{Type: event.TypeUsage, Usage: u})
}

// UpdateCompaction implements [Host]. Like compaction_update, it runs on the
// notification loop, which alone touches the compaction fold.
func (s *session) UpdateCompaction(id string, status acp.CompactionStatus, summary *string, errText string) {
	cid := acp.CompactionId(id)
	e, _ := s.compactions.entry(cid)
	e.status = string(status)
	if summary != nil {
		e.summary, e.truncated = e.summary[:0], false
		e.appendSummary(strings.TrimSpace(*summary))
	}
	if status == acp.CompactionStatusFailed {
		e.err = strings.TrimSpace(errText)
	}
	s.emitCompaction(cid, e, time.Now().UTC())
}

// NoteLimitText implements [Host].
func (s *session) NoteLimitText(line string) { s.noteEngineLogLine(line) }

// EndTurnWithLimit implements [Host]. It is noteEngineLogLine for a limit the
// agent reported structurally, so it needs no classifier's agreement, and its
// retry time travels to the limit card.
func (s *session) EndTurnWithLimit(message string, retryAt time.Time) {
	s.mu.Lock()
	if s.closed || !s.prompting || s.limitNotified {
		s.mu.Unlock()
		return
	}
	s.limitNotified = true
	s.limitRaw = message
	s.limitRetryAt = retryAt
	cancel := s.turnCancel
	s.mu.Unlock()
	s.log.Warn("agent provider limit reported",
		slog.String("session_id", s.localID),
		slog.String("text", message),
		slog.Time("retry_at", retryAt),
	)
	if cancel != nil {
		cancel()
		return
	}
	s.emitClassifiedTurnError(message)
}

// UpdateMCPStatus implements [Host].
func (s *session) UpdateMCPStatus(update func([]provider.MCPServerStatus) []provider.MCPServerStatus) {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	s.mcpStatus = update(s.mcpStatus)
}

// SetModelCatalog implements [Host].
func (s *session) SetModelCatalog(cat picker.Catalog) {
	if s.provider == nil {
		return
	}
	s.provider.catalogMu.Lock()
	s.provider.catalogCache = cat
	s.provider.catalogHas = true
	s.provider.catalogMu.Unlock()
}

// AwaitPermission implements [Host].
func (s *session) AwaitPermission(ctx context.Context, req event.Event) (string, bool) {
	permID := uuid.NewString()
	req.Type = event.TypePermission
	req.SessionID = s.localID
	req.Timestamp = time.Now().UTC()
	req.PermissionID = permID
	req.Status = "pending"
	res := s.awaitDecision(ctx, permID, req)
	return res.optionID, res.cancelled
}

// AwaitAnswers implements [Host].
func (s *session) AwaitAnswers(ctx context.Context, req event.Event) ([][]string, bool) {
	qID := uuid.NewString()
	req.Type = event.TypeQuestion
	req.SessionID = s.localID
	req.Timestamp = time.Now().UTC()
	req.QuestionID = qID
	req.Status = "pending"
	res := s.awaitAnswers(ctx, qID, req)
	return res.answers, res.cancelled
}
