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
	rate   map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

// New creates a relay server. Call [Server.ListenAndServe] or [Server.Serve].
func New(cfg Config, log *slog.Logger) *Server {
	if cfg.Limits.MaxHosts == 0 {
		cfg.Limits = DefaultLimits()
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:  cfg,
		hub:  newHub(cfg.Allow, cfg.Limits, log),
		log:  log.With(slog.String("component", "mcrelay")),
		rate: make(map[string]*rateWindow),
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
	if s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "" {
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
	} else {
		s.log.Warn("listening without TLS — join plane is cleartext; use only on loopback or tests",
			slog.String("addr", ln.Addr().String()))
	}
	return s.Serve(ctx, ln)
}

// Serve serves on an existing listener (tests may pass plain TCP).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.http.Serve(ln)
	}()
	select {
	case <-ctx.Done():
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

// Addr is the configured listen address (may be ":0" before serve).
func (s *Server) Addr() string { return s.cfg.ListenAddr }

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"service":"mcrelay"}`))
}

func (s *Server) allowAccept(r *http.Request) bool {
	ip := clientIP(r)
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	w := s.rate[ip]
	if w == nil || now.Sub(w.start) >= time.Minute {
		s.rate[ip] = &rateWindow{start: now, count: 1}
		return true
	}
	if w.count >= s.cfg.Limits.AcceptPerMinute {
		return false
	}
	w.count++
	return true
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
		// Native clients; no browser Origin game for v1.
		OriginPatterns: []string{"*"},
	})
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
	if !s.hub.checkSecret(reg.HostID, reg.Secret) {
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
	go func() {
		defer cancel()
		defer s.hub.unregister(reg.HostID, conn)
		defer conn.Close(websocket.StatusNormalClosure, "")
		for {
			_, _, err := conn.Read(hostCtx)
			if err != nil {
				return
			}
			// Host should not send data frames on control after register;
			// ignore or tear down — ignore keeps pings flexible.
		}
	}()
	<-hostCtx.Done()
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
	pending, err := s.hub.beginJoin(join.HostID, conn)
	if err != nil {
		code := err.Error()
		_ = writeErr(ctx, conn, env.ID, code, code)
		_ = conn.Close(websocket.StatusTryAgainLater, code)
		s.log.Info("join denied", slog.String("host_id", join.HostID), slog.String("reason", code))
		return
	}

	// Ask host to open a tunnel.
	dial, _ := NewEnvelope(TypeDial, "", SessionPayload{
		SessionID: pending.sessionID,
		HostID:    join.HostID,
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
	splice(ctx, conn, tunnel, s.cfg.Limits.MaxMessageBytes)
	close(pending.done)
	s.hub.endPhone(join.HostID)
	s.log.Info("splice ended",
		slog.String("host_id", join.HostID),
		slog.String("session_id", pending.sessionID),
		slog.String("reason", "client_gone"))
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
	pending, err := s.hub.completeTunnel(tun.SessionID, tun.HostID, tun.Secret, conn)
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

func splice(ctx context.Context, a, b *websocket.Conn, maxBytes int) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.SetReadLimit(int64(maxBytes))
	b.SetReadLimit(int64(maxBytes))

	var wg sync.WaitGroup
	copyDir := func(dst, src *websocket.Conn) {
		defer wg.Done()
		defer cancel()
		for {
			typ, data, err := src.Read(ctx)
			if err != nil {
				return
			}
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
