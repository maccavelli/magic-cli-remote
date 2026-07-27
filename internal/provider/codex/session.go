package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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

	pendingPerms     map[string]json.RawMessage
	pendingQuestions map[string]json.RawMessage
	permTimeout      time.Duration
	stallNotice      time.Duration

	emitMu sync.Mutex
	chunks *chunkbuf.Buffer

	flushMu    sync.Mutex
	flushTimer *time.Timer
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
		dir, _ = os.Getwd()
	}
	s := &session{
		p:                p,
		cfg:              cfg,
		opts:             opts,
		localID:          localID,
		cwd:              dir,
		events:           make(chan event.Event, 256),
		done:             make(chan struct{}),
		pendingPerms:     make(map[string]json.RawMessage),
		pendingQuestions: make(map[string]json.RawMessage),
		permTimeout:      cfg.PermissionTimeout,
		stallNotice:      cfg.TurnStallNotice,
		log:              log.With(slog.String("session", localID)),
	}
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

func (s *session) startNew(ctx context.Context, fr *conn) error {
	params := map[string]any{}
	if s.cwd != "" {
		params["cwd"] = s.cwd
	}
	if s.cfg.SandboxMode != "" {
		params["sandbox"] = map[string]any{
			"type":          s.cfg.SandboxMode,
			"networkAccess": false,
		}
	}
	if s.cfg.ApprovalPolicy != "" {
		params["approvalPolicy"] = s.cfg.ApprovalPolicy
	}
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

	_, err := fr.sendRequest(ctx, "thread/resume", params)
	if err != nil {
		return fmt.Errorf("thread/resume: %w", err)
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

	turnCtx := context.WithoutCancel(ctx)
	go s.runTurn(turnCtx, fr, blocks)
	return nil
}

func (s *session) runTurn(ctx context.Context, fr *conn, blocks []map[string]any) {
	s.lastActivity.Store(time.Now().UnixNano())

	params := map[string]any{
		"threadId": s.agentID,
		"input":    blocks,
	}
	s.mu.Lock()
	model := s.opts.Model
	if model == "" {
		model = s.cfg.Model
	}
	s.mu.Unlock()
	if model != "" {
		params["model"] = model
	}
	raw, err := fr.sendRequest(ctx, "turn/start", params)
	if err != nil {
		s.mu.Lock()
		wasBusy := s.turnBusy
		s.turnBusy = false
		s.turnID = ""
		s.steerable = false
		s.mu.Unlock()
		if !wasBusy {
			return
		}
		if errors.Is(err, errConnLost) || errors.Is(err, context.Canceled) {
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

	s.mu.Lock()
	for qID, rID := range s.pendingQuestions {
		delete(s.pendingQuestions, qID)
		_ = rID
	}
	s.mu.Unlock()

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

func (s *session) Fork(ctx context.Context, messageID string) (string, error) {
	fr := s.p.framer()
	if fr == nil {
		return "", fmt.Errorf("engine not running")
	}
	params := map[string]any{
		"threadId": s.agentID,
	}
	if messageID != "" {
		params["turnId"] = messageID
	}
	raw, err := fr.sendRequest(ctx, "thread/fork", params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("thread/fork: decode: %w", err)
	}
	return resp.Thread.ID, nil
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
	s.mu.Unlock()
	return nil
}

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

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	s.mu.Lock()
	rpcID, ok := s.pendingPerms[permissionID]
	delete(s.pendingPerms, permissionID)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown permission: %s", permissionID)
	}

	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}

	var decision string
	if cancelled {
		decision = "cancel"
	} else {
		decision = optionID
	}

	return fr.sendResponse(ctx, rpcID, map[string]any{
		"decision": decision,
	}, nil)
}

func (s *session) handleNotification(method string, params json.RawMessage) {
	now := time.Now().UTC()
	// MADR 0035 D8: one atomic store per notification. The stall ticker
	// (started in newSession) reads this on each tick; no per-chunk
	// timer allocation.
	s.lastActivity.Store(now.UnixNano())

	switch method {
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
				Error:     fmt.Sprintf("Codex rate limit reached (%d%% of window). Pausing.", prim.UsedPercent),
			})
		} else if prim.UsedPercent >= 90 {
			text := fmt.Sprintf("Approaching codex rate limit (%d%%).", prim.UsedPercent)
			if !resetAt.IsZero() {
				text += fmt.Sprintf(" Resets at %s.", resetAt.Format(time.RFC3339))
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

func (s *session) handleApprovalRequest(method string, id json.RawMessage, params json.RawMessage) {
	if s.cfg.AlwaysApprove {
		fr := s.p.framer()
		if fr != nil {
			_ = fr.sendResponse(context.Background(), id, map[string]any{
				"decision": "accept",
			}, nil)
		}
		return
	}

	permID := uuid.NewString()
	s.mu.Lock()
	s.pendingPerms[permID] = id
	s.mu.Unlock()

	var text string
	var toolName string
	switch method {
	case "item/commandExecution/requestApproval":
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			text = p.Command
			toolName = "command"
		}
	case "item/fileChange/requestApproval":
		var p struct {
			FilePath string `json:"filePath"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			text = p.FilePath
			toolName = "file"
		}
	}

	opts := []event.PermissionOption{
		{OptionID: "accept", Name: "Allow once", Kind: "allow_once"},
	}
	if method == "item/commandExecution/requestApproval" {
		opts = append(opts, event.PermissionOption{OptionID: "acceptForSession", Name: "Allow for session", Kind: "allow_always"})
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
		time.AfterFunc(s.permTimeout, func() {
			s.mu.Lock()
			rID, ok := s.pendingPerms[permID]
			delete(s.pendingPerms, permID)
			s.mu.Unlock()
			if !ok {
				return
			}
			fr := s.p.framer()
			if fr != nil {
				_ = fr.sendResponse(context.Background(), rID, map[string]any{
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

func (s *session) clearTurnBusy() {
	s.mu.Lock()
	s.turnBusy = false
	s.turnID = ""
	s.steerable = false
	s.mu.Unlock()
}

// emitTurnComplete is the single implementation of "the turn is over" used
// by the turn/completed notification and the runTurn RPC-failure path.
// MADR 0035 D5: stop is already a daemon-vocabulary reason (mapped via
// codexStopReason); turnErrMsg is non-empty when the turn failed and the
// engine surfaced a message — in that case we also emit a TypeError so
// the failure is reported rather than swallowed.
//
// MADR 0035 D7: the explicit drainChunks() that used to live here was
// redundant — emit(TypeTurnComplete) already drains the pending run
// through the chunkbuf boundary path, in order, on the blocking path.
func (s *session) emitTurnComplete(stop, turnErrMsg string) {
	now := time.Now().UTC()
	s.emit(event.Event{
		Type:           event.TypeTurnComplete,
		SessionID:      s.localID,
		Timestamp:      now,
		StopReason:     stop,
		Status:         stop,
		AgentSessionID: s.agentID,
	})
	if turnErrMsg != "" {
		s.emit(event.Event{
			Type:           event.TypeError,
			SessionID:      s.localID,
			Timestamp:      now,
			Error:          clip(turnErrMsg, 400),
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
			s.emit(event.Event{
				Type:      event.TypeError,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Error:     err.Error(),
			})
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
		s.emit(event.Event{
			Type:      event.TypeError,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Error:     err.Error(),
		})
		s.tryDrainQueue()
	}
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
		s.chunks = chunkbuf.New(s.cfg.streamCoalesceWindow(), maxPendingChunkBytes)
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
	s.emitMu.Unlock()

	if retry {
		s.armFlush(time.Now().Add(chunkRetryDelay))
	}
}

func (s *session) drainChunks() {
	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	if ok {
		s.trySend(ev)
	}
	s.emitMu.Unlock()
	s.stopFlush()
}

// drainChunksClose is the close-path flush (MADR 0035 D7). No control event
// follows here, so the buffered run is delivered via trySend (non-blocking
// — the manager pump may already be gone). If the consumer could not
// accept it, the chunkbuf's Unflush returns the byte count that did not
// fit, which we log at warn so a full channel at session close is
// visible in the daemon's log rather than silently swallowed.
func (s *session) drainChunksClose() {
	s.emitMu.Lock()
	ev, ok := s.chunkBuffer().Drain()
	dropped := 0
	if ok {
		if !s.trySend(ev) {
			dropped = s.chunks.Unflush(ev)
		}
	}
	s.emitMu.Unlock()
	s.stopFlush()
	if dropped > 0 {
		s.log.Warn("close: stream buffer overflow; discarded text",
			slog.Int("bytes", dropped))
	}
}

// resetStallTimer is retained as a no-op so older callers compile. The
// MADR 0035 D8 implementation stamps lastActivity directly on every
// notification and turn start; the stall ticker is started in newSession
// and stopped by closing s.done in Close.
func (s *session) resetStallTimer() {
	s.lastActivity.Store(time.Now().UnixNano())
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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
