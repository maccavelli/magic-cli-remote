//go:build debugpprof

// Package debugserve exposes the Go pprof endpoints — including the Go 1.26
// goroutine-leak profile — on a loopback listener, in debug builds only
// (MADR 0068 P6). Release binaries compile the no-op twin: the profiling
// surface cannot be enabled in production by configuration, only by
// building with `make debug` (tag debugpprof +
// GOEXPERIMENT=goroutineleakprofile) and setting MC_DEBUG_ADDR.
package debugserve

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

// Start serves /debug/pprof/* on MC_DEBUG_ADDR (e.g. "127.0.0.1:6060").
// Unset means off, even in a debug build. Non-loopback hosts are refused —
// pprof output includes memory contents and must never face a network edge.
// The listener dies with ctx.
func Start(ctx context.Context, log *slog.Logger) {
	addr := os.Getenv("MC_DEBUG_ADDR")
	if addr == "" {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Error("MC_DEBUG_ADDR invalid", slog.String("addr", addr), slog.String("err", err.Error()))
		return
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Error("MC_DEBUG_ADDR must be loopback", slog.String("addr", addr))
		return
	}
	mux := http.NewServeMux()
	// Index also serves every named runtime profile at /debug/pprof/<name>,
	// which is where `goroutineleak` appears when the binary was built with
	// GOEXPERIMENT=goroutineleakprofile.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	go func() {
		log.Warn("debug pprof listener up (debug build)", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("debug pprof listener failed", slog.String("err", err.Error()))
		}
	}()
}
