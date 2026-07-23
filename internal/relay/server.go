package relay

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Server is the public mcrelay edge.
type Server struct {
	cfg  Config
	hub  *hub
	log  *slog.Logger
	http *http.Server

	rateMu sync.Mutex
	// rate keys are "bucket\x00id" (R16 multi-bucket).
	rate map[string]*rateWindow

	// activeSplices tracks live opaque tunnels so Shutdown can drain them
	// (MADR 0016 R17).
	activeMu      sync.Mutex
	activeSplices map[*activeSplice]struct{}

	// sweeperCancel stops the R18 orphan-pending GC.
	sweeperCancel context.CancelFunc
}

type rateWindow struct {
	start time.Time
	count int
}

// Rate bucket names (MADR 0016 R16).
const (
	rateBucketAccept   = "accept"
	rateBucketJoin     = "join"
	rateBucketRegister = "register"
	rateBucketJoinHost = "joinhost"
)

// activeSplice is one phone↔tunnel byte pipe.
type activeSplice struct {
	cancel context.CancelFunc
	a, b   *websocket.Conn
}

// New creates a relay server. Call [Server.ListenAndServe] or [Server.Serve].
func New(cfg Config, log *slog.Logger) *Server {
	// Field-wise defaults only (MADR 0016 R4): never replace a partially set Limits.
	cfg.Limits = ResolvedLimits(cfg.Limits)
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:           cfg,
		hub:           newHub(cfg.Allow, cfg.Limits, log),
		log:           log.With(slog.String("component", "mcrelay")),
		rate:          make(map[string]*rateWindow),
		activeSplices: make(map[*activeSplice]struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/host", s.handleHost)
	mux.HandleFunc("GET /v1/phone", s.handlePhone)
	mux.HandleFunc("GET /v1/tunnel", s.handleTunnel)
	s.http = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Handler returns the HTTP handler (tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }

// ListenAndServe binds and serves until ctx cancel or error.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	switch {
	case s.cfg.TLSConfig != nil:
		cfg := s.cfg.TLSConfig.Clone()
		if cfg.MinVersion == 0 {
			cfg.MinVersion = tls.VersionTLS12
		}
		ln = tls.NewListener(ln, cfg)
		s.log.Info("listening", slog.String("addr", ln.Addr().String()), slog.String("tls", "managed"))
	case s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "":
		cert, err := tls.LoadX509KeyPair(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		if err != nil {
			_ = ln.Close()
			return err
		}
		ln = tls.NewListener(ln, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		})
		s.log.Info("listening", slog.String("addr", ln.Addr().String()), slog.String("tls", "files"))
	default:
		s.log.Warn("listening without TLS — join plane is cleartext; use only on loopback or tests",
			slog.String("addr", ln.Addr().String()))
	}
	return s.Serve(ctx, ln)
}

// Serve serves on an existing listener (tests may pass plain TCP).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.startPendingSweeper()
	defer s.stopPendingSweeper()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.http.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		// R17: tear down hijacked WebSocket splices so Shutdown can finish.
		s.closeAllSplices("shutdown")
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shCtx)
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// startPendingSweeper runs R18 orphan GC until stopPendingSweeper.
func (s *Server) startPendingSweeper() {
	s.stopPendingSweeper()
	ctx, cancel := context.WithCancel(context.Background())
	s.sweeperCancel = cancel
	// Interval: half of TunnelWait so orphans clear soon after phone-side timeout.
	every := s.cfg.Limits.TunnelWait / 2
	if every < time.Second {
		every = time.Second
	}
	maxAge := s.cfg.Limits.TunnelWait * 2
	if maxAge < 30*time.Second {
		maxAge = 30 * time.Second
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n := s.hub.expireStalePending(maxAge); n > 0 {
					s.log.Info("expired orphan pending joins", slog.Int("count", n))
				}
			}
		}
	}()
}

func (s *Server) stopPendingSweeper() {
	if s.sweeperCancel != nil {
		s.sweeperCancel()
		s.sweeperCancel = nil
	}
}

// closeAllSplices cancels and closes every tracked opaque splice (R17).
func (s *Server) closeAllSplices(reason string) {
	s.activeMu.Lock()
	list := make([]*activeSplice, 0, len(s.activeSplices))
	for tr := range s.activeSplices {
		list = append(list, tr)
	}
	s.activeMu.Unlock()
	for _, tr := range list {
		if tr.cancel != nil {
			tr.cancel()
		}
		if tr.a != nil {
			_ = tr.a.Close(websocket.StatusGoingAway, reason)
		}
		if tr.b != nil {
			_ = tr.b.Close(websocket.StatusGoingAway, reason)
		}
	}
}

// trackSplice registers a live splice; the returned function unregisters it.
func (s *Server) trackSplice(cancel context.CancelFunc, a, b *websocket.Conn) (untrack func()) {
	tr := &activeSplice{cancel: cancel, a: a, b: b}
	s.activeMu.Lock()
	s.activeSplices[tr] = struct{}{}
	s.activeMu.Unlock()
	return func() {
		s.activeMu.Lock()
		delete(s.activeSplices, tr)
		s.activeMu.Unlock()
	}
}

// ActiveSplices returns the number of live opaque splices (tests / diagnostics).
func (s *Server) ActiveSplices() int {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return len(s.activeSplices)
}

// Addr is the configured listen address (may be ":0" before serve).
func (s *Server) Addr() string { return s.cfg.ListenAddr }

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	// R11: minimal liveness only — no host counts, versions, or allowlist size.
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// rateWindowTTL drops stale per-IP windows so the map cannot grow without bound
// under internet scanning (MADR 0016 R9 / D6).
const rateWindowTTL = 2 * time.Minute

// rateMapMax caps distinct rate keys tracked; oldest windows are pruned first
// when over capacity.
const rateMapMax = 4096

func (s *Server) allowAccept(r *http.Request) bool {
	return s.allowRate(clientIP(r), rateBucketAccept, s.cfg.Limits.AcceptPerMinute)
}

// allowRate enforces a per-key sliding one-minute window (R16 multi-bucket).
func (s *Server) allowRate(id, bucket string, max int) bool {
	if max <= 0 {
		return true
	}
	key := bucket + "\x00" + id
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	s.pruneRateLocked(now)
	w := s.rate[key]
	if w == nil || now.Sub(w.start) >= time.Minute {
		if w == nil {
			for len(s.rate) >= rateMapMax {
				for other := range s.rate {
					delete(s.rate, other)
					break
				}
			}
		}
		s.rate[key] = &rateWindow{start: now, count: 1}
		return true
	}
	if w.count >= max {
		return false
	}
	w.count++
	return true
}

func (s *Server) pruneRateLocked(now time.Time) {
	for key, w := range s.rate {
		if now.Sub(w.start) >= rateWindowTTL {
			delete(s.rate, key)
		}
	}
	// Hard cap safety net (map iteration order).
	for len(s.rate) > rateMapMax {
		for key := range s.rate {
			delete(s.rate, key)
			break
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	if !s.allowAccept(r) {
		http.Error(w, "rate_limited", http.StatusTooManyRequests)
		return nil, fmt.Errorf("rate_limited")
	}
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		// MADR 0016 R8 / D5: do not use OriginPatterns: ["*"] on a public edge.
		// Empty patterns: coder/websocket accepts requests with no Origin
		// (native Flutter / Go clients) and same-origin only — browsers on
		// arbitrary sites cannot open join-plane sockets for free.
		// Opt-in browser allowlist can be added later via config.
	})
}

func (s *Server) rateLimitedWS(ctx context.Context, conn *websocket.Conn, reqID string) {
	_ = writeErr(ctx, conn, reqID, "rate_limited", "rate_limited")
	_ = conn.Close(websocket.StatusTryAgainLater, "rate_limited")
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	conn.SetReadLimit(int64(s.cfg.Limits.MaxMessageBytes))

	env, err := readEnv(ctx, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_frame")
		return
	}
	if env.Type != TypeRegister {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "first message must be register")
		_ = conn.Close(websocket.StatusPolicyViolation, "expected register")
		return
	}
	var reg RegisterPayload
	if err := DecodePayload(env, &reg); err != nil || strings.TrimSpace(reg.HostID) == "" {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid register payload")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	// R16: separate register bucket (do not share only with accept/join).
	if !s.allowRate(clientIP(r), rateBucketRegister, s.cfg.Limits.RegisterPerMinute) {
		s.rateLimitedWS(ctx, conn, env.ID)
		return
	}
	if !s.hub.checkSecret(reg.HostID, reg.Secret) {
		// Same error for unknown host_id and wrong secret (no enumeration).
		_ = writeErr(ctx, conn, env.ID, "unauthorized", "invalid host credentials")
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		s.log.Info("register denied", slog.String("host_id", reg.HostID), slog.String("reason", "unauthorized"))
		return
	}
	hostCtx, cancel := context.WithCancel(context.Background())
	if err := s.hub.register(reg.HostID, conn, cancel); err != nil {
		_ = writeErr(ctx, conn, env.ID, err.Error(), err.Error())
		_ = conn.Close(websocket.StatusTryAgainLater, err.Error())
		cancel()
		return
	}
	ok, _ := NewEnvelope(TypeRegisterOK, env.ID, SessionPayload{HostID: reg.HostID})
	if err := writeEnv(ctx, conn, ok); err != nil {
		s.hub.unregister(reg.HostID, conn)
		cancel()
		return
	}

	// Keep control connection alive; dial notifications are written here.
	// Read loop: any client close ends registration.
	// R5: concurrent app-level Ping on RegisterIdle so idle middleboxes
	// do not silently drop the host registration.
	go func() {
		defer cancel()
		defer s.hub.unregister(reg.HostID, conn)
		defer conn.Close(websocket.StatusNormalClosure, "")
		if idle := s.cfg.Limits.RegisterIdle; idle > 0 {
			go s.pingHostControl(hostCtx, conn, cancel, idle)
		}
		for {
			_, _, err := conn.Read(hostCtx)
			if err != nil {
				return
			}
			// Host should not send data frames on control after register;
			// ignore — Ping/Pong is handled by the websocket library.
		}
	}()
	<-hostCtx.Done()
}

// pingHostControl sends periodic WebSocket pings until ctx is done (R5).
// Ping must run concurrently with Read (coder/websocket contract).
func (s *Server) pingHostControl(ctx context.Context, conn *websocket.Conn, fail context.CancelFunc, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				s.log.Info("host control ping failed", slog.String("err", err.Error()))
				fail()
				return
			}
		}
	}
}

func (s *Server) handlePhone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	conn.SetReadLimit(int64(s.cfg.Limits.MaxMessageBytes))

	env, err := readEnv(ctx, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_frame")
		return
	}
	if env.Type != TypeJoin {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "first message must be join")
		_ = conn.Close(websocket.StatusPolicyViolation, "expected join")
		return
	}
	var join JoinPayload
	if err := DecodePayload(env, &join); err != nil || strings.TrimSpace(join.HostID) == "" {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid join payload")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	// R16 / H3: per-IP and per-host_id join caps (enumeration / join spam).
	ip := clientIP(r)
	if !s.allowRate(ip, rateBucketJoin, s.cfg.Limits.JoinPerMinute) ||
		!s.allowRate(join.HostID, rateBucketJoinHost, s.cfg.Limits.JoinPerHostPerMinute) {
		s.rateLimitedWS(ctx, conn, env.ID)
		s.log.Info("join denied", slog.String("host_id", join.HostID), slog.String("reason", "rate_limited"))
		return
	}
	pending, err := s.hub.beginJoin(join.HostID, conn)
	if err != nil {
		code := err.Error()
		_ = writeErr(ctx, conn, env.ID, code, code)
		_ = conn.Close(websocket.StatusTryAgainLater, code)
		s.log.Info("join denied", slog.String("host_id", join.HostID), slog.String("reason", code))
		return
	}

	// Ask host to open a tunnel; include single-use token (R12).
	dial, _ := NewEnvelope(TypeDial, "", SessionPayload{
		SessionID:   pending.sessionID,
		HostID:      join.HostID,
		TunnelToken: pending.tunnelToken,
	})
	if err := s.hub.writeControl(ctx, join.HostID, dial); err != nil {
		s.hub.cancelJoin(pending.sessionID)
		_ = writeErr(ctx, conn, env.ID, "host_offline", "host unreachable")
		_ = conn.Close(websocket.StatusTryAgainLater, "host_offline")
		return
	}

	// Wait for host tunnel.
	timer := time.NewTimer(s.cfg.Limits.TunnelWait)
	defer timer.Stop()
	var tunnel *websocket.Conn
	select {
	case tunnel = <-pending.ready:
		if tunnel == nil {
			// cancelJoin already released phone capacity.
			_ = writeErr(ctx, conn, env.ID, "host_offline", "join cancelled")
			_ = conn.Close(websocket.StatusTryAgainLater, "host_offline")
			return
		}
	case <-timer.C:
		s.hub.cancelJoin(pending.sessionID)
		_ = writeErr(ctx, conn, env.ID, "timeout", "host did not open tunnel")
		_ = conn.Close(websocket.StatusTryAgainLater, "timeout")
		s.log.Info("join timeout", slog.String("host_id", join.HostID), slog.String("session_id", pending.sessionID))
		return
	case <-ctx.Done():
		s.hub.cancelJoin(pending.sessionID)
		return
	}

	ok, _ := NewEnvelope(TypeJoinOK, env.ID, SessionPayload{
		SessionID: pending.sessionID,
		HostID:    join.HostID,
	})
	if err := writeEnv(ctx, conn, ok); err != nil {
		_ = tunnel.Close(websocket.StatusGoingAway, "phone gone")
		close(pending.done)
		s.hub.endPhone(join.HostID)
		return
	}

	s.log.Info("join ok",
		slog.String("host_id", join.HostID),
		slog.String("session_id", pending.sessionID),
		slog.String("reason", "splice"))
	// Opaque splice: no protocol-v1 parsing (MADR 0015 D2).
	// R15: idle + max lifetime; R17: tracked for shutdown drain.
	spliceOpts := spliceOptions{
		maxBytes: s.cfg.Limits.MaxMessageBytes,
		idle:     s.cfg.Limits.SpliceIdle,
		maxLife:  s.cfg.Limits.SpliceMax,
	}
	spliceCtx, spliceCancel := context.WithCancel(ctx)
	untrack := s.trackSplice(spliceCancel, conn, tunnel)
	reason := splice(spliceCtx, conn, tunnel, spliceOpts)
	untrack()
	spliceCancel()
	close(pending.done)
	s.hub.endPhone(join.HostID)
	s.log.Info("splice ended",
		slog.String("host_id", join.HostID),
		slog.String("session_id", pending.sessionID),
		slog.String("reason", reason))
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	conn.SetReadLimit(int64(s.cfg.Limits.MaxMessageBytes))

	env, err := readEnv(ctx, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_frame")
		return
	}
	if env.Type != TypeTunnel {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "first message must be tunnel")
		_ = conn.Close(websocket.StatusPolicyViolation, "expected tunnel")
		return
	}
	var tun TunnelPayload
	if err := DecodePayload(env, &tun); err != nil || tun.SessionID == "" || tun.HostID == "" {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid tunnel payload")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	pending, err := s.hub.completeTunnel(tun.SessionID, tun.HostID, tun.Token, tun.Secret, conn)
	if err != nil {
		_ = writeErr(ctx, conn, env.ID, err.Error(), err.Error())
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	ok, _ := NewEnvelope(TypeTunnelOK, env.ID, SessionPayload{
		SessionID: pending.sessionID,
		HostID:    pending.hostID,
	})
	if err := writeEnv(ctx, conn, ok); err != nil {
		close(pending.done)
		return
	}
	// Splice is driven by handlePhone; block until it finishes so the
	// upgraded connection stays owned by this handler.
	select {
	case <-pending.done:
	case <-ctx.Done():
	}
}

type spliceOptions struct {
	maxBytes int
	idle     time.Duration // negative = disabled; zero uses caller defaults
	maxLife  time.Duration // negative = disabled
}

// splice copies frames both ways until either side fails, idle expires, or
// maxLife is reached. Returns a short reason for logging (R15).
func splice(ctx context.Context, a, b *websocket.Conn, opts spliceOptions) string {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reason := "client_gone"
	var reasonMu sync.Mutex
	setReason := func(r string) {
		reasonMu.Lock()
		if reason == "client_gone" {
			reason = r
		}
		reasonMu.Unlock()
	}

	if opts.maxLife > 0 {
		var lifeCancel context.CancelFunc
		ctx, lifeCancel = context.WithTimeout(ctx, opts.maxLife)
		defer lifeCancel()
		go func() {
			<-ctx.Done()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				setReason("max_lifetime")
			}
		}()
	}

	a.SetReadLimit(int64(opts.maxBytes))
	b.SetReadLimit(int64(opts.maxBytes))

	// Idle watchdog: each successful read bumps lastActivity.
	var actMu sync.Mutex
	lastActivity := time.Now()
	bump := func() {
		actMu.Lock()
		lastActivity = time.Now()
		actMu.Unlock()
	}
	if opts.idle > 0 {
		go func() {
			tick := opts.idle / 4
			if tick < time.Second {
				tick = time.Second
			}
			if tick > opts.idle {
				tick = opts.idle
			}
			t := time.NewTicker(tick)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					actMu.Lock()
					idleFor := time.Since(lastActivity)
					actMu.Unlock()
					if idleFor >= opts.idle {
						setReason("idle_timeout")
						cancel()
						return
					}
				}
			}
		}()
	}

	var wg sync.WaitGroup
	copyDir := func(dst, src *websocket.Conn) {
		defer wg.Done()
		defer cancel()
		for {
			typ, data, err := src.Read(ctx)
			if err != nil {
				return
			}
			bump()
			if err := dst.Write(ctx, typ, data); err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go copyDir(a, b)
	go copyDir(b, a)
	wg.Wait()
	_ = a.Close(websocket.StatusNormalClosure, "")
	_ = b.Close(websocket.StatusNormalClosure, "")
	reasonMu.Lock()
	defer reasonMu.Unlock()
	return reason
}

func readEnv(ctx context.Context, c *websocket.Conn) (Envelope, error) {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if typ != websocket.MessageText {
		return Envelope{}, fmt.Errorf("expected text frame")
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, err
	}
	if env.V != 0 && env.V != Version {
		return Envelope{}, fmt.Errorf("unsupported version %d", env.V)
	}
	return env, nil
}

func writeEnv(ctx context.Context, c *websocket.Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func writeErr(ctx context.Context, c *websocket.Conn, id, code, msg string) error {
	// Pick error type based on context is caller's job; use generic TypeError
	// or typed variants.
	typ := TypeError
	switch code {
	case "unauthorized":
		// keep TypeError; hosts/phones both understand code
	}
	env, err := NewEnvelope(typ, id, ErrorPayload{Code: code, Message: msg})
	if err != nil {
		return err
	}
	// Prefer typed errors for register/join when id present
	_ = typ
	return writeEnv(ctx, c, env)
}
