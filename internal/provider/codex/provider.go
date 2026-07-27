package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

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

// ListModels returns the model catalog from the engine.
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	if _, err := p.ensureEngine(ctx); err != nil {
		return picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true), nil
	}
	fr := p.framer()
	if fr == nil {
		return picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true), nil
	}
	raw, err := fr.sendRequest(ctx, "model/list", nil)
	if err != nil {
		return picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true), nil
	}
	var resp struct {
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			IsDefault   bool   `json:"isDefault"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true), nil
	}
	opts := make([]picker.Option, 0, len(resp.Models))
	for _, m := range resp.Models {
		opts = append(opts, picker.Option{
			ID:    m.ID,
			Label: m.DisplayName,
		})
	}
	return picker.SingleCatalog(picker.SourceMerged, opts, p.cfg.Model, true), nil
}

// EnsureServer starts the engine asynchronously if not already running.
func (p *Provider) EnsureServer() {
	if !p.Ready() {
		return
	}
	go func() {
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

	waitCh := make(chan error, 1)
	dead := make(chan struct{})
	go func() {
		waitCh <- cmd.Wait()
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
