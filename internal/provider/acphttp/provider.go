package acphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const engineStartTimeout = 60 * time.Second
const engineStopTimeout = 5 * time.Second

type engine struct {
	cmd  *exec.Cmd
	url  string
	port int
	dead chan struct{}
}

// Provider manages an ACP-over-HTTP engine process and its sessions.
type Provider struct {
	spec Spec
	cfg  Config
	log  *slog.Logger

	mu       sync.Mutex
	eng      *engine
	starting bool
	closed   bool

	ws         *websocket.Conn
	wsFramer   *wsFramer
	connID     string
	sessions   map[string]*session
	generation int

	httpc *http.Client
}

// New creates a Provider from spec and config.
func New(spec Spec, cfg Config) *Provider {
	return NewWithLogger(spec, cfg, nil)
}

// NewWithLogger creates a Provider with a custom logger.
func NewWithLogger(spec Spec, cfg Config, log *slog.Logger) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = spec.DefaultBin
	}
	l := slog.Default()
	if log != nil {
		l = log
	}
	return &Provider{
		spec: spec,
		cfg:  cfg,
		log:  l.With(slog.String("component", "provider."+string(spec.ID)+"-acphttp")),
		httpc: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:    2,
				IdleConnTimeout: 30 * time.Second,
			},
		},
		sessions: make(map[string]*session),
	}
}

// ID returns the provider identifier.
func (p *Provider) ID() provider.ID { return p.spec.ID }

// Ready reports whether the engine binary is found on PATH.
func (p *Provider) Ready() bool {
	_, err := exec.LookPath(p.cfg.Bin)
	return err == nil
}

// CommandTable returns the command table from the spec.
func (p *Provider) CommandTable() command.Table {
	return p.spec.Commands
}

// ListModels returns the static model catalog from the spec.
func (p *Provider) ListModels(_ context.Context) (picker.Catalog, error) {
	return picker.SingleCatalog(picker.SourceStatic, p.spec.StaticModels, p.cfg.Model, true), nil
}

// EnsureServer starts the engine asynchronously if not already running.
func (p *Provider) EnsureServer() {
	if !p.Ready() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), engineStartTimeout)
		defer cancel()
		if _, err := p.ensureServer(ctx); err != nil {
			p.log.Warn("engine pre-start failed",
				slog.String("bin", p.cfg.Bin), slog.String("err", err.Error()))
		}
	}()
}

func (p *Provider) ensureServer(ctx context.Context) (string, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return "", fmt.Errorf("provider shut down")
		}
		if p.eng != nil {
			url := p.eng.url
			p.mu.Unlock()
			return url, nil
		}
		if !p.starting {
			p.starting = true
			p.mu.Unlock()
			break
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	url, err := p.startServer(ctx)
	p.mu.Lock()
	p.starting = false
	p.mu.Unlock()
	return url, err
}

func (p *Provider) startServer(ctx context.Context) (string, error) {
	port, err := freePort()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(p.cfg.Bin, p.spec.ServeArgs(port)...)
	procutil.SetProcessGroup(cmd)
	procutil.SetDeathSignal(cmd)
	cmd.Stdout = io.Discard
	stderr := &lineRing{log: p.log, prefix: string(p.spec.ID) + "-stderr", max: 20}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitCh := make(chan error, 1)
	dead := make(chan struct{})
	go func() {
		waitCh <- cmd.Wait()
		close(dead)
	}()

	deadline := time.Now().Add(engineStartTimeout)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			_ = procutil.KillProcessGroup(cmd.Process)
			<-waitCh
			tail := stderr.tail()
			if tail != "" {
				return "", fmt.Errorf("%s server did not become healthy in %s; stderr:\n%s",
					p.cfg.Bin, engineStartTimeout, tail)
			}
			return "", fmt.Errorf("%s server did not become healthy in %s", p.cfg.Bin, engineStartTimeout)
		}
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url+p.spec.HealthPath, nil)
		res, err := p.httpc.Do(req)
		cancel()
		if err == nil {
			_, _ = io.ReadAll(io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-waitCh:
			tail := stderr.tail()
			if tail != "" {
				return "", fmt.Errorf("%s server exited during startup; stderr:\n%s", p.cfg.Bin, tail)
			}
			return "", fmt.Errorf("%s server exited during startup", p.cfg.Bin)
		case <-time.After(50 * time.Millisecond):
		}
	}

	conn := newACPConn(url, p.cfg)
	if err := conn.initialize(ctx); err != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		<-waitCh
		return "", fmt.Errorf("acp initialize: %w", err)
	}

	ws, err := conn.dialWS(ctx)
	if err != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		<-waitCh
		return "", fmt.Errorf("ws dial: %w", err)
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		ws.Close(websocket.StatusNormalClosure, "shutdown")
		_ = procutil.TerminateProcessGroup(cmd.Process, dead, engineStopTimeout)
		<-waitCh
		return "", fmt.Errorf("provider shut down")
	}
	p.eng = &engine{cmd: cmd, url: url, port: port, dead: dead}
	p.ws = ws
	p.wsFramer = newWSFramer(ws, p.log)
	p.connID = conn.connID
	p.generation++
	gen := p.generation
	p.mu.Unlock()

	p.log.Info("engine ready", slog.String("bin", p.cfg.Bin), slog.String("url", url))

	go func() {
		err := <-waitCh
		p.mu.Lock()
		if p.generation != gen {
			p.mu.Unlock()
			return
		}
		p.eng = nil
		p.ws = nil
		p.wsFramer = nil
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

	go p.readPump()
	return url, nil
}

// Shutdown stops the engine and closes the WebSocket.
func (p *Provider) Shutdown() {
	p.mu.Lock()
	p.closed = true
	eng := p.eng
	p.eng = nil
	ws := p.ws
	p.ws = nil
	p.wsFramer = nil
	p.mu.Unlock()

	if ws != nil {
		ws.Close(websocket.StatusNormalClosure, "shutdown")
	}
	if eng != nil && eng.cmd != nil && eng.cmd.Process != nil {
		procutil.TerminateProcessGroup(eng.cmd.Process, eng.dead, engineStopTimeout)
	}
}

// Start creates a new session with the engine.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	base, err := p.ensureServer(ctx)
	if err != nil {
		return nil, err
	}
	return p.startSession(ctx, base, opts)
}

func (p *Provider) startSession(ctx context.Context, base string, opts provider.StartOptions) (*session, error) {
	s := newSession(p, p.cfg, opts, p.log)
	if err := s.create(ctx); err != nil {
		return nil, fmt.Errorf("session create: %w", err)
	}
	p.mu.Lock()
	p.sessions[s.agentID] = s
	p.mu.Unlock()
	return s, nil
}

func (p *Provider) routeNotification(method string, params json.RawMessage, full []byte) {
	switch method {
	case "sessionUpdate":
		var notif struct {
			SessionID string          `json:"sessionId"`
			Update    json.RawMessage `json:"update"`
		}
		if err := json.Unmarshal(params, &notif); err != nil {
			p.log.Debug("route: unparseable sessionUpdate params")
			return
		}
		p.mu.Lock()
		s := p.sessions[notif.SessionID]
		p.mu.Unlock()
		if s == nil {
			return
		}
		s.handleUpdate(notif.Update)
	case "permission_request":
		var notif struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(params, &notif); err != nil {
			return
		}
		p.mu.Lock()
		s := p.sessions[notif.SessionID]
		p.mu.Unlock()
		if s != nil {
			s.handlePermissionRequest(params)
		}
	}
}

func (p *Provider) handleWSError(err error) {
	p.log.Debug("ws error", slog.String("err", err.Error()))
	p.mu.Lock()
	ws := p.ws
	if ws != nil {
		ws.Close(websocket.StatusAbnormalClosure, "read error")
	}
	p.mu.Unlock()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type lineRing struct {
	log    *slog.Logger
	prefix string
	max    int

	mu   sync.Mutex
	buf  []byte
	ring []string
}

func (w *lineRing) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) > 4096 {
				w.pushLocked(string(w.buf[:4096]) + "…")
				w.buf = w.buf[:0]
			}
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		n := copy(w.buf, w.buf[i+1:])
		w.buf = w.buf[:n]
		if line != "" {
			w.pushLocked(line)
		}
	}
	return len(p), nil
}

func (w *lineRing) pushLocked(line string) {
	if w.log != nil {
		w.log.Debug(w.prefix, slog.String("line", line))
	}
	if w.max <= 0 {
		return
	}
	w.ring = append(w.ring, line)
	if len(w.ring) > w.max {
		w.ring = append([]string(nil), w.ring[len(w.ring)-w.max:]...)
	}
}

func (w *lineRing) tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.ring) == 0 {
		return ""
	}
	return strings.Join(w.ring, "\n")
}
