// Package daemon wires configuration into a running mcremote process.
package daemon

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/debugserve"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker/saved"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/registered"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
	"github.com/maccavelli/magic-cli-remote/internal/relayhost"
	"github.com/maccavelli/magic-cli-remote/internal/sdnotify"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/tcc"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// Options for Run.
type Options struct {
	Config  config.Config
	Version string
	Log     *slog.Logger
	// Shutdown, when set, is what the admin socket's shutdown operation
	// calls. serve cancels the context its signals cancel with it, so a
	// request runs the same graceful shutdown as a signal (MADR 0054 A2).
	// Nil refuses the operation.
	Shutdown func()
}

// InspectProviders builds the providers cfg enables, for inspection only
// (mcremote doctor, MADR 0201 D32): no credential coordination, no orphan
// reaping, no engine, no prewarm. Constructing them starts nothing; callers
// must not call Start. Ready reports what daemon.Run's providers would.
func InspectProviders(cfg config.Config, version string, log *slog.Logger) *provider.Registry {
	reg := provider.NewRegistry()
	registerDescribed(reg, cfg, buildBase(cfg, version, log), nil, false)
	return reg
}

// buildBase is what every described provider is built with: mcremote's
// version, the daemon's runtime directory and its logger. Run and
// InspectProviders share it, so they cannot drift.
func buildBase(cfg config.Config, version string, log *slog.Logger) provider.Build {
	return provider.Build{Version: version, RuntimeDir: cfg.Paths.RuntimeDir, Log: log}
}

// runBase is buildBase with the instance's saved model catalogs, which only
// the running daemon reads and writes: InspectProviders keeps buildBase, so
// mcremote doctor touches none (MADR 0235 D1).
func runBase(cfg config.Config, version string, log *slog.Logger) provider.Build {
	b := buildBase(cfg, version, log)
	b.SavedCatalogs = saved.Open(cfg.Paths.CatalogDir, log)
	return b
}

// registerDescribed builds and registers each enabled provider of the
// registration list (0208 MADR D6), and returns how many it registered.
//
// base carries what every provider is built with. coordinate, when set,
// returns a provider's credential coordinator and its live-session count
// (MADR 0074 D21). live is false for inspection (mcremote doctor), which
// warns of nothing and warms nothing.
func registerDescribed(reg *provider.Registry, cfg config.Config, base provider.Build,
	coordinate func(provider.ID) (*providerauth.Coordinator, func() int), live bool) int {
	n := 0
	for _, d := range registered.Descriptors() {
		sec := d.SectionIn(cfg)
		if !sec.IsEnabled() {
			continue
		}
		b := base
		b.Section = sec
		if coordinate != nil {
			b.Credentials, b.Busy = coordinate(d.ID)
		}
		p := d.New(b)
		reg.Register(p)
		n++
		if !live {
			continue
		}
		if agent, ok := sec.(config.AgentSection); ok && !p.Ready() {
			base.Log.Warn(d.Name+" provider enabled but binary not found in PATH",
				slog.String("bin", agent.Agent().Bin))
		}
		if prewarmWants(cfg, d.ID) {
			provider.Warm(p)
		}
	}
	return n
}

// Run starts the daemon and blocks until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "daemon"))
	// Before anything is spawned: no child inherits the unit's notify socket
	// (MADR 0210 D10).
	notify := sdnotify.Take()

	cfg := opts.Config
	if err := appdirs.EnsurePrivateDir(cfg.DataDir); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	// Converge the product's own config directory too, so an installation that
	// never re-runs setup-service still repairs (MADR 0155 D1). On Windows this
	// re-propagates to the config file already inside it, which is what makes
	// the check in config.Load pass for an operator who did nothing (0155 F7).
	//
	// Scoped deliberately to the directory the product owns. A --config
	// pointing into a repository checkout or a home directory must NOT have its
	// inheritance severed: that directory belongs to the operator for other
	// purposes, and making it private is not this daemon's business. When the
	// config lives elsewhere the guard in config.Load still reports it; only
	// the automatic repair is withheld.
	if cfg.ConfigFile != "" && cfg.Paths.ConfigDir != "" &&
		filepath.Dir(cfg.ConfigFile) == filepath.Clean(cfg.Paths.ConfigDir) {
		if err := appdirs.EnsurePrivateDir(cfg.Paths.ConfigDir); err != nil {
			log.Warn("could not make the config directory private",
				slog.String("dir", cfg.Paths.ConfigDir), slog.Any("err", err))
		}
	}

	// macOS privacy probe (MADR 0069 D5): a headless daemon cannot be
	// prompted and a denial otherwise first surfaces as a confusing agent
	// failure. One stat, one warn, never fatal — sessions outside protected
	// folders are unaffected. No-op off darwin.
	if tcc.Probe() == tcc.Denied {
		log.Warn("macOS Full Disk Access not granted for this binary; " +
			"agent sessions under Documents/Desktop/Downloads will fail " +
			"with 'operation not permitted' — run `mcremote doctor` or see " +
			"docs/guides/ops-macos-tcc.md")
	}

	// No-op in release builds; `make debug` + MC_DEBUG_ADDR only (0068 P6,
	// goroutine-leak triage — docs/guides/ops-mcrelay.md).
	debugserve.Start(ctx, log)

	// Resolve the "tailscale" sentinel before anything derives from the bind
	// address (certificate SANs, the advertised listen addr, the listener).
	// Wait rather than exit: user units cannot order on system tailscaled, so
	// a fail-fast return at boot burns systemd's start limit and stays down.
	if err := cfg.ResolveListenHostWait(ctx, log); err != nil {
		return err
	}
	if cfg.Listen.Host == "0.0.0.0" {
		log.Warn("listening on 0.0.0.0 exposes the daemon beyond the tailnet "+
			"(set listen.host to \"tailscale\" to bind the mesh interface only)",
			slog.Bool("require_device_token", cfg.Auth.RequireDeviceToken),
		)
	}
	for _, d := range registered.Descriptors() {
		if d.DevOnly && d.SectionIn(cfg).IsEnabled() {
			log.Warn(d.Name + " provider is enabled (dev/smoke only)")
		}
	}

	// The client-key allowlist is bound to the mTLS peer certificate presented
	// at TLS termination. With TLS off there is no handshake and no peer cert,
	// so the fingerprint is always empty and every auth/claim/hello would 401
	// with an unrecoverable "client key required". Fail loudly here instead of
	// serving a daemon that nothing can authenticate to.
	if cfg.Auth.RequireClientKey && !cfg.TLS.Active() {
		return fmt.Errorf("auth.require_client_key is set but TLS is off (tls.mode=%s): "+
			"a client key can only be verified from an mTLS client certificate, which requires the daemon to terminate TLS. "+
			"Enable TLS (tls.mode=selfsigned or letsencrypt) or set auth.require_client_key=false",
			cfg.TLS.ResolvedMode())
	}

	store, err := auth.OpenStore(filepath.Join(cfg.DataDir, "devices.json"))
	if err != nil {
		return err
	}
	// Debounced LastUsedAt updates would otherwise drop on every restart —
	// and `pair prune --stale` treats never-flushed devices as never used.
	defer func() {
		if err := store.Flush(); err != nil {
			log.Warn("flushing device store on shutdown failed", slog.String("err", err.Error()))
		}
	}()
	pairCodes, err := auth.OpenPairCodeStore(filepath.Join(cfg.DataDir, "pair_codes.json"))
	if err != nil {
		return err
	}

	sessStore, err := session.OpenStore(cfg.DataDir)
	if err != nil {
		return err
	}

	// Before starting any engine of our own, clear out any left behind by a
	// previous daemon that died without running its shutdown path — opencode,
	// kilo, and codex all spawn theirs the same marked way. Only
	// processes carrying our ownership marker whose owner is gone are
	// touched — an engine belonging to a concurrently running mcremote is
	// left alone (MADR 0019 §5.4). Registry path is the cross-platform contract
	// (MADR 0059 D8); env markers remain Linux defense-in-depth.
	if err := cfg.RecomputePaths(); err == nil && cfg.Paths.EngineRegistryDir != "" {
		procutil.SetDefaultEngineRegistryDir(cfg.Paths.EngineRegistryDir)
		procutil.ReapOrphanEngines(log, cfg.Paths.EngineRegistryDir)
	} else {
		procutil.ReapOrphanEngines(log)
	}

	reg := provider.NewRegistry()
	// Credential transactions (MADR 0074 D21). Built before any provider so a
	// coordinated provider can be constructed with its coordinator, and before
	// any mutation so startup recovery runs first.
	guard, guardErr := newCredentialGuard(cfg.DataDir, CredentialAdapters(cfg, true), log)
	if guardErr != nil {
		return fmt.Errorf("credential coordinator: %w", guardErr)
	}
	guard.recover(ctx)

	// liveCount is bound after the session manager exists. Providers capture
	// this indirection rather than a package global, so a validated credential
	// can ask "is a session running right now?" at activation time
	// (MADR 0074 D25/P20 step 11).
	var liveCount func(provider.ID) int
	busyFor := func(id provider.ID) func() int {
		return func() int {
			if liveCount == nil {
				return 0
			}
			return liveCount(id)
		}
	}

	base := runBase(cfg, opts.Version, log)
	if base.SavedCatalogs == nil {
		log.Info("no catalog directory; model pickers have no saved catalogs")
	}
	// Deferred before the spares' shutdown, so it runs after it: a catalog
	// saved just before the daemon stops still reaches the disk.
	defer base.SavedCatalogs.Flush()
	described := registerDescribed(reg, cfg, base,
		func(id provider.ID) (*providerauth.Coordinator, func() int) {
			if c := guard.coordinator(string(id)); c != nil {
				return c, busyFor(id)
			}
			return nil, nil
		}, true)
	// Release pre-warmed spare processes on shutdown (live sessions are closed
	// by the manager; spares are provider-owned).
	defer func() {
		for _, p := range reg.All() {
			if sd, ok := p.(interface{ Shutdown() }); ok {
				sd.Shutdown()
			}
		}
	}()

	// Operator signal when every enabled provider is missing its binary (Phase 4.1).
	// registerDescribed counts every enabled provider, so none can be missed
	// (MADR 0076 M3).
	if described > 0 {
		ready := 0
		for _, p := range reg.All() {
			if p.Ready() {
				ready++
			}
		}
		if ready == 0 {
			log.Warn("no agent provider is ready (binaries missing from PATH); " +
				"session.create will fail until grok/opencode/fake is installable")
		}
	}

	limits := cfg.Limits.Resolved()
	hub := &eventHub{}
	mgr := session.NewManagerWithLimits(reg, sessStore, log, hub.Broadcast, limits.MaxLiveSessions)
	// Flush debounced session meta on process exit.
	defer mgr.FlushPersist()
	// Shutdown order is deliberate and is the mirror of startup: CloseClients
	// cancels and drains device flows before sockets close, then watchers stop,
	// then session state flushes, then providers shut down. Reversing any of
	// these would let a process exit while a credential transaction was still
	// mid-publication (MADR 0074 P20 step 14).
	defer guard.close(context.WithoutCancel(ctx))
	liveCfg := &config.Live{Path: cfg.ConfigFile, Cfg: &cfg}
	prewarm := provider.NewController(liveCfg, reg, mgr.LiveCountFor)
	liveCount = mgr.LiveCountFor
	// Compose the single idle hook: a validated credential waiting on a busy
	// provider must be published before prewarm can start a fresh process
	// against the old one (MADR 0074 P20 step 11).
	mgr.OnProviderIdle = func(id provider.ID) {
		guard.activatePending(id)
		prewarm.OnIdle(id)
	}
	// Watchers start only after recovery has settled every manifest, so a
	// watcher cannot race the startup checkpoint it depends on.
	guard.startWatchers(ctx)

	wsServer := ws.New(ws.Options{
		Store:                    store,
		PairCodes:                pairCodes,
		Sessions:                 mgr,
		Registry:                 reg,
		Prewarm:                  prewarm,
		ProviderAuthTransactions: guard.enabled(),
		RequireDeviceToken:       cfg.Auth.RequireDeviceToken,
		RequireClientKey:         cfg.Auth.RequireClientKey,
		AllowedOrigins:           cfg.Auth.AllowedOrigins,
		Version:                  opts.Version,
		ListenAddr:               cfg.Addr(),
		HeadscaleURL:             cfg.Headscale.ControlURL,
		DisplayName:              cfg.DisplayName,
		Log:                      log,
		MaxClients:               limits.MaxWSClients,
		// Contract number: advertised to v2 clients in the capability
		// block (MADR 0068 D2), enforced by the read loop.
		ReadDeadline: time.Duration(limits.WSReadDeadlineSeconds) * time.Second,
		ResumeWindow: time.Duration(limits.WSResumeWindowSeconds) * time.Second,
	})
	hub.server = wsServer
	// Codex terminal output is a live push, not session history, so it takes
	// its own sink to the WS server rather than the event hub (MADR 0109 D11).
	if codexProvider, err := reg.Get(provider.IDCodex); err == nil {
		if cp, ok := codexProvider.(*codex.Provider); ok {
			cp.SetTerminalOutputSink(hub.BroadcastTerminalOutput)
		}
	}

	// Local admin socket so `mcremote pair revoke` can kick live WS clients.
	adminErrCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("admin serve panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				adminErrCh <- fmt.Errorf("admin serve panic: %v", r)
			}
		}()
		sock := cfg.Paths.AdminSocket
		if sock == "" {
			// Tests or partial configs may lack Paths; derive from DataDir instance.
			if err := cfg.RecomputePaths(); err != nil {
				adminErrCh <- err
				return
			}
			sock = cfg.Paths.AdminSocket
		}
		if err := admin.Serve(ctx, sock, wsServer, log, opts.Shutdown); err != nil {
			adminErrCh <- err
			return
		}
		adminErrCh <- nil
	}()

	httpServer := newHTTPServer(cfg.Addr(), wsServer.Handler(), log)

	identity, err := EnsureTLS(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer identity.Close()
	if b := identity.SelfSigned; b != nil && b.Generated && b.GeneratedReason == certs.ReasonExpiring {
		log.Warn("the self-signed TLS identity expired and has been regenerated; "+
			"its fingerprint changed, so every paired device must re-pair",
			slog.String("cert_fingerprint_sha256", b.FingerprintColonHex()),
			slog.String("cert_file", b.CertPath),
		)
	}
	wsServer.SetTLSStatus(identity.Mode, identity.FellBack)
	if cfg.Receipts.Enabled {
		// Deliberately EnsureCerts(cfg) again here, not identity.SelfSigned:
		// that field is nil whenever ACME issuance succeeded or tls.mode is
		// off (MADR 0077 P7 grounding), but EnsureCerts always resolves a
		// stable, disk-persisted ECDSA key regardless of what's actually
		// serving live traffic — all D8's marker needs is *a* daemon-
		// controlled key, not necessarily the one presented over the wire.
		if daemonBundle, err := EnsureCerts(cfg); err != nil {
			log.Warn("receipts enabled but the daemon's signing key could not be "+
				"resolved; receipts will not be generated until this is fixed",
				slog.String("err", err.Error()))
		} else if daemonKey, ok := daemonBundle.Certificate.PrivateKey.(*ecdsa.PrivateKey); ok {
			if receiptStore, err := receipt.NewStore(cfg.DataDir); err != nil {
				log.Warn("receipts enabled but the receipt store could not be opened; "+
					"receipts will not be generated until this is fixed",
					slog.String("err", err.Error()))
			} else {
				mgr.SetReceiptSupport(session.ReceiptSupport{
					Config:    cfg.Receipts,
					Store:     receiptStore,
					AuthStore: store,
					DaemonKey: daemonKey,
					Transport: wsServer,
				})
			}
		} else {
			log.Warn("receipts enabled but the daemon's TLS key is not ECDSA; " +
				"receipts will not be generated")
		}
	}
	if identity.FellBack {
		// Distinct WARN, not just an attribute on the "listening" line, so it
		// is greppable/alertable. Let's Encrypt was requested but issuance
		// failed; the daemon is serving its self-signed fallback and paired
		// phones survive on the pin — but the ACME cert is not being renewed.
		log.Warn("TLS fell back to self-signed: Let's Encrypt issuance failed; "+
			"phones connect via the pinned fallback, but fix ACME before the "+
			"90-day cliff (check acme_domains and Route 53 credentials)",
			slog.Bool("tls_letsencrypt_fallback", true),
		)
	}
	serveTLS := identity.Mode != config.TLSModeOff
	if serveTLS {
		httpServer.TLSConfig = identity.Config
	} else {
		log.Warn("TLS is disabled; device tokens travel in cleartext " +
			"(set tls.mode=letsencrypt|selfsigned or drop --tls=false)")
	}

	// Kernel keepalive beneath the app deadline (MADR 0068 P1): a peer that
	// died without a FIN — a suspended phone — is reaped at ~45 s by probes
	// instead of holding its slot for the full read deadline.
	lc := net.ListenConfig{
		KeepAliveConfig: cfg.Limits.Resolved().TCPKeepalive.NetConfig(),
	}
	ln, err := lc.Listen(ctx, "tcp", cfg.Addr())
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr(), err)
	}
	attrs := []any{
		slog.String("addr", ln.Addr().String()),
		slog.Bool("tls", serveTLS),
		slog.String("tls_mode", identity.Mode),
		slog.Bool("require_device_token", cfg.Auth.RequireDeviceToken),
	}
	if identity.FellBack {
		attrs = append(attrs, slog.Bool("tls_letsencrypt_fallback", true))
	}
	if b := identity.SelfSigned; b != nil {
		attrs = append(attrs,
			slog.String("cert_fingerprint_sha256", b.FingerprintColonHex()),
			slog.Bool("cert_generated", b.Generated),
		)
		if b.Generated {
			// cert_generated=true now means one of exactly two deliberate
			// things — a first run or a renewal — never "we failed to read the
			// file and quietly changed identity".
			attrs = append(attrs, slog.String("cert_generated_reason", b.GeneratedReason))
		}
	}
	if a := identity.ACME; a != nil {
		attrs = append(attrs,
			slog.String("acme_domains", strings.Join(a.Domains, ",")),
			slog.String("acme_directory", a.Directory),
		)
	}
	log.Info("listening", attrs...)

	// Outbound mcrelay registration (MADR 0015 E2). Bridges phone tunnels to
	// this listener so off-mesh clients can complete an inner TLS hop (E3).
	if cfg.Relay.Enabled() {
		if !cfg.Relay.CanRegister() {
			// url/host_id alone are enough for pair QR advertising; serve still
			// needs the registration secret (usually MCREMOTE_RELAY_SECRET).
			return fmt.Errorf("relay.url is set but registration credentials are incomplete (need relay.host_id and relay.secret / MCREMOTE_RELAY_SECRET)")
		}
		rc := relayhost.New(relayhost.Config{
			URL:                cfg.Relay.URL,
			HostID:             cfg.Relay.HostID,
			Secret:             cfg.Relay.Secret,
			LocalAddr:          ln.Addr().String(),
			InsecureSkipVerify: cfg.Relay.InsecureSkipVerify,
			MaxFrameBytes:      cfg.Relay.MaxFrameBytes,
		}, log)
		rc.SetLocalAddr(ln.Addr().String())
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("relay host client panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				}
			}()
			if err := rc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("relay host client stopped", slog.String("err", err.Error()))
			}
		}()
		log.Info("mcrelay registration enabled",
			slog.String("relay_url", cfg.Relay.URL),
			slog.String("host_id", cfg.Relay.HostID),
		)
	}

	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("http serve panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				errCh <- fmt.Errorf("http serve panic: %v", r)
			}
		}()
		var err error
		if serveTLS {
			err = httpServer.ServeTLS(ln, "", "")
		} else {
			err = httpServer.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Under the unit's Type=notify, systemctl start returns, and the unit
	// becomes active, at READY=1: the listener is open, so the daemon serves
	// (MADR 0054 D5, amendment S8). Off such a unit NOTIFY_SOCKET is unset and
	// nothing is sent.
	notify.Notify(log, "READY=1")

	// Clear launchd's and Task Scheduler's finished handoffs in the
	// background, so start-up never waits on the service manager (MADR 0210
	// D5, D11).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("handoff cleanup panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		cleanupCtx, cancel := context.WithTimeout(ctx, handOffCleanupTimeout)
		defer cancel()
		cleanupHandOffs(cleanupCtx, runtime.GOOS, newHandOffBackend, log)
	}()

	// gracefulDrain performs the ordered shutdown: stop accepting, close the
	// hijacked WebSocket conns (http.Server.Shutdown never touches them), close
	// live sessions, then drain the serve goroutines.
	gracefulDrain := func(reason string) error {
		notify.Notify(log, "STOPPING=1")
		log.Info("shutting down", slog.String("reason", reason))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		wsServer.CloseClients()
		mgr.CloseAll(shutdownCtx)
		// Drain admin serve (ctx already cancelled closes the listener).
		select {
		case <-adminErrCh:
		case <-time.After(2 * time.Second):
		}
		return <-errCh
	}
	causeStr := func() string {
		cause := context.Cause(ctx)
		if cause == nil {
			cause = ctx.Err()
		}
		return cause.Error()
	}

	select {
	case <-ctx.Done():
		return gracefulDrain(causeStr())
	case err := <-errCh:
		notify.Notify(log, "STOPPING=1")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		wsServer.CloseClients()
		mgr.CloseAll(shutdownCtx)
		return err
	case err := <-adminErrCh:
		// A clean SIGTERM cancels ctx, which closes the admin listener and lands
		// a nil here at (almost) the same instant as ctx.Done(). Select picks a
		// ready case at random, so guard: if ctx is already cancelled this is the
		// normal shutdown, not an unexpected admin exit — take the graceful path.
		if ctx.Err() != nil {
			return gracefulDrain(causeStr())
		}
		if err != nil {
			notify.Notify(log, "STOPPING=1")
			log.Error("admin socket failed", slog.String("err", err.Error()))
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
			wsServer.CloseClients()
			mgr.CloseAll(shutdownCtx)
			return fmt.Errorf("admin socket: %w", err)
		}
		// Admin exited cleanly without ctx cancel — treat as unexpected.
		return fmt.Errorf("admin socket exited unexpectedly")
	}
}

// eventHub decouples session manager construction from the WS server.
type eventHub struct {
	server *ws.Server
}

func (h *eventHub) Broadcast(ev event.Event) {
	if h.server != nil {
		h.server.BroadcastEvent(ev)
	}
}

// BroadcastTerminalOutput forwards one bounded terminal chunk to the session
// owner's Codex-surface connections.
func (h *eventHub) BroadcastTerminalOutput(sessionID string, chunk provider.TerminalOutput) {
	if h.server != nil {
		h.server.BroadcastCodexTerminalOutput(sessionID, chunk)
	}
}

// prewarmPlan is the set of enabled providers whose engine should start at
// serve boot (MADR 0089 D5). Empty when every default is off.
func prewarmPlan(cfg config.Config) []provider.ID {
	var out []provider.ID
	for _, d := range registered.Descriptors() {
		if agent, ok := d.SectionIn(cfg).(config.AgentSection); ok && agent.Agent().Enabled && agent.Agent().Prewarm {
			out = append(out, d.ID)
		}
	}
	return out
}

func prewarmWants(cfg config.Config, id provider.ID) bool {
	return slices.Contains(prewarmPlan(cfg), id)
}

// newHTTPServer builds the daemon's HTTP server.
//
// WriteTimeout is intentionally unset (0): a global write deadline kills
// long-lived WebSocket connections. Per-frame deadlines live in
// ws.BroadcastEvent.
func newHTTPServer(addr string, handler http.Handler, log *slog.Logger) *http.Server {
	// HTTP/1.1 only, in every TLS mode: the daemon serves WebSocket upgrades
	// and small GETs, and a nil Protocols with a nil TLSNextProto made
	// ServeTLS offer h2 as well (MADR 0201 D8, 0073 F-T8).
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return &http.Server{
		Protocols:         protocols,
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// Route net/http's own error lines (TLS handshake failures, accept
		// errors) through slog so json-log deployments stay parseable.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}
