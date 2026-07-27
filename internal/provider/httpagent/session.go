package httpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/chunkbuf"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/sessionutil"
)

// session is one server-side session multiplexed over the shared engine.
// There is no per-session process: Close tears down only local state, which
// is what makes resume free. All protocol semantics live in s.ds.
type session struct {
	p       *Provider
	ds      DialectSession
	localID string
	agentID string
	cwd     string
	model   string
	// agent is the optional OpenCode agent name for prompt_async (Sprint 3).
	agent  string
	log    *slog.Logger
	events chan event.Event
	done   chan struct{}

	// emitMu serializes the streaming coalescer AND the delivery of what it
	// produces (MADR 0024). Two goroutines call Emit — the engine-wide SSE
	// reader and the MADR 0014 resync — and holding this only across the
	// buffer op would let a boundary event overtake a timed flush, landing
	// turn_complete ahead of the text it terminates.
	//
	// Lock order is s.mu -> s.emitMu, never the reverse. Nothing held under
	// emitMu takes s.mu or the dialect's mutex.
	emitMu sync.Mutex
	chunks *chunkbuf.Buffer

	// flushMu guards flushTimer only. The timer is armed once per run and
	// never Reset: resetting per event is exactly why the manager's
	// history-persist debounce never fires under a continuous stream.
	flushMu    sync.Mutex
	flushTimer *time.Timer

	mu         sync.Mutex
	closed     bool
	turnActive bool
	// promptInFlight covers the window between claiming the turn and the
	// dialect's submit call returning. Resync must not run inside it: the
	// engine does not know about the turn yet, so its message log would read
	// as a finished previous turn and falsely end this one.
	promptInFlight bool
	// turnStartedAt is when the active turn was claimed; resync ignores engine
	// evidence of turns that finished before it.
	turnStartedAt time.Time
	// pending permission requests by id (answered via the dialect's REST op).
	pending map[string]struct{}
	// questionPending tracks open question forms (MADR 0020 Sprint 1b).
	questionPending map[string]struct{}
	// permOrigin maps permission id → agent session id that asked (MADR 0020).
	permOrigin map[string]string
	// treeNodes tracks liveness of parent + children for tree-idle EndTurn.
	// Key: agent session id; parent uses s.agentID.
	treeNodes map[string]NodeStatus
	// confirmInFlight prevents stacked idle-confirm REST calls.
	confirmInFlight bool
	// eventAgentID is the agent session id of the SSE frame being handled.
	eventAgentID string
	// promptQueue holds prompts accepted while a turn was already active
	// (MADR 0020 Sprint 3 / PR7b). FIFO; drained after tree-idle EndTurn when
	// no permission/question is pending. Overflow returns ErrTurnBusy.
	promptQueue [][]provider.Content
	// drainDue records that a turn ended with the queue non-empty. The drain
	// itself is deferred to [flushDrain] rather than run inside EndTurn: the
	// dialect emits its turn_complete/idle events AFTER EndTurn returns, so
	// draining there put the next turn's "running" BEFORE the previous turn's
	// "idle" — leaving the manager (which tracks the last status event) showing
	// idle while the queued turn was actually running.
	drainDue bool

	lastActivity atomic.Int64

	// lastResyncAt tracks when the last stall-triggered resync ran, so the
	// watchdog does not fire an expensive multi-REST recovery on every tick
	// when the agent is genuinely still working.
	lastResyncAt atomic.Int64
}

// maxPromptQueue is the per-session FIFO depth for second prompts while busy.
// Excess prompts return provider.ErrTurnBusy (Owner Q1 queue with overflow).
const maxPromptQueue = 4

// defaultStreamCoalesce caps mid-stream assistant/thought updates at ~12 per
// second (MADR 0024). It sits comfortably inside the mobile client's 32ms
// event batch window and well inside its 120ms streaming-markdown throttle
// tier, so finer updates were being coalesced away by the phone regardless —
// they cost a WebSocket frame, a JSON decode and a history-ring slot each and
// bought nothing.
const defaultStreamCoalesce = 80 * time.Millisecond

// maxPendingChunkBytes force-flushes a run that grew large before its window
// elapsed. At normal token rates (~200 B/s) this never fires: it is a cap on
// catch-up bursts (MADR 0014 resync tails, message-log replay), not a knob.
const maxPendingChunkBytes = 8 << 10

// chunkRetryDelay re-arms a flush whose non-blocking send lost to a slow pump.
// A var so tests can shorten it.
var chunkRetryDelay = 50 * time.Millisecond

var _ provider.Session = (*session)(nil)
var _ provider.PermissionSession = (*session)(nil)
var _ provider.QuestionSession = (*session)(nil)
var _ provider.CWDSession = (*session)(nil)
var _ provider.ForkSession = (*session)(nil)
var _ provider.RevertSession = (*session)(nil)
var _ provider.DiffSession = (*session)(nil)
var _ provider.ModeSession = (*session)(nil)
var _ provider.CompactSession = (*session)(nil)
var _ provider.ModelSession = (*session)(nil)
var _ provider.UndoSession = (*session)(nil)
var _ provider.RenameSession = (*session)(nil)
var _ provider.DiagnosticsSession = (*session)(nil)
var _ Host = (*session)(nil)

// dialectFork is optionally implemented by a DialectSession (OpenCode).
type dialectFork interface {
	Fork(ctx context.Context, messageID string) (string, error)
}

// dialectRevert is optionally implemented by a DialectSession (OpenCode).
type dialectRevert interface {
	Revert(ctx context.Context, messageID, partID string) error
	Unrevert(ctx context.Context) error
}

// dialectDiff is optionally implemented by a DialectSession (OpenCode).
type dialectDiff interface {
	Diff(ctx context.Context, messageID string) (string, error)
}

// dialectMode is optionally implemented by a DialectSession whose provider
// exposes switchable operating modes (OpenCode: its primary agents). SetMode
// returns the id now current so the transport can confirm it to clients.
type dialectMode interface {
	SetMode(ctx context.Context, modeID string) (currentID string, err error)
}

// dialectCompact is optionally implemented by a DialectSession whose engine can
// summarise the conversation in place (OpenCode).
type dialectCompact interface {
	Compact(ctx context.Context) error
}

// dialectModel is optionally implemented by a DialectSession whose engine can
// change the session's model without a restart (OpenCode).
type dialectModel interface {
	SetModel(ctx context.Context, model string) error
}

// dialectUndo is optionally implemented by a DialectSession that can revert the
// last turn on its own, without a message id from the caller (OpenCode).
type dialectUndo interface {
	UndoLast(ctx context.Context) (string, error)
}

type dialectRename interface {
	Rename(ctx context.Context, title string) error
}

type dialectDiagnostics interface {
	Diagnostics(ctx context.Context) (provider.Diagnostics, error)
}

// Fork implements [provider.ForkSession].
func (s *session) Fork(ctx context.Context, messageID string) (string, error) {
	f, ok := s.ds.(dialectFork)
	if !ok {
		return "", fmt.Errorf("fork not supported by this provider")
	}
	return f.Fork(ctx, messageID)
}

// Revert implements [provider.RevertSession].
func (s *session) Revert(ctx context.Context, messageID, partID string) error {
	r, ok := s.ds.(dialectRevert)
	if !ok {
		return fmt.Errorf("revert not supported by this provider")
	}
	return r.Revert(ctx, messageID, partID)
}

// Unrevert implements [provider.RevertSession].
func (s *session) Unrevert(ctx context.Context) error {
	r, ok := s.ds.(dialectRevert)
	if !ok {
		return fmt.Errorf("unrevert not supported by this provider")
	}
	return r.Unrevert(ctx)
}

// Diff implements [provider.DiffSession].
func (s *session) Diff(ctx context.Context, messageID string) (string, error) {
	d, ok := s.ds.(dialectDiff)
	if !ok {
		return "", fmt.Errorf("diff not supported by this provider")
	}
	return d.Diff(ctx, messageID)
}

// SetMode implements [provider.ModeSession]. The dialect owns what a mode means
// (OpenCode: the primary agent a prompt runs under) and reports the id now
// current; the confirming session_mode event is emitted here so every mode path
// — this op, the /plan builtin, a dialect-side change — looks the same to
// clients.
func (s *session) SetMode(ctx context.Context, modeID string) error {
	m, ok := s.ds.(dialectMode)
	if !ok {
		return fmt.Errorf("modes not supported by this provider")
	}
	current, err := m.SetMode(ctx, modeID)
	if err != nil {
		return err
	}
	s.Emit(event.Event{Type: event.TypeMode, CurrentModeID: current})
	return nil
}

// Compact implements [provider.CompactSession].
func (s *session) Compact(ctx context.Context) error {
	c, ok := s.ds.(dialectCompact)
	if !ok {
		return fmt.Errorf("compaction not supported by this provider")
	}
	return c.Compact(ctx)
}

// SetModel implements [provider.ModelSession].
func (s *session) SetModel(ctx context.Context, model string) error {
	m, ok := s.ds.(dialectModel)
	if !ok {
		return fmt.Errorf("in-place model switching not supported by this provider")
	}
	return m.SetModel(ctx, model)
}

// Rename implements provider.RenameSession when the dialect owns a
// provider-native title operation.
func (s *session) Rename(ctx context.Context, title string) error {
	r, ok := s.ds.(dialectRename)
	if !ok {
		return fmt.Errorf("session does not support rename")
	}
	return r.Rename(ctx, title)
}

// Diagnostics implements provider.DiagnosticsSession when the dialect exposes
// bounded, read-only project metadata.
func (s *session) Diagnostics(ctx context.Context) (provider.Diagnostics, error) {
	d, ok := s.ds.(dialectDiagnostics)
	if !ok {
		return provider.Diagnostics{}, fmt.Errorf("session does not support diagnostics")
	}
	return d.Diagnostics(ctx)
}

// UndoLast implements [provider.UndoSession].
func (s *session) UndoLast(ctx context.Context) (string, error) {
	u, ok := s.ds.(dialectUndo)
	if !ok {
		return "", fmt.Errorf("undo not supported by this provider")
	}
	return u.UndoLast(ctx)
}

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return s.p.dialect.ID() }
func (s *session) AgentSessionID() string     { return s.agentID }
func (s *session) CWD() string                { return s.cwd }
func (s *session) Config() Config             { return s.p.cfg }
func (s *session) Log() *slog.Logger          { return s.log }
func (s *session) API() API                   { return s.p.api }
func (s *session) Events() <-chan event.Event { return s.events }

// Agent/SetAgent and Model/SetModel bracket the mutable pieces of session
// identity: a mode switch rewrites the agent (SetMode) and an in-place model
// switch rewrites the model (SetModel), while the SSE goroutine reads both to
// build the next prompt — so all four go through the mutex.
func (s *session) Agent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agent
}

func (s *session) SetAgent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agent = name
}

func (s *session) Model() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

func (s *session) RecordModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
}

// Start creates (or re-attaches to) a server-side session.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("%s binary %q not found in PATH: %w",
			p.dialect.ID(), p.cfg.Bin, provider.ErrNotImplemented)
	}

	cwd := opts.CWD
	if cwd == "" {
		cwd = p.cfg.DefaultCWD
	}
	if cwd == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for session cwd: %w", err)
		}
		cwd = home
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("cwd %q is not a directory", cwd)
	}

	startCtx, cancel := context.WithTimeout(ctx, serverStartTimeout)
	defer cancel()
	base, err := p.ensureServer(startCtx)
	if err != nil {
		return nil, fmt.Errorf("%s server: %w", p.cfg.Bin, err)
	}
	// MADR 0020 KD10: refuse session-tree mode on engines older than the pin.
	if vg, ok := p.dialect.(VersionGate); ok {
		if err := vg.CheckMinVersion(p.cfg); err != nil {
			return nil, err
		}
	}
	if agent := strings.TrimSpace(opts.Agent); agent != "" {
		if av, ok := p.dialect.(StartAgentValidator); ok {
			canonical, err := av.ValidateStartAgent(startCtx, p.apiAt(base), cwd, agent)
			if err != nil {
				return nil, err
			}
			opts.Agent = canonical
		}
	}

	localID := opts.LocalSessionID
	if localID == "" {
		localID = uuid.NewString()
	}

	model := opts.Model
	if model == "" {
		model = p.cfg.Model
	}

	s := &session{
		p:               p,
		localID:         localID,
		cwd:             cwd,
		model:           model,
		agent:           strings.TrimSpace(opts.Agent),
		log:             p.log.With(slog.String("session_id", localID)),
		events:          make(chan event.Event, 256),
		done:            make(chan struct{}),
		chunks:          chunkbuf.New(p.cfg.StreamCoalesceWindow(), maxPendingChunkBytes, chunkbuf.WithToolLane()),
		pending:         make(map[string]struct{}),
		questionPending: make(map[string]struct{}),
		permOrigin:      make(map[string]string),
		treeNodes:       make(map[string]NodeStatus),
	}
	s.ds = p.dialect.NewSession(s)

	if opts.AgentSessionID != "" {
		// Resume: the session already lives on the server — no replay, no
		// engine work. Verify it exists, then rebuild our history ring from
		// its message log (marked Replay so clients don't double-append).
		agentID, err := s.ds.Resume(startCtx, opts.AgentSessionID)
		if err != nil {
			return nil, fmt.Errorf("%s session resume: %w", p.dialect.ID(), err)
		}
		s.agentID = agentID
		if err := p.register(s); err != nil {
			return nil, fmt.Errorf("%s session resume: %w", p.dialect.ID(), err)
		}
		s.ds.Replay(startCtx)
		s.log.Info("http session resumed", slog.String("agent_session_id", s.agentID))
	} else {
		agentID, err := s.ds.Create(startCtx, opts)
		if err != nil {
			return nil, fmt.Errorf("%s session create: %w", p.dialect.ID(), err)
		}
		if agentID == "" {
			return nil, fmt.Errorf("%s session create: empty id", p.dialect.ID())
		}
		s.agentID = agentID
		if err := p.register(s); err != nil {
			return nil, fmt.Errorf("%s session create: %w", p.dialect.ID(), err)
		}
		s.log.Info("http session created", slog.String("agent_session_id", s.agentID))
	}

	s.Emit(event.Event{
		Type:           event.TypeSessionStatus,
		Status:         "idle",
		AgentSessionID: s.agentID,
	})
	return s, nil
}

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnActive {
		// FIFO queue (MADR 0020 Sprint 3): accept the prompt, drain after idle.
		if len(s.promptQueue) >= maxPromptQueue {
			s.mu.Unlock()
			return provider.ErrTurnBusy
		}
		s.promptQueue = append(s.promptQueue, sessionutil.CloneContent(parts))
		n := len(s.promptQueue)
		s.mu.Unlock()
		s.emitUserMessage(parts)
		s.Emit(event.Event{
			Type: event.TypeNotice,
			Text: fmt.Sprintf("Queued (%d/%d) — will send when the agent is idle", n, maxPromptQueue),
		})
		return nil
	}
	s.mu.Unlock()
	return s.beginTurn(parts, true)
}

// beginTurn claims the turn and submits parts to the dialect.
// emitUser controls whether a user_message is emitted (false when draining a
// queue entry that already showed the user bubble at enqueue time).
func (s *session) beginTurn(parts []provider.Content, emitUser bool) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnActive {
		s.mu.Unlock()
		return provider.ErrTurnBusy
	}
	s.turnActive = true
	s.promptInFlight = true
	s.turnStartedAt = time.Now()
	// New turn: parent busy; drop prior tree node status (aliases stay for demux).
	s.treeNodes = map[string]NodeStatus{s.agentID: NodeBusy}
	s.mu.Unlock()

	if emitUser {
		s.emitUserMessage(parts)
	}
	s.Emit(event.Event{Type: event.TypeSessionStatus, Status: "running"})

	// The submit call returns once the turn is enqueued; the turn itself
	// streams over SSE and ends with the dialect's turn-end event.
	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.ds.Prompt(callCtx, parts)
	s.mu.Lock()
	s.promptInFlight = false
	stillActive := s.turnActive
	s.mu.Unlock()
	if err != nil {
		// Only emit "idle" if the SSE stream hasn't already ended the turn
		// with a different status (error, idle, etc.) via HandleEvent.
		if stillActive {
			s.EndTurn()
			s.Emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
		}
		// A failed submit must not strand the rest of the queue.
		s.flushDrain()
		return err
	}

	s.lastActivity.Store(time.Now().UnixNano())
	if s.p.cfg.TurnStallNotice > 0 {
		go s.watchStall()
	}
	return nil
}

func (s *session) emitUserMessage(parts []provider.Content) {
	s.Emit(sessionutil.UserMessage(parts))
}

// tryDrainQueue starts the next queued prompt if the session is idle and no
// permission/question is outstanding (MADR 0020 queue policy).
func (s *session) tryDrainQueue() {
	s.mu.Lock()
	if s.closed || s.turnActive || len(s.promptQueue) == 0 ||
		len(s.pending) > 0 || len(s.questionPending) > 0 {
		s.mu.Unlock()
		return
	}
	next := s.promptQueue[0]
	s.promptQueue = s.promptQueue[1:]
	s.mu.Unlock()

	if err := s.beginTurn(next, false); err != nil {
		s.log.Warn("queued prompt failed", slog.String("err", err.Error()))
		s.Emit(event.Event{
			Type:  event.TypeError,
			Error: clipErr(err, 300),
		})
		// Keep draining remaining items so one failure does not strand the queue.
		s.tryDrainQueue()
	}
}

func clipErr(err error, n int) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) <= n {
		return msg
	}
	return msg[:n] + "…"
}

// stallTickInterval is how often watchStall re-checks a quiet turn. A var so
// tests can shorten it.
var stallTickInterval = 10 * time.Second

// watchStall mirrors the ACP transport's stall notice: escalating back-off
// notices while a turn is active with no SSE activity.
func (s *session) watchStall() {
	threshold := s.p.cfg.TurnStallNotice
	tick := time.NewTicker(stallTickInterval)
	defer tick.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-tick.C:
			s.mu.Lock()
			active := s.turnActive
			s.mu.Unlock()
			if !active {
				return
			}
			quiet := time.Since(time.Unix(0, s.lastActivity.Load()))
			if quiet < threshold {
				continue
			}
			// Reconcile with the engine at most once per threshold window, not
			// on every escalator tick — resync is expensive (multi-REST tree
			// scan, message-log fetch) and a genuinely long-running turn must
			// not pay it repeatedly. The first tick past threshold probes; if
			// the turn is still active on the next escalation we skip resync
			// and only nag.
			if time.Since(time.Unix(0, s.lastResyncAt.Load())) > threshold {
				s.lastResyncAt.Store(time.Now().UnixNano())
				s.resync()
				s.mu.Lock()
				active = s.turnActive
				s.mu.Unlock()
				if !active {
					return
				}
			}
			s.Emit(event.Event{
				Type: event.TypeNotice,
				Text: fmt.Sprintf(
					"Still waiting — no output from the agent for %s. It may "+
						"be working on something long, or stuck: use Stop to "+
						"cancel the turn, or /reset to restart it.",
					quiet.Round(time.Second)),
			})
			threshold *= 2
			s.lastActivity.Store(time.Now().UnixNano())
		}
	}
}

// resync asks the dialect to reconcile this session against engine state when
// SSE frames may have been missed (stream reconnect, stall watchdog). Only a
// turn-active session has in-flight state to recover, and the promptInFlight
// window is excluded — see the field comment. Bounded and best-effort.
func (s *session) resync() {
	s.mu.Lock()
	if s.closed || !s.turnActive || s.promptInFlight {
		s.mu.Unlock()
		return
	}
	started := s.turnStartedAt
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.ds.Resync(ctx, started)
	// Resync can recover a missed turn-end; hand off to the queue after its
	// turn-end events have been emitted.
	s.flushDrain()
}

func (s *session) Cancel(ctx context.Context) error {
	// Cancel clears the prompt queue — do not auto-run queued prompts after stop.
	s.mu.Lock()
	s.promptQueue = nil
	s.mu.Unlock()
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return s.ds.Abort(callCtx)
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	if !s.TakePending(permissionID) {
		return fmt.Errorf("unknown or expired permission %q", permissionID)
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.ds.RespondPermission(callCtx, permissionID, optionID, cancelled); err != nil {
		// Still outstanding server-side; allow a retry.
		s.mu.Lock()
		s.pending[permissionID] = struct{}{}
		s.mu.Unlock()
		return err
	}
	s.clearPermissionOrigin(permissionID)
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.Emit(event.Event{
		Type:         event.TypePermissionResolved,
		PermissionID: permissionID,
		Status:       status,
	})
	// Answering may unblock a waiting queue drain.
	s.tryDrainQueue()
	return nil
}

// RespondQuestion implements [provider.QuestionSession].
func (s *session) RespondQuestion(ctx context.Context, questionID string, answers [][]string, cancelled bool) error {
	if !s.TakeQuestionPending(questionID) {
		return fmt.Errorf("unknown or expired question %q", questionID)
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.ds.RespondQuestion(callCtx, questionID, answers, cancelled); err != nil {
		s.mu.Lock()
		if s.questionPending == nil {
			s.questionPending = make(map[string]struct{})
		}
		s.questionPending[questionID] = struct{}{}
		s.mu.Unlock()
		return err
	}
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.Emit(event.Event{
		Type:       event.TypeQuestionResolved,
		QuestionID: questionID,
		Status:     status,
	})
	s.tryDrainQueue()
	return nil
}

// Purge removes the server-side session. Implements [provider.PurgeSession] for
// session.delete.
//
// Local teardown happens FIRST, before the server-side delete: unregister stops
// the shared SSE pump from routing (and so blocking an Emit) into this session,
// and close(done) unblocks any control Emit already parked on a full buffer.
// Otherwise a full 256-event buffer plus the up-to-15s delete round-trip would
// stall the engine-wide pump — and every other session on it — for that window.
// Close is idempotent and reports no error; the engine error (if any) is the
// caller's signal.
func (s *session) Purge(ctx context.Context) error {
	_ = s.Close(ctx)
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return s.ds.Delete(callCtx)
}

// Close releases local state only: the server-side session persists (that is
// exactly what makes resume instant). Use [Purge] when the user ends a session
// for good (daemon session.delete).
func (s *session) Close(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	pending := s.pending
	s.pending = map[string]struct{}{}
	qPending := s.questionPending
	s.questionPending = map[string]struct{}{}
	s.promptQueue = nil
	s.mu.Unlock()

	// Before close(s.done), so the tail of an interrupted reply still reaches
	// the transcript instead of dying in the coalescer (MADR 0024).
	s.drainChunks()

	close(s.done)
	for id := range pending {
		ev := event.Event{
			Type:         event.TypePermissionResolved,
			SessionID:    s.localID,
			Timestamp:    time.Now().UTC(),
			PermissionID: id,
			Status:       event.PermissionStatusCancelled,
		}
		select {
		case s.events <- ev:
		default:
		}
	}
	for id := range qPending {
		ev := event.Event{
			Type:       event.TypeQuestionResolved,
			SessionID:  s.localID,
			Timestamp:  time.Now().UTC(),
			QuestionID: id,
			Status:     event.PermissionStatusCancelled,
		}
		select {
		case s.events <- ev:
		default:
		}
	}
	s.p.unregister(s)
	return nil
}

var _ provider.PurgeSession = (*session)(nil)

// serverDied is invoked by the provider's death monitor.
func (s *session) serverDied() {
	s.Emit(event.Event{Type: event.TypeError,
		Error: fmt.Sprintf("%s server exited", s.p.cfg.Bin)})
	s.Emit(event.Event{Type: event.TypeSessionStatus, Status: "disconnected"})
}

// dispatch routes one SSE event into the dialect, stamping liveness for the
// stall watchdog and the agent sid of the current frame first.
func (s *session) dispatch(typ string, props json.RawMessage, agentSID string) {
	s.lastActivity.Store(time.Now().UnixNano())
	s.mu.Lock()
	s.eventAgentID = agentSID
	s.mu.Unlock()
	s.ds.HandleEvent(typ, props)
	s.mu.Lock()
	s.eventAgentID = ""
	s.mu.Unlock()
	// The dialect has finished emitting (including any turn-end events), so a
	// turn it ended can now hand off to the next queued prompt.
	s.flushDrain()
}

// EventAgentSessionID implements [Host].
func (s *session) EventAgentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eventAgentID
}

// EndTurn implements [Host]. The queue drain it arms runs in [flushDrain],
// after the caller has finished emitting its turn-end events.
func (s *session) EndTurn() bool {
	s.mu.Lock()
	was := s.turnActive
	s.turnActive = false
	if was {
		s.drainDue = true
	}
	s.mu.Unlock()
	return was
}

// flushDrain runs a queue drain armed by EndTurn / TryEndTurnIfTreeIdle. Call
// it once the turn-end events for the turn that just finished have been
// emitted, so the drained turn's "running" cannot precede them.
func (s *session) flushDrain() {
	s.mu.Lock()
	due := s.drainDue
	s.drainDue = false
	s.mu.Unlock()
	if due {
		s.tryDrainQueue()
	}
}

// BindChildAlias implements [Host].
func (s *session) BindChildAlias(childAgentID string) {
	if !s.p.cfg.treeEnabled() {
		return
	}
	if childAgentID == "" || childAgentID == s.agentID {
		return
	}
	s.p.bindChild(childAgentID, s)
	s.noteChildBound(childAgentID)
}

// UnbindChildAlias implements [Host].
func (s *session) UnbindChildAlias(childAgentID string) {
	if childAgentID == "" {
		return
	}
	if !s.p.cfg.treeEnabled() {
		return
	}
	s.p.unbindChild(childAgentID, s)
	s.mu.Lock()
	delete(s.treeNodes, childAgentID)
	s.mu.Unlock()
}

// noteChildBound records a newly bound child as busy for tree EndTurn.
// Safe to call from the provider under p.mu (uses only s.mu).
func (s *session) noteChildBound(childAgentID string) {
	if childAgentID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.treeNodes == nil {
		s.treeNodes = make(map[string]NodeStatus)
	}
	if _, ok := s.treeNodes[childAgentID]; !ok {
		s.treeNodes[childAgentID] = NodeBusy
	}
}

// NoteNodeStatus implements [Host].
func (s *session) NoteNodeStatus(agentSessionID string, status NodeStatus) {
	if status != NodeIdle && status != NodeBusy && status != NodeRetry {
		return
	}
	id := agentSessionID
	if id == "" {
		id = s.agentID
	}
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.treeNodes == nil {
		s.treeNodes = make(map[string]NodeStatus)
	}
	s.treeNodes[id] = status
}

// TryEndTurnIfTreeIdle implements [Host].
func (s *session) TryEndTurnIfTreeIdle() bool {
	// KD11 kill switch: parent-only EndTurn, no idle-confirm REST.
	if !s.p.cfg.treeEnabled() {
		return s.EndTurn()
	}
	s.mu.Lock()
	if s.closed || !s.turnActive {
		s.mu.Unlock()
		return false
	}
	if s.treeNodes == nil {
		s.treeNodes = make(map[string]NodeStatus)
	}
	// Ensure parent is present; missing parent with only children is odd but
	// treat missing parent as idle so known-busy children still block.
	if _, ok := s.treeNodes[s.agentID]; !ok && s.agentID != "" {
		s.treeNodes[s.agentID] = NodeIdle
	}
	for _, st := range s.treeNodes {
		if NodeBusyForEndTurn(st) {
			s.mu.Unlock()
			return false
		}
	}
	// Snapshot tree for optional REST confirm (must not hold s.mu across I/O).
	parentID := s.agentID
	known := make([]string, 0, len(s.treeNodes))
	for id := range s.treeNodes {
		known = append(known, id)
	}
	if s.confirmInFlight {
		s.mu.Unlock()
		return false
	}
	confirmer, ok := s.ds.(TreeIdleConfirmer)
	if !ok || parentID == "" {
		was := s.turnActive
		s.turnActive = false
		if was {
			s.drainDue = true
		}
		s.mu.Unlock()
		return was
	}
	s.confirmInFlight = true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	stillBusy, discovered, err := confirmer.ConfirmTreeIdle(ctx, parentID, known)
	cancel()

	s.mu.Lock()
	s.confirmInFlight = false
	if s.closed || !s.turnActive {
		s.mu.Unlock()
		return false
	}
	if err != nil {
		// Cannot confirm idle — keep the turn active (MADR 0020).
		s.mu.Unlock()
		return false
	}
	// discovered ids were included in the confirmer's status probe, so stillBusy
	// below is the authoritative answer for them. Seeding them busy here instead
	// (as this used to) wedged the turn permanently: every child a session ever
	// spawned is still listed by the engine on later turns, and a child the
	// engine reports idle never sends another frame to clear the flag.
	for _, id := range discovered {
		if id == "" || id == parentID {
			continue
		}
		if _, ok := s.treeNodes[id]; !ok {
			s.treeNodes[id] = NodeIdle
		}
	}
	for _, id := range stillBusy {
		if id != "" {
			s.treeNodes[id] = NodeBusy
		}
	}
	// Bind discovered children after releasing s.mu (bind takes p.mu).
	toBind := append([]string(nil), discovered...)
	for _, st := range s.treeNodes {
		if NodeBusyForEndTurn(st) {
			s.mu.Unlock()
			for _, id := range toBind {
				s.p.bindChild(id, s)
			}
			return false
		}
	}
	was := s.turnActive
	s.turnActive = false
	if was {
		s.drainDue = true
	}
	s.mu.Unlock()
	for _, id := range toBind {
		s.p.bindChild(id, s)
	}
	return was
}

// TrackPermission implements [Host].
func (s *session) TrackPermission(id string) {
	s.mu.Lock()
	s.pending[id] = struct{}{}
	s.mu.Unlock()
	if s.p.cfg.PermissionTimeout > 0 {
		go s.expirePermission(id)
	}
}

// TrackPermissionOrigin implements [Host].
func (s *session) TrackPermissionOrigin(permissionID, agentSessionID string) {
	if permissionID == "" {
		return
	}
	origin := agentSessionID
	if origin == "" {
		origin = s.agentID
	}
	s.mu.Lock()
	if s.permOrigin == nil {
		s.permOrigin = make(map[string]string)
	}
	s.permOrigin[permissionID] = origin
	s.mu.Unlock()
}

// PermissionOrigin implements [Host].
func (s *session) PermissionOrigin(permissionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.permOrigin[permissionID]; ok && id != "" {
		return id
	}
	return s.agentID
}

// TakePending implements [Host].
// Origin is kept until a successful dialect reply (or explicit clear) so
// RespondPermission can still route to the child/parent REST path.
func (s *session) TakePending(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[id]
	delete(s.pending, id)
	return ok
}

func (s *session) clearPermissionOrigin(id string) {
	s.mu.Lock()
	delete(s.permOrigin, id)
	s.mu.Unlock()
}

// expirePermission mirrors the ACP transport's fail-safe: reject server-side
// after the timeout so a missed notification can't hang the agent forever.
func (s *session) expirePermission(id string) {
	select {
	case <-s.done:
		return
	case <-time.After(s.p.cfg.PermissionTimeout):
	}
	if !s.TakePending(id) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.ds.RespondPermission(ctx, id, "", true)
	s.clearPermissionOrigin(id)
	s.Emit(event.Event{
		Type: event.TypeNotice,
		Text: fmt.Sprintf("Permission request timed out after %s — the agent "+
			"stopped waiting. Prompt again to retry.", s.p.cfg.PermissionTimeout),
	})
	s.Emit(event.Event{
		Type:         event.TypePermissionResolved,
		PermissionID: id,
		Status:       event.PermissionStatusCancelled,
	})
}

// TrackQuestion implements [Host].
func (s *session) TrackQuestion(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	if s.questionPending == nil {
		s.questionPending = make(map[string]struct{})
	}
	s.questionPending[id] = struct{}{}
	s.mu.Unlock()
	if s.p.cfg.PermissionTimeout > 0 {
		go s.expireQuestion(id)
	}
}

// TakeQuestionPending implements [Host].
func (s *session) TakeQuestionPending(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.questionPending[id]
	delete(s.questionPending, id)
	return ok
}

func (s *session) expireQuestion(id string) {
	select {
	case <-s.done:
		return
	case <-time.After(s.p.cfg.PermissionTimeout):
	}
	if !s.TakeQuestionPending(id) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.ds.RespondQuestion(ctx, id, nil, true)
	s.Emit(event.Event{
		Type: event.TypeNotice,
		Text: fmt.Sprintf("Question timed out after %s — the agent stopped waiting. "+
			"Prompt again to retry.", s.p.cfg.PermissionTimeout),
	})
	s.Emit(event.Event{
		Type:       event.TypeQuestionResolved,
		QuestionID: id,
		Status:     event.PermissionStatusCancelled,
	})
}

// Emit implements [Host]: delivers ev, blocking for control events until
// consumed or closed — same guarantees as the ACP transport (the manager
// pump is attached from the moment Start returns; Start emits only one
// status event before that, which the 256 buffer trivially holds).
//
// Assistant and thought text is coalesced into ~one event per
// [Config.StreamCoalesceWindow] instead of one per model token (MADR 0024).
// Two invariants hold regardless of the window:
//
//   - The first chunk after any control event is delivered immediately, so
//     time-to-first-token is unchanged.
//   - A control event is a boundary: pending text is delivered ahead of it,
//     so turn_complete can never precede the text it terminates. Every
//     turn-end path must therefore go through Emit.
func (s *session) Emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}

	s.emitMu.Lock()
	out, deadline, blocking := s.chunkBuffer().Add(ev)
	for _, e := range out {
		if blocking || event.IsControl(e.Type) {
			select {
			case s.events <- e:
			case <-s.done:
			}
			continue
		}
		if s.trySend(e) {
			continue
		}
		if !chunkbuf.IsChunk(e.Type) {
			// Telemetry (usage_update, available_commands): a stale count
			// self-corrects on the next report, so dropping stays correct.
			s.log.Warn("dropping event; slow consumer", slog.String("type", string(e.Type)))
			continue
		}
		// Reply text is never dropped: hand it back and retry on the next
		// tick. Pre-0024 this path lost the tokens outright.
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

// chunkBuffer returns the streaming coalescer, building it on first use.
// Start wires it up front; the lazy path covers sessions assembled field by
// field (tests, and any future constructor) so no build path can reach Emit
// with a nil buffer. Caller holds emitMu.
func (s *session) chunkBuffer() *chunkbuf.Buffer {
	if s.chunks == nil {
		s.chunks = chunkbuf.New(s.p.cfg.StreamCoalesceWindow(), maxPendingChunkBytes, chunkbuf.WithToolLane())
	}
	return s.chunks
}

// trySend enqueues ev without blocking, reporting whether it landed.
func (s *session) trySend(ev event.Event) bool {
	select {
	case s.events <- ev:
		return true
	default:
		return false
	}
}

// noteUnflush logs the coalescer's growth guard firing. Non-zero means the
// consumer has stopped draining entirely and streamed text was discarded.
func (s *session) noteUnflush(dropped int) {
	if dropped > 0 {
		s.log.Warn("stream buffer overflow; discarded oldest text",
			slog.Int("bytes", dropped))
	}
}

// armFlush schedules the pending run's flush for deadline. Idempotent within a
// run: an already-armed timer is left alone rather than reset, so a continuous
// stream still flushes on schedule.
func (s *session) armFlush(deadline time.Time) {
	d := max(time.Until(deadline), 0)
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if s.flushTimer != nil {
		return
	}
	s.flushTimer = time.AfterFunc(d, s.onFlushTimer)
}

// stopFlush cancels a pending flush (nothing is buffered any more).
func (s *session) stopFlush() {
	s.flushMu.Lock()
	t := s.flushTimer
	s.flushTimer = nil
	s.flushMu.Unlock()
	if t != nil {
		t.Stop()
	}
}

// onFlushTimer delivers the coalesced run. It never blocks: only the boundary
// path in Emit does, so a stalled pump cannot wedge a timer goroutine.
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
	// Held tool updates are control events: deliver them with the blocking
	// send, never trySend. Dropping one would pin a card on "running" forever,
	// which is why the lane holds only non-terminal states in the first place.
	for _, t := range s.chunkBuffer().DrainTools() {
		select {
		case s.events <- t:
		case <-s.done:
		}
	}
	s.emitMu.Unlock()

	if retry {
		s.armFlush(time.Now().Add(chunkRetryDelay))
	}
}

// drainChunks flushes any buffered streaming text on the way out of a session,
// best effort — the manager pump may already be gone. In every normal path
// this drains nothing: turn_complete and session_status are boundaries and
// have already flushed the tail. This is the abnormal-termination backstop.
func (s *session) drainChunks() {
	s.stopFlush()
	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	tools := s.chunkBuffer().DrainTools()
	s.emitMu.Unlock()
	if ok {
		s.trySend(ev)
	}
	// Best effort on the way out — the manager pump may already be gone, so
	// this cannot block the way the timed flush does.
	for _, t := range tools {
		s.trySend(t)
	}
}

// EmitReplay implements [Host]. Pre-attach (Start has not returned): never
// block — these only feed the history ring, so dropping the OLDEST when the
// buffer fills keeps the most recent conversation, mirroring the ring.
func (s *session) EmitReplay(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	ev.Replay = true
	for {
		select {
		case s.events <- ev:
			return
		default:
			select {
			case <-s.events:
			default:
			}
		}
	}
}
