package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Callers must not invoke this after the child has been reaped (see
	// session.procExited): past that point the PID may be recycled and the
	// group signal would SIGKILL an unrelated process tree.
	_ = procutil.KillProcessGroup(cmd.Process)
	_, err := cmd.Process.Wait()
	return err
}

// session is one ACP-backed agent conversation.
type session struct {
	providerID provider.ID
	localID    string
	agentID    string
	cwd        string
	conn       *acp.ClientSideConnection
	cmd        *exec.Cmd
	terms      *terminalHost
	log        *slog.Logger
	events     chan event.Event
	// done is closed exactly once by Close. Senders blocked on a full events
	// buffer select on it, so teardown can never strand (or panic) a producer.
	// The events channel itself is never closed: closing it while a control
	// sender is parked in `events <- ev` is a guaranteed panic window.
	done chan struct{}
	cfg  Config

	mu     sync.Mutex
	closed bool
	// attached flips true when Start returns, i.e. once the manager's pump is
	// guaranteed to begin draining events. Before that (session/load replay,
	// the initial status emit) there is no consumer, so control-event delivery
	// must not block — it drops the oldest buffered event instead.
	attached bool
	// procExited is set by the exit watcher after cmd.Wait has reaped the
	// child. Once reaped, the PID may be recycled, so Close must not signal
	// the (now unrelated) process group.
	procExited bool
	// loading is true while ACP session/load runs: the agent replays the
	// whole prior conversation as ordinary updates then, and those events
	// must be marked Replay so the manager keeps them out of live broadcast.
	loading   bool
	prompting bool
	pending   map[string]chan permResult // permissionID -> result

	// lastActivity is the wall time (UnixNano) of the last agent output,
	// updated on every SessionUpdate and at turn start. Drives the stall
	// watchdog that tells the user a turn has gone quiet.
	lastActivity atomic.Int64

	// coalesced holds assistant/thought chunk text that a full event buffer
	// forced us to hold back, keyed by event type. Rather than dropping the
	// chunk (which punched holes in replies on slow links), we merge it into
	// the next same-type chunk or flush it before the next boundary event, so
	// no text is ever lost — only batched. Guarded by mu.
	coalesced map[event.Type]string
}

type permResult struct {
	optionID  string
	cancelled bool
}

var _ provider.Session = (*session)(nil)
var _ provider.PermissionSession = (*session)(nil)
var _ provider.CWDSession = (*session)(nil)
var _ acp.Client = (*session)(nil)

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return s.providerID }
func (s *session) AgentSessionID() string     { return s.agentID }
func (s *session) CWD() string                { return s.cwd }
func (s *session) Events() <-chan event.Event { return s.events }

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.prompting {
		s.mu.Unlock()
		return fmt.Errorf("prompt already in progress")
	}
	s.prompting = true
	s.mu.Unlock()

	var text strings.Builder
	blocks := make([]acp.ContentBlock, 0, len(parts))
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			text.WriteString(p.Text)
			blocks = append(blocks, acp.TextBlock(p.Text))
		}
	}

	s.emit(event.Event{
		Type:      event.TypeUserMessage,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Text:      text.String(),
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "running",
	})

	// The turn must survive the caller: ctx is typically the phone's WebSocket
	// request context, and a dropped mobile connection mid-turn must not abort
	// the agent's work (sessions are designed to outlive disconnects — that is
	// what history replay is for). Explicit Cancel() and process teardown
	// remain the ways to stop a turn.
	turnCtx := context.WithoutCancel(ctx)

	// Stall watchdog: a wedged agent otherwise pins "running" silently and
	// the user has no way to tell "thinking hard" from "dead".
	s.lastActivity.Store(time.Now().UnixNano())
	turnDone := make(chan struct{})
	if s.cfg.TurnStallNotice > 0 {
		go s.watchStall(turnDone)
	}

	go func() {
		defer func() {
			close(turnDone)
			s.mu.Lock()
			s.prompting = false
			s.mu.Unlock()
		}()

		resp, err := s.conn.Prompt(turnCtx, acp.PromptRequest{
			SessionId: acp.SessionId(s.agentID),
			Prompt:    blocks,
		})
		if err != nil {
			// Cancel/close should not flood the chat with scary error bubbles.
			if isBenignPromptErr(err) {
				s.emit(event.Event{
					Type:       event.TypeTurnComplete,
					SessionID:  s.localID,
					Timestamp:  time.Now().UTC(),
					StopReason: "cancelled",
					Status:     "cancelled",
				})
				s.emit(event.Event{
					Type:      event.TypeSessionStatus,
					SessionID: s.localID,
					Timestamp: time.Now().UTC(),
					Status:    "idle",
				})
				return
			}
			// Classify on the FULL error text — quota/rate-limit hints (and
			// their reset times) often live past the sanitizer's truncation.
			cls := agenterr.Classify(err.Error(), time.Now())
			s.emit(event.Event{
				Type:      event.TypeError,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Error:     sanitizeUserFacingErr(err),
				ErrorKind: string(cls.Kind),
				RetryAt:   cls.ResetAt,
			})
			s.emit(event.Event{
				Type:      event.TypeSessionStatus,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Status:    "error",
			})
			return
		}
		s.emit(event.Event{
			Type:       event.TypeTurnComplete,
			SessionID:  s.localID,
			Timestamp:  time.Now().UTC(),
			Status:     string(resp.StopReason),
			StopReason: string(resp.StopReason),
		})
		s.emit(event.Event{
			Type:      event.TypeSessionStatus,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Status:    "idle",
		})
	}()
	return nil
}

// watchStall emits a notice when a running turn has produced no output for
// cfg.TurnStallNotice, and again for each further silent period twice as
// long, so a genuinely stuck agent surfaces without spamming long tool runs.
func (s *session) watchStall(turnDone <-chan struct{}) {
	threshold := s.cfg.TurnStallNotice
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-turnDone:
			return
		case <-s.done:
			return
		case <-tick.C:
			quiet := time.Since(time.Unix(0, s.lastActivity.Load()))
			if quiet < threshold {
				continue
			}
			s.emit(event.Event{
				Type:      event.TypeNotice,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Text: fmt.Sprintf(
					"Still waiting — no output from the agent for %s. It may "+
						"be working on something long, or stuck: use Stop to "+
						"cancel the turn, or /reset to restart the agent.",
					quiet.Round(time.Second)),
			})
			// Back off so a long quiet run notices at t, 3t, 7t, …
			threshold *= 2
			s.lastActivity.Store(time.Now().UnixNano())
		}
	}
}

func (s *session) Cancel(ctx context.Context) error {
	s.mu.Lock()
	// Snapshot pending under lock, then release waiters without holding mu
	// (a blocked send must not stall other session ops).
	pending := s.pending
	s.pending = make(map[string]chan permResult)
	s.mu.Unlock()

	for _, ch := range pending {
		// Buffered (1): empty channel always accepts; full means already resolved.
		select {
		case ch <- permResult{cancelled: true}:
		default:
		}
	}

	return s.conn.Cancel(ctx, acp.CancelNotification{
		SessionId: acp.SessionId(s.agentID),
	})
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	_ = ctx
	s.mu.Lock()
	ch, ok := s.pending[permissionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown or expired permission %q", permissionID)
	}
	select {
	case ch <- permResult{optionID: optionID, cancelled: cancelled}:
		return nil
	default:
		return fmt.Errorf("permission %q already resolved", permissionID)
	}
}

// markClosedAndKill abandons a session whose setup failed (or a spare that
// was never used): suppresses further watcher events, wakes any parked
// deliverers, and SIGKILLs the process group. The exit watcher owns reaping.
func (s *session) markClosedAndKill() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	exited := s.procExited
	s.mu.Unlock()
	close(s.done)
	if !exited && s.cmd != nil && s.cmd.Process != nil {
		_ = procutil.KillProcessGroup(s.cmd.Process)
	}
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	// Snapshot-and-swap pending under the lock; do not mark closed yet so
	// permission_resolved can still be delivered on the control path before
	// done is closed (Phase 2.5 / B.2).
	pending := s.pending
	s.pending = make(map[string]chan permResult)
	exited := s.procExited
	s.mu.Unlock()

	// Unblock RequestPermission waiters first (buffered 1 — never hangs on
	// an empty channel; full means already resolved).
	for _, ch := range pending {
		select {
		case ch <- permResult{cancelled: true}:
		default:
		}
	}

	// Announce abandonment while the session is still "open" for emit/deliver
	// so clients unlock the composer. Bound each send so a fully stopped pump
	// cannot pin Close forever.
	for id := range pending {
		ev := s.permissionResolved(id, event.PermissionStatusCancelled)
		s.prepareEvent(&ev)
		select {
		case s.events <- ev:
		case <-time.After(200 * time.Millisecond):
			s.log.Debug("permission_resolved dropped on close; pump not draining",
				slog.String("permission_id", id),
				slog.String("session_id", s.localID),
			)
		}
	}

	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	// Wake any control sender parked on the full events buffer before doing
	// anything that could wait on them (CloseSession shares their goroutines).
	close(s.done)

	// Best-effort ACP close, bounded: a wedged agent that stops reading stdin
	// must not pin the caller — the process kill below is the real teardown.
	if s.conn != nil {
		closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, _ = s.conn.CloseSession(closeCtx, acp.CloseSessionRequest{
			SessionId: acp.SessionId(s.agentID),
		})
		cancel()
	}

	if s.terms != nil {
		s.terms.CloseAll()
	}
	if !exited && s.cmd != nil && s.cmd.Process != nil {
		_ = killProcessTree(s.cmd)
	}
	return nil
}

func (s *session) emit(ev event.Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.prepareEvent(&ev)

	if isHighFrequencyEvent(ev.Type) {
		// Coalesce-on-backpressure: never drop reply/thought text. Fold in any
		// text a previous full-buffer send left pending for this type, then try
		// to enqueue the whole run without blocking. If the buffer is still
		// full, keep it pending for the next chunk (or the pre-boundary flush) —
		// batched, but intact.
		if s.coalesced == nil {
			s.coalesced = make(map[event.Type]string, 2)
		}
		if p := s.coalesced[ev.Type]; p != "" {
			ev.Text = p + ev.Text
			delete(s.coalesced, ev.Type)
		}
		select {
		case s.events <- ev:
		default:
			s.coalesced[ev.Type] = ev.Text
		}
		s.mu.Unlock()
		return
	}

	// Any non-chunk event is a boundary in the stream: flush accumulated chunk
	// text first (blocking, so the tail of a reply always lands before the
	// turn_complete that follows it), then deliver this event.
	flush := s.drainCoalescedLocked()
	control := isControlEvent(ev.Type)
	s.mu.Unlock()
	for _, fe := range flush {
		s.deliver(fe, true)
	}
	s.deliver(ev, control)
}

// drainCoalescedLocked removes and returns any pending coalesced chunk text as
// deliverable events, in a stable order (assistant reply text before thoughts).
// Caller holds s.mu.
func (s *session) drainCoalescedLocked() []event.Event {
	if len(s.coalesced) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]event.Event, 0, len(s.coalesced))
	for _, ty := range []event.Type{event.TypeAssistantChunk, event.TypeThoughtChunk} {
		if txt := s.coalesced[ty]; txt != "" {
			out = append(out, event.Event{
				Type:      ty,
				SessionID: s.localID,
				Timestamp: now,
				Text:      txt,
			})
		}
	}
	s.coalesced = nil
	return out
}

// emitLocked is emit for callers already holding s.mu.
// Control events may temporarily unlock to block-send without deadlocking the
// session consumer (R5=A: never drop control events).
func (s *session) emitLocked(ev event.Event) {
	if s.closed {
		return
	}
	s.prepareEvent(&ev)
	control := isControlEvent(ev.Type)
	if !control {
		select {
		case s.events <- ev:
		default:
			s.log.Warn("dropping event; slow consumer",
				slog.String("type", string(ev.Type)),
				slog.String("session_id", s.localID),
			)
		}
		return
	}
	// Try non-blocking first while still holding the lock.
	select {
	case s.events <- ev:
		return
	default:
	}
	// Channel full: unlock, block until delivered or session closes, re-lock.
	s.mu.Unlock()
	s.deliver(ev, true)
	s.mu.Lock()
}

func (s *session) prepareEvent(ev *event.Event) {
	// Skip agent_session_id on high-frequency chunks to cut wire noise;
	// include it on status/tool/permission/turn events for resume/debug.
	if ev.AgentSessionID == "" && !isHighFrequencyEvent(ev.Type) {
		ev.AgentSessionID = s.agentID
	}
	// Callers hold s.mu (or run before/after any concurrent emitter exists).
	if s.loading {
		ev.Replay = true
	}
}

// deliver sends ev. Control events block until the consumer receives them or
// the session ends (done closed). Best-effort events may be dropped when the
// buffer is full.
func (s *session) deliver(ev event.Event, control bool) {
	if !control {
		select {
		case s.events <- ev:
		default:
			s.log.Warn("dropping event; slow consumer",
				slog.String("type", string(ev.Type)),
				slog.String("session_id", s.localID),
			)
		}
		return
	}
	s.mu.Lock()
	closed := s.closed
	attached := s.attached
	s.mu.Unlock()
	if closed {
		return
	}
	if !attached {
		// Inside Start there is no consumer yet (session/load replays the whole
		// conversation before the manager pump exists). Blocking here deadlocks
		// Start — and with it every session create, since the manager serializes
		// creates. Keep the newest events by dropping the oldest instead.
		for {
			select {
			case s.events <- ev:
				return
			default:
			}
			select {
			case <-s.events:
			default:
			}
		}
	}
	// Control path (R5=A): never drop once a consumer is attached; done
	// unblocks us if the session is torn down while we wait.
	select {
	case s.events <- ev:
	case <-s.done:
	}
}

// permissionResolved builds the terminal event for a permission request, so a
// client never keeps its composer locked on a request that will never answer.
func (s *session) permissionResolved(permID, status string) event.Event {
	return event.Event{
		Type:         event.TypePermissionResolved,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permID,
		Status:       status,
	}
}

func isHighFrequencyEvent(t event.Type) bool {
	switch t {
	case event.TypeAssistantChunk, event.TypeThoughtChunk:
		return true
	default:
		return false
	}
}

// isControlEvent reports events that must not be dropped under back-pressure
// (permissions, status, turn lifecycle). Chunks may still drop. Notices are
// control too: they carry the *reason* for a state change (e.g. why a
// permission auto-cancelled), which matters most exactly when the client is
// slow enough to cause drops.
func isControlEvent(t event.Type) bool {
	switch t {
	case event.TypeSessionStatus,
		event.TypePermission,
		event.TypePermissionResolved,
		event.TypeTurnComplete,
		event.TypeError,
		event.TypeNotice,
		// Tool events are state machines on the client: dropping the terminal
		// "completed"/"failed" update leaves a spinner running forever, which
		// reads as a hang. Their rate is bounded by tool executions, so
		// blocking delivery is safe.
		event.TypeToolCall,
		event.TypeToolUpdate,
		event.TypeUserMessage:
		return true
	default:
		return false
	}
}

// --- acp.Client implementation ---

func (s *session) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	u := params.Update
	now := time.Now().UTC()
	s.lastActivity.Store(now.UnixNano())
	switch {
	case u.AgentMessageChunk != nil:
		// Whitespace-only chunks are real content mid-message: token-granular
		// streams deliver paragraph breaks as standalone "\n\n" chunks, and
		// trimming them jammed paragraphs together. Only fully empty chunks
		// are noise (the client skips whitespace that would OPEN a message).
		text := contentText(u.AgentMessageChunk.Content)
		if text == "" {
			return nil
		}
		s.emit(event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: s.localID,
			Timestamp: now,
			Text:      text,
		})
	case u.AgentThoughtChunk != nil:
		text := contentText(u.AgentThoughtChunk.Content)
		if text == "" {
			return nil
		}
		s.emit(event.Event{
			Type:      event.TypeThoughtChunk,
			SessionID: s.localID,
			Timestamp: now,
			Text:      text,
		})
	case u.UserMessageChunk != nil:
		// Skip: Prompt() already emits a single user_message with the full
		// text. ACP often echoes the same prompt as UserMessageChunk(s),
		// which would duplicate bubbles in the client.
		s.log.Debug("ignoring acp user_message_chunk (already emitted on prompt)",
			slog.String("session_id", s.localID),
		)
	case u.ToolCall != nil:
		tc := u.ToolCall
		title := strings.TrimSpace(tc.Title)
		s.emit(event.Event{
			Type:      event.TypeToolCall,
			SessionID: s.localID,
			Timestamp: now,
			ToolID:    string(tc.ToolCallId),
			ToolName:  firstNonEmpty(title, string(tc.Kind), "tool"),
			ToolKind:  string(tc.Kind),
			Status:    string(tc.Status),
			// Human summary only — never dump rawInput/rawOutput JSON into chat.
			Text: summarizeToolContent(tc.Content, tc.RawInput, nil, 400),
		})
	case u.ToolCallUpdate != nil:
		tu := u.ToolCallUpdate
		status := ""
		if tu.Status != nil {
			status = string(*tu.Status)
		}
		title := ""
		if tu.Title != nil {
			title = strings.TrimSpace(*tu.Title)
		}
		kind := ""
		if tu.Kind != nil {
			kind = string(*tu.Kind)
		}
		s.emit(event.Event{
			Type:      event.TypeToolUpdate,
			SessionID: s.localID,
			Timestamp: now,
			ToolID:    string(tu.ToolCallId),
			ToolName:  firstNonEmpty(title, "tool"),
			ToolKind:  kind,
			Status:    status,
			Text:      summarizeToolContent(tu.Content, tu.RawInput, tu.RawOutput, 600),
		})
	case u.AvailableCommandsUpdate != nil:
		// Forward ACP slash commands so the phone can autocomplete /invoke them.
		// Invoking is still a normal session/prompt with text like "/web query".
		raw := u.AvailableCommandsUpdate.AvailableCommands
		cmds := make([]event.AvailableCommand, 0, len(raw))
		for _, c := range raw {
			name := strings.TrimSpace(c.Name)
			if name == "" {
				continue
			}
			// Strip a leading slash if the agent includes it.
			name = strings.TrimPrefix(name, "/")
			hint := ""
			if c.Input != nil && c.Input.Unstructured != nil {
				hint = strings.TrimSpace(c.Input.Unstructured.Hint)
			}
			cmds = append(cmds, event.AvailableCommand{
				Name:        name,
				Description: strings.TrimSpace(c.Description),
				Hint:        hint,
			})
		}
		s.emit(event.Event{
			Type:      event.TypeAvailableCommands,
			SessionID: s.localID,
			Timestamp: now,
			Commands:  cmds,
		})
	case u.Plan != nil:
		// A plan update is the full current plan (replace-semantics); forward
		// the mapped entries so the phone can render the agent's task list.
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   mapPlanEntries(u.Plan.Entries),
		})
	case u.PlanRemoved != nil:
		// Clear: a plan event with an empty (non-nil) entries list.
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   []event.PlanEntry{},
		})
	default:
		s.log.Debug("unhandled session update")
	}
	return nil
}

func (s *session) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if s.cfg.AlwaysApprove {
		return autoAllow(params), nil
	}

	permID := uuid.NewString()
	opts := make([]event.PermissionOption, 0, len(params.Options))
	for _, o := range params.Options {
		opts = append(opts, event.PermissionOption{
			OptionID: string(o.OptionId),
			Name:     o.Name,
			Kind:     string(o.Kind),
		})
	}

	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}
	toolID := string(params.ToolCall.ToolCallId)
	// Surface the actual command/path so the phone can show what it is
	// approving, not just the tool title. Falls back to the title.
	detail := summarizeToolContent(
		params.ToolCall.Content,
		params.ToolCall.RawInput,
		params.ToolCall.RawOutput,
		400,
	)
	if strings.TrimSpace(detail) == "" {
		detail = title
	}

	ch := make(chan permResult, 1)
	s.mu.Lock()
	if s.closed {
		// Best-effort: the stream is already torn down here (Close drains and
		// announces anything that was actually pending), and no
		// permission_request was ever emitted for this id, so nobody is waiting.
		s.emitLocked(s.permissionResolved(permID, event.PermissionStatusCancelled))
		s.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}, nil
	}
	s.pending[permID] = ch
	s.mu.Unlock()

	// Every branch below must retire the id from pending *before* emitting its
	// resolution, so a racing RespondPermission gets "unknown or expired"
	// instead of a false success against an already-decided request.
	takePending := func() {
		s.mu.Lock()
		delete(s.pending, permID)
		s.mu.Unlock()
	}

	s.emit(event.Event{
		Type:         event.TypePermission,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permID,
		Options:      opts,
		ToolID:       toolID,
		ToolName:     title,
		Text:         detail,
		Status:       "pending",
	})

	// Optional safety valve: stop waiting after a bounded time so a missed
	// notification cannot hang the agent forever. A zero timeout leaves the
	// channel nil, which blocks in select — i.e. waits indefinitely as before.
	var timeout <-chan time.Time
	if s.cfg.PermissionTimeout > 0 {
		t := time.NewTimer(s.cfg.PermissionTimeout)
		defer t.Stop()
		timeout = t.C
	}

	select {
	case <-ctx.Done():
		// Abandoned: without this the client composer stays locked forever.
		takePending()
		s.emit(s.permissionResolved(permID, event.PermissionStatusCancelled))
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}, nil
	case <-timeout:
		// Timed out waiting for a decision: treat as cancelled (fail safe) and
		// tell the user why, so the agent unblocks instead of hanging.
		takePending()
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text: fmt.Sprintf(
				"Permission request timed out after %s — the agent stopped "+
					"waiting. Prompt again to retry.", s.cfg.PermissionTimeout),
		})
		s.emit(s.permissionResolved(permID, event.PermissionStatusCancelled))
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}, nil
	case res := <-ch:
		takePending()
		if res.cancelled || res.optionID == "" {
			// A cancelled decision must not masquerade as "resolved" in the
			// transcript: cancelled means no decision was applied.
			s.emit(s.permissionResolved(permID, event.PermissionStatusCancelled))
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
				},
			}, nil
		}
		s.emit(s.permissionResolved(permID, event.PermissionStatusResolved))
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{
					OptionId: acp.PermissionOptionId(res.optionID),
					Outcome:  "selected",
				},
			},
		}, nil
	}
}

func autoAllow(params acp.RequestPermissionRequest) acp.RequestPermissionResponse {
	// Prefer option kind containing "allow" / "approve", else first option.
	var chosen *acp.PermissionOption
	for i := range params.Options {
		o := &params.Options[i]
		k := strings.ToLower(string(o.Kind))
		n := strings.ToLower(o.Name)
		if strings.Contains(k, "allow") || strings.Contains(n, "allow") ||
			strings.Contains(k, "approve") || strings.Contains(n, "approve") {
			chosen = o
			break
		}
	}
	if chosen == nil && len(params.Options) > 0 {
		chosen = &params.Options[0]
	}
	if chosen == nil {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{
				OptionId: chosen.OptionId,
				Outcome:  "selected",
			},
		},
	}
}

func (s *session) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	path := params.Path
	if !filepath.IsAbs(path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("path must be absolute: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	content := string(b)
	if params.Line != nil || params.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 0
		if params.Line != nil && *params.Line > 0 {
			start = min(max(*params.Line-1, 0), len(lines))
		}
		end := len(lines)
		if params.Limit != nil && *params.Limit > 0 && start+*params.Limit < end {
			end = start + *params.Limit
		}
		content = strings.Join(lines[start:end], "\n")
	}
	return acp.ReadTextFileResponse{Content: content}, nil
}

func (s *session) WriteTextFile(_ context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	path := params.Path
	if !filepath.IsAbs(path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("path must be absolute: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (s *session) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	// Default cwd to session cwd if not provided.
	if params.Cwd == nil || *params.Cwd == "" {
		cwd := s.cwd
		params.Cwd = &cwd
	}
	return s.terms.Create(ctx, params)
}

func (s *session) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return s.terms.Output(ctx, params)
}

func (s *session) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return s.terms.WaitForExit(ctx, params)
}

func (s *session) KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return s.terms.Kill(ctx, params)
}

func (s *session) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return s.terms.Release(ctx, params)
}

func contentText(c acp.ContentBlock) string {
	switch {
	case c.Text != nil:
		return c.Text.Text
	case c.ResourceLink != nil:
		name := c.ResourceLink.Name
		if name == "" {
			name = c.ResourceLink.Uri
		}
		return name
	case c.Image != nil:
		return "[image]"
	case c.Audio != nil:
		return "[audio]"
	case c.Resource != nil:
		return "[resource]"
	default:
		return ""
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// mapPlanEntries converts ACP plan entries into the daemon event model,
// normalising status/priority to the fixed string vocabulary. Unknown
// priorities fall back to medium; unknown statuses pass through as-is.
func mapPlanEntries(in []acp.PlanEntry) []event.PlanEntry {
	out := make([]event.PlanEntry, 0, len(in))
	for _, e := range in {
		out = append(out, event.PlanEntry{
			Content:  e.Content,
			Status:   string(e.Status),
			Priority: mapPlanPriority(e.Priority),
		})
	}
	return out
}

func mapPlanPriority(p acp.PlanEntryPriority) string {
	switch p {
	case acp.PlanEntryPriorityHigh:
		return event.PlanPriorityHigh
	case acp.PlanEntryPriorityMedium:
		return event.PlanPriorityMedium
	case acp.PlanEntryPriorityLow:
		return event.PlanPriorityLow
	default:
		return event.PlanPriorityMedium
	}
}

func isBenignPromptErr(err error) bool {
	if err == nil {
		return false
	}
	// The SDK maps a cancelled request to *RequestError{Code: -32800}
	// (JSON-RPC "request cancelled") rather than wrapping context.Canceled, so
	// inspect the structured error first; string matching is only a fallback.
	var reqErr *acp.RequestError
	if errors.As(err, &reqErr) && reqErr.Code == -32800 {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context cancelled") ||
		strings.Contains(msg, "canceled") && strings.Contains(msg, "prompt")
}

// truncateRunes cuts s to at most max bytes without splitting a UTF-8 rune
// (a byte-offset cut mid-sequence turns into mojibake in JSON events).
func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// sanitizeUserFacingErr keeps chat errors short (no huge JSON blobs).
func sanitizeUserFacingErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Collapse multi-line tool / terminal dumps.
	if i := strings.Index(msg, "\n"); i > 0 && i < 200 {
		msg = msg[:i] + "…"
	}
	return truncateRunes(msg, 400)
}

// summarizeToolContent builds a short human summary for chat tool cards.
// Never dumps full RawOutput JSON trees into the event stream.
func summarizeToolContent(content []acp.ToolCallContent, rawIn, rawOut any, maxLen int) string {
	var parts []string
	for _, c := range content {
		switch {
		case c.Content != nil:
			if t := strings.TrimSpace(contentText(c.Content.Content)); t != "" {
				parts = append(parts, t)
			}
		case c.Diff != nil:
			path := c.Diff.Path
			if path == "" {
				path = "file"
			}
			parts = append(parts, "diff "+path)
		case c.Terminal != nil:
			parts = append(parts, "terminal "+string(c.Terminal.TerminalId))
		}
	}
	// Prefer structured content. Only use raw fields as a short fallback.
	if len(parts) == 0 {
		if s := shortAny(rawIn, 120); s != "" {
			parts = append(parts, "in: "+s)
		}
		if s := shortAny(rawOut, 200); s != "" {
			parts = append(parts, s)
		}
	}
	out := strings.Join(parts, " · ")
	out = strings.Join(strings.Fields(out), " ")
	return truncateRunes(out, maxLen)
}

func shortAny(v any, max int) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return ""
		}
		return truncateRunes(s, max)
	case float64, int, int64, bool:
		return fmt.Sprint(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		s := string(b)
		// Skip JSON objects/arrays in chat — they look like "logging".
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			return ""
		}
		return truncateRunes(s, max)
	}
}
