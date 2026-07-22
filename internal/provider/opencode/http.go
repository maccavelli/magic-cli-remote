// Package opencode implements the OpenCode agent provider for mcremote.
//
// The HTTP dialect drives OpenCode through its native HTTP + SSE server
// (`opencode serve`) over the shared internal/provider/httpagent transport,
// instead of per-session `opencode acp` subprocesses.
//
// Why: every `opencode acp` process is a full Bun engine (~3s cold start,
// measured) and N processes contend on OpenCode's single global SQLite DB —
// both upstream WONTFIXes. The HTTP server is the surface OpenCode itself
// recommends for programmatic clients (its own TUI is one), sessions are
// cheap server-side objects, and one SSE stream carries every session's
// events. See docs/0011-opencode-provider-plan.md, "Performance addendum".
package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// zenDefaultModel is the free/zero-auth OpenCode model we seed before the
// multi-MB /provider catalog returns. Prefer the Flash free tier for low
// latency (measured ~1.2s "hi" vs ~2.5–8s for big-pickle on this host).
// AfterBoot may keep this or fall back if the catalog no longer lists it.
const zenDefaultModel = "deepseek-v4-flash-free"

// zenFallbackModels are tried in order when refining the default from the
// live catalog. First match wins.
var zenFallbackModels = []string{
	"deepseek-v4-flash-free",
	"north-mini-code-free",
	"big-pickle",
}

// NewHTTP creates the OpenCode HTTP-transport provider.
func NewHTTP(cfg Config) *httpagent.Provider {
	return NewHTTPWithLogger(cfg, nil)
}

// NewHTTPWithLogger is like NewHTTP but sets a logger.
func NewHTTPWithLogger(cfg Config, log *slog.Logger) *httpagent.Provider {
	l := slog.Default()
	if log != nil {
		l = log
	}
	d := &httpDialect{
		log:                  l,
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
	}
	return httpagent.NewWithLogger(d, cfg, log)
}

// httpDialect is the engine-level half: launch/health/SSE conventions plus
// the catalog-resolved default model shared by every session.
type httpDialect struct {
	log *slog.Logger

	mu sync.Mutex
	// defaultModelProvider/ID is the engine-catalog fallback applied to
	// prompts when neither session nor config names a model. Seeded with
	// zenDefaultModel at construction; AfterBoot may refine it.
	defaultModelProvider string
	defaultModelID       string
}

var _ httpagent.Dialect = (*httpDialect)(nil)

func (d *httpDialect) ID() provider.ID    { return provider.IDOpencode }
func (d *httpDialect) DefaultBin() string { return "opencode" }

func (d *httpDialect) ServeArgs(port int) []string {
	return []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port)}
}

func (d *httpDialect) HealthPath() string { return "/global/health" }
func (d *httpDialect) EventsPath() string { return "/global/event" }

// AfterBoot refines the fallback model from the engine catalog. It must be
// fast-path safe to skip: the dialect already seeds zenDefaultModel, and the
// transport runs this asynchronously so a 4MB /provider download never
// blocks session create.
//
// Preference order (latency-first for free tier):
//  1. First zenFallbackModels entry still present in the opencode catalog
//  2. Engine-reported default["opencode"] (often big-pickle — slower)
//  3. Keep the seed
func (d *httpDialect) AfterBoot(ctx context.Context, api httpagent.API) {
	// Bound catalog fetch — a hung provider list must not pin a goroutine forever.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out struct {
		Default map[string]string `json:"default"`
		All     []struct {
			ID     string `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"all"`
	}
	if err := api(ctx, "GET", "/provider", nil, &out); err != nil {
		d.log.Warn("opencode default-model resolve failed; using seeded fallback",
			slog.String("fallback", "opencode/"+zenDefaultModel),
			slog.String("err", err.Error()))
		return
	}
	available := map[string]struct{}{}
	for _, p := range out.All {
		if p.ID != "opencode" {
			continue
		}
		for id := range p.Models {
			available[id] = struct{}{}
		}
	}
	chosen := ""
	for _, id := range zenFallbackModels {
		if _, ok := available[id]; ok {
			chosen = id
			break
		}
	}
	if chosen == "" {
		if m := out.Default["opencode"]; m != "" {
			chosen = m
		} else {
			chosen = zenDefaultModel
		}
	}
	d.mu.Lock()
	d.defaultModelProvider, d.defaultModelID = "opencode", chosen
	d.mu.Unlock()
	d.log.Info("opencode default model resolved",
		slog.String("model", "opencode/"+chosen),
		slog.Int("catalog_models", len(available)))
}

// fallbackModel returns the catalog default for prompts with no model.
func (d *httpDialect) fallbackModel() (string, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.defaultModelProvider, d.defaultModelID
}

// DecodeFrame accepts both OpenCode SSE envelope forms: /global/event wraps
// each event as {directory, project, payload:{type, properties}}; the
// per-directory /event stream sends the bare {type, properties} form.
func (d *httpDialect) DecodeFrame(data []byte) (string, json.RawMessage, string, bool) {
	var frame struct {
		Payload struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
		} `json:"payload"`
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return "", nil, "", false
	}
	typ, props := frame.Type, frame.Properties
	if frame.Payload.Type != "" {
		typ, props = frame.Payload.Type, frame.Payload.Properties
	}
	return typ, props, sessionIDOf(props), true
}

// sessionIDOf pulls properties.sessionID (or nested part/info sessionID).
func sessionIDOf(props json.RawMessage) string {
	var probe struct {
		SessionID string `json:"sessionID"`
		Part      struct {
			SessionID string `json:"sessionID"`
		} `json:"part"`
		Info struct {
			SessionID string `json:"sessionID"`
		} `json:"info"`
	}
	if json.Unmarshal(props, &probe) != nil {
		return ""
	}
	if probe.SessionID != "" {
		return probe.SessionID
	}
	if probe.Part.SessionID != "" {
		return probe.Part.SessionID
	}
	return probe.Info.SessionID
}

func (d *httpDialect) NewSession(h httpagent.Host) httpagent.DialectSession {
	return &httpSession{
		d:         d,
		h:         h,
		partText:  make(map[string]int),
		partType:  make(map[string]string),
		partDelta: make(map[string]struct{}),
		msgRole:   make(map[string]string),
	}
}

// httpSession is the per-session half: OpenCode REST shapes and SSE event
// translation.
type httpSession struct {
	d *httpDialect
	h httpagent.Host

	mu sync.Mutex
	// partText tracks cumulative text per part id so SSE part updates (which
	// carry the FULL text each time) become deltas when no streaming deltas
	// were seen for that part.
	partText map[string]int
	// partType remembers part type (text/reasoning/tool/…) from part.updated
	// so subsequent part.delta frames can be classified without a full snapshot.
	partType map[string]string
	// partDelta marks parts that already streamed via message.part.delta so
	// a later full-text part.updated does not double-emit.
	partDelta map[string]struct{}
	// msgRole records message role by id; user-authored parts echo back over
	// SSE and must not become assistant chunks.
	msgRole map[string]string
	// seenTools distinguishes the first sighting of a tool call (tool_call)
	// from updates (tool_call_update).
	seenTools map[string]struct{}
}

var _ httpagent.DialectSession = (*httpSession)(nil)

// dir is the ?directory= query OpenCode uses to scope requests to a project.
func (o *httpSession) dir() string {
	return "?directory=" + url.QueryEscape(o.h.CWD())
}

func (o *httpSession) Create(ctx context.Context, opts provider.StartOptions) (string, error) {
	body := map[string]any{}
	if opts.Name != "" {
		body["title"] = opts.Name
	}
	// Always pin a model at create time so the server never falls through to
	// its broken zen/… default (UnknownError: Model not found).
	if mp, mid := o.resolveModel(); mid != "" {
		// Create expects {providerID, id}. Prompt uses {providerID, modelID}
		// — different keys on the same engine (OpenCode 1.18 OpenAPI).
		body["model"] = map[string]string{"providerID": mp, "id": mid}
	}
	start := time.Now()
	var created struct {
		ID string `json:"id"`
	}
	if err := o.h.API()(ctx, "POST", "/session"+o.dir(), body, &created); err != nil {
		return "", err
	}
	o.h.Log().Info("opencode session create",
		slog.String("agent_session_id", created.ID),
		slog.Duration("ms", time.Since(start)),
		slog.String("model", o.h.Model()),
	)
	return created.ID, nil
}

// resolveModel returns the effective provider/model for this session: explicit
// session/config model first, then the dialect fallback (seeded zen default).
func (o *httpSession) resolveModel() (providerID, modelID string) {
	mp, mid := splitModel(o.h.Model())
	if mid == "" {
		mp, mid = o.d.fallbackModel()
	}
	return mp, mid
}

func (o *httpSession) Resume(ctx context.Context, agentSessionID string) (string, error) {
	var info struct {
		ID string `json:"id"`
	}
	if err := o.h.API()(ctx, "GET", "/session/"+agentSessionID+o.dir(), nil, &info); err != nil {
		return "", err
	}
	return info.ID, nil
}

// Replay rebuilds the daemon-side history ring for a resumed session from the
// server's message log (best-effort).
func (o *httpSession) Replay(ctx context.Context) {
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
	if err := o.h.API()(ctx, "GET", "/session/"+o.h.AgentSessionID()+"/message"+o.dir(), nil, &msgs); err != nil {
		o.h.Log().Warn("resume message replay failed", slog.String("err", err.Error()))
		return
	}
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
			o.h.EmitReplay(ev)
		}
	}
}

func (o *httpSession) Prompt(ctx context.Context, parts []provider.Content) error {
	apiParts := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			apiParts = append(apiParts, map[string]any{"type": "text", "text": p.Text})
		}
	}
	body := map[string]any{"parts": apiParts}
	mp, mid := o.resolveModel()
	if mid != "" {
		// Prompt uses modelID (not id). OpenCode 1.18's prompt_async schema
		// requires {providerID, modelID}; create uses {providerID, id}.
		// Unifying them to "id" breaks every prompt with HTTP 400.
		body["model"] = map[string]string{"providerID": mp, "modelID": mid}
	}
	start := time.Now()
	// prompt_async returns immediately; the turn streams over SSE and ends
	// with session.idle.
	err := o.h.API()(ctx, "POST", "/session/"+o.h.AgentSessionID()+"/prompt_async"+o.dir(), body, nil)
	o.h.Log().Info("opencode prompt_async",
		slog.String("agent_session_id", o.h.AgentSessionID()),
		slog.Duration("enqueue_ms", time.Since(start)),
		slog.String("model", mp+"/"+mid),
		slog.Bool("ok", err == nil),
	)
	return err
}

func (o *httpSession) Abort(ctx context.Context) error {
	return o.h.API()(ctx, "POST", "/session/"+o.h.AgentSessionID()+"/abort"+o.dir(), nil, nil)
}

// Delete purges the server-side session (session.delete / hard purge).
func (o *httpSession) Delete(ctx context.Context) error {
	id := o.h.AgentSessionID()
	if id == "" {
		return nil
	}
	return o.h.API()(ctx, "DELETE", "/session/"+id+o.dir(), nil, nil)
}

func (o *httpSession) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	response := "reject"
	if !cancelled {
		switch optionID {
		case "once", "allow", "allow_once":
			response = "once"
		case "always", "allow_always":
			response = "always"
		}
	}
	return o.h.API()(ctx, "POST", "/session/"+o.h.AgentSessionID()+"/permissions/"+permissionID+o.dir(),
		map[string]string{"response": response}, nil)
}

// HandleEvent translates one SSE event into daemon events.
func (o *httpSession) HandleEvent(typ string, props json.RawMessage) {
	switch typ {
	case "message.updated":
		var p struct {
			Info struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"info"`
		}
		if json.Unmarshal(props, &p) == nil && p.Info.ID != "" {
			o.mu.Lock()
			o.msgRole[p.Info.ID] = p.Info.Role
			o.mu.Unlock()
		}

	// OpenCode 1.18 streams assistant text primarily via part.delta (token
	// fragments). part.updated carries full snapshots / tool state. Handling
	// only updated made the phone wait until the turn finished (~seconds)
	// before any assistant text appeared.
	case "message.part.delta":
		var p struct {
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		}
		if json.Unmarshal(props, &p) != nil || p.Delta == "" {
			return
		}
		if p.Field != "" && p.Field != "text" {
			return
		}
		o.mu.Lock()
		role := o.msgRole[p.MessageID]
		ptype := o.partType[p.PartID]
		if p.PartID != "" {
			o.partDelta[p.PartID] = struct{}{}
			o.partText[p.PartID] += len(p.Delta)
		}
		o.mu.Unlock()
		if role == "user" {
			return
		}
		// Unknown part type defaults to assistant text; reasoning parts are
		// classified once part.updated announces type=reasoning.
		t := event.TypeAssistantChunk
		if ptype == "reasoning" {
			t = event.TypeThoughtChunk
		}
		o.h.Emit(event.Event{Type: t, Text: p.Delta})

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
		o.mu.Lock()
		role := o.msgRole[part.MessageID]
		if part.ID != "" && part.Type != "" {
			o.partType[part.ID] = part.Type
		}
		streamed := false
		if part.ID != "" {
			_, streamed = o.partDelta[part.ID]
		}
		o.mu.Unlock()
		if role == "user" {
			return
		}
		switch part.Type {
		case "text", "reasoning":
			// If we already streamed this part via deltas, only catch up any
			// trailing snapshot text (and advance the cursor).
			o.mu.Lock()
			prev := o.partText[part.ID]
			if streamed {
				if len(part.Text) > prev {
					o.partText[part.ID] = len(part.Text)
				}
				o.mu.Unlock()
				if len(part.Text) <= prev {
					return
				}
				delta := part.Text[prev:]
				t := event.TypeAssistantChunk
				if part.Type == "reasoning" {
					t = event.TypeThoughtChunk
				}
				o.h.Emit(event.Event{Type: t, Text: delta})
				return
			}
			// Snapshot path (no deltas for this part): full text → deltas.
			if len(part.Text) > prev {
				o.partText[part.ID] = len(part.Text)
			}
			o.mu.Unlock()
			if len(part.Text) <= prev {
				return
			}
			delta := part.Text[prev:]
			t := event.TypeAssistantChunk
			if part.Type == "reasoning" {
				t = event.TypeThoughtChunk
			}
			o.h.Emit(event.Event{Type: t, Text: delta})
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
			o.h.Emit(event.Event{
				Type:     toolEventType(status, o.noteTool(id)),
				ToolID:   id,
				ToolName: firstNonEmpty(part.State.Title, part.Tool, "tool"),
				ToolKind: kindForTool(part.Tool),
				Status:   status,
				Text:     detail,
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
		if o.h.Config().AlwaysApprove {
			// Auto-allow without remoting to the phone.
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = o.RespondPermission(ctx, p.ID, "once", false)
			}()
			return
		}
		o.h.TrackPermission(p.ID)
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
		o.h.Emit(event.Event{
			Type:         event.TypePermission,
			PermissionID: p.ID,
			ToolName:     p.Permission,
			Text:         detail,
			Options:      opts,
			Status:       "pending",
		})

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
		if o.h.TakePending(id) {
			o.h.Emit(event.Event{
				Type:         event.TypePermissionResolved,
				PermissionID: id,
				Status:       event.PermissionStatusResolved,
			})
		}

	case "session.idle":
		active := o.h.EndTurn()
		// New turn, fresh delta state (part ids are per-message anyway; this
		// just bounds the maps).
		o.mu.Lock()
		if len(o.partText) > 4096 {
			o.partText = make(map[string]int)
		}
		if len(o.partType) > 4096 {
			o.partType = make(map[string]string)
		}
		if len(o.partDelta) > 4096 {
			o.partDelta = make(map[string]struct{})
		}
		if len(o.msgRole) > 4096 {
			o.msgRole = make(map[string]string)
		}
		o.mu.Unlock()
		if !active {
			return
		}
		o.h.Emit(event.Event{Type: event.TypeTurnComplete, Status: "end_turn", StopReason: "end_turn"})
		o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})

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
		active := o.h.EndTurn()
		if p.Error.Name == "MessageAbortedError" {
			if active {
				o.h.Emit(event.Event{Type: event.TypeTurnComplete, Status: "cancelled", StopReason: "cancelled"})
				o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
			}
			return
		}
		msg := firstNonEmpty(p.Error.Data.Message, p.Error.Name, "agent error")
		// Classify before clipping — reset-time hints can sit past 400 runes.
		cls := agenterr.Classify(msg, time.Now())
		o.h.Emit(event.Event{
			Type:      event.TypeError,
			Error:     clip(msg, 400),
			ErrorKind: string(cls.Kind),
			RetryAt:   cls.ResetAt,
		})
		o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: "error"})
	}
}

// noteTool records that a tool id has been seen; returns true if it is new.
func (o *httpSession) noteTool(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seenTools == nil {
		o.seenTools = make(map[string]struct{})
	}
	_, seen := o.seenTools[id]
	o.seenTools[id] = struct{}{}
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

// splitModel turns "provider/model" into its OpenCode parts. A bare name is
// treated as an opencode Zen model.
func splitModel(m string) (providerID, modelID string) {
	m = strings.TrimSpace(m)
	if m == "" {
		return "", ""
	}
	if i := strings.IndexByte(m, '/'); i > 0 {
		return m[:i], m[i+1:]
	}
	return "opencode", m
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
