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
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/chunkbuf"
	"github.com/maccavelli/magic-cli-remote/internal/event"
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
	stallTimer  *time.Timer

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
	return &session{
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
	s.resetStallTimer()

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
			s.emitTurnComplete("cancelled")
		} else {
			s.emitTurnComplete("error")
			s.emit(event.Event{
				Type:           event.TypeError,
				SessionID:      s.localID,
				Timestamp:      time.Now().UTC(),
				Error:          err.Error(),
				AgentSessionID: s.agentID,
			})
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

	s.drainChunks()

	s.mu.Lock()
	if s.stallTimer != nil {
		s.stallTimer.Stop()
		s.stallTimer = nil
	}
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

func (s *session) SetModel(ctx context.Context, model string) error {
	s.mu.Lock()
	s.opts.Model = model
	s.mu.Unlock()
	return nil
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
	s.resetStallTimer()

	switch method {
	case "turn/completed":
		var p struct {
			Turn struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			status := p.Turn.Status
			s.drainChunks()
			s.emit(event.Event{
				Type:           event.TypeTurnComplete,
				SessionID:      s.localID,
				Timestamp:      now,
				StopReason:     status,
				Status:         status,
				AgentSessionID: s.agentID,
			})
			s.emit(event.Event{
				Type:           event.TypeSessionStatus,
				SessionID:      s.localID,
				Timestamp:      now,
				Status:         "idle",
				AgentSessionID: s.agentID,
			})
			s.mu.Lock()
			s.turnBusy = false
			s.turnID = ""
			s.steerable = false
			s.mu.Unlock()
			s.tryDrainQueue()
		}
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
		var p struct {
			ItemID   string          `json:"itemId"`
			TurnID   string          `json:"turnId"`
			ItemType string          `json:"itemType"`
			Item     json.RawMessage `json:"item"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			switch p.ItemType {
			case "commandExecution":
				var item struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(p.Item, &item); err == nil {
					s.emit(event.Event{
						Type:           event.TypeToolCall,
						SessionID:      s.localID,
						Timestamp:      now,
						ToolID:         p.ItemID,
						ToolName:       "command",
						ToolKind:       "execute",
						Text:           item.Command,
						Status:         "in_progress",
						AgentSessionID: s.agentID,
					})
				}
			case "fileChange":
				var item struct {
					FilePath string `json:"filePath"`
				}
				if err := json.Unmarshal(p.Item, &item); err == nil {
					s.emit(event.Event{
						Type:           event.TypeToolCall,
						SessionID:      s.localID,
						Timestamp:      now,
						ToolID:         p.ItemID,
						ToolName:       "file",
						ToolKind:       "edit",
						Text:           item.FilePath,
						Status:         "in_progress",
						AgentSessionID: s.agentID,
					})
				}
			case "collabAgentToolCall":
				var item struct {
					AgentName string `json:"agentName"`
					Prompt    string `json:"prompt"`
				}
				if err := json.Unmarshal(p.Item, &item); err == nil {
					s.emit(event.Event{
						Type:           event.TypeToolCall,
						SessionID:      s.localID,
						Timestamp:      now,
						ToolID:         p.ItemID,
						ToolName:       firstOr(item.AgentName, "collab agent"),
						ToolKind:       "think",
						Text:           truncate(item.Prompt, 400),
						Status:         "in_progress",
						AgentSessionID: s.agentID,
					})
				}
			case "subAgentActivity":
				var item struct {
					AgentName    string `json:"agentName"`
					Goal         string `json:"goal"`
					Instructions string `json:"instructions"`
				}
				if err := json.Unmarshal(p.Item, &item); err == nil {
					text := firstOr(item.Goal, item.Instructions)
					s.emit(event.Event{
						Type:           event.TypeToolCall,
						SessionID:      s.localID,
						Timestamp:      now,
						ToolID:         p.ItemID,
						ToolName:       firstOr(item.AgentName, "sub-agent"),
						ToolKind:       "think",
						Text:           truncate(text, 400),
						Status:         "in_progress",
						AgentSessionID: s.agentID,
					})
				}
			default:
				s.emit(event.Event{
					Type:           event.TypeToolCall,
					SessionID:      s.localID,
					Timestamp:      now,
					ToolID:         p.ItemID,
					ToolName:       p.ItemType,
					Status:         "in_progress",
					AgentSessionID: s.agentID,
				})
			}
		}
	case "item/completed":
		var p struct {
			ItemID   string `json:"itemId"`
			ItemType string `json:"itemType"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			if p.ItemType != "agentMessage" && p.ItemType != "userMessage" && p.ItemType != "reasoning" {
				s.emit(event.Event{
					Type:           event.TypeToolUpdate,
					SessionID:      s.localID,
					Timestamp:      now,
					ToolID:         p.ItemID,
					Status:         "completed",
					AgentSessionID: s.agentID,
				})
			}
		}
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
				Status:         "in_progress",
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
		// Phase 3
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

func (s *session) emitTurnComplete(status string) {
	s.drainChunks()
	s.emit(event.Event{
		Type:           event.TypeTurnComplete,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		StopReason:     status,
		Status:         status,
		AgentSessionID: s.agentID,
	})
	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "idle",
		AgentSessionID: s.agentID,
	})
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
