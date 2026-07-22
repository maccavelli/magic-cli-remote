// Package opencodehttp drives OpenCode through its native HTTP + SSE server
// (`opencode serve`) instead of per-session `opencode acp` subprocesses.
//
// Why: every `opencode acp` process is a full Bun engine (~3s cold start,
// measured) and N processes contend on OpenCode's single global SQLite DB —
// both upstream WONTFIXes. The HTTP server is the surface OpenCode itself
// recommends for programmatic clients (its own TUI is one), sessions are
// cheap server-side objects, and one SSE stream carries every session's
// events. See docs/0011-opencode-provider-plan.md, "Performance addendum".
//
// The daemon owns one long-lived `opencode serve` child bound to loopback on
// an ephemeral port, restarts it if it dies, and multiplexes all sessions
// over it.
package opencodehttp

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
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Config reuses the shared agent config shape (Bin, DefaultCWD, Model,
// AlwaysApprove, PermissionTimeout, TurnStallNotice are honoured; Args and
// Prewarm are ACP-specific and ignored here).
type Config = acpagent.Config

// serverStartTimeout bounds spawn → /global/health healthy.
const serverStartTimeout = 60 * time.Second

// Provider manages the shared `opencode serve` engine and its SSE stream.
type Provider struct {
	cfg Config
	log *slog.Logger

	mu       sync.Mutex
	cmd      *exec.Cmd
	baseURL  string
	starting bool
	closed   bool
	// defaultModelProvider/ID is the engine-catalog fallback applied to
	// prompts when neither session nor config names a model.
	defaultModelProvider string
	defaultModelID       string
	// sessions routes SSE events by OpenCode session id.
	sessions map[string]*session
	// generation increments per server (re)start so stale monitors are inert.
	generation int

	httpc *http.Client
}

// New creates the provider. The server is spawned lazily (or via EnsureServer).
func New(cfg Config) *Provider {
	return NewWithLogger(cfg, nil)
}

// NewWithLogger creates the provider with a logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = "opencode"
	}
	l := slog.Default()
	if log != nil {
		l = log
	}
	return &Provider{
		cfg:      cfg,
		log:      l.With(slog.String("component", "provider.opencode-http")),
		sessions: make(map[string]*session),
		httpc: &http.Client{
			// Per-request timeouts are set via context; SSE needs no global cap.
			Timeout: 0,
		},
	}
}

func (p *Provider) ID() provider.ID { return provider.IDOpencode }

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
			p.log.Warn("opencode serve pre-start failed", slog.String("err", err.Error()))
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
	cmd := exec.Command(p.cfg.Bin, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
	procutil.SetProcessGroup(cmd)
	home, _ := os.UserHomeDir()
	if home != "" {
		cmd.Dir = home
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s serve: %w", p.cfg.Bin, err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Poll health until the engine is up.
	deadline := time.Now().Add(serverStartTimeout)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			_ = procutil.KillProcessGroup(cmd.Process)
			_ = cmd.Wait()
			return "", fmt.Errorf("opencode serve did not become healthy in %s", serverStartTimeout)
		}
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url+"/global/health", nil)
		res, err := p.httpc.Do(req)
		cancel()
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	p.mu.Lock()
	p.cmd = cmd
	p.generation++
	gen := p.generation
	p.mu.Unlock()

	p.log.Info("opencode serve ready", slog.String("url", url))

	// Server-mode default-model resolution is broken upstream (it resolves a
	// legacy "zen/…" alias that its own catalog rejects), so resolve a
	// working default from the engine's catalog: OpenCode's zero-auth Zen
	// tier when present. Sessions with an explicit model are unaffected.
	p.resolveDefaultModel(ctx, url)

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
		p.log.Warn("opencode serve exited", slog.Any("err", err))
		for _, s := range sessions {
			s.serverDied()
		}
	}()

	// One SSE stream for every session on this server generation.
	go p.pumpEvents(url, gen)

	return url, nil
}

// resolveDefaultModel picks a usable fallback model from the catalog.
func (p *Provider) resolveDefaultModel(ctx context.Context, url string) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url+"/provider", nil)
	if err != nil {
		return
	}
	res, err := p.httpc.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	var out struct {
		Default map[string]string `json:"default"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&out) != nil {
		return
	}
	if m := out.Default["opencode"]; m != "" {
		p.mu.Lock()
		p.defaultModelProvider, p.defaultModelID = "opencode", m
		p.mu.Unlock()
		p.log.Info("opencode default model resolved",
			slog.String("model", "opencode/"+m))
	}
}

// fallbackModel returns the catalog default for prompts with no model.
func (p *Provider) fallbackModel() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.defaultModelProvider, p.defaultModelID
}

// pumpEvents reads /global/event and routes each event to its session.
// Reconnects with backoff while this server generation is alive.
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
	req, err := http.NewRequest(http.MethodGet, url+"/global/event", nil)
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
		// /global/event wraps each event as {directory, project, payload:{…}};
		// per-directory /event streams the bare {type, properties} form.
		// Accept both.
		var frame struct {
			Payload struct {
				Type       string          `json:"type"`
				Properties json.RawMessage `json:"properties"`
			} `json:"payload"`
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(line[len("data: "):], &frame); err != nil {
			continue
		}
		ev := struct {
			Type       string
			Properties json.RawMessage
		}{frame.Type, frame.Properties}
		if frame.Payload.Type != "" {
			ev.Type = frame.Payload.Type
			ev.Properties = frame.Payload.Properties
		}
		sid := sessionIDOf(ev.Properties)
		if sid == "" {
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
			s.handleEvent(ev.Type, ev.Properties)
		}
	}
	return sc.Err()
}

// sessionIDOf pulls properties.sessionID (or nested part/info sessionID).
func sessionIDOf(props json.RawMessage) string {
	var probe struct {
		SessionID string `json:"sessionID"`
		Part      struct {
			SessionID string `json:"sessionID"`
		} `json:"part"`
		Info struct {
			SessionID string `json:"sessionID"`
		} `json:"info"`
	}
	if json.Unmarshal(props, &probe) != nil {
		return ""
	}
	if probe.SessionID != "" {
		return probe.SessionID
	}
	if probe.Part.SessionID != "" {
		return probe.Part.SessionID
	}
	return probe.Info.SessionID
}

func (p *Provider) register(s *session) {
	p.mu.Lock()
	p.sessions[s.ocID] = s
	p.mu.Unlock()
}

func (p *Provider) unregister(ocID string) {
	p.mu.Lock()
	delete(p.sessions, ocID)
	p.mu.Unlock()
}

// api performs a JSON request against the engine.
func (p *Provider) api(ctx context.Context, method, path string, body any, out any) error {
	p.mu.Lock()
	url := p.baseURL
	p.mu.Unlock()
	if url == "" {
		return fmt.Errorf("opencode serve not running")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url+path, rd)
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
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
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

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// splitModel turns "provider/model" into its OpenCode parts. A bare name is
// treated as an opencode Zen model.
func splitModel(m string) (providerID, modelID string) {
	m = strings.TrimSpace(m)
	if m == "" {
		return "", ""
	}
	if i := strings.IndexByte(m, '/'); i > 0 {
		return m[:i], m[i+1:]
	}
	return "opencode", m
}
