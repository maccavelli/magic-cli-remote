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
	"runtime/debug"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/debugserve"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
	"github.com/maccavelli/magic-cli-remote/internal/relayhost"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/tcc"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// Options for Run.
type Options struct {
	Config  config.Config
	Version string
	Log     *slog.Logger
}

// opencodeConfigFrom maps daemon configuration onto the OpenCode transport
// config.
//
// Extracted so the mapping itself is testable: a policy flag that is settable,
// documented and tested at the config layer but never copied here would be a
// default in name only (MADR 0112 A8/A9).
func opencodeConfigFrom(cfg config.Config, sessionTree *bool, streamCoalesce *time.Duration) opencode.Config {
	return opencode.Config{
		Bin:           cfg.Providers.Opencode.Bin,
		AlwaysApprove: cfg.Providers.Opencode.AlwaysApprove,
		DefaultCWD:    cfg.Providers.Opencode.DefaultCWD,
		Model:         cfg.Providers.Opencode.Model,
		PermissionTimeout: time.Duration(
			cfg.Providers.Opencode.PermissionTimeoutSeconds) * time.Second,
		TurnStallNotice: time.Duration(
			cfg.Providers.Opencode.TurnStallNoticeSeconds) * time.Second,
		// Explicit pointer so false kill-switch is distinct from zero Config.
		SessionTree: sessionTree,
		// Likewise explicit: 0 means "stream one event per token" (the
		// pre-MADR-0024 path), not "use the transport default".
		StreamCoalesce: streamCoalesce,
		Pure:           cfg.Providers.Opencode.Pure,
		// Remote-mutation policy, both false unless an operator turned them on
		// for this host (MADR 0112 A8/A9).
		AllowRemoteShare: cfg.Providers.Opencode.AllowRemoteShare,
		AllowRemoteShell: cfg.Providers.Opencode.AllowRemoteShell,
	}
}

// Run starts the daemon and blocks until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "daemon"))

	cfg := opts.Config
	if err := appdirs.EnsurePrivateDir(cfg.DataDir); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}

	// macOS privacy probe (MADR 0069 D5): a headless daemon cannot be
	// prompted and a denial otherwise first surfaces as a confusing agent
	// failure. One stat, one warn, never fatal — sessions outside protected
	// folders are unaffected. No-op off darwin.
	if tcc.Probe() == tcc.Denied {
		log.Warn("macOS Full Disk Access not granted for this binary; " +
			"agent sessions under Documents/Desktop/Downloads will fail " +
			"with 'operation not permitted' — run `mcremote doctor` or see " +
			"docs/ops-macos-tcc.md")
	}

	// No-op in release builds; `make debug` + MC_DEBUG_ADDR only (0068 P6,
	// goroutine-leak triage — docs/ops-mcrelay.md).
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
	if cfg.Providers.Fake.Enabled {
		log.Warn("fake provider is enabled (dev/smoke only)")
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
	// previous daemon that died without running its shutdown path — goose,
	// opencode, and codex all spawn theirs the same marked way. Only
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
	if cfg.Providers.Fake.Enabled {
		reg.Register(fake.New())
	}
	// Credential transactions (MADR 0074 D21). Built before any provider so a
	// coordinated provider can be constructed with its coordinator, and before
	// any mutation so startup recovery runs first.
	guard, guardErr := newCredentialGuard(cfg.DataDir, cfg.Providers.Codex.Enabled,
		cfg.Providers.Grok.Enabled, cfg.Providers.Codex.Bin, log)
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

	if cfg.Providers.Grok.Enabled {
		acpCfg := acpAgentConfig(cfg.Providers.Grok.ACPProviderConfig)
		acpCfg.ReasoningEffort = cfg.Providers.Grok.ReasoningEffort
		acpCfg.PermissionMode = cfg.Providers.Grok.PermissionMode
		acpCfg.Sandbox = cfg.Providers.Grok.Sandbox
		acpCfg.AllowedTools = cfg.Providers.Grok.AllowedTools
		acpCfg.DisallowedTools = cfg.Providers.Grok.DisallowedTools
		acpCfg.AllowRules = cfg.Providers.Grok.AllowRules
		acpCfg.DenyRules = cfg.Providers.Grok.DenyRules
		acpCfg.NoSubagents = cfg.Providers.Grok.NoSubagents
		acpCfg.DisableWebSearch = cfg.Providers.Grok.DisableWebSearch
		// Explicit pointer so 0 means "one event per token" (pre-0057), not
		// "use the transport default".
		streamCoalesce := time.Duration(cfg.Providers.Grok.StreamCoalesceMs) * time.Millisecond
		acpCfg.StreamCoalesce = &streamCoalesce
		gp := grok.NewWithLogger(acpCfg, log)
		if gc := guard.coordinator("grok"); gc != nil {
			// The coordinated wrapper owns the transactional device-auth and
			// method-clearing contracts; the underlying provider is unchanged.
			reg.Register(grok.NewCoordinated(gp, cfg.Providers.Grok.Bin, log, gc, busyFor(provider.IDGrok)))
		} else {
			reg.Register(gp)
		}
		if !gp.Ready() {
			log.Warn("grok provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Grok.Bin),
			)
		}
		if prewarmWants(cfg, provider.IDGrok) {
			gp.EnsureWarm()
		}
	}
	if cfg.Providers.Goose.Enabled {
		// Before the provider exists, so the first engine — including a
		// prewarmed one — already sees the intended secret backend
		// (MADR 0110 D1/D9, plan P5).
		reconcileGooseKeyring(cfg.Providers.Goose.KeyringDisabled, log)
		gp := goose.NewWithLogger(acpHTTPConfig(cfg.Providers.Goose), log)
		reg.Register(gp)
		if !gp.Ready() {
			log.Warn("goose provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Goose.Bin),
			)
		}
		if prewarmWants(cfg, provider.IDGoose) {
			gp.EnsureServer()
		}
	}
	if cfg.Providers.Opencode.Enabled {
		// One shared long-lived `opencode serve` engine (HTTP + SSE) drives
		// every OpenCode session; they are cheap server-side objects, so there
		// is no per-session process (MADR 0019).
		sessionTree := cfg.Providers.Opencode.SessionTree
		streamCoalesce := time.Duration(
			cfg.Providers.Opencode.StreamCoalesceMs) * time.Millisecond
		op := opencode.NewHTTPWithLogger(
			opencodeConfigFrom(cfg, &sessionTree, &streamCoalesce), log)
		reg.Register(op)
		if !op.Ready() {
			log.Warn("opencode provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Opencode.Bin),
			)
		}
		if prewarmWants(cfg, provider.IDOpencode) {
			// Boot the engine now so the first session create is instant.
			// Disabled, the first create pays the ~3-5s Bun cold start and the
			// host holds no idle engine.
			op.EnsureServer()
		}
	}
	if cfg.Providers.Codex.Enabled {
		streamCoalesce := time.Duration(cfg.Providers.Codex.StreamCoalesceMs) * time.Millisecond
		executionEnvironments := make([]provider.ExecutionEnvironment, 0, len(cfg.Providers.Codex.Environments))
		for _, environment := range cfg.Providers.Codex.Environments {
			executionEnvironments = append(executionEnvironments, provider.ExecutionEnvironment{
				ID: environment.ID, ExecServerURL: environment.ExecServerURL,
				ConnectTimeout:        time.Duration(environment.ConnectTimeoutMS) * time.Millisecond,
				RuntimeWorkspaceRoots: append([]string(nil), environment.RuntimeWorkspaceRoots...),
			})
		}
		codexConf := codex.Config{
			Bin:                           cfg.Providers.Codex.Bin,
			AlwaysApprove:                 cfg.Providers.Codex.AlwaysApprove,
			DefaultCWD:                    cfg.Providers.Codex.DefaultCWD,
			Model:                         cfg.Providers.Codex.Model,
			PermissionTimeout:             time.Duration(cfg.Providers.Codex.PermissionTimeoutSeconds) * time.Second,
			Prewarm:                       cfg.Providers.Codex.Prewarm,
			TurnStallNotice:               time.Duration(cfg.Providers.Codex.TurnStallNoticeSeconds) * time.Second,
			StreamCoalesce:                &streamCoalesce,
			ApprovalPolicy:                cfg.Providers.Codex.ApprovalPolicy,
			SandboxMode:                   cfg.Providers.Codex.SandboxMode,
			AllowFullAccess:               cfg.Providers.Codex.AllowFullAccess,
			SandboxBrokenPolicy:           cfg.Providers.Codex.SandboxBrokenPolicy,
			Transport:                     codex.TransportMode(cfg.Providers.Codex.Transport),
			ListenAddress:                 cfg.Providers.Codex.ListenAddress,
			WSAuthMode:                    codex.WSAuthMode(cfg.Providers.Codex.WSAuthMode),
			ReconnectAttempts:             cfg.Providers.Codex.ReconnectAttempts,
			ReconnectAttemptsConfigured:   true,
			RuntimeDir:                    cfg.Paths.RuntimeDir,
			Environments:                  executionEnvironments,
			StandaloneProcessesEnabled:    cfg.Providers.Codex.StandaloneProcessesEnabled,
			StandaloneProcessEnvAllowlist: append([]string(nil), cfg.Providers.Codex.StandaloneProcessEnvAllowlist...),
		}
		// The coordinated constructor differs only by carrying a credential
		// coordinator; every other behaviour is identical, which keeps the
		// activation a wiring change rather than a second code path.
		cp := codex.NewWithLogger(codexConf, log)
		if cc := guard.coordinator("codex"); cc != nil {
			cp = codex.NewCoordinated(codexConf, log, cc, busyFor(provider.IDCodex))
		}
		reg.Register(cp)
		if !cp.Ready() {
			log.Warn("codex provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Codex.Bin),
			)
		}
		if prewarmWants(cfg, provider.IDCodex) {
			cp.EnsureServer()
		}
	}
	if cfg.Providers.Kilo.Enabled {
		// One shared long-lived `kilo serve` engine (HTTP + SSE) drives every
		// Kilo session, same architecture as OpenCode — kilo is an OpenCode
		// fork with a distinct dialect (MADR 0075 D2/D4). Spawned un-gated on
		// loopback like the opencode engine (D5 as amended, plan PD1).
		sessionTree := cfg.Providers.Kilo.SessionTree
		streamCoalesce := time.Duration(
			cfg.Providers.Kilo.StreamCoalesceMs) * time.Millisecond
		kp := kilo.NewHTTPWithLogger(kilo.Config{
			Bin:           cfg.Providers.Kilo.Bin,
			AlwaysApprove: cfg.Providers.Kilo.AlwaysApprove,
			DefaultCWD:    cfg.Providers.Kilo.DefaultCWD,
			Model:         cfg.Providers.Kilo.Model,
			PermissionTimeout: time.Duration(
				cfg.Providers.Kilo.PermissionTimeoutSeconds) * time.Second,
			TurnStallNotice: time.Duration(
				cfg.Providers.Kilo.TurnStallNoticeSeconds) * time.Second,
			// Explicit pointer so the false default (plan PD2) is distinct
			// from zero Config; kilo flips this only after MADR 0075 Q7.
			SessionTree: &sessionTree,
			// Likewise explicit: 0 means "stream one event per token".
			StreamCoalesce: &streamCoalesce,
			Pure:           cfg.Providers.Kilo.Pure,
		}, log)
		reg.Register(kp)
		if !kp.Ready() {
			log.Warn("kilo provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Kilo.Bin),
			)
		}
		if prewarmWants(cfg, provider.IDKilo) {
			// Boot the engine now so the first session create is instant
			// (Bun-class cold start, same rationale as opencode).
			kp.EnsureServer()
		}
	}
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
	// Keep this OR-chain in sync with ProvidersConfig — a provider missing here
	// silently skips the warning instead of just being ready=false (MADR 0076 M3).
	if anyEnabled := cfg.Providers.Fake.Enabled || cfg.Providers.Grok.Enabled || cfg.Providers.Goose.Enabled || cfg.Providers.Opencode.Enabled || cfg.Providers.Codex.Enabled || cfg.Providers.Kilo.Enabled; anyEnabled {
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
		if err := admin.Serve(ctx, sock, wsServer, log); err != nil {
			adminErrCh <- err
			return
		}
		adminErrCh <- nil
	}()

	// WriteTimeout is intentionally unset (0): a global write deadline kills
	// long-lived WebSocket connections. Per-frame deadlines live in ws.BroadcastEvent.
	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           wsServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// Route net/http's own error lines (TLS handshake failures, accept
		// errors) through slog so json-log deployments stay parseable.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

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

	// gracefulDrain performs the ordered shutdown: stop accepting, close the
	// hijacked WebSocket conns (http.Server.Shutdown never touches them), close
	// live sessions, then drain the serve goroutines.
	gracefulDrain := func(reason string) error {
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

// acpAgentConfig builds an acpagent.Config from the shared ACP provider config.
// Every ACP CLI agent (grok today; goose and codex next) is constructed through
// this one converter so they stay identical in how config maps to the adapter.
// acpHTTPConfig builds a goose.Config (acphttp.Config) from the goose provider
// config. Unlike acpAgentConfig it drops Args and FSRoots because the HTTP
// transport does not start a per-session process.
func acpHTTPConfig(c config.GooseProviderConfig) goose.Config {
	mcp := make([]goose.McpServer, 0, len(c.MCPServers))
	for _, m := range c.MCPServers {
		mcp = append(mcp, goose.McpServer{
			Name:      m.Name,
			Transport: m.Transport,
			URL:       m.URL,
			Headers:   m.Headers,
		})
	}
	// Explicit pointer so 0 means "stream one event per token" (the
	// pre-MADR-0024 path), not "use the transport default".
	streamCoalesce := time.Duration(c.StreamCoalesceMs) * time.Millisecond
	return goose.Config{
		Config: acphttp.Config{
			Bin:               c.Bin,
			AlwaysApprove:     c.AlwaysApprove,
			DefaultCWD:        c.DefaultCWD,
			Model:             c.Model,
			PermissionTimeout: time.Duration(c.PermissionTimeoutSeconds) * time.Second,
			Prewarm:           c.Prewarm,
			TurnStallNotice:   time.Duration(c.TurnStallNoticeSeconds) * time.Second,
			AuthMethodID:      c.AuthMethodID,
			McpServers:        mcp,
			StreamCoalesce:    &streamCoalesce,
		},
		WithBuiltins: append([]string(nil), c.WithBuiltins...),
	}
}

// prewarmPlan is the set of enabled providers whose engine should start at
// serve boot (MADR 0089 D5). Empty when every default is off.
func prewarmPlan(cfg config.Config) []provider.ID {
	var out []provider.ID
	if cfg.Providers.Grok.Enabled && cfg.Providers.Grok.Prewarm {
		out = append(out, provider.IDGrok)
	}
	if cfg.Providers.Goose.Enabled && cfg.Providers.Goose.Prewarm {
		out = append(out, provider.IDGoose)
	}
	if cfg.Providers.Opencode.Enabled && cfg.Providers.Opencode.Prewarm {
		out = append(out, provider.IDOpencode)
	}
	if cfg.Providers.Codex.Enabled && cfg.Providers.Codex.Prewarm {
		out = append(out, provider.IDCodex)
	}
	if cfg.Providers.Kilo.Enabled && cfg.Providers.Kilo.Prewarm {
		out = append(out, provider.IDKilo)
	}
	return out
}

func prewarmWants(cfg config.Config, id provider.ID) bool {
	for _, got := range prewarmPlan(cfg) {
		if got == id {
			return true
		}
	}
	return false
}

func acpAgentConfig(c config.ACPProviderConfig) acpagent.Config {
	mcp := make([]acpagent.McpServer, 0, len(c.MCPServers))
	for _, m := range c.MCPServers {
		mcp = append(mcp, acpagent.McpServer{
			Name:      m.Name,
			Transport: acpagent.McpTransport(m.Transport),
			URL:       m.URL,
			Headers:   m.Headers,
		})
	}
	return acpagent.Config{
		Bin:               c.Bin,
		Args:              c.Args,
		AlwaysApprove:     c.AlwaysApprove,
		DefaultCWD:        c.DefaultCWD,
		Model:             c.Model,
		PermissionTimeout: time.Duration(c.PermissionTimeoutSeconds) * time.Second,
		Prewarm:           c.Prewarm,
		TurnStallNotice:   time.Duration(c.TurnStallNoticeSeconds) * time.Second,
		FSRoots:           c.FSRoots,
		McpServers:        mcp,
		AuthMethodID:      c.AuthMethodID,
	}
}
