package httpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// session is one OpenCode server-side session multiplexed over the shared
// engine. There is no per-session process: Close tears down only local state,
// which is what makes resume free.
type session struct {
	p       *Provider
	localID string
	ocID    string
	cwd     string
	log     *slog.Logger
	events  chan event.Event
	done    chan struct{}

	// modelProvider/modelID ride on every prompt when set.
	modelProvider string
	modelID       string

	mu         sync.Mutex
	closed     bool
	turnActive bool
	// partText tracks cumulative text per part id so SSE part updates (which
	// carry the FULL text each time) become deltas.
	partText map[string]int
	// msgRole records message role by id; user-authored parts echo back over
	// SSE and must not become assistant chunks.
	msgRole map[string]string
	// pending permission requests by id (respond via REST).
	pending map[string]struct{}
	// seenTools distinguishes the first sighting of a tool call (tool_call)
	// from updates (tool_call_update).
	seenTools map[string]struct{}

	lastActivity atomic.Int64
}

var _ provider.Session = (*session)(nil)
var _ provider.PermissionSession = (*session)(nil)
var _ provider.CWDSession = (*session)(nil)

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return provider.IDOpencode }
func (s *session) AgentSessionID() string     { return s.ocID }
func (s *session) CWD() string                { return s.cwd }
func (s *session) Events() <-chan event.Event { return s.events }

// Start creates (or re-attaches to) a server-side session.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("opencode binary %q not found in PATH: %w", p.cfg.Bin, provider.ErrNotImplemented)
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
	if _, err := p.ensureServer(startCtx); err != nil {
		return nil, fmt.Errorf("opencode serve: %w", err)
	}

	localID := opts.LocalSessionID
	if localID == "" {
		localID = uuid.NewString()
	}

	model := opts.Model
	if model == "" {
		model = p.cfg.Model
	}
	mp, mid := splitModel(model)

	s := &session{
		p:             p,
		localID:       localID,
		cwd:           cwd,
		log:           p.log.With(slog.String("session_id", localID)),
		events:        make(chan event.Event, 256),
		done:          make(chan struct{}),
		modelProvider: mp,
		modelID:       mid,
		partText:      make(map[string]int),
		msgRole:       make(map[string]string),
		pending:       make(map[string]struct{}),
	}

	dir := "?directory=" + urlQueryEscape(cwd)
	if opts.AgentSessionID != "" {
		// Resume: the session already lives on the server — no replay, no
		// engine work. Verify it exists, then rebuild our history ring from
		// its message log (marked Replay so clients don't double-append).
		var info struct {
			ID string `json:"id"`
		}
		if err := p.api(startCtx, "GET", "/session/"+opts.AgentSessionID+dir, nil, &info); err != nil {
			return nil, fmt.Errorf("opencode session resume: %w", err)
		}
		s.ocID = info.ID
		p.register(s)
		s.replayMessages(startCtx, dir)
		s.log.Info("opencode http session resumed", slog.String("oc_session_id", s.ocID))
	} else {
		body := map[string]any{}
		if opts.Name != "" {
			body["title"] = opts.Name
		}
		if mid != "" {
			body["model"] = map[string]string{"providerID": mp, "id": mid}
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := p.api(startCtx, "POST", "/session"+dir, body, &created); err != nil {
			return nil, fmt.Errorf("opencode session create: %w", err)
		}
		if created.ID == "" {
			return nil, fmt.Errorf("opencode session create: empty id")
		}
		s.ocID = created.ID
		p.register(s)
		s.log.Info("opencode http session created", slog.String("oc_session_id", s.ocID))
	}

	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "idle",
		AgentSessionID: s.ocID,
	})
	return s, nil
}

// replayMessages rebuilds the daemon-side history ring for a resumed session
// from the server's message log (best-effort).
func (s *session) replayMessages(ctx context.Context, dir string) {
	var msgs []struct {
		Info struct {
			Role string `json:"role"`
		} `json:"info"`
		Parts []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Tool  string `json:"tool"`
			State struct {
				Status string `json:"status"`
				Title  string `json:"title"`
			} `json:"state"`
			CallID string `json:"callID"`
		} `json:"parts"`
	}
	if err := s.p.api(ctx, "GET", "/session/"+s.ocID+"/message"+dir, nil, &msgs); err != nil {
		s.log.Warn("resume message replay failed", slog.String("err", err.Error()))
		return
	}
	now := time.Now().UTC()
	for _, m := range msgs {
		for _, part := range m.Parts {
			var ev event.Event
			switch {
			case part.Type == "text" && m.Info.Role == "user":
				ev = event.Event{Type: event.TypeUserMessage, Text: part.Text}
			case part.Type == "text":
				ev = event.Event{Type: event.TypeAssistantChunk, Text: part.Text}
			case part.Type == "reasoning":
				ev = event.Event{Type: event.TypeThoughtChunk, Text: part.Text}
			case part.Type == "tool":
				ev = event.Event{
					Type:     event.TypeToolCall,
					ToolID:   part.CallID,
					ToolName: firstNonEmpty(part.State.Title, part.Tool, "tool"),
					ToolKind: kindForTool(part.Tool),
					Status:   mapToolStatus(part.State.Status),
				}
			default:
				continue
			}
			if strings.TrimSpace(ev.Text) == "" && ev.Type != event.TypeToolCall {
				continue
			}
			ev.SessionID = s.localID
			ev.Timestamp = now
			ev.Replay = true
			// Pre-attach (Start has not returned): never block — these only
			// feed the history ring, so dropping the OLDEST when the buffer
			// fills keeps the most recent conversation, mirroring the ring.
			for {
				select {
				case s.events <- ev:
				default:
					select {
					case <-s.events:
					default:
					}
					continue
				}
				break
			}
		}
	}
}

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnActive {
		s.mu.Unlock()
		return fmt.Errorf("prompt already in progress")
	}
	s.turnActive = true
	s.mu.Unlock()

	var text strings.Builder
	apiParts := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			text.WriteString(p.Text)
			apiParts = append(apiParts, map[string]any{"type": "text", "text": p.Text})
		}
	}

	body := map[string]any{"parts": apiParts}
	mp, mid := s.modelProvider, s.modelID
	if mid == "" {
		// No session/config model: use the engine-catalog fallback (the
		// server's own default resolution is broken upstream).
		mp, mid = s.p.fallbackModel()
	}
	if mid != "" {
		body["model"] = map[string]string{"providerID": mp, "modelID": mid}
	}

	// prompt_async returns immediately; the turn streams over SSE and ends
	// with session.idle.
	dir := "?directory=" + urlQueryEscape(s.cwd)
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.p.api(callCtx, "POST", "/session/"+s.ocID+"/prompt_async"+dir, body, nil); err != nil {
		s.mu.Lock()
		s.turnActive = false
		s.mu.Unlock()
		return err
	}

	now := time.Now().UTC()
	s.emit(event.Event{Type: event.TypeUserMessage, SessionID: s.localID, Timestamp: now, Text: text.String()})
	s.emit(event.Event{Type: event.TypeSessionStatus, SessionID: s.localID, Timestamp: now, Status: "running"})

	s.lastActivity.Store(time.Now().UnixNano())
	if s.p.cfg.TurnStallNotice > 0 {
		go s.watchStall()
	}
	return nil
}

// watchStall mirrors the ACP transport's stall notice: escalating back-off
// notices while a turn is active with no SSE activity.
func (s *session) watchStall() {
	threshold := s.p.cfg.TurnStallNotice
	tick := time.NewTicker(10 * time.Second)
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
			s.emit(event.Event{
				Type:      event.TypeNotice,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
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

func (s *session) Cancel(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	dir := "?directory=" + urlQueryEscape(s.cwd)
	return s.p.api(callCtx, "POST", "/session/"+s.ocID+"/abort"+dir, nil, nil)
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	s.mu.Lock()
	_, ok := s.pending[permissionID]
	delete(s.pending, permissionID)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown or expired permission %q", permissionID)
	}
	response := "reject"
	if !cancelled {
		switch optionID {
		case "once", "allow", "allow_once":
			response = "once"
		case "always", "allow_always":
			response = "always"
		}
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	dir := "?directory=" + urlQueryEscape(s.cwd)
	err := s.p.api(callCtx, "POST", "/session/"+s.ocID+"/permissions/"+permissionID+dir,
		map[string]string{"response": response}, nil)
	if err != nil {
		// Still outstanding server-side; allow a retry.
		s.mu.Lock()
		s.pending[permissionID] = struct{}{}
		s.mu.Unlock()
		return err
	}
	s.emit(event.Event{
		Type:         event.TypePermissionResolved,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permissionID,
		Status:       event.PermissionStatusResolved,
	})
	return nil
}

// Close releases local state only: the server-side session persists (that is
// exactly what makes resume instant). Purging happens via the daemon's
// session.delete flow when the user ends a session for good.
func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	pending := s.pending
	s.pending = map[string]struct{}{}
	s.mu.Unlock()

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
	s.p.unregister(s.ocID)
	return nil
}

// serverDied is invoked by the provider's death monitor.
func (s *session) serverDied() {
	now := time.Now().UTC()
	s.emit(event.Event{Type: event.TypeError, SessionID: s.localID, Timestamp: now,
		Error: "opencode server exited"})
	s.emit(event.Event{Type: event.TypeSessionStatus, SessionID: s.localID, Timestamp: now,
		Status: "disconnected"})
}

// emit delivers ev, blocking for control events until consumed or closed —
// same guarantees as the ACP transport (the manager pump is attached from
// the moment Start returns; Start emits only one status event before that,
// which the 256 buffer trivially holds).
func (s *session) emit(ev event.Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if isControl(ev.Type) {
		select {
		case s.events <- ev:
		case <-s.done:
		}
		return
	}
	select {
	case s.events <- ev:
	default:
		s.log.Warn("dropping event; slow consumer", slog.String("type", string(ev.Type)))
	}
}

func isControl(t event.Type) bool {
	switch t {
	case event.TypeSessionStatus, event.TypePermission, event.TypePermissionResolved,
		event.TypeTurnComplete, event.TypeError, event.TypeNotice,
		event.TypeToolCall, event.TypeToolUpdate, event.TypeUserMessage:
		return true
	default:
		return false
	}
}

// handleEvent translates one SSE event into daemon events.
func (s *session) handleEvent(typ string, props json.RawMessage) {
	s.lastActivity.Store(time.Now().UnixNano())
	now := time.Now().UTC()
	switch typ {
	case "message.updated":
		var p struct {
			Info struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"info"`
		}
		if json.Unmarshal(props, &p) == nil && p.Info.ID != "" {
			s.mu.Lock()
			s.msgRole[p.Info.ID] = p.Info.Role
			s.mu.Unlock()
		}

	case "message.part.updated":
		var p struct {
			Part struct {
				ID        string `json:"id"`
				MessageID string `json:"messageID"`
				Type      string `json:"type"`
				Text      string `json:"text"`
				Tool      string `json:"tool"`
				CallID    string `json:"callID"`
				State     struct {
					Status string          `json:"status"`
					Title  string          `json:"title"`
					Input  json.RawMessage `json:"input"`
					Output string          `json:"output"`
					Error  string          `json:"error"`
				} `json:"state"`
			} `json:"part"`
		}
		if json.Unmarshal(props, &p) != nil {
			return
		}
		part := p.Part
		s.mu.Lock()
		role := s.msgRole[part.MessageID]
		s.mu.Unlock()
		if role == "user" {
			return
		}
		switch part.Type {
		case "text", "reasoning":
			// Full-text snapshots → deltas.
			s.mu.Lock()
			prev := s.partText[part.ID]
			if len(part.Text) > prev {
				s.partText[part.ID] = len(part.Text)
			}
			s.mu.Unlock()
			if len(part.Text) <= prev {
				return
			}
			delta := part.Text[prev:]
			t := event.TypeAssistantChunk
			if part.Type == "reasoning" {
				t = event.TypeThoughtChunk
			}
			s.emit(event.Event{Type: t, SessionID: s.localID, Timestamp: now, Text: delta})
		case "tool":
			status := mapToolStatus(part.State.Status)
			detail := strings.TrimSpace(part.State.Title)
			if detail == "" {
				detail = shortJSON(part.State.Input, 300)
			}
			if part.State.Error != "" {
				detail = clip(part.State.Error, 300)
			}
			id := part.CallID
			if id == "" {
				id = part.ID
			}
			s.emit(event.Event{
				Type:      toolEventType(status, s.noteTool(id)),
				SessionID: s.localID,
				Timestamp: now,
				ToolID:    id,
				ToolName:  firstNonEmpty(part.State.Title, part.Tool, "tool"),
				ToolKind:  kindForTool(part.Tool),
				Status:    status,
				Text:      detail,
			})
		}

	case "permission.asked":
		var p struct {
			ID         string          `json:"id"`
			Permission string          `json:"permission"`
			Patterns   []string        `json:"patterns"`
			Metadata   json.RawMessage `json:"metadata"`
			Always     []string        `json:"always"`
		}
		if json.Unmarshal(props, &p) != nil || p.ID == "" {
			return
		}
		if s.p.cfg.AlwaysApprove {
			// Auto-allow without remoting to the phone.
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				dir := "?directory=" + urlQueryEscape(s.cwd)
				_ = s.p.api(ctx, "POST", "/session/"+s.ocID+"/permissions/"+p.ID+dir,
					map[string]string{"response": "once"}, nil)
			}()
			return
		}
		s.mu.Lock()
		s.pending[p.ID] = struct{}{}
		s.mu.Unlock()
		detail := strings.Join(p.Patterns, "\n")
		if detail == "" {
			detail = shortJSON(p.Metadata, 300)
		}
		opts := []event.PermissionOption{
			{OptionID: "once", Name: "Allow once", Kind: "allow_once"},
		}
		if len(p.Always) > 0 {
			opts = append(opts, event.PermissionOption{OptionID: "always", Name: "Allow always", Kind: "allow_always"})
		}
		opts = append(opts, event.PermissionOption{OptionID: "reject", Name: "Reject", Kind: "reject_once"})
		s.emit(event.Event{
			Type:         event.TypePermission,
			SessionID:    s.localID,
			Timestamp:    now,
			PermissionID: p.ID,
			ToolName:     p.Permission,
			Text:         detail,
			Options:      opts,
			Status:       "pending",
		})
		if s.p.cfg.PermissionTimeout > 0 {
			go s.expirePermission(p.ID)
		}

	case "permission.replied":
		var p struct {
			RequestID    string `json:"requestID"`
			PermissionID string `json:"permissionID"`
			ID           string `json:"id"`
		}
		_ = json.Unmarshal(props, &p)
		id := firstNonEmpty(p.RequestID, p.PermissionID, p.ID)
		if id == "" {
			return
		}
		s.mu.Lock()
		_, was := s.pending[id]
		delete(s.pending, id)
		s.mu.Unlock()
		if was {
			s.emit(event.Event{
				Type:         event.TypePermissionResolved,
				SessionID:    s.localID,
				Timestamp:    now,
				PermissionID: id,
				Status:       event.PermissionStatusResolved,
			})
		}

	case "session.idle":
		s.mu.Lock()
		active := s.turnActive
		s.turnActive = false
		// New turn, fresh delta state (part ids are per-message anyway; this
		// just bounds the maps).
		if len(s.partText) > 4096 {
			s.partText = make(map[string]int)
		}
		if len(s.msgRole) > 4096 {
			s.msgRole = make(map[string]string)
		}
		s.mu.Unlock()
		if !active {
			return
		}
		s.emit(event.Event{Type: event.TypeTurnComplete, SessionID: s.localID, Timestamp: now,
			Status: "end_turn", StopReason: "end_turn"})
		s.emit(event.Event{Type: event.TypeSessionStatus, SessionID: s.localID, Timestamp: now,
			Status: "idle"})

	case "session.error":
		var p struct {
			Error struct {
				Name string `json:"name"`
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"error"`
		}
		_ = json.Unmarshal(props, &p)
		s.mu.Lock()
		active := s.turnActive
		s.turnActive = false
		s.mu.Unlock()
		if p.Error.Name == "MessageAbortedError" {
			if active {
				s.emit(event.Event{Type: event.TypeTurnComplete, SessionID: s.localID, Timestamp: now,
					Status: "cancelled", StopReason: "cancelled"})
				s.emit(event.Event{Type: event.TypeSessionStatus, SessionID: s.localID, Timestamp: now,
					Status: "idle"})
			}
			return
		}
		msg := firstNonEmpty(p.Error.Data.Message, p.Error.Name, "agent error")
		s.emit(event.Event{Type: event.TypeError, SessionID: s.localID, Timestamp: now,
			Error: clip(msg, 400)})
		s.emit(event.Event{Type: event.TypeSessionStatus, SessionID: s.localID, Timestamp: now,
			Status: "error"})
	}
}

// expirePermission mirrors the ACP transport's fail-safe: reject server-side
// after the timeout so a missed notification can't hang the agent forever.
func (s *session) expirePermission(id string) {
	select {
	case <-s.done:
		return
	case <-time.After(s.p.cfg.PermissionTimeout):
	}
	s.mu.Lock()
	_, still := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !still {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := "?directory=" + urlQueryEscape(s.cwd)
	_ = s.p.api(ctx, "POST", "/session/"+s.ocID+"/permissions/"+id+dir,
		map[string]string{"response": "reject"}, nil)
	now := time.Now().UTC()
	s.emit(event.Event{
		Type:      event.TypeNotice,
		SessionID: s.localID,
		Timestamp: now,
		Text: fmt.Sprintf("Permission request timed out after %s — the agent "+
			"stopped waiting. Prompt again to retry.", s.p.cfg.PermissionTimeout),
	})
	s.emit(event.Event{
		Type:         event.TypePermissionResolved,
		SessionID:    s.localID,
		Timestamp:    now,
		PermissionID: id,
		Status:       event.PermissionStatusCancelled,
	})
}

// noteTool records that a tool id has been seen; returns true if it is new.
func (s *session) noteTool(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seenTools == nil {
		s.seenTools = make(map[string]struct{})
	}
	_, seen := s.seenTools[id]
	s.seenTools[id] = struct{}{}
	return !seen
}

func toolEventType(status string, isNew bool) event.Type {
	if isNew {
		return event.TypeToolCall
	}
	return event.TypeToolUpdate
}

// kindForTool maps an OpenCode tool name onto the ACP tool-kind vocabulary
// carried in event.Event.ToolKind, so clients classify actions uniformly
// across transports ("Ran N commands", "Edited N files", …).
func kindForTool(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return "execute"
	case "edit", "write", "patch", "multiedit":
		return "edit"
	case "read":
		return "read"
	case "grep", "glob", "list", "ls":
		return "search"
	case "webfetch", "websearch":
		return "fetch"
	case "todowrite", "todoread":
		return "think"
	case "":
		return ""
	default:
		return "other"
	}
}

func mapToolStatus(s string) string {
	switch s {
	case "completed":
		return "completed"
	case "error":
		return "failed"
	case "running":
		return "running"
	case "pending":
		return "pending"
	default:
		return s
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

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func shortJSON(raw json.RawMessage, max int) string {
	if len(raw) == 0 {
		return ""
	}
	return clip(string(raw), max)
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}
