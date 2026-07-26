package acphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

type session struct {
	p       *Provider
	cfg     Config
	opts    provider.StartOptions
	localID string
	agentID string
	cwd     string
	log     *slog.Logger

	mu       sync.Mutex
	closed   bool
	turnBusy bool
	loading  bool
	events   chan event.Event
	done     chan struct{}

	agentCaps   acp.AgentCapabilities
	staticModes []event.SessionMode

	pendingPerms   map[string]string
	pendingPermsMu sync.Mutex
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
		p:            p,
		cfg:          cfg,
		opts:         opts,
		localID:      localID,
		cwd:          dir,
		events:       make(chan event.Event, 256),
		done:         make(chan struct{}),
		pendingPerms: make(map[string]string),
		staticModes:  p.spec.StaticModes,
		log:          log.With(slog.String("session", localID)),
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
	mcpServers := filterMcpServers(s.cfg.McpServers, s.p.agentCaps)

	if s.opts.AgentSessionID != "" {
		return s.load(ctx, mcpServers)
	}
	return s.createNew(ctx, mcpServers)
}

func (s *session) createNew(ctx context.Context, mcpServers []acp.McpServer) error {
	params := acp.NewSessionRequest{
		Cwd:        s.cwd,
		McpServers: mcpServers,
	}
	raw, err := s.p.wsFramer.sendRequest(ctx, "session/new", params)
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
	s.emitCapabilities(nil)

	model := s.opts.Model
	if model == "" {
		model = s.cfg.Model
	}
	if model != "" {
		cp := map[string]any{
			"sessionId": s.agentID,
			"optionId":  "model",
			"value":     model,
		}
		if _, err := s.p.wsFramer.sendRequest(ctx, "session/set_config_option", cp); err != nil {
			s.log.Warn("model override via set_config_option failed", slog.String("err", err.Error()))
		}
	}
	return nil
}

func (s *session) load(ctx context.Context, mcpServers []acp.McpServer) error {
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
	raw, err := s.p.wsFramer.sendRequest(loadCtx, "session/load", params)
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
	s.emitCapabilities(nil)
	return nil
}

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	s.turnBusy = true
	s.mu.Unlock()

	prompt := make([]acp.ContentBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			prompt = append(prompt, acp.ContentBlock{
				Text: &acp.ContentBlockText{Type: "text", Text: p.Text},
			})
		case "image":
			prompt = append(prompt, acp.ContentBlock{
				Image: &acp.ContentBlockImage{Type: "image", MimeType: p.MimeType, Data: p.Data},
			})
		default:
			prompt = append(prompt, acp.ContentBlock{
				Text: &acp.ContentBlockText{Type: "text", Text: p.Text},
			})
		}
	}
	params := map[string]any{
		"sessionId": s.agentID,
		"prompt":    prompt,
	}
	raw, err := s.p.wsFramer.sendRequest(ctx, "session/prompt", params)
	if err == nil {
		var r struct {
			StopReason string `json:"stopReason"`
		}
		if json.Unmarshal(raw, &r) == nil && r.StopReason == "end_turn" {
			s.emit(event.Event{
				Type:       event.TypeTurnComplete,
				SessionID:  s.localID,
				Timestamp:  time.Now().UTC(),
				StopReason: r.StopReason,
			})
		}
	}
	s.mu.Lock()
	s.turnBusy = false
	s.mu.Unlock()
	return err
}

func (s *session) Cancel(ctx context.Context) error {
	_, err := s.p.wsFramer.sendRequest(ctx, "session/cancel", map[string]any{
		"sessionId": s.agentID,
	})
	return err
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	_, _ = s.p.wsFramer.sendRequest(ctx, "session/close", map[string]any{
		"sessionId": s.agentID,
	})
	s.p.mu.Lock()
	delete(s.p.sessions, s.agentID)
	s.p.mu.Unlock()

	close(s.done)
	return nil
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	s.pendingPermsMu.Lock()
	reqID, ok := s.pendingPerms[permissionID]
	delete(s.pendingPerms, permissionID)
	s.pendingPermsMu.Unlock()
	if !ok {
		return fmt.Errorf("unknown permission: %s", permissionID)
	}
	optionVal := map[string]any{"optionId": optionID, "outcome": "selected"}
	if cancelled {
		optionVal = map[string]any{"outcome": "cancelled"}
	}
	_, err := s.p.wsFramer.sendRequest(ctx, "session/respond_permission", map[string]any{
		"sessionId": s.agentID,
		"requestId": reqID,
		"response":  optionVal,
	})
	return err
}

func (s *session) SetMode(ctx context.Context, modeID string) error {
	_, err := s.p.wsFramer.sendRequest(ctx, "session/set_config_option", map[string]any{
		"sessionId": s.agentID,
		"optionId":  "mode",
		"value":     modeID,
	})
	return err
}

func (s *session) SetConfigOption(ctx context.Context, optionID, kind, value string) error {
	_, err := s.p.wsFramer.sendRequest(ctx, "session/set_config_option", map[string]any{
		"sessionId": s.agentID,
		"optionId":  optionID,
		"value":     value,
	})
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

func (s *session) emitCapabilities(_ *acp.AgentCapabilities) {
	s.emit(event.Event{
		Type:      event.TypeSessionCapabilities,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Capabilities: &event.Capabilities{
			Image:       true,
			Audio:       false,
			LoadSession: true,
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
	s.emit(event.Event{
		Type:          event.TypeSessionConfig,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		ConfigOptions: out,
	})
}

func (s *session) handleUpdate(updateJSON json.RawMessage) {
	var u acp.SessionUpdate
	if err := json.Unmarshal(updateJSON, &u); err != nil {
		s.log.Debug("handleUpdate: unmarshal", slog.String("err", err.Error()))
		return
	}
	now := time.Now().UTC()

	switch {
	case u.AgentMessageChunk != nil:
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
			Entries:   mapPlanEntries(u.Plan.Entries),
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
	default:
		s.log.Debug("unhandled session update")
	}
}

type permissionRequestParams struct {
	RequestID string                 `json:"requestId"`
	SessionID acp.SessionId          `json:"sessionId"`
	Options   []acp.PermissionOption `json:"options"`
	ToolCall  acp.ToolCallUpdate     `json:"toolCall"`
}

func (s *session) handlePermissionRequest(requestJSON json.RawMessage) {
	var req permissionRequestParams
	if err := json.Unmarshal(requestJSON, &req); err != nil {
		s.log.Debug("handlePermissionRequest: unmarshal", slog.String("err", err.Error()))
		return
	}
	if s.cfg.AlwaysApprove {
		s.autoAllow(req)
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
	s.pendingPerms[permID] = req.RequestID
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

func (s *session) autoAllow(req permissionRequestParams) error {
	if len(req.Options) == 0 || s.p.wsFramer == nil {
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
	_, err := s.p.wsFramer.sendRequest(context.Background(), "session/respond_permission", map[string]any{
		"sessionId": s.agentID,
		"requestId": req.RequestID,
		"response":  map[string]any{"optionId": firstAllow, "outcome": "selected"},
	})
	return err
}

func (s *session) emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	s.mu.Lock()
	loading := s.loading
	s.mu.Unlock()
	if loading {
		ev.Replay = true
	}
	select {
	case s.events <- ev:
	case <-s.done:
	default:
	}
}

func buildMcpServers(cfgs []McpServer) []acp.McpServer {
	return filterMcpServers(cfgs, acp.AgentCapabilities{})
}

func filterMcpServers(cfgs []McpServer, caps acp.AgentCapabilities) []acp.McpServer {
	out := make([]acp.McpServer, 0, len(cfgs))
	mcp := caps.McpCapabilities
	haveHTTP := mcp.Http || (!mcp.Http && !mcp.Sse)
	haveSSE := mcp.Sse || (!mcp.Http && !mcp.Sse)
	for _, m := range cfgs {
		switch m.Transport {
		case "http", "":
			if !haveHTTP {
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
	return out
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

func contentBlockText(cb acp.ContentBlock) string {
	if cb.Text != nil {
		return strings.TrimSpace(cb.Text.Text)
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

func mapPlanEntries(entries []acp.PlanEntry) []event.PlanEntry {
	out := make([]event.PlanEntry, 0, len(entries))
	for _, e := range entries {
		status := string(e.Status)
		priority := string(e.Priority)
		out = append(out, event.PlanEntry{
			Content:  e.Content,
			Status:   status,
			Priority: priority,
		})
	}
	return out
}
