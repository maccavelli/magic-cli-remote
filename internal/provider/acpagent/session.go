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
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/chunkbuf"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpcommon"
	"github.com/maccavelli/magic-cli-remote/internal/provider/sessionutil"
	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// defaultStreamCoalesce caps mid-stream assistant/thought updates at ~12 per
// second (MADR 0024 / 0057). Matches httpagent/acphttp/codex defaults.
const defaultStreamCoalesce = 80 * time.Millisecond

// maxPendingChunkBytes force-flushes a coalesced run that grows large before
// its window elapses (catch-up bursts, not normal token rates).
const maxPendingChunkBytes = 8 << 10

// chunkRetryDelay re-arms a flush whose non-blocking send lost to a slow pump.
var chunkRetryDelay = 50 * time.Millisecond

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// SIGKILL the group only; the exit watcher (spawnAgent) owns cmd.Wait and
	// reaps. A second Wait here would race the watcher's Wait (concurrent Wait
	// on one process is unsafe). Callers MUST hold s.mu and confirm procExited
	// is false so the PID cannot have been recycled before this signal.
	return procutil.KillProcessGroup(cmd.Process)
}

// session is one ACP-backed agent conversation.
type session struct {
	// wire records raw agent frames when MCREMOTE_WIRE_CAPTURE_DIR is set.
	wire *wirecap.Capture

	providerID provider.ID
	// cmdDedupe suppresses repeated identical available_commands
	// advertisements (MADR 0137 F2). Touched only from the notification
	// handler goroutine, which is the sole caller of the emit site.
	cmdDedupe               event.CommandDeduper
	provider                *Provider
	extNotificationHandlers map[string]ExtensionNotificationHandler
	localID                 string
	agentID                 string
	// cwd is the ACP session working directory (session/new|load Cwd).
	cwd string
	// procDir is the OS working directory of the agent process (cmd.Dir).
	// Set once at spawn and never changed. Stdio MCP servers (e.g. gopls mcp)
	// inherit this directory, so a prewarmed spare may only be claimed when
	// the requested session cwd matches procDir.
	procDir string
	conn    *acp.ClientSideConnection
	cmd     *exec.Cmd
	terms   *terminalHost
	log     *slog.Logger
	events  chan event.Event
	// agentCaps is the capability set the agent advertised at initialize.
	// Read-only after spawnAgent; gates loadSession, prompt image/audio, and
	// which MCP transports may be forwarded.
	agentCaps acp.AgentCapabilities
	// authRequired is set when initialize advertised no headless-safe
	// method (MADR 0085 D7). Start must not call session/new; EnsureWarm
	// must not retain the process.
	authRequired bool
	// advertisedAuth is the initialize method ids, for the D7 error text.
	advertisedAuth []string
	// staticModes/defaultModeID come from the Spec and are read-only after
	// spawnAgent: the fallback mode vocabulary for an agent that accepts
	// session/set_mode without advertising modes (see Spec.StaticModes).
	staticModes   []event.SessionMode
	defaultModeID string
	// synthesizeAuto mirrors Spec.SynthesizeAutoMode; read-only after
	// construction. See advertisedModes and SetMode.
	synthesizeAuto bool
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
	// disconnected latches the terminal teardown so the two independent
	// watchers (process exit via cmd.Wait, connection death via conn.Done)
	// emit the disconnected status at most once between them.
	disconnected bool
	// syntheticModes is set when the advertised mode list came from
	// staticModes because the agent declared none; SetMode then validates ids
	// against that list.
	syntheticModes bool
	// autoApprovals accumulates this turn's auto-approved requests for the
	// approval_summary card (MADR 0051 Phase 3). Guarded by s.mu.
	autoApprovals []event.ApprovalItem

	// turnCancel aborts an in-flight session/prompt when engine stderr
	// reports a quota/rate-limit the agent is silently retrying (parity
	// with acphttp/goose — MADR 0073 F1). limitNotified / limitRaw keep
	// the abort path from double-emitting and preserve the provider text.
	turnCancel    context.CancelFunc
	limitNotified bool
	limitRaw      string

	// subagents is this turn's sub-agent set, published as event.TypeSubagents
	// (MADR 0051 D8). Populated from grok's _x.ai/session_notification; empty
	// for agents that report nothing. subagentsPublished latches that a
	// non-empty set went out, so the clear at turn end is only sent to sessions
	// that actually had one. Both guarded by s.mu.
	subagents          map[string]subagentState
	subagentsPublished bool
	// autoApprove is armed by the synthetic `auto` mode and answered in
	// RequestPermission. Per session, unlike cfg.AlwaysApprove which is
	// process-wide (MADR 0049 D1).
	autoApprove bool

	// thinkingLevel is the effort grok applied (session/new|load harvest, or
	// the intended `_meta.reasoningEffort` if harvest is empty). Mid-session
	// SetThinkingLevel is MADR 0106 Phase C. Guarded by s.mu.
	thinkingLevel string
	// currentModelID is the last applied ACP model id (sessionDetail or
	// SetModel), not spawn argv. Guarded by s.mu.
	currentModelID string
	// loading is true while ACP session/load runs: the agent replays the
	// whole prior conversation as ordinary updates then, and those events
	// must be marked Replay so the manager keeps them out of live broadcast.
	loading   bool
	prompting bool
	pending   map[string]*permWaiter // permissionID -> waiter
	// questions holds outstanding multi-question forms (grok's
	// _x.ai/ask_user_question). Kept separate from pending because the answer is
	// a label list per question rather than one option id; released on the same
	// Cancel/Close paths.
	questions map[string]*questionWaiter // questionID -> waiter
	// promptQueue holds prompts accepted while a turn was already active
	// (MADR 0020 Sprint 3 / ACP parity with httpagent). FIFO; drained after
	// the in-flight Prompt RPC returns when no permission is pending.
	// Overflow returns provider.ErrTurnBusy. Cancel/Close clear the queue.
	promptQueue [][]provider.Content

	// lastActivity is the wall time (UnixNano) of the last agent output,
	// updated on every SessionUpdate and at turn start. Drives the stall
	// watchdog that tells the user a turn has gone quiet.
	lastActivity atomic.Int64

	// testSubmit, when non-nil, replaces s.conn.Prompt (unit tests only).
	testSubmit func(ctx context.Context, blocks []acp.ContentBlock) (acp.PromptResponse, error)
	// testCancel, when non-nil, replaces s.conn.Cancel (unit tests only).
	testCancel func(ctx context.Context) error

	// chunks coalesces assistant/thought text (MADR 0057 H-1 / chunkbuf).
	// emitMu serializes Add/Drain/Unflush and delivery of the events they
	// return. flushMu guards the flush timer only.
	chunks     *chunkbuf.Buffer
	emitMu     sync.Mutex
	flushMu    sync.Mutex
	flushTimer *time.Timer

	mcpMu     sync.Mutex
	mcpStatus []provider.MCPServerStatus
}

type permResult struct {
	optionID  string
	cancelled bool
	// deviceID is which paired device sent this decision (MADR 0077 §1) —
	// empty for internal outcomes (session close, ctx cancellation, timeout,
	// an auto-mode-arm sweep), populated only when a real RespondPermission
	// call carried one.
	deviceID string
}

// permWaiter is one outstanding RequestPermission. resolved is the single-winner
// latch: exactly one of {RespondPermission, ctx-cancel, timeout, Cancel/Close}
// may flip it true (under s.mu), and that party alone decides the outcome. It
// closes the window where a client's answer landing as the timeout fired
// returned success to the client while the tool call was actually cancelled.
//
// allowOptionID / toolName / detail are filled when the request is emitted so
// arming auto can sweep already-open sheets without inventing an option id
// (MADR 0053 F2 / MADR 0044 D4.5).
type permWaiter struct {
	ch            chan permResult
	resolved      bool
	allowOptionID string
	toolName      string
	detail        string
}

// questionResult is one answered (or abandoned) question form. answers[i] holds
// the labels chosen for questions[i].
type questionResult struct {
	answers   [][]string
	cancelled bool
}

// questionWaiter is one outstanding question form, with the same single-winner
// latch discipline as permWaiter.
type questionWaiter struct {
	ch       chan questionResult
	resolved bool
}

var _ provider.Session = (*session)(nil)
var _ provider.PermissionSession = (*session)(nil)
var _ provider.CWDSession = (*session)(nil)
var _ provider.ModeSession = (*session)(nil)
var _ provider.ModelSession = (*session)(nil)
var _ provider.MCPStatusSession = (*session)(nil)
var _ provider.DiagnosticsSession = (*session)(nil)
var _ provider.ConfigSession = (*session)(nil)
var _ acp.Client = (*session)(nil)

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return s.providerID }
func (s *session) AgentSessionID() string     { return s.agentID }
func (s *session) CWD() string                { return s.cwd }
func (s *session) Events() <-chan event.Event { return s.events }

// maxPromptQueue is the per-session FIFO depth for second prompts while busy.
// Excess prompts return provider.ErrTurnBusy (Owner Q1 queue with overflow).
// Matches httpagent so both transports share the same product policy.
const maxPromptQueue = 4

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.prompting {
		// FIFO queue (MADR 0020 Sprint 3): accept the prompt, drain after idle.
		if len(s.promptQueue) >= maxPromptQueue {
			s.mu.Unlock()
			return provider.ErrTurnBusy
		}
		s.promptQueue = append(s.promptQueue, sessionutil.CloneContent(parts))
		n := len(s.promptQueue)
		s.mu.Unlock()
		s.emitUserMessage(parts)
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text: fmt.Sprintf("Queued (%d/%d) — will send when the agent is idle",
				n, maxPromptQueue),
		})
		return nil
	}
	s.mu.Unlock()
	return s.beginTurn(ctx, parts, true)
}

// beginTurn claims the turn and submits parts to the agent.
// emitUser controls whether a user_message is emitted (false when draining a
// queue entry that already showed the user bubble at enqueue time).
func (s *session) beginTurn(ctx context.Context, parts []provider.Content, emitUser bool) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.prompting {
		s.mu.Unlock()
		return provider.ErrTurnBusy
	}
	s.prompting = true
	s.mu.Unlock()

	text, blocks, attachments := s.buildPromptBlocks(parts)
	// A prompt with only unsupported attachments and no text would send an
	// empty block list; refuse rather than issue a no-op turn.
	if len(blocks) == 0 {
		s.mu.Lock()
		s.prompting = false
		s.mu.Unlock()
		err := fmt.Errorf("prompt has no sendable content (text empty; attachments unsupported by agent)")
		if !emitUser {
			// Drained queue entry failed validation — keep draining the rest.
			s.emit(event.Event{
				Type:      event.TypeError,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Error:     err.Error(),
			})
			s.tryDrainQueue()
			return nil
		}
		return err
	}

	if emitUser {
		s.emit(event.Event{
			Type:        event.TypeUserMessage,
			SessionID:   s.localID,
			Timestamp:   time.Now().UTC(),
			Text:        text,
			Attachments: attachments,
		})
	}
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "running",
	})

	// The turn must survive the caller: ctx is typically the phone's WebSocket
	// request context, and a dropped mobile connection mid-turn must not abort
	// the agent's work (sessions are designed to outlive disconnects — that is
	// what history replay is for). Explicit Cancel(), stderr limit detection,
	// and process teardown remain the ways to stop a turn.
	baseCtx := context.WithoutCancel(ctx)
	turnCtx, cancel := context.WithCancel(baseCtx)
	s.mu.Lock()
	s.turnCancel = cancel
	s.limitNotified = false
	s.limitRaw = ""
	s.mu.Unlock()

	// Stall watchdog: a wedged agent otherwise pins "running" silently and
	// the user has no way to tell "thinking hard" from "dead".
	s.lastActivity.Store(time.Now().UnixNano())
	turnDone := make(chan struct{})
	if s.cfg.TurnStallNotice > 0 {
		go s.watchStall(turnDone)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("agent turn handler panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		defer func() {
			cancel()
			close(turnDone)
			s.mu.Lock()
			s.prompting = false
			s.turnCancel = nil
			s.limitNotified = false
			s.limitRaw = ""
			s.mu.Unlock()
			// Drain next queued prompt after the turn fully ends.
			s.tryDrainQueue()
			// Re-arm the spare now the turn is over, not when it began
			// (MADR 0137 F5). Arming on Start put a full ~3.8 s spawn —
			// process launch, ACP initialize, model-catalog harvest — in
			// flight at the same moment as the user's first prompt,
			// competing with the turn they were waiting on.
			//
			// In the deferred block rather than beside the turn_complete emit,
			// so an errored or cancelled turn re-arms too. Arming only on the
			// happy path would leave a session that failed its first turn with
			// no spare at all, which is the state prewarm exists to avoid.
			//
			// EnsureWarm returns immediately, does its work in a goroutine and
			// is a no-op when a spare already exists, so a long conversation
			// arms once rather than once per turn. After tryDrainQueue on
			// purpose: a queued prompt has already chosen to contend, and the
			// spare must not be skipped because one was waiting.
			//
			// The nil check is not defensive padding. A session built without
			// a provider is a test fixture, and this defer runs under a
			// recover — so the nil dereference this guard prevents was caught,
			// logged and swallowed, leaving four package tests passing while
			// their turn goroutines died mid-cleanup. It took a -race CI run
			// to surface it.
			if s.provider != nil {
				s.provider.EnsureWarm()
			}
		}()

		resp, err := s.submitPrompt(turnCtx, blocks)
		// However the turn ends — cancelled, errored or complete — close the
		// approval card here, ahead of every exit path's boundary event, so it
		// is marked done rather than left running into the next turn. Not a
		// defer: that would fire after turn_complete, not before (MADR 0051).
		s.finishApprovals()
		// Same for the sub-agent panel: the turn is over, so nothing is running.
		s.clearSubagents()
		if err != nil {
			// Stderr scrape aborted the wait for a provider limit.
			s.mu.Lock()
			limitRaw := s.limitRaw
			limitHit := s.limitNotified && limitRaw != ""
			s.mu.Unlock()
			if limitHit {
				s.emitClassifiedTurnError(limitRaw)
				return
			}
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
			// Present rewrites 429/529/quota dumps into short natural-language
			// copy; fall back to the sanitizer for unclassified failures.
			s.emitClassifiedTurnError(err.Error())
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

// buildPromptBlocks maps provider content to ACP blocks, dropping unsupported
// attachment kinds (image/audio) with a warning.
func (s *session) buildPromptBlocks(parts []provider.Content) (text string, blocks []acp.ContentBlock, attachments []event.AttachmentInfo) {
	var b strings.Builder
	blocks = make([]acp.ContentBlock, 0, len(parts))
	promptCaps := s.agentCaps.PromptCapabilities
	for _, p := range parts {
		switch p.Type {
		case "", "text":
			b.WriteString(p.Text)
			blocks = append(blocks, acp.TextBlock(p.Text))
		case "image":
			// Gate on the agent's advertised capability: sending an image block
			// to an agent that did not advertise promptCapabilities.image is a
			// protocol violation. Drop with a warning rather than fail the turn.
			if !promptCaps.Image {
				s.log.Warn("dropping image prompt content: agent lacks promptCapabilities.image")
				continue
			}
			blocks = append(blocks, acp.ImageBlock(p.Data, p.MimeType))
			attachments = append(attachments, event.AttachmentInfo{Kind: "image", MimeType: p.MimeType})
		case "audio":
			if !promptCaps.Audio {
				s.log.Warn("dropping audio prompt content: agent lacks promptCapabilities.audio")
				continue
			}
			blocks = append(blocks, acp.AudioBlock(p.Data, p.MimeType))
			attachments = append(attachments, event.AttachmentInfo{Kind: "audio", MimeType: p.MimeType})
		default:
			s.log.Warn("dropping unknown prompt content type", slog.String("type", p.Type))
		}
	}
	return b.String(), blocks, attachments
}

// emitUserMessage records the user turn in the transcript at enqueue/send time.
// Attachment descriptors are listed even when the agent will drop them later —
// the bubble should match what the user attached, not capability filtering.
func (s *session) emitUserMessage(parts []provider.Content) {
	ev := sessionutil.UserMessage(parts)
	ev.SessionID = s.localID
	ev.Timestamp = time.Now().UTC()
	s.emit(ev)
}

func (s *session) submitPrompt(ctx context.Context, blocks []acp.ContentBlock) (acp.PromptResponse, error) {
	if s.testSubmit != nil {
		return s.testSubmit(ctx, blocks)
	}
	return s.conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acp.SessionId(s.agentID),
		Prompt:    blocks,
	})
}

// tryDrainQueue starts the next queued prompt if the session is idle and no
// permission is outstanding (MADR 0020 queue policy).
func (s *session) tryDrainQueue() {
	s.mu.Lock()
	if s.closed || s.prompting || len(s.promptQueue) == 0 || len(s.pending) > 0 {
		s.mu.Unlock()
		return
	}
	next := s.promptQueue[0]
	s.promptQueue = s.promptQueue[1:]
	s.mu.Unlock()

	// Background drain uses a detached context — the original phone request
	// is long gone; cancel/close remain the stop paths.
	if err := s.beginTurn(context.Background(), next, false); err != nil {
		s.log.Warn("queued prompt failed", slog.String("err", err.Error()))
		cls := agenterr.Present(err.Error(), time.Now())
		msg := cls.Message
		if msg == "" {
			msg = sanitizeUserFacingErr(err)
		}
		s.emit(event.Event{
			Type:      event.TypeError,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Error:     msg,
			ErrorKind: string(cls.Kind),
			RetryAt:   cls.ResetAt,
		})
		// Keep draining remaining items so one failure does not strand the queue.
		s.tryDrainQueue()
	}
}

// emitClassifiedTurnError writes turn_complete + Present TypeError + status
// for a failed prompt (shared by RPC errors and stderr limit aborts).
func (s *session) emitClassifiedTurnError(raw string) {
	cls := agenterr.Present(raw, time.Now())
	msg := cls.Message
	if msg == "" {
		msg = strings.TrimSpace(raw)
	}
	now := time.Now().UTC()
	s.emit(event.Event{
		Type:       event.TypeTurnComplete,
		SessionID:  s.localID,
		Timestamp:  now,
		StopReason: "error",
		Status:     "error",
	})
	s.emit(event.Event{
		Type:      event.TypeError,
		SessionID: s.localID,
		Timestamp: now,
		Error:     msg,
		ErrorKind: string(cls.Kind),
		RetryAt:   cls.ResetAt,
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: now,
		Status:    "error",
	})
}

// noteEngineLogLine is invoked for each complete agent stderr line. When the
// line is a provider quota/rate-limit and a turn is in flight, abort the wait
// so the phone sees a limit card instead of hanging.
func (s *session) noteEngineLogLine(line string) {
	if !agenterr.IsLimit(line, time.Now()) {
		return
	}
	s.mu.Lock()
	if s.closed || !s.prompting || s.limitNotified {
		s.mu.Unlock()
		return
	}
	s.limitNotified = true
	s.limitRaw = line
	cancel := s.turnCancel
	s.mu.Unlock()
	s.log.Warn("agent provider limit detected",
		slog.String("session_id", s.localID),
		slog.String("text", line),
	)
	if cancel != nil {
		cancel()
		return
	}
	s.emitClassifiedTurnError(line)
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

// SetMode switches the agent's active operating mode (ACP session/set_mode).
//
// Real modes: the agent confirms via current_mode_update, which we forward as a
// session_mode event (no local current-mode store for those).
//
// Synthetic auto (Spec.SynthesizeAutoMode): the daemon arms interception and
// always announces current_mode_id=auto itself. Relying on the agent is wrong
// when the session is already on its normal mode — set_mode is a no-op and no
// notification fires (MADR 0053 F1).
//
// When the mode list is ours (Spec.StaticModes — the agent advertised none) the
// id is checked here: such an agent has no declared vocabulary, and grok in
// particular accepts any id and echoes it back, so an unchecked typo would look
// like a successful switch.
func (s *session) SetMode(ctx context.Context, modeID string) error {
	s.mu.Lock()
	agentID := s.agentID
	closed := s.closed
	synthetic := s.syntheticModes
	canAuto := s.synthesizeAuto
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}

	if canAuto && strings.EqualFold(modeID, autoModeID) {
		return s.armAutoMode(ctx, agentID)
	}

	// Prefer the canonical id from the static list so a case variant never
	// reaches the agent (MADR 0053 F6).
	forwardID := modeID
	if synthetic {
		var ok bool
		forwardID, ok = canonicalStaticModeID(s.staticModes, modeID)
		if !ok {
			return fmt.Errorf("unknown mode %q", modeID)
		}
	}
	_, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
		SessionId: acp.SessionId(agentID),
		ModeId:    acp.SessionModeId(forwardID),
	})
	if err == nil {
		// Any real mode disarms auto. Only after the agent accepted: a failed
		// switch must not leave the session claiming a mode it is not in.
		s.mu.Lock()
		wasAuto := s.autoApprove
		s.autoApprove = false
		s.mu.Unlock()
		if wasAuto {
			s.log.Info("acp auto-approve disarmed",
				slog.String("session_id", s.localID),
				slog.String("agent_session_id", agentID),
				slog.String("mode", forwardID),
			)
		}
		// Nothing further joins the approval card once auto is off.
		s.finishApprovals()
	}
	return err
}

// armAutoMode turns on daemon-side auto-approve and puts the agent into its
// normal working mode.
//
// The second half matters: auto-approving a session sitting in `plan` would be
// prompts-off and edits-off at the same time, which is not what the user asked
// for when they armed auto (MADR 0049 D1). The synthetic id itself is never
// sent — the agent has no such mode.
func (s *session) armAutoMode(ctx context.Context, agentID string) error {
	target := s.normalModeID()
	s.mu.Lock()
	s.autoApprove = true
	s.mu.Unlock()

	if target != "" {
		if _, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
			SessionId: acp.SessionId(agentID),
			ModeId:    acp.SessionModeId(target),
		}); err != nil {
			// Disarm rather than report auto while the agent stayed in plan.
			s.mu.Lock()
			s.autoApprove = false
			s.mu.Unlock()
			return err
		}
	}
	// Always announce. The agent only sends current_mode_update when its mode
	// actually changes; arming from the already-normal state is a no-op for
	// the agent and was silent for the client (MADR 0053 F1). Codex and
	// httpagent self-emit on every SetMode for the same reason.
	s.emitArmedMode()
	s.log.Warn("acp auto-approve armed",
		slog.String("session_id", s.localID),
		slog.String("agent_session_id", agentID),
		slog.String("agent_mode", target),
	)
	s.sweepPendingPermissions()
	return nil
}

// emitArmedMode publishes the synthetic auto id as the current mode. The modes
// list is omitted — clients keep the create-time list (protocol-v1 change form;
// same as codex/httpagent).
func (s *session) emitArmedMode() {
	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		CurrentModeID: autoModeID,
	})
}

// sweepPendingPermissions answers every outstanding permission_request as
// allowed. Arming auto to unblock a stuck turn is the common reason to reach
// for it; without this the sheet stays up (MADR 0044 D4.5, MADR 0053 F2).
// Questions (s.questions) are intentionally not touched — same boundary as
// AlwaysApprove vs ask_user_question.
func (s *session) sweepPendingPermissions() {
	s.mu.Lock()
	pending := s.pending
	s.pending = make(map[string]*permWaiter)
	s.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	for _, w := range pending {
		if w == nil {
			continue
		}
		opt := w.allowOptionID
		if opt == "" {
			select {
			case w.ch <- permResult{cancelled: true}:
			default:
			}
			continue
		}
		select {
		case w.ch <- permResult{optionID: opt}:
		default:
		}
		// awaitDecision's channel path emits permission_resolved; we only add
		// the auto-approve audit line the hot path would have written.
		s.noteAutoApprovalItem(w.toolName, w.detail)
	}
}

// canonicalStaticModeID returns the list id matching modeID (case-insensitive)
// and whether any entry matched.
func canonicalStaticModeID(modes []event.SessionMode, modeID string) (string, bool) {
	for _, m := range modes {
		if strings.EqualFold(m.ID, modeID) {
			return m.ID, true
		}
	}
	return "", false
}

// ThinkingLevel returns the effort grok applied, or "" when none was set.
func (s *session) ThinkingLevel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thinkingLevel
}

// ThinkingMutability reports live: grok 1.0.5 applies reasoning effort
// mid-session via session/set_model `_meta.reasoningEffort`, confirmed by
// session/resume (MADR 0106), and SetThinkingLevel never returns
// ErrThinkingLevelFixed. The phone client hardcoded the opposite for the whole
// of 0106's life and told users to start a new session; this method is the
// answer it should have been asking for (MADR 0123 F5).
func (s *session) ThinkingMutability() provider.ThinkingMutability {
	return provider.ThinkingMutabilityLive
}

// Compile-time check: ACP sessions report ThinkingSession so /thinking is
// advertised. SetThinkingLevel is in thinking.go (MADR 0106).
var _ provider.ThinkingSession = (*session)(nil)

// SetModel switches the live model mid-session via ACP session/set_model
// (verified live against grok 0.2.112; MADR 0039 D1).
func (s *session) SetModel(ctx context.Context, model string) error {
	s.mu.Lock()
	agentID := s.agentID
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}
	var resp struct {
		Meta struct {
			Model struct {
				Ok  string `json:"Ok,omitempty"`
				Err string `json:"Err,omitempty"`
			} `json:"model"`
		} `json:"_meta"`
	}
	if err := s.rawRequest(ctx, "session/set_model",
		map[string]any{"sessionId": agentID, "modelId": model}, &resp); err != nil {
		return err
	}
	if resp.Meta.Model.Err != "" {
		return fmt.Errorf("set_model: %s", resp.Meta.Model.Err)
	}
	applied := model
	if resp.Meta.Model.Ok != "" {
		applied = resp.Meta.Model.Ok
		if applied != model {
			s.log.Warn("set_model accepted a different id",
				slog.String("requested", model),
				slog.String("accepted", applied))
		}
	}
	s.mu.Lock()
	s.currentModelID = applied
	s.mu.Unlock()
	return nil
}

// MCPStatus implements provider.MCPStatusSession.
func (s *session) MCPStatus(ctx context.Context) ([]provider.MCPServerStatus, error) {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	res := make([]provider.MCPServerStatus, len(s.mcpStatus))
	copy(res, s.mcpStatus)
	return res, nil
}

// Diagnostics implements provider.DiagnosticsSession.
func (s *session) Diagnostics(ctx context.Context) (provider.Diagnostics, error) {
	mcp, err := s.MCPStatus(ctx)
	if err != nil {
		return provider.Diagnostics{}, err
	}
	return provider.Diagnostics{MCP: mcp}, nil
}

// SetConfigOption changes an agent-defined session config option (ACP
// session/set_config_option). kind selects the request variant: "boolean"
// sends a bool (value "true"/"false"); anything else sends a select value id.
func (s *session) SetConfigOption(ctx context.Context, optionID, kind, value string) error {
	s.mu.Lock()
	agentID := s.agentID
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}
	var req acp.SetSessionConfigOptionRequest
	switch kind {
	case "boolean":
		req.Boolean = &acp.SetSessionConfigOptionBoolean{
			Type:      "boolean",
			ConfigId:  acp.SessionConfigId(optionID),
			SessionId: acp.SessionId(agentID),
			Value:     value == "true",
		}
	default:
		req.ValueId = &acp.SetSessionConfigOptionValueId{
			ConfigId:  acp.SessionConfigId(optionID),
			SessionId: acp.SessionId(agentID),
			Value:     acp.SessionConfigValueId(value),
		}
	}
	_, err := s.conn.SetSessionConfigOption(ctx, req)
	return err
}

func (s *session) Cancel(ctx context.Context) error {
	s.mu.Lock()
	// Cancel clears the prompt queue — do not auto-run queued prompts after stop
	// (MADR 0020 queue policy).
	s.promptQueue = nil
	// Snapshot pending under lock, then release waiters without holding mu
	// (a blocked send must not stall other session ops).
	pending := s.pending
	s.pending = make(map[string]*permWaiter)
	questions := s.questions
	s.questions = make(map[string]*questionWaiter)
	s.mu.Unlock()

	for _, w := range pending {
		// Buffered (1): empty channel always accepts; full means already resolved.
		select {
		case w.ch <- permResult{cancelled: true}:
		default:
		}
	}
	for _, w := range questions {
		select {
		case w.ch <- questionResult{cancelled: true}:
		default:
		}
	}

	if s.testCancel != nil {
		return s.testCancel(ctx)
	}
	return s.conn.Cancel(ctx, acp.CancelNotification{
		SessionId: acp.SessionId(s.agentID),
	})
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool, deviceID string) error {
	_ = ctx
	// Claim the resolution under the lock: whoever flips resolved first (this
	// answer, or the RequestPermission ctx/timeout branch) is the sole decider.
	// Without the latch, an answer arriving exactly as the timeout fired would
	// land in the buffered channel and return success here while the waiter had
	// already taken the cancelled path — the client believing its choice applied.
	s.mu.Lock()
	w, ok := s.pending[permissionID]
	if !ok || w.resolved {
		s.mu.Unlock()
		return fmt.Errorf("unknown or expired permission %q", permissionID)
	}
	w.resolved = true
	delete(s.pending, permissionID)
	s.mu.Unlock()
	// We own the outcome now; the waiter is guaranteed to read this (its ctx/
	// timeout branches see resolved and defer to the channel).
	w.ch <- permResult{optionID: optionID, cancelled: cancelled, deviceID: deviceID}
	// Answering may unblock a waiting queue drain (rare on ACP — permissions
	// usually resolve while prompting is still true — but matches httpagent).
	s.tryDrainQueue()
	return nil
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

// signalDisconnected emits the terminal error + disconnected status so the
// session manager tears the session down (pump → autoClose → Close → reap).
// Both exit watchers funnel through here: cmd.Wait (the process died) and
// watchConnClose (the ACP connection died while the process may still be
// alive). The latch fires the pair at most once, and a session already being
// closed stays silent — conn.Done() also fires on every normal teardown.
func (s *session) signalDisconnected(msg string) {
	s.mu.Lock()
	if s.closed || s.disconnected {
		s.mu.Unlock()
		return
	}
	s.disconnected = true
	s.mu.Unlock()
	s.emit(event.Event{
		Type:      event.TypeError,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Error:     msg,
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "disconnected",
	})
}

// watchConnClose turns a dead ACP connection into a clean session teardown.
// The SDK runs every session/update notification on ONE goroutine; if our
// control-event delivery blocks it long enough, the SDK's inbound queue
// overflows and it tears the connection down WITHOUT killing the agent process.
// Nothing else observes that: cmd.Wait only fires on process exit, so the
// session would otherwise zombie (process alive, no disconnected event, manager
// entry live forever). Funnel a connection death through the same path as a
// process exit; the manager's Close then reaps the process. Exits quietly when
// the session is closed the normal way (done closes; conn.Done fires too, but
// signalDisconnected no-ops once closed).
func (s *session) watchConnClose(conn interface{ Done() <-chan struct{} }) {
	select {
	case <-conn.Done():
	case <-s.done:
		return
	}
	s.signalDisconnected(fmt.Sprintf("%s agent connection lost", s.providerID))
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	// Snapshot-and-swap pending under the lock; do not mark closed yet so
	// permission_resolved can still be delivered on the control path before
	// done is closed (Phase 2.5 / B.2). Drop any queued prompts with the session.
	pending := s.pending
	s.pending = make(map[string]*permWaiter)
	questions := s.questions
	s.questions = make(map[string]*questionWaiter)
	s.promptQueue = nil
	s.mu.Unlock()

	// Unblock RequestPermission waiters first (buffered 1 — never hangs on
	// an empty channel; full means already resolved).
	for _, w := range pending {
		select {
		case w.ch <- permResult{cancelled: true}:
		default:
		}
	}
	for _, w := range questions {
		select {
		case w.ch <- questionResult{cancelled: true}:
		default:
		}
	}

	// Announce abandonment while the session is still "open" for emit/deliver
	// so clients unlock the composer. Bound each send so a fully stopped pump
	// cannot pin Close forever.
	for id := range pending {
		ev := s.permissionResolved(id, event.PermissionStatusCancelled, "", "")
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
	for id := range questions {
		ev := s.questionResolvedEvent(id, event.PermissionStatusCancelled)
		s.prepareEvent(&ev)
		select {
		case s.events <- ev:
		case <-time.After(200 * time.Millisecond):
			s.log.Debug("question_resolved dropped on close; pump not draining",
				slog.String("question_id", id),
				slog.String("session_id", s.localID),
			)
		}
	}

	// Flush any buffered stream text while the session is still open for
	// delivery (MADR 0057). After closed+done, emit/deliver short-circuit.
	s.drainChunks()

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
	// Kill under the lock, re-reading procExited so the decision is atomic with
	// the exit watcher setting it: if the child was already reaped (during the
	// bounded CloseSession above, which commonly makes the agent exit), the PID
	// may be recycled and we must NOT signal its group. The signal itself is a
	// non-blocking syscall, so holding s.mu across it cannot deadlock.
	s.mu.Lock()
	if !s.procExited && s.cmd != nil && s.cmd.Process != nil {
		_ = killProcessTree(s.cmd)
	}
	s.mu.Unlock()
	return nil
}

// emit delivers ev, coalescing high-frequency streaming text into ~one event
// per [Config.StreamCoalesceWindow] instead of one per model token
// (MADR 0024 / 0057 H-1). Control events are boundaries: pending text is
// delivered ahead of them. Text is never dropped under backpressure (Unflush).
func (s *session) emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		// Required for chunkbuf deadlines: a zero Timestamp makes since.Add(win)
		// land in the year 0001, so the flush timer fires immediately and
		// defeats timed coalescing entirely.
		ev.Timestamp = time.Now().UTC()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.prepareEvent(&ev)
	s.mu.Unlock()

	s.emitMu.Lock()
	out, deadline, blocking := s.chunkBuffer().Add(ev)
	for _, e := range out {
		if blocking || event.IsControl(e.Type) {
			// Release emitMu before a possibly-blocking control deliver so a
			// stalled consumer cannot pin the flush timer out of onFlushTimer.
			s.emitMu.Unlock()
			s.deliver(e, true)
			s.emitMu.Lock()
			continue
		}
		if s.trySend(e) {
			continue
		}
		if !chunkbuf.IsChunk(e.Type) {
			s.log.Warn("dropping event; slow consumer",
				slog.String("type", string(e.Type)),
				slog.String("session_id", s.localID),
			)
			continue
		}
		s.noteUnflush(s.chunks.Unflush(e))
		deadline = time.Now().Add(chunkRetryDelay)
	}
	s.emitMu.Unlock()

	if deadline.IsZero() {
		s.stopFlush()
		return
	}
	s.armFlush(deadline)
}

// emitLocked is emit for callers already holding s.mu. Releases the lock around
// the full emit path so chunkbuf delivery can block without deadlocking.
func (s *session) emitLocked(ev event.Event) {
	if s.closed {
		return
	}
	s.mu.Unlock()
	s.emit(ev)
	s.mu.Lock()
}

// chunkBuffer returns the streaming coalescer, building it on first use so
// field-assembled test sessions reach emit safely. Caller holds emitMu.
func (s *session) chunkBuffer() *chunkbuf.Buffer {
	if s.chunks == nil {
		// grok's tool updates carry the full ACP content each time
		// (summarizeToolContent below), so the replacing lane is correct here —
		// and grok is the provider whose event volume most needs it: one `hi`
		// turn emits 247 frames (MADR 0138 F3).
		s.chunks = chunkbuf.New(s.cfg.StreamCoalesceWindow(), maxPendingChunkBytes, chunkbuf.WithToolLane())
	}
	return s.chunks
}

func (s *session) trySend(ev event.Event) bool {
	select {
	case s.events <- ev:
		return true
	default:
		return false
	}
}

func (s *session) noteUnflush(dropped int) {
	if dropped > 0 {
		s.log.Warn("stream buffer overflow; discarded oldest text",
			slog.Int("bytes", dropped),
			slog.String("session_id", s.localID),
		)
	}
}

func (s *session) armFlush(deadline time.Time) {
	d := max(time.Until(deadline), 0)
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if s.flushTimer != nil {
		return
	}
	s.flushTimer = time.AfterFunc(d, s.onFlushTimer)
}

func (s *session) stopFlush() {
	s.flushMu.Lock()
	t := s.flushTimer
	s.flushTimer = nil
	s.flushMu.Unlock()
	if t != nil {
		t.Stop()
	}
}

func (s *session) onFlushTimer() {
	s.flushMu.Lock()
	s.flushTimer = nil
	s.flushMu.Unlock()

	select {
	case <-s.done:
		return
	default:
	}

	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	retry := false
	if ok && !s.trySend(ev) {
		s.noteUnflush(s.chunks.Unflush(ev))
		retry = true
	}
	s.emitMu.Unlock()

	if retry {
		s.armFlush(time.Now().Add(chunkRetryDelay))
	}
}

// drainChunks flushes buffered streaming text on the way out of a session.
func (s *session) drainChunks() {
	s.stopFlush()
	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	if ok {
		s.trySend(ev)
	}
	s.emitMu.Unlock()
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
// deviceID/optionID are empty for internal outcomes (session close, ctx
// cancellation, timeout, an auto-mode-arm sweep) — see permResult's doc.
func (s *session) permissionResolved(permID, status, deviceID, optionID string) event.Event {
	return event.Event{
		Type:         event.TypePermissionResolved,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permID,
		Status:       status,
		DeviceID:     deviceID,
		OptionID:     optionID,
	}
}

// permissionExpired is the timeout arm's resolution: cancelled, marked
// TimedOut per the wire contract (protocol-v1.md), which reserves the flag
// for the permission_timeout_seconds expiry alone. Every other cancelled
// path (abandonment, close) stays on permissionResolved -- a separate helper
// so the distinction survives future callers (MADR 0101 B).
func (s *session) permissionExpired(permID string) event.Event {
	ev := s.permissionResolved(permID, event.PermissionStatusCancelled, "", "")
	ev.TimedOut = true
	return ev
}

func isHighFrequencyEvent(t event.Type) bool {
	switch t {
	case event.TypeAssistantChunk, event.TypeThoughtChunk:
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
	// Sub-agent frames arrive on this same connection carrying the child's
	// session id — grok 0.2.114 streams its subagents that way, and measured on
	// one two-file task the child produced 53% of the turn's assistant chunks,
	// all of it landing in the parent's transcript. Its content is not this
	// conversation: the sub-agent reports to the main agent, and the parent's
	// reply carries the conclusion (MADR 0051 D6).
	//
	// The other two transports already do this lookup — acphttp routes by
	// sessionId and drops misses, codex the same by threadId. This is the stdio
	// transport catching up.
	//
	// Compared against the live agent id rather than dropping unknown ids
	// outright, so a frame arriving before session/new returns (agentID still
	// empty) is not lost.
	if id := string(params.SessionId); id != "" {
		if live := s.AgentSessionID(); live != "" && id != live {
			s.log.Debug("acp: dropping child-session update",
				slog.String("frame_session_id", id),
				slog.String("session_id", s.localID))
			return nil
		}
	}
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
		// Skip an advertisement identical to the last one (MADR 0137 F2).
		// grok re-sends the full list on every turn boundary — 22 times in one
		// `hi` — and each repeat crosses the websocket, lands in session
		// history and re-renders on the phone without carrying any news.
		if s.cmdDedupe.ShouldEmit(cmds) {
			s.emit(event.Event{
				Type:      event.TypeAvailableCommands,
				SessionID: s.localID,
				Timestamp: now,
				Commands:  cmds,
			})
		}
	case u.Plan != nil:
		// A plan update is the full current plan (replace-semantics); forward
		// the mapped entries so the phone can render the agent's task list.
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   acpcommon.PlanEntries(u.Plan.Entries),
		})
	case u.PlanRemoved != nil:
		// Clear: a plan event with an empty (non-nil) entries list.
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   []event.PlanEntry{},
		})
	case u.UsageUpdate != nil:
		// Token/context report → advisory usage event (droppable telemetry).
		s.emit(event.Event{
			Type:      event.TypeUsage,
			SessionID: s.localID,
			Timestamp: now,
			Usage:     &event.Usage{Used: u.UsageUpdate.Used, Size: u.UsageUpdate.Size},
		})
	case u.CurrentModeUpdate != nil:
		// The active mode changed. Carry only the current id; the client keeps
		// the available-mode list it received at session create/load.
		//
		// Through reportedModeID: arming the synthetic auto mode puts the agent
		// into its normal mode, so the agent confirms *that* id here. Publishing
		// it raw overwrote the `auto` the user had just selected, leaving a chip
		// that named a mode the daemon was not enforcing (MADR 0049 D4).
		s.emit(event.Event{
			Type:      event.TypeMode,
			SessionID: s.localID,
			Timestamp: now,
			CurrentModeID: s.reportedModeID(
				string(u.CurrentModeUpdate.CurrentModeId)),
		})
	case u.ConfigOptionUpdate != nil:
		// The agent updated its config options mid-session. Re-emit the full
		// set so the client can refresh its UI.
		s.emitConfigOptions(u.ConfigOptionUpdate.ConfigOptions)
	default:
		s.log.Debug("unhandled session update")
	}
	return nil
}

// emitCapabilities forwards the agent's negotiated capabilities (from ACP
// initialize) as a session_capabilities event so a client can gate UI such as
// the image-attach button. Emitted once at session create/load.
func (s *session) emitCapabilities() {
	pc := s.agentCaps.PromptCapabilities
	s.emit(event.Event{
		Type:      event.TypeSessionCapabilities,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Capabilities: &event.Capabilities{
			Image:       pc.Image,
			Audio:       pc.Audio,
			LoadSession: s.agentCaps.LoadSession,
		},
	})
}

// emitModes forwards the agent's session mode state (available modes + current
// mode) as a session_mode event. No-op when the agent reported no modes.
// autoModeID is the synthetic, daemon-enforced auto-approve mode. It is never
// forwarded to the agent: for the providers that opt in, the agent's own auto
// (if any) lives in a different namespace — grok's is a `--permission-mode`
// launch flag, not an ACP mode id (MADR 0049 D1).
const autoModeID = "auto"

func syntheticAutoMode() event.SessionMode {
	return event.SessionMode{
		ID:   autoModeID,
		Name: "Auto",
		// Human name matches Build/Plan casing on grok; id stays "auto"
		// (MADR 0053 F5).
		Description: "Auto-approve — no prompts",
		// What makes the phone gate arming behind a confirmation
		// (MADR 0044 D1). Do not drop it.
		Dangerous: true,
	}
}

// advertisedModes is [base] plus the synthetic auto mode, when this provider
// opted in and the agent has no auto of its own.
func (s *session) advertisedModes(base []event.SessionMode) []event.SessionMode {
	if !s.synthesizeAuto || len(base) == 0 {
		return base
	}
	if slices.ContainsFunc(base, func(m event.SessionMode) bool {
		return strings.EqualFold(m.ID, autoModeID)
	}) {
		// The agent advertises its own auto; shadowing it would put two
		// entries with the same id in front of the user.
		return base
	}
	// Cloned before appending: staticModes is shared by every session built
	// from one Spec, and appending into its spare capacity would write through
	// to another session's list. Appended last so the normal mode stays the
	// menu head (MADR 0047 D1, MADR 0049 D3).
	return append(slices.Clone(base), syntheticAutoMode())
}

// reportedModeID is the mode id the client should show: auto whenever it is
// armed, otherwise whatever the agent (or the static fallback) reports.
// Reporting the agent's mode while the daemon is silently auto-approving would
// be the same lie MADR 0047 removed from the mobile side (MADR 0049 D4).
func (s *session) reportedModeID(agentModeID string) string {
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()
	if auto {
		return autoModeID
	}
	return agentModeID
}

// normalModeID is the mode auto runs the agent in: `default` when advertised,
// else `build`, else the first that is neither plan nor auto. Mirrors
// opencode's normalAgentID so the two providers resolve "normal" the same way.
func (s *session) normalModeID() string {
	for _, want := range []string{"default", "build"} {
		for _, m := range s.staticModes {
			if strings.EqualFold(m.ID, want) {
				return m.ID
			}
		}
	}
	for _, m := range s.staticModes {
		if !strings.EqualFold(m.ID, "plan") && !strings.EqualFold(m.ID, autoModeID) {
			return m.ID
		}
	}
	return ""
}

func (s *session) emitModes(st *acp.SessionModeState) {
	if st == nil {
		return
	}
	modes := make([]event.SessionMode, 0, len(st.AvailableModes))
	for _, m := range st.AvailableModes {
		desc := ""
		if m.Description != nil {
			desc = *m.Description
		}
		modes = append(modes, event.SessionMode{
			ID:          string(m.Id),
			Name:        m.Name,
			Description: desc,
		})
	}
	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		Modes:         s.advertisedModes(modes),
		CurrentModeID: s.reportedModeID(string(st.CurrentModeId)),
	})
}

// emitModesOrStatic forwards the agent's mode state, falling back to the Spec's
// static list when the agent advertised none. Grok is the case in point: it
// honors session/set_mode (ids "plan"/"default", confirmed by
// current_mode_update) but returns no modes from session/new|load, so without a
// fallback no client could ever offer the switch.
func (s *session) emitModesOrStatic(st *acp.SessionModeState) {
	if st != nil && len(st.AvailableModes) > 0 {
		s.emitModes(st)
		return
	}
	if len(s.staticModes) == 0 {
		return
	}
	current := s.defaultModeID
	if current == "" {
		current = s.staticModes[0].ID
	}
	// Records that the advertised vocabulary is ours, which is what makes id
	// checking in SetMode correct (an agent-supplied list stays authoritative).
	s.mu.Lock()
	s.syntheticModes = true
	s.mu.Unlock()
	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		Modes:         s.advertisedModes(s.staticModes),
		CurrentModeID: s.reportedModeID(current),
	})
}

// emitConfigOptions forwards the agent's session config options as a
// session_config event. No-op when the agent exposed none. Only the stable
// select and boolean option kinds are mapped.
func (s *session) emitConfigOptions(opts []acp.SessionConfigOption) {
	if len(opts) == 0 {
		return
	}
	out := make([]event.ConfigOption, 0, len(opts))
	for _, o := range opts {
		switch {
		case o.Select != nil:
			sel := o.Select
			desc := ""
			if sel.Description != nil {
				desc = *sel.Description
			}
			vals := make([]event.ConfigOptionValue, 0)
			if sel.Options.Ungrouped != nil {
				for _, v := range *sel.Options.Ungrouped {
					vals = append(vals, event.ConfigOptionValue{ID: string(v.Value), Name: v.Name})
				}
			}
			out = append(out, event.ConfigOption{
				ID:           string(sel.Id),
				Name:         sel.Name,
				Description:  desc,
				Kind:         "select",
				CurrentValue: string(sel.CurrentValue),
				Values:       vals,
			})
		case o.Boolean != nil:
			b := o.Boolean
			desc := ""
			if b.Description != nil {
				desc = *b.Description
			}
			out = append(out, event.ConfigOption{
				ID:          string(b.Id),
				Name:        b.Name,
				Description: desc,
				Kind:        "boolean",
				BoolValue:   b.CurrentValue,
			})
		}
	}
	if len(out) == 0 {
		return
	}
	s.emit(event.Event{
		Type:          event.TypeSessionConfig,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		ConfigOptions: out,
	})
}

// approvalGroupID is the stable client-side upsert key for this session's
// auto-approval card: one card per turn, replaced rather than appended.
const approvalGroupID = "auto-approvals"

// maxApprovalItems bounds the per-turn audit list, matching the other providers.
const maxApprovalItems = 512

// maxApprovalDetail caps one audit line, matching maxPermissionSummary on the
// OpenCode side.
const maxApprovalDetail = 120

// noteAutoApproval records one auto-approved ACP permission and re-publishes
// the whole list for this turn (MADR 0051 Phase 3).
//
// RequestPermission can be called concurrently by the ACP connection, so the
// snapshot is taken under s.mu and cloned before the emit — a shorter snapshot
// landing after a longer one would drop approvals from the audit under the
// event's replace semantics.
func (s *session) noteAutoApproval(params acp.RequestPermissionRequest) {
	name := "permission"
	if params.ToolCall.Title != nil && strings.TrimSpace(*params.ToolCall.Title) != "" {
		name = strings.TrimSpace(*params.ToolCall.Title)
	} else if params.ToolCall.Kind != nil {
		name = firstNonEmpty(string(*params.ToolCall.Kind), name)
	}
	// The same summariser the permission sheet uses below, so the audit can
	// never carry raw rawInput JSON that the sheet would have withheld.
	detail := summarizeToolContent(params.ToolCall.Content, params.ToolCall.RawInput, nil, maxApprovalDetail)
	s.noteAutoApprovalItem(name, detail)
}

// noteAutoApprovalItem is the shared audit append used by the hot auto path and
// by sweepPendingPermissions (which has no ACP request params left).
func (s *session) noteAutoApprovalItem(name, detail string) {
	if strings.TrimSpace(name) == "" {
		name = "permission"
	}
	s.mu.Lock()
	now := time.Now().UTC()
	s.autoApprovals = append(s.autoApprovals, event.ApprovalItem{
		ToolName: name,
		Detail:   detail,
		Time:     now,
	})
	if n := len(s.autoApprovals); n > maxApprovalItems {
		s.autoApprovals = append([]event.ApprovalItem(nil), s.autoApprovals[n-maxApprovalItems:]...)
	}
	out := slices.Clone(s.autoApprovals)
	s.mu.Unlock()

	s.emit(event.Event{
		Type:            event.TypeApprovalSummary,
		SessionID:       s.localID,
		Timestamp:       now,
		ApprovalGroupID: approvalGroupID,
		Approvals:       out,
		Status:          event.ApprovalStatusRunning,
		Text:            approvalFallbackText(len(out)),
	})
}

// finishApprovals publishes the terminal summary for a turn and clears the
// list. Called when a turn ends and when the mode leaves auto.
func (s *session) finishApprovals() {
	s.mu.Lock()
	items := slices.Clone(s.autoApprovals)
	s.autoApprovals = nil
	s.mu.Unlock()
	if len(items) == 0 {
		return
	}
	s.emit(event.Event{
		Type:            event.TypeApprovalSummary,
		SessionID:       s.localID,
		Timestamp:       time.Now().UTC(),
		ApprovalGroupID: approvalGroupID,
		Approvals:       items,
		Status:          event.ApprovalStatusCompleted,
		Text:            approvalFallbackText(len(items)),
	})
}

// approvalFallbackText is what a client that does not understand
// approval_summary renders instead: one system line, not twenty.
func approvalFallbackText(n int) string {
	return "Auto-approved (" + strconv.Itoa(n) + ")"
}

func (s *session) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()
	// cfg.AlwaysApprove stays first so operator config behaves exactly as
	// before; the per-session arm is the synthetic auto mode (MADR 0049 D5).
	if s.cfg.AlwaysApprove || auto {
		// ACP auto-approval used to leave no record at all: grok and goose
		// answered silently and the user had no way to scroll back and see what
		// ran on their behalf. One collapsing card per turn, same contract as
		// the other providers (MADR 0051 Phase 3).
		s.noteAutoApproval(params)
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

	res := s.awaitDecision(ctx, permID, event.Event{
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
	if res.cancelled || res.optionID == "" {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}, nil
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{
				OptionId: acp.PermissionOptionId(res.optionID),
				Outcome:  "selected",
			},
		},
	}, nil
}

// awaitDecision emits req (a permission_request already carrying permID) and
// blocks until the client answers, the agent abandons the request, or
// PermissionTimeout elapses. It owns the whole transcript side of the exchange —
// the matching permission_resolved is emitted on every path — so callers only
// translate the returned decision into their own protocol's reply. A cancelled
// result means no decision was applied.
//
// Shared by ACP's session/request_permission and grok's plan-approval extension
// (see extensions.go): both are "ask the phone to pick an option", and routing
// both through here means one client answer path (RespondPermission), one
// timeout policy, and one Cancel/Close release path.
func (s *session) awaitDecision(ctx context.Context, permID string, req event.Event) permResult {
	w := &permWaiter{
		ch:            make(chan permResult, 1),
		allowOptionID: pickAllowOptionID(req.Options),
		toolName:      firstNonEmpty(req.ToolName, "permission"),
		detail:        req.Text,
	}
	s.mu.Lock()
	if s.closed {
		// Best-effort: the stream is already torn down here (Close drains and
		// announces anything that was actually pending), and no
		// permission_request was ever emitted for this id, so nobody is waiting.
		s.emitLocked(s.permissionResolved(permID, event.PermissionStatusCancelled, "", ""))
		s.mu.Unlock()
		return permResult{cancelled: true}
	}
	s.pending[permID] = w
	s.mu.Unlock()

	// claim atomically flips this waiter to resolved and retires it from pending.
	// It returns true only for the party that flips it — so a ctx/timeout branch
	// that loses the race to a just-landed RespondPermission gets false and
	// defers to the channel result instead of stamping a conflicting outcome.
	claim := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		cur, ok := s.pending[permID]
		if !ok || cur != w || w.resolved {
			return false
		}
		w.resolved = true
		delete(s.pending, permID)
		return true
	}

	s.emit(req)

	// Optional safety valve: stop waiting after a bounded time so a missed
	// notification cannot hang the agent forever. A zero timeout leaves the
	// channel nil, which blocks in select — i.e. waits indefinitely as before.
	var timeout <-chan time.Time
	if s.cfg.PermissionTimeout > 0 {
		t := time.NewTimer(s.cfg.PermissionTimeout)
		defer t.Stop()
		timeout = t.C
	}

	// applyResult records a decision that arrived on the channel (a client answer
	// or a Cancel/Close cancellation) in the transcript.
	applyResult := func(res permResult) permResult {
		if res.cancelled || res.optionID == "" {
			// A cancelled decision must not masquerade as "resolved" in the
			// transcript: cancelled means no decision was applied. Still
			// carries res.deviceID when a real device sent the cancel — empty
			// only when res itself came from an internal path (session close,
			// sweep).
			s.emit(s.permissionResolved(permID, event.PermissionStatusCancelled, res.deviceID, ""))
			return permResult{cancelled: true}
		}
		s.emit(s.permissionResolved(permID, event.PermissionStatusResolved, res.deviceID, res.optionID))
		return res
	}

	select {
	case <-ctx.Done():
		// Abandoned: without this the client composer stays locked forever. If a
		// RespondPermission beat us to the claim, honor its answer instead.
		if !claim() {
			return applyResult(<-w.ch)
		}
		s.emit(s.permissionResolved(permID, event.PermissionStatusCancelled, "", ""))
		return permResult{cancelled: true}
	case <-timeout:
		// Timed out waiting for a decision: treat as cancelled (fail safe) and
		// tell the user why, so the agent unblocks instead of hanging. A
		// RespondPermission that landed in the same instant wins the claim.
		if !claim() {
			return applyResult(<-w.ch)
		}
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text: fmt.Sprintf(
				"Permission request timed out after %s — the agent stopped "+
					"waiting. Prompt again to retry.", s.cfg.PermissionTimeout),
		})
		s.emit(s.permissionExpired(permID))
		return permResult{cancelled: true}
	case res := <-w.ch:
		// Whoever sent here already retired the id (RespondPermission via claim,
		// Cancel/Close via the pending swap), so we only render the outcome.
		return applyResult(res)
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

// pickAllowOptionID is the event.PermissionOption twin of autoAllow: which
// option id a sweep should select for an already-open permission sheet.
func pickAllowOptionID(opts []event.PermissionOption) string {
	var first string
	for _, o := range opts {
		if first == "" {
			first = o.OptionID
		}
		k := strings.ToLower(o.Kind)
		n := strings.ToLower(o.Name)
		if strings.Contains(k, "allow") || strings.Contains(n, "allow") ||
			strings.Contains(k, "approve") || strings.Contains(n, "approve") {
			return o.OptionID
		}
	}
	return first
}

func (s *session) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	path := params.Path
	if !filepath.IsAbs(path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("path must be absolute: %s", path)
	}
	if err := s.auditFSAccess("read_file", path); err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		// The daemon performs agent file I/O itself (ACP fs caps), so an OS
		// denial happens HERE under the daemon's identity — on macOS that
		// includes TCC privacy protection. Say so: the bare PathError reads
		// like an agent-side bug (MADR 0069 F6 #2, D4).
		if agenterr.IsPermission(err) {
			return acp.ReadTextFileResponse{}, fmt.Errorf(
				"%w — the mcremote daemon lacks OS permission for this path "+
					"(macOS: grant Full Disk Access; see docs/ops-macos-tcc.md)", err)
		}
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
	if err := s.auditFSAccess("write_file", path); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, wrapFSPermission(err)
	}
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, wrapFSPermission(err)
	}
	return acp.WriteTextFileResponse{}, nil
}

// wrapFSPermission annotates an OS permission denial from the daemon's own
// file I/O (see ReadTextFile — same rationale, MADR 0069 D4); other errors
// pass through untouched.
func wrapFSPermission(err error) error {
	if err == nil || !agenterr.IsPermission(err) {
		return err
	}
	return fmt.Errorf(
		"%w — the mcremote daemon lacks OS permission for this path "+
			"(macOS: grant Full Disk Access; see docs/ops-macos-tcc.md)", err)
}

// auditFSAccess emits a tool event for an agent filesystem callback (so the
// activity is visible in history and on the phone) and, when Config.FSRoots
// confinement is set, refuses a path that resolves outside the allowed roots or
// the session cwd. The event is emitted either way — a refused access is
// recorded with a "failed" status — so the audit trail is complete regardless
// of the policy. Confinement is defense-in-depth: the agent has terminal access
// as the same user, so this is a policy gate and audit surface, not a sandbox.
func (s *session) auditFSAccess(op, path string) error {
	allowed := true
	if len(s.cfg.FSRoots) > 0 {
		roots := append([]string{s.cwd}, s.cfg.FSRoots...)
		allowed = pathWithinRoots(path, roots)
	}
	status := "completed"
	if !allowed {
		status = "failed"
	}
	s.emit(event.Event{
		Type:      event.TypeToolCall,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		ToolID:    uuid.NewString(),
		ToolName:  op,
		ToolKind:  "fs",
		Status:    status,
		Text:      path,
	})
	if !allowed {
		return fmt.Errorf("path %q is outside the permitted filesystem roots", path)
	}
	return nil
}

// pathWithinRoots reports whether an absolute path resolves within one of the
// roots after symlink evaluation, so a symlink inside an allowed root cannot be
// used to escape it. A not-yet-existing leaf (a write target) is resolved via
// its nearest existing ancestor. An empty root is ignored.
func pathWithinRoots(path string, roots []string) bool {
	real := resolveExistingAncestor(path)
	for _, root := range roots {
		if root == "" {
			continue
		}
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			r = filepath.Clean(root)
		}
		if real == r || strings.HasPrefix(real, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// resolveExistingAncestor returns path with symlinks resolved. When the leaf
// does not exist yet, it resolves the nearest existing ancestor and rejoins the
// not-yet-created tail, so a write is still checked against real (post-symlink)
// directories rather than a spoofable literal path.
func resolveExistingAncestor(path string) string {
	path = filepath.Clean(path)
	cur := path
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path // nothing along the path exists
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
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

// rawRequest sends a JSON-RPC request over the ACP connection and decodes the result into out.
// Used for methods acp-go-sdk@v0.13.5 does not model (session/set_model, _x.ai/session/fork).
func (s *session) rawRequest(ctx context.Context, method string, params any, out any) error {
	if s.conn == nil {
		return errors.New("no active connection")
	}
	rawConn := *(**acp.Connection)(unsafe.Pointer(s.conn))
	rawResp, err := acp.SendRequest[json.RawMessage](rawConn, ctx, method, params)
	if err != nil {
		return err
	}
	if out != nil && len(rawResp) > 0 {
		if err := json.Unmarshal(rawResp, out); err != nil {
			return fmt.Errorf("rawRequest decode: %w", err)
		}
	}
	return nil
}
