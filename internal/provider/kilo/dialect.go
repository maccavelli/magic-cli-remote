package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// httpDialect is the engine-level half: launch/health/SSE conventions.
// Session-level REST and event translation land in MADR 0075 plan P2.
type httpDialect struct {
	log *slog.Logger
	// pure runs the serve process without external plugins (--pure,
	// live-confirmed on kilo 7.4.20).
	pure bool

	mu sync.Mutex
	// engineVersion is the last /global/health version string, retained for
	// doctor output and the future session_tree version gate (plan P1).
	engineVersion string
	// defaultModelProvider/ID is the fallback applied to prompts when neither
	// session nor config names a model. Seeded with the Gateway free
	// auto-router (plan PD4); P3's AfterBoot refines it from the engine's own
	// auth-state-dependent default.
	defaultModelProvider string
	defaultModelID       string
	// syncSeq is the last sequence seen per aggregate (session) on kilo's
	// `sync` stream, used to detect dropped events (MADR 0137 F7).
	syncSeq map[string]int64
	// contextLimits is "providerID/modelID" → context-window size for the
	// usage indicator. Empty until P3's AfterBoot harvests the catalog;
	// a missing entry renders as a bare token count, never an error.
	contextLimits map[string]int
	// onToolPartUpdated is an optional callback for live probing tool frames
	// (MADR 0034 Phase 0 parity; MADR 0076 L2).
	onToolPartUpdated func(RawToolPartFrame)
}

var (
	_ httpagent.Dialect             = (*httpDialect)(nil)
	_ httpagent.ModelLister         = (*httpDialect)(nil)
	_ httpagent.ModelProviderLister = (*httpDialect)(nil)
	_ httpagent.AgentLister         = (*httpDialect)(nil)
	_ httpagent.CommandLister       = (*httpDialect)(nil)
	_ httpagent.CommandTabler       = (*httpDialect)(nil)
	_ httpagent.HealthyHook         = (*httpDialect)(nil)
	_ httpagent.ChildFrame          = (*httpDialect)(nil)
	_ httpagent.StartAgentValidator = (*httpDialect)(nil)
)

// DefaultModel implements [httpagent.DialectDefaultModel], exposing the same
// resolved default fallbackModel serves to prompts so the daemon can name the
// model a default-model session is actually running on (MADR 0137).
func (d *httpDialect) DefaultModel() (string, string) {
	return d.fallbackModel()
}

// fallbackModel returns the catalog default for prompts with no model.
func (d *httpDialect) fallbackModel() (string, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.defaultModelProvider, d.defaultModelID
}

// ParentIDFromProps implements [httpagent.ChildFrame] for session.created/updated
// bootstrap demux when the child sid is not yet aliased (same shape as OpenCode).
func (d *httpDialect) ParentIDFromProps(props json.RawMessage) string {
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

func (d *httpDialect) ID() provider.ID    { return provider.IDKilo }
func (d *httpDialect) DefaultBin() string { return "kilo" }

// ServeArgs starts the shared engine loopback-only (MADR 0075 §2.3). Never
// add --mdns here: it rebinds the hostname to 0.0.0.0 and exposes the
// un-gated engine off-host (D5 as amended / plan PD1).
func (d *httpDialect) ServeArgs(port int) []string {
	args := []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port)}
	if d.pure {
		args = append(args, "--pure")
	}
	return args
}

func (d *httpDialect) HealthPath() string { return "/global/health" }
func (d *httpDialect) EventsPath() string { return "/global/event" }

// AfterBoot resolves the engine's own default model and harvests context
// limits from GET /config/providers — the cheap connected-set endpoint, not
// the multi-MB /provider (plan PD5). The default is auth-state-dependent
// (kilo-auto/free unauthenticated vs kilo-auto/balanced with Gateway OAuth,
// MADR 0075 §2.6), which is exactly why it is read from the engine instead of
// hard-coded (plan PD4). Best-effort: on failure the seeded kilo-auto/free
// fallback stands.
func (d *httpDialect) AfterBoot(ctx context.Context, api httpagent.API) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := d.connectedProviders(ctx, api)
	if err != nil {
		d.log.Warn("kilo default-model resolve failed; using seeded fallback",
			slog.String("fallback", "kilo/"+defaultModelID),
			slog.String("err", err.Error()))
		return
	}
	limits := map[string]int{}
	for _, p := range conn.Providers {
		for modelID, m := range p.Models {
			id := m.ID
			if id == "" {
				id = modelID
			}
			if m.Limit.Context > 0 {
				limits[p.ID+"/"+id] = m.Limit.Context
			}
		}
	}
	provider, model := "kilo", conn.Default["kilo"]
	if model == "" {
		// No Gateway default (e.g. logged out): first connected provider's
		// default keeps prompts working; else the seed stands.
		for _, p := range conn.Providers {
			if m := conn.Default[p.ID]; m != "" {
				provider, model = p.ID, m
				break
			}
		}
	}
	d.mu.Lock()
	if model != "" {
		d.defaultModelProvider, d.defaultModelID = provider, model
	}
	d.contextLimits = limits
	d.mu.Unlock()
	if model != "" {
		d.log.Info("kilo default model resolved",
			slog.String("model", provider+"/"+model),
			slog.Int("context_limits", len(limits)))
	}
}

// DecodeFrame accepts both Kilo SSE envelope forms: /global/event wraps each
// event as {directory, project, payload:{type, properties|data}}; the
// per-directory /event stream sends the bare {type, properties|data} form.
//
// kilo 7.4.22's OpenAPI lists both the Event* shape (`properties`) and the
// durable shape (`data`) on the same /global/event stream. An empty
// session id drops the frame in httpagent, so a permission.asked that only
// carries `data` would never reach auto-approve — the agent stays blocked
// on bash/grep even in auto mode.
func (d *httpDialect) DecodeFrame(data []byte) (string, json.RawMessage, string, bool) {
	var frame struct {
		Payload struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
			Data       json.RawMessage `json:"data"`
			SyncEvent  *kiloSyncEvent  `json:"syncEvent"`
		} `json:"payload"`
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
		Data       json.RawMessage `json:"data"`
		SyncEvent  *kiloSyncEvent  `json:"syncEvent"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return "", nil, "", false
	}
	// A `sync` frame is kilo's event-sourced twin of a plain frame, carrying a
	// per-aggregate `seq`. It is NOT routed to a session — doing so would
	// deliver every event twice — but its sequence is recorded, so a gap in
	// the stream becomes observable (MADR 0137 F7).
	if ev := firstSync(frame.SyncEvent, frame.Payload.SyncEvent); ev != nil {
		d.noteSyncSeq(ev.AggregateID, ev.Seq)
	}
	typ, props := frame.Type, firstRaw(frame.Properties, frame.Data)
	if frame.Payload.Type != "" {
		typ, props = frame.Payload.Type, firstRaw(frame.Payload.Properties, frame.Payload.Data)
	}
	return typ, props, sessionIDOf(props), true
}

// firstRaw returns the first non-empty, non-null JSON value.
func firstRaw(vals ...json.RawMessage) json.RawMessage {
	for _, v := range vals {
		if len(v) == 0 || string(v) == "null" {
			continue
		}
		return v
	}
	return nil
}

// sessionIDOf pulls properties.sessionID (or nested part/info sessionID /
// info.id for lifecycle frames). Same shape as OpenCode's demux.
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

// NewSession creates the per-session adapter (forked OpenCode session shape,
// MADR 0075 plan P2).
func (d *httpDialect) NewSession(h httpagent.Host) httpagent.DialectSession {
	return &httpSession{
		d:         d,
		h:         h,
		partText:  make(map[string]string),
		partType:  make(map[string]string),
		msgRole:   make(map[string]string),
		subagents: make(map[string]subagentState),
	}
}
