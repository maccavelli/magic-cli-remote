package httpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// session is one server-side session multiplexed over the shared engine.
// There is no per-session process: Close tears down only local state, which
// is what makes resume free. All protocol semantics live in s.ds.
type session struct {
	p       *Provider
	ds      DialectSession
	localID string
	agentID string
	cwd     string
	model   string
	log     *slog.Logger
	events  chan event.Event
	done    chan struct{}

	mu         sync.Mutex
	closed     bool
	turnActive bool
	// pending permission requests by id (answered via the dialect's REST op).
	pending map[string]struct{}

	lastActivity atomic.Int64
}

var _ provider.Session = (*session)(nil)
var _ provider.PermissionSession = (*session)(nil)
var _ provider.CWDSession = (*session)(nil)
var _ Host = (*session)(nil)

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return s.p.dialect.ID() }
func (s *session) AgentSessionID() string     { return s.agentID }
func (s *session) CWD() string                { return s.cwd }
func (s *session) Model() string              { return s.model }
func (s *session) Config() Config             { return s.p.cfg }
func (s *session) Log() *slog.Logger          { return s.log }
func (s *session) API() API                   { return s.p.api }
func (s *session) Events() <-chan event.Event { return s.events }

// Start creates (or re-attaches to) a server-side session.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("%s binary %q not found in PATH: %w",
			p.dialect.ID(), p.cfg.Bin, provider.ErrNotImplemented)
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
		return nil, fmt.Errorf("%s server: %w", p.cfg.Bin, err)
	}

	localID := opts.LocalSessionID
	if localID == "" {
		localID = uuid.NewString()
	}

	model := opts.Model
	if model == "" {
		model = p.cfg.Model
	}

	s := &session{
		p:       p,
		localID: localID,
		cwd:     cwd,
		model:   model,
		log:     p.log.With(slog.String("session_id", localID)),
		events:  make(chan event.Event, 256),
		done:    make(chan struct{}),
		pending: make(map[string]struct{}),
	}
	s.ds = p.dialect.NewSession(s)

	if opts.AgentSessionID != "" {
		// Resume: the session already lives on the server — no replay, no
		// engine work. Verify it exists, then rebuild our history ring from
		// its message log (marked Replay so clients don't double-append).
		agentID, err := s.ds.Resume(startCtx, opts.AgentSessionID)
		if err != nil {
			return nil, fmt.Errorf("%s session resume: %w", p.dialect.ID(), err)
		}
		s.agentID = agentID
		p.register(s)
		s.ds.Replay(startCtx)
		s.log.Info("http session resumed", slog.String("agent_session_id", s.agentID))
	} else {
		agentID, err := s.ds.Create(startCtx, opts)
		if err != nil {
			return nil, fmt.Errorf("%s session create: %w", p.dialect.ID(), err)
		}
		if agentID == "" {
			return nil, fmt.Errorf("%s session create: empty id", p.dialect.ID())
		}
		s.agentID = agentID
		p.register(s)
		s.log.Info("http session created", slog.String("agent_session_id", s.agentID))
	}

	s.Emit(event.Event{
		Type:           event.TypeSessionStatus,
		Status:         "idle",
		AgentSessionID: s.agentID,
	})
	return s, nil
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
	for _, c := range parts {
		if c.Type == "" || c.Type == "text" {
			text.WriteString(c.Text)
		}
	}

	// The submit call returns once the turn is enqueued; the turn itself
	// streams over SSE and ends with the dialect's turn-end event.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.ds.Prompt(callCtx, parts); err != nil {
		s.EndTurn()
		return err
	}

	s.Emit(event.Event{Type: event.TypeUserMessage, Text: text.String()})
	s.Emit(event.Event{Type: event.TypeSessionStatus, Status: "running"})

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
			s.Emit(event.Event{
				Type: event.TypeNotice,
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
	return s.ds.Abort(callCtx)
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	if !s.TakePending(permissionID) {
		return fmt.Errorf("unknown or expired permission %q", permissionID)
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.ds.RespondPermission(callCtx, permissionID, optionID, cancelled); err != nil {
		// Still outstanding server-side; allow a retry.
		s.mu.Lock()
		s.pending[permissionID] = struct{}{}
		s.mu.Unlock()
		return err
	}
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.Emit(event.Event{
		Type:         event.TypePermissionResolved,
		PermissionID: permissionID,
		Status:       status,
	})
	return nil
}

// Purge removes the server-side session, then tears down local state.
// Implements [provider.PurgeSession] for session.delete.
func (s *session) Purge(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.ds.Delete(callCtx); err != nil {
		// Still close local state; report the engine error to the manager.
		_ = s.Close(ctx)
		return err
	}
	return s.Close(ctx)
}

// Close releases local state only: the server-side session persists (that is
// exactly what makes resume instant). Use [Purge] when the user ends a session
// for good (daemon session.delete).
func (s *session) Close(ctx context.Context) error {
	_ = ctx
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
	s.p.unregister(s.agentID)
	return nil
}

var _ provider.PurgeSession = (*session)(nil)

// serverDied is invoked by the provider's death monitor.
func (s *session) serverDied() {
	s.Emit(event.Event{Type: event.TypeError,
		Error: fmt.Sprintf("%s server exited", s.p.cfg.Bin)})
	s.Emit(event.Event{Type: event.TypeSessionStatus, Status: "disconnected"})
}

// dispatch routes one SSE event into the dialect, stamping liveness for the
// stall watchdog first.
func (s *session) dispatch(typ string, props json.RawMessage) {
	s.lastActivity.Store(time.Now().UnixNano())
	s.ds.HandleEvent(typ, props)
}

// EndTurn implements [Host].
func (s *session) EndTurn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	was := s.turnActive
	s.turnActive = false
	return was
}

// TrackPermission implements [Host].
func (s *session) TrackPermission(id string) {
	s.mu.Lock()
	s.pending[id] = struct{}{}
	s.mu.Unlock()
	if s.p.cfg.PermissionTimeout > 0 {
		go s.expirePermission(id)
	}
}

// TakePending implements [Host].
func (s *session) TakePending(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[id]
	delete(s.pending, id)
	return ok
}

// expirePermission mirrors the ACP transport's fail-safe: reject server-side
// after the timeout so a missed notification can't hang the agent forever.
func (s *session) expirePermission(id string) {
	select {
	case <-s.done:
		return
	case <-time.After(s.p.cfg.PermissionTimeout):
	}
	if !s.TakePending(id) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.ds.RespondPermission(ctx, id, "", true)
	s.Emit(event.Event{
		Type: event.TypeNotice,
		Text: fmt.Sprintf("Permission request timed out after %s — the agent "+
			"stopped waiting. Prompt again to retry.", s.p.cfg.PermissionTimeout),
	})
	s.Emit(event.Event{
		Type:         event.TypePermissionResolved,
		PermissionID: id,
		Status:       event.PermissionStatusCancelled,
	})
}

// Emit implements [Host]: delivers ev, blocking for control events until
// consumed or closed — same guarantees as the ACP transport (the manager
// pump is attached from the moment Start returns; Start emits only one
// status event before that, which the 256 buffer trivially holds).
func (s *session) Emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
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

// EmitReplay implements [Host]. Pre-attach (Start has not returned): never
// block — these only feed the history ring, so dropping the OLDEST when the
// buffer fills keeps the most recent conversation, mirroring the ring.
func (s *session) EmitReplay(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.localID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	ev.Replay = true
	for {
		select {
		case s.events <- ev:
			return
		default:
			select {
			case <-s.events:
			default:
			}
		}
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
