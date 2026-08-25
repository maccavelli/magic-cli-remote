// Package chunkbuf coalesces high-frequency streaming text events into fewer,
// larger events so a phone-side chat window is not asked to absorb one frame
// per model token (MADR 0024).
//
// It is a pure state machine: no goroutines, no timers, no internal locking.
// The caller owns the timer and the lock, and must serialize Add/Drain/Unflush
// *and the delivery of the events they return* under that one lock — otherwise
// a timed flush can be overtaken by a concurrent boundary event and text lands
// after the turn_complete that terminates it.
//
// Three rules define the behaviour:
//
//   - Leading edge: the first chunk after any boundary is returned verbatim
//     with nothing buffered, so time-to-first-token is unchanged.
//   - Control events are boundaries: pending text is returned ahead of them,
//     so ordering with turn_complete / permission / status is preserved.
//   - Everything else passes through without draining. usage_update and
//     available_commands must not fragment a run, or the coalescer is defeated
//     exactly the way the mobile client's batch window was (MADR 0024 §1.1).
package chunkbuf

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// growthFactor bounds a run that keeps failing to send. Above
// growthFactor*maxBytes the consumer is presumed dead and the oldest half of
// the run is discarded rather than growing without bound.
const growthFactor = 4

// Buffer coalesces one streaming run at a time. Holding a single run rather
// than a map keyed by type (the acpagent precedent) means a type switch flushes
// the previous run first, so byte order out always equals byte order in — an
// interleaved reasoning/reply stream can never be reordered.
//
// The zero value is not usable; call New.
type Buffer struct {
	win      time.Duration
	maxBytes int

	// toolLane enables coalescing of in-place tool updates; see WithToolLane.
	toolLane bool
	// tools holds at most one pending update per tool id, toolIDs preserves
	// arrival order so a drain is deterministic, and toolSince is when the
	// oldest of them arrived (the lane's flush deadline is measured from it).
	tools     map[string]event.Event
	toolIDs   []string
	toolSince time.Time

	// leading is true when the next chunk should be emitted immediately: no
	// chunk has been emitted since the last boundary. Cleared by the first
	// chunk of a run, restored by the next boundary. A timed or size flush
	// does NOT restore it — otherwise every flush would grant a fresh
	// immediate emit and double the frame rate.
	leading bool

	// tmpl is the first event of the pending run; its Text is ignored (parts
	// holds the text) and its Timestamp is replaced by since on drain.
	tmpl  event.Event
	parts []string
	n     int
	// since is when the pending run's first chunk arrived. Draining stamps the
	// merged event with it, so transcript timestamps stay monotone and report
	// when the text began rather than when it happened to be flushed.
	since time.Time
}

// New returns a Buffer that holds streaming text for at most win, or until
// maxBytes have accumulated. A win of zero or less disables coalescing
// entirely: every event is returned as it arrives, reproducing pre-MADR-0024
// behaviour exactly. maxBytes is a safety cap on catch-up bursts (resync
// tails, message-log replay), not a normal flush trigger — at typical token
// rates it never fires.
func New(win time.Duration, maxBytes int, opts ...Option) *Buffer {
	b := &Buffer{win: win, maxBytes: maxBytes, leading: true}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Option configures a Buffer at construction.
type Option func(*Buffer)

// WithToolLane also coalesces in-place tool updates (`tool_call_update` with a
// tool id), holding at most one pending state per tool id and keeping the
// latest (MADR 0042 D4).
//
// A tool that streams its output — a `bash` running a build — produces one
// update per upstream delta, each carrying up to the provider's output cap and
// each costing a WebSocket frame the client then re-clips and copies. Only
// *superseded* states are folded away, and superseding merges fields forward so
// an intermediate that set a name or kind is never lost.
//
// Three things are never held:
//
//   - a terminal status (see [event.IsTerminalToolStatus]) — a finished card
//     must not wait on the window;
//   - a `tool_call`, which positions the item rather than updating it;
//   - anything, once a boundary arrives — pending tools drain ahead of it.
//
// Opt-in because the cost it removes was measured on OpenCode's HTTP transport;
// providers whose tool emission has not been profiled keep the old pass-through.
func WithToolLane() Option {
	return func(b *Buffer) { b.toolLane = true }
}

// Enabled reports whether this Buffer coalesces at all.
func (b *Buffer) Enabled() bool { return b.win > 0 }

// IsChunk reports the high-frequency streaming text types this package
// coalesces. These are exactly the droppable *text* types in
// [event.IsControl]; everything else is either a boundary or order-independent
// telemetry. Callers use it to tell text that must be retried from telemetry
// that may be dropped.
func IsChunk(t event.Type) bool {
	return t == event.TypeAssistantChunk || t == event.TypeThoughtChunk
}

// Add folds ev into the buffer.
//
// out is what the caller must deliver now, in order. deadline is when a timed
// flush becomes due, or the zero time when nothing is pending. blocking
// reports whether out must be delivered with control-event guarantees (the
// boundary path); when false, out may be delivered with the non-blocking send
// and any chunk event that fails to send should be handed back via Unflush.
func (b *Buffer) Add(ev event.Event) (out []event.Event, deadline time.Time, blocking bool) {
	if !b.Enabled() {
		return []event.Event{ev}, time.Time{}, event.IsControl(ev.Type)
	}

	switch {
	case !IsChunk(ev.Type) && event.IsControl(ev.Type) && !event.IsInPlaceUpdate(ev):
		// Boundary: creates a transcript position, so the pending tail must
		// land first and the next chunk earns a fresh immediate emit. Held tool
		// updates go out too — a client that saw turn_complete before them
		// would render a tool still working after the turn ended.
		out = append(b.drain(), b.drainTools()...)
		out = append(out, ev)
		b.leading = true
		blocking = true

	case b.toolLane && event.IsInPlaceUpdate(ev):
		if event.IsTerminalToolStatus(ev.Status) {
			// Terminal: emit now, superseding whatever was held for this tool.
			out = []event.Event{b.takeTool(ev)}
			blocking = true
			break
		}
		b.holdTool(ev)

	case !IsChunk(ev.Type):
		// Order-independent: telemetry (usage_update, available_commands) and
		// in-place updates, which mutate an item an earlier event positioned.
		// Pass through without touching the run. Delivery is still guaranteed —
		// the caller blocking-sends on event.IsControl.
		out = []event.Event{ev}
		blocking = event.IsControl(ev.Type)

	case !b.mergeable(ev):
		// A different run (type, session, agent session or replay flag
		// changed): flush what we have, then emit the first chunk of the
		// new run immediately (leading edge). Without this the transition
		// from e.g. reasoning to text waits for the coalesce window,
		// adding ~80ms latency to the first visible reply token.
		out = b.drain()
		b.leading = true
		fallthrough

	default:
		if b.leading {
			b.leading = false
			out = append(out, ev)
			break
		}
		if len(b.parts) == 0 {
			b.tmpl = ev
			b.since = ev.Timestamp
		}
		b.parts = append(b.parts, ev.Text)
		b.n += len(ev.Text)
		if b.n >= b.maxBytes {
			out = append(out, b.drain()...)
		}
	}

	return out, b.deadline(), blocking
}

// Drain returns the pending run, if any. Used by the caller's flush timer and
// by session close.
func (b *Buffer) Drain() (event.Event, bool) {
	evs := b.drain()
	if len(evs) == 0 {
		return event.Event{}, false
	}
	return evs[0], true
}

// Unflush returns a drained run to the front of the buffer after its delivery
// failed, so streaming text is retried rather than dropped. It reports how many
// bytes were discarded by the growth guard — non-zero means the consumer is not
// draining at all and the caller should say so once, loudly.
func (b *Buffer) Unflush(ev event.Event) (dropped int) {
	if !b.Enabled() || ev.Text == "" {
		return 0
	}
	if len(b.parts) == 0 {
		b.tmpl = ev
		b.since = ev.Timestamp
	}
	b.parts = append([]string{ev.Text}, b.parts...)
	b.n += len(ev.Text)

	if limit := growthFactor * b.maxBytes; b.n > limit {
		text := strings.Join(b.parts, "")
		cut := len(text) - limit
		// Never cut mid-rune: the surviving text is shown to a user, and a
		// leading partial rune renders as a replacement character.
		for cut < len(text) && !utf8.RuneStart(text[cut]) {
			cut++
		}
		dropped = cut
		b.parts = []string{text[cut:]}
		b.n = len(b.parts[0])
	}
	return dropped
}

// Pending reports how many bytes of streaming text are buffered.
func (b *Buffer) Pending() int { return b.n }

// DrainTools returns the pending in-place tool updates in arrival order, or nil
// when the lane is idle or disabled. They are control events: the caller must
// deliver them with the same guarantee it gives any other control event, never
// the droppable path.
func (b *Buffer) DrainTools() []event.Event { return b.drainTools() }

// holdTool parks a non-terminal update, superseding any state already held for
// the same tool.
func (b *Buffer) holdTool(ev event.Event) {
	if b.tools == nil {
		b.tools = make(map[string]event.Event)
	}
	if prev, ok := b.tools[ev.ToolID]; ok {
		ev = mergeTool(prev, ev)
	} else {
		b.toolIDs = append(b.toolIDs, ev.ToolID)
		if b.toolSince.IsZero() {
			b.toolSince = ev.Timestamp
		}
	}
	b.tools[ev.ToolID] = ev
}

// takeTool merges ev over anything held for its tool id and forgets the hold.
func (b *Buffer) takeTool(ev event.Event) event.Event {
	prev, ok := b.tools[ev.ToolID]
	if !ok {
		return ev
	}
	merged := mergeTool(prev, ev)
	delete(b.tools, ev.ToolID)
	for i, id := range b.toolIDs {
		if id == ev.ToolID {
			b.toolIDs = append(b.toolIDs[:i], b.toolIDs[i+1:]...)
			break
		}
	}
	if len(b.toolIDs) == 0 {
		b.toolSince = time.Time{}
	}
	return merged
}

// mergeTool layers next over prev. Superseding must not lose information: an
// intermediate update may have carried a title or kind that the newer one
// omits, and the client's upsert keeps a field only when the incoming one is
// empty — so dropping the intermediate outright would drop that field for good.
func mergeTool(prev, next event.Event) event.Event {
	if next.ToolName == "" {
		next.ToolName = prev.ToolName
	}
	if next.ToolKind == "" {
		next.ToolKind = prev.ToolKind
	}
	if next.Status == "" {
		next.Status = prev.Status
	}
	if next.Text == "" {
		next.Text = prev.Text
	}
	return next
}

func (b *Buffer) drainTools() []event.Event {
	if len(b.toolIDs) == 0 {
		return nil
	}
	out := make([]event.Event, 0, len(b.toolIDs))
	for _, id := range b.toolIDs {
		if ev, ok := b.tools[id]; ok {
			out = append(out, ev)
		}
	}
	b.tools = nil
	b.toolIDs = nil
	b.toolSince = time.Time{}
	return out
}

// mergeable reports whether ev belongs to the pending run. An empty run
// accepts anything. event.Event holds slices, so the fields are compared
// explicitly rather than with == — which also forces a look at this list if
// chunk events ever gain a field.
func (b *Buffer) mergeable(ev event.Event) bool {
	// A replacement snapshot is never merged, in either direction: it is the
	// authoritative full text of one part, so concatenating it onto earlier
	// deltas — or letting a later delta extend it — would produce text the
	// agent never emitted (MADR 0112 A3, PLAN P4 step 6).
	if ev.Replace {
		return false
	}
	if len(b.parts) == 0 {
		return true
	}
	if b.tmpl.Replace {
		return false
	}
	return ev.Type == b.tmpl.Type &&
		ev.SessionID == b.tmpl.SessionID &&
		ev.AgentSessionID == b.tmpl.AgentSessionID &&
		ev.Replay == b.tmpl.Replay &&
		// Append deltas only coalesce within one native part. Two parts of the
		// same message are separate transcript components, and merging them
		// would give the run a single identity it cannot honestly carry.
		ev.NativeMessageID == b.tmpl.NativeMessageID &&
		ev.NativePartID == b.tmpl.NativePartID
}

// drain empties the pending run into at most one event.
func (b *Buffer) drain() []event.Event {
	if len(b.parts) == 0 {
		return nil
	}
	ev := b.tmpl
	ev.Text = strings.Join(b.parts, "")
	ev.Timestamp = b.since
	b.parts = nil
	b.n = 0
	b.tmpl = event.Event{}
	b.since = time.Time{}
	return []event.Event{ev}
}

// deadline is when the earliest pending thing must be flushed — the text run,
// the tool lane, or whichever comes first — or zero when both are idle.
func (b *Buffer) deadline() time.Time {
	var text, tools time.Time
	if len(b.parts) > 0 {
		text = b.since.Add(b.win)
	}
	if len(b.toolIDs) > 0 {
		tools = b.toolSince.Add(b.win)
	}
	switch {
	case text.IsZero():
		return tools
	case tools.IsZero():
		return text
	case tools.Before(text):
		return tools
	default:
		return text
	}
}
