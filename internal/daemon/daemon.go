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
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
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
		gp := grok.NewWithLogger(grok.Config{
			Bin:           cfg.Providers.Grok.Bin,
			Args:          cfg.Providers.Grok.Args,
			AlwaysApprove: cfg.Providers.Grok.AlwaysApprove,
			DefaultCWD:    cfg.Providers.Grok.DefaultCWD,
			Model:         cfg.Providers.Grok.Model,
			PermissionTimeout: time.Duration(
				cfg.Providers.Grok.PermissionTimeoutSeconds) * time.Second,
			Prewarm: cfg.Providers.Grok.Prewarm,
			TurnStallNotice: time.Duration(
				cfg.Providers.Grok.TurnStallNoticeSeconds) * time.Second,
		}, log)
		reg.Register(gp)
		if !gp.Ready() {
			log.Warn("grok provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Grok.Bin),
			)
		}
		gp.EnsureWarm()
	}
	if cfg.Providers.Opencode.Enabled {
		occfg := opencode.Config{
			Bin:           cfg.Providers.Opencode.Bin,
			Args:          cfg.Providers.Opencode.Args,
			AlwaysApprove: cfg.Providers.Opencode.AlwaysApprove,
			DefaultCWD:    cfg.Providers.Opencode.DefaultCWD,
			Model:         cfg.Providers.Opencode.Model,
			PermissionTimeout: time.Duration(
				cfg.Providers.Opencode.PermissionTimeoutSeconds) * time.Second,
			Prewarm: cfg.Providers.Opencode.Prewarm,
			TurnStallNotice: time.Duration(
				cfg.Providers.Opencode.TurnStallNoticeSeconds) * time.Second,
		}
		if cfg.Providers.Opencode.Transport == "acp" {
			// Legacy transport: one full engine subprocess per session.
			op := opencode.NewWithLogger(occfg, log)
			reg.Register(op)
			if !op.Ready() {
				log.Warn("opencode provider enabled but binary not found in PATH",
					slog.String("bin", cfg.Providers.Opencode.Bin),
				)
			}
			// Arm the spare engine now so even the first create is warm.
			op.EnsureWarm()
		} else {
			// Default: one shared long-lived `opencode serve` engine
			// (HTTP + SSE); sessions are cheap server-side objects.
			op := opencode.NewHTTPWithLogger(occfg, log)
			reg.Register(op)
			if !op.Ready() {
				log.Warn("opencode provider enabled but binary not found in PATH",
					slog.String("bin", cfg.Providers.Opencode.Bin),
				)
			}
			// Boot the engine now so the first session create is instant.
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

	select {
	case <-ctx.Done():
		cause := context.Cause(ctx)
		if cause == nil {
			cause = ctx.Err()
		}
		log.Info("shutting down", slog.String("reason", cause.Error()))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		mgr.CloseAll(shutdownCtx)
		// Drain admin serve (ctx already cancelled closes the listener).
		select {
		case <-adminErrCh:
		case <-time.After(2 * time.Second):
		}
		return <-errCh
	case err := <-errCh:
		mgr.CloseAll(context.Background())
		return err
	case err := <-adminErrCh:
		if err != nil {
			log.Error("admin socket failed", slog.String("err", err.Error()))
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
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
