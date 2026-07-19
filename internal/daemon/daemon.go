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
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
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

	if cfg.Listen.Host == "0.0.0.0" && !cfg.Auth.RequireDeviceToken {
		log.Warn("listening on 0.0.0.0 without device tokens is unsafe")
	}
	if cfg.Providers.Fake.Enabled {
		log.Warn("fake provider is enabled (dev/smoke only)")
	}

	store, err := auth.OpenStore(filepath.Join(cfg.DataDir, "devices.json"))
	if err != nil {
		return err
	}
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
		}, log)
		reg.Register(gp)
		if !gp.Ready() {
			log.Warn("grok provider enabled but binary not found in PATH",
				slog.String("bin", cfg.Providers.Grok.Bin),
			)
		}
	}

	hub := &eventHub{}
	mgr := session.NewManager(reg, sessStore, log, hub.Broadcast)
	wsServer := ws.New(ws.Options{
		Store:              store,
		PairCodes:          pairCodes,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: cfg.Auth.RequireDeviceToken,
		Version:            opts.Version,
		ListenAddr:         cfg.Addr(),
		HeadscaleURL:       cfg.Headscale.ControlURL,
		Log:                log,
	})
	hub.server = wsServer

	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           wsServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr(), err)
	}
	log.Info("listening",
		slog.String("addr", ln.Addr().String()),
		slog.Bool("require_device_token", cfg.Auth.RequireDeviceToken),
	)

	errCh := make(chan error, 1)
	go func() {
		err := httpServer.Serve(ln)
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
		return <-errCh
	case err := <-errCh:
		mgr.CloseAll(context.Background())
		return err
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
