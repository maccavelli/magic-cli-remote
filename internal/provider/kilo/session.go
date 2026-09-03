package kilo

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

type toolEmit struct{ status, name, kind, text string }

// httpSession is the per-session half: OpenCode REST shapes and SSE event
// translation.
type httpSession struct {
	d *httpDialect
	h httpagent.Host

	mu sync.Mutex
	// cmdDedupe suppresses repeated identical available_commands
	// advertisements (MADR 0137 F2). Guarded by mu: advertiseCommands runs
	// from more than one goroutine.
	cmdDedupe event.CommandDeduper
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
	// autoHandled remembers permission ids this session already auto-approved.
	//
	// Host.TakePending alone is not enough to dedupe: the engine sends both
	// permission.asked and permission.updated for one id, and if the second
	// arrives after the first has already been claimed and answered, its
	// TrackPermission *resurrects* the id and a second goroutine claims it
	// again — two replies for one permission. Bounded by maxAutoHandled
	// (MADR 0044).
	autoHandled map[string]struct{}
	autoOrder   []string
	// approvalMu serialises the auto-approval snapshot with its emit, and is
	// deliberately NOT o.mu: autoApprove runs one goroutine per permission
	// (permission.go), so two concurrent emits could deliver snapshots out of
	// order and a stale, shorter list would replace a longer one — losing an
	// approval for good under the event's replace semantics. o.mu is unsuitable
	// because it is already held across non-blocking chunk emits and must not
	// gain a blocking-emit path (MADR 0051 §4.3).
	approvalMu    sync.Mutex
	autoApprovals []event.ApprovalItem
	// lastToolEmit holds the last payload actually emitted per tool id, so the
	// engine's repeated part.updated frames do not each cost a frame (MADR
	// 0034 D1). Cleared by turnCleanup alongside seenTools.
	lastToolEmit map[string]toolEmit
	// subagents tracks this turn's child agent sessions: agent session id →
	// name, task and status. Completed ids are retained (not deleted) so a
	// post-completion session.updated cannot reopen one; the whole map is
	// dropped by turnCleanup. Published as event.TypeSubagents — the phone
	// renders a panel, never transcript items (MADR 0051 D8).
	subagents map[string]subagentState
	// subagentsPublished latches that a non-empty set went out this turn, so
	// the clear at turn end is only sent to sessions that actually had one.
	subagentsPublished bool
	// runningSent latches that "running" has already been announced, so the
	// engine's repeated session.status busy frames do not each cost a frame
	// (MADR 0024). Cleared by any other status and by turnCleanup.
	runningSent bool
	// lastUsed/lastSize/usageSent hold the last usage report actually emitted,
	// so an unchanged token count is not re-sent (MADR 0024). usageSent is
	// cleared by turnCleanup so every turn reports at least once.
	lastUsed  int
	lastSize  int
	usageSent bool
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
	// Always pin a model at create time so the server never enqueues against a
	// model it cannot resolve (session.error: Model not found).
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
	o.h.Log().Info("kilo session create",
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
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			Tool      string          `json:"tool"`
			Synthetic bool            `json:"synthetic"`
			Ignored   bool            `json:"ignored"`
			Metadata  json.RawMessage `json:"metadata"`
			State     struct {
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
			if isKiloChromePart(part.Synthetic, part.Ignored, part.Metadata, part.Text, part.Type) {
				continue
			}
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
	o.h.Log().Info("kilo prompt_async",
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

// fromChild reports whether the SSE frame being handled originated in a child
// (sub-agent) session rather than this one.
//
// Child *content* never reaches the transcript: the sub-agent reports to the
// main agent over the engine's own channel, and the parent's own reply carries
// the conclusion. Measured on opencode 1.18.7, a single sub-agent doing a
// two-file read produced 43–81% of a turn's streamed text (MADR 0051 D6).
//
// Only valid inside HandleEvent: httpagent clears eventAgentID once dispatch
// returns, so a goroutine that outlives the handler sees "" and must capture
// the answer before it starts.
func (o *httpSession) fromChild() bool {
	ev := o.h.EventAgentSessionID()
	return ev != "" && ev != o.h.AgentSessionID()
}

// HandleEvent translates one SSE event into daemon events.
func (o *httpSession) HandleEvent(typ string, props json.RawMessage) {
	switch typ {
	case "message.updated":
		var p struct {
			Info struct {
				ID   string `json:"id"`
				Role string `json:"role"`
				// Token counts and the model that produced them: an assistant
				// message names its model flat (modelID/providerID), unlike the
				// nested ModelRef the REST bodies take.
				Tokens     *msgTokens `json:"tokens"`
				ModelID    string     `json:"modelID"`
				ProviderID string     `json:"providerID"`
				Model      *msgModel  `json:"model"`
			} `json:"info"`
		}
		if json.Unmarshal(props, &p) == nil && p.Info.ID != "" {
			o.mu.Lock()
			// Keyed by message id, so recording a child's role is harmless and
			// keeps the part handlers below able to classify what they skip.
			o.msgRole[p.Info.ID] = p.Info.Role
			o.mu.Unlock()
			// A child's token counts are not this session's context usage: the
			// indicator reports the conversation the user is having, not the
			// sub-agent's private window (MADR 0051 D7).
			if o.fromChild() {
				return
			}
			model := p.Info.Model
			if model == nil && p.Info.ModelID != "" {
				model = &msgModel{ProviderID: p.Info.ProviderID, ModelID: p.Info.ModelID}
			}
			o.emitUsage(p.Info.Role, p.Info.Tokens, model)
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
		// Before the partText bookkeeping below, so a child's part ids never
		// enter the per-turn maps that only turnCleanup clears.
		if o.fromChild() {
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
		// Kilo synthetic lifecycle parts are filtered at classification time
		// (part.updated); their deltas must not leak either.
		if ptype == "transient" {
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
				// Live kilo 7.4.20–7.4.22 marks engine chrome with
				// synthetic:true and metadata["kilocode.lifecycle"]="transient"
				// (dotted key, not a nested object). The 0075 filter looked
				// for metadata.kilocode.lifecycle and never matched, so the
				// braille spinner ("Initializing snapshot/session…") streamed
				// as assistant text on every tick.
				Synthetic bool            `json:"synthetic"`
				Ignored   bool            `json:"ignored"`
				Metadata  json.RawMessage `json:"metadata"`
			} `json:"part"`
		}
		if json.Unmarshal(props, &p) != nil {
			return
		}
		if isKiloChromePart(p.Part.Synthetic, p.Part.Ignored, p.Part.Metadata, p.Part.Text, p.Part.Type) {
			// Classify the part so any deltas for it are dropped too, and
			// never render its text.
			if p.Part.ID != "" {
				o.mu.Lock()
				o.partType[p.Part.ID] = "transient"
				o.mu.Unlock()
			}
			return
		}
		// Child text, reasoning and tool calls all arrive here; none of them is
		// this conversation. Returning before the partType write keeps the
		// per-turn maps parent-only (MADR 0051 D6).
		if o.fromChild() {
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
			id := part.CallID
			if id == "" {
				id = part.ID
			}
			if o.d != nil && o.d.onToolPartUpdated != nil {
				o.d.onToolPartUpdated(RawToolPartFrame{
					CallID: id,
					PartID: part.ID,
					Tool:   part.Tool,
					Status: part.State.Status,
					Title:  part.State.Title,
					Input:  string(part.State.Input),
					Output: part.State.Output,
					Error:  part.State.Error,
				})
			}
			status := mapToolStatus(part.State.Status)
			detail := strings.TrimSpace(part.State.Title)
			if detail == "" {
				detail = shortJSON(part.State.Input, 300)
			}
			if out := strings.TrimRight(part.State.Output, " \t\n"); out != "" {
				detail = clipBlock(out, maxToolOutputChars)
			}
			if part.State.Error != "" {
				detail = clip(part.State.Error, 300)
			}
			name := firstNonEmpty(part.State.Title, part.Tool, "tool")
			kind := kindForTool(part.Tool)

			e := toolEmit{status, name, kind, detail}
			isNew := o.noteTool(id)
			changed := o.noteToolEmit(id, e)
			if !isNew && !changed {
				return
			}
			o.h.Emit(event.Event{
				Type:     toolEventType(status, isNew),
				ToolID:   id,
				ToolName: name,
				ToolKind: kind,
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

	case "session.turn.close":
		// Kilo's explicit turn boundary (MADR 0075 §2.4) — an EndTurn signal
		// alongside session.idle/status. Safe to fire on both: the second
		// arrival finds no active turn and does nothing. A close with
		// reason:error is accompanied by session.error, which owns the card.
		sid := firstNonEmpty(sessionIDOf(props), o.h.EventAgentSessionID(), o.h.AgentSessionID())
		o.noteNodeIdle(sid)
		o.tryTreeEndTurn()

	case "session.turn.open", "sync", "file.watcher.updated",
		"message.part.removed", "session.next.agent.switched",
		"session.next.model.switched", "session.next.synthetic",
		"file.edited", "session.next.text.started", "session.next.text.ended",
		"session.next.reasoning.started", "session.next.reasoning.ended",
		"session.next.step.started", "session.next.step.ended",
		"session.next.step.failed":
		// Kilo extras that carry nothing for the transcript (MADR 0075 §2.4):
		// turn.open duplicates session.status busy; sync is the engine's sync
		// bus; part.removed is transient UI cleanup for parts never rendered;
		// next.* switches are acknowledged via the mode/model paths.
		// session.next.synthetic is the 7.4.22 "Initializing session…" ticker
		// — the same chrome as the transient text part, not assistant chat.
		// file.edited is snapshot/edit metadata; diffs are pulled via Diff().

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
				o.finishAllSubagents()
				// An errored turn is still a finished turn: without this the
				// per-turn maps (notably seenTools) leaked into the next one,
				// so its first tool re-used call id emitted tool_call_update
				// with no preceding tool_call.
				o.turnCleanup()
				// turnCleanup dropped the map; tell the phone to drop the panel.
				o.clearSubagents()
				// Same reasoning for the approval audit: an errored turn still
				// approved whatever it approved, and the card must be closed
				// rather than left running into the next turn.
				o.finishApprovals()
			}
			if p.Error.Name == "MessageAbortedError" {
				if active {
					o.h.Emit(event.Event{Type: event.TypeTurnComplete, Status: "cancelled", StopReason: "cancelled"})
					o.emitStatus("idle")
				}
				return
			}
			msg := firstNonEmpty(p.Error.Data.Message, p.Error.Name, "agent error")
			// Present classifies and rewrites 429/529/quota dumps; fall back
			// to a clipped raw message for unclassified failures.
			cls := agenterr.Present(msg, time.Now())
			out := cls.Message
			if out == "" {
				out = clip(msg, 400)
			}
			o.h.Emit(event.Event{
				Type:      event.TypeError,
				Error:     out,
				ErrorKind: string(cls.Kind),
				RetryAt:   cls.ResetAt,
			})
			o.emitStatus("error")
			return
		}
		o.h.NoteNodeStatus(sid, httpagent.NodeIdle)
		// A sub-agent that errored is not one that finished: the panel says so,
		// and the notice below stays because a failed sub-agent is worth a
		// transcript line even though its output never is.
		o.finishSubagent(sid, event.SubagentStatusFailed)
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
// resync-recovered turn-end). All per-turn maps are cleared unconditionally
// so they never grow stale over a session's lifetime.
func (o *httpSession) turnCleanup() {
	o.mu.Lock()
	o.partText = make(map[string]string)
	o.partType = make(map[string]string)
	o.msgRole = make(map[string]string)
	o.seenTools = nil
	o.lastToolEmit = nil
	o.subagents = nil
	// A new turn must re-announce "running" and re-report usage even if the
	// numbers are unchanged, so the phone never sits on stale turn state
	// (MADR 0024). emitStatus clears runningSent on any non-running status
	// too; this covers a turn that ends without emitting one at all.
	o.runningSent = false
	o.usageSent = false
	o.mu.Unlock()
}

// emitStatus emits a session_status, suppressing a repeated "running".
//
// OpenCode re-sends session.status busy for every step of a turn. Each repeat
// costs a WebSocket frame and — because session_status is not batchable on the
// phone — forces an immediate transcript commit, defeating the client's own
// 32ms coalescing window (MADR 0024 §1.1). Any other status clears the latch
// and is always delivered, so a status the client must not miss (idle, error,
// and the MADR 0014 resync corrections that emit them) is never suppressed.
func (o *httpSession) emitStatus(status string) {
	o.mu.Lock()
	if status == "running" {
		if o.runningSent {
			o.mu.Unlock()
			return
		}
		o.runningSent = true
	} else {
		o.runningSent = false
	}
	o.mu.Unlock()
	o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: status})
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

// noteToolEmit records the last emitted payload for a tool id; returns true if changed.
func (o *httpSession) noteToolEmit(id string, e toolEmit) bool {
	if id == "" {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.lastToolEmit == nil {
		o.lastToolEmit = make(map[string]toolEmit)
	}
	last, ok := o.lastToolEmit[id]
	o.lastToolEmit[id] = e
	return !ok || last != e
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
	case "bash", "shell":
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

// splitModel turns "provider/model" into its parts, splitting on the FIRST
// slash only (plan PD3): Kilo model ids may themselves contain slashes
// ("kilo/~anthropic/x" → provider "kilo", model "~anthropic/x"). A bare name
// is treated as a Kilo Gateway model.
func splitModel(m string) (providerID, modelID string) {
	m = strings.TrimSpace(m)
	if m == "" {
		return "", ""
	}
	if i := strings.IndexByte(m, '/'); i > 0 {
		return m[:i], m[i+1:]
	}
	return "kilo", m
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// isKiloChromePart reports engine-generated parts that must never reach the
// transcript. Live kilo (docs/kilo-spike-7.4.20/sse-or.raw, 7.4.22 OpenAPI)
// marks them with synthetic:true and/or metadata["kilocode.lifecycle"] as a
// dotted key. The nested metadata.kilocode.lifecycle shape is also accepted
// so a future engine cleanup cannot re-open the leak.
func isKiloChromePart(synthetic, ignored bool, metadata json.RawMessage, text, partType string) bool {
	switch strings.ToLower(strings.TrimSpace(partType)) {
	case "tool":
		return false
	case "snapshot", "patch", "step-start", "step-finish", "compaction", "retry":
		return true
	}
	if synthetic || ignored {
		return true
	}
	if kiloLifecycleIsTransient(metadata) {
		return true
	}
	return looksLikeKiloLifecycleText(text)
}

func kiloLifecycleIsTransient(metadata json.RawMessage) bool {
	if len(metadata) == 0 || string(metadata) == "null" {
		return false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(metadata, &raw) != nil {
		return false
	}
	if v, ok := raw["kilocode.lifecycle"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.EqualFold(strings.TrimSpace(s), "transient") {
			return true
		}
	}
	if v, ok := raw["kilocode"]; ok {
		var nested struct {
			Lifecycle string `json:"lifecycle"`
		}
		if json.Unmarshal(v, &nested) == nil && strings.EqualFold(strings.TrimSpace(nested.Lifecycle), "transient") {
			return true
		}
	}
	return false
}

// looksLikeKiloLifecycleText matches the braille-spinner "Initializing
// snapshot/session…" chrome that kilo writes into a synthetic text part
// every few hundred milliseconds. Used as a last-resort filter when a
// frame arrives without metadata (or with a key we have not seen yet).
func looksLikeKiloLifecycleText(s string) bool {
	t := strings.TrimLeftFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r)
	})
	t = strings.ToLower(strings.TrimSpace(t))
	return strings.HasPrefix(t, "initializing snapshot") ||
		strings.HasPrefix(t, "initializing session")
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

// maxToolOutputChars caps emitted tool stdout/stderr detail. Aligned with the client's
// kMaxExpandedDetailChars (chat_models.dart:47). Not set to kMaxItemTextChars (100,000)
// because shipping 100 KB the mobile UI will never render is waste (MADR 0034 D2).
const maxToolOutputChars = 8000

// clipBlock truncates multi-line tool output for transport. Unlike clip it
// preserves line structure — a directory listing or grep result is
// unreadable once newlines are collapsed — and never cuts mid-rune.
func clipBlock(s string, max int) string {
	s = strings.TrimRight(s, " \t\n")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…[truncated]"
}
