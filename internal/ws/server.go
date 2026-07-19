// Package ws implements the mcremote.v1 WebSocket control plane.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Server hosts WebSocket clients and HTTP health endpoints.
type Server struct {
	store              *auth.Store
	sessions           *session.Manager
	registry           *provider.Registry
	requireDeviceToken bool
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
}

// Options configure the WS server.
type Options struct {
	Store              *auth.Store
	Sessions           *session.Manager
	Registry           *provider.Registry
	RequireDeviceToken bool
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
		sessions:           opts.Sessions,
		registry:           opts.Registry,
		requireDeviceToken: opts.RequireDeviceToken,
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

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"version": s.version,
	})
}

func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
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
	if authz := r.Header.Get("Authorization"); len(authz) > 7 && authz[:7] == "Bearer " {
		if _, err := s.authenticate(c, authz[7:]); err != nil {
			_ = s.writeAuthError(ctx, c, "", err)
		}
	}

	authDeadline := time.Now().Add(5 * time.Second)
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

	if s.requireDeviceToken && !c.authed && env.Type != protocol.TypeAuth {
		return s.writeError(ctx, c, env.ID, "unauthorized", "authenticate first")
	}

	switch env.Type {
	case protocol.TypeAuth:
		return s.handleAuth(ctx, c, env)
	case protocol.TypePing:
		out, _ := protocol.NewEnvelope(protocol.TypePong, env.ID, nil)
		return s.writeJSON(ctx, c, out)
	case protocol.TypeSessionCreate:
		return s.handleSessionCreate(ctx, c, env)
	case protocol.TypeSessionList:
		return s.handleSessionList(ctx, c, env)
	case protocol.TypeSessionClose:
		return s.handleSessionClose(ctx, c, env)
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
	c.authed = true
	c.deviceID = dev.ID
	s.log.Info("device authenticated", slog.String("device_id", dev.ID), slog.String("device_name", dev.Name))
	return dev, nil
}

func (s *Server) handleSessionCreate(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionCreatePayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.Provider == "" {
		p.Provider = string(provider.IDFake)
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

func (s *Server) writeAuthError(ctx context.Context, c *client, id string, err error) error {
	code := "auth_failed"
	if errors.Is(err, auth.ErrInvalidToken) {
		code = "invalid_token"
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
