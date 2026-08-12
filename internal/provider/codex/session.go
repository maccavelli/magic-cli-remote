package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/chunkbuf"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	maxPromptQueue  = 4
	turnCooldown    = 500 * time.Millisecond
	chunkRetryDelay = 50 * time.Millisecond

	maxImageBytes  = 10 << 20
	maxDataURLSize = 16 << 20
)

// pendingPerm is a permission awaiting the phone: the JSON-RPC id to answer,
// plus the description captured when it was raised.
//
// The description is carried here because sweepPendingApprovals folds
// outstanding permissions into the auto-approval audit and no longer has the
// params to re-derive it from — the rpc id alone cannot name what was approved
// (MADR 0051 §4.4).
type pendingPerm struct {
	rpcID  json.RawMessage
	tool   string
	detail string
}

type session struct {
	p       *Provider
	cfg     Config
	opts    provider.StartOptions
	localID string
	agentID string
	cwd     string
	log     *slog.Logger

	mu          sync.Mutex
	closed      bool
	turnBusy    bool
	turnID      string
	steerable   bool
	promptQueue [][]provider.Content
	events      chan event.Event
	done        chan struct{}

	// lastActivity is the unix-nanosecond timestamp of the most recent
	// notification on this session. The stall ticker reads this on each
	// tick (MADR 0035 D8) — one atomic store per notification replaces
	// the per-notification resetStallTimer() that used to allocate a new
	// time.AfterFunc under s.mu on every chunk.
	lastActivity atomic.Int64

	// approvalPolicy/sandboxMode are the session's live policy, seeded from
	// config and rewritten by SetMode. Re-sent on every turn/start so the
	// engine converges on daemon state after a restart or resume rather than
	// drifting (MADR 0044 D5). autoApprove mirrors approvalPolicy == "never"
	// and gates the belt-and-braces interception in handleApprovalRequest:
	// codex can still route approvals the policy does not cover (D6).
	approvalPolicy string
	sandboxMode    string
	autoApprove    bool

	// thinkingLevel is the user's explicit reasoning preference. It is sent
	// as turn/start.effort only on Default collaboration mode. Plan uses the
	// catalog preset instead (MADR 0080 D6).
	thinkingLevel string
	serviceTier   string
	personality   string
	goal          provider.Goal
	goalPresent   bool

	collabSupported  bool
	collabCatalog    collaborationCatalog
	collabMode       string
	engineGeneration int
	turnDiffs        map[string]string
	lastTurnDiffID   string

	// autoApprovals accumulates this turn's auto-approved requests for the
	// approval_summary card (MADR 0051 Part I). Guarded by s.mu.
	autoApprovals []event.ApprovalItem

	// subagents is this turn's sub-agent set, published as event.TypeSubagents
	// (MADR 0051 D8). subagentsPublished latches that a non-empty set went out,
	// so the clear at turn end only reaches sessions that actually had one.
	// Both guarded by s.mu.
	subagents          map[string]subagentState
	subagentsPublished bool

	pendingPerms map[string]pendingPerm
	// pendingOrder is the insertion order of pendingPerms. Map iteration is
	// random, and a sweep that folds outstanding permissions into the approval
	// audit must produce the same list on every run (MADR 0051 §4.4).
	pendingOrder     []string
	pendingQuestions map[string]json.RawMessage
	// answeredPerms remembers ids this session already answered, so a repeat
	// answer (a resync replay, or a second device) is a quiet no-op instead of
	// an "unknown permission" error the client turns into a re-present loop.
	// Bounded by maxAnsweredPerms.
	answeredPerms map[string]struct{}
	answeredOrder []string
	permTimeout   time.Duration
	stallNotice   time.Duration

	// respond writes a JSON-RPC response back to codex. Injectable so the
	// approval paths are testable without an engine; nil means "use the
	// provider's live framer".
	respond func(ctx context.Context, id json.RawMessage, result any, rpcErr *rpcErrorBody) error

	emitMu sync.Mutex
	chunks *chunkbuf.Buffer

	flushMu    sync.Mutex
	flushTimer *time.Timer

	// sandboxNoticeSent latches the MADR 0048 broken-sandbox TypeNotice so
	// create/resume/SetMode/mid-turn do not spam the transcript.
	sandboxNoticeSent bool

	// turnCancel aborts an in-flight turn/start when engine stderr reports a
	// quota/rate-limit the agent is silently retrying (MADR 0073 parity).
	turnCancel    context.CancelFunc
	limitNotified bool
	limitRaw      string
}

func newSession(p *Provider, cfg Config, opts provider.StartOptions, log *slog.Logger) *session {
	localID := opts.LocalSessionID
	if localID == "" {
		localID = uuid.NewString()
	}
	dir := opts.CWD
	if dir == "" {
		dir = cfg.DefaultCWD
	}
	if dir == "" {
		// Production resolves via provider.ResolveSessionCWD in
		// startSession (0069 P1); this covers direct test construction.
		// Never os.Getwd(): under launchd/systemd the process cwd is an
		// accident of the unit file.
		dir, _ = os.UserHomeDir()
	}
	// Seed live policy from config or the provider default mode so create
	// always has a real current mode and never arms auto without a sandbox
	// (MADR 0047 D2). Raw cfg is kept for gating (e.g. AllowFullAccess).
	// MADR 0048 may force full-access seed when sandbox is broken.
	approval, sandbox, _ := seedPolicy(cfg)
	if p != nil {
		h := p.sandboxHealth()
		if a, sb, _, err := applySandboxBrokenPolicy(cfg, h, approval, sandbox, ""); err == nil {
			approval, sandbox = a, sb
		}
		// Start fails earlier under refuse/require without gate; here we only
		// adjust seed for warn / require_full_access success paths.
	}
	s := &session{
		p:       p,
		cfg:     cfg,
		opts:    opts,
		localID: localID,
		cwd:     dir,
		// Seed from create-session; SetThinkingLevel can change it mid-session
		// (next turn/start). Empty means omit effort (MADR 0052).
		thinkingLevel:    strings.TrimSpace(opts.ThinkingLevel),
		serviceTier:      normalizeServiceTier(opts.ServiceTier),
		personality:      strings.TrimSpace(opts.Personality),
		events:           make(chan event.Event, 256),
		done:             make(chan struct{}),
		pendingPerms:     make(map[string]pendingPerm),
		pendingQuestions: make(map[string]json.RawMessage),
		permTimeout:      cfg.PermissionTimeout,
		stallNotice:      cfg.TurnStallNotice,
		log:              log.With(slog.String("session", localID)),
		approvalPolicy:   approval,
		sandboxMode:      sandbox,
		autoApprove:      approval == "never",
		collabMode:       collaborationModeDefault,
	}
	s.seedCollaboration()
	// MADR 0035 D8: stamp the activity clock and start a single per-
	// session ticker that reads it. The previous per-notification timer
	// reset cost a mutex acquire and a time.AfterFunc allocation per
	// chunk, paid by every active turn.
	s.lastActivity.Store(time.Now().UnixNano())
	if cfg.TurnStallNotice > 0 {
		go s.stallTicker()
	}
	return s
}

// stallTicker fires a TypeNotice when the turn has been silent for at
// least stallNotice (MADR 0035 D8). One ticker per session replaces the
// per-notification time.AfterFunc churn.
func (s *session) stallTicker() {
	interval := s.cfg.TurnStallNotice
	if interval <= 0 {
		return
	}
	// Tick at the configured interval: 120 s default. The notice fires
	// when a tick observes a gap >= interval. A 5 s jittered half-tick
	// keeps the first notice from racing the first chunk.
	tick := interval / 4
	if tick < 250*time.Millisecond {
		tick = 250 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.mu.Lock()
			busy := s.turnBusy
			s.mu.Unlock()
			if !busy {
				continue
			}
			last := time.Unix(0, s.lastActivity.Load())
			gap := time.Since(last)
			if gap < interval {
				continue
			}
			s.emit(event.Event{
				Type:      event.TypeNotice,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Text:      "The agent has been silent for " + s.stallNotice.String() + ". It may still be working.",
			})
		}
	}
}

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return provider.IDCodex }
func (s *session) CWD() string                { return s.cwd }
func (s *session) Events() <-chan event.Event { return s.events }

func (s *session) AgentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentID
}

func (s *session) create(ctx context.Context, fr *conn) error {
	if s.opts.AgentSessionID != "" {
		return s.resume(ctx, fr)
	}
	return s.startNew(ctx, fr)
}

// applyPolicyParams sets the sandbox and approval-policy overrides on
// thread/start and thread/resume params. Both take plain enum *strings*:
// ThreadStartParams.sandbox is a SandboxMode ("read-only" | "workspace-write" |
// "danger-full-access"), not an object. Sending the object form
// ({"type":…,"networkAccess":…}) is rejected by codex 0.145 with
//
//	-32600 Invalid request: invalid value: map, expected map with a single key
//
// which fails session create outright. Note the asymmetry with turn/start,
// whose sandboxPolicy *is* an object with a camelCase type tag — the two are
// different protocol types and must not be cross-wired.
//
// Empty values are omitted so the engine keeps its own configured defaults.
func applyPolicyParams(params map[string]any, approvalPolicy, sandboxMode string) {
	if sandboxMode != "" {
		params["sandbox"] = sandboxMode
	}
	if approvalPolicy != "" {
		params["approvalPolicy"] = approvalPolicy
	}
}

// policy snapshots the session's live approval policy and sandbox mode. Both
// thread/start, thread/resume and turn/start read them through here so there is
// exactly one place that decides what the engine is told.
func (s *session) policy() (approvalPolicy, sandboxMode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.approvalPolicy, s.sandboxMode
}

func (s *session) startNew(ctx context.Context, fr *conn) error {
	params := map[string]any{}
	if s.cwd != "" {
		params["cwd"] = s.cwd
	}
	approval, sandbox := s.policy()
	applyPolicyParams(params, approval, sandbox)
	model := s.opts.Model
	if model == "" {
		model = s.cfg.Model
	}
	if model != "" {
		params["model"] = model
	}

	raw, err := fr.sendRequest(ctx, "thread/start", params)
	if err != nil {
		return fmt.Errorf("thread/start: %w", err)
	}

	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("thread/start: decode: %w", err)
	}

	s.mu.Lock()
	s.agentID = resp.Thread.ID
	s.mu.Unlock()

	if resp.CWD != "" {
		s.cwd = resp.CWD
	}

	// MADR 0035 D4: advertise the session's capabilities so the phone can
	// gate UI (e.g. the image-attach button) without a probe.
	//   Image:     the provider implements image input end-to-end
	//              (session.go:26,1183-1217) and all six codex models
	//              advertise inputModalities [text,image] (MADR 0028 §16.3).
	//   Audio:     codex advertises no audio modality.
	//   LoadSession: thread/resume verified OK (MADR 0028 §16.3).
	// Leave the ACP-specific fields (EmbeddedContext, MCPHTTP, …) false:
	// codex has no equivalent negotiation and claiming them would be a guess.
	s.emitCapabilities()
	s.emitModes()
	s.emitCollaboration(true)
	if s.p != nil {
		s.emitSandboxBrokenNotice(s.p.sandboxHealth())
	}

	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "idle",
		AgentSessionID: s.agentID,
	})
	return nil
}

func (s *session) resume(ctx context.Context, fr *conn) error {
	s.mu.Lock()
	s.agentID = s.opts.AgentSessionID
	s.mu.Unlock()

	params := map[string]any{
		"threadId": s.opts.AgentSessionID,
	}
	if s.cwd != "" {
		params["cwd"] = s.cwd
	}
	// ThreadResumeParams carries the same sandbox/approvalPolicy overrides as
	// ThreadStartParams. Send them here too: a resumed thread would otherwise
	// silently fall back to the engine's own defaults, so the same session
	// would run under a different policy after a daemon restart.
	approval, sandbox := s.policy()
	applyPolicyParams(params, approval, sandbox)

	_, err := fr.sendRequest(ctx, "thread/resume", params)
	if err != nil {
		return fmt.Errorf("thread/resume: %w", err)
	}

	// A resumed session needs its mode chip too, and the policy it reports is
	// the config-seeded one this resume just re-asserted — auto-approve is
	// never restored from the previous run (MADR 0044 D8).
	s.emitModes()
	s.emitCollaboration(true)
	if s.p != nil {
		s.emitSandboxBrokenNotice(s.p.sandboxHealth())
	}

	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "idle",
		AgentSessionID: s.agentID,
	})
	return nil
}

// emitModes advertises the session's selectable modes and the one in effect.
// A policy pair that matches no advertised mode reports an empty current id
// rather than naming a mode that is not really active.
func (s *session) emitModes() {
	approval, sandbox := s.policy()
	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		Modes:         advertisedModes(s.cfg),
		CurrentModeID: modeIDFor(s.cfg, approval, sandbox),
	})
}

// SetMode implements [provider.ModeSession]. Codex carries the approval policy
// and sandbox per turn, so the switch takes effect on the next turn/start
// rather than immediately — that is the protocol's own semantic ("this turn and
// subsequent turns"), not a daemon limitation.
func (s *session) SetMode(ctx context.Context, modeID string) error {
	m, ok := findCodexMode(s.cfg, modeID)
	if !ok {
		return fmt.Errorf("unknown mode %q", modeID)
	}

	s.mu.Lock()
	s.approvalPolicy = m.approvalPolicy
	s.sandboxMode = m.sandbox
	s.autoApprove = m.approvalPolicy == "never"
	auto := s.autoApprove
	s.mu.Unlock()

	if auto {
		s.log.Warn("codex auto-approve armed",
			slog.String("agent_session_id", s.AgentSessionID()),
			slog.String("mode", m.mode.ID),
			slog.String("sandbox", m.sandbox),
		)
		s.sweepPendingApprovals()
	} else {
		s.log.Info("codex mode switch",
			slog.String("agent_session_id", s.AgentSessionID()),
			slog.String("mode", m.mode.ID),
		)
		// Leaving auto: nothing further joins the approval card, so close it
		// rather than let it run on into a mode that prompts (MADR 0051).
		s.finishApprovals()
	}

	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		CurrentModeID: m.mode.ID,
	})
	// MADR 0048: arming a sandboxed mode while the host cannot write.
	if s.p != nil && m.sandbox != "danger-full-access" {
		s.emitSandboxBrokenNotice(s.p.sandboxHealth())
	}
	return nil
}

// sweepPendingApprovals accepts approvals that were already waiting when auto
// was armed. Without it, arming auto to unblock an agent that is already
// stuck — the most likely reason to reach for it — does nothing until the agent
// asks again (MADR 0044 D4.5).
func (s *session) sweepPendingApprovals() {
	s.mu.Lock()
	pending := s.pendingPerms
	// Insertion order, not map order: the audit list these become must read the
	// same on every run (MADR 0051 §4.4).
	order := s.pendingOrder
	s.pendingPerms = make(map[string]pendingPerm)
	s.pendingOrder = nil
	for permID := range pending {
		s.noteAnsweredLocked(permID)
	}
	s.mu.Unlock()

	if len(pending) == 0 {
		// Nothing waiting: do not reach for the engine framer at all.
		return
	}

	send := s.responder()
	for _, permID := range order {
		p := pending[permID]
		if send != nil {
			if err := send(context.Background(), p.rpcID,
				map[string]any{"decision": "accept"}, nil); err != nil {
				s.log.Warn("auto-approve sweep failed",
					slog.String("permission_id", permID), slog.String("err", err.Error()))
			}
		}
		s.emit(event.Event{
			Type:         event.TypePermissionResolved,
			SessionID:    s.localID,
			Timestamp:    time.Now().UTC(),
			PermissionID: permID,
			Status:       event.PermissionStatusResolved,
			// DeviceID/OptionID intentionally left empty (MADR 0077 §1):
			// arming auto mode answers previously pending permissions in
			// bulk, not a single fresh human tap.
		})
		// The user armed auto mode; everything approved after that point
		// belongs in the audit, including what was already waiting.
		s.noteAutoApproval(p.tool, p.detail)
	}
}

var _ provider.ModeSession = (*session)(nil)

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnBusy && s.steerable {
		s.mu.Unlock()
		return s.steerTurn(ctx, parts)
	}
	if s.turnBusy {
		if len(s.promptQueue) >= maxPromptQueue {
			s.mu.Unlock()
			return provider.ErrTurnBusy
		}
		s.promptQueue = append(s.promptQueue, cloneContent(parts))
		n := len(s.promptQueue)
		s.mu.Unlock()
		s.emitUserMessage(parts)
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text:      fmt.Sprintf("Queued (%d/%d) — will send when the agent is idle", n, maxPromptQueue),
		})
		return nil
	}
	s.mu.Unlock()
	return s.beginTurn(ctx, parts, true)
}

func (s *session) steerTurn(ctx context.Context, parts []provider.Content) error {
	_, blocks, _ := buildPrompt(parts)
	if len(blocks) == 0 {
		return fmt.Errorf("prompt has no sendable content")
	}

	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}

	s.mu.Lock()
	expectedTurnID := s.turnID
	s.mu.Unlock()

	s.emitUserMessage(parts)

	turnCtx := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("steer turn panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		_, err := fr.sendRequest(turnCtx, "turn/steer", map[string]any{
			"threadId":       s.agentID,
			"expectedTurnId": expectedTurnID,
			"input":          blocks,
		})
		if err != nil {
			s.log.Debug("steer failed", slog.String("err", err.Error()))
		}
	}()
	return nil
}

func (s *session) beginTurn(ctx context.Context, parts []provider.Content, emitUser bool) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnBusy {
		s.mu.Unlock()
		return provider.ErrTurnBusy
	}
	s.turnBusy = true
	s.mu.Unlock()

	text, blocks, attachments := buildPrompt(parts)
	if len(blocks) == 0 {
		s.clearTurnBusy()
		return fmt.Errorf("prompt has no sendable content")
	}

	fr := s.p.framer()
	if fr == nil {
		s.clearTurnBusy()
		return fmt.Errorf("engine not running")
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

	baseCtx := context.WithoutCancel(ctx)
	turnCtx, cancel := context.WithCancel(baseCtx)
	s.mu.Lock()
	s.turnCancel = cancel
	s.limitNotified = false
	s.limitRaw = ""
	s.mu.Unlock()
	go s.runTurn(turnCtx, cancel, fr, blocks)
	return nil
}

func (s *session) runTurn(ctx context.Context, cancel context.CancelFunc, fr *conn, blocks []map[string]any) {
	defer func() {
		cancel()
		s.mu.Lock()
		s.turnCancel = nil
		s.limitNotified = false
		s.limitRaw = ""
		s.mu.Unlock()
	}()
	s.lastActivity.Store(time.Now().UnixNano())

	params := map[string]any{
		"threadId": s.agentID,
		"input":    blocks,
	}
	s.mu.Lock()
	model := s.effectiveModelLocked()
	effort := s.thinkingLevel
	collabOK := s.collabSupported
	collabMode := s.collabMode
	mask, _ := s.collabCatalog.lookup(collabMode)
	tier, personality := s.serviceTier, s.personality
	s.mu.Unlock()
	if model != "" {
		params["model"] = model
	}
	// effort is only set when the user (or a default) chose a rung. Omitting
	// the key — not sending "" — is what "Provider default" means: codex then
	// uses the model's defaultReasoningEffort (MADR 0052 §2.1). Assert absence
	// in tests; a hard-coded effort:"" would silently override the default.
	// Plan collaboration uses the catalog preset via collaborationMode and
	// omits top-level effort (MADR 0080 D6).
	applyCollaborationTurnParams(params, collabOK, mask, collabMode, model, effort)
	applyServiceTurnParams(params, tier, personality)
	// Override for this turn and subsequent turns. Re-sent every turn rather
	// than once so the engine converges on daemon state after an engine restart
	// or a thread resume, instead of drifting (MADR 0044 D5).
	//
	// NB: turn/start takes `sandboxPolicy` — an object with a camelCase type
	// tag — not thread/start's kebab-case `sandbox` string. Verified live
	// against codex 0.145; the wrong shape is rejected with -32600.
	approval, sandbox := s.policy()
	if approval != "" {
		params["approvalPolicy"] = approval
	}
	if pol := sandboxPolicyParam(sandbox); pol != nil {
		params["sandboxPolicy"] = pol
	}
	raw, err := fr.sendRequest(ctx, "turn/start", params)
	if err != nil {
		s.mu.Lock()
		wasBusy := s.turnBusy
		limitRaw := s.limitRaw
		limitHit := s.limitNotified && limitRaw != ""
		s.turnBusy = false
		s.turnID = ""
		s.steerable = false
		s.mu.Unlock()
		if !wasBusy {
			return
		}
		if limitHit {
			// Stderr scrape aborted a silent provider limit hang.
			s.emitTurnComplete("error", limitRaw)
		} else if errors.Is(err, errConnLost) || errors.Is(err, context.Canceled) {
			s.emitTurnComplete("cancelled", "")
		} else {
			// The runTurn RPC failed; pass the error message through
			// emitTurnComplete so it goes out as a single TypeError
			// (the previous code emitted a separate TypeError right
			// after, which then led to a turn_complete ordering that
			// put the error after the idle status — fixed here).
			s.emitTurnComplete("error", err.Error())
		}
		s.tryDrainQueue()
		return
	}

	var resp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &resp); err == nil && resp.Turn.ID != "" {
		s.mu.Lock()
		s.turnID = resp.Turn.ID
		s.mu.Unlock()
	}
}

func (s *session) Cancel(ctx context.Context) error {
	s.mu.Lock()
	turnID := s.turnID
	s.steerable = false
	s.promptQueue = nil
	s.mu.Unlock()

	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}

	_, err := fr.sendRequest(ctx, "turn/interrupt", map[string]any{
		"threadId": s.agentID,
		"turnId":   turnID,
	})
	if err != nil {
		var rpcErr *rpcErrorBody
		if errors.As(err, &rpcErr) && strings.Contains(rpcErr.Message, "no active turn") {
			return nil
		}
		return err
	}
	return nil
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.drainChunksClose()

	// MADR 0035 D8: the stall ticker is stopped by closing s.done, which
	// happens further down. The previous per-session time.AfterFunc field
	// is gone; nothing to stop here.

	// Answer everything still waiting *before* closing s.done, or the resolved
	// events are dropped by emit's done-select and the phone is left holding
	// sheets for requests nothing will ever read.
	s.cancelPendingPermissions("session closed")
	s.cancelPendingQuestions("session closed")

	fr := s.p.framer()
	if fr != nil {
		_, _ = fr.sendRequest(ctx, "thread/unsubscribe", map[string]any{
			"threadId": s.agentID,
		})
	}

	s.p.mu.Lock()
	delete(s.p.sessions, s.agentID)
	s.p.mu.Unlock()

	close(s.done)
	return nil
}

func (s *session) Purge(ctx context.Context) error {
	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	_, err := fr.sendRequest(ctx, "thread/delete", map[string]any{
		"threadId": s.agentID,
	})
	return err
}

func (s *session) Fork(ctx context.Context, opts provider.ForkOptions) (provider.ForkResult, error) {
	s.mu.Lock()
	if s.turnBusy {
		s.mu.Unlock()
		return provider.ForkResult{}, provider.ErrTurnBusy
	}
	threadID := s.agentID
	experimental := s.p != nil && s.p.eng != nil && s.p.eng.experimental
	s.mu.Unlock()
	if opts.DeferGoalContinuation && !experimental {
		return provider.ForkResult{}, fmt.Errorf("defer goal continuation requires experimental API")
	}
	fr := s.p.framer()
	if fr == nil {
		return provider.ForkResult{}, fmt.Errorf("engine not running")
	}
	params := map[string]any{
		"threadId": threadID,
	}
	if opts.LastTurnID != "" {
		params["lastTurnId"] = opts.LastTurnID
	}
	if opts.DeferGoalContinuation {
		params["deferGoalContinuation"] = true
	}
	raw, err := fr.sendRequest(ctx, "thread/fork", params)
	if err != nil {
		var rpc *rpcErrorBody
		if errors.As(err, &rpc) && rpc != nil && strings.Contains(strings.ToLower(rpc.Message), "no rollout found") {
			return provider.ForkResult{}, provider.ErrForkNothing
		}
		return provider.ForkResult{}, err
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		ForkedFromID string `json:"forkedFromId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return provider.ForkResult{}, fmt.Errorf("thread/fork: decode: %w", err)
	}
	return provider.ForkResult{AgentSessionID: resp.Thread.ID, ForkedFromID: resp.ForkedFromID}, nil
}

func (s *session) Compact(ctx context.Context) error {
	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	_, err := fr.sendRequest(ctx, "thread/compact/start", map[string]any{
		"threadId": s.agentID,
	})
	return err
}

func (s *session) Rename(ctx context.Context, title string) error {
	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	_, err := fr.sendRequest(ctx, "thread/name/set", map[string]any{
		"threadId": s.agentID,
		"name":     title,
	})
	return err
}

// modelLister is the subset of *Provider that SetModel needs. Splitting it
// out of *Provider lets tests substitute a fake without running an engine.
type modelLister interface {
	ListModels(ctx context.Context) (picker.Catalog, error)
}

func (s *session) SetModel(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model name is empty")
	}
	// Validate against the live model catalog. An engine hiccup on model/list
	// is a permit (log and proceed) — a typo would otherwise be reported by
	// the engine on the next turn/start, which is too late to undo. The daemon
	// confirms "Model is now X — the conversation is kept" right after this
	// returns (setModelInPlace in session/commands.go), so an unvalidated typo
	// becomes a lie that breaks the next turn.
	if err := validateModelName(ctx, s.p, model, s.log); err != nil {
		return err
	}
	s.mu.Lock()
	s.opts.Model = model
	s.revalidateModelSettingsLocked()
	s.mu.Unlock()
	return nil
}

// SetThinkingLevel records the reasoning effort for subsequent turn/start
// calls. Empty clears the override so the next turn omits effort and codex
// uses the model default. The change is next-turn by construction (MADR 0052
// D7); no special mid-turn handling is needed.
//
// The ThinkingSession interface and /thinking command land in A4; this method
// is the codex half of that contract so turn/start can be tested now.
func (s *session) SetThinkingLevel(_ context.Context, level string) error {
	s.mu.Lock()
	s.thinkingLevel = strings.TrimSpace(level)
	s.mu.Unlock()
	return nil
}

// ThinkingLevel returns the session's current effort override, or "" when the
// provider default applies.
func (s *session) ThinkingLevel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thinkingLevel
}

// Compile-time check: codex sessions accept mid-session thinking changes.
var _ provider.ThinkingSession = (*session)(nil)

// Compile-time check: codex sessions can resolve remote permission prompts.
// Missing this let *session silently stop satisfying provider.PermissionSession
// when its RespondPermission signature drifted from the interface (MADR
// 0077 P6) — session.Manager only asserts this at runtime via a type
// switch, so go build alone did not catch it.
var _ provider.PermissionSession = (*session)(nil)

// validateModelName checks the model name against the engine's live model
// catalog. An error from ListModels is permitted (logged) so a transient
// engine hiccup does not block a legitimate switch. Pulled out as a package-
// level function so the test can exercise the validation contract without
// running an engine.
func validateModelName(ctx context.Context, p any, model string, log *slog.Logger) error {
	lister, ok := p.(modelLister)
	if !ok {
		return nil
	}
	cat, err := lister.ListModels(ctx)
	if err != nil {
		if log != nil {
			log.Warn("set model: list models failed; permitting the change",
				slog.String("err", err.Error()))
		}
		return nil
	}
	for _, opt := range cat.Options {
		if opt.ID == model {
			return nil
		}
	}
	return fmt.Errorf("model %q is not in the codex model catalog", model)
}

// maxAnsweredPerms bounds the answered-id set. A turn issues a handful of
// approvals; this is generous and keeps a long session from growing the map.
const maxAnsweredPerms = 256

// responder returns the function that writes a JSON-RPC response to codex.
func (s *session) responder() func(context.Context, json.RawMessage, any, *rpcErrorBody) error {
	if s.respond != nil {
		return s.respond
	}
	fr := s.p.framer()
	if fr == nil {
		return nil
	}
	return fr.sendResponse
}

// noteAnsweredLocked records an id as answered, evicting the oldest past the
// cap. Caller holds s.mu.
func (s *session) noteAnsweredLocked(permissionID string) {
	if s.answeredPerms == nil {
		s.answeredPerms = make(map[string]struct{})
	}
	if _, dup := s.answeredPerms[permissionID]; dup {
		return
	}
	s.answeredPerms[permissionID] = struct{}{}
	s.answeredOrder = append(s.answeredOrder, permissionID)
	for len(s.answeredOrder) > maxAnsweredPerms {
		delete(s.answeredPerms, s.answeredOrder[0])
		s.answeredOrder = s.answeredOrder[1:]
	}
}

// RespondPermission answers one approval request.
//
// Three properties this needs and did not have — together they are the
// reported approve -> "permission respond failed" -> re-present loop:
//
//   - It emits permission_resolved. Every other provider does. Without it the
//     daemon's history ring holds an unresolved permission_request forever, so
//     every reconnect, resync and history hydrate re-raises a sheet for a
//     request codex was already told about.
//   - The pending entry is retired only once the write to codex succeeds.
//     Deleting first meant a failed write lost the request: codex still
//     waiting, and every retry answering "unknown permission".
//   - A repeat answer is a no-op that re-announces the resolution, rather than
//     an error. The client's failure path re-presents the request, so turning
//     a duplicate into an error is what closes the loop.
func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool, deviceID string) error {
	s.mu.Lock()
	pend, ok := s.pendingPerms[permissionID]
	_, alreadyAnswered := s.answeredPerms[permissionID]
	s.mu.Unlock()

	if !ok {
		if alreadyAnswered {
			// Re-announce so a client that lost the first resolution clears.
			s.emitPermissionResolved(permissionID, cancelled, deviceID, optionID)
			return nil
		}
		return fmt.Errorf("unknown permission: %s", permissionID)
	}

	send := s.responder()
	if send == nil {
		return fmt.Errorf("engine not running")
	}

	decision := optionID
	if cancelled {
		decision = "cancel"
	}
	if err := send(ctx, pend.rpcID, map[string]any{"decision": decision}, nil); err != nil {
		// Leave the request outstanding so the client can retry: codex is
		// still waiting for an answer either way.
		return err
	}

	s.mu.Lock()
	s.dropPendingLocked(permissionID)
	s.noteAnsweredLocked(permissionID)
	s.mu.Unlock()

	s.emitPermissionResolved(permissionID, cancelled, deviceID, optionID)
	return nil
}

// emitPermissionResolved tells clients the request is answered so their sheet
// closes and the history ring records the resolution. deviceID/optionID are
// empty for internal outcomes (session teardown sweep) — see callers.
func (s *session) emitPermissionResolved(permissionID string, cancelled bool, deviceID, optionID string) {
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.emit(event.Event{
		Type:           event.TypePermissionResolved,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		PermissionID:   permissionID,
		Status:         status,
		AgentSessionID: s.agentID,
		DeviceID:       deviceID,
		OptionID:       optionID,
	})
}

// approvalGroupID is the stable client-side upsert key for this session's
// auto-approval card: one card per turn, replaced rather than appended.
const approvalGroupID = "auto-approvals"

// maxApprovalItems bounds the per-turn audit list, matching the OpenCode side.
const maxApprovalItems = 512

// noteAutoApproval records one auto-approved request and re-publishes the whole
// list for this turn (MADR 0051 Part I).
//
// No dedicated mutex, unlike OpenCode: handleApprovalRequest and
// sweepPendingApprovals are both reached only from readPump (conn.go), a single
// goroutine, so snapshots cannot be emitted out of order here.
func (s *session) noteAutoApproval(tool, detail string) {
	s.mu.Lock()
	now := time.Now().UTC()
	s.autoApprovals = append(s.autoApprovals, event.ApprovalItem{
		ToolName: firstNonEmpty(tool, "approval"),
		Detail:   truncateRunes(detail, maxApprovalSummary),
		Time:     now,
	})
	if n := len(s.autoApprovals); n > maxApprovalItems {
		s.autoApprovals = append([]event.ApprovalItem(nil), s.autoApprovals[n-maxApprovalItems:]...)
	}
	// Clone under the lock: the event is marshalled later, on the writer
	// goroutine, while the next append may reallocate this backing array.
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
// list. Called when the turn ends and when the mode leaves auto.
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

// trackPendingLocked records an outstanding permission and its insertion
// order. Callers hold s.mu.
func (s *session) trackPendingLocked(permID string, p pendingPerm) {
	if _, dup := s.pendingPerms[permID]; !dup {
		s.pendingOrder = append(s.pendingOrder, permID)
	}
	s.pendingPerms[permID] = p
}

// dropPendingLocked forgets an outstanding permission, keeping pendingOrder in
// step so a later sweep cannot iterate an id the map no longer has. Callers
// hold s.mu.
func (s *session) dropPendingLocked(permID string) {
	if _, ok := s.pendingPerms[permID]; !ok {
		return
	}
	delete(s.pendingPerms, permID)
	s.pendingOrder = slices.DeleteFunc(s.pendingOrder, func(id string) bool {
		return id == permID
	})
}

// cancelPendingPermissions answers every outstanding approval with `cancel` and
// tells clients so. Without it a session that ends with sheets up leaves them
// on the phone waiting for an answer nothing will ever read.
func (s *session) cancelPendingPermissions(reason string) {
	s.mu.Lock()
	pending := s.pendingPerms
	order := s.pendingOrder
	s.pendingPerms = make(map[string]pendingPerm)
	s.pendingOrder = nil
	for id := range pending {
		s.noteAnsweredLocked(id)
	}
	s.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	send := s.responder()
	for _, permID := range order {
		if send != nil {
			_ = send(context.Background(), pending[permID].rpcID,
				map[string]any{"decision": "cancel"}, nil)
		}
		s.log.Debug("cancelling outstanding permission",
			slog.String("permission_id", permID), slog.String("reason", reason))
		// Internal teardown, not a device decision — deviceID/optionID stay
		// empty (MADR 0077 §1).
		s.emitPermissionResolved(permID, true, "", "")
	}
}

// cancelPendingQuestions is the same sweep for outstanding user-input forms.
// Close used to delete them without answering codex or telling the client, so
// a question sheet raised at the end of a session stayed up forever.
func (s *session) cancelPendingQuestions(reason string) {
	s.mu.Lock()
	pending := s.pendingQuestions
	s.pendingQuestions = make(map[string]json.RawMessage)
	s.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	send := s.responder()
	for qID, rpcID := range pending {
		if send != nil {
			_ = send(context.Background(), rpcID, nil, &rpcErrorBody{
				Code: -32800, Message: reason,
			})
		}
		s.log.Debug("cancelling outstanding question",
			slog.String("question_id", qID), slog.String("reason", reason))
		s.emit(event.Event{
			Type:           event.TypeQuestionResolved,
			SessionID:      s.localID,
			Timestamp:      time.Now().UTC(),
			QuestionID:     qID,
			Status:         event.PermissionStatusCancelled,
			AgentSessionID: s.agentID,
		})
	}
}

func (s *session) handleNotification(method string, params json.RawMessage) {
	now := time.Now().UTC()
	// MADR 0035 D8: one atomic store per notification. The stall ticker
	// (started in newSession) reads this on each tick; no per-chunk
	// timer allocation.
	s.lastActivity.Store(now.UnixNano())

	switch method {
	case "thread/settings/updated":
		s.applySettingsUpdated(params)
	case "thread/goal/updated", "thread/goal/cleared":
		s.applyGoalNotification(method, params)
	case "turn/diff/updated":
		var p struct {
			TurnID string `json:"turnId"`
			Diff   string `json:"diff"`
		}
		if json.Unmarshal(params, &p) == nil {
			s.rememberTurnDiff(p.TurnID, p.Diff)
		}
	case "turn/completed":
		// MADR 0035 D5: route through emitTurnComplete so the cancel/error
		// paths (runTurn RPC failure) share one implementation. The two
		// previous emitters were a 1-site-fix trap. The status enum
		// (completed | interrupted | failed | inProgress) is mapped
		// through codexStopReason; the engine's `turn.error` field is
		// captured on `failed` and surfaced as TypeError so the failure
		// is reported rather than swallowed.
		var p struct {
			Turn struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			break
		}
		wire := p.Turn.Status
		stop := codexStopReason(wire)
		var turnErrMsg string
		if wire == "failed" && p.Turn.Error != nil {
			turnErrMsg = p.Turn.Error.Message
		}
		s.emitTurnComplete(stop, turnErrMsg)
		s.mu.Lock()
		s.turnBusy = false
		s.turnID = ""
		s.steerable = false
		s.mu.Unlock()
		s.tryDrainQueue()
	case "turn/started":
		var p struct {
			TurnID string `json:"turnId"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.TurnID != "" {
			s.mu.Lock()
			s.turnID = p.TurnID
			s.steerable = true
			s.mu.Unlock()
			s.tryDrainQueue()
		}
	case "item/agentMessage/delta":
		var p struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
			TurnID string `json:"turnId"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.Delta != "" {
			s.emit(event.Event{
				Type:           event.TypeAssistantChunk,
				SessionID:      s.localID,
				Timestamp:      now,
				Text:           p.Delta,
				ToolID:         p.ItemID,
				AgentSessionID: s.agentID,
			})
		}
	case "item/reasoning/textDelta":
		var p struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.Delta != "" {
			s.emit(event.Event{
				Type:           event.TypeThoughtChunk,
				SessionID:      s.localID,
				Timestamp:      now,
				Text:           p.Delta,
				ToolID:         p.ItemID,
				AgentSessionID: s.agentID,
			})
		}
	case "item/started":
		// v2 wire format: the v2 schema moved itemType to item.type, so
		// params.itemType is always "" and params.item carries the full
		// ThreadItem with its own `type` and `id` fields. Read from item.
		// MADR 0035 D1: a single allowlist drives both this handler and
		// item/completed, so the two cannot disagree about which item
		// types produce tool cards.
		var p struct {
			ItemID string          `json:"itemId"`
			TurnID string          `json:"turnId"`
			Item   json.RawMessage `json:"item"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			break
		}
		itemID, itemType := extractItemID(p.Item)
		if itemID == "" {
			// Fall back to the v1-shaped id at params level in case an
			// older engine slips through.
			itemID = p.ItemID
		}
		if ev, ok := itemAsNotice(itemType); ok {
			ev.SessionID = s.localID
			ev.AgentSessionID = s.agentID
			s.emit(ev)
			break
		}
		// Sub-agent activity is panel state, not a transcript card
		// (MADR 0051 D8/D10).
		if s.noteSubagentItem(itemType, p.Item) {
			break
		}
		if _, isTool := itemsRenderedAsTools[itemType]; !isTool {
			s.log.Debug("codex: item/started for non-tool item type",
				slog.String("type", itemType), slog.String("id", itemID))
			break
		}
		s.emitToolStarted(itemType, itemID, p.Item, now)
	case "item/completed":
		// MADR 0035 D1: same allowlist as item/started. The previous code
		// excluded "agentMessage", "userMessage", "reasoning" — a deny-list
		// that was always going to miss new types — instead of the
		// allowlist, which is silent-by-default and explicit.
		var p struct {
			ItemID string          `json:"itemId"`
			TurnID string          `json:"turnId"`
			Item   json.RawMessage `json:"item"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			break
		}
		itemID, itemType := extractItemID(p.Item)
		if itemID == "" {
			itemID = p.ItemID
		}
		// A completed collab tool call is where a spawned agent's terminal
		// status actually shows up, so it must be read here too.
		if s.noteSubagentItem(itemType, p.Item) {
			break
		}
		if _, isTool := itemsRenderedAsTools[itemType]; !isTool {
			break
		}
		// Carry the terminal status over to the tool card so the mobile
		// reducer (which keys on tool_id) sees the actual outcome.
		var probe struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(p.Item, &probe)
		terminal := codexToolStatus(probe.Status)
		if terminal == "" {
			terminal = "completed"
		}
		// MADR 0048: promote mid-turn bwrap/userns failures into sticky health.
		s.maybePromoteSandboxFailure(string(p.Item))
		s.emit(event.Event{
			Type:           event.TypeToolUpdate,
			SessionID:      s.localID,
			Timestamp:      now,
			ToolID:         itemID,
			Status:         terminal,
			AgentSessionID: s.agentID,
		})
	case "item/commandExecution/outputDelta":
		var p struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.Delta != "" {
			s.emit(event.Event{
				Type:           event.TypeToolUpdate,
				SessionID:      s.localID,
				Timestamp:      now,
				ToolID:         p.ItemID,
				Text:           p.Delta,
				Status:         "running",
				AgentSessionID: s.agentID,
			})
		}
	case "thread/tokenUsage/updated":
		var p struct {
			TokenUsage struct {
				Total int `json:"total"`
				Last  int `json:"last"`
			} `json:"tokenUsage"`
			ModelContextWindow int `json:"modelContextWindow"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.emit(event.Event{
				Type:           event.TypeUsage,
				SessionID:      s.localID,
				Timestamp:      now,
				Usage:          &event.Usage{Used: p.TokenUsage.Total, Size: p.ModelContextWindow},
				AgentSessionID: s.agentID,
			})
		}
	case "thread/status/changed":
		// Tracked but not surfaced as a separate event; turn state drives status.
	case "turn/plan/updated":
		// MADR 0035 phase 8 / MADR 0035 D1 follow-on: codex's plan
		// notification. The v2 schema carries
		//   {threadId, turnId, plan: [{step, status}], explanation?}
		// with TurnPlanStepStatus = pending|inProgress|completed.
		// Priority is not on the wire, so we default to medium
		// (the daemon's safe default for an unknown priority).
		var p struct {
			Plan []struct {
				Step   string `json:"step"`
				Status string `json:"status"`
			} `json:"plan"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			break
		}
		entries := codexPlanEntries(p.Plan)
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   entries,
		})
	case "account/rateLimits/updated":
		// MADR 0035 D9: codex pushes exactly the data the limit-card
		// contract already wants (error_kind, retry_at). Map primary
		// usage >= 100 to a rate_limit error; leave a "rate limit
		// approaching" as a notice at >= 90.
		var p struct {
			RateLimits struct {
				Primary *struct {
					UsedPercent        int   `json:"usedPercent"`
					WindowDurationMins int   `json:"windowDurationMins"`
					ResetsAt           int64 `json:"resetsAt"`
				} `json:"primary"`
			} `json:"rateLimits"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.RateLimits.Primary == nil {
			break
		}
		prim := p.RateLimits.Primary
		var resetAt time.Time
		if prim.ResetsAt > 0 {
			resetAt = time.Unix(prim.ResetsAt, 0).UTC()
		}
		if prim.UsedPercent >= 100 {
			s.emit(event.Event{
				Type:      event.TypeError,
				SessionID: s.localID,
				Timestamp: now,
				ErrorKind: "rate_limit",
				RetryAt:   resetAt,
				Error: fmt.Sprintf(
					"Codex rate limit reached (%d%% of the current window). Wait for the window to reset, or try again later.",
					prim.UsedPercent),
			})
		} else if prim.UsedPercent >= 90 {
			text := fmt.Sprintf("Approaching codex rate limit (%d%% of the current window).", prim.UsedPercent)
			if !resetAt.IsZero() {
				text += fmt.Sprintf(" Resets at %s.", resetAt.Local().Format(time.Kitchen))
			}
			s.emit(event.Event{
				Type:      event.TypeNotice,
				SessionID: s.localID,
				Timestamp: now,
				Text:      text,
			})
		}
	case "mcpServer/startupStatus/updated":
		// MADR 0035 D9: a successful MCP startup needs no line; a failed
		// one is worth a notice so the user can see "tools mysteriously
		// absent" before they ask.
		var p struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			break
		}
		if p.Status == "failed" {
			msg := p.Error
			if msg == "" {
				msg = "MCP server failed to start"
			}
			s.emit(event.Event{
				Type:      event.TypeNotice,
				SessionID: s.localID,
				Timestamp: now,
				Text:      fmt.Sprintf("MCP server %q failed to start: %s", p.Name, msg),
			})
		}
	default:
		s.log.Debug("codex: unhandled notification", slog.String("method", method))
	}
}

func (s *session) handleServerRequest(method string, id json.RawMessage, params json.RawMessage) {
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval":
		s.handleApprovalRequest(method, id, params)
	case "item/tool/requestUserInput":
		s.handleUserInputRequest(method, id, params)
	case "item/tool/call":
		s.rejectServerRequest(id, "dynamic tool calls not supported")
	case "mcpServer/elicitation/request":
		s.rejectServerRequest(id, "MCP elicitation not supported")
	case "account/chatgptAuthTokens/refresh":
		s.rejectServerRequest(id, "token refresh not supported — manage credentials outside mcremote")
	case "attestation/generate":
		s.rejectServerRequest(id, "attestation not supported")
	default:
		s.log.Debug("codex: unhandled server request", slog.String("method", method))
		s.rejectServerRequest(id, "method not found: "+method)
	}
}

func (s *session) rejectServerRequest(id json.RawMessage, message string) {
	fr := s.p.framer()
	if fr != nil {
		_ = fr.sendResponse(context.Background(), id, nil, &rpcErrorBody{
			Code: -32601, Message: message,
		})
	}
}

// describeApproval extracts the tool name and human description for one
// approval request. Every branch must set a tool name: a sheet that describes
// nothing is a sheet the user cannot make a decision about, and
// `item/permissions/requestApproval` used to fall through the switch with both
// fields empty.
func describeApproval(method string, params json.RawMessage) (toolName, text string) {
	switch method {
	case "item/commandExecution/requestApproval":
		var p struct {
			Command string `json:"command"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(params, &p)
		return "command", firstNonEmpty(p.Command, p.Reason)
	case "item/fileChange/requestApproval":
		var p struct {
			FilePath string `json:"filePath"`
			Path     string `json:"path"`
			Reason   string `json:"reason"`
		}
		_ = json.Unmarshal(params, &p)
		return "file", firstNonEmpty(p.FilePath, p.Path, p.Reason)
	case "item/permissions/requestApproval":
		var p struct {
			Reason      string `json:"reason"`
			Permission  string `json:"permission"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal(params, &p)
		return "permission", firstNonEmpty(p.Reason, p.Description, p.Permission)
	default:
		return "approval", ""
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

func (s *session) handleApprovalRequest(method string, id json.RawMessage, params json.RawMessage) {
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()

	// Belt and braces over approvalPolicy="never": codex can still route
	// approvals the policy does not cover (MCP elicitations, and whatever the
	// granular policy handles once experimentalApi is negotiated). Answering
	// them here keeps one user-visible contract — "auto means you will not be
	// asked" — regardless of which layer the engine uses (MADR 0044 D6).
	if s.cfg.AlwaysApprove || auto {
		toolName, text := describeApproval(method, params)
		if send := s.responder(); send != nil {
			if err := send(context.Background(), id, map[string]any{
				"decision": "accept",
			}, nil); err != nil {
				// No retry: this is a local JSON-RPC write, so a failure means
				// the connection is gone and the turn is already dead. Unlike
				// the OpenCode path, there is nothing transient to wait out.
				s.log.Warn("auto-approve reply failed",
					slog.String("tool", toolName), slog.String("err", err.Error()))
				return
			}
		}
		s.log.Info("auto-approved approval request",
			slog.String("agent_session_id", s.AgentSessionID()),
			slog.String("tool", toolName),
		)
		// Auto-approve must never mean invisible: the user has to be able to
		// scroll back and see what was allowed on their behalf. One collapsing
		// card per turn, not one notice line per approval (MADR 0051).
		s.noteAutoApproval(toolName, text)
		return
	}

	toolName, text := describeApproval(method, params)
	permID := uuid.NewString()
	s.mu.Lock()
	// Record the description alongside the rpc id: a later sweep folds this
	// permission into the approval audit and cannot re-derive it from params.
	s.trackPendingLocked(permID, pendingPerm{rpcID: id, tool: toolName, detail: text})
	s.mu.Unlock()

	if text == "" {
		// Better a generic label than a blank sheet.
		text = "codex is requesting approval"
	}

	// Option ids are codex's own decision enums (spike inventory, 0.145.0):
	// commandExecution and fileChange both accept accept / acceptForSession /
	// decline / cancel. Sending anything outside them fails the approval.
	opts := []event.PermissionOption{
		{OptionID: "accept", Name: "Allow once", Kind: "allow_once"},
	}
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		opts = append(opts, event.PermissionOption{
			OptionID: "acceptForSession", Name: "Allow for session", Kind: "allow_always",
		})
	}
	opts = append(opts,
		event.PermissionOption{OptionID: "decline", Name: "Deny", Kind: "deny"},
		event.PermissionOption{OptionID: "cancel", Name: "Cancel", Kind: "cancel"},
	)

	s.emit(event.Event{
		Type:           event.TypePermission,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		PermissionID:   permID,
		Options:        opts,
		ToolName:       toolName,
		Text:           text,
		Status:         "pending",
		AgentSessionID: s.agentID,
	})

	if s.permTimeout > 0 {
		timer := time.AfterFunc(s.permTimeout, func() {
			s.mu.Lock()
			pend, ok := s.pendingPerms[permID]
			if ok {
				s.dropPendingLocked(permID)
				s.noteAnsweredLocked(permID)
			}
			s.mu.Unlock()
			if !ok {
				return
			}
			if send := s.responder(); send != nil {
				_ = send(context.Background(), pend.rpcID, map[string]any{
					"decision": "cancel",
				}, nil)
			}
			s.emit(event.Event{
				Type:           event.TypePermissionResolved,
				SessionID:      s.localID,
				Timestamp:      time.Now().UTC(),
				PermissionID:   permID,
				Status:         event.PermissionStatusCancelled,
				TimedOut:       true,
				AgentSessionID: s.agentID,
			})
		})
		// Stop the timer when the session ends, so a long permTimeout (15
		// minutes by default) does not keep a closed session's timer alive.
		go func() {
			<-s.done
			timer.Stop()
		}()
	}
}

func (s *session) handleUserInputRequest(method string, id json.RawMessage, params json.RawMessage) {
	_ = method
	var p struct {
		Questions []struct {
			ID       string   `json:"id"`
			Question string   `json:"question"`
			Header   string   `json:"header"`
			Type     string   `json:"type"`
			Options  []string `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(params, &p); err != nil || len(p.Questions) == 0 {
		s.rejectServerRequest(id, "invalid requestUserInput params")
		return
	}

	questionID := uuid.NewString()
	s.mu.Lock()
	s.pendingQuestions[questionID] = id
	s.mu.Unlock()

	questions := make([]event.QuestionItem, 0, len(p.Questions))
	for _, q := range p.Questions {
		opts := make([]event.PermissionOption, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, event.PermissionOption{OptionID: o, Name: o})
		}
		questions = append(questions, event.QuestionItem{
			Header:   q.Header,
			Text:     q.Question,
			Multiple: q.Type == "multi-select",
			Options:  opts,
		})
	}

	s.emit(event.Event{
		Type:           event.TypeQuestion,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		QuestionID:     questionID,
		Questions:      questions,
		AgentSessionID: s.agentID,
	})
}

func (s *session) RespondQuestion(ctx context.Context, questionID string, answers [][]string, cancelled bool) error {
	s.mu.Lock()
	rpcID, ok := s.pendingQuestions[questionID]
	delete(s.pendingQuestions, questionID)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown question: %s", questionID)
	}

	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}

	if cancelled {
		return fr.sendResponse(ctx, rpcID, nil, &rpcErrorBody{
			Code: -32800, Message: "cancelled",
		})
	}

	answerList := make([]map[string]any, 0, len(answers))
	for _, ans := range answers {
		answerList = append(answerList, map[string]any{
			"answers": ans,
		})
	}

	return fr.sendResponse(ctx, rpcID, map[string]any{
		"answers": answerList,
	}, nil)
}

func (s *session) serverDied() {
	s.mu.Lock()
	closed := s.closed
	s.closed = true
	s.steerable = false
	s.mu.Unlock()
	if closed {
		return
	}
	s.drainChunks()
	// The engine is gone, so no answer can reach it — but the phone is still
	// holding any sheet that was up, and only a resolved event closes it.
	s.cancelPendingPermissions("engine lost")
	s.cancelPendingQuestions("engine lost")
	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "disconnected",
		AgentSessionID: s.agentID,
	})
	s.emit(event.Event{
		Type:           event.TypeError,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Error:          "engine lost",
		AgentSessionID: s.agentID,
	})
	close(s.done)
}

// sandboxConfining reports whether the session's live sandbox policy
// confines the agent to the workspace (0069 D4.5). Empty means "inherit
// codex's own config" — unknown, so no hint is offered.
func (s *session) sandboxConfining() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sandboxMode == "read-only" || s.sandboxMode == "workspace-write"
}

func (s *session) clearTurnBusy() {
	s.mu.Lock()
	s.turnBusy = false
	s.turnID = ""
	s.steerable = false
	s.mu.Unlock()
}

// genericTurnError is what an "error" stop reports when the engine failed the
// turn without saying why. MADR 0036 D1 makes the error event unconditional,
// so the client can render the failure from one place; this is the fallback
// text for the path that used to emit nothing at all.
const genericTurnError = "The agent's turn failed."

// emitTurnComplete is the single implementation of "the turn is over" used
// by the turn/completed notification and the runTurn RPC-failure path.
// MADR 0035 D5: stop is already a daemon-vocabulary reason (mapped via
// codexStopReason).
//
// MADR 0036 D1 — the invariant this function exists to hold: a stop reason of
// "error" is ALWAYS accompanied by a TypeError for the same turn. The mobile
// reducer relies on it to suppress the "Turn ended (error)" system line, so an
// error stop that emitted no error event would be a completely silent failure.
// When the engine supplied no message we substitute a generic one rather than
// skipping the event.
//
// MADR 0035 D7: the explicit drainChunks() that used to live here was
// redundant — emit(TypeTurnComplete) already drains the pending run
// through the chunkbuf boundary path, in order, on the blocking path.
func (s *session) emitTurnComplete(stop, turnErrMsg string) {
	now := time.Now().UTC()
	// Close the approval card before the turn boundary, so it is marked done
	// ahead of turn_complete rather than left running into the next turn.
	s.finishApprovals()
	// Same for the sub-agent panel. This is also the only completion signal for
	// entries seeded from subAgentActivity, whose `kind` enum
	// (started|interacted|interrupted) has no terminal value.
	s.clearSubagents()
	s.emit(event.Event{
		Type:           event.TypeTurnComplete,
		SessionID:      s.localID,
		Timestamp:      now,
		StopReason:     stop,
		Status:         stop,
		AgentSessionID: s.agentID,
	})
	if stop == "error" && turnErrMsg == "" {
		turnErrMsg = genericTurnError
	}
	if turnErrMsg != "" {
		// Present like every other provider: 429/529/quota get natural-
		// language copy + RetryAt. A permission denial under a confining
		// sandbox gets the mode-pointing hint: Seatbelt/bwrap EPERM out of
		// the workspace is the sandbox by design, and "grant Full Disk
		// Access" would be the wrong advice (MADR 0069 D4.5, F5).
		cls := agenterr.Present(turnErrMsg, now)
		msg := cls.Message
		if msg == "" {
			msg = clip(turnErrMsg, 400)
		}
		if cls.Kind == agenterr.KindPermission && s.sandboxConfining() {
			msg += " — this session's mode sandboxes the agent to the " +
				"workspace. Switch session modes, or enable " +
				"allow_full_access on the host to offer Full access."
		}
		s.emit(event.Event{
			Type:           event.TypeError,
			SessionID:      s.localID,
			Timestamp:      now,
			Error:          msg,
			ErrorKind:      string(cls.Kind),
			RetryAt:        cls.ResetAt,
			AgentSessionID: s.agentID,
		})
	}
	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      now,
		Status:         "idle",
		AgentSessionID: s.agentID,
	})
}

// codexStopReason maps the codex turn_status enum onto the daemon's
// stop-reason vocabulary. The mobile reducer (transcript_reducer.dart:296)
// prints a system bubble for anything it does not recognise, so an
// unmapped value becomes visible noise ("Turn ended (completed)").
//
//	completed   -> end_turn   (the mobile reducer's silent no-op)
//	interrupted -> cancelled  (the mobile reducer's "Turn cancelled")
//	failed      -> error      (paired with a TypeError carrying turn.error)
func codexStopReason(status string) string {
	switch status {
	case "completed":
		return "end_turn"
	case "interrupted":
		return "cancelled"
	case "failed":
		return "error"
	}
	return status
}

func (s *session) tryDrainQueue() {
	s.mu.Lock()
	if s.closed || len(s.promptQueue) == 0 {
		s.mu.Unlock()
		return
	}
	if s.turnBusy && !s.steerable {
		s.mu.Unlock()
		return
	}
	next := s.promptQueue[0]
	s.promptQueue[0] = nil
	s.promptQueue = s.promptQueue[1:]
	steerable := s.steerable
	s.mu.Unlock()

	if steerable {
		if err := s.steerTurn(context.Background(), next); err != nil {
			s.log.Warn("queued steer failed", slog.String("err", err.Error()))
			s.emitClassifiedError(err.Error())
		}
		s.tryDrainQueue()
		return
	}

	if err := s.beginTurn(context.Background(), next, false); err != nil {
		if errors.Is(err, provider.ErrTurnBusy) {
			s.mu.Lock()
			s.promptQueue = append([][]provider.Content{next}, s.promptQueue...)
			s.mu.Unlock()
			return
		}
		s.log.Warn("queued prompt failed", slog.String("err", err.Error()))
		s.emitClassifiedError(err.Error())
		s.tryDrainQueue()
	}
}

// emitClassifiedError surfaces a TypeError with agenterr Present so 429/529
// and quota dumps land as natural-language limit cards (shared with the
// turn-complete error path).
func (s *session) emitClassifiedError(raw string) {
	cls := agenterr.Present(raw, time.Now())
	msg := cls.Message
	if msg == "" {
		msg = clip(raw, 400)
	}
	s.emit(event.Event{
		Type:           event.TypeError,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Error:          msg,
		ErrorKind:      string(cls.Kind),
		RetryAt:        cls.ResetAt,
		AgentSessionID: s.agentID,
	})
}

// noteProviderLimit aborts an in-flight turn when engine stderr reports a
// quota/rate-limit (shared path with goose/acphttp — MADR 0073 F1).
//
// Codex returns from turn/start quickly and then streams; a hang after start
// has no request to cancel, so we force a classified turn end instead.
func (s *session) noteProviderLimit(raw string) {
	if !agenterr.IsLimit(raw, time.Now()) {
		return
	}
	s.mu.Lock()
	if s.closed || !s.turnBusy || s.limitNotified {
		s.mu.Unlock()
		return
	}
	s.limitNotified = true
	s.limitRaw = raw
	cancel := s.turnCancel
	// Past turn/start: clear busy here so a later engine completion cannot
	// double-emit turn_complete.
	pastStart := cancel == nil
	if pastStart {
		s.turnBusy = false
		s.turnID = ""
		s.steerable = false
	}
	s.mu.Unlock()
	s.log.Warn("codex provider limit detected",
		slog.String("session_id", s.localID),
		slog.String("text", raw),
	)
	if cancel != nil {
		cancel()
		return
	}
	s.emitTurnComplete("error", raw)
	s.tryDrainQueue()
}

func (s *session) emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.AgentSessionID == "" && !chunkbuf.IsChunk(ev.Type) {
		s.mu.Lock()
		ev.AgentSessionID = s.agentID
		s.mu.Unlock()
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
			continue
		}
		_ = s.chunks.Unflush(e)
		deadline = time.Now().Add(chunkRetryDelay)
	}
	s.emitMu.Unlock()

	if deadline.IsZero() {
		s.stopFlush()
		return
	}
	s.armFlush(deadline)
}

func (s *session) chunkBuffer() *chunkbuf.Buffer {
	if s.chunks == nil {
		// WithToolLane: supersede non-terminal tool_call_update per id
		// (MADR 0057 M-2 / OpenCode parity).
		s.chunks = chunkbuf.New(s.cfg.streamCoalesceWindow(), maxPendingChunkBytes, chunkbuf.WithToolLane())
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
		_ = s.chunks.Unflush(ev)
		retry = true
	}
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

func (s *session) drainChunks() {
	s.stopFlush()
	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	tools := s.chunkBuffer().DrainTools()
	s.emitMu.Unlock()
	if ok {
		s.trySend(ev)
	}
	for _, t := range tools {
		s.trySend(t)
	}
}

// drainChunksClose is the close-path flush (MADR 0035 D7). No control event
// follows here, so the buffered run is delivered via trySend (non-blocking
// — the manager pump may already be gone). If the consumer could not
// accept it, the chunkbuf's Unflush returns the byte count that did not
// fit, which we log at warn so a full channel at session close is
// visible in the daemon's log rather than silently swallowed.
func (s *session) drainChunksClose() {
	s.stopFlush()
	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	tools := s.chunkBuffer().DrainTools()
	dropped := 0
	if ok {
		if !s.trySend(ev) {
			dropped = s.chunks.Unflush(ev)
		}
	}
	for _, t := range tools {
		s.trySend(t)
	}
	s.emitMu.Unlock()
	if dropped > 0 {
		s.log.Warn("close: stream buffer overflow; discarded text",
			slog.Int("bytes", dropped))
	}
}

func (s *session) emitUserMessage(parts []provider.Content) {
	var text string
	var attachments []event.AttachmentInfo
	for _, p := range parts {
		switch p.Type {
		case "image":
			attachments = append(attachments, event.AttachmentInfo{Kind: "image", MimeType: p.MimeType})
		default:
			text += p.Text
		}
	}
	s.emit(event.Event{
		Type:           event.TypeUserMessage,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Text:           text,
		Attachments:    attachments,
		AgentSessionID: s.agentID,
	})
}

func buildPrompt(parts []provider.Content) (string, []map[string]any, []event.AttachmentInfo) {
	var text strings.Builder
	blocks := make([]map[string]any, 0, len(parts))
	var attachments []event.AttachmentInfo
	for _, p := range parts {
		switch p.Type {
		case "image":
			if len(p.Data) > maxDataURLSize {
				continue
			}
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":     "base64",
					"data":     p.Data,
					"mimeType": p.MimeType,
				},
			})
			attachments = append(attachments, event.AttachmentInfo{Kind: "image", MimeType: p.MimeType})
		default:
			text.WriteString(p.Text)
			blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
		}
	}
	return text.String(), blocks, attachments
}

func firstOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// clip truncates s to max runes, returning the original when it fits.
// The daemon's TypeError events use this so a 5MB error from the engine
// does not flood the wire.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// emitCapabilities advertises the session's capabilities so a phone can
// gate UI (e.g. the image-attach button) without a live probe. Emitted
// once at session create. MADR 0035 D4.
//
// Values, each justified rather than assumed:
//   - Image:       codex implements image input end-to-end (buildPrompt +
//     dataURL/MIME handling, session.go:26,1183-1217) and all
//     six codex models advertise inputModalities [text,image]
//     (MADR 0028 §16.3).
//   - LoadSession: thread/resume verified OK (MADR 0028 §16.3).
//
// The ACP-specific fields (EmbeddedContext, MCPHTTP, MCPSSE, MCPACP,
// ListSessions, CloseSession) are left false: codex has no equivalent
// negotiation and claiming them would be a guess.
func (s *session) emitCapabilities() {
	s.emit(event.Event{
		Type:      event.TypeSessionCapabilities,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Capabilities: &event.Capabilities{
			Image:       true,
			LoadSession: true,
		},
		AgentSessionID: s.agentID,
	})
}

func cloneContent(parts []provider.Content) []provider.Content {
	out := make([]provider.Content, len(parts))
	copy(out, parts)
	for i := range out {
		out[i].Text = parts[i].Text
		out[i].Data = parts[i].Data
		out[i].MimeType = parts[i].MimeType
	}
	return out
}
