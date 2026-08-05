package acphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/chunkbuf"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpcommon"
	"github.com/maccavelli/magic-cli-remote/internal/provider/sessionutil"
)

// maxPromptQueue is the per-session FIFO depth for prompts sent while a turn
// is active (acpagent parity, MADR 0020 Sprint 3).
const maxPromptQueue = 4

// defaultStreamCoalesce caps mid-stream assistant/thought updates at ~12 per
// second — finer granularity is coalesced away by the phone regardless
// (MADR 0024).
const defaultStreamCoalesce = 80 * time.Millisecond

// maxPendingChunkBytes force-flushes a coalesced run that grows large before
// its window elapses (resync tails, replay bursts), capping per-event size.
const maxPendingChunkBytes = 8 << 10

// turnCooldown is how long after the last session/update notification the
// daemon waits before emitting turn_complete. session/prompt may return
// before all streaming updates have been flushed to the transport, and
// emitting turn_complete immediately would make the phone declare the turn
// done while tool results and text are still appearing.
const turnCooldown = 500 * time.Millisecond

// chunkRetryDelay re-arms a flush whose non-blocking send lost to a slow pump.
var chunkRetryDelay = 50 * time.Millisecond

// errConnLost unblocks in-flight RPCs when the engine socket dies underneath
// them. Distinct from wire JSON-RPC errors so turn code can tell "engine
// gone" (already announced via serverDied) from a real agent error.
var errConnLost = errors.New("engine connection lost")

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
	loading     bool
	promptQueue [][]provider.Content
	events      chan event.Event
	done        chan struct{}
	stallTimer  *time.Timer

	staticModes []event.SessionMode

	// configMu guards configOpts: the last session config options the agent
	// reported. Retained rather than only emitted because for goose these ARE
	// the model catalog — a `provider` select of 71 values and a `model` select
	// scoped to whichever one is current (MADR 0043 D6).
	configMu   sync.Mutex
	configOpts []event.ConfigOption

	pendingPerms      map[string]json.RawMessage
	pendingPermsMu    sync.Mutex
	pendingPermTimers map[string]*time.Timer
	// answeredPerms remembers ids already answered, so a repeat answer (a
	// resync replay, or a second device) is a quiet no-op instead of an
	// "unknown permission" error the client turns into a re-present loop.
	// Bounded by maxAnsweredPerms.
	answeredPerms map[string]struct{}
	answeredOrder []string

	permTimeout time.Duration
	stallNotice time.Duration

	promptReturned bool
	lastUpdateAt   time.Time

	// emitMu serializes the streaming coalescer AND the delivery of what it
	// returns (chunkbuf contract: Add/Drain/Unflush and their sends are one
	// critical section, or a timed flush can be overtaken by a boundary).
	emitMu sync.Mutex
	chunks *chunkbuf.Buffer
	// flushMu guards flushTimer only.
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
		// Production resolves via provider.ResolveSessionCWD in
		// startSession (0069 P1); this covers direct test construction.
		// Never os.Getwd(): under launchd/systemd the process cwd is an
		// accident of the unit file.
		dir, _ = os.UserHomeDir()
	}
	return &session{
		p:                 p,
		cfg:               cfg,
		opts:              opts,
		localID:           localID,
		cwd:               dir,
		events:            make(chan event.Event, 256),
		done:              make(chan struct{}),
		pendingPerms:      make(map[string]json.RawMessage),
		pendingPermTimers: make(map[string]*time.Timer),
		staticModes:       p.spec.StaticModes,
		permTimeout:       cfg.PermissionTimeout,
		stallNotice:       cfg.TurnStallNotice,
		log:               log.With(slog.String("session", localID)),
	}
}

func (s *session) ID() string { return s.localID }

func (s *session) ProviderID() provider.ID { return s.p.spec.ID }

func (s *session) AgentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentID
}

func (s *session) CWD() string { return s.cwd }

func (s *session) Events() <-chan event.Event { return s.events }

const loadTimeout = 120 * time.Second

func (s *session) create(ctx context.Context) error {
	mcpServers, skippedMCP := mcpServersForCapabilities(s.cfg.McpServers, s.p.caps())
	for _, mcp := range skippedMCP {
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text: fmt.Sprintf("MCP server %q was not attached: the agent does not support the configured %q transport.",
				mcp.Name, mcp.Transport),
		})
	}

	if s.opts.AgentSessionID != "" {
		return s.load(ctx, mcpServers)
	}
	return s.createNew(ctx, mcpServers)
}

func (s *session) createNew(ctx context.Context, mcpServers []acp.McpServer) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	params := acp.NewSessionRequest{
		Cwd:        s.cwd,
		McpServers: mcpServers,
	}
	if s.opts.Name != "" {
		params.Meta = map[string]any{"name": s.opts.Name}
	}
	raw, err := fr.sendRequest(ctx, "session/new", params)
	if err != nil {
		return err
	}
	var resp acp.NewSessionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("session/new: decode: %w", err)
	}
	s.agentID = string(resp.SessionId)
	s.emitModesOrStatic(resp.Modes)
	s.emitConfigOptions(resp.ConfigOptions)
	s.emitCapabilities(s.p.caps())

	model := s.opts.Model
	if model == "" {
		model = s.cfg.Model
	}
	if model != "" {
		if err := s.applyModel(ctx, model); err != nil {
			return err
		}
	}
	return nil
}

// applyModel sets the session's model, and its model provider first when the
// id is qualified.
//
// Order matters: the agent re-scopes its model list when the provider changes,
// so setting `model` before `provider` would apply an id the engine is about to
// stop offering. An unqualified id sets only `model`, which is the pre-0043
// behaviour and what a persisted session or a hand-typed id needs.
func (s *session) applyModel(ctx context.Context, model string) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	set := func(configID, value string) error {
		_, err := fr.sendRequest(ctx, "session/set_config_option", map[string]any{
			"sessionId": s.agentID,
			"configId":  configID,
			"value":     value,
		})
		return err
	}
	mp, id := splitQualifiedModel(model)
	if mp != "" {
		if err := set(configOptionProvider, mp); err != nil {
			// The agent reports this as a bare internal error with no detail
			// (goose: -32603), and the real cause is almost always missing
			// credentials — which the user can act on, unlike the raw text.
			s.log.Debug("set model provider failed",
				slog.String("provider", mp), slog.String("err", err.Error()))
			return fmt.Errorf("%s has no credentials for %s — run `%s configure` on the host",
				s.p.spec.ID, mp, s.p.cfg.Bin)
		}
	}
	if err := set(configOptionModel, id); err != nil {
		return fmt.Errorf("set requested model %q: %w", model, err)
	}
	return nil
}

// SetModel implements [provider.ModelSession]: switch model in place, keeping
// the conversation. Before MADR 0043 D6 goose's /model was KindDaemon, which
// relaunches the agent and costs it the conversation — for a switch the agent
// can do without restarting.
func (s *session) SetModel(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model is required")
	}
	return s.applyModel(ctx, model)
}

func (s *session) load(ctx context.Context, mcpServers []acp.McpServer) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.loading = true
	s.agentID = s.opts.AgentSessionID
	s.mu.Unlock()

	loadCtx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()

	params := acp.LoadSessionRequest{
		Cwd:        s.cwd,
		McpServers: mcpServers,
		SessionId:  acp.SessionId(s.opts.AgentSessionID),
	}
	raw, err := fr.sendRequest(loadCtx, "session/load", params)
	if err != nil {
		s.mu.Lock()
		s.loading = false
		s.mu.Unlock()
		return err
	}

	var resp acp.LoadSessionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		s.mu.Lock()
		s.loading = false
		s.mu.Unlock()
		return fmt.Errorf("session/load: decode: %w", err)
	}

	s.mu.Lock()
	s.loading = false
	s.mu.Unlock()

	s.log.Info("acp session loaded", slog.String("agent_session_id", s.agentID))
	s.emitModesOrStatic(resp.Modes)
	s.emitConfigOptions(resp.ConfigOptions)
	s.emitCapabilities(s.p.caps())
	return nil
}

// Prompt submits parts to the agent. When a turn is already active the prompt
// joins a FIFO queue (echoing its user bubble immediately) and drains in
// order; a full queue is refused with provider.ErrTurnBusy. Otherwise the
// turn starts now and runs detached — Prompt returns once the turn is
// accepted, not when it ends, so a slow agent does not pin the phone's
// request handler.
func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnBusy {
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

// beginTurn claims the turn and submits parts to the agent. emitUser controls
// whether a user_message is emitted (false when draining a queue entry that
// already showed the user bubble at enqueue time).
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
	// A prompt with no sendable content would issue a no-op turn; refuse.
	if len(blocks) == 0 {
		s.clearTurnBusy()
		err := fmt.Errorf("prompt has no sendable content (text empty; attachments unsupported by agent)")
		if !emitUser {
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

	fr, err := s.p.framer()
	if err != nil {
		s.clearTurnBusy()
		if !emitUser {
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
	// what history replay is for). Explicit Cancel() and engine teardown
	// remain the ways to stop a turn.
	turnCtx := context.WithoutCancel(ctx)
	go s.runTurn(turnCtx, fr, blocks)
	return nil
}

// runTurn waits on the session/prompt response and emits the turn lifecycle:
// turn_complete for EVERY stop reason (gating on end_turn alone left the
// phone stuck "running" after cancellations and refusals), an error event for
// failures, and the idle status afterwards.
func (s *session) runTurn(ctx context.Context, fr rpcFramer, blocks []acp.ContentBlock) {
	defer func() {
		s.mu.Lock()
		s.turnBusy = false
		s.promptReturned = false
		s.mu.Unlock()
		s.resetStallTimer()
		// Drain the next queued prompt only after the turn fully ends.
		s.tryDrainQueue()
	}()

	// Arm the stall watchdog now that a turn is active.
	s.resetStallTimer()

	params := map[string]any{
		"sessionId": s.agentID,
		"prompt":    blocks,
	}
	raw, err := fr.sendRequest(ctx, "session/prompt", params)
	if err != nil {
		// Cancel/close and engine loss need no scary bubble: engine loss is
		// already announced by serverDied ("engine lost"), and a local close
		// is the user's own doing.
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
		// Classify on the full error text — quota/rate-limit hints (and their
		// reset times) often live past the first line.
		cls := agenterr.Classify(err.Error(), time.Now())
		s.emit(event.Event{
			Type:       event.TypeTurnComplete,
			SessionID:  s.localID,
			Timestamp:  time.Now().UTC(),
			StopReason: "error",
			Status:     "error",
		})
		s.emit(event.Event{
			Type:      event.TypeError,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Error:     err.Error(),
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

	stopReason := "end_turn"
	var r struct {
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(raw, &r) == nil && r.StopReason != "" {
		stopReason = r.StopReason
	}

	// session/prompt may return before every streaming update has been
	// flushed to the transport. Wait until the update stream goes quiet
	// so turn_complete does not precede the last tool result or text
	// chunk (goose engine behaviour observed in practice).
	s.mu.Lock()
	s.promptReturned = true
	s.mu.Unlock()
	s.waitForCooldown()

	s.emit(event.Event{
		Type:       event.TypeTurnComplete,
		SessionID:  s.localID,
		Timestamp:  time.Now().UTC(),
		StopReason: stopReason,
		Status:     stopReason,
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "idle",
	})
}

// isBenignPromptErr reports whether a session/prompt failure is just the turn
// ending underneath us (cancel, session close, engine loss) rather than an
// agent error the user should see. Engine loss is announced separately via
// serverDied, so reporting it here too would double the error bubbles.
func isBenignPromptErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrEngineDown) || errors.Is(err, errConnLost) {
		return true
	}
	// A write to a socket that is already closing: the engine-loss path
	// announces it.
	return websocket.CloseStatus(err) != -1
}

func (s *session) clearTurnBusy() {
	s.mu.Lock()
	s.turnBusy = false
	s.promptReturned = false
	s.mu.Unlock()
}

// waitForCooldown polls lastUpdateAt until no session/update has arrived for
// at least turnCooldown, then returns. A closed session or engine death skips
// the wait (the defer-shutdown path still emits the tail events).
func (s *session) waitForCooldown() {
	for {
		s.mu.Lock()
		last := s.lastUpdateAt
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		wait := turnCooldown - time.Since(last)
		if wait <= 0 {
			return
		}
		select {
		case <-time.After(wait):
		case <-s.done:
			return
		}
	}
}

// tryDrainQueue starts the next queued prompt when the session is idle.
// Failures are reported and draining continues so one bad entry cannot
// strand the queue.
func (s *session) tryDrainQueue() {
	s.mu.Lock()
	if s.closed || s.turnBusy || len(s.promptQueue) == 0 {
		s.mu.Unlock()
		return
	}
	next := s.promptQueue[0]
	s.promptQueue[0] = nil
	s.promptQueue = s.promptQueue[1:]
	s.mu.Unlock()

	// Background drain uses a detached context — the original phone request
	// is long gone; cancel/close remain the stop paths.
	if err := s.beginTurn(context.Background(), next, false); err != nil {
		if errors.Is(err, provider.ErrTurnBusy) {
			// Lost a race with a direct Prompt: put the entry back at the
			// head so it runs after that turn instead of vanishing (its user
			// bubble is already on screen).
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

func (s *session) Cancel(ctx context.Context) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	// ACP defines session/cancel as a notification. Sending it as a request
	// makes Goose return JSON-RPC -32601 because it has no request handler.
	// Also discard queued prompts: cancelling a turn must not immediately start
	// another prompt the user asked to stop.
	s.mu.Lock()
	s.promptQueue = nil
	s.mu.Unlock()
	return fr.sendNotification(ctx, "session/cancel", map[string]any{
		"sessionId": s.agentID,
	})
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// Land any coalesced streaming text before the stream ends.
	s.drainChunks()

	if fr, err := s.p.framer(); err == nil {
		_, _ = fr.sendRequest(ctx, "session/close", map[string]any{
			"sessionId": s.agentID,
		})
	}
	s.p.mu.Lock()
	delete(s.p.sessions, s.agentID)
	s.p.mu.Unlock()

	s.mu.Lock()
	if s.stallTimer != nil {
		s.stallTimer.Stop()
		s.stallTimer = nil
	}
	s.mu.Unlock()

	s.pendingPermsMu.Lock()
	for id, t := range s.pendingPermTimers {
		t.Stop()
		delete(s.pendingPermTimers, id)
		delete(s.pendingPerms, id)
	}
	s.pendingPermsMu.Unlock()

	close(s.done)
	return nil
}

// maxAnsweredPerms bounds the answered-id set (see [session.answeredPerms]).
const maxAnsweredPerms = 256

// RespondPermission answers one agent permission request.
//
// Three properties, matching codex's: the same three were missing here, and
// only the rate of goose's approval prompts kept it from being noticed.
//
//   - emit permission_resolved, so the client's sheet closes and the daemon's
//     history ring records the resolution. Without it a reconnect or resync
//     replays the unresolved permission_request and re-raises the sheet.
//   - retire the pending entry only after the reply reaches the agent, so a
//     failed write leaves the request retryable instead of stranding it.
//   - treat a repeat answer as a no-op that re-announces the resolution,
//     rather than an error the client turns into a re-present loop.
func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	s.pendingPermsMu.Lock()
	rpcID, ok := s.pendingPerms[permissionID]
	_, alreadyAnswered := s.answeredPerms[permissionID]
	s.pendingPermsMu.Unlock()

	if !ok {
		if alreadyAnswered {
			s.emitPermissionResolved(permissionID, cancelled)
			return nil
		}
		return fmt.Errorf("unknown permission: %s", permissionID)
	}

	if err := s.replyPermission(ctx, rpcID, optionID, cancelled); err != nil {
		// Still outstanding agent-side; allow a retry.
		return err
	}

	s.pendingPermsMu.Lock()
	delete(s.pendingPerms, permissionID)
	if t, ok := s.pendingPermTimers[permissionID]; ok {
		t.Stop()
		delete(s.pendingPermTimers, permissionID)
	}
	s.noteAnsweredLocked(permissionID)
	s.pendingPermsMu.Unlock()

	s.emitPermissionResolved(permissionID, cancelled)
	return nil
}

// noteAnsweredLocked records an id as answered, evicting the oldest past the
// cap. Caller holds pendingPermsMu.
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

// emitPermissionResolved tells clients the request is answered.
func (s *session) emitPermissionResolved(permissionID string, cancelled bool) {
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.emit(event.Event{
		Type:         event.TypePermissionResolved,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permissionID,
		Status:       status,
	})
}

// replyPermission answers an agent session/request_permission JSON-RPC request
// with a standard ACP RequestPermissionResponse on the same request id.
func (s *session) replyPermission(ctx context.Context, rpcID json.RawMessage, optionID string, cancelled bool) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	var result any
	if cancelled {
		result = map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		}
	} else {
		result = map[string]any{
			"outcome": map[string]any{
				"outcome":  "selected",
				"optionId": optionID,
			},
		}
	}
	return fr.sendResponse(ctx, rpcID, result, nil)
}

func (s *session) SetMode(ctx context.Context, modeID string) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	_, err = fr.sendRequest(ctx, "session/set_mode", map[string]any{
		"sessionId": s.agentID,
		"modeId":    modeID,
	})
	return err
}

func (s *session) SetConfigOption(ctx context.Context, optionID, kind, value string) error {
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	params := map[string]any{
		"sessionId": s.agentID,
		"configId":  optionID,
	}
	if kind == "boolean" {
		params["type"] = "boolean"
		params["value"] = value == "true" || value == "1"
	} else {
		params["value"] = value
	}
	_, err = fr.sendRequest(ctx, "session/set_config_option", params)
	return err
}

func (s *session) serverDied() {
	s.mu.Lock()
	closed := s.closed
	s.closed = true
	s.mu.Unlock()
	if closed {
		return
	}
	// Land any coalesced streaming text ahead of the death notice.
	s.drainChunks()
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "disconnected",
	})
	s.emit(event.Event{
		Type:      event.TypeError,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Error:     "engine lost",
	})
	close(s.done)
}

func (s *session) emitCapabilities(caps acp.AgentCapabilities) {
	pc := caps.PromptCapabilities
	sc := caps.SessionCapabilities
	mcp := caps.McpCapabilities
	s.emit(event.Event{
		Type:      event.TypeSessionCapabilities,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Capabilities: &event.Capabilities{
			Image:           pc.Image,
			Audio:           pc.Audio,
			LoadSession:     caps.LoadSession,
			EmbeddedContext: pc.EmbeddedContext,
			ListSessions:    sc.List != nil,
			CloseSession:    sc.Close != nil,
			MCPHTTP:         mcp.Http,
			MCPSSE:          mcp.Sse,
			MCPACP:          mcp.Acp,
		},
	})
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
		Modes:         modes,
		CurrentModeID: string(st.CurrentModeId),
	})
}

func (s *session) emitModesOrStatic(st *acp.SessionModeState) {
	if st != nil && len(st.AvailableModes) > 0 {
		s.emitModes(st)
		return
	}
	if len(s.staticModes) == 0 {
		return
	}
	current := s.p.spec.DefaultModeID
	if current == "" {
		current = s.staticModes[0].ID
	}
	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		Modes:         s.staticModes,
		CurrentModeID: current,
	})
}

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
	s.retainConfigOptions(out)
	s.emit(event.Event{
		Type:          event.TypeSessionConfig,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		ConfigOptions: out,
	})
}

// retainConfigOptions merges the newest options into the session's snapshot and
// refreshes the provider-level catalog cache.
//
// A config update may carry a subset (goose re-emits only what changed), so
// merging by id is required: replacing wholesale would drop the `provider`
// option the moment a `model` change arrives on its own.
func (s *session) retainConfigOptions(opts []event.ConfigOption) {
	s.configMu.Lock()
	merged := make([]event.ConfigOption, 0, len(s.configOpts)+len(opts))
	merged = append(merged, s.configOpts...)
	for _, o := range opts {
		replaced := false
		for i := range merged {
			if merged[i].ID == o.ID {
				merged[i] = o
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, o)
		}
	}
	s.configOpts = merged
	snapshot := append([]event.ConfigOption(nil), merged...)
	s.configMu.Unlock()
	// Any live session keeps the pre-session picker warm for free, which is
	// what makes the probe session (Provider.catalogSession) a rarity.
	s.p.noteConfigOptions(snapshot)
}

// configOption returns the retained option with this id.
func (s *session) configOption(id string) (event.ConfigOption, bool) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	for _, o := range s.configOpts {
		if o.ID == id {
			return o, true
		}
	}
	return event.ConfigOption{}, false
}

// ModelCatalog implements [provider.ModelCatalogSession]: goose's model catalog
// is its ACP session config options, and it is already provider-scoped — the
// `model` select lists only the current `provider`'s models (MADR 0043 D6).
func (s *session) ModelCatalog(ctx context.Context, scope string) (picker.Catalog, error) {
	_ = ctx
	if scope == provider.CatalogScopeProviders {
		opt, ok := s.configOption(configOptionProvider)
		if !ok {
			return picker.Catalog{}, fmt.Errorf("session reports no provider option")
		}
		return providerCatalogFromOption(opt), nil
	}
	modelOpt, ok := s.configOption(configOptionModel)
	if !ok {
		return picker.Catalog{}, fmt.Errorf("session reports no model option")
	}
	mp := ""
	if p, ok := s.configOption(configOptionProvider); ok {
		mp = p.CurrentValue
	}
	return modelCatalogFromOption(modelOpt, mp), nil
}

func (s *session) handleUpdate(updateJSON json.RawMessage) {
	var u acp.SessionUpdate
	if err := json.Unmarshal(updateJSON, &u); err != nil {
		s.log.Debug("handleUpdate: unmarshal", slog.String("err", err.Error()))
		return
	}
	now := time.Now().UTC()

	s.resetStallTimer()

	// Record the last update time so runTurn can wait for the stream to
	// settle before emitting turn_complete (guards against goose sending
	// updates after the session/prompt response).
	s.mu.Lock()
	s.lastUpdateAt = now
	s.mu.Unlock()

	switch {
	case u.AgentMessageChunk != nil:
		// Whitespace-only chunks are real content mid-message: token-granular
		// streams deliver paragraph breaks as standalone "\n\n" chunks, and
		// trimming them jammed paragraphs together (the acpagent lesson this
		// transport re-learned the hard way). Only fully empty chunks are
		// noise; the client skips whitespace that would OPEN a message.
		text := contentBlockText(u.AgentMessageChunk.Content)
		if text == "" {
			return
		}
		s.emit(event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: s.localID,
			Timestamp: now,
			Text:      text,
		})
	case u.AgentThoughtChunk != nil:
		text := contentBlockText(u.AgentThoughtChunk.Content)
		if text == "" {
			return
		}
		s.emit(event.Event{
			Type:      event.TypeThoughtChunk,
			SessionID: s.localID,
			Timestamp: now,
			Text:      text,
		})
	case u.UserMessageChunk != nil:
		// Skip: Prompt() already emits a single user_message with the full
		// text. ACP echoes the prompt back as user_message_chunk updates,
		// which would duplicate bubbles in the client.
		s.log.Debug("ignoring acp user_message_chunk (already emitted on prompt)",
			slog.String("session_id", s.localID))
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
			Text:      summarizeTCContent(tc.Content, tc.RawInput, nil, 400),
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
			Text:      summarizeTCContent(tu.Content, tu.RawInput, tu.RawOutput, 600),
		})
	case u.AvailableCommandsUpdate != nil:
		raw := u.AvailableCommandsUpdate.AvailableCommands
		cmds := make([]event.AvailableCommand, 0, len(raw))
		for _, c := range raw {
			name := strings.TrimSpace(c.Name)
			if name == "" {
				continue
			}
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
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   acpcommon.PlanEntries(u.Plan.Entries),
		})
	case u.PlanRemoved != nil:
		s.emit(event.Event{
			Type:      event.TypePlan,
			SessionID: s.localID,
			Timestamp: now,
			Entries:   []event.PlanEntry{},
		})
	case u.UsageUpdate != nil:
		s.emit(event.Event{
			Type:      event.TypeUsage,
			SessionID: s.localID,
			Timestamp: now,
			Usage:     &event.Usage{Used: u.UsageUpdate.Used, Size: u.UsageUpdate.Size},
		})
	case u.CurrentModeUpdate != nil:
		s.emit(event.Event{
			Type:          event.TypeMode,
			SessionID:     s.localID,
			Timestamp:     now,
			CurrentModeID: string(u.CurrentModeUpdate.CurrentModeId),
		})
	case u.ConfigOptionUpdate != nil:
		s.emitConfigOptions(u.ConfigOptionUpdate.ConfigOptions)
	case u.SessionInfoUpdate != nil:
		info := u.SessionInfoUpdate
		if info.Title != nil {
			s.emit(event.Event{
				Type:      event.TypeSessionTitle,
				SessionID: s.localID,
				Timestamp: now,
				Title:     *info.Title,
			})
		}
	case u.PlanUpdate != nil:
		if items := u.PlanUpdate.Plan.Items; items != nil {
			s.emit(event.Event{
				Type:      event.TypePlan,
				SessionID: s.localID,
				Timestamp: now,
				Entries:   acpcommon.PlanEntries(items.Entries),
			})
		}
	default:
		s.log.Debug("unhandled session update")
	}
}

type permissionRequestParams struct {
	SessionID acp.SessionId          `json:"sessionId"`
	Options   []acp.PermissionOption `json:"options"`
	ToolCall  acp.ToolCallUpdate     `json:"toolCall"`
}

// handlePermissionRequest processes an agent-initiated session/request_permission
// JSON-RPC request. rpcID is the request's JSON-RPC id (must be echoed in the response).
func (s *session) handlePermissionRequest(rpcID json.RawMessage, requestJSON json.RawMessage) {
	var req permissionRequestParams
	if err := json.Unmarshal(requestJSON, &req); err != nil {
		s.log.Debug("handlePermissionRequest: unmarshal", slog.String("err", err.Error()))
		if fr, ferr := s.p.framer(); ferr == nil {
			_ = fr.sendResponse(context.Background(), rpcID, nil, &rpcErrorBody{
				Code: -32602, Message: "invalid params",
			})
		}
		return
	}
	if s.cfg.AlwaysApprove {
		_ = s.autoAllow(rpcID, req)
		return
	}
	permID := uuid.NewString()
	opts := make([]event.PermissionOption, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, event.PermissionOption{
			OptionID: string(o.OptionId),
			Name:     o.Name,
			Kind:     string(o.Kind),
		})
	}
	title := ""
	if req.ToolCall.Title != nil {
		title = strings.TrimSpace(*req.ToolCall.Title)
	}
	toolID := string(req.ToolCall.ToolCallId)
	detail := summarizeTCContent(req.ToolCall.Content, req.ToolCall.RawInput, nil, 400)
	if strings.TrimSpace(detail) == "" {
		detail = title
	}
	s.pendingPermsMu.Lock()
	s.pendingPerms[permID] = append(json.RawMessage(nil), rpcID...)
	if s.permTimeout > 0 {
		s.pendingPermTimers[permID] = time.AfterFunc(s.permTimeout, func() {
			s.pendingPermsMu.Lock()
			rID, ok := s.pendingPerms[permID]
			delete(s.pendingPerms, permID)
			delete(s.pendingPermTimers, permID)
			if ok {
				s.noteAnsweredLocked(permID)
			}
			s.pendingPermsMu.Unlock()
			if !ok {
				return
			}
			s.log.Warn("permission timed out",
				slog.String("permission_id", permID),
				slog.Duration("timeout", s.permTimeout))
			_ = s.replyPermission(context.Background(), rID, "", true)
			s.emit(event.Event{
				Type:         event.TypePermissionResolved,
				SessionID:    s.localID,
				Timestamp:    time.Now().UTC(),
				PermissionID: permID,
				Status:       event.PermissionStatusCancelled,
				TimedOut:     true,
			})
		})
	}
	s.pendingPermsMu.Unlock()

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
}

func (s *session) autoAllow(rpcID json.RawMessage, req permissionRequestParams) error {
	if _, err := s.p.framer(); err != nil {
		return err
	}
	if len(req.Options) == 0 {
		return nil
	}
	var firstAllow string
	for _, o := range req.Options {
		k := string(o.Kind)
		if k == "allow_once" || k == "allow_always" ||
			strings.Contains(k, "allow") || strings.Contains(k, "approve") {
			firstAllow = string(o.OptionId)
			break
		}
	}
	if firstAllow == "" {
		firstAllow = string(req.Options[0].OptionId)
	}
	return s.replyPermission(context.Background(), rpcID, firstAllow, false)
}

// emit delivers ev, coalescing high-frequency streaming text into ~one event
// per [Config.StreamCoalesceWindow] instead of one per model token
// (MADR 0024), and blocking for control events until consumed or closed —
// the manager pump is attached from the moment Start returns, and control
// events are never dropped (acpagent R5=A parity).
//
// Two invariants hold regardless of the window:
//
//   - The first chunk after any control event is delivered immediately, so
//     time-to-first-token is unchanged.
//   - A control event is a boundary: pending text is delivered ahead of it,
//     so turn_complete can never precede the text it terminates.
func (s *session) emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	s.mu.Lock()
	loading := s.loading
	// Stamp the agent session id on everything but high-frequency chunks
	// (wire-noise parity with acpagent).
	if ev.AgentSessionID == "" && !chunkbuf.IsChunk(ev.Type) {
		ev.AgentSessionID = s.agentID
	}
	s.mu.Unlock()
	if loading {
		ev.Replay = true
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
			// Telemetry (usage_update, available_commands): a stale report
			// self-corrects on the next one, so dropping stays correct.
			s.log.Warn("dropping event; slow consumer", slog.String("type", string(e.Type)))
			continue
		}
		// Reply text is never dropped: hand it back and retry on the next
		// tick (MADR 0024).
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

// chunkBuffer returns the streaming coalescer, building it on first use so no
// construction path (tests assemble sessions field by field) can reach emit
// with a nil buffer. Caller holds emitMu.
func (s *session) chunkBuffer() *chunkbuf.Buffer {
	if s.chunks == nil {
		// WithToolLane: supersede non-terminal tool_call_update per id so a
		// streaming tool does not emit one WS frame per upstream delta
		// (MADR 0057 M-2 / OpenCode parity).
		s.chunks = chunkbuf.New(s.cfg.StreamCoalesceWindow(), maxPendingChunkBytes, chunkbuf.WithToolLane())
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
// path in emit does, so a stalled pump cannot wedge a timer goroutine.
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
	// Held tool updates are control events: deliver with blocking send.
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

// drainChunks flushes any buffered streaming text on the way out of a session
// (Close, serverDied) so the tail of a reply is not lost.
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

func (s *session) resetStallTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stallTimer != nil {
		s.stallTimer.Stop()
		s.stallTimer = nil
	}
	if s.stallNotice > 0 && s.turnBusy {
		s.stallTimer = time.AfterFunc(s.stallNotice, func() {
			s.mu.Lock()
			busy := s.turnBusy
			s.mu.Unlock()
			if busy {
				s.emit(event.Event{
					Type:      event.TypeNotice,
					SessionID: s.localID,
					Timestamp: time.Now().UTC(),
					Text:      "The agent has been silent for " + s.stallNotice.String() + ". It may still be working.",
				})
			}
		})
	}
}

func buildMcpServers(cfgs []McpServer) []acp.McpServer {
	servers, _ := mcpServersForCapabilities(cfgs, acp.AgentCapabilities{})
	return servers
}

// mcpServersForCapabilities returns the MCP servers the agent can accept and
// the configured entries it cannot. Empty capabilities are treated as unknown
// (rather than unsupported) for callers that construct a request before ACP
// initialize; once initialized, an explicit false capability is authoritative.
func mcpServersForCapabilities(cfgs []McpServer, caps acp.AgentCapabilities) ([]acp.McpServer, []McpServer) {
	out := make([]acp.McpServer, 0, len(cfgs))
	skipped := make([]McpServer, 0)
	mcp := caps.McpCapabilities
	haveHTTP := mcp.Http || (!mcp.Http && !mcp.Sse)
	haveSSE := mcp.Sse || (!mcp.Http && !mcp.Sse)
	for _, m := range cfgs {
		switch m.Transport {
		case "http", "":
			if !haveHTTP {
				skipped = append(skipped, m)
				continue
			}
			out = append(out, acp.McpServer{Http: &acp.McpServerHttpInline{
				Name:    m.Name,
				Type:    "http",
				Url:     m.URL,
				Headers: convertHeaders(m.Headers),
			}})
		case "sse":
			if !haveSSE {
				skipped = append(skipped, m)
				continue
			}
			out = append(out, acp.McpServer{Sse: &acp.McpServerSseInline{
				Name:    m.Name,
				Type:    "sse",
				Url:     m.URL,
				Headers: convertHeaders(m.Headers),
			}})
		}
	}
	return out, skipped
}

func convertHeaders(h map[string]string) []acp.HttpHeader {
	out := make([]acp.HttpHeader, 0, len(h))
	for field, value := range h {
		out = append(out, acp.HttpHeader{
			Name:  field,
			Value: value,
		})
	}
	return out
}

// buildPrompt flattens parts into the transcript text for the user bubble,
// the ACP content blocks for the wire, and attachment descriptors. Text parts
// keep their text verbatim on both paths.
func buildPrompt(parts []provider.Content) (string, []acp.ContentBlock, []event.AttachmentInfo) {
	var text strings.Builder
	blocks := make([]acp.ContentBlock, 0, len(parts))
	var attachments []event.AttachmentInfo
	for _, p := range parts {
		switch p.Type {
		case "image":
			blocks = append(blocks, acp.ContentBlock{
				Image: &acp.ContentBlockImage{Type: "image", MimeType: p.MimeType, Data: p.Data},
			})
			attachments = append(attachments, event.AttachmentInfo{Kind: "image", MimeType: p.MimeType})
		default:
			text.WriteString(p.Text)
			blocks = append(blocks, acp.ContentBlock{
				Text: &acp.ContentBlockText{Type: "text", Text: p.Text},
			})
		}
	}
	return text.String(), blocks, attachments
}

// emitUserMessage records the user turn in the transcript at enqueue time so
// the bubble shows what the user sent even though the agent has not started
// on it yet.
func (s *session) emitUserMessage(parts []provider.Content) {
	ev := sessionutil.UserMessage(parts)
	ev.SessionID = s.localID
	ev.Timestamp = time.Now().UTC()
	s.emit(ev)
}

// contentBlockText returns chunk text verbatim. No TrimSpace: streaming
// chunks are fragments, and their edge whitespace is the spaces and newlines
// of the final message — trimming each fragment glued the reassembled words
// and paragraphs together ("text run together" bug). Only fully empty chunks
// are noise, and callers skip those.
func contentBlockText(cb acp.ContentBlock) string {
	if cb.Text != nil {
		return cb.Text.Text
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func summarizeTCContent(content []acp.ToolCallContent, rawInput, rawOutput any, max int) string {
	var parts []string
	for _, c := range content {
		if c.Content != nil && c.Content.Content.Text != nil {
			t := strings.TrimSpace(c.Content.Content.Text.Text)
			if t != "" {
				parts = append(parts, t)
			}
		}
	}
	rawStr := func(v any) string {
		switch x := v.(type) {
		case string:
			return x
		case []byte:
			return string(x)
		default:
			b, _ := json.Marshal(x)
			return string(b)
		}
	}
	if s := rawStr(rawInput); s != "" && s != "null" {
		if len(s) > max {
			s = s[:max] + "…"
		}
		parts = append(parts, s)
	}
	if s := rawStr(rawOutput); s != "" && s != "null" {
		if len(s) > max {
			s = s[:max] + "…"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}
