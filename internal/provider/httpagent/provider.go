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

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// serverStartTimeout bounds spawn → health-path healthy.
const serverStartTimeout = 60 * time.Second

// engine is one running agent server process together with everything needed
// to supervise it. Grouping these keeps the "is there an engine, and where"
// question a single nil check, and gives shutdown access to the reap signal
// without threading a channel through startServer's callers.
type engine struct {
	cmd  *exec.Cmd
	url  string
	port int
	// id is this engine's MCREMOTE_ENGINE_ID, so a startup sweep can tell the
	// engine we just adopted responsibility for from one to reap.
	id string
	// dead is closed once cmd has been reaped. It is closed rather than
	// written to so any number of waiters can observe the exit without
	// stealing the exit status from the single cmd.Wait owner.
	dead chan struct{}
}

// Provider manages one shared engine process and its SSE stream for a
// [Dialect].
type Provider struct {
	dialect Dialect
	cfg     Config
	log     *slog.Logger

	mu sync.Mutex
	// eng is the current engine, or nil when none is running.
	eng      *engine
	starting bool
	closed   bool
	// sessions routes SSE events by agent-side session id.
	sessions map[string]*session
	// generation increments per server (re)start so stale monitors are inert.
	generation int

	httpc *http.Client
}

// engineURL returns the current engine's base URL, or "" when none is running.
func (p *Provider) engineURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng == nil {
		return ""
	}
	return p.eng.url
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

// ListModels implements [provider.ModelCatalog].
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	ml, ok := p.dialect.(ModelLister)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true), nil
	}
	static := ml.StaticModels(p.cfg)
	if !p.Ready() {
		return static, nil
	}
	// Prefer an already-running engine; only boot if models.list is the first
	// touch. Bound the wait so a hung engine cannot stall the WS handler.
	base, err := p.ensureServer(ctx)
	if err != nil {
		p.log.Debug("list models: engine unavailable; static catalog",
			slog.String("err", err.Error()))
		return static, nil
	}
	live, err := ml.ListModelsLive(ctx, p.apiAt(base))
	if err != nil {
		p.log.Debug("list models: live fetch failed; static catalog",
			slog.String("err", err.Error()))
		return static, nil
	}
	if len(static.Options) == 0 {
		return live.Normalize(), nil
	}
	return picker.MergeLiveStatic(live, static), nil
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

// engineStopTimeout bounds the graceful half of engine shutdown before the
// SIGKILL escalation. The engine flushes a session store on SIGTERM; a few
// seconds is generous for that and still well inside systemd's stop timeout.
const engineStopTimeout = 5 * time.Second

// Shutdown stops the engine (daemon exit). SIGTERM first so the engine can
// flush its session storage, escalating to SIGKILL only if it does not go.
func (p *Provider) Shutdown() {
	p.mu.Lock()
	p.closed = true
	eng := p.eng
	p.eng = nil
	p.mu.Unlock()
	if eng == nil || eng.cmd == nil || eng.cmd.Process == nil {
		return
	}
	graceful := procutil.TerminateProcessGroup(eng.cmd.Process, eng.dead, engineStopTimeout)
	p.log.Info("engine stopped",
		slog.String("bin", p.cfg.Bin),
		slog.Int("pid", eng.cmd.Process.Pid),
		slog.Bool("graceful", graceful),
	)
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
	// baseURL is published inside startServer (before pumpEvents spawns); here we
	// only clear the starting gate.
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
	cmd := exec.Command(p.cfg.Bin, p.dialect.ServeArgs(port)...)
	procutil.SetProcessGroup(cmd)
	// Die with the daemon if it is killed without running Shutdown. Best-effort
	// (see SetDeathSignal); ReapOrphans at the next startup is the backstop.
	procutil.SetDeathSignal(cmd)
	engineID := uuid.NewString()
	// Stamp ownership into the environment. This is the ONLY thing that later
	// authorises killing a process: argv is not enough, because an engine a
	// human started by hand is indistinguishable from ours on the command line.
	cmd.Env = append(os.Environ(),
		EnvEngineID+"="+engineID,
		EnvEngineOwner+"="+procutil.OwnerToken(),
	)
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

	// Single reaper: exactly one cmd.Wait ever runs, on this goroutine, and its
	// result is delivered over waitCh. The health poll watches waitCh so an
	// engine that dies instantly (bad binary, immediate crash) fails startup at
	// once instead of spinning the full serverStartTimeout on connection-refused;
	// the timeout/shutdown branches and the post-boot death monitor all consume
	// this same channel rather than calling Wait a second time (a double-Wait
	// races and reports a bogus error).
	//
	// dead carries the same news to observers that must NOT consume the status —
	// Shutdown's graceful terminate waits on it while the death monitor is still
	// parked on waitCh.
	waitCh := make(chan error, 1)
	dead := make(chan struct{})
	go func() {
		waitCh <- cmd.Wait()
		close(dead)
	}()

	// Poll health until the engine is up.
	deadline := time.Now().Add(serverStartTimeout)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			_ = procutil.KillProcessGroup(cmd.Process)
			<-waitCh
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
		// Wait 50ms before the next probe — a tight poll, since engine boot is
		// the cold-start critical path (~3–5s) — but bail out the instant the
		// process exits rather than probing a corpse until the deadline. The
		// receive here is the sole consumer of waitCh on the early-exit path, so
		// no other Wait runs; the process is already reaped, so no kill is needed.
		select {
		case <-waitCh:
			tail := stderr.tail()
			if tail != "" {
				return "", fmt.Errorf("%s server exited during startup; recent stderr:\n%s", p.cfg.Bin, tail)
			}
			return "", fmt.Errorf("%s server exited during startup", p.cfg.Bin)
		case <-time.After(50 * time.Millisecond):
		}
	}

	p.mu.Lock()
	if p.closed {
		// Shutdown ran during the health poll: it already cleared p.eng and
		// killed nothing (there was no engine yet), so this freshly-healthy
		// process would leak past daemon exit. Stop it here instead of
		// registering it — gracefully, since it is healthy and holds state.
		p.mu.Unlock()
		procutil.TerminateProcessGroup(cmd.Process, dead, engineStopTimeout)
		<-waitCh
		return "", fmt.Errorf("provider shut down")
	}
	// Publish the engine here — under the same lock that bumps generation and
	// BEFORE pumpEvents is spawned below. If we left this to ensureServer (after
	// startServer returns), pumpEvents could run its first liveness check
	// (p.eng.url == url) before it was set, see no engine, and exit immediately —
	// permanently killing the SSE stream for this engine generation.
	p.eng = &engine{cmd: cmd, url: url, port: port, dead: dead, id: engineID}
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
		alive := p.generation == gen && p.eng != nil && p.eng.url == url && !p.closed
		p.mu.Unlock()
		if !alive {
			return
		}

		start := time.Now()
		err := p.streamOnce(url, gen)
		if err != nil {
			p.log.Debug("sse stream ended", slog.String("err", err.Error()))
		}
		// A stream that stayed up for a while was healthy: reset the backoff so a
		// single blip after hours of uptime does not inherit a 10s reconnect
		// delay (which would widen the event-loss window on the next drop).
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
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

	// H4: frames emitted while the stream was down are gone — the engine does
	// not replay. If one of them was a turn-end, the session would show
	// "running" forever (and refuse new prompts). Now that the stream is up
	// again, reconcile every turn-active session against engine state. Also
	// runs on the first connect of a generation, where it is a no-op (no
	// session can have a turn before the engine is up).
	go p.resyncSessions(gen)

	// A bufio.Scanner dies permanently (ErrTooLong) on any single line past its
	// cap, which would abort the shared stream for EVERY session on this engine —
	// one oversized part.updated snapshot and reconnect would re-hit it in a loop.
	// Read with a bounded reader that discards an over-long line and keeps going.
	r := bufio.NewReaderSize(res.Body, 64*1024)
	for {
		line, tooLong, err := readSSELine(r, maxSSELine)
		if tooLong {
			p.log.Warn("dropping oversized SSE line", slog.Int("limit_bytes", maxSSELine))
		}
		if len(line) > 0 && !tooLong && bytes.HasPrefix(line, []byte("data: ")) {
			if typ, props, sid, ok := p.dialect.DecodeFrame(line[len("data: "):]); ok && sid != "" {
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
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// maxSSELine caps a single SSE line. Engine model-catalog frames are large but
// bounded; anything past this is treated as runaway and skipped, not fatal.
const maxSSELine = 10 << 20

// readSSELine reads one '\n'-terminated line, trimming the trailing CR/LF. A
// line longer than max is fully consumed (so the stream stays aligned) but
// returned as tooLong with no data, letting the caller skip it without tearing
// down the connection. err is non-nil only for a real read error or io.EOF
// (which may accompany a final unterminated line).
func readSSELine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		frag, e := r.ReadSlice('\n')
		if !tooLong {
			line = append(line, frag...)
			if len(line) > max {
				tooLong = true
				line = nil
			}
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		if tooLong {
			line = nil
		} else {
			line = bytes.TrimRight(line, "\r\n")
		}
		return line, tooLong, e
	}
}

// resyncSessions runs the SSE-gap reconciliation for every registered session
// of this generation. Sessions gate internally (turn-active only), so this is
// cheap for idle sessions; each resync is independently time-bounded.
func (p *Provider) resyncSessions(gen int) {
	p.mu.Lock()
	if p.closed || p.generation != gen {
		p.mu.Unlock()
		return
	}
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	p.mu.Unlock()
	for _, s := range sessions {
		go s.resync()
	}
}

// register routes SSE events for s by its agent-side id. It refuses to attach a
// second local session to an id already in the routing table: the incumbent is
// live and streaming it, and a silent overwrite would both hijack its events and
// (on the loser's Close) unregister the survivor.
func (p *Provider) register(s *session) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.sessions[s.agentID]; ok && existing != s {
		return fmt.Errorf("agent session %s is already attached", s.agentID)
	}
	p.sessions[s.agentID] = s
	return nil
}

// unregister removes s from the routing table only if s is still the registered
// owner of its id — so a late Close from a rejected/replaced session cannot
// evict the session that currently holds the id.
func (p *Provider) unregister(s *session) {
	p.mu.Lock()
	if p.sessions[s.agentID] == s {
		delete(p.sessions, s.agentID)
	}
	p.mu.Unlock()
}

// api performs a JSON request against the current engine.
func (p *Provider) api(ctx context.Context, method, path string, body any, out any) error {
	url := p.engineURL()
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
