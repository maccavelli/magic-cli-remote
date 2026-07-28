// Package session manages live agent sessions and event fan-out.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// StatusDisconnected is the session status reported once the backing provider
// process is gone. Sessions in this state are no longer live.
const StatusDisconnected = "disconnected"

// historyBufferCap bounds the per-session replay ring buffer. Aligned with the
// mobile client's kMaxTranscriptItems (800) so cold reopen can rebuild a full
// phone-side transcript (MADR 0018 E4). Oldest events drop.
const historyBufferCap = 800

// historyDefaultPage is the default number of events returned by a single
// session.history response when the client does not set limit.
const historyDefaultPage = 200

// historyMaxPage caps client-requested page size.
const historyMaxPage = 800

// historyMaxResponseBytes soft-caps one history_result frame (~0.5 MiB) so a
// tool-heavy ring cannot force a multi-megabyte JSON blob onto a slow phone.
const historyMaxResponseBytes = 512 << 10

// maxSessionNameLen bounds user-managed labels independently of the transport
// frame limit. It matches the create-name boundary in ws.Server.
const maxSessionNameLen = 256

var (
	// ErrNotLive is returned when a mutating op targets a missing or dead session.
	ErrNotLive = errors.New("session not found or not live")
	// ErrForbidden is returned when a device is not the session owner (R4=B).
	ErrForbidden = errors.New("session access forbidden")
	// ErrShuttingDown is returned when Create is attempted after CloseAll has
	// begun (Phase 1.6): the daemon is draining and must not start new agents.
	ErrShuttingDown = errors.New("session manager shutting down")
)

// Meta is public session metadata.
type Meta struct {
	ID       string      `json:"id"`
	Provider provider.ID `json:"provider"`
	Name     string      `json:"name"`
	// Model is the agent model this session was last (re)started with. Empty
	// means the provider's default. Persisted on disk with the session record
	// so resume after restart keeps the same model (Phase 3.3).
	Model          string `json:"model,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// OwnerDeviceID is the paired device that created (or claimed) the session.
	// Empty means legacy/unowned — visible to all devices until claimed (R4=B).
	OwnerDeviceID string    `json:"owner_device_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"`
	Live          bool      `json:"live"`
}

type entry struct {
	meta   Meta
	sess   provider.Session
	cancel context.CancelFunc
	// dead is set briefly while auto-close runs after a disconnected status.
	// Entries are removed from m.sessions on close; dead is not a long-lived
	// tombstone (R2=A).
	dead bool
	// history is a bounded ring buffer of every event this session emitted,
	// oldest first, for session.history replay. Capped at historyBufferCap.
	history []event.Event
	// seq is the per-session monotonic event sequence, stamped onto each event
	// as it enters history (and therefore onto the broadcast copy too).
	seq uint64
	// agentCommands is the set of slash commands the agent last advertised over
	// ACP (available_commands), if any. Used to decide whether an unknown
	// /command should be forwarded to the agent or reported as unavailable.
	agentCommands []string
	// agentModes is the operating-mode list last advertised for this session
	// (session_mode), and currentModeID the active one. Tracked so /plan and
	// /mode can resolve a mode id without asking the provider (MADR 0022).
	agentModes    []event.SessionMode
	currentModeID string
	// lastUsage is the most recent token/context report (usage_update). It is
	// what /context answers from, and its presence is what makes /context
	// available at all on a provider that reports usage (MADR 0023).
	lastUsage *event.Usage
	// advertised is the canonical command list last sent to clients, kept so a
	// re-resolution only emits when the answer actually changed.
	advertised []event.RemoteCommand
}

// historyTrimTo is what the ring is cut back to when it exceeds
// historyBufferCap. Trimming in batches instead of one-at-a-time avoids an
// O(cap) memmove on every event past the cap.
const historyTrimTo = historyBufferCap - historyBufferCap/4

// appendHistoryLocked stamps ev with the next sequence number and records it.
// Caller holds m.mu; the stamp is visible to the caller's broadcast copy.
func (e *entry) appendHistoryLocked(ev *event.Event) {
	e.seq++
	ev.Seq = e.seq
	e.history = append(e.history, *ev)
	if len(e.history) > historyBufferCap {
		e.history = slices.Delete(e.history, 0, len(e.history)-historyTrimTo)
	}
}

// EventHandler is called for every session event (e.g. WS broadcast).
type EventHandler func(ev event.Event)

// ErrLimitReached is returned when MaxLiveSessions would be exceeded.
var ErrLimitReached = errors.New("live session limit reached")

// Manager tracks sessions created via providers.
type Manager struct {
	reg     *provider.Registry
	store   *Store
	log     *slog.Logger
	onEvent EventHandler
	// maxLive caps concurrent live sessions (0 = unlimited).
	maxLive int

	// createMu guards createLocks. Creates are serialized per session id (not
	// globally): close-and-replace for one id must not race another Create for
	// that id, but a slow agent launch (up to startTimeout) must not block
	// every other device's session.create.
	createMu    sync.Mutex
	createLocks map[string]*createLock

	// advertiseMu serializes canonical-command advertisement. Resolution reads a
	// snapshot of the session and then compares it with the last list sent, so
	// two concurrent advertisers — session create and the pump, when the agent's
	// modes or commands arrive — could otherwise store the *stale* snapshot last
	// and leave clients with a list nothing will correct. Held across the
	// snapshot, not just the store, or the same race just moves.
	advertiseMu sync.Mutex

	mu       sync.RWMutex
	sessions map[string]*entry
	// reserved counts Creates that passed the maxLive check but have not yet
	// inserted their entry, so concurrent creates of different ids cannot
	// overshoot the cap now that they no longer share one lock.
	reserved int
	// shuttingDown is set by CloseAll; Create fails closed once true so a
	// Start that finishes after shutdown cannot re-insert a live session.
	shuttingDown bool

	// Debounced disk writes for chatty session_status updates (Phase 4.3).
	// Create / claim / close always flush immediately via persistNow.
	//
	// dirtyPersist is a set of session ids, not a snapshot of Meta: the flush
	// re-reads the authoritative in-memory meta so a debounced write can never
	// revert a newer immediate write (e.g. an owner claim) nor resurrect a
	// deleted session — ids no longer live are dropped at flush time.
	persistMu    sync.Mutex
	dirtyPersist map[string]struct{}
	persistTimer *time.Timer

	// Debounced durable transcript writes (Phase D). Close / CloseAll flush
	// immediately so a restart still sees the last ring.
	historyMu    sync.Mutex
	dirtyHistory map[string]struct{}
	historyTimer *time.Timer

	// runCtx is cancelled by CloseAll so session pumps deriving from it also
	// exit when the manager shuts down.
	runCtx    context.Context
	runCancel context.CancelFunc
}

// persistDebounce batches status-only meta writes under chatty agents.
const persistDebounce = 2 * time.Second

// historyPersistDebounce batches transcript snapshots under streaming agents.
const historyPersistDebounce = 1 * time.Second

type createLock struct {
	mu   sync.Mutex
	refs int
}

// lockCreate acquires the per-id create lock, returning its release func.
func (m *Manager) lockCreate(id string) func() {
	m.createMu.Lock()
	l := m.createLocks[id]
	if l == nil {
		l = &createLock{}
		m.createLocks[id] = l
	}
	l.refs++
	m.createMu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		m.createMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(m.createLocks, id)
		}
		m.createMu.Unlock()
	}
}

// NewManager creates a session manager. store may be nil (no persistence).
// maxLiveSessions bounds concurrent live sessions; 0 means unlimited.
func NewManager(reg *provider.Registry, store *Store, log *slog.Logger, onEvent EventHandler) *Manager {
	return NewManagerWithLimits(reg, store, log, onEvent, 0)
}

// NewManagerWithLimits is like NewManager but sets a live-session cap.
func NewManagerWithLimits(reg *provider.Registry, store *Store, log *slog.Logger, onEvent EventHandler, maxLiveSessions int) *Manager {
	if log == nil {
		log = slog.Default()
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	return &Manager{
		reg:          reg,
		store:        store,
		log:          log.With(slog.String("component", "session")),
		onEvent:      onEvent,
		maxLive:      maxLiveSessions,
		createLocks:  make(map[string]*createLock),
		sessions:     make(map[string]*entry),
		dirtyPersist: make(map[string]struct{}),
		dirtyHistory: make(map[string]struct{}),
		runCtx:       runCtx,
		runCancel:    runCancel,
	}
}

// validSessionID constrains client-chosen local session ids. Anything outside
// this shape could alias store paths ("../x" vs "x" hit the same meta.json) or
// nest directories the store's one-level List never returns.
var validSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// LiveCount returns the number of non-dead sessions currently tracked.
func (m *Manager) LiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, e := range m.sessions {
		if !e.dead {
			n++
		}
	}
	return n
}

// Create starts a new session with the given provider, owned by ownerDeviceID.
//
// If opts.LocalSessionID already names a live session, that session is fully
// closed first and replaced (R3=B). The returned Meta is live only after the
// new provider Start succeeds.
//
// ownerDeviceID may be empty in tests; production WS paths pass the authed
// device id (R4=B).
func (m *Manager) Create(ctx context.Context, providerID provider.ID, opts provider.StartOptions, ownerDeviceID string) (Meta, error) {
	p, err := m.reg.Get(providerID)
	if err != nil {
		return Meta{}, err
	}
	if opts.LocalSessionID == "" {
		opts.LocalSessionID = uuid.NewString()
	} else if !validSessionID.MatchString(opts.LocalSessionID) {
		return Meta{}, fmt.Errorf("invalid session id %q", opts.LocalSessionID)
	}

	unlock := m.lockCreate(opts.LocalSessionID)
	defer unlock()

	// Close-and-replace only if the caller owns the existing session (R4=B).
	// Dead entries (mid auto-close) still carry an owner and are checked too.
	replacing := false
	reserved := false
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return Meta{}, ErrShuttingDown
	}
	if e, ok := m.sessions[opts.LocalSessionID]; ok {
		replacing = !e.dead
		if e.meta.OwnerDeviceID != "" && ownerDeviceID != "" && e.meta.OwnerDeviceID != ownerDeviceID {
			m.mu.Unlock()
			return Meta{}, fmt.Errorf("%w: %q", ErrForbidden, opts.LocalSessionID)
		}
	}
	// Soft limit: count live sessions plus in-flight creates; replace of an
	// existing id does not grow.
	if m.maxLive > 0 && !replacing {
		live := 0
		for _, e := range m.sessions {
			if !e.dead {
				live++
			}
		}
		if live+m.reserved >= m.maxLive {
			m.mu.Unlock()
			return Meta{}, fmt.Errorf("%w: max %d", ErrLimitReached, m.maxLive)
		}
		m.reserved++
		reserved = true
	}
	m.mu.Unlock()
	defer func() {
		if reserved {
			m.mu.Lock()
			m.reserved--
			m.mu.Unlock()
		}
	}()

	// A persisted (non-live) record is still owned: without this check any
	// device could squat a known id, resume its agent session, and overwrite
	// the record's owner.
	if !replacing && m.store != nil && ownerDeviceID != "" {
		if rec, err := m.store.Get(opts.LocalSessionID); err == nil {
			if rec.OwnerDeviceID != "" && rec.OwnerDeviceID != ownerDeviceID {
				return Meta{}, fmt.Errorf("%w: %q", ErrForbidden, opts.LocalSessionID)
			}
		}
	}

	// Close-and-replace: never map-overwrite without closing the prior process.
	if err := m.close(ctx, opts.LocalSessionID, false); err != nil && !errors.Is(err, ErrNotLive) {
		return Meta{}, err
	}

	sess, err := p.Start(ctx, opts)
	if err != nil {
		return Meta{}, err
	}

	// Prefer the session's resolved working directory (config default or
	// home-dir fallback) over the raw request value, so metadata reflects
	// where the agent actually runs even when the client sent nothing.
	cwd := opts.CWD
	if c, ok := sess.(provider.CWDSession); ok && c.CWD() != "" {
		cwd = c.CWD()
	}

	runCtx, cancel := context.WithCancel(m.runCtx)
	meta := Meta{
		ID:             sess.ID(),
		Provider:       providerID,
		Name:           opts.Name,
		Model:          opts.Model,
		CWD:            cwd,
		AgentSessionID: sess.AgentSessionID(),
		OwnerDeviceID:  ownerDeviceID,
		CreatedAt:      time.Now().UTC(),
		Status:         "idle",
		Live:           true,
	}
	// Prefer created_at from a prior disk record so resume does not look new.
	if m.store != nil {
		if rec, err := m.store.Get(sess.ID()); err == nil && !rec.CreatedAt.IsZero() {
			meta.CreatedAt = rec.CreatedAt
			if meta.OwnerDeviceID == "" && rec.OwnerDeviceID != "" {
				meta.OwnerDeviceID = rec.OwnerDeviceID
			}
		}
	}
	// Seed the live ring from durable history so seq continues and cold
	// clients can replay across daemon restart / close-and-replace (Phase D).
	var priorHist []event.Event
	var priorSeq uint64
	if m.store != nil {
		priorHist = m.store.LoadHistory(sess.ID())
		if n := len(priorHist); n > 0 {
			priorSeq = priorHist[n-1].Seq
			// Copy so later ring mutations never alias the store's slice.
			priorHist = append([]event.Event(nil), priorHist...)
		}
	}

	m.mu.Lock()
	// CloseAll may have flipped shuttingDown while Start ran — abandon the
	// new process rather than re-inserting a live session after drain.
	if m.shuttingDown {
		m.mu.Unlock()
		cancel()
		_ = sess.Close(ctx)
		return Meta{}, ErrShuttingDown
	}
	// Defensive: if another path inserted the same id, close it out-of-band.
	if prev, ok := m.sessions[sess.ID()]; ok {
		delete(m.sessions, sess.ID())
		m.mu.Unlock()
		prev.cancel()
		_ = prev.sess.Close(ctx)
		m.mu.Lock()
		if m.shuttingDown {
			m.mu.Unlock()
			cancel()
			_ = sess.Close(ctx)
			return Meta{}, ErrShuttingDown
		}
	}
	m.sessions[sess.ID()] = &entry{
		meta:    meta,
		sess:    sess,
		cancel:  cancel,
		history: priorHist,
		seq:     priorSeq,
	}
	m.mu.Unlock()

	go m.pump(runCtx, sess)
	// Advertise the canonical command list up front, so a client has it before
	// the first keystroke; the pump re-emits it as the session learns more
	// (agent commands, modes, first usage report).
	m.advertiseCommands(meta.ID)
	m.persistNow(meta)

	m.log.Info("session created",
		slog.String("session_id", meta.ID),
		slog.String("provider", string(providerID)),
		slog.String("agent_session_id", meta.AgentSessionID),
		slog.String("owner_device_id", meta.OwnerDeviceID),
	)
	return meta, nil
}

func (m *Manager) pump(ctx context.Context, sess provider.Session) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sess.Events():
			if !ok {
				if ctx.Err() == nil {
					m.autoClose(sess.ID(), sess, "events closed")
				}
				return
			}
			autoClose := false
			reresolve := false
			var persistMeta *Meta
			m.mu.Lock()
			e, mine := m.sessions[sess.ID()]
			// Only touch (or broadcast for) the entry when it still belongs to
			// THIS pump's session. After close-and-replace, a stale buffered
			// event from the old process must not stamp state — least of all
			// death — onto the replacement entry, nor reach clients under the
			// replacement's id.
			mine = mine && e.sess == sess
			if mine {
				if ev.Type == event.TypeSessionStatus && ev.Status != "" {
					e.meta.Status = ev.Status
					if ev.AgentSessionID != "" {
						e.meta.AgentSessionID = ev.AgentSessionID
					}
					if ev.Status == StatusDisconnected {
						e.dead = true
						e.meta.Live = false
						autoClose = true
					}
					// Snapshot; the disk write happens after unlock — persist
					// under m.mu would stall every session on slow disk.
					meta := e.meta
					persistMeta = &meta
				}
				if ev.Type == event.TypeAvailableCommands {
					names := make([]string, 0, len(ev.Commands))
					for _, c := range ev.Commands {
						if n := strings.TrimPrefix(c.Name, "/"); n != "" {
							names = append(names, n)
						}
					}
					e.agentCommands = names
					reresolve = true
				}
				if ev.Type == event.TypeMode {
					// Same merge rule as the clients': the full list arrives at
					// session create/load, later updates carry only the new
					// current id.
					if len(ev.Modes) > 0 {
						e.agentModes = ev.Modes
						reresolve = true
					}
					if ev.CurrentModeID != "" {
						e.currentModeID = ev.CurrentModeID
					}
				}
				if ev.Type == event.TypeUsage && ev.Usage != nil {
					// The first report is also what makes /context possible, so
					// the advertised list needs a second look.
					reresolve = e.lastUsage == nil
					e.lastUsage = ev.Usage
				}
				e.appendHistoryLocked(&ev)
			}
			histID := ""
			if mine {
				histID = sess.ID()
			}
			m.mu.Unlock()
			if reresolve {
				// Outside the lock: resolution reads provider capabilities and
				// emits its own event.
				m.advertiseCommands(sess.ID())
			}
			if histID != "" {
				m.scheduleHistoryPersist(histID)
			}
			if persistMeta != nil {
				// Disconnect status must hit disk immediately; idle/running
				// chatter is debounced (Phase 4.3).
				if autoClose || persistMeta.Status == StatusDisconnected {
					m.persistNow(*persistMeta)
				} else {
					m.persist(persistMeta.ID)
				}
			}
			// Replay events (session/load re-emitting the prior conversation)
			// go into history for cold clients but are never broadcast live —
			// a client that resumed a session it already displays would
			// append the whole conversation again.
			if mine && !ev.Replay && m.onEvent != nil {
				m.onEvent(ev)
			}
			if autoClose {
				if ctx.Err() == nil {
					m.autoClose(sess.ID(), sess, "provider disconnected")
				}
				return
			}
		}
	}
}

func (m *Manager) autoClose(id string, sess provider.Session, reason string) {
	ctx, cancel := context.WithTimeout(m.runCtx, 15*time.Second)
	defer cancel()
	// closeMatching removes the entry only while it still holds this exact
	// session — a check-then-close by id alone could tear down a replacement
	// created in between.
	if err := m.closeMatching(ctx, id, sess, false); err != nil {
		if !errors.Is(err, ErrNotLive) {
			m.log.Warn("auto-close session failed",
				slog.String("session_id", id),
				slog.String("reason", reason),
				slog.String("err", err.Error()),
			)
		}
		return
	}
	m.log.Info("session auto-closed",
		slog.String("session_id", id),
		slog.String("reason", reason),
	)
}

// Get returns session metadata if currently tracked (live).
func (m *Manager) Get(id string) (Meta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok || e.dead {
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	return e.meta, nil
}

// OwnerOf returns the owner device id for a live session, or a persisted
// record if not live. found is false if the session is unknown.
func (m *Manager) OwnerOf(sessionID string) (owner string, found bool) {
	m.mu.RLock()
	if e, ok := m.sessions[sessionID]; ok {
		owner = e.meta.OwnerDeviceID
		m.mu.RUnlock()
		return owner, true
	}
	m.mu.RUnlock()
	if m.store == nil {
		return "", false
	}
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return "", false
	}
	return rec.OwnerDeviceID, true
}

// visibleTo reports whether deviceID may see a session with the given owner.
// Empty owner (legacy) is visible to every device.
func visibleTo(ownerDeviceID, deviceID string) bool {
	return ownerDeviceID == "" || ownerDeviceID == deviceID
}

// Authorize checks that deviceID may access sessionID.
// When claim is true and the session has an empty (legacy) owner, the device
// is stamped as owner (first-touch claim).
func (m *Manager) Authorize(sessionID, deviceID string, claim bool) error {
	m.mu.Lock()
	if e, ok := m.sessions[sessionID]; ok && !e.dead {
		if e.meta.OwnerDeviceID == "" {
			if claim && deviceID != "" {
				e.meta.OwnerDeviceID = deviceID
				meta := e.meta
				m.mu.Unlock()
				m.persistNow(meta)
				return nil
			}
			m.mu.Unlock()
			return nil
		}
		if e.meta.OwnerDeviceID != deviceID {
			m.mu.Unlock()
			return fmt.Errorf("%w: %q", ErrForbidden, sessionID)
		}
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	// Disk-only (not live): used for delete of persisted rows.
	if m.store == nil {
		return fmt.Errorf("%w: %q", ErrNotLive, sessionID)
	}
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrNotLive, sessionID)
	}
	if rec.OwnerDeviceID == "" {
		if claim && deviceID != "" {
			rec.OwnerDeviceID = deviceID
			if err := m.store.Save(rec); err != nil {
				m.log.Warn("persist session claim failed",
					slog.String("session_id", sessionID),
					slog.String("err", err.Error()),
				)
			}
		}
		return nil
	}
	if rec.OwnerDeviceID != deviceID {
		return fmt.Errorf("%w: %q", ErrForbidden, sessionID)
	}
	return nil
}

// History returns a copy of the full buffered event replay for a session,
// oldest first (up to historyBufferCap). Live sessions use the in-memory ring;
// closed sessions load durable history from disk when a store is configured
// (Phase D). An unknown or never-active session returns an empty (non-nil)
// slice, not an error.
//
// Wire clients should prefer HistoryPage / HistoryPageFor so one response stays
// within historyMaxResponseBytes (Phase 3.5).
//
// Callers that enforce ownership should use Authorize before History, or
// HistoryFor / HistoryPageFor.
func (m *Manager) History(id string) []event.Event {
	m.mu.RLock()
	e, ok := m.sessions[id]
	if ok && !e.dead && len(e.history) > 0 {
		out := make([]event.Event, len(e.history))
		copy(out, e.history)
		m.mu.RUnlock()
		return out
	}
	m.mu.RUnlock()
	if m.store == nil {
		return []event.Event{}
	}
	return m.store.LoadHistory(id)
}

// HistoryPage returns a page of history events with Seq > sinceSeq (exclusive),
// oldest first. limit 0 uses historyDefaultPage; values above historyMaxPage
// are clamped. truncated is true when more events remain after this page;
// nextSinceSeq is the last Seq in the page (0 if empty) for the next request.
//
// A soft byte budget (historyMaxResponseBytes) may shorten the page further so
// one WS frame cannot become multi-megabyte under tool-heavy rings (Phase 3.5).
// Closed sessions page from durable disk history when available (Phase D).
func (m *Manager) HistoryPage(id string, sinceSeq uint64, limit int) (events []event.Event, truncated bool, nextSinceSeq uint64) {
	if limit <= 0 {
		limit = historyDefaultPage
	}
	if limit > historyMaxPage {
		limit = historyMaxPage
	}

	ring := m.historyRing(id)
	if len(ring) == 0 {
		return []event.Event{}, false, 0
	}

	// Skip events at or below sinceSeq.
	start := 0
	if sinceSeq > 0 {
		for start < len(ring) && ring[start].Seq <= sinceSeq {
			start++
		}
	}
	if start >= len(ring) {
		return []event.Event{}, false, 0
	}

	end := start + limit
	if end > len(ring) {
		end = len(ring)
	}
	// Soft byte budget: shrink end until the JSON-ish estimate fits.
	for end > start {
		approx := 0
		for i := start; i < end; i++ {
			approx += approxEventBytes(ring[i])
		}
		if approx <= historyMaxResponseBytes || end == start+1 {
			break
		}
		end--
	}

	out := make([]event.Event, end-start)
	copy(out, ring[start:end])
	truncated = end < len(ring)
	if len(out) > 0 {
		nextSinceSeq = out[len(out)-1].Seq
	}
	return out, truncated, nextSinceSeq
}

// historyRing returns a copy of live memory history, or durable disk history.
func (m *Manager) historyRing(id string) []event.Event {
	m.mu.RLock()
	e, ok := m.sessions[id]
	if ok && !e.dead && len(e.history) > 0 {
		out := make([]event.Event, len(e.history))
		copy(out, e.history)
		m.mu.RUnlock()
		return out
	}
	m.mu.RUnlock()
	if m.store == nil {
		return nil
	}
	return m.store.LoadHistory(id)
}

// approxEventBytes estimates wire size for history paging (not exact JSON).
func approxEventBytes(ev event.Event) int {
	n := 128 // envelope / field overhead
	n += len(ev.Text) + len(ev.Error) + len(ev.ToolName) + len(ev.ToolID)
	n += len(ev.Status) + len(ev.PermissionID) + len(ev.AgentSessionID)
	return n
}

// HistoryFor returns the full history ring after an ownership check (no claim).
func (m *Manager) HistoryFor(sessionID, deviceID string) ([]event.Event, error) {
	if err := m.Authorize(sessionID, deviceID, false); err != nil {
		if errors.Is(err, ErrNotLive) {
			return []event.Event{}, nil
		}
		return nil, err
	}
	return m.History(sessionID), nil
}

// HistoryPageFor is HistoryPage with an ownership check (no claim).
func (m *Manager) HistoryPageFor(sessionID, deviceID string, sinceSeq uint64, limit int) (events []event.Event, truncated bool, nextSinceSeq uint64, err error) {
	if err := m.Authorize(sessionID, deviceID, false); err != nil {
		// Unknown session: empty history is the protocol contract for history
		// (not an error). Forbidden is still an error.
		if errors.Is(err, ErrNotLive) {
			return []event.Event{}, false, 0, nil
		}
		return nil, false, 0, err
	}
	events, truncated, nextSinceSeq = m.HistoryPage(sessionID, sinceSeq, limit)
	return events, truncated, nextSinceSeq, nil
}

// List returns all live sessions merged with persisted records (no owner filter).
// Prefer ListFor in multi-device paths.
func (m *Manager) List() []Meta {
	return m.listFiltered("")
}

// ListFor returns sessions visible to deviceID (owned by it, or legacy empty owner).
// When deviceID is empty, returns all sessions (test / unrestricted paths).
func (m *Manager) ListFor(deviceID string) []Meta {
	return m.listFiltered(deviceID)
}

func (m *Manager) listFiltered(deviceID string) []Meta {
	m.mu.RLock()
	live := make(map[string]Meta, len(m.sessions))
	out := make([]Meta, 0, len(m.sessions))
	for _, e := range m.sessions {
		if e.dead {
			continue
		}
		meta := e.meta
		meta.Live = true
		if deviceID != "" && !visibleTo(meta.OwnerDeviceID, deviceID) {
			continue
		}
		live[meta.ID] = meta
		out = append(out, meta)
	}
	m.mu.RUnlock()

	if m.store == nil {
		return out
	}
	recs, err := m.store.List()
	if err != nil {
		return out
	}
	for _, rec := range recs {
		if _, ok := live[rec.ID]; ok {
			continue
		}
		if deviceID != "" && !visibleTo(rec.OwnerDeviceID, deviceID) {
			continue
		}
		out = append(out, Meta{
			ID:             rec.ID,
			Provider:       rec.Provider,
			Name:           rec.Name,
			Model:          rec.Model,
			CWD:            rec.CWD,
			AgentSessionID: rec.AgentSessionID,
			OwnerDeviceID:  rec.OwnerDeviceID,
			CreatedAt:      rec.CreatedAt,
			Status:         rec.Status,
			Live:           false,
		})
	}
	return out
}

func (m *Manager) liveSession(id string) (provider.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok || e.dead {
		return nil, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	return e.sess, nil
}

// Prompt sends a text prompt to a live session owned by (or claimable by)
// deviceID. A leading built-in slash command (see [BuiltinCommands]) is
// intercepted and handled by the daemon; every other prompt — including
// unknown /commands, which belong to the agent — is forwarded unchanged.
func (m *Manager) Prompt(ctx context.Context, id, text string, attachments []provider.Content, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	// A leading token that looks like a command ("/name …") is routed rather
	// than sent verbatim to the agent. A "/" that is not a plausible command
	// name (a path, code, etc.) falls through as a normal prompt.
	if name, rest, ok := parseSlashCommand(text); ok && isCommandName(name) {
		// Canonical commands are resolved against what this session really
		// offers (MADR 0023); anything else belongs to the agent.
		if res, canonical := m.resolveCommand(id, name); canonical {
			handled, forward, err := m.runCanonical(ctx, id, deviceID, res, name, rest)
			if handled || err != nil {
				return err
			}
			// Not handled here: the agent executes this one. Send its own
			// command text, which may differ from what the user typed.
			text = forward
		} else if !m.agentAdvertises(id, name) {
			// Neither canonical nor offered by the agent. Report it rather than
			// sending confusing literal text to the model.
			m.echoUser(id, text)
			m.emitNotice(id, fmt.Sprintf(
				"“/%s” isn't available over the remote — it's not a built-in and "+
					"the agent hasn't offered it. Type /help for what you can run "+
					"from here.", name))
			return nil
		}
		// Else: the agent owns this command; forward it unchanged (it echoes the
		// user message itself), falling through to the normal prompt path.
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	parts := make([]provider.Content, 0, 1+len(attachments))
	// Text first so it precedes attachments in the block list; skip an empty
	// text part on an attachment-only prompt.
	if text != "" || len(attachments) == 0 {
		parts = append(parts, provider.Content{Type: "text", Text: text})
	}
	parts = append(parts, attachments...)
	return sess.Prompt(ctx, parts)
}

// SetMode switches the session's active operating mode (ACP session modes).
// Errors when the provider session does not support modes.
func (m *Manager) SetMode(ctx context.Context, id, modeID, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	return m.setMode(ctx, id, modeID)
}

// setMode is the unauthorized half of [Manager.SetMode], shared with the /plan
// and /mode builtins (the prompt path authorized the caller already).
func (m *Manager) setMode(ctx context.Context, id, modeID string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	ms, ok := sess.(provider.ModeSession)
	if !ok {
		return fmt.Errorf("session does not support modes")
	}
	return ms.SetMode(ctx, modeID)
}

// SetConfigOption changes an agent-defined session config option (ACP session
// config options). Errors when the provider session does not support them.
func (m *Manager) SetConfigOption(ctx context.Context, id, optionID, kind, value, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	cs, ok := sess.(provider.ConfigSession)
	if !ok {
		return fmt.Errorf("session does not support config options")
	}
	return cs.SetConfigOption(ctx, optionID, kind, value)
}

// Rename changes a user-visible session label only after the provider-native
// title accepts the update. It is metadata, never an agent prompt or event.
func (m *Manager) Rename(ctx context.Context, id, title, deviceID string) (Meta, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Meta{}, fmt.Errorf("session name required")
	}
	if len(title) > maxSessionNameLen {
		return Meta{}, fmt.Errorf("session name too long")
	}
	if err := m.Authorize(id, deviceID, true); err != nil {
		return Meta{}, err
	}
	m.mu.RLock()
	e, ok := m.sessions[id]
	var sess provider.Session
	if ok && !e.dead {
		sess = e.sess
	}
	m.mu.RUnlock()
	if sess == nil {
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	rs, ok := sess.(provider.RenameSession)
	if !ok {
		return Meta{}, fmt.Errorf("session %q does not support rename", id)
	}
	if err := rs.Rename(ctx, title); err != nil {
		return Meta{}, err
	}
	m.mu.Lock()
	e, ok = m.sessions[id]
	if !ok || e.dead || e.sess != sess {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	e.meta.Name = title
	meta := e.meta
	m.mu.Unlock()
	m.persistNow(meta)
	return meta, nil
}

// Diagnostics obtains bounded, read-only project metadata. It intentionally
// does not append history or broadcast a transcript event.
func (m *Manager) Diagnostics(ctx context.Context, id, deviceID string) (provider.Diagnostics, error) {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return provider.Diagnostics{}, err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return provider.Diagnostics{}, err
	}
	ds, ok := sess.(provider.DiagnosticsSession)
	if !ok {
		return provider.Diagnostics{}, fmt.Errorf("session %q does not support diagnostics", id)
	}
	return ds.Diagnostics(ctx)
}

// ModelCatalog returns the model picker catalog scoped to one live session:
// the models of the model provider that session is using, with its current
// model as the default (MADR 0043 D9). scope is provider.CatalogScopeModels or
// provider.CatalogScopeProviders.
//
// Unlike Diagnostics this does not claim the session — listing what a session
// could switch to is a read, and claiming it would steal ownership from another
// device just because a picker was opened. It still requires ownership: a
// session's model is not public information.
func (m *Manager) ModelCatalog(ctx context.Context, id, deviceID, scope string) (picker.Catalog, error) {
	if err := m.Authorize(id, deviceID, false); err != nil {
		return picker.Catalog{}, err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return picker.Catalog{}, err
	}
	mc, ok := sess.(provider.ModelCatalogSession)
	if !ok {
		return picker.Catalog{}, fmt.Errorf("session %q does not report a model catalog", id)
	}
	return mc.ModelCatalog(ctx, scope)
}

// Cancel cancels the in-flight turn on a session.
func (m *Manager) Cancel(ctx context.Context, id, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	return sess.Cancel(ctx)
}

// RespondPermission forwards a permission decision to the session.
func (m *Manager) RespondPermission(ctx context.Context, sessionID, permissionID, optionID string, cancelled bool, deviceID string) error {
	if err := m.Authorize(sessionID, deviceID, true); err != nil {
		return err
	}
	sess, err := m.liveSession(sessionID)
	if err != nil {
		return err
	}
	ps, ok := sess.(provider.PermissionSession)
	if !ok {
		return fmt.Errorf("session %q does not support remote permissions", sessionID)
	}
	return ps.RespondPermission(ctx, permissionID, optionID, cancelled)
}

// RespondQuestion forwards a multi-question form answer to the session.
func (m *Manager) RespondQuestion(ctx context.Context, sessionID, questionID string, answers [][]string, cancelled bool, deviceID string) error {
	if err := m.Authorize(sessionID, deviceID, true); err != nil {
		return err
	}
	sess, err := m.liveSession(sessionID)
	if err != nil {
		return err
	}
	qs, ok := sess.(provider.QuestionSession)
	if !ok {
		return fmt.Errorf("session %q does not support remote questions", sessionID)
	}
	return qs.RespondQuestion(ctx, questionID, answers, cancelled)
}

// Fork branches the provider-native conversation into a new live session
// (MADR 0020 Sprint 5). messageID is optional. The new session is owned by
// the same device and resumes the forked agent id.
func (m *Manager) Fork(ctx context.Context, id, messageID, deviceID string) (Meta, error) {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return Meta{}, err
	}
	m.mu.RLock()
	e, ok := m.sessions[id]
	var meta Meta
	var sess provider.Session
	if ok && !e.dead {
		meta = e.meta
		sess = e.sess
	}
	m.mu.RUnlock()
	if sess == nil {
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	fs, ok := sess.(provider.ForkSession)
	if !ok {
		return Meta{}, fmt.Errorf("session %q does not support fork", id)
	}
	newAgentID, err := fs.Fork(ctx, messageID)
	if err != nil {
		return Meta{}, err
	}
	name := meta.Name
	if name != "" {
		name = name + " (fork)"
	} else {
		name = "fork"
	}
	return m.Create(ctx, meta.Provider, provider.StartOptions{
		Name:           name,
		CWD:            meta.CWD,
		Model:          meta.Model,
		AgentSessionID: newAgentID,
	}, deviceID)
}

// Revert undoes a message in the provider-native session (OpenCode).
func (m *Manager) Revert(ctx context.Context, id, messageID, partID, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	rs, ok := sess.(provider.RevertSession)
	if !ok {
		return fmt.Errorf("session %q does not support revert", id)
	}
	if err := rs.Revert(ctx, messageID, partID); err != nil {
		return err
	}
	m.emitNotice(id, "Reverted message "+messageID)
	return nil
}

// Unrevert restores previously reverted messages.
func (m *Manager) Unrevert(ctx context.Context, id, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	rs, ok := sess.(provider.RevertSession)
	if !ok {
		return fmt.Errorf("session %q does not support unrevert", id)
	}
	if err := rs.Unrevert(ctx); err != nil {
		return err
	}
	m.emitNotice(id, "Restored reverted messages")
	return nil
}

// Diff returns a short file-change summary for the session (OpenCode GET …/diff).
// Also emits a notice so multi-device clients see the same strip.
func (m *Manager) Diff(ctx context.Context, id, messageID, deviceID string) (string, error) {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return "", err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return "", err
	}
	ds, ok := sess.(provider.DiffSession)
	if !ok {
		return "", fmt.Errorf("session %q does not support diff", id)
	}
	summary, err := ds.Diff(ctx, messageID)
	if err != nil {
		return "", err
	}
	if summary != "" {
		m.emitNotice(id, summary)
	}
	return summary, nil
}

// Close closes and removes a live session; persistence is updated to disconnected
// unless purge is true (hard delete from disk).
func (m *Manager) Close(ctx context.Context, id, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	return m.close(ctx, id, false)
}

// Delete closes a live session if any and removes disk record.
func (m *Manager) Delete(ctx context.Context, id, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	return m.close(ctx, id, true)
}

func (m *Manager) close(ctx context.Context, id string, purge bool) error {
	return m.closeMatching(ctx, id, nil, purge)
}

// closeMatching closes and removes a session. When expect is non-nil the entry
// is only removed if it still holds that exact session, making the removal
// atomic with the identity check (auto-close vs. close-and-replace races).
func (m *Manager) closeMatching(ctx context.Context, id string, expect provider.Session, purge bool) error {
	m.mu.Lock()
	e, ok := m.sessions[id]
	if ok && expect != nil && e.sess != expect {
		// Replaced while we decided to close: the new session is not ours.
		m.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	var histSnap []event.Event
	if ok {
		// Snapshot history before dropping the entry so close can persist it
		// without holding m.mu across disk I/O (Phase D).
		if !purge && len(e.history) > 0 {
			histSnap = make([]event.Event, len(e.history))
			copy(histSnap, e.history)
		}
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	// Cancel any pending debounced writes for this id — we flush or purge now.
	// Without clearing dirtyPersist a scheduled meta flush could re-create a
	// just-deleted record up to persistDebounce later.
	m.clearHistoryDirty(id)
	m.clearPersistDirty(id)

	if ok {
		e.cancel()
		var closeErr error
		if purge {
			// Hard delete: purge provider-side durable state when supported
			// (e.g. OpenCode HTTP engine session), then local close.
			if ps, ok := e.sess.(provider.PurgeSession); ok {
				closeErr = ps.Purge(ctx)
			} else {
				closeErr = e.sess.Close(ctx)
			}
		} else {
			closeErr = e.sess.Close(ctx)
		}
		if closeErr != nil {
			m.log.Warn("provider session close failed",
				slog.String("session_id", id),
				slog.Bool("purge", purge),
				slog.String("err", closeErr.Error()),
			)
		}
		meta := e.meta
		meta.Status = StatusDisconnected
		meta.Live = false
		var err error
		if purge {
			if m.store != nil {
				// The live session is gone either way, but a surviving disk
				// record is not a success — report it.
				err = m.store.Delete(id)
			}
		} else {
			// Durable transcript first, then meta — a restart must see history
			// for listed non-live rows (Phase D).
			if m.store != nil {
				if werr := m.store.SaveHistory(id, histSnap); werr != nil {
					m.log.Warn("persist session history failed",
						slog.String("session_id", id),
						slog.String("err", werr.Error()),
					)
				}
			}
			m.persistNow(meta)
		}
		m.log.Info("session closed", slog.String("session_id", id), slog.Bool("purge", purge))
		// Prefer disk errors to the client; provider close already logged.
		return err
	}

	if purge && m.store != nil {
		return m.store.Delete(id)
	}
	return fmt.Errorf("%w: %q", ErrNotLive, id)
}

// CloseAll closes every live session (daemon shutdown; bypasses owner checks).
// It marks the manager as shutting down so concurrent Create calls fail with
// ErrShuttingDown, drains in-flight per-id create locks, then closes sessions.
func (m *Manager) CloseAll(ctx context.Context) {
	m.runCancel()
	m.mu.Lock()
	m.shuttingDown = true
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	// Ensure any debounced status / transcript writes land before tear-down.
	m.FlushPersist()
	m.FlushHistory()

	// Wait for Creates that already hold a createLock (and may be inside
	// provider Start) to finish their unlock path, so we do not race insert.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m.createMu.Lock()
		n := len(m.createLocks)
		m.createMu.Unlock()
		if n == 0 {
			break
		}
		select {
		case <-ctx.Done():
			// Still close whatever is registered; residual Creates will fail
			// the post-Start shuttingDown check and tear down their process.
			goto closeSessions
		case <-time.After(10 * time.Millisecond):
		}
	}

closeSessions:
	for _, id := range ids {
		_ = m.close(ctx, id, false)
	}
	// Anything that slipped in between the snapshot and the drain (should be
	// none once shuttingDown is set and createLocks empty) still gets closed.
	m.mu.Lock()
	extra := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		extra = append(extra, id)
	}
	m.mu.Unlock()
	for _, id := range extra {
		_ = m.close(ctx, id, false)
	}
}

// persist schedules a debounced disk write for a live session's meta (status
// churn). The current in-memory meta is re-read at flush time, so callers pass
// only the id — a stale snapshot can never overwrite a newer immediate write.
func (m *Manager) persist(id string) {
	if m.store == nil {
		return
	}
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	if m.dirtyPersist == nil {
		m.dirtyPersist = make(map[string]struct{})
	}
	m.dirtyPersist[id] = struct{}{}
	if m.persistTimer != nil {
		m.persistTimer.Reset(persistDebounce)
		return
	}
	m.persistTimer = time.AfterFunc(persistDebounce, m.FlushPersist)
}

// persistNow writes meta immediately and cancels any pending debounced write
// for the same id (create / claim / close / disconnect).
func (m *Manager) persistNow(meta Meta) {
	if m.store == nil {
		return
	}
	m.persistMu.Lock()
	delete(m.dirtyPersist, meta.ID)
	m.persistMu.Unlock()
	m.writePersist(meta)
}

// clearPersistDirty cancels a pending debounced meta write for id. Called on
// close/delete so a scheduled flush cannot resurrect a removed session's record.
func (m *Manager) clearPersistDirty(id string) {
	m.persistMu.Lock()
	delete(m.dirtyPersist, id)
	m.persistMu.Unlock()
}

// FlushPersist writes all pending debounced session records. Safe to call
// repeatedly; used on daemon shutdown and CloseAll. Each id's meta is re-read
// from the live map under m.mu, and ids whose session is gone are dropped, so
// the debounced path never reverts a newer write nor recreates a deleted row.
func (m *Manager) FlushPersist() {
	m.persistMu.Lock()
	if m.persistTimer != nil {
		m.persistTimer.Stop()
		m.persistTimer = nil
	}
	ids := make([]string, 0, len(m.dirtyPersist))
	for id := range m.dirtyPersist {
		ids = append(ids, id)
	}
	m.dirtyPersist = make(map[string]struct{})
	m.persistMu.Unlock()
	for _, id := range ids {
		m.mu.RLock()
		e, ok := m.sessions[id]
		var meta Meta
		if ok {
			meta = e.meta
		}
		m.mu.RUnlock()
		if !ok {
			continue
		}
		m.writePersist(meta)
	}
	m.persistMu.Lock()
	if len(m.dirtyPersist) > 0 && m.persistTimer == nil {
		m.persistTimer = time.AfterFunc(persistDebounce, m.FlushPersist)
	}
	m.persistMu.Unlock()
}

func (m *Manager) writePersist(meta Meta) {
	if m.store == nil {
		return
	}
	err := m.store.Save(Record{
		ID:             meta.ID,
		Provider:       meta.Provider,
		Name:           meta.Name,
		Model:          meta.Model,
		CWD:            meta.CWD,
		AgentSessionID: meta.AgentSessionID,
		OwnerDeviceID:  meta.OwnerDeviceID,
		CreatedAt:      meta.CreatedAt,
		Status:         meta.Status,
	})
	if err != nil {
		// Persistence is best-effort for the live path, but a failing disk must
		// not be invisible.
		m.log.Warn("persist session failed",
			slog.String("session_id", meta.ID),
			slog.String("err", err.Error()),
		)
	}
}

// scheduleHistoryPersist marks a session's transcript dirty and starts/resets
// the debounce timer. Close paths flush immediately instead.
func (m *Manager) scheduleHistoryPersist(id string) {
	if m.store == nil || id == "" {
		return
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	if m.dirtyHistory == nil {
		m.dirtyHistory = make(map[string]struct{})
	}
	m.dirtyHistory[id] = struct{}{}
	if m.historyTimer != nil {
		m.historyTimer.Reset(historyPersistDebounce)
		return
	}
	m.historyTimer = time.AfterFunc(historyPersistDebounce, m.FlushHistory)
}

func (m *Manager) clearHistoryDirty(id string) {
	m.historyMu.Lock()
	delete(m.dirtyHistory, id)
	m.historyMu.Unlock()
}

// FlushHistory writes dirty durable transcripts. Safe to call repeatedly;
// used on CloseAll and tests.
func (m *Manager) FlushHistory() {
	if m.store == nil {
		return
	}
	m.historyMu.Lock()
	if m.historyTimer != nil {
		m.historyTimer.Stop()
		m.historyTimer = nil
	}
	ids := make([]string, 0, len(m.dirtyHistory))
	for id := range m.dirtyHistory {
		ids = append(ids, id)
	}
	m.dirtyHistory = make(map[string]struct{})
	m.historyMu.Unlock()

	for _, id := range ids {
		m.mu.RLock()
		e, ok := m.sessions[id]
		var snap []event.Event
		if ok && !e.dead {
			snap = make([]event.Event, len(e.history))
			copy(snap, e.history)
		}
		dead := ok && e.dead
		m.mu.RUnlock()
		// A dead-but-not-yet-removed entry has a nil snapshot; writing it would
		// clobber the full transcript the close path is about to flush. Skip it
		// (and unknown ids) — the close path owns the final durable write.
		if !ok || dead {
			continue
		}
		if err := m.store.SaveHistory(id, snap); err != nil {
			m.log.Warn("persist session history failed",
				slog.String("session_id", id),
				slog.String("err", err.Error()),
			)
		}
	}
	m.historyMu.Lock()
	if len(m.dirtyHistory) > 0 && m.historyTimer == nil {
		m.historyTimer = time.AfterFunc(historyPersistDebounce, m.FlushHistory)
	}
	m.historyMu.Unlock()
}
