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
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const engineStartTimeout = 60 * time.Second
const engineStopTimeout = 5 * time.Second

// maxListedAgentSessions and maxListResponseBytes bound provider-controlled
// session/list output before it reaches the remote control plane.
const (
	maxListedAgentSessions = 100
	maxListResponseBytes   = 1 << 20
)

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
	fr         rpcFramer
	connID     string
	agentCaps  acp.AgentCapabilities
	sessions   map[string]*session
	generation int
	// configOpts is the last session config options any live session reported.
	// For goose these carry the model catalog (MADR 0043 D6), so keeping them
	// provider-wide is what makes the create-session picker work without a
	// session — and free while one is open.
	configOpts []event.ConfigOption

	httpc *http.Client
	// catalogs single-flights and TTLs the catalog probe.
	catalogs *picker.Cache[string]
}

// framer returns the live engine RPC framer, or ErrEngineDown when the engine
// is dead or shut down. Sessions must go through here: p.fr is nilled on
// engine death, and reading it without p.mu was a data race (and a nil
// dereference waiting for the next RPC after an engine crash).
func (p *Provider) framer() (rpcFramer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fr == nil {
		return nil, ErrEngineDown
	}
	return p.fr, nil
}

// caps returns the capabilities reported by the engine at initialize time.
// Written under p.mu at (re)start; read here under the same lock.
func (p *Provider) caps() acp.AgentCapabilities {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agentCaps
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
		catalogs: picker.NewCache[string](0),
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

// ListModels / ListModelProviders / ListModelsFor live in catalog.go: they are
// derived from the agent's ACP session config options rather than from the
// spec's static list.

// ListAgentSessions returns the first bounded page of ACP-native sessions.
// ACP exposes cursor pagination, but the remote picker deliberately presents a
// bounded discovery set; importing a selected session remains session/load.
func (p *Provider) ListAgentSessions(ctx context.Context) ([]provider.AgentSessionMeta, error) {
	if _, err := p.ensureServer(ctx); err != nil {
		return nil, err
	}
	if p.caps().SessionCapabilities.List == nil {
		return nil, fmt.Errorf("agent does not support session/list")
	}
	fr, err := p.framer()
	if err != nil {
		return nil, err
	}
	raw, err := fr.sendRequest(ctx, "session/list", acp.ListSessionsRequest{})
	if err != nil {
		return nil, err
	}
	if len(raw) > maxListResponseBytes {
		return nil, fmt.Errorf("session/list response exceeds %d-byte limit", maxListResponseBytes)
	}
	var response acp.ListSessionsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("session/list: decode: %w", err)
	}
	if len(response.Sessions) > maxListedAgentSessions {
		response.Sessions = response.Sessions[:maxListedAgentSessions]
	}
	out := make([]provider.AgentSessionMeta, 0, len(response.Sessions))
	for _, session := range response.Sessions {
		if session.SessionId == "" {
			continue
		}
		meta := provider.AgentSessionMeta{
			ID:    string(session.SessionId),
			CWD:   session.Cwd,
			Title: stringValue(session.Title),
		}
		if session.UpdatedAt != nil {
			updatedAt, err := time.Parse(time.RFC3339, *session.UpdatedAt)
			if err == nil {
				meta.UpdatedAt = updatedAt
			}
		}
		out = append(out, meta)
	}
	return out, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
	// Stamp ownership into the environment: Linux defense-in-depth for reaping
	// (MADR 0019). The durable cross-platform contract is the engine registry
	// (MADR 0059 D8).
	engineID := uuid.NewString()
	cmd.Env = append(os.Environ(),
		procutil.EnvEngineID+"="+engineID,
		procutil.EnvEngineOwner+"="+procutil.OwnerToken(),
	)
	cmd.Stdout = io.Discard
	stderr := &lineRing{log: p.log, prefix: string(p.spec.ID) + "-stderr", max: 20}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}
	lease, regErr := procutil.RegisterEngine("", procutil.EngineRecord{
		ID:       engineID,
		Provider: string(p.spec.ID),
		PID:      cmd.Process.Pid,
		PGID:     cmd.Process.Pid,
		Owner:    procutil.OwnerToken(),
	})
	if regErr != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		return "", fmt.Errorf("register engine: %w", regErr)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
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

	deadline := time.Now().Add(engineStartTimeout)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			_ = procutil.KillProcessGroup(cmd.Process)
			<-waitCh
			tail := stderr.tail()
			if tail != "" {
				provider.LogStderrTail(p.log, p.cfg.Bin, tail)
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
				provider.LogStderrTail(p.log, p.cfg.Bin, tail)
				return "", fmt.Errorf("%s server exited during startup; stderr:\n%s", p.cfg.Bin, tail)
			}
			return "", fmt.Errorf("%s server exited during startup", p.cfg.Bin)
		case <-time.After(50 * time.Millisecond):
		}
	}

	conn := newACPConn(url, p.cfg)
	caps, err := conn.initialize(ctx)
	if err != nil {
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
	fr := newWSFramer(ws, p.log)
	p.eng = &engine{cmd: cmd, url: url, port: port, dead: dead}
	p.ws = ws
	p.fr = fr
	p.connID = conn.connID
	if caps != nil {
		p.agentCaps = *caps
	}
	p.generation++
	gen := p.generation
	p.mu.Unlock()

	p.log.Info("engine ready", slog.String("bin", p.cfg.Bin), slog.String("url", url))

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
		p.ws = nil
		p.fr = nil
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

	go p.readPump(fr)
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
	p.fr = nil
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
	// Shared resolution + errno-preserving validation (0069 P1): acphttp
	// previously fell back to os.Getwd() with no validation at all.
	cwd, err := provider.ResolveSessionCWD(opts.CWD, p.cfg.DefaultCWD, nil)
	if err != nil {
		return nil, err
	}
	opts.CWD = cwd
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
	case "session/update":
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
	default:
		p.log.Debug("route: unhandled notification", slog.String("method", method))
	}
}

// routeAgentRequest handles JSON-RPC requests from the agent (method + id).
// Goose issues session/request_permission this way; the client must reply with
// a JSON-RPC response carrying the same id (not a separate RPC method).
func (p *Provider) routeAgentRequest(method string, id json.RawMessage, params json.RawMessage) {
	fr, ferr := p.framer()
	respond := func(result any, rpcErr *rpcErrorBody) {
		if ferr != nil {
			return
		}
		_ = fr.sendResponse(context.Background(), id, result, rpcErr)
	}
	switch method {
	case "session/request_permission":
		var req struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			p.log.Debug("route: unparseable request_permission params")
			respond(nil, &rpcErrorBody{Code: -32602, Message: "invalid params"})
			return
		}
		p.mu.Lock()
		s := p.sessions[req.SessionID]
		p.mu.Unlock()
		if s == nil {
			respond(nil, &rpcErrorBody{Code: -32000, Message: "unknown session"})
			return
		}
		s.handlePermissionRequest(id, params)
	default:
		p.log.Debug("route: unhandled agent request", slog.String("method", method))
		respond(nil, &rpcErrorBody{Code: -32601, Message: "method not found: " + method})
	}
}

// handleWSError runs when the engine WebSocket read fails. The engine process
// may still be alive, but without the socket no session RPC can succeed and no
// update can arrive: every session would hang in its last state forever while
// ensureServer kept returning the dead engine. Kill the process instead — the
// wait-goroutine then performs the usual generation-checked teardown (sessions
// hear serverDied → clients see "disconnected") and the next Start respawns a
// healthy engine. On Shutdown the engine fields are already nil, so the
// expected read error from our own close is a no-op here.
func (p *Provider) handleWSError(err error) {
	p.mu.Lock()
	ws := p.ws
	eng := p.eng
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	p.log.Warn("engine websocket lost; restarting engine", slog.String("err", err.Error()))
	if ws != nil {
		ws.Close(websocket.StatusAbnormalClosure, "read error")
	}
	if eng != nil && eng.cmd != nil && eng.cmd.Process != nil {
		_ = procutil.KillProcessGroup(eng.cmd.Process)
	}
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
