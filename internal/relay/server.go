package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Server is the public mcrelay edge.
type Server struct {
	cfg  Config
	hub  *hub
	log  *slog.Logger
	http *http.Server

	// trustedProxies from Config (MADR 0017 E1); nil/empty → RemoteAddr only.
	trustedProxies []*net.IPNet

	rateMu sync.Mutex
	// rate is keyed per bucket+id (R16 multi-bucket). A comparable struct
	// key makes the hot-path lookup allocation-free (0115 F13).
	rate map[rateKey]*rateWindow

	// activeSplices tracks live opaque tunnels so Shutdown can drain them
	// (MADR 0016 R17).
	activeMu      sync.Mutex
	activeSplices map[*activeSplice]struct{}

	// sweeperCancel stops R18 orphan-pending GC and R39 rate-map prune.
	sweeperCancel context.CancelFunc
}

type rateWindow struct {
	start time.Time
	count int
}

// rateKey identifies one sliding window: a bucket name plus a client identity
// (IP or host_id).
type rateKey struct {
	bucket, id string
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
	// hostID lets the slot sweep (0068 P6) count live splices per host
	// without walking the hub's join state.
	hostID string
}

// slotSweepInterval paces the phone-slot reconciliation (0068 P6). A
// divergence is corrected only after surviving two consecutive sweeps, so
// the worst-case self-heal is ~2× this.
const slotSweepInterval = 30 * time.Second

// spliceCopyBufSize is the io.CopyBuffer size for opaque splice (MADR 0017 E2).
const spliceCopyBufSize = 32 * 1024

// spliceBufPool reuses CopyBuffer scratch for both splice directions.
var spliceBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, spliceCopyBufSize)
		return &b
	},
}

// New creates a relay server. Call [Server.ListenAndServe] or [Server.Serve].
func New(cfg Config, log *slog.Logger) *Server {
	// Field-wise defaults only (MADR 0016 R4): never replace a partially set Limits.
	cfg.Limits = ResolvedLimits(cfg.Limits)
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:            cfg,
		hub:            newHub(cfg.Allow, cfg.Limits, cfg.AllowLegacyTunnelSecret, log),
		log:            log.With(slog.String("component", "mcrelay")),
		trustedProxies: cfg.TrustedProxies,
		rate:           make(map[rateKey]*rateWindow),
		activeSplices:  make(map[*activeSplice]struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/host", s.handleHost)
	mux.HandleFunc("GET /v1/phone", s.handlePhone)
	mux.HandleFunc("GET /v1/tunnel", s.handleTunnel)
	p := new(http.Protocols)
	p.SetHTTP1(true)
	s.http = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       httpIdleTimeout(),
		// 0091 D3: stdlib default is 1 MiB; join-plane handshakes are small.
		MaxHeaderBytes: maxHeaderBytes,
		Protocols:      p,
		// Internet scanners hit :8443 with SSLv2/TLS1.0/junk ciphers and
		// the stdlib logs each attempt at Info via ErrorLog. Demote those
		// to Debug so ops can still see real failures without drowning
		// host_registered / splice lines (overnight forensics 2026-08).
		ErrorLog: stdlog.New(&tlsHandshakeLogFilter{lg: s.log}, "", 0),
	}
	return s
}

// tlsHandshakeLogFilter routes net/http TLS handshake ErrorLog lines through
// slog at Debug (scanner noise) and everything else at Warn.
type tlsHandshakeLogFilter struct {
	lg *slog.Logger
}

func (f *tlsHandshakeLogFilter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "tls handshake error") ||
		strings.Contains(lower, "http request to an https server") ||
		strings.Contains(lower, "unsupported sslv2") {
		f.lg.Debug(msg)
		return len(p), nil
	}
	f.lg.Warn(msg)
	return len(p), nil
}

// Handler returns the HTTP handler (tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }

// firstEnvelopeTimeout bounds the wait for the first control envelope after
// a WS upgrade (MADR 0068 P1): a peer that upgrades and then suspends —
// an iOS phone backgrounding mid-join — previously held its goroutine and
// connection forever, uncounted by any pool. Mirrors ReadHeaderTimeout.
//
// Atomic, not a plain var, because tests shorten it: a package-level write from
// one test's cleanup raced the read below in a still-draining httptest handler
// goroutine belonging to another test, which the race detector fails the run
// for. Accessed only through the two helpers.
var firstEnvelopeTimeoutNanos atomic.Int64

func init() {
	firstEnvelopeTimeoutNanos.Store(int64(10 * time.Second))
	httpIdleTimeoutNanos.Store(int64(120 * time.Second))
}

var httpIdleTimeoutNanos atomic.Int64

func httpIdleTimeout() time.Duration {
	return time.Duration(httpIdleTimeoutNanos.Load())
}

func setHTTPIdleTimeout(d time.Duration) time.Duration {
	return time.Duration(httpIdleTimeoutNanos.Swap(int64(d)))
}

func firstEnvelopeTimeout() time.Duration {
	return time.Duration(firstEnvelopeTimeoutNanos.Load())
}

// setFirstEnvelopeTimeout swaps the deadline and returns the previous value so
// a caller can restore it. Tests only.
func setFirstEnvelopeTimeout(d time.Duration) time.Duration {
	return time.Duration(firstEnvelopeTimeoutNanos.Swap(int64(d)))
}

// maxHeaderBytes is the HTTP handshake cap (0091 D3). 16 KiB covers a
// WebSocket upgrade; the stdlib default of 1 MiB is a free DoS.
const maxHeaderBytes = 16 << 10

func (s *Server) requireTLSOrLoopback() error {
	if s.cfg.TLSConfig != nil || (s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "") {
		return nil
	}
	if s.cfg.AllowPlaintext || listenHostIsLoopback(s.cfg.ListenAddr) {
		return nil
	}
	return fmt.Errorf("plaintext listen on %s refused: set tls.mode=files|letsencrypt, bind a loopback address, or pass --allow-plaintext",
		s.cfg.ListenAddr)
}

func listenHostIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" || host == "*" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ListenAndServe binds and serves until ctx cancel or error.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.requireTLSOrLoopback(); err != nil {
		return err
	}
	// Kernel keepalive on accepted connections (MADR 0068 P1), same shape
	// as the daemon's listener: silent peers reaped at ~45 s.
	lc := net.ListenConfig{
		KeepAliveConfig: net.KeepAliveConfig{
			Enable:   true,
			Idle:     25 * time.Second,
			Interval: 5 * time.Second,
			Count:    4,
		},
	}
	ln, err := lc.Listen(ctx, "tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	// 0142 F19: cap concurrent accepts before TLS so handshake floods count.
	ln = limitListener(ln, s.cfg.Limits.MaxConns)
	switch {
	case s.cfg.TLSConfig != nil:
		cfg := s.cfg.TLSConfig.Clone()
		if cfg.MinVersion == 0 {
			cfg.MinVersion = tls.VersionTLS12
		}
		stripHTTP2(cfg)
		ln = tls.NewListener(ln, cfg)
		s.log.Info("listening", slog.String("addr", ln.Addr().String()), slog.String("tls", "managed"))
	case s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "":
		cert, err := tls.LoadX509KeyPair(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		if err != nil {
			_ = ln.Close()
			return err
		}
		cfg := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
		stripHTTP2(cfg)
		ln = tls.NewListener(ln, cfg)
		s.log.Info("listening", slog.String("addr", ln.Addr().String()), slog.String("tls", "files"))
	default:
		s.log.Warn("listening without TLS — join plane is cleartext; use only on loopback or tests",
			slog.String("addr", ln.Addr().String()))
	}
	return s.Serve(ctx, ln)
}

// Serve serves on an existing listener (tests may pass plain TCP).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.http.IdleTimeout = httpIdleTimeout()
	s.startPendingSweeper(ctx)
	defer s.stopPendingSweeper()

	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("http serve panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				errCh <- fmt.Errorf("http serve panic: %v", r)
			}
		}()
		errCh <- s.http.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		// R17 / D11: tear down hijacked WebSocket splices and host controls.
		s.drainConnections("shutdown")
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shCtx)
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		s.drainConnections("serve_error")
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// drainConnections closes active splices and host control sockets (MADR 0017 D11).
func (s *Server) drainConnections(reason string) {
	s.closeAllSplices(reason)
	s.hub.closeAllHosts(reason)
}

// ratePruneInterval is how often the background job drops TTL-expired rate
// windows (MADR 0017 E3 / R39). Hot-path allowRate only prunes under hard cap.
const ratePruneInterval = 30 * time.Second

// startPendingSweeper runs R18 orphan GC and R39 rate-map prune until stop.
func (s *Server) startPendingSweeper(ctx context.Context) {
	s.stopPendingSweeper()
	ctx, cancel := context.WithCancel(ctx)
	s.sweeperCancel = cancel
	// Interval: half of TunnelWait so orphans clear soon after phone-side timeout.
	every := max(s.cfg.Limits.TunnelWait/2, time.Second)
	maxAge := max(s.cfg.Limits.TunnelWait*2, 30*time.Second)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("pending sweeper panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		pendingTick := time.NewTicker(every)
		rateTick := time.NewTicker(ratePruneInterval)
		slotTick := time.NewTicker(slotSweepInterval)
		defer pendingTick.Stop()
		defer rateTick.Stop()
		defer slotTick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pendingTick.C:
				if n := s.hub.expireStalePending(maxAge); n > 0 {
					s.log.Info("expired orphan pending joins", slog.Int("count", n))
				}
			case <-rateTick.C:
				s.rateMu.Lock()
				s.pruneRateLocked(time.Now())
				s.rateMu.Unlock()
			case <-slotTick.C:
				s.hub.reconcilePhones(s.spliceHostCounts())
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
func (s *Server) trackSplice(cancel context.CancelFunc, a, b *websocket.Conn, hostID string) (untrack func()) {
	tr := &activeSplice{cancel: cancel, a: a, b: b, hostID: hostID}
	s.activeMu.Lock()
	s.activeSplices[tr] = struct{}{}
	s.activeMu.Unlock()
	return func() {
		s.activeMu.Lock()
		delete(s.activeSplices, tr)
		s.activeMu.Unlock()
	}
}

// spliceHostCounts snapshots live splices per host for the slot sweep
// (0068 P6).
func (s *Server) spliceHostCounts() map[string]int {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	counts := make(map[string]int, len(s.activeSplices))
	for tr := range s.activeSplices {
		counts[tr.hostID]++
	}
	return counts
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

// retryFloor is the smallest retry_after hint ever sent (0068 P6): capacity
// refusals have no window to measure, and sub-second hints would just invite
// a tight retry loop.
const retryFloor = 5 * time.Second

// allowRate enforces a per-key sliding one-minute window (R16 multi-bucket).
func (s *Server) allowRate(id, bucket string, max int) bool {
	ok, _ := s.allowRateRetry(id, bucket, max)
	return ok
}

// allowRateRetry is allowRate plus, on denial, the remainder of the fixed
// window — the earliest instant a retry can possibly pass (0068 P6).
// TTL cleanup of other keys runs in the background (E3); the hot path only
// prunes when the map is at capacity so lock hold time stays small under load.
func (s *Server) allowRateRetry(id, bucket string, max int) (bool, time.Duration) {
	if max <= 0 {
		return true, 0
	}
	key := rateKey{bucket: bucket, id: id}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	if len(s.rate) >= rateMapMax {
		s.pruneRateLocked(now)
	}
	w := s.rate[key]
	if w == nil || now.Sub(w.start) >= time.Minute {
		if w == nil {
			// Still at capacity after the TTL prune: evict the oldest window,
			// deterministically (0115 F4). Random eviction let an attacker
			// filling the map reset active windows — including their own.
			for len(s.rate) >= rateMapMax {
				s.evictOldestRateLocked()
			}
		}
		s.rate[key] = &rateWindow{start: now, count: 1}
		return true, 0
	}
	if w.count >= max {
		return false, w.start.Add(time.Minute).Sub(now)
	}
	w.count++
	return true, 0
}

func (s *Server) pruneRateLocked(now time.Time) {
	for key, w := range s.rate {
		if now.Sub(w.start) >= rateWindowTTL {
			delete(s.rate, key)
		}
	}
	// Hard cap safety net: oldest windows go first (0115 F4).
	for len(s.rate) > rateMapMax {
		s.evictOldestRateLocked()
	}
}

// evictOldestRateLocked removes the window with the earliest start. Linear
// scan over at most rateMapMax entries, and only on capacity events — the
// steady-state hot path never reaches it. Caller holds rateMu.
func (s *Server) evictOldestRateLocked() {
	var (
		oldestKey rateKey
		oldest    time.Time
		found     bool
	)
	for key, w := range s.rate {
		if !found || w.start.Before(oldest) {
			oldestKey, oldest, found = key, w.start, true
		}
	}
	if found {
		delete(s.rate, oldestKey)
	}
}

func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	if ok, retry := s.allowRateRetry(s.clientIP(r), rateBucketAccept, s.cfg.Limits.AcceptPerMinute); !ok {
		// Standard header, whole seconds, rounded up (RFC 9110 §10.2.3).
		w.Header().Set("Retry-After", strconv.Itoa(int((retry+time.Second-1)/time.Second)))
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

func (s *Server) rateLimitedWS(ctx context.Context, conn *websocket.Conn, reqID string, retry time.Duration) {
	if retry < retryFloor {
		retry = retryFloor
	}
	_ = writeErrRetry(ctx, conn, reqID, "rate_limited", "rate_limited", retry)
	_ = conn.Close(websocket.StatusTryAgainLater, "rate_limited")
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	// D16: control plane uses a small read limit (not splice MaxMessageBytes).
	conn.SetReadLimit(int64(ControlReadLimitBytes))

	env, err := readFirstEnv(ctx, conn)
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
	if err := DecodePayload(env, &reg); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid register payload")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	reg.HostID = strings.TrimSpace(reg.HostID)
	// MADR 0017 D7: validate before rate keys / hub.
	if err := validateHostID(reg.HostID); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid host_id")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	// R16: separate register bucket (do not share only with accept/join).
	if ok, retry := s.allowRateRetry(s.clientIP(r), rateBucketRegister, s.cfg.Limits.RegisterPerMinute); !ok {
		s.rateLimitedWS(ctx, conn, env.ID, retry)
		return
	}
	match := s.hub.checkSecret(reg.HostID, reg.Secret)
	reg.Secret = ""
	env.Payload = nil
	if !match {
		// Same error for unknown host_id and wrong secret (no enumeration).
		_ = writeErr(ctx, conn, env.ID, "unauthorized", "invalid host credentials")
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		s.log.Info("register denied", slog.String("host_id", slogHostID(reg.HostID)), slog.String("reason", "unauthorized"))
		return
	}
	hostCtx, cancel := context.WithCancel(ctx)
	if err := s.hub.register(reg.HostID, conn, cancel); err != nil {
		code := errCode(err)
		_ = writeErr(ctx, conn, env.ID, code, code)
		_ = conn.Close(websocket.StatusTryAgainLater, code)
		cancel()
		return
	}
	ok, _ := NewEnvelope(TypeRegisterOK, env.ID, SessionPayload{HostID: reg.HostID})
	if err := WriteEnvelope(ctx, conn, ok); err != nil {
		s.hub.unregister(reg.HostID, conn)
		cancel()
		return
	}

	// Keep control connection alive; dial notifications are written here.
	// Read loop: any client close ends registration.
	// R5: concurrent app-level Ping on RegisterIdle so idle middleboxes
	// do not silently drop the host registration.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("host control read loop panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				cancel()
			}
		}()
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
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("host ping panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			fail()
		}
	}()
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
	// D16 extended (0115 F2): control limit until join_ok; splice raises it.
	conn.SetReadLimit(int64(ControlReadLimitBytes))

	env, err := readFirstEnv(ctx, conn)
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
	if err := DecodePayload(env, &join); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid join payload")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	join.HostID = strings.TrimSpace(join.HostID)
	// MADR 0017 D7: validate before rate keys that embed host_id (R29).
	if err := validateHostID(join.HostID); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid host_id")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	// R16 / H3: per-IP and per-host_id join caps (enumeration / join spam).
	ip := s.clientIP(r)
	okIP, retryIP := s.allowRateRetry(ip, rateBucketJoin, s.cfg.Limits.JoinPerMinute)
	okHost, retryHost := s.allowRateRetry(join.HostID, rateBucketJoinHost, s.cfg.Limits.JoinPerHostPerMinute)
	if !okIP || !okHost {
		s.rateLimitedWS(ctx, conn, env.ID, max(retryIP, retryHost))
		s.log.Info("join denied", slog.String("host_id", slogHostID(join.HostID)), slog.String("reason", "rate_limited"))
		return
	}
	pending, err := s.hub.beginJoin(join.HostID, conn)
	if err != nil {
		code := errCode(err)
		if errors.Is(err, errLimit) {
			// Capacity, not a window: no remainder to measure, so send the
			// courtesy floor rather than nothing (0068 P6).
			_ = writeErrRetry(ctx, conn, env.ID, code, code, retryFloor)
		} else {
			_ = writeErr(ctx, conn, env.ID, code, code)
		}
		_ = conn.Close(websocket.StatusTryAgainLater, code)
		s.log.Info("join denied", slog.String("host_id", slogHostID(join.HostID)), slog.String("reason", code))
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
		if orphan := s.hub.phoneGone(pending); orphan != nil {
			_ = orphan.Close(websocket.StatusGoingAway, "phone_gone")
		}
		_ = writeErr(ctx, conn, env.ID, "timeout", "host did not open tunnel")
		_ = conn.Close(websocket.StatusTryAgainLater, "timeout")
		s.log.Info("join timeout", slog.String("host_id", slogHostID(join.HostID)), slog.String("session_id", pending.sessionID))
		return
	case <-ctx.Done():
		if orphan := s.hub.phoneGone(pending); orphan != nil {
			_ = orphan.Close(websocket.StatusGoingAway, "phone_gone")
		}
		return
	}

	ok, _ := NewEnvelope(TypeJoinOK, env.ID, SessionPayload{
		SessionID: pending.sessionID,
		HostID:    join.HostID,
	})
	if err := WriteEnvelope(ctx, conn, ok); err != nil {
		_ = tunnel.Close(websocket.StatusGoingAway, "phone gone")
		pending.closeDone()
		s.hub.endPhone(join.HostID)
		return
	}

	s.log.Info("join ok",
		slog.String("host_id", slogHostID(join.HostID)),
		slog.String("session_id", pending.sessionID),
		slog.String("reason", "splice"))
	// Opaque splice: no protocol-v1 parsing (MADR 0015 D2).
	// R15: idle + max lifetime; R17: tracked for shutdown drain.
	// E2: Reader/Writer + pooled CopyBuffer.
	spliceOpts := spliceOptions{
		maxBytes: s.cfg.Limits.MaxMessageBytes,
		idle:     s.cfg.Limits.SpliceIdle,
		maxLife:  s.cfg.Limits.SpliceMax,
	}
	spliceCtx, spliceCancel := context.WithCancel(ctx)
	untrack := s.trackSplice(spliceCancel, conn, tunnel, join.HostID)
	reason := splice(spliceCtx, conn, tunnel, spliceOpts, s.log)
	untrack()
	spliceCancel()
	pending.closeDone()
	s.hub.endPhone(join.HostID)
	s.log.Info("splice ended",
		slog.String("host_id", slogHostID(join.HostID)),
		slog.String("session_id", pending.sessionID),
		slog.String("reason", reason))
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	// D16 extended (0115 F2): control limit until tunnel_ok; splice raises it.
	conn.SetReadLimit(int64(ControlReadLimitBytes))

	env, err := readFirstEnv(ctx, conn)
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
	if err := DecodePayload(env, &tun); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid tunnel payload")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	tun.HostID = strings.TrimSpace(tun.HostID)
	tun.SessionID = strings.TrimSpace(tun.SessionID)
	if err := validateHostID(tun.HostID); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid host_id")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	if err := validateSessionID(tun.SessionID); err != nil {
		_ = writeErr(ctx, conn, env.ID, "bad_payload", "invalid session_id")
		_ = conn.Close(websocket.StatusPolicyViolation, "bad_payload")
		return
	}
	pending, err := s.hub.claimTunnel(tun.SessionID, tun.HostID, tun.Token, tun.Secret)
	tun.Secret = ""
	tun.Token = ""
	env.Payload = nil
	if err != nil {
		code := errCode(err)
		_ = writeErr(ctx, conn, env.ID, code, code)
		_ = conn.Close(websocket.StatusPolicyViolation, code)
		return
	}
	// Handshake BEFORE publishing: handlePhone splices as soon as it has the
	// tunnel, so a tunnel published first can receive phone bytes in place of
	// this envelope (see claimTunnel).
	ok, _ := NewEnvelope(TypeTunnelOK, env.ID, SessionPayload{
		SessionID: pending.sessionID,
		HostID:    pending.hostID,
	})
	if err := WriteEnvelope(ctx, conn, ok); err != nil {
		s.hub.abandonTunnel(pending)
		return
	}
	if !s.hub.publishTunnel(pending, conn) {
		// Phone left between claim and publish; tunnel_ok is already sent, so
		// there is no error envelope to send — just close.
		_ = conn.Close(websocket.StatusGoingAway, "phone gone")
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
// maxLife is reached. Uses Reader/Writer + pooled buffer (MADR 0017 E2).
// Returns a short reason for logging (R15).
func splice(ctx context.Context, a, b *websocket.Conn, opts spliceOptions, log *slog.Logger) string {
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
			defer func() {
				if r := recover(); r != nil {
					log.Error("max lifetime watchdog panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				}
			}()
			<-ctx.Done()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				setReason("max_lifetime")
			}
		}()
	}

	a.SetReadLimit(int64(opts.maxBytes))
	b.SetReadLimit(int64(opts.maxBytes))

	// Idle watchdog: each successful frame transfer bumps lastActivity.
	var actMu sync.Mutex
	lastActivity := time.Now()
	bump := func() {
		actMu.Lock()
		lastActivity = time.Now()
		actMu.Unlock()
	}
	if opts.idle > 0 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("idle watchdog panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				}
			}()
			tick := min(max(opts.idle/4, time.Second), opts.idle)
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
		defer func() {
			if r := recover(); r != nil {
				log.Error("splice copyDir panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				cancel()
			}
		}()
		defer cancel()
		bufPtr := spliceBufPool.Get().(*[]byte)
		buf := *bufPtr
		defer func() {
			spliceBufPool.Put(bufPtr)
		}()
		for {
			typ, r, err := src.Reader(ctx)
			if err != nil {
				return
			}
			w, err := dst.Writer(ctx, typ)
			if err != nil {
				// Drain reader so the connection state stays consistent.
				_, _ = io.Copy(io.Discard, r)
				return
			}
			_, copyErr := io.CopyBuffer(w, r, buf)
			closeErr := w.Close()
			if copyErr != nil || closeErr != nil {
				return
			}
			bump()
		}
	}
	wg.Go(func() { copyDir(a, b) })
	wg.Go(func() { copyDir(b, a) })
	wg.Wait()
	_ = a.Close(websocket.StatusNormalClosure, "")
	_ = b.Close(websocket.StatusNormalClosure, "")
	reasonMu.Lock()
	defer reasonMu.Unlock()
	return reason
}

// readFirstEnv bounds the first control envelope after an upgrade with
// firstEnvelopeTimeout (MADR 0068 P1). Later reads on join/register planes
// keep their own lifecycles.
func readFirstEnv(ctx context.Context, c *websocket.Conn) (Envelope, error) {
	rctx, cancel := context.WithTimeout(ctx, firstEnvelopeTimeout())
	defer cancel()
	return ReadEnvelope(rctx, c)
}

func writeErr(ctx context.Context, c *websocket.Conn, id, code, msg string) error {
	env, err := NewEnvelope(TypeError, id, ErrorPayload{Code: code, Message: msg})
	if err != nil {
		return err
	}
	return WriteEnvelope(ctx, c, env)
}

// writeErrRetry is writeErr with a retry_after_ms hint (0068 P6).
func writeErrRetry(ctx context.Context, c *websocket.Conn, id, code, msg string, retry time.Duration) error {
	env, err := NewEnvelope(TypeError, id, ErrorPayload{
		Code:         code,
		Message:      msg,
		RetryAfterMS: retry.Milliseconds(),
	})
	if err != nil {
		return err
	}
	return WriteEnvelope(ctx, c, env)
}
