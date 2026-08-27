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
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/launch"
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

	// Cached vendor catalog (MADR 0074 D16). Guarded by its own mutex because
	// it is read on the phone's paging path, not on the engine lifecycle path.
	authCatalogMu     sync.Mutex
	authCatalog       *provider.AuthCatalog
	authCatalogExpiry time.Time

	// Connected-set cache and mutation ring (MADR 0086 D13 Layer 0).
	connectedMu sync.Mutex
	connected   connectedCache
	mutations   [mutationRingCap]credMutation
	mutHead     int
	mutSeq      uint64
	// Single-flight for Layer 3 GET /provider.
	sfMu  sync.Mutex
	sfCh  chan struct{}
	sfIDs map[string]struct{}
	sfErr error

	mu sync.Mutex
	// eng is the current engine, or nil when none is running.
	eng      *engine
	starting bool
	closed   bool
	// sessions routes SSE events by parent agent-side session id.
	sessions map[string]*session

	// lastDiagnosticsMark is when the last diagnostics-change marker went out,
	// for debouncing bursts. nowFn is overridable so tests can drive the
	// window with a fake clock instead of sleeping.
	lastDiagnosticsMark time.Time
	// nowFn is the clock, overridable in tests so debounce boundaries can be
	// driven exactly rather than by sleeping.
	nowFn func() time.Time

	// instanceMu serialises engine-instance recycling against ordinary work.
	// Separate from mu, which guards the routing table: holding mu across a
	// disposal HTTP call would stall every unrelated lookup.
	instanceMu sync.RWMutex
	// childAliases maps child agent-session ids → the parent *session that
	// owns the user-visible transcript (MADR 0020). Cleared with the parent.
	childAliases map[string]*session
	// generation increments per server (re)start so stale monitors are inert.
	generation int

	httpc *http.Client
	// catalogs memoizes model catalogs. Without it every picker open cost a
	// fresh engine fetch — 4.3 MB and ~0.9 s for OpenCode's /provider (MADR
	// 0043 D4). Invalidated on engine (re)start, since a new engine may have a
	// different catalog.
	catalogs *picker.Cache[string]
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
	p := &Provider{
		dialect:      d,
		cfg:          cfg,
		log:          l.With(slog.String("component", "provider."+string(d.ID())+"-http")),
		sessions:     make(map[string]*session),
		childAliases: make(map[string]*session),
		catalogs:     picker.NewCache[string](0),
		httpc: &http.Client{
			// Per-request timeouts are set via context; SSE needs no global cap.
			Timeout: 0,
		},
	}
	p.connected.ids = map[string]struct{}{}
	p.connected.negUntil = map[string]time.Time{}
	return p
}

// ID implements [provider.Provider].
func (p *Provider) ID() provider.ID { return p.dialect.ID() }

// Ready implements [provider.Provider].
func (p *Provider) Ready() bool {
	_, err := launch.Resolve(p.cfg.Bin)
	return err == nil
}

// Catalog cache keys. The model catalog is keyed by the model provider it
// covers, with these two sentinels for the whole-set queries; a real model
// provider id can never collide because both contain a character engines do
// not use in provider ids.
const (
	cacheKeyDefaultModels  = "\x00models"
	cacheKeyModelProviders = "\x00providers"
)

// ListModels implements [provider.ModelCatalog]: the provider's default model
// catalog. For a dialect that groups models by model provider, that is its
// connected set — never the full catalog, which for OpenCode is 5,788 models
// and half the WebSocket frame budget (MADR 0043 D2).
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	ml, ok := p.dialect.(ModelLister)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true), nil
	}
	return p.catalogs.Get(ctx, cacheKeyDefaultModels, func(ctx context.Context) (picker.Catalog, error) {
		static := ml.StaticModels(p.cfg)
		live, err := p.liveCatalog(ctx, func(ctx context.Context, api API) (picker.Catalog, error) {
			return ml.ListModelsLive(ctx, api)
		})
		if err != nil {
			p.log.Debug("list models: live unavailable; static catalog",
				slog.String("err", err.Error()))
			return static, nil
		}
		// Config is the daemon operator's pre-session policy. A live engine
		// catalog supplies options and labels, but its own default must not
		// make the picker claim a different model than Start will actually use.
		if len(static.Options) == 0 {
			return withConfiguredDefault(live.Normalize(), p.cfg.Model), nil
		}
		if _, scoped := p.dialect.(ModelProviderLister); scoped {
			// A dialect that enumerates model providers has an authoritative
			// connected set, and the static list is not scoped to it. Merging
			// would put back exactly the unconfigured providers the default
			// catalog exists to leave out (MADR 0043 D2); they stay reachable
			// through the provider step.
			return withConfiguredDefault(live.Normalize(), p.cfg.Model), nil
		}
		merged := picker.MergeLiveStatic(live, static)
		return withConfiguredDefault(merged, p.cfg.Model), nil
	})
}

// ListModelProviders implements [provider.ModelProviderCatalog].
func (p *Provider) ListModelProviders(ctx context.Context) (picker.Catalog, error) {
	mpl, ok := p.dialect.(ModelProviderLister)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", false), nil
	}
	return p.catalogs.Get(ctx, cacheKeyModelProviders, func(ctx context.Context) (picker.Catalog, error) {
		return p.liveCatalog(ctx, mpl.ListModelProvidersLive)
	})
}

// ListModelsFor implements [provider.ModelProviderCatalog].
func (p *Provider) ListModelsFor(ctx context.Context, modelProvider string) (picker.Catalog, error) {
	mpl, ok := p.dialect.(ModelProviderLister)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	return p.catalogs.Get(ctx, modelProvider, func(ctx context.Context) (picker.Catalog, error) {
		cat, err := p.liveCatalog(ctx, func(ctx context.Context, api API) (picker.Catalog, error) {
			return mpl.ListModelsForLive(ctx, api, modelProvider)
		})
		if err != nil {
			// No static fallback per model provider — the offline list is not
			// provider-scoped — but keep free text so a known id still works.
			p.log.Debug("list models for provider: live unavailable",
				slog.String("model_provider", modelProvider),
				slog.String("err", err.Error()))
			return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
		}
		return withConfiguredDefault(cat, p.cfg.Model), nil
	})
}

// liveCatalog runs one dialect catalog fetch against a healthy engine,
// booting it only if this is the first touch. Bounds the wait so a hung engine
// cannot stall the WS handler.
func (p *Provider) liveCatalog(ctx context.Context, fetch func(context.Context, API) (picker.Catalog, error)) (picker.Catalog, error) {
	if !p.Ready() {
		return picker.Catalog{}, fmt.Errorf("%s not installed", p.cfg.Bin)
	}
	base, err := p.ensureServer(ctx)
	if err != nil {
		return picker.Catalog{}, err
	}
	return fetch(ctx, p.apiAt(base))
}

// withConfiguredDefault makes a daemon config model authoritative for a
// pre-session picker without throwing away the live option catalog.
func withConfiguredDefault(c picker.Catalog, model string) picker.Catalog {
	if model != "" {
		c.DefaultIDs = []string{model}
	}
	return c
}

// ListAgents implements [provider.AgentCatalog].
func (p *Provider) ListAgents(ctx context.Context) (picker.Catalog, error) {
	al, ok := p.dialect.(AgentLister)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	static := al.StaticAgents(p.cfg)
	if !p.Ready() {
		return static, nil
	}
	base, err := p.ensureServer(ctx)
	if err != nil {
		p.log.Debug("list agents: engine unavailable; static catalog",
			slog.String("err", err.Error()))
		return static, nil
	}
	live, err := al.ListAgentsLive(ctx, p.apiAt(base))
	if err != nil {
		p.log.Debug("list agents: live fetch failed; static catalog",
			slog.String("err", err.Error()))
		return static, nil
	}
	if len(static.Options) == 0 {
		return live.Normalize(), nil
	}
	return picker.MergeLiveStatic(live, static), nil
}

// ListAgentSessions implements [provider.AgentSessionLister] for dialects that
// implement [AgentSessionDiscoverer].
//
// Unlike the catalog listers above, discovery has no static fallback and does
// not degrade to an empty result: the engine must actually be reachable, since
// an empty picker that silently meant "the engine is down" would invite the
// user to start a duplicate session.
func (p *Provider) ListAgentSessions(ctx context.Context) ([]provider.AgentSessionMeta, error) {
	d, ok := p.dialect.(AgentSessionDiscoverer)
	if !ok {
		return nil, fmt.Errorf("%s does not support native session discovery", p.ID())
	}
	if !p.Ready() {
		return nil, fmt.Errorf("%s binary not found", p.cfg.Bin)
	}
	base, err := p.ensureServer(ctx)
	if err != nil {
		return nil, err
	}
	return d.ListAgentSessionsLive(ctx, p.apiAt(base))
}

// ListProjects implements [provider.ProjectCatalog] for dialects that implement
// [ProjectDiscoverer]. Same reachability rule as ListAgentSessions.
func (p *Provider) ListProjects(ctx context.Context) ([]provider.ProjectMeta, error) {
	d, ok := p.dialect.(ProjectDiscoverer)
	if !ok {
		return nil, fmt.Errorf("%s does not support project discovery", p.ID())
	}
	if !p.Ready() {
		return nil, fmt.Errorf("%s binary not found", p.cfg.Bin)
	}
	base, err := p.ensureServer(ctx)
	if err != nil {
		return nil, err
	}
	return d.ListProjectsLive(ctx, p.apiAt(base))
}

// CommandTable implements [command.Tabler] by delegating to the dialect. A
// dialect that declares nothing leaves every canonical command on its default.
func (p *Provider) CommandTable() command.Table {
	ct, ok := p.dialect.(CommandTabler)
	if !ok {
		return nil
	}
	return ct.CommandTable()
}

// ListCommands implements [provider.CommandCatalog].
func (p *Provider) ListCommands(ctx context.Context) (picker.Catalog, error) {
	cl, ok := p.dialect.(CommandLister)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	static := cl.StaticCommands(p.cfg)
	if !p.Ready() {
		return static, nil
	}
	base, err := p.ensureServer(ctx)
	if err != nil {
		p.log.Debug("list commands: engine unavailable; static catalog",
			slog.String("err", err.Error()))
		return static, nil
	}
	live, err := cl.ListCommandsLive(ctx, p.apiAt(base))
	if err != nil {
		p.log.Debug("list commands: live fetch failed; static catalog",
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
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("engine pre-start panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
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

// engineBootDrainTimeout bounds how long Shutdown waits for an engine that is
// still booting. Boot is ~3-5s, so this covers it with margin while staying
// far inside systemd's TimeoutStopSec.
const engineBootDrainTimeout = 10 * time.Second

// Shutdown stops the engine (daemon exit). SIGTERM first so the engine can
// flush its session storage, escalating to SIGKILL only if it does not go.
func (p *Provider) Shutdown() {
	p.mu.Lock()
	p.closed = true
	eng := p.eng
	starting := p.starting
	p.mu.Unlock()

	// A shutdown that lands inside the engine's boot window sees a live
	// process that has not been published yet: p.eng is still nil, so taking it
	// here would find nothing and the engine would outlive us. startServer
	// re-checks p.closed the moment it goes healthy and stops the engine
	// itself — but only while this process is alive to run that code, so give
	// it a bounded chance to finish rather than exiting out from under it.
	if eng == nil && starting {
		p.log.Info("waiting for in-flight engine boot before shutdown",
			slog.Duration("timeout", engineBootDrainTimeout))
		deadline := time.Now().Add(engineBootDrainTimeout)
		for time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			p.mu.Lock()
			eng, starting = p.eng, p.starting
			p.mu.Unlock()
			if eng != nil || !starting {
				break
			}
		}
	}

	p.mu.Lock()
	eng = p.eng
	p.eng = nil
	p.mu.Unlock()
	if eng == nil || eng.cmd == nil || eng.cmd.Process == nil {
		// Either no engine ran, or the in-flight boot stopped its own process
		// on seeing p.closed. Nothing left to signal.
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
	// Process supervision: SetProcessGroup places the engine in its own group;
	// SetDeathSignal (Linux Pdeathsig) SIGKILLs the engine if the daemon dies
	// un-gracefully; TerminateProcessGroup sends SIGTERM-then-SIGKILL to the group
	// on normal teardown. Residual gap: if the daemon is killed with SIGKILL,
	// Pdeathsig terminates the engine, but any grandchild that escaped its group
	// via setsid will survive (procutil.FindByEnv / OwnerAlive exist for a future reaper).
	procutil.SetDeathSignal(cmd)
	engineID := uuid.NewString()
	// Stamp ownership into the environment. This is the ONLY thing that later
	// authorises killing a process: argv is not enough, because an engine a
	// human started by hand is indistinguishable from ours on the command line.
	cmd.Env = append(os.Environ(),
		procutil.EnvEngineID+"="+engineID,
		procutil.EnvEngineOwner+"="+procutil.OwnerToken(),
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
	lease, regErr := procutil.RegisterEngine("", procutil.EngineRecord{
		ID:       engineID,
		Provider: string(p.dialect.ID()),
		PID:      cmd.Process.Pid,
		PGID:     cmd.Process.Pid,
		Owner:    procutil.OwnerToken(),
	})
	if regErr != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		return "", fmt.Errorf("register engine: %w", regErr)
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
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("cmd wait panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		waitCh <- cmd.Wait()
		_ = procutil.RemoveEngine(lease)
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
				provider.LogStderrTail(p.log, p.cfg.Bin, tail)
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
			body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				if hh, ok := p.dialect.(HealthyHook); ok {
					if herr := hh.OnHealthy(body); herr != nil {
						_ = procutil.KillProcessGroup(cmd.Process)
						<-waitCh
						return "", fmt.Errorf("%s health check rejected: %w", p.cfg.Bin, herr)
					}
				}
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
				provider.LogStderrTail(p.log, p.cfg.Bin, tail)
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

	// A catalog cached from the previous engine describes a process that is
	// gone; its models may not be the ones this engine will accept.
	p.catalogs.Invalidate()

	p.log.Info("engine ready", slog.String("bin", p.cfg.Bin), slog.String("url", url))

	// Dialect boot hook (e.g. catalog model refine) runs async: OpenCode's
	// /provider payload is multi-MB and must not delay the first session
	// create. Dialects that need a fallback must seed one before AfterBoot.
	go func() {
		p.dialect.AfterBoot(context.WithoutCancel(ctx), p.apiAt(url))
		// The catalog that AfterBoot just resolved is what decides a session's
		// advertised prompt capabilities and reasoning rungs. A session created
		// before it landed started conservatively false, so re-emit now rather
		// than leaving it wrong until a restart (MADR 0112 A2, PLAN P3 step 2).
		p.refineSessions(gen)
	}()

	// Death monitor: mark the server gone so the next Start respawns, and
	// fail every live session (their server-side state is unreachable).
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
			if typ, props, sid, ok := p.dialect.DecodeFrame(line[len("data: "):]); ok && sid == "" {
				// Engine-global frame: no session owns it. The dialect decides
				// whether it invalidates diagnostics; nothing from the payload
				// travels any further (PLAN P7 step 10).
				if ed, hasHook := p.dialect.(EngineEventDialect); hasHook &&
					ed.EngineEventNeedsDiagnostics(typ) {
					p.noteDiagnosticsChanged(gen)
				}
			} else if ok && sid != "" {
				p.mu.Lock()
				stale := p.generation != gen
				s := p.lookupSessionLocked(sid, props)
				p.mu.Unlock()
				if stale {
					return nil
				}
				if s != nil {
					s.dispatch(typ, props, sid)
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

// instanceOp guards operations that must not interleave with recycling an
// engine instance (MADR 0112 A10, PLAN P7 step 6).
//
// Ordinary work — start, prompt, shell, close — takes the *read* lock, so it
// runs concurrently with itself as it always has. Only a skill refresh takes
// the write lock, and it holds it across the busy check, the disposal call and
// the catalog reload. Doing the check under the same lock as the disposal is
// the whole point: checking first and disposing after would let a prompt start
// in the gap and be destroyed by the recycle.
//
// The lock is provider-wide rather than per-directory, which briefly pauses new
// starts and prompts in *every* project for the bounded duration of one
// refresh. That is the conservative trade: OpenCode keys instances by
// normalized directory, and a per-key lock would have to be taken before the
// key is known.
func (p *Provider) instanceReadLock() func() {
	p.instanceMu.RLock()
	return p.instanceMu.RUnlock
}

// normalizeInstanceKey matches how OpenCode keys an instance by directory, so
// "busy" is decided on the same identity the engine would use.
func normalizeInstanceKey(dir string) string {
	cleaned := filepath.Clean(strings.TrimSpace(dir))
	if cleaned == "." {
		return ""
	}
	return strings.TrimSuffix(cleaned, string(filepath.Separator))
}

// busyInInstance reports a registered session in the target directory that is
// doing work a recycle would destroy.
//
// Caller holds instanceMu for writing. Idle sessions in *other* directories are
// deliberately not busy: recycling one project's instance does not touch
// another's, and refusing on their account would make refresh unusable on a
// busy machine.
func (p *Provider) busyInInstance(target string) bool {
	p.mu.Lock()
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	p.mu.Unlock()

	for _, s := range sessions {
		if normalizeInstanceKey(s.CWD()) != target {
			continue
		}
		if s.busyForRefresh() {
			return true
		}
	}
	return false
}

// RefreshInstance recycles the engine instance for dir, then runs reload.
//
// It refuses rather than waiting when the target is busy: a refresh exists to
// make a just-written skill visible, and blocking behind a long turn would turn
// a quick confirmation into an unexplained hang.
func (p *Provider) RefreshInstance(ctx context.Context, dir string, dispose, reload func(context.Context) error) error {
	p.instanceMu.Lock()
	defer p.instanceMu.Unlock()

	target := normalizeInstanceKey(dir)
	if p.busyInInstance(target) {
		return provider.ErrInstanceBusy
	}
	if err := dispose(ctx); err != nil {
		return err
	}
	// Reload happens under the same write lock so a new operation cannot win
	// the race and observe a disposed-but-not-reloaded instance.
	return reload(ctx)
}

// diagnosticsDebounce bounds how often a diagnostics-change marker is emitted.
// Engine events arrive in bursts — a language server restarting emits several
// in a row — and one marker per burst is all a client needs to re-request.
const diagnosticsDebounce = 500 * time.Millisecond

// clock returns the provider's time source.
func (p *Provider) clock() time.Time {
	if p.nowFn != nil {
		return p.nowFn()
	}
	return time.Now()
}

// noteDiagnosticsChanged emits at most one marker per registered session per
// debounce window.
//
// The window is per provider rather than per session: the events are global, so
// two sessions seeing the same burst should not produce two rounds of markers
// at different offsets.
func (p *Provider) noteDiagnosticsChanged(gen int) {
	now := p.clock()
	p.mu.Lock()
	if p.closed || p.generation != gen {
		p.mu.Unlock()
		return
	}
	if !p.lastDiagnosticsMark.IsZero() && now.Sub(p.lastDiagnosticsMark) < diagnosticsDebounce {
		p.mu.Unlock()
		return
	}
	p.lastDiagnosticsMark = now
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	p.mu.Unlock()

	for _, s := range sessions {
		s.Emit(event.Event{Type: event.TypeDiagnosticsChanged})
	}
}

// refineSessions asks every registered session of this generation to re-resolve
// and re-emit its model surface after an asynchronous catalog refresh.
//
// It mirrors resyncSessions: snapshot under the lock, then act outside it. A
// session closed or re-modelled in the meantime is harmless — the re-emit is
// idempotent and a closed session drops it.
func (p *Provider) refineSessions(gen int) {
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
		s.refineModelSurface()
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
	// A parent id must not collide with a live child alias owned by someone else.
	if owner, ok := p.childAliases[s.agentID]; ok && owner != s {
		return fmt.Errorf("agent session %s is already a child alias", s.agentID)
	}
	p.sessions[s.agentID] = s
	return nil
}

// unregister removes s from the routing table only if s is still the registered
// owner of its id — so a late Close from a rejected/replaced session cannot
// evict the session that currently holds the id. Also drops every child alias
// that pointed at s.
func (p *Provider) unregister(s *session) {
	p.mu.Lock()
	if p.sessions[s.agentID] == s {
		delete(p.sessions, s.agentID)
	}
	for id, owner := range p.childAliases {
		if owner == s {
			delete(p.childAliases, id)
		}
	}
	p.mu.Unlock()
}

// lookupSessionLocked resolves the local *session for an SSE frame sid.
// Callers hold p.mu. Bootstrap: if sid is unknown, try parentID from props
// (ChildFrame) so the first session.created for a child is not dropped
// (MADR 0020 chicken-and-egg).
func (p *Provider) lookupSessionLocked(sid string, props json.RawMessage) *session {
	if s := p.sessions[sid]; s != nil {
		return s
	}
	// KD11 kill switch: parent-only demux (no aliases, no bootstrap bind).
	if !p.cfg.treeEnabled() {
		return nil
	}
	if s := p.childAliases[sid]; s != nil {
		return s
	}
	parentID := ""
	if cf, ok := p.dialect.(ChildFrame); ok {
		parentID = cf.ParentIDFromProps(props)
	}
	if parentID == "" {
		return nil
	}
	owner := p.sessions[parentID]
	if owner == nil {
		owner = p.childAliases[parentID] // nested: mid-node is only in aliases
	}
	if owner == nil {
		return nil
	}
	// Eager alias so subsequent child frames hit without re-parsing parentID.
	// Reject if this id is already a different parent session.
	if existing, ok := p.sessions[sid]; ok && existing != owner {
		return nil
	}
	p.childAliases[sid] = owner
	owner.noteChildBound(sid)
	return owner
}

// bindChild inserts a child alias under parent. No-op if already bound to the
// same parent; refuses if bound to a different live parent or if sid is a
// top-level sessions key owned by someone else.
func (p *Provider) bindChild(childID string, parent *session) {
	if !p.cfg.treeEnabled() {
		return
	}
	if childID == "" || parent == nil || childID == parent.agentID {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.sessions[childID]; ok && existing != parent {
		return
	}
	if owner, ok := p.childAliases[childID]; ok && owner != parent {
		return
	}
	p.childAliases[childID] = parent
}

// unbindChild removes a child alias only if parent still owns it.
func (p *Provider) unbindChild(childID string, parent *session) {
	if childID == "" || parent == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.childAliases[childID] == parent {
		delete(p.childAliases, childID)
	}
}

// childrenOf returns agent ids currently aliased to parent (snapshot).
func (p *Provider) childrenOf(parent *session) []string {
	if parent == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0)
	for id, owner := range p.childAliases {
		if owner == parent {
			out = append(out, id)
		}
	}
	return out
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
