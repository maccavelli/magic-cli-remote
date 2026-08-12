// Package session manages live agent sessions and event fan-out.
package session

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
)

// StatusDisconnected is the session status reported once the backing provider
// process is gone. Sessions in this state are no longer live.
const StatusDisconnected = "disconnected"

// historyBufferCap bounds the per-session replay ring buffer. Aligned with the
// mobile client's kMaxTranscriptItems (800) so cold reopen can rebuild a full
// phone-side transcript (MADR 0018 E4). Oldest events drop.
const historyBufferCap = 800

// HistoryRingCap exposes the ring size for the v2 capability block
// (MADR 0068 D1) so the advertised limit cannot drift from the enforced one.
const HistoryRingCap = historyBufferCap

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
	// ErrNotReleased is returned when Claim targets a session that still has
	// an owner — a device may only claim a session its owner has released
	// (MADR 0078 D3).
	ErrNotReleased = errors.New("session not released for handoff")
	// ErrPersist is returned when a security-critical durable write fails
	// (create owner stamp or first-touch claim). Callers must treat this as
	// failure — never widen ownership without a durable record (MADR 0056 H-4).
	ErrPersist = errors.New("session persist failed")
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
	Model string `json:"model,omitempty"`
	// ThinkingLevel is the session's reasoning/thinking effort override.
	// Empty means the provider default. Set at create and by /thinking.
	ThinkingLevel       string `json:"thinking_level,omitempty"`
	ModeID              string `json:"mode_id,omitempty"`
	CollaborationModeID string `json:"collaboration_mode_id,omitempty"`
	ServiceTier         string `json:"service_tier,omitempty"`
	Personality         string `json:"personality,omitempty"`
	CWD                 string `json:"cwd,omitempty"`
	AgentSessionID      string `json:"agent_session_id,omitempty"`
	// OwnerDeviceID is the paired device that created (or claimed) the session.
	// Empty means legacy/unowned — visible to all devices until claimed (R4=B).
	OwnerDeviceID string `json:"owner_device_id,omitempty"`
	// PendingHandoffTo scopes a released session (OwnerDeviceID == "") to a
	// single target device during a handoff (MADR 0078 D2): only that device
	// sees and may claim it. Empty during a release means an *open* release —
	// any paired device may claim, exactly like a legacy unowned session.
	// Always empty once the session has an owner (cleared on claim).
	PendingHandoffTo string `json:"pending_handoff_to,omitempty"`
	// HandoffNonce is the per-transfer id minted at release, so the claim
	// receipt can share the release receipt's subject name and an auditor can
	// link the two halves across the devices' separate chains (MADR 0078 D4).
	// Cleared on claim; only meaningful while a release is pending.
	HandoffNonce string    `json:"handoff_nonce,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Status       string    `json:"status"`
	Live         bool      `json:"live"`
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
	// pending asks are authoritative while the live entry exists. History is
	// bounded, so it cannot reliably answer whether an old request remains open.
	pendingPermissions map[string]event.Event
	pendingQuestions   map[string]event.Event
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
	agentModes          []event.SessionMode
	currentModeID       string
	collabModes         []event.CollaborationMode
	currentCollabModeID string
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
	// epoch identifies this seq-counter lineage (MADR 0068 P3). Minted
	// fresh whenever the previous run did not shut down cleanly — seq may
	// have regressed by up to persistDebounce of events — and kept across
	// clean restarts. Empty when the manager has no store (tests).
	epoch string

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
	historyMu         sync.Mutex
	dirtyHistory      map[string]struct{}
	historyDirtySince map[string]time.Time
	historyTimer      *time.Timer

	// runCtx is cancelled by CloseAll so session pumps deriving from it also
	// exit when the manager shuts down.
	runCtx    context.Context
	runCancel context.CancelFunc

	// receiptsMu guards receipts (MADR 0077 P7). Set once, after construction,
	// via SetReceiptSupport — daemon.go wires it in only when ws.Server (the
	// Transport implementation) exists, breaking what would otherwise be a
	// construction-order cycle (mirrors the existing onEvent/eventHub bridge).
	receiptsMu sync.RWMutex
	receipts   ReceiptSupport
}

// ReceiptTransport asks a specific device's live connection to sign a
// Statement and waits for its signed reply, or returns an error/timeout
// (MADR 0077 D8). Implemented by *ws.Server; the session package does not
// import ws — same dependency-inversion shape as the existing
// onEvent/eventHub bridge in internal/daemon.
//
// correlationID identifies the pending request so the reply can be matched
// back (the waiter is keyed by it and bound to deviceID — 0077 F2). It is a
// permission id for a permission-decision receipt and a handoff nonce for a
// session-handoff receipt (MADR 0078 D5); the transport treats it as an
// opaque string either way.
type ReceiptTransport interface {
	RequestReceipt(ctx context.Context, deviceID, sessionID, correlationID string, statement json.RawMessage) (jwsCompact string, err error)
}

// ReceiptSupport bundles what the receipt orchestration hook needs (MADR
// 0077 P7). The zero value (Transport nil) means receipts are off — checked
// before any of this phase's code runs, so an operator who never configures
// receipts.enabled pays no cost and the hook is a no-op even if the field is
// never set at all (tests, `fake`-only setups).
type ReceiptSupport struct {
	Config    config.ReceiptsConfig
	Store     *receipt.Store
	AuthStore *auth.Store
	// DaemonKey signs the receipt-unavailable marker (D8) — the daemon's own
	// TLS serving private key, ECDSA P-256 (internal/certs/certs.go:239).
	DaemonKey *ecdsa.PrivateKey
	Transport ReceiptTransport
}

// SetReceiptSupport wires receipt orchestration in after construction (see
// ReceiptSupport's doc comment for why). Safe to call at most once, before
// the manager starts handling permission decisions; safe to never call.
func (m *Manager) SetReceiptSupport(rs ReceiptSupport) {
	m.receiptsMu.Lock()
	m.receipts = rs
	m.receiptsMu.Unlock()
}

// ReceiptsEnabled reports whether the daemon is keeping signed receipts —
// the phone shows its receipt UI only when this is true (MADR 0078 D7's
// capability bit).
func (m *Manager) ReceiptsEnabled() bool {
	m.receiptsMu.RLock()
	defer m.receiptsMu.RUnlock()
	return m.receipts.Store != nil && m.receipts.Config.Enabled
}

// ReceiptEntry is one line of a device's receipt chain, as served to the
// phone (MADR 0078 D8): the raw JWS compact string (the phone re-verifies the
// signature itself — D9) and the decoded Statement (so the phone need not
// re-implement the Statement schema just to display it).
type ReceiptEntry struct {
	JWS       string             `json:"jws"`
	Statement *receipt.Statement `json:"statement,omitempty"`
}

// ReceiptEntriesFor returns deviceID's own receipt chain, newest first
// (MADR 0078 D8). A device may only read its own chain — the caller (the WS
// layer) passes the connection's authenticated device id, never another's.
// Returns an empty slice when receipts are off or the device has no chain.
func (m *Manager) ReceiptEntriesFor(deviceID string) ([]ReceiptEntry, error) {
	m.receiptsMu.RLock()
	store := m.receipts.Store
	enabled := m.receipts.Config.Enabled
	m.receiptsMu.RUnlock()
	if store == nil || !enabled || deviceID == "" {
		return nil, nil
	}
	lines, err := store.Lines(deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]ReceiptEntry, 0, len(lines))
	// Newest first: the phone shows the most recent decision at the top.
	for i := len(lines) - 1; i >= 0; i-- {
		e := ReceiptEntry{JWS: lines[i]}
		if payload, derr := receipt.DecodePayloadUnverified(lines[i]); derr == nil {
			var stmt receipt.Statement
			if json.Unmarshal(payload, &stmt) == nil {
				e.Statement = &stmt
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// persistDebounce batches status-only meta writes under chatty agents.
const persistDebounce = 2 * time.Second

// historyPersistDebounce batches transcript snapshots under streaming agents.
const historyPersistDebounce = 1 * time.Second

// historyMaxLatency is the longest a dirty transcript may wait for flush under
// a continuous stream (MADR 0056 M-3 / Phase 5). Debounce may reset, but this
// bound still forces a write.
const historyMaxLatency = 5 * time.Second

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
	m := &Manager{
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
	if store != nil {
		// Seq-lineage epoch (MADR 0068 P3): keep it across clean restarts,
		// mint fresh after an unclean one. The dirty marker is written now
		// and cleared only by CloseAll's final flush, so a kill -9 leaves
		// it dirty — which is exactly the signal.
		epoch, clean, ok := store.LoadEpoch()
		if !ok || !clean {
			epoch = newEpoch()
		}
		m.epoch = epoch
		if err := store.SaveEpoch(epoch, false); err != nil {
			m.log.Warn("epoch persist failed", slog.String("err", err.Error()))
		}
	}
	return m
}

// newEpoch mints an opaque seq-lineage identifier.
func newEpoch() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a time-derived value; uniqueness across restarts is
		// what matters, not unpredictability.
		return fmt.Sprintf("t%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Epoch identifies this manager's seq lineage; empty without a store.
// Clients that see it change must drop cached per-session seqs
// (MADR 0068 P3 / protocol-v2).
func (m *Manager) Epoch() string { return m.epoch }

// SeqBounds reports the oldest retained and newest event seq for a session
// (MADR 0068 P3): first==0 means nothing retained. A client whose cached
// seq is below first knows the ring truncated past it and must refetch in
// full rather than trust a silently filtered page.
func (m *Manager) SeqBounds(id string) (first, latest uint64) {
	m.mu.RLock()
	if e, ok := m.sessions[id]; ok && len(e.history) > 0 {
		first, latest = e.history[0].Seq, e.history[len(e.history)-1].Seq
		m.mu.RUnlock()
		return first, latest
	}
	m.mu.RUnlock()
	if m.store != nil {
		if h := m.store.LoadHistory(id); len(h) > 0 {
			return h[0].Seq, h[len(h)-1].Seq
		}
	}
	return 0, 0
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
	if m.store != nil && opts.LocalSessionID != "" {
		if rec, err := m.store.Get(opts.LocalSessionID); err == nil {
			if !replacing && ownerDeviceID != "" && rec.OwnerDeviceID != "" && rec.OwnerDeviceID != ownerDeviceID {
				return Meta{}, fmt.Errorf("%w: %q", ErrForbidden, opts.LocalSessionID)
			}
			if opts.ModeID == "" {
				opts.ModeID = rec.ModeID
			}
			if opts.CollaborationModeID == "" {
				opts.CollaborationModeID = rec.CollaborationModeID
			}
			if opts.ServiceTier == "" {
				opts.ServiceTier = rec.ServiceTier
			}
			if opts.Personality == "" {
				opts.Personality = rec.Personality
			}
			if opts.ThinkingLevel == "" {
				opts.ThinkingLevel = rec.ThinkingLevel
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
	if id := strings.TrimSpace(opts.ModeID); id != "" {
		if ms, ok := sess.(provider.ModeSession); ok {
			if err := ms.SetMode(ctx, id); err != nil {
				m.log.Debug("restore persisted mode failed",
					slog.String("session_id", sess.ID()),
					slog.String("mode_id", id),
					slog.String("err", err.Error()),
				)
			}
		}
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
		ID:                  sess.ID(),
		Provider:            providerID,
		Name:                opts.Name,
		Model:               opts.Model,
		ThinkingLevel:       strings.TrimSpace(opts.ThinkingLevel),
		ModeID:              strings.TrimSpace(opts.ModeID),
		CollaborationModeID: strings.TrimSpace(opts.CollaborationModeID),
		ServiceTier:         strings.TrimSpace(opts.ServiceTier),
		Personality:         strings.TrimSpace(opts.Personality),
		CWD:                 cwd,
		AgentSessionID:      sess.AgentSessionID(),
		OwnerDeviceID:       ownerDeviceID,
		CreatedAt:           time.Now().UTC(),
		Status:              "idle",
		Live:                true,
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
	// Owner durability is security-critical: do not report success if Save fails.
	if err := m.persistNow(meta); err != nil {
		m.log.Error("session create persist failed; rolling back",
			slog.String("session_id", meta.ID),
			slog.String("err", err.Error()),
		)
		// Bypass owner checks: this device just created it and persist failed.
		_ = m.close(ctx, meta.ID, false)
		return Meta{}, fmt.Errorf("%w: %v", ErrPersist, err)
	}

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
			// Captured at TypePermissionResolved from the matching TypePermission
			// entry (still in e.pendingPermissions at that point) for the receipt
			// orchestration hook below — the resolved event alone carries no
			// tool_name/detail (MADR 0077 P7).
			var receiptReq event.Event
			receiptReqOK := false
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
						e.meta.ModeID = ev.CurrentModeID
						meta := e.meta
						persistMeta = &meta
					}
				}
				if ev.Type == event.TypeCollaboration {
					if len(ev.CollaborationModes) > 0 {
						e.collabModes = ev.CollaborationModes
						reresolve = true
					}
					if ev.CurrentCollaborationModeID != "" {
						e.currentCollabModeID = ev.CurrentCollaborationModeID
						e.meta.CollaborationModeID = ev.CurrentCollaborationModeID
						meta := e.meta
						persistMeta = &meta
					}
				}
				if ev.Type == event.TypeSessionTitle {
					title := strings.TrimSpace(ev.Title)
					if title != "" && title != e.meta.Name {
						e.meta.Name = title
						meta := e.meta
						persistMeta = &meta
					}
				}
				if ev.Type == event.TypeUsage && ev.Usage != nil {
					// The first report is also what makes /context possible, so
					// the advertised list needs a second look.
					reresolve = e.lastUsage == nil
					e.lastUsage = ev.Usage
				}
				e.appendHistoryLocked(&ev)
				switch ev.Type {
				case event.TypePermission:
					if ev.PermissionID != "" {
						if e.pendingPermissions == nil {
							e.pendingPermissions = make(map[string]event.Event)
						}
						e.pendingPermissions[ev.PermissionID] = ev
					}
				case event.TypeQuestion:
					if ev.QuestionID != "" {
						if e.pendingQuestions == nil {
							e.pendingQuestions = make(map[string]event.Event)
						}
						e.pendingQuestions[ev.QuestionID] = ev
					}
				case event.TypePermissionResolved:
					receiptReq, receiptReqOK = e.pendingPermissions[ev.PermissionID]
					delete(e.pendingPermissions, ev.PermissionID)
				case event.TypeQuestionResolved:
					delete(e.pendingQuestions, ev.QuestionID)
				case event.TypeTurnComplete, event.TypeError:
					clear(e.pendingPermissions)
					clear(e.pendingQuestions)
				}
			}
			histID := ""
			if mine {
				histID = sess.ID()
			}
			m.mu.Unlock()
			if mine && !ev.Replay && ev.Type == event.TypePermissionResolved && receiptReqOK {
				// Outside the lock and never awaited here — D8's non-blocking
				// requirement. A no-op instantly if receipts aren't configured
				// (checked first thing inside). !ev.Replay is defense in depth:
				// a session/load replay re-emits the prior conversation's
				// permission events through this same pump, and a replayed
				// resolution must never mint a second receipt for a decision
				// already receipted in its first life. (Today replayed events
				// are re-translated from the agent's own transcript and carry
				// no DeviceID, so maybeCreateReceipt's DeviceID guard would
				// also skip them — but that's incidental, not contractual.)
				m.maybeCreateReceipt(sess.ID(), ev, receiptReq)
			}
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
				// chatter is debounced (Phase 4.3). Advisory: log-only on fail.
				if autoClose || persistMeta.Status == StatusDisconnected {
					_ = m.persistNow(*persistMeta)
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

// PendingAsks returns a deterministic, owner-scoped copy of every unresolved
// live permission/question request. Entries disappear naturally on close or
// replacement because they live with the session entry.
func (m *Manager) PendingAsks(deviceID string) []event.Event {
	m.mu.RLock()
	out := make([]event.Event, 0)
	for id, e := range m.sessions {
		if e.dead || (e.meta.OwnerDeviceID != "" && e.meta.OwnerDeviceID != deviceID) {
			continue
		}
		for _, ev := range e.pendingPermissions {
			ev.SessionID = id
			out = append(out, ev)
		}
		for _, ev := range e.pendingQuestions {
			ev.SessionID = id
			out = append(out, ev)
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].SessionID != out[j].SessionID {
			return out[i].SessionID < out[j].SessionID
		}
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		return askID(out[i]) < askID(out[j])
	})
	return out
}

func askID(ev event.Event) string {
	if ev.PermissionID != "" {
		return ev.PermissionID
	}
	return ev.QuestionID
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

// visibleTo reports whether deviceID may see a session with the given owner
// and pending-handoff target. An owned session is visible only to its owner.
// An unowned session is visible to every device (legacy / open release),
// UNLESS a handoff target is named (MADR 0078 D2), in which case only that
// target sees it — narrowing the open-release default to a directed one.
func visibleTo(ownerDeviceID, pendingHandoffTo, deviceID string) bool {
	if ownerDeviceID != "" {
		return ownerDeviceID == deviceID
	}
	if pendingHandoffTo != "" {
		return pendingHandoffTo == deviceID
	}
	return true
}

// Authorize checks that deviceID may access sessionID.
// When claim is true and the session has an empty (legacy) owner, the device
// is stamped as owner (first-touch claim).
func (m *Manager) Authorize(sessionID, deviceID string, claim bool) error {
	m.mu.Lock()
	if e, ok := m.sessions[sessionID]; ok && !e.dead {
		if e.meta.OwnerDeviceID == "" {
			if claim && deviceID != "" {
				// Persist first; only stamp in-memory owner after Save succeeds
				// so a disk failure cannot leave a live claim that restart loses
				// (MADR 0056 H-4).
				claimMeta := e.meta
				claimMeta.OwnerDeviceID = deviceID
				m.mu.Unlock()
				if err := m.persistNow(claimMeta); err != nil {
					return fmt.Errorf("%w: %v", ErrPersist, err)
				}
				m.mu.Lock()
				if e2, ok := m.sessions[sessionID]; ok && !e2.dead && e2.meta.OwnerDeviceID == "" {
					e2.meta.OwnerDeviceID = deviceID
				}
				m.mu.Unlock()
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
				return fmt.Errorf("%w: %v", ErrPersist, err)
			}
		}
		return nil
	}
	if rec.OwnerDeviceID != deviceID {
		return fmt.Errorf("%w: %q", ErrForbidden, sessionID)
	}
	return nil
}

// Release hands a session off for a later Claim (MADR 0078 D1/D2): the owner
// clears its ownership so the session returns to the unowned state, optionally
// scoped to a single target device via toDeviceID (empty = open release, any
// paired device may claim). Only the current owner may release. Persists
// before mutating in-memory state (H-4): a disk failure must not leave a
// live release the next restart loses.
//
// Returns the released Meta (OwnerDeviceID cleared, PendingHandoffTo set) so
// the caller — the WS layer — can trigger a handoff receipt and tell the
// releasing device's UI the session has left its list.
func (m *Manager) Release(sessionID, ownerDeviceID, toDeviceID string) (Meta, error) {
	// Serialize per session id against concurrent create/release/claim, so
	// the persist-then-stamp window below cannot interleave with another
	// ownership mutation for the same session (same lock Create uses).
	unlock := m.lockCreate(sessionID)
	defer unlock()

	m.mu.Lock()
	e, ok := m.sessions[sessionID]
	if !ok || e.dead {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, sessionID)
	}
	// Only the owner may release. An unowned session has no owner to hand it
	// off; a different device is forbidden.
	if e.meta.OwnerDeviceID != ownerDeviceID || ownerDeviceID == "" {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrForbidden, sessionID)
	}
	nonce := uuid.NewString()
	released := e.meta
	released.OwnerDeviceID = ""
	released.PendingHandoffTo = toDeviceID
	released.HandoffNonce = nonce
	m.mu.Unlock()

	if err := m.persistNow(released); err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrPersist, err)
	}
	m.mu.Lock()
	if e2, ok := m.sessions[sessionID]; ok && !e2.dead {
		e2.meta.OwnerDeviceID = ""
		e2.meta.PendingHandoffTo = toDeviceID
		e2.meta.HandoffNonce = nonce
	}
	m.mu.Unlock()

	// The releasing device signs "I gave S away" into its own chain (D4).
	m.maybeCreateHandoffReceipt(sessionID, ownerDeviceID, ownerDeviceID, toDeviceID, nonce, true)
	return released, nil
}

// Claim takes ownership of a released session (MADR 0078 D1/D3): the mirror
// of Release. Rejects a session that still has an owner (ErrNotReleased) and
// a claim by anyone other than a named handoff target (ErrForbidden). On
// success the claimer becomes owner, PendingHandoffTo is cleared, and the
// claimer's Meta is returned. Single-winner under the manager lock: two
// devices racing to claim an open release cannot both succeed, since the
// persist-then-stamp ordering re-checks ownership was still empty.
func (m *Manager) Claim(sessionID, deviceID string) (Meta, error) {
	if deviceID == "" {
		return Meta{}, fmt.Errorf("%w: %q", ErrForbidden, sessionID)
	}
	// Per-session serialization (as in Release): two devices racing to claim
	// an open release are ordered here, so exactly one sees OwnerDeviceID
	// empty and the other observes the winner's stamp — single-winner
	// without holding m.mu across the disk write.
	unlock := m.lockCreate(sessionID)
	defer unlock()

	m.mu.Lock()
	e, ok := m.sessions[sessionID]
	if !ok || e.dead {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, sessionID)
	}
	if e.meta.OwnerDeviceID != "" {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrNotReleased, sessionID)
	}
	if e.meta.PendingHandoffTo != "" && e.meta.PendingHandoffTo != deviceID {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrForbidden, sessionID)
	}
	nonce := e.meta.HandoffNonce
	claimed := e.meta
	claimed.OwnerDeviceID = deviceID
	claimed.PendingHandoffTo = ""
	claimed.HandoffNonce = ""
	m.mu.Unlock()

	if err := m.persistNow(claimed); err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrPersist, err)
	}
	m.mu.Lock()
	e2, ok := m.sessions[sessionID]
	if !ok || e2.dead {
		m.mu.Unlock()
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, sessionID)
	}
	e2.meta.OwnerDeviceID = deviceID
	e2.meta.PendingHandoffTo = ""
	e2.meta.HandoffNonce = ""
	out := e2.meta
	m.mu.Unlock()

	// The claiming device signs "I took S" into its own chain (D4), sharing
	// the release's nonce so the two halves link. fromDeviceID is left empty:
	// the released Meta no longer names the releaser, and the shared nonce
	// subject already links to the release Statement, which does record it.
	if nonce != "" {
		m.maybeCreateHandoffReceipt(sessionID, deviceID, "", "", nonce, false)
	}
	return out, nil
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
	// Soft byte budget: shrink end until the marshalled page payload fits
	// (MADR 0056 M-1). Always allow at least one event.
	for end > start {
		page := ring[start:end]
		b, err := json.Marshal(page)
		if err != nil {
			break
		}
		if len(b) <= historyMaxResponseBytes || end == start+1 {
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

// ListSnapshot is an owner-filtered session list with completeness metadata
// (MADR 0056 H-6). Complete is true only when the durable store enumeration
// succeeded without skipping corrupt rows.
type ListSnapshot struct {
	Sessions []Meta
	Complete bool
	Degraded bool
	Skipped  int
}

// List returns all live sessions merged with persisted records (no owner filter).
// Prefer ListFor in multi-device paths.
func (m *Manager) List() []Meta {
	snap, _ := m.ListSnapshot("")
	return snap.Sessions
}

// ListFor returns sessions visible to deviceID (owned by it, or legacy empty owner).
// When deviceID is empty, returns all sessions (test / unrestricted paths).
// Root store errors are swallowed here for backward-compatible internal callers;
// wire clients must use ListSnapshot.
func (m *Manager) ListFor(deviceID string) []Meta {
	snap, _ := m.ListSnapshot(deviceID)
	return snap.Sessions
}

// ListSnapshot returns sessions visible to deviceID plus complete/degraded flags.
// A root store enumeration error is returned and Complete is false.
func (m *Manager) ListSnapshot(deviceID string) (ListSnapshot, error) {
	m.mu.RLock()
	live := make(map[string]Meta, len(m.sessions))
	out := make([]Meta, 0, len(m.sessions))
	for _, e := range m.sessions {
		if e.dead {
			continue
		}
		meta := e.meta
		meta.Live = true
		if deviceID != "" && !visibleTo(meta.OwnerDeviceID, meta.PendingHandoffTo, deviceID) {
			continue
		}
		live[meta.ID] = meta
		out = append(out, meta)
	}
	m.mu.RUnlock()

	if m.store == nil {
		return ListSnapshot{Sessions: out, Complete: true}, nil
	}
	recs, skipped, err := m.store.List()
	if err != nil {
		// Live rows only; never present a partial disk merge as complete.
		return ListSnapshot{
			Sessions: out,
			Complete: false,
			Degraded: true,
		}, err
	}
	for _, rec := range recs {
		if _, ok := live[rec.ID]; ok {
			continue
		}
		if deviceID != "" && !visibleTo(rec.OwnerDeviceID, rec.PendingHandoffTo, deviceID) {
			continue
		}
		out = append(out, Meta{
			ID:                  rec.ID,
			Provider:            rec.Provider,
			Name:                rec.Name,
			Model:               rec.Model,
			ThinkingLevel:       rec.ThinkingLevel,
			ModeID:              rec.ModeID,
			CollaborationModeID: rec.CollaborationModeID,
			ServiceTier:         rec.ServiceTier,
			Personality:         rec.Personality,
			CWD:                 rec.CWD,
			AgentSessionID:      rec.AgentSessionID,
			OwnerDeviceID:       rec.OwnerDeviceID,
			PendingHandoffTo:    rec.PendingHandoffTo,
			HandoffNonce:        rec.HandoffNonce,
			CreatedAt:           rec.CreatedAt,
			Status:              rec.Status,
			Live:                false,
		})
	}
	complete := skipped == 0
	return ListSnapshot{
		Sessions: out,
		Complete: complete,
		Degraded: skipped > 0,
		Skipped:  skipped,
	}, nil
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
			handled, forward, err := m.runCanonical(ctx, id, deviceID, res, name, rest, attachments)
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

// SetCollaborationMode switches the independent Plan/Default axis.
func (m *Manager) SetCollaborationMode(ctx context.Context, id, modeID, deviceID string) error {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return err
	}
	return m.setCollaborationMode(ctx, id, modeID)
}

func (m *Manager) setCollaborationMode(ctx context.Context, id, modeID string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	cs, ok := sess.(provider.CollaborationModeSession)
	if !ok {
		return provider.ErrCollaborationUnsupported
	}
	return cs.SetCollaborationMode(ctx, modeID)
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
	_ = m.persistNow(meta)
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
	return ps.RespondPermission(ctx, permissionID, optionID, cancelled, deviceID)
}

// maybeCreateReceipt triggers the signed-receipt round trip for one resolved
// permission decision (MADR 0077 P7). Called from the event pump, never from
// RespondPermission's own call path — D8 requires this never delay it, and
// the resolved event alone carries no tool_name/detail to build a Statement
// from, so req (the matching TypePermission event, captured by the caller
// before it was evicted from pendingPermissions) supplies them.
//
// A cancelled resolution or one with no DeviceID (a sweep, a timeout — see
// event.Event's DeviceID doc) has no device decision to attest to and is
// skipped outright, before ShouldReceipt is even consulted.
func (m *Manager) maybeCreateReceipt(sessionID string, resolved, req event.Event) {
	m.receiptsMu.RLock()
	rs := m.receipts
	m.receiptsMu.RUnlock()
	if rs.Transport == nil || !rs.Config.Enabled {
		return
	}
	if resolved.Status != event.PermissionStatusResolved || resolved.DeviceID == "" {
		return
	}
	toolName, detail := req.ToolName, req.Text
	if !receipt.ShouldReceipt(rs.Config, toolName, detail) {
		return
	}
	go m.runReceiptRoundTrip(rs, sessionID, resolved.PermissionID, resolved.DeviceID, resolved.OptionID, toolName, detail)
}

// receiptRoundTripTimeout bounds the phone's signing round trip (MADR 0077
// D8), fully decoupled from any provider's PermissionTimeoutSeconds: by the
// time this runs the permission has already been resolved and returned to
// the caller: this timeout only bounds how long a receipt is worth waiting
// for, never anything user-visible.
const receiptRoundTripTimeout = 10 * time.Second

// runReceiptRoundTrip performs the daemon-constructs/phone-signs round trip
// (D2) and appends exactly one line to deviceID's chain: either the real
// device-signed receipt, or — on timeout or an invalid signature — a
// daemon-signed receipt-unavailable marker (D8), so a gap in the chain is
// never silently indistinguishable from tampering. Always runs in its own
// goroutine (see maybeCreateReceipt); logs and returns on any local error
// building/storing the Statement, since there is nothing further to escalate
// to — the permission decision itself already completed successfully.
func (m *Manager) runReceiptRoundTrip(rs ReceiptSupport, sessionID, permissionID, deviceID, optionID, toolName, detail string) {
	m.signReceipt(rs, deviceID, sessionID, permissionID,
		func(chainScope string, prev *string) (*receipt.Statement, error) {
			return receipt.BuildPermissionDecisionStatement(
				sessionID, permissionID, deviceID, optionID, toolName, detail,
				time.Now().UTC(), chainScope, prev,
			)
		})
}

// signReceipt runs the daemon-constructs/device-signs round trip for one
// receipt of any kind (MADR 0077 D2, generalized in 0078 D5): build the
// Statement (via buildStmt, which receives the device's chain scope and the
// previous-line hash so it can set chain.prev_sha256), ask the device to
// sign it (correlationID keys the reply), verify the signature AND that the
// signed payload equals the sent one, and append to the device's chain — or,
// on timeout/invalid signature, append a daemon-signed receipt-unavailable
// marker so the chain never gaps. Always runs in its own goroutine (callers
// spawn it); logs and returns on any local error.
func (m *Manager) signReceipt(rs ReceiptSupport, deviceID, sessionID, correlationID string, buildStmt func(chainScope string, prev *string) (*receipt.Statement, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), receiptRoundTripTimeout)
	defer cancel()

	// Archive the device's public key beside its chain first, best-effort:
	// the auth store is the ONLY other holder of this key, and a later
	// `pair revoke`/`pair prune` deletes it there — archiving at receipt
	// time is what keeps this chain verifiable for the rest of its life
	// (docs/receipts.md "Revoked devices"). Fetched once here and reused for
	// signature verification below.
	devicePub, devicePubErr := rs.AuthStore.PublicKeyFor(deviceID)
	if devicePubErr == nil {
		if spki, err := x509.MarshalPKIXPublicKey(devicePub); err != nil {
			m.log.Warn("receipt: marshal device key for archive failed",
				slog.String("device_id", deviceID), slog.String("err", err.Error()))
		} else if err := rs.Store.ArchiveKey(deviceID, spki); err != nil {
			m.log.Warn("receipt: archive device key failed",
				slog.String("device_id", deviceID), slog.String("err", err.Error()))
		}
	}

	chainScope := "device:" + deviceID
	lastHash, ok, err := rs.Store.LastHash(deviceID)
	if err != nil {
		m.log.Warn("receipt: read last chain hash failed",
			slog.String("device_id", deviceID), slog.String("err", err.Error()))
		return
	}
	var prevSHA256 *string
	if ok {
		prevSHA256 = &lastHash
	}

	stmt, err := buildStmt(chainScope, prevSHA256)
	if err != nil {
		m.log.Warn("receipt: build statement failed", slog.String("err", err.Error()))
		return
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		m.log.Warn("receipt: marshal statement failed", slog.String("err", err.Error()))
		return
	}

	reason := ""
	jws, err := rs.Transport.RequestReceipt(ctx, deviceID, sessionID, correlationID, payload)
	switch {
	case err != nil:
		reason = "timeout"
	default:
		if devicePubErr != nil {
			reason = "invalid_signature"
			break
		}
		signed, verr := receipt.VerifyES256Compact(devicePub, jws)
		if verr != nil {
			reason = "invalid_signature"
			break
		}
		// D2's other half: a valid signature over the WRONG content is still
		// a rejection. The device signs the statement the daemon constructed —
		// it must not be able to substitute its own and have the daemon
		// durably record that. Compared semantically, not byte-for-byte: the
		// Dart client decodes and re-encodes the statement JSON before signing
		// (its parser has no raw-bytes passthrough), which is
		// content-preserving but not guaranteed byte-preserving.
		if !jsonSemanticallyEqual(payload, signed) {
			m.log.Warn("receipt: device signed a different statement than the daemon sent",
				slog.String("device_id", deviceID), slog.String("correlation_id", correlationID))
			reason = "invalid_signature"
			break
		}
		if err := rs.Store.Append(deviceID, jws); err != nil {
			m.log.Warn("receipt: append failed",
				slog.String("device_id", deviceID), slog.String("err", err.Error()))
		}
		return
	}

	// Re-read the chain hash for the marker: a permission receipt racing on
	// the same device could have appended between our read above and here.
	markerPrev := prevSHA256
	if h, ok2, herr := rs.Store.LastHash(deviceID); herr == nil && ok2 {
		markerPrev = &h
	}
	unavailable, err := receipt.BuildReceiptUnavailableStatement(correlationID, deviceID, reason, chainScope, markerPrev)
	if err != nil {
		m.log.Warn("receipt: build unavailable statement failed", slog.String("err", err.Error()))
		return
	}
	upayload, err := json.Marshal(unavailable)
	if err != nil {
		m.log.Warn("receipt: marshal unavailable statement failed", slog.String("err", err.Error()))
		return
	}
	daemonJWS, err := receipt.SignES256Compact(rs.DaemonKey, upayload)
	if err != nil {
		m.log.Warn("receipt: sign unavailable marker failed", slog.String("err", err.Error()))
		return
	}
	if err := rs.Store.Append(deviceID, daemonJWS); err != nil {
		m.log.Warn("receipt: append unavailable marker failed",
			slog.String("device_id", deviceID), slog.String("err", err.Error()))
	}
}

// maybeCreateHandoffReceipt fires a background handoff receipt round trip
// (MADR 0078 D4) if receipts and handoff attestation are both enabled. The
// signer is the device performing the attested half: the releaser signs the
// release Statement (into its chain), the claimer signs the claim Statement
// (into its). release=true selects the release predicate. Non-blocking:
// callers (Release/Claim) have already completed the ownership change.
func (m *Manager) maybeCreateHandoffReceipt(sessionID, signerDeviceID, fromDeviceID, toDeviceID, nonce string, release bool) {
	m.receiptsMu.RLock()
	rs := m.receipts
	m.receiptsMu.RUnlock()
	if rs.Transport == nil || !rs.Config.Enabled || !rs.Config.Handoffs {
		return
	}
	now := time.Now().UTC()
	// The transport correlates the reply by an opaque id; the handoff subject
	// name is unique per transfer and per side, so it doubles as that id.
	correlationID := "handoff:" + nonce + ":"
	if release {
		correlationID += "release"
	} else {
		correlationID += "claim"
	}
	go m.signReceipt(rs, signerDeviceID, sessionID, correlationID,
		func(chainScope string, prev *string) (*receipt.Statement, error) {
			if release {
				return receipt.BuildHandoffReleaseStatement(
					sessionID, fromDeviceID, toDeviceID, nonce, now, chainScope, prev)
			}
			return receipt.BuildHandoffClaimStatement(
				sessionID, signerDeviceID, fromDeviceID, nonce, now, chainScope, prev)
		})
}

// jsonSemanticallyEqual reports whether a and b decode to the same JSON
// value — same object keys/values, same array elements in order — ignoring
// key order and whitespace. Undecodable input is never equal to anything.
func jsonSemanticallyEqual(a, b []byte) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
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
	deferGoal := false
	if gs, ok := sess.(provider.GoalSession); ok {
		if g, present := gs.CurrentGoal(); provider.GoalIsActive(g, present) {
			deferGoal = true
		}
	}
	res, err := fs.Fork(ctx, provider.ForkOptions{LastTurnID: messageID, DeferGoalContinuation: deferGoal})
	if err != nil && deferGoal && strings.Contains(err.Error(), "experimental") {
		return Meta{}, fmt.Errorf("pause or clear the active goal before forking")
	}
	if err != nil {
		return Meta{}, err
	}
	if res.ForkedFromID != "" && res.ForkedFromID != sess.AgentSessionID() {
		return Meta{}, fmt.Errorf("fork source mismatch: got %q want %q", res.ForkedFromID, sess.AgentSessionID())
	}
	newAgentID := res.AgentSessionID
	name := meta.Name
	if name != "" {
		name = name + " (fork)"
	} else {
		name = "fork"
	}
	return m.Create(ctx, meta.Provider, provider.StartOptions{
		Name:                name,
		CWD:                 meta.CWD,
		Model:               meta.Model,
		ThinkingLevel:       meta.ThinkingLevel,
		ModeID:              meta.ModeID,
		CollaborationModeID: meta.CollaborationModeID,
		ServiceTier:         meta.ServiceTier,
		Personality:         meta.Personality,
		AgentSessionID:      newAgentID,
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
func (m *Manager) Diff(ctx context.Context, id, messageID, deviceID string) (provider.DiffResult, error) {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return provider.DiffResult{}, err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return provider.DiffResult{}, err
	}
	ds, ok := sess.(provider.DiffSession)
	if !ok {
		return provider.DiffResult{}, fmt.Errorf("session %q does not support diff", id)
	}
	res, err := ds.Diff(ctx, messageID)
	if err != nil {
		return provider.DiffResult{}, err
	}
	if res.Summary != "" {
		m.emitNotice(id, res.Summary)
	}
	return res, nil
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
			_ = m.persistNow(meta)
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
	// Everything above flushed; mark the epoch clean so the next boot keeps
	// the seq lineage (MADR 0068 P3). Written last: a crash anywhere before
	// this line correctly leaves the marker dirty.
	if m.store != nil && m.epoch != "" {
		if err := m.store.SaveEpoch(m.epoch, true); err != nil {
			m.log.Warn("epoch clean-mark failed", slog.String("err", err.Error()))
		}
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
// for the same id (create / claim / close / disconnect). Returns a non-nil
// error when the durable write fails (callers that treat ownership as a
// security boundary must fail closed).
func (m *Manager) persistNow(meta Meta) error {
	if m.store == nil {
		return nil
	}
	m.persistMu.Lock()
	delete(m.dirtyPersist, meta.ID)
	m.persistMu.Unlock()
	return m.writePersist(meta)
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
		_ = m.writePersist(meta) // advisory status flush: log inside writePersist
	}
	m.persistMu.Lock()
	if len(m.dirtyPersist) > 0 && m.persistTimer == nil {
		m.persistTimer = time.AfterFunc(persistDebounce, m.FlushPersist)
	}
	m.persistMu.Unlock()
}

func (m *Manager) writePersist(meta Meta) error {
	if m.store == nil {
		return nil
	}
	err := m.store.Save(Record{
		ID:                  meta.ID,
		Provider:            meta.Provider,
		Name:                meta.Name,
		Model:               meta.Model,
		ThinkingLevel:       meta.ThinkingLevel,
		ModeID:              meta.ModeID,
		CollaborationModeID: meta.CollaborationModeID,
		ServiceTier:         meta.ServiceTier,
		Personality:         meta.Personality,
		CWD:                 meta.CWD,
		AgentSessionID:      meta.AgentSessionID,
		OwnerDeviceID:       meta.OwnerDeviceID,
		PendingHandoffTo:    meta.PendingHandoffTo,
		HandoffNonce:        meta.HandoffNonce,
		CreatedAt:           meta.CreatedAt,
		Status:              meta.Status,
	})
	if err != nil {
		// Always log; security-critical callers also return the error (H-4).
		m.log.Warn("persist session failed",
			slog.String("session_id", meta.ID),
			slog.String("err", err.Error()),
		)
		return err
	}
	return nil
}

// scheduleHistoryPersist marks a session's transcript dirty and starts/resets
// the debounce timer. Close paths flush immediately instead.
// Under continuous streams the debounce would reset forever; historyDirtySince
// + historyMaxLatency force a flush within the crash-loss bound (MADR 0056 M-3).
func (m *Manager) scheduleHistoryPersist(id string) {
	if m.store == nil || id == "" {
		return
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	if m.dirtyHistory == nil {
		m.dirtyHistory = make(map[string]struct{})
	}
	if m.historyDirtySince == nil {
		m.historyDirtySince = make(map[string]time.Time)
	}
	if _, ok := m.historyDirtySince[id]; !ok {
		m.historyDirtySince[id] = time.Now()
	}
	m.dirtyHistory[id] = struct{}{}
	// Force flush when this id has been dirty longer than max latency.
	if since, ok := m.historyDirtySince[id]; ok && time.Since(since) >= historyMaxLatency {
		// Unlock before flush (FlushHistory takes historyMu).
		m.historyMu.Unlock()
		m.FlushHistory()
		m.historyMu.Lock()
		return
	}
	if m.historyTimer != nil {
		m.historyTimer.Reset(historyPersistDebounce)
		return
	}
	m.historyTimer = time.AfterFunc(historyPersistDebounce, m.FlushHistory)
}

func (m *Manager) clearHistoryDirty(id string) {
	m.historyMu.Lock()
	delete(m.dirtyHistory, id)
	if m.historyDirtySince != nil {
		delete(m.historyDirtySince, id)
	}
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
	// Clear dirty-since for flushed ids so max-latency restarts cleanly.
	if m.historyDirtySince != nil {
		for _, id := range ids {
			delete(m.historyDirtySince, id)
		}
	}
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
