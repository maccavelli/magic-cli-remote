package httpagent

import (
	"bufio"
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
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// serverStartTimeout bounds spawn → health-path healthy.
const serverStartTimeout = 60 * time.Second

// Provider manages one shared engine process and its SSE stream for a
// [Dialect].
type Provider struct {
	dialect Dialect
	cfg     Config
	log     *slog.Logger

	mu       sync.Mutex
	cmd      *exec.Cmd
	baseURL  string
	starting bool
	closed   bool
	// sessions routes SSE events by agent-side session id.
	sessions map[string]*session
	// generation increments per server (re)start so stale monitors are inert.
	generation int

	httpc *http.Client
}

// New creates the provider. The server is spawned lazily (or via EnsureServer).
func New(d Dialect, cfg Config) *Provider {
	return NewWithLogger(d, cfg, nil)
}

// NewWithLogger creates the provider with a logger.
func NewWithLogger(d Dialect, cfg Config, log *slog.Logger) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = d.DefaultBin()
	}
	l := slog.Default()
	if log != nil {
		l = log
	}
	return &Provider{
		dialect:  d,
		cfg:      cfg,
		log:      l.With(slog.String("component", "provider."+string(d.ID())+"-http")),
		sessions: make(map[string]*session),
		httpc: &http.Client{
			// Per-request timeouts are set via context; SSE needs no global cap.
			Timeout: 0,
		},
	}
}

// ID implements [provider.Provider].
func (p *Provider) ID() provider.ID { return p.dialect.ID() }

// Ready implements [provider.Provider].
func (p *Provider) Ready() bool {
	_, err := exec.LookPath(p.cfg.Bin)
	return err == nil
}

// EnsureServer spawns (or confirms) the engine in the background so the first
// session create doesn't pay the boot. Errors are logged, not returned — the
// next Start retries synchronously.
func (p *Provider) EnsureServer() {
	if !p.Ready() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), serverStartTimeout)
		defer cancel()
		if _, err := p.ensureServer(ctx); err != nil {
			p.log.Warn("engine pre-start failed",
				slog.String("bin", p.cfg.Bin), slog.String("err", err.Error()))
		}
	}()
}

// Shutdown stops the engine (daemon exit).
func (p *Provider) Shutdown() {
	p.mu.Lock()
	p.closed = true
	cmd := p.cmd
	p.cmd = nil
	p.baseURL = ""
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
	}
}

// ensureServer returns the base URL of a healthy engine, spawning it if
// needed. Serialized under p.mu via the starting flag.
func (p *Provider) ensureServer(ctx context.Context) (string, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return "", fmt.Errorf("provider shut down")
		}
		if p.baseURL != "" {
			url := p.baseURL
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
	if err == nil {
		p.baseURL = url
	}
	p.mu.Unlock()
	return url, err
}

func (p *Provider) startServer(ctx context.Context) (string, error) {
	port, err := freePort()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(p.cfg.Bin, p.dialect.ServeArgs(port)...)
	procutil.SetProcessGroup(cmd)
	home, _ := os.UserHomeDir()
	if home != "" {
		cmd.Dir = home
	}
	cmd.Stdout = io.Discard
	// Capture stderr for debug logging and health-failure diagnostics (Phase 2.7).
	stderr := &lineRing{log: p.log, prefix: string(p.dialect.ID()) + "-stderr", max: 20}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s server: %w", p.cfg.Bin, err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Poll health until the engine is up.
	deadline := time.Now().Add(serverStartTimeout)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			_ = procutil.KillProcessGroup(cmd.Process)
			_ = cmd.Wait()
			tail := stderr.tail()
			if tail != "" {
				return "", fmt.Errorf("%s server did not become healthy in %s; recent stderr:\n%s",
					p.cfg.Bin, serverStartTimeout, tail)
			}
			return "", fmt.Errorf("%s server did not become healthy in %s", p.cfg.Bin, serverStartTimeout)
		}
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url+p.dialect.HealthPath(), nil)
		res, err := p.httpc.Do(req)
		cancel()
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		// Tight poll: engine boot is the cold-start critical path (~3–5s);
		// 50ms detection beats the old 250ms average half-interval waste.
		time.Sleep(50 * time.Millisecond)
	}

	p.mu.Lock()
	p.cmd = cmd
	p.generation++
	gen := p.generation
	p.mu.Unlock()

	p.log.Info("engine ready", slog.String("bin", p.cfg.Bin), slog.String("url", url))

	// Dialect boot hook (e.g. catalog model refine) runs async: OpenCode's
	// /provider payload is multi-MB and must not delay the first session
	// create. Dialects that need a fallback must seed one before AfterBoot.
	go p.dialect.AfterBoot(context.WithoutCancel(ctx), p.apiAt(url))

	// Death monitor: mark the server gone so the next Start respawns, and
	// fail every live session (their server-side state is unreachable).
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		if p.generation != gen {
			p.mu.Unlock()
			return
		}
		p.cmd = nil
		p.baseURL = ""
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

	// One SSE stream for every session on this server generation.
	go p.pumpEvents(url, gen)

	return url, nil
}

// pumpEvents reads the dialect's event stream and routes each event to its
// session. Reconnects with backoff while this server generation is alive.
func (p *Provider) pumpEvents(url string, gen int) {
	backoff := time.Second
	for {
		p.mu.Lock()
		alive := p.generation == gen && p.baseURL == url && !p.closed
		p.mu.Unlock()
		if !alive {
			return
		}

		err := p.streamOnce(url, gen)
		if err != nil {
			p.log.Debug("sse stream ended", slog.String("err", err.Error()))
		}
		time.Sleep(backoff)
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func (p *Provider) streamOnce(url string, gen int) error {
	req, err := http.NewRequest(http.MethodGet, url+p.dialect.EventsPath(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	res, err := p.httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("sse status %d", res.StatusCode)
	}

	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		typ, props, sid, ok := p.dialect.DecodeFrame(line[len("data: "):])
		if !ok || sid == "" {
			continue
		}
		p.mu.Lock()
		stale := p.generation != gen
		s := p.sessions[sid]
		p.mu.Unlock()
		if stale {
			return nil
		}
		if s != nil {
			s.dispatch(typ, props)
		}
	}
	return sc.Err()
}

func (p *Provider) register(s *session) {
	p.mu.Lock()
	p.sessions[s.agentID] = s
	p.mu.Unlock()
}

func (p *Provider) unregister(agentID string) {
	p.mu.Lock()
	delete(p.sessions, agentID)
	p.mu.Unlock()
}

// api performs a JSON request against the current engine.
func (p *Provider) api(ctx context.Context, method, path string, body any, out any) error {
	p.mu.Lock()
	url := p.baseURL
	p.mu.Unlock()
	if url == "" {
		return fmt.Errorf("%s server not running", p.cfg.Bin)
	}
	return p.apiAt(url)(ctx, method, path, body, out)
}

// apiAt returns an API bound to a specific base URL (used during boot,
// before p.baseURL is published).
func (p *Provider) apiAt(base string) API {
	return func(ctx context.Context, method, path string, body any, out any) error {
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := p.httpc.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		// 16MB: engine catalogs (e.g. OpenCode's /provider model list) exceed
		// 4MB, and a truncated read decodes as corrupt JSON.
		data, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))
		if res.StatusCode < 200 || res.StatusCode > 299 {
			msg := strings.TrimSpace(string(data))
			if len(msg) > 300 {
				msg = msg[:300] + "…"
			}
			return fmt.Errorf("%s %s: HTTP %d: %s", method, path, res.StatusCode, msg)
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("%s %s: decode: %w", method, path, err)
			}
		}
		return nil
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

// lineRing writes process stderr as slog debug lines and keeps the last max
// lines for health-failure diagnostics.
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
