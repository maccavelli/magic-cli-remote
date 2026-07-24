// Package daemon wires configuration into a running mcremote process.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
	"github.com/maccavelli/magic-cli-remote/internal/relayhost"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
	"github.com/maccavelli/magic-cli-remote/internal/xdg"
)

// Options for Run.
type Options struct {
	Config  config.Config
	Version string
	Log     *slog.Logger
}

// Run starts the daemon and blocks until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "daemon"))

	cfg := opts.Config
	if err := xdg.EnsureDir(cfg.DataDir); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}

	// Resolve the "tailscale" sentinel before anything derives from the bind
	// address (certificate SANs, the advertised listen addr, the listener).
	if err := cfg.ResolveListenHost(); err != nil {
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

	reg := provider.NewRegistry()
	if cfg.Providers.Fake.Enabled {
		reg.Register(fake.New())
	}
	if cfg.Providers.Grok.Enabled {
		gp := grok.NewWithLogger(acpAgentConfig(cfg.Providers.Grok.ACPProviderConfig), log)
		reg.Register(gp)
		if !gp.Ready() {
			log.Warn("grok provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Grok.Bin),
			)
		}
		gp.EnsureWarm()
	}
	if cfg.Providers.Opencode.Enabled {
		// Before starting an engine of our own, clear out any left behind by a
		// previous daemon that died without running its shutdown path. Only
		// processes carrying our ownership marker whose owner is gone are
		// touched — an engine belonging to a concurrently running mcremote is
		// left alone (MADR 0019 §5.4).
		httpagent.ReapOrphanEngines(log)

		// One shared long-lived `opencode serve` engine (HTTP + SSE) drives
		// every OpenCode session; they are cheap server-side objects, so there
		// is no per-session process (MADR 0019).
		sessionTree := cfg.Providers.Opencode.SessionTree
		op := opencode.NewHTTPWithLogger(opencode.Config{
			Bin:           cfg.Providers.Opencode.Bin,
			AlwaysApprove: cfg.Providers.Opencode.AlwaysApprove,
			DefaultCWD:    cfg.Providers.Opencode.DefaultCWD,
			Model:         cfg.Providers.Opencode.Model,
			PermissionTimeout: time.Duration(
				cfg.Providers.Opencode.PermissionTimeoutSeconds) * time.Second,
			TurnStallNotice: time.Duration(
				cfg.Providers.Opencode.TurnStallNoticeSeconds) * time.Second,
			// Explicit pointer so false kill-switch is distinct from zero Config.
			SessionTree: &sessionTree,
		}, log)
		reg.Register(op)
		if !op.Ready() {
			log.Warn("opencode provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Opencode.Bin),
			)
		}
		if cfg.Providers.Opencode.Prewarm {
			// Boot the engine now so the first session create is instant.
			// Disabled, the first create pays the ~3-5s Bun cold start and the
			// host holds no idle engine.
			op.EnsureServer()
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
	if anyEnabled := cfg.Providers.Fake.Enabled || cfg.Providers.Grok.Enabled || cfg.Providers.Opencode.Enabled; anyEnabled {
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
	wsServer := ws.New(ws.Options{
		Store:              store,
		PairCodes:          pairCodes,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: cfg.Auth.RequireDeviceToken,
		RequireClientKey:   cfg.Auth.RequireClientKey,
		AllowedOrigins:     cfg.Auth.AllowedOrigins,
		Version:            opts.Version,
		ListenAddr:         cfg.Addr(),
		HeadscaleURL:       cfg.Headscale.ControlURL,
		Log:                log,
		MaxClients:         limits.MaxWSClients,
	})
	hub.server = wsServer

	// Local admin socket so `mcremote pair revoke` can kick live WS clients.
	adminErrCh := make(chan error, 1)
	go func() {
		if err := admin.Serve(ctx, cfg.DataDir, wsServer, log); err != nil {
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

	ln, err := net.Listen("tcp", cfg.Addr())
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
		rc := relayhost.New(relayhost.Config{
			URL:                cfg.Relay.URL,
			HostID:             cfg.Relay.HostID,
			Secret:             cfg.Relay.Secret,
			LocalAddr:          ln.Addr().String(),
			InsecureSkipVerify: cfg.Relay.InsecureSkipVerify,
		}, log)
		rc.SetLocalAddr(ln.Addr().String())
		go func() {
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
		var err error
		if serveTLS {
			// Cert/key come from httpServer.TLSConfig.
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
		mgr.CloseAll(context.Background())
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

// acpAgentConfig builds an acpagent.Config from the shared ACP provider config.
// Every ACP CLI agent (grok today; goose and codex next) is constructed through
// this one converter so they stay identical in how config maps to the adapter.
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
