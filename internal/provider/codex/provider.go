package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

type engine struct {
	cmd  *exec.Cmd
	conn *conn
	dead chan struct{}
}

// Provider manages a Codex app-server engine process and its sessions.
type Provider struct {
	cfg Config
	log *slog.Logger

	mu       sync.Mutex
	eng      *engine
	starting bool
	closed   bool

	sessions   map[string]*session
	generation int
}

// New creates a Provider from config.
func New(cfg Config) *Provider {
	return NewWithLogger(cfg, nil)
}

// NewWithLogger creates a Provider from config with a custom logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = "codex"
	}
	l := slog.Default()
	if log != nil {
		l = log
	}
	return &Provider{
		cfg:      cfg,
		log:      l.With(slog.String("component", "provider.codex")),
		sessions: make(map[string]*session),
	}
}

// ID returns the provider identifier.
func (p *Provider) ID() provider.ID { return provider.IDCodex }

// Ready reports whether the engine binary is found on PATH.
func (p *Provider) Ready() bool {
	_, err := exec.LookPath(p.cfg.Bin)
	return err == nil
}

// CommandTable returns the command table.
func (p *Provider) CommandTable() command.Table { return commandTable }

// maxModelListPages bounds model/list cursor following. Codex returns 7 models
// in one page today; the bound exists so a malformed or looping cursor cannot
// spin the WS handler.
const maxModelListPages = 10

// modelListEntry is one row of a codex model/list page.
//
// The field names are load-bearing and were both wrong before MADR 0043 D5:
// the response array is `data`, not `models`, and the request needs a `params`
// object — codex answers a missing one with `-32600 Invalid request: missing
// field 'params'`. Both failures used to fall into the same empty static
// catalog as "no engine", which is why an empty codex model picker looked like
// a codex limitation for a release.
type modelListEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Hidden      bool   `json:"hidden"`
	IsDefault   bool   `json:"isDefault"`
	// DefaultReasoningEffort and InputModalities are display metadata; codex
	// exposes reasoning effort per model and the daemon shows it as a badge.
	DefaultReasoningEffort string   `json:"defaultReasoningEffort"`
	InputModalities        []string `json:"inputModalities"`
	// SupportedReasoningEfforts is the model's own ladder, with prose per rung.
	// Required in codex's schema and it genuinely varies: measured on 0.145.0,
	// gpt-5.6-sol/terra advertise six rungs (including `ultra`), luna five and
	// the 5.4/5.5 family four (MADR 0052 §1).
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
		Description     string `json:"description"`
	} `json:"supportedReasoningEfforts"`
}

type modelListPage struct {
	Data       []modelListEntry `json:"data"`
	NextCursor string           `json:"nextCursor"`
}

// ListModels returns the model catalog from the engine.
//
// It works with no thread open — model/list is an app-server request, not a
// thread one — so codex supports pre-session model selection like every other
// provider.
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	fallback := picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true)
	if _, err := p.ensureEngine(ctx); err != nil {
		p.log.Warn("list models: engine unavailable", slog.String("err", err.Error()))
		return fallback, nil
	}
	fr := p.framer()
	if fr == nil {
		p.log.Warn("list models: engine not running")
		return fallback, nil
	}
	return listModelsVia(ctx, fr.sendRequest, p.cfg.Model, p.log)
}

// rpcSender is the slice of the engine connection catalog code needs. Taking it
// as a function lets the paging, the `params` shape and the `data` field name
// be tested without an engine — all three were wrong at once before, and the
// only reason that survived a release is that nothing could exercise them.
type rpcSender func(ctx context.Context, method string, params any) (json.RawMessage, error)

func listModelsVia(ctx context.Context, send rpcSender, cfgModel string, log *slog.Logger) (picker.Catalog, error) {
	fallback := picker.SingleCatalog(picker.SourceStatic, nil, cfgModel, true)
	opts := make([]picker.Option, 0, 8)
	defaultID := ""
	cursor := ""
	for page := 0; page < maxModelListPages; page++ {
		// An empty object, never nil: codex rejects a request with no params.
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := send(ctx, "model/list", params)
		if err != nil {
			// Loud, not silent: this is exactly the failure that hid two bugs.
			log.Warn("list models: model/list failed",
				slog.Int("page", page), slog.String("err", err.Error()))
			return fallback, nil
		}
		var resp modelListPage
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Warn("list models: model/list decode failed", slog.String("err", err.Error()))
			return fallback, nil
		}
		for _, m := range resp.Data {
			if m.ID == "" || m.Hidden {
				continue
			}
			meta := map[string]string{}
			if m.DefaultReasoningEffort != "" {
				meta["reasoning_effort"] = m.DefaultReasoningEffort
			}
			if len(m.InputModalities) > 0 {
				meta["input"] = strings.Join(m.InputModalities, ",")
			}
			if len(meta) == 0 {
				meta = nil
			}
			levels := make([]picker.ThinkingLevel, 0, len(m.SupportedReasoningEfforts))
			for _, e := range m.SupportedReasoningEfforts {
				if e.ReasoningEffort == "" {
					continue
				}
				levels = append(levels, picker.ThinkingLevel{
					ID: e.ReasoningEffort,
					// codex ships no label, only prose; clients fall back to ID.
					Description: e.Description,
					Default:     e.ReasoningEffort == m.DefaultReasoningEffort,
				})
			}
			opts = append(opts, picker.Option{
				ID:          m.ID,
				Label:       m.DisplayName,
				Description: m.Description,
				Meta:        meta,
				// Normalised even though codex already reports cheapest-first:
				// the direction is the daemon's guarantee to clients, not an
				// assumption about one provider's ordering.
				ThinkingLevels: picker.NormalizeThinkingLevels(levels),
			})
			if m.IsDefault && defaultID == "" {
				defaultID = m.ID
			}
		}
		if resp.NextCursor == "" || resp.NextCursor == cursor {
			break
		}
		cursor = resp.NextCursor
	}
	if len(opts) == 0 {
		// An engine that answered with nothing is still a live answer, but an
		// empty picker is indistinguishable from a broken one — say so.
		log.Warn("list models: engine returned no models")
		return fallback, nil
	}
	// Config is the operator's pre-session policy and outranks the engine's own
	// default, so the picker cannot claim a model Start will not use.
	if cfgModel != "" {
		defaultID = cfgModel
	}
	// Codex reports no release dates, so ordering only pins the default first
	// and leaves the engine's own order intact (MADR 0043 D3).
	return picker.SingleCatalog(picker.SourceLive,
		picker.OrderModels(opts, defaultID), defaultID, true), nil
}

// EnsureServer starts the engine asynchronously if not already running.
func (p *Provider) EnsureServer() {
	if !p.Ready() {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("engine pre-start panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), engineStartTimeout)
		defer cancel()
		if _, err := p.ensureEngine(ctx); err != nil {
			p.log.Warn("engine pre-start failed",
				slog.String("bin", p.cfg.Bin), slog.String("err", err.Error()))
		}
	}()
}

// enginePollInterval paces callers waiting on another goroutine's in-flight
// engine start. It must never be zero: a spin here burns a whole core per
// waiter and, because ws async handlers run on a per-connection slot budget,
// wedges every other operation on that connection behind it.
const enginePollInterval = 200 * time.Millisecond

func (p *Provider) ensureEngine(ctx context.Context) (*conn, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, fmt.Errorf("provider shut down")
		}
		if p.eng != nil {
			cn := p.eng.conn
			p.mu.Unlock()
			return cn, nil
		}
		if !p.starting {
			p.starting = true
			p.mu.Unlock()
			break
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(enginePollInterval):
		}
	}
	// Bound the handshake on our own clock as well as the caller's: session
	// creates arrive with a deliberately cancel-free context (a phone that
	// drops mid-create must not abort the launch), so a wedged engine would
	// otherwise hold the starting gate — and every waiter — forever.
	startCtx, cancel := context.WithTimeout(ctx, engineStartTimeout)
	defer cancel()
	fr, err := p.startEngine(startCtx)
	p.mu.Lock()
	p.starting = false
	p.mu.Unlock()
	return fr, err
}

func (p *Provider) startEngine(ctx context.Context) (*conn, error) {
	cmd := exec.Command(p.cfg.Bin, "app-server", "--listen", "stdio://")
	procutil.SetProcessGroup(cmd)
	procutil.SetDeathSignal(cmd)
	// Stamp ownership into the environment (Linux reaping) and registry (cross-platform).
	engineID := uuid.NewString()
	cmd.Env = append(os.Environ(),
		procutil.EnvEngineID+"="+engineID,
		procutil.EnvEngineOwner+"="+procutil.OwnerToken(),
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr := &lineRing{log: p.log, prefix: "codex-stderr", max: 20}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}
	lease, regErr := procutil.RegisterEngine("", procutil.EngineRecord{
		ID:       engineID,
		Provider: "codex",
		PID:      cmd.Process.Pid,
		PGID:     cmd.Process.Pid,
		Owner:    procutil.OwnerToken(),
	})
	if regErr != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		return nil, fmt.Errorf("register engine: %w", regErr)
	}

	waitCh := make(chan error, 1)
	dead := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("cmd wait panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		waitCh <- cmd.Wait()
		_ = procutil.RemoveEngine(lease)
		close(dead)
	}()

	cn := newConn(stdin, stdout, p.log)
	// Start consuming JSON-RPC frames before the initialize request. The
	// handshake itself is a request, so waiting for its response before the
	// read pump runs leaves the response in stdout unread until the caller's
	// context expires.
	go cn.readPump(p.routeNotification, p.routeServerRequest)

	params := map[string]any{
		"clientInfo": map[string]string{
			"name":    "mcremote",
			"title":   "magic-cli-remote",
			"version": "dev",
		},
		"capabilities": map[string]bool{
			"experimentalApi": false,
		},
	}
	raw, err := cn.sendRequest(ctx, "initialize", params)
	if err != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		<-waitCh
		tail := stderr.tail()
		if tail != "" {
			return nil, fmt.Errorf("initialize: %w; stderr:\n%s", err, tail)
		}
		return nil, fmt.Errorf("initialize: %w", err)
	}

	_ = cn.sendNotification(ctx, "initialized", nil)

	var initResp struct {
		CodexHome string `json:"codexHome"`
	}
	if err := json.Unmarshal(raw, &initResp); err != nil {
		p.log.Debug("codex: initialize response parse", slog.String("err", err.Error()))
	}
	if initResp.CodexHome != "" {
		p.log.Debug("codex: engine ready", slog.String("codex_home", initResp.CodexHome))
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = procutil.KillProcessGroup(cmd.Process)
		<-waitCh
		return nil, fmt.Errorf("provider shut down")
	}
	p.eng = &engine{cmd: cmd, conn: cn, dead: dead}
	p.generation++
	gen := p.generation
	p.mu.Unlock()

	p.log.Info("engine ready", slog.String("bin", p.cfg.Bin))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("engine death monitor panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		err := <-waitCh
		p.mu.Lock()
		if p.generation != gen {
			p.mu.Unlock()
			return
		}
		p.eng = nil
		sessions := make([]*session, 0, len(p.sessions))
		for _, s := range p.sessions {
			sessions = append(sessions, s)
		}
		p.sessions = make(map[string]*session)
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return
		}
		p.log.Warn("engine exited", slog.String("bin", p.cfg.Bin), slog.Any("err", err))
		for _, s := range sessions {
			s.serverDied()
		}
	}()

	return cn, nil
}

// Shutdown stops the engine process.
func (p *Provider) Shutdown() {
	p.mu.Lock()
	p.closed = true
	eng := p.eng
	p.eng = nil
	p.mu.Unlock()

	if eng != nil && eng.cmd != nil && eng.cmd.Process != nil {
		procutil.TerminateProcessGroup(eng.cmd.Process, eng.dead, engineStopTimeout)
	}
}

// Start creates a new session with the engine.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	_, err := p.ensureEngine(ctx)
	if err != nil {
		return nil, err
	}
	return p.startSession(ctx, opts)
}

func (p *Provider) startSession(ctx context.Context, opts provider.StartOptions) (*session, error) {
	fr := p.framer()
	if fr == nil {
		return nil, fmt.Errorf("engine not running")
	}

	// Shared resolution + errno-preserving validation (0069 P1): codex
	// previously fell back to os.Getwd() with no validation at all.
	cwd, err := provider.ResolveSessionCWD(opts.CWD, p.cfg.DefaultCWD, nil)
	if err != nil {
		return nil, err
	}
	opts.CWD = cwd
	s := newSession(p, p.cfg, opts, p.log)
	if err := s.create(ctx, fr); err != nil {
		return nil, fmt.Errorf("session create: %w", err)
	}

	p.mu.Lock()
	p.sessions[s.agentID] = s
	p.mu.Unlock()

	return s, nil
}

func (p *Provider) framer() *conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng == nil {
		return nil
	}
	return p.eng.conn
}

func (p *Provider) routeNotification(method string, params json.RawMessage) {
	var info struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(params, &info)
	if info.ThreadID != "" {
		p.mu.Lock()
		s := p.sessions[info.ThreadID]
		p.mu.Unlock()
		if s != nil {
			s.handleNotification(method, params)
			return
		}
	}
	p.log.Debug("codex: unhandled notification", slog.String("method", method))
}

func (p *Provider) routeServerRequest(method string, id json.RawMessage, params json.RawMessage) {
	var info struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(params, &info)
	if info.ThreadID != "" {
		p.mu.Lock()
		s := p.sessions[info.ThreadID]
		p.mu.Unlock()
		if s != nil {
			s.handleServerRequest(method, id, params)
			return
		}
	}
	p.log.Debug("codex: unhandled server request", slog.String("method", method))
	fr := p.framer()
	if fr != nil {
		_ = fr.sendResponse(context.Background(), id, nil, &rpcErrorBody{
			Code: -32000, Message: "unknown thread",
		})
	}
}

type lineRing struct {
	log    *slog.Logger
	prefix string
	max    int

	mu   sync.Mutex
	ring []string
}

func (w *lineRing) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' {
			line := string(p[:i])
			if line != "" {
				if w.log != nil {
					w.log.Debug(w.prefix, slog.String("line", line))
				}
				w.ring = append(w.ring, line)
				if len(w.ring) > w.max {
					w.ring = w.ring[len(w.ring)-w.max:]
				}
			}
			p = p[i+1:]
			i = -1
		}
	}
	return len(p), nil
}

func (w *lineRing) tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.ring) == 0 {
		return ""
	}
	out := ""
	for i, line := range w.ring {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}
