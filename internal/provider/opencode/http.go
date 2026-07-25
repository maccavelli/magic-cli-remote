// Package opencode implements the OpenCode agent provider for mcremote.
//
// OpenCode is driven through its native HTTP + SSE server (`opencode serve`)
// over the shared internal/provider/httpagent transport: ONE long-lived engine
// process per daemon, with every session a cheap server-side object
// multiplexed over a single SSE stream.
//
// Why not per-session `opencode acp` subprocesses (removed in MADR 0019):
// every such process is a full Bun engine (~3s cold start, measured) and N of
// them contend on OpenCode's single global SQLite DB — both upstream
// WONTFIXes. The HTTP server is also the surface OpenCode itself recommends
// for programmatic clients (its own TUI is one), and it supports /undo and
// /redo, which its ACP surface does not. See
// docs/0011-opencode-provider-plan.md ("Performance addendum") and
// docs/0019-opencode-process-management-plan.md.
package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
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

// staticModelOptions is the offline catalog used when the engine is not up (or
// its multi-MB /provider catalog has not returned yet). Live ListModelsLive
// merges the engine's real list over this.
func staticModelOptions() []picker.Option {
	opts := make([]picker.Option, 0, len(zenFallbackModels))
	for _, id := range zenFallbackModels {
		opts = append(opts, picker.Option{
			ID:    "opencode/" + id,
			Label: id,
			Group: "opencode",
		})
	}
	// Common third-party ids users pin in config (still AllowCustom for the rest).
	extras := []picker.Option{
		{ID: "anthropic/claude-sonnet-4-5", Label: "Claude Sonnet 4.5", Group: "anthropic"},
		{ID: "anthropic/claude-haiku-4-5", Label: "Claude Haiku 4.5", Group: "anthropic"},
		{ID: "openai/gpt-5", Label: "GPT-5", Group: "openai"},
	}
	return append(opts, extras...)
}

// Config configures the OpenCode provider (shared agent config shape).
type Config = httpagent.Config

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
	// engineVersion is the last /global/health version string (MADR 0020 KD10).
	engineVersion string
}

var (
	_ httpagent.Dialect       = (*httpDialect)(nil)
	_ httpagent.ModelLister   = (*httpDialect)(nil)
	_ httpagent.AgentLister   = (*httpDialect)(nil)
	_ httpagent.CommandLister = (*httpDialect)(nil)
	_ httpagent.HealthyHook   = (*httpDialect)(nil)
	_ httpagent.VersionGate   = (*httpDialect)(nil)
)

func (d *httpDialect) ID() provider.ID    { return provider.IDOpencode }
func (d *httpDialect) DefaultBin() string { return "opencode" }

// StaticModels implements [httpagent.ModelLister].
func (d *httpDialect) StaticModels(cfg httpagent.Config) picker.Catalog {
	def := cfg.Model
	if def == "" {
		def = "opencode/" + zenDefaultModel
	}
	return picker.SingleCatalog(picker.SourceStatic, staticModelOptions(), def, true)
}

// StaticAgents implements [httpagent.AgentLister]. Offline fallback of common
// primary agents; live GET /agent replaces this when the engine is up.
func (d *httpDialect) StaticAgents(cfg httpagent.Config) picker.Catalog {
	_ = cfg
	opts := []picker.Option{
		{ID: "build", Label: "build", Description: "Default agent", Group: "primary"},
		{ID: "plan", Label: "plan", Description: "Plan mode (no edits)", Group: "primary"},
	}
	return picker.SingleCatalog(picker.SourceStatic, opts, "build", true)
}

// ListAgentsLive implements [httpagent.AgentLister] via GET /agent.
func (d *httpDialect) ListAgentsLive(ctx context.Context, api httpagent.API) (picker.Catalog, error) {
	agents, err := fetchAgents(ctx, api, "")
	if err != nil {
		return picker.Catalog{}, err
	}
	opts := make([]picker.Option, 0, len(agents))
	for _, a := range agents {
		// Hidden agents (compaction, summary, title) are engine internals
		// reported as primary; they are not selectable work.
		if !a.visible() {
			continue
		}
		name := a.Name
		mode := a.Mode
		if mode == "" {
			mode = "primary"
		}
		desc := strings.TrimSpace(a.Description)
		if desc == "" {
			desc = mode
		}
		opts = append(opts, picker.Option{
			ID:          name,
			Label:       name,
			Description: desc,
			Group:       mode,
			Meta:        map[string]string{"mode": mode},
		})
	}
	slices.SortFunc(opts, func(a, b picker.Option) int {
		// primary first, then subagent, then by id
		rank := func(g string) int {
			switch g {
			case "primary":
				return 0
			case "all":
				return 1
			case "subagent":
				return 2
			default:
				return 3
			}
		}
		if ra, rb := rank(a.Group), rank(b.Group); ra != rb {
			return ra - rb
		}
		return strings.Compare(a.ID, b.ID)
	})
	def := "build"
	hasBuild := false
	for _, o := range opts {
		if o.ID == "build" {
			hasBuild = true
			break
		}
	}
	if !hasBuild && len(opts) > 0 {
		// Prefer first primary-mode option.
		def = opts[0].ID
		for _, o := range opts {
			if o.Group == "primary" {
				def = o.ID
				break
			}
		}
	}
	return picker.SingleCatalog(picker.SourceLive, opts, def, true), nil
}

// ListModelsLive implements [httpagent.ModelLister].
func (d *httpDialect) ListModelsLive(ctx context.Context, api httpagent.API) (picker.Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out struct {
		Default map[string]string `json:"default"`
		All     []struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"all"`
	}
	if err := api(ctx, "GET", "/provider", nil, &out); err != nil {
		return picker.Catalog{}, err
	}
	opts := make([]picker.Option, 0, 64)
	for _, p := range out.All {
		if p.ID == "" {
			continue
		}
		for modelID := range p.Models {
			if modelID == "" {
				continue
			}
			full := p.ID + "/" + modelID
			opts = append(opts, picker.Option{
				ID:    full,
				Label: modelID,
				Group: p.ID,
			})
		}
	}
	// Map iteration is random; sort for a usable picker.
	slices.SortFunc(opts, func(a, b picker.Option) int {
		if a.Group != b.Group {
			return strings.Compare(a.Group, b.Group)
		}
		return strings.Compare(a.ID, b.ID)
	})
	def := ""
	if m := out.Default["opencode"]; m != "" {
		def = "opencode/" + m
	}
	d.mu.Lock()
	if def == "" && d.defaultModelID != "" {
		def = d.defaultModelProvider + "/" + d.defaultModelID
	}
	d.mu.Unlock()
	return picker.SingleCatalog(picker.SourceLive, opts, def, true), nil
}

func (d *httpDialect) ServeArgs(port int) []string {
	return []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port)}
}

func (d *httpDialect) HealthPath() string { return "/global/health" }
func (d *httpDialect) EventsPath() string { return "/global/event" }

// OnHealthy implements [httpagent.HealthyHook]: records the engine version from
// GET /global/health. Does not refuse here — [CheckMinVersion] gates Start when
// session_tree is on so kill-switch configs can still run older engines.
func (d *httpDialect) OnHealthy(body []byte) error {
	var h struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &h); err != nil {
		// Non-JSON health is unexpected but not fatal: keep boot path open.
		d.log.Debug("opencode health body not JSON", slog.String("err", err.Error()))
		return nil
	}
	v := strings.TrimSpace(h.Version)
	d.mu.Lock()
	d.engineVersion = v
	d.mu.Unlock()
	if v != "" && !VersionMeetsMin(v) {
		d.log.Warn("opencode engine below minimum version for session-tree features",
			slog.String("version", v),
			slog.String("min", MinVersion),
			slog.String("hint", "upgrade opencode, or set providers.opencode.session_tree=false"),
		)
	} else if v != "" {
		d.log.Info("opencode engine version",
			slog.String("version", v),
			slog.String("min", MinVersion),
		)
	}
	return nil
}

// CheckMinVersion implements [httpagent.VersionGate] (MADR 0020 KD10).
func (d *httpDialect) CheckMinVersion(cfg httpagent.Config) error {
	if !cfg.TreeEnabled() {
		return nil
	}
	d.mu.Lock()
	v := d.engineVersion
	d.mu.Unlock()
	if v == "" {
		// Health probe has not reported yet (or body had no version).
		return nil
	}
	if !VersionMeetsMin(v) {
		return VersionPinError(v)
	}
	return nil
}

// EngineVersion returns the last health-reported version, or "".
func (d *httpDialect) EngineVersion() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.engineVersion
}

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
			ID     string                     `json:"id"`
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

// sessionIDOf pulls properties.sessionID (or nested part/info sessionID / Session.id).
// Lifecycle frames (session.created) use info.id without a top-level sessionID
// (MADR 0020); without info.id they demux as empty sid and are dropped.
func sessionIDOf(props json.RawMessage) string {
	var probe struct {
		SessionID string `json:"sessionID"`
		Part      struct {
			SessionID string `json:"sessionID"`
		} `json:"part"`
		Info struct {
			SessionID string `json:"sessionID"`
			ID        string `json:"id"`
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
	if probe.Info.SessionID != "" {
		return probe.Info.SessionID
	}
	return probe.Info.ID
}

// ParentIDFromProps implements [httpagent.ChildFrame] for session.created/updated
// bootstrap demux when the child sid is not yet aliased (MADR 0020).
func (d *httpDialect) ParentIDFromProps(props json.RawMessage) string {
	return parentIDOf(props)
}

func parentIDOf(props json.RawMessage) string {
	var probe struct {
		Info struct {
			ParentID string `json:"parentID"`
		} `json:"info"`
		ParentID string `json:"parentID"`
	}
	if json.Unmarshal(props, &probe) != nil {
		return ""
	}
	if probe.Info.ParentID != "" {
		return probe.Info.ParentID
	}
	return probe.ParentID
}

func (d *httpDialect) NewSession(h httpagent.Host) httpagent.DialectSession {
	return &httpSession{
		d:         d,
		h:         h,
		partText:  make(map[string]string),
		partType:  make(map[string]string),
		msgRole:   make(map[string]string),
		subagents: make(map[string]string),
	}
}

// httpSession is the per-session half: OpenCode REST shapes and SSE event
// translation.
type httpSession struct {
	d *httpDialect
	h httpagent.Host

	mu sync.Mutex
	// partText tracks the actual accumulated text per part id (NOT a byte count):
	// SSE part.updated frames carry the FULL text each time, so we emit only the
	// suffix beyond what we already streamed. Storing the real text (rather than a
	// cursor) lets a snapshot re-align after a dropped/reordered delta instead of
	// slicing at a stale offset — which dropped, duplicated, or split runes.
	partText map[string]string
	// partType remembers part type (text/reasoning/tool/…) from part.updated
	// so subsequent part.delta frames can be classified without a full snapshot.
	partType map[string]string
	// msgRole records message role by id; user-authored parts echo back over
	// SSE and must not become assistant chunks.
	msgRole map[string]string
	// seenTools distinguishes the first sighting of a tool call (tool_call)
	// from updates (tool_call_update).
	seenTools map[string]struct{}
	// subagents tracks synthetic subagent tool cards for this turn: agent
	// session id → cardRunning | cardCompleted. Completed ids are retained
	// (not deleted) so a post-completion session.updated cannot reopen a card.
	subagents map[string]string
}

var _ httpagent.DialectSession = (*httpSession)(nil)
var _ httpagent.TreeIdleConfirmer = (*httpSession)(nil)

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
	// Advertise slash commands so manager/mobile treat /init etc. as agent
	// commands (MADR 0020 Sprint 5). Best-effort; static fallback inside.
	o.advertiseCommands(ctx)
	// Advertise the primary agents as session modes so the switcher and /plan
	// work (MADR 0022). Best-effort; static fallback inside.
	o.advertiseModes(ctx)
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
	// Command and mode catalogs are session-agnostic; re-advertise both so
	// autocomplete and the mode strip survive a resume.
	o.advertiseCommands(ctx)
	o.advertiseModes(ctx)
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
	// OpenCode custom slash commands → POST …/command (Sprint 5). Manager only
	// forwards names advertised via available_commands.
	if name, args, ok := soleSlashCommand(parts); ok {
		return o.submitCommand(ctx, name, args)
	}
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
	// Optional agent name (MADR 0020 Sprint 3): "build", "plan", …
	if agent := strings.TrimSpace(o.h.Agent()); agent != "" {
		body["agent"] = agent
	}
	start := time.Now()
	// prompt_async returns immediately; the turn streams over SSE and ends
	// with session.idle.
	err := o.h.API()(ctx, "POST", "/session/"+o.h.AgentSessionID()+"/prompt_async"+o.dir(), body, nil)
	o.h.Log().Info("opencode prompt_async",
		slog.String("agent_session_id", o.h.AgentSessionID()),
		slog.Duration("enqueue_ms", time.Since(start)),
		slog.String("model", mp+"/"+mid),
		slog.String("agent", o.h.Agent()),
		slog.Bool("ok", err == nil),
	)
	return err
}

// Abort cancels the parent session and best-effort aborts known children (A7).
func (o *httpSession) Abort(ctx context.Context) error {
	parent := o.h.AgentSessionID()
	if parent == "" {
		return nil
	}
	err := o.h.API()(ctx, "POST", "/session/"+parent+"/abort"+o.dir(), nil, nil)
	// Best-effort child abort cascade (MADR 0020 §6.9).
	for _, id := range o.discoverTreeChildren(ctx, parent) {
		_ = o.h.API()(ctx, "POST", "/session/"+id+"/abort"+o.dir(), nil, nil)
	}
	return err
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
	return o.respondPermissionEngine(ctx, permissionID, response)
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
		// M9: stream text only once we KNOW the message is the assistant's. A
		// missed message.updated (role) frame leaves role "" — defaulting that to
		// assistant echoed the user's own prompt back as an assistant bubble. The
		// authoritative part.updated snapshot re-delivers the text once role is
		// known, so skipping here loses nothing.
		if role != "assistant" {
			o.mu.Unlock()
			return
		}
		if p.PartID != "" {
			// M8: accumulate the real text (not a byte count) so the part.updated
			// catch-up compares against exactly what we streamed.
			o.partText[p.PartID] += p.Delta
		}
		o.mu.Unlock()
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
		o.mu.Unlock()
		if role == "user" {
			return
		}
		switch part.Type {
		case "text", "reasoning":
			o.emitTextCatchUp(part.ID, part.Type, part.Text)
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

	case "permission.asked", "permission.updated":
		if p, ok := normalizePermissionAsk(props); ok {
			o.emitPermissionAsk(p)
		}

	case "permission.v2.asked":
		if p, ok := normalizePermissionV2Ask(props); ok {
			o.emitPermissionAsk(p)
		}

	case "permission.replied", "permission.v2.replied":
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

	case "question.asked", "question.v2.asked":
		o.handleQuestionAsked(props)

	case "question.replied", "question.v2.replied":
		o.handleQuestionResolved(props, false)

	case "question.rejected", "question.v2.rejected":
		o.handleQuestionResolved(props, true)

	case "session.diff":
		o.handleSessionDiff(props)

	case "command.executed":
		o.handleCommandExecuted(props)

	case "todo.updated":
		o.handleTodoUpdated(props)

	case "session.created":
		o.handleSessionLifecycle(props, true)

	case "session.updated":
		o.handleSessionLifecycle(props, false)

	case "session.deleted":
		o.handleSessionDeleted(props)

	case "session.status":
		o.handleSessionStatus(props)

	case "session.idle":
		// Tree-aware EndTurn (MADR 0020): mark this agent node idle and only
		// complete the phone turn when every known tree node is idle.
		sid := firstNonEmpty(sessionIDOf(props), o.h.EventAgentSessionID(), o.h.AgentSessionID())
		o.noteNodeIdle(sid)
		o.tryTreeEndTurn()

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
		sid := firstNonEmpty(sessionIDOf(props), o.h.EventAgentSessionID(), o.h.AgentSessionID())
		// Parent error is terminal for the turn; child error isolates (Q3).
		if sid == "" || sid == o.h.AgentSessionID() {
			o.h.NoteNodeStatus(o.h.AgentSessionID(), httpagent.NodeIdle)
			active := o.h.EndTurn()
			if active {
				o.completeAllSubagentCards()
				// An errored turn is still a finished turn: without this the
				// per-turn maps (notably seenTools) leaked into the next one,
				// so its first tool re-used call id emitted tool_call_update
				// with no preceding tool_call.
				o.turnCleanup()
			}
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
			return
		}
		o.h.NoteNodeStatus(sid, httpagent.NodeIdle)
		o.completeSubagentCard(sid)
		msg := firstNonEmpty(p.Error.Data.Message, p.Error.Name, "subagent error")
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: clip("Subagent error: "+msg, 400),
		})
		// Child finished (with error); parent may still be busy.
		o.tryTreeEndTurn()
	}
}

// emitTextCatchUp reconciles the authoritative full text of a part against
// what was already streamed and emits only the missing tail. part.updated
// carries the FULL text of the part; so does the message log used by Resync.
// Comparing the real accumulated text (M8) means a dropped/reordered delta no
// longer corrupts output: on a clean prefix we emit the tail; when the
// snapshot lags the deltas we already streamed we emit nothing; on divergence
// we re-align to the authoritative snapshot at a rune-safe boundary instead of
// slicing at a stale byte cursor (which dropped text, duplicated it, or split
// a multi-byte rune into replacement characters).
//
// The o.mu hold spans the Emit so concurrent catch-ups (SSE pump vs resync)
// cannot interleave their tails out of order; chunk emits never block
// (drop-on-full), so the hold is bounded.
func (o *httpSession) emitTextCatchUp(partID, partType, full string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	prev := o.partText[partID]
	var delta string
	switch {
	case full == prev:
		// Snapshot matches what we streamed: nothing new.
	case strings.HasPrefix(full, prev):
		delta = full[len(prev):]
		o.partText[partID] = full
	case strings.HasPrefix(prev, full):
		// Stale/short snapshot lagging the deltas we already streamed:
		// keep the longer accumulated text, emit nothing.
	default:
		n := commonPrefixLen(prev, full)
		delta = full[n:]
		o.partText[partID] = full
	}
	if delta == "" {
		return
	}
	t := event.TypeAssistantChunk
	if partType == "reasoning" {
		t = event.TypeThoughtChunk
	}
	o.h.Emit(event.Event{Type: t, Text: delta})
}

// turnCleanup resets per-turn state once a turn ends (session.idle or a
// resync-recovered turn-end). Part ids are per-message anyway; the size checks
// just bound the maps.
func (o *httpSession) turnCleanup() {
	o.mu.Lock()
	if len(o.partText) > 4096 {
		o.partText = make(map[string]string)
	}
	if len(o.partType) > 4096 {
		o.partType = make(map[string]string)
	}
	if len(o.msgRole) > 4096 {
		o.msgRole = make(map[string]string)
	}
	// seenTools only distinguishes first-sighting from update WITHIN a turn.
	// The turn is over, so drop the accumulated tool ids outright — left
	// alone they grow unbounded for the life of the session. noteTool
	// re-inits the map lazily on the next turn's first tool.
	o.seenTools = nil
	// subagents cards are completed by tryTreeEndTurn / completeAllSubagentCards
	// before turnCleanup; clear any residual.
	o.subagents = nil
	o.mu.Unlock()
}

// Resync is implemented in resync.go (MADR 0020 PR5 tree-aware extension of H4).

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

// commonPrefixLen returns the byte length of the longest common prefix of a and
// b that ends on a UTF-8 rune boundary, so slicing b at the result never splits
// a multi-byte rune. Used to re-align part text after a dropped/reordered delta.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	// Back off to the start of the rune straddling the divergence point.
	for i > 0 && i < len(b) && !utf8.RuneStart(b[i]) {
		i--
	}
	return i
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
