// Package ws implements the mcremote.v1 WebSocket control plane.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Server hosts WebSocket clients and HTTP health endpoints.
type Server struct {
	store              *auth.Store
	pairCodes          *auth.PairCodeStore
	sessions           *session.Manager
	registry           *provider.Registry
	requireDeviceToken bool
	requireClientKey   bool
	version            string
	listenAddr         string
	headscaleURL       string
	log                *slog.Logger

	mu      sync.Mutex
	clients map[*client]struct{}
}

type client struct {
	conn     *websocket.Conn
	deviceID string
	authed   bool
	writeMu  sync.Mutex
	// failedClaims counts unsuccessful pair.claim attempts on this connection.
	failedClaims int
	// clientKeyFP is the SPKI fingerprint of the client certificate presented at
	// TLS handshake time (ADR 0005), captured once at upgrade. Empty means no
	// client certificate was presented (or TLS is not terminated here).
	clientKeyFP string
}

// Options configure the WS server.
type Options struct {
	Store              *auth.Store
	PairCodes          *auth.PairCodeStore
	Sessions           *session.Manager
	Registry           *provider.Registry
	RequireDeviceToken bool
	RequireClientKey   bool
	Version            string
	ListenAddr         string
	HeadscaleURL       string
	Log                *slog.Logger
}

// New creates a Server.
func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		store:              opts.Store,
		pairCodes:          opts.PairCodes,
		sessions:           opts.Sessions,
		registry:           opts.Registry,
		requireDeviceToken: opts.RequireDeviceToken,
		requireClientKey:   opts.RequireClientKey,
		version:            opts.Version,
		listenAddr:         opts.ListenAddr,
		headscaleURL:       opts.HeadscaleURL,
		log:                log.With(slog.String("component", "ws")),
		clients:            make(map[*client]struct{}),
	}
}

// Handler returns the root HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/hello", s.handleHello)
	mux.HandleFunc("GET /v1/ws", s.handleWS)
	return mux
}

// BroadcastEvent sends an event to all authenticated clients.
func (s *Server) BroadcastEvent(ev event.Event) {
	env, err := protocol.NewEnvelope(protocol.TypeEvent, "", protocol.EventPayload{Event: ev})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if !c.authed {
			continue
		}
		_ = s.writeJSON(context.Background(), c, env)
	}
}

// DisconnectDevice closes all connections for a device id (after revoke).
func (s *Server) DisconnectDevice(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if c.deviceID == deviceID {
			_ = c.conn.Close(websocket.StatusPolicyViolation, "device revoked")
		}
	}
}

// handleHealthz is the unauthenticated liveness probe. It deliberately
// discloses nothing beyond "the process is up" — anything identifying the
// service or the mesh belongs on the authenticated /v1/hello.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true,
	})
}

// bearerToken extracts a Bearer credential from the Authorization header.
func bearerToken(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	if len(authz) > 7 && strings.EqualFold(authz[:7], "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}

// authorizeHTTP reports whether an HTTP request carries a valid device token,
// using the same store as the WebSocket auth path.
func (s *Server) authorizeHTTP(r *http.Request) bool {
	if !s.requireDeviceToken {
		return true
	}
	token := bearerToken(r)
	if token == "" || s.store == nil {
		return false
	}
	_, err := s.store.Validate(token)
	return err == nil
}

func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHTTP(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcremote"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "unauthorized",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":               s.version,
		"listen":                s.listenAddr,
		"headscale_control_url": s.headscaleURL,
		"protocol":              protocol.Version,
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Local/mesh clients; Flutter web may need origin flexibility later.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.log.Warn("websocket accept failed", slog.String("err", err.Error()))
		return
	}

	c := &client{conn: conn}
	// Capture the presented client key before any message is read. With the
	// listener's tls.RequestClientCert, a presented certificate appears here
	// even though it is unverified — possession is what the handshake proves.
	// The auth and pair.claim handlers compare this fingerprint against the
	// store (ADR 0005).
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		c.clientKeyFP = certs.SPKIFingerprint(r.TLS.PeerCertificates[0])
	}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	ctx := r.Context()
	// Optional Bearer on upgrade.
	if token := bearerToken(r); token != "" {
		if _, err := s.authenticate(c, token); err != nil {
			_ = s.writeAuthError(ctx, c, "", err)
		}
	}

	// 30s window so the phone can connect and immediately send pair.claim
	// after the user finishes typing an 8-char code.
	authDeadline := time.Now().Add(30 * time.Second)
	for {
		readCtx := ctx
		if s.requireDeviceToken && !c.authed {
			var cancel context.CancelFunc
			readCtx, cancel = context.WithDeadline(ctx, authDeadline)
			_, data, err := conn.Read(readCtx)
			cancel()
			if err != nil {
				return
			}
			if err := s.handleMessage(ctx, c, data); err != nil {
				s.log.Debug("ws message error", slog.String("err", err.Error()))
			}
			if s.requireDeviceToken && !c.authed && time.Now().After(authDeadline) {
				_ = conn.Close(websocket.StatusPolicyViolation, "auth timeout")
				return
			}
			continue
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := s.handleMessage(ctx, c, data); err != nil {
			s.log.Debug("ws message error", slog.String("err", err.Error()))
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, c *client, data []byte) error {
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return s.writeError(ctx, c, "", "bad_json", "invalid JSON envelope")
	}
	if env.V != 0 && env.V != protocol.Version {
		return s.writeError(ctx, c, env.ID, "bad_version", fmt.Sprintf("unsupported protocol version %d", env.V))
	}

	if s.requireDeviceToken && !c.authed &&
		env.Type != protocol.TypeAuth && env.Type != protocol.TypePairClaim {
		return s.writeError(ctx, c, env.ID, "unauthorized", "authenticate first")
	}

	switch env.Type {
	case protocol.TypeAuth:
		return s.handleAuth(ctx, c, env)
	case protocol.TypePairClaim:
		return s.handlePairClaim(ctx, c, env)
	case protocol.TypePing:
		out, _ := protocol.NewEnvelope(protocol.TypePong, env.ID, nil)
		return s.writeJSON(ctx, c, out)
	case protocol.TypeSessionCreate:
		return s.handleSessionCreate(ctx, c, env)
	case protocol.TypeSessionList:
		return s.handleSessionList(ctx, c, env)
	case protocol.TypeSessionClose:
		return s.handleSessionClose(ctx, c, env)
	case protocol.TypeSessionDelete:
		return s.handleSessionDelete(ctx, c, env)
	case protocol.TypeSessionPrompt:
		return s.handleSessionPrompt(ctx, c, env)
	case protocol.TypeSessionCancel:
		return s.handleSessionCancel(ctx, c, env)
	case protocol.TypeProvidersList:
		return s.handleProvidersList(ctx, c, env)
	case protocol.TypePermissionRespond:
		return s.handlePermissionRespond(ctx, c, env)
	default:
		return s.writeError(ctx, c, env.ID, "unknown_type", "unknown message type: "+env.Type)
	}
}

func (s *Server) handleAuth(ctx context.Context, c *client, env protocol.Envelope) error {
	token := env.Token
	if token == "" {
		var p protocol.AuthPayload
		_ = protocol.DecodePayload(env, &p)
		token = p.Token
	}
	dev, err := s.authenticate(c, token)
	if err != nil {
		return s.writeAuthError(ctx, c, env.ID, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeAuthOK, env.ID, protocol.AuthOKPayload{
		DeviceID:   dev.ID,
		DeviceName: dev.Name,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handlePairClaim(ctx context.Context, c *client, env protocol.Envelope) error {
	if c.authed {
		return s.writePairError(ctx, c, env.ID, "already_authed", "already authenticated")
	}
	if s.pairCodes == nil || s.store == nil {
		return s.writePairError(ctx, c, env.ID, "unavailable", "pair codes not available")
	}
	// Per-connection rate limit on failed claims.
	const maxFailedClaims = 10
	if c.failedClaims >= maxFailedClaims {
		return s.writePairError(ctx, c, env.ID, "rate_limited", "too many failed pair attempts")
	}

	var p protocol.PairClaimPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writePairError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if strings.TrimSpace(p.Code) == "" {
		c.failedClaims++
		return s.writePairError(ctx, c, env.ID, "invalid_code", "pair code required")
	}

	name, err := s.pairCodes.Claim(p.Code)
	if err != nil {
		c.failedClaims++
		code := "invalid_code"
		switch {
		case errors.Is(err, auth.ErrExpiredPairCode):
			code = "expired"
		case errors.Is(err, auth.ErrPairCodeRateLimited):
			code = "rate_limited"
		case errors.Is(err, auth.ErrInvalidPairCode):
			code = "invalid_code"
		}
		return s.writePairError(ctx, c, env.ID, code, err.Error())
	}
	if n := strings.TrimSpace(p.Name); n != "" {
		name = n
	}

	// Enrol the client key (ADR 0005). Under enforcement a device with no client
	// certificate cannot pair; otherwise a presented key is recorded
	// opportunistically (and an absent one leaves a keyless record).
	if s.requireClientKey && c.clientKeyFP == "" {
		return s.writePairError(ctx, c, env.ID, "client_key_required", "a client key is required to pair")
	}

	dev, token, err := s.store.CreateWithClientKey(name, c.clientKeyFP)
	if err != nil {
		return s.writePairError(ctx, c, env.ID, "create_failed", err.Error())
	}
	c.authed = true
	c.deviceID = dev.ID
	s.log.Info("device paired via short code",
		slog.String("device_id", dev.ID),
		slog.String("device_name", dev.Name),
	)
	out, _ := protocol.NewEnvelope(protocol.TypePairOK, env.ID, protocol.PairOKPayload{
		Token:      token,
		DeviceID:   dev.ID,
		DeviceName: dev.Name,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) writePairError(ctx context.Context, c *client, id, code, msg string) error {
	out, _ := protocol.NewEnvelope(protocol.TypePairError, id, protocol.ErrorPayload{
		Message: msg,
		Code:    code,
	})
	return s.writeJSON(ctx, c, out)
}

// errClientKeyRequired and errClientKeyMismatch are the two permanent
// client-key rejection modes (ADR 0005). They map to the auth_error codes
// client_key_required / client_key_mismatch in writeAuthError.
var (
	errClientKeyRequired = errors.New("a client key is required to connect")
	errClientKeyMismatch = errors.New("client key does not match the enrolled key")
)

func (s *Server) authenticate(c *client, token string) (auth.Device, error) {
	if !s.requireDeviceToken {
		c.authed = true
		c.deviceID = "dev"
		return auth.Device{ID: "dev", Name: "dev"}, nil
	}
	dev, err := s.store.Validate(token)
	if err != nil {
		return auth.Device{}, err
	}
	if err := s.verifyClientKey(c, dev); err != nil {
		return auth.Device{}, err
	}
	c.authed = true
	c.deviceID = dev.ID
	s.log.Info("device authenticated", slog.String("device_id", dev.ID), slog.String("device_name", dev.Name))
	return dev, nil
}

// verifyClientKey enforces the client-key allowlist for a token-resolved
// device (ADR 0005). When enforcement is off it is a no-op — a keyless device
// authenticates by token alone. When on: an absent client key is
// errClientKeyRequired, and one that does not equal the enrolled fingerprint
// (including a keyless legacy record, whose empty fingerprint no presented key
// can match) is errClientKeyMismatch. Both are permanent.
func (s *Server) verifyClientKey(c *client, dev auth.Device) error {
	if !s.requireClientKey {
		return nil
	}
	if c.clientKeyFP == "" {
		return errClientKeyRequired
	}
	if c.clientKeyFP != dev.ClientKeyFP {
		return errClientKeyMismatch
	}
	return nil
}

func (s *Server) handleSessionCreate(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionCreatePayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.Provider == "" {
		p.Provider = string(s.defaultProviderID())
	}
	meta, err := s.sessions.Create(ctx, provider.ID(p.Provider), provider.StartOptions{
		Name:           p.Name,
		CWD:            p.CWD,
		AgentSessionID: p.AgentSessionID,
		LocalSessionID: p.SessionID,
	})
	if err != nil {
		return s.writeError(ctx, c, env.ID, "session_create_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionCreated, env.ID, meta)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handlePermissionRespond(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.PermissionRespondPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" || p.PermissionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id and permission_id required")
	}
	if err := s.sessions.RespondPermission(ctx, p.SessionID, p.PermissionID, p.OptionID, p.Cancelled); err != nil {
		return s.writeError(ctx, c, env.ID, "permission_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionList(ctx context.Context, c *client, env protocol.Envelope) error {
	list := s.sessions.List()
	out, _ := protocol.NewEnvelope(protocol.TypeSessionListResult, env.ID, protocol.SessionListResultPayload{Sessions: list})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionClose(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Close(ctx, p.SessionID); err != nil {
		return s.writeError(ctx, c, env.ID, "session_close_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionDelete(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Delete(ctx, p.SessionID); err != nil {
		return s.writeError(ctx, c, env.ID, "session_delete_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionPrompt(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionPromptPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Prompt(ctx, p.SessionID, p.Text); err != nil {
		return s.writeError(ctx, c, env.ID, "session_prompt_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionCancel(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Cancel(ctx, p.SessionID); err != nil {
		return s.writeError(ctx, c, env.ID, "session_cancel_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleProvidersList(ctx context.Context, c *client, env protocol.Envelope) error {
	list := s.registry.List()
	out, _ := protocol.NewEnvelope(protocol.TypeProvidersResult, env.ID, map[string]any{"providers": list})
	return s.writeJSON(ctx, c, out)
}

// defaultProviderID prefers a ready grok provider, otherwise fake, otherwise first registered.
func (s *Server) defaultProviderID() provider.ID {
	list := s.registry.List()
	var fakeReady *provider.Info
	for i := range list {
		info := list[i]
		if info.ID == provider.IDGrok && info.Ready {
			return provider.IDGrok
		}
		if info.ID == provider.IDFake && info.Ready {
			fakeReady = &list[i]
		}
	}
	if fakeReady != nil {
		return fakeReady.ID
	}
	for _, info := range list {
		if info.Ready {
			return info.ID
		}
	}
	if len(list) > 0 {
		return list[0].ID
	}
	return provider.IDFake
}

func (s *Server) writeAuthError(ctx context.Context, c *client, id string, err error) error {
	code := "auth_failed"
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		code = "invalid_token"
	case errors.Is(err, errClientKeyRequired):
		code = "client_key_required"
	case errors.Is(err, errClientKeyMismatch):
		code = "client_key_mismatch"
	}
	out, _ := protocol.NewEnvelope(protocol.TypeAuthError, id, protocol.ErrorPayload{
		Message: err.Error(),
		Code:    code,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) writeError(ctx context.Context, c *client, id, code, msg string) error {
	out, _ := protocol.NewEnvelope(protocol.TypeError, id, protocol.ErrorPayload{Message: msg, Code: code})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) writeJSON(ctx context.Context, c *client, env protocol.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, b)
}
