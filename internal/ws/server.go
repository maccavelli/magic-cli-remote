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
	maxClients         int
	readDeadline       time.Duration

	// TLS status, set once after the certificate is resolved (SetTLSStatus).
	// Guarded by mu because the listener goroutine sets it while requests read.
	tlsMode     string
	tlsFellBack bool

	// Process-wide pair.claim rate limit so new connections cannot reset
	// per-connection failedClaims cheaply (B6).
	pairMu          sync.Mutex
	pairWindowStart time.Time
	pairWindowCount int

	mu      sync.Mutex
	clients map[*client]struct{}
}

// maxPairClaimsPerMinute caps successful+failed pair.claim attempts process-wide.
const maxPairClaimsPerMinute = 30

// SetTLSStatus records the certificate mode actually in force so an
// authenticated /v1/hello can report it. Called once at startup, after the
// certificate is resolved and before requests are served.
func (s *Server) SetTLSStatus(mode string, fellBack bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsMode = mode
	s.tlsFellBack = fellBack
}

type client struct {
	conn *websocket.Conn
	// deviceID and authed are written only under Server.mu (setAuthed) and
	// read under Server.mu by BroadcastEvent/DisconnectDevices. Async handlers
	// never read these fields: dispatchAsync snapshots deviceID under the lock
	// and passes the copy into the handler (Phase 1.1).
	deviceID string
	authed   bool
	// asyncInFlight counts goroutines started by dispatchAsync. Capped at
	// maxAsyncPerClient so a paired device cannot unbounded-spawn create/close
	// work (Phase 1.2 / P1-3).
	asyncInFlight int
	// out is the bounded outbound frame queue, drained by a per-client
	// writeLoop. All writes are enqueues: a peer that stops reading can only
	// fill its own queue and get dropped — it can never block a handler or a
	// session event pump on a socket write.
	out       chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	// failedClaims counts unsuccessful pair.claim attempts on this connection.
	failedClaims int
	// failedAuths counts unsuccessful auth attempts on this connection.
	failedAuths int
	// clientKeyFP is the SPKI fingerprint of the client certificate presented at
	// TLS handshake time (ADR 0005), captured once at upgrade. Empty means no
	// client certificate was presented (or TLS is not terminated here).
	clientKeyFP string
}

// maxAsyncPerClient bounds concurrent dispatchAsync work per WebSocket (D3).
const maxAsyncPerClient = 2

// shutdown signals the writer loop to exit; safe to call more than once.
func (c *client) shutdown() {
	c.closeOnce.Do(func() { close(c.closed) })
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
	// MaxClients caps simultaneous WebSocket connections (0 = unlimited).
	MaxClients int
	// ReadDeadline determines how long the server will wait for a message from an
	// authenticated client before forcefully closing the socket to prevent leaks.
	ReadDeadline time.Duration
}

// New creates a Server.
func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	if opts.ReadDeadline == 0 {
		opts.ReadDeadline = 60 * time.Second
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
		maxClients:         opts.MaxClients,
		readDeadline:       opts.ReadDeadline,
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

// writeDeadline bounds a single WebSocket frame write (broadcast and control).
// Slow clients that exceed it are disconnected (R5=B safety valve).
const writeDeadline = 5 * time.Second

// maxWSMessageBytes is the max inbound WS message size (prompts + history).
// The library default is 32KiB, which is too small for session.history replay.
const maxWSMessageBytes = 1 << 20 // 1 MiB

// outboundQueueLen bounds the per-client outbound frame queue. Deep enough to
// absorb a history replay burst plus streaming chunks; a client that lets it
// fill is not reading and gets dropped.
const outboundQueueLen = 1024

// Caps on client-supplied session.create / pair fields. These flow into map
// keys, disk records, logs, and every broadcast — without caps a single 1MiB
// message can smuggle a megabyte into each.
const (
	maxNameLen           = 256
	maxModelLen          = 256
	maxAgentSessionIDLen = 256
	maxCWDLen            = 4096
)

// BroadcastEvent sends an event to authenticated clients that may see it.
// Session-scoped events go only to the owning device (R4=B); legacy empty
// owner still fans out to all authed clients. Unknown owner fails closed: an
// event whose session cannot be attributed (e.g. racing a delete) reaches
// nobody rather than everybody. Clients are snapshotted under s.mu; delivery
// is a non-blocking enqueue, so a slow peer can never stall the calling event
// pump (N1/R5=B).
func (s *Server) BroadcastEvent(ev event.Event) {
	env, err := protocol.NewEnvelope(protocol.TypeEvent, "", protocol.EventPayload{Event: ev})
	if err != nil {
		s.log.Error("broadcast: encoding event failed; dropping",
			slog.String("type", string(ev.Type)),
			slog.String("err", err.Error()),
		)
		return
	}

	var owner string
	var ownerKnown bool
	if ev.SessionID != "" && s.sessions != nil {
		owner, ownerKnown = s.sessions.OwnerOf(ev.SessionID)
		if !ownerKnown {
			return
		}
	}

	s.mu.Lock()
	targets := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		if !c.authed {
			continue
		}
		if ev.SessionID != "" {
			// Empty owner (legacy) → all devices; else only the owner.
			if owner != "" && c.deviceID != owner {
				continue
			}
		}
		targets = append(targets, c)
	}
	s.mu.Unlock()

	for _, c := range targets {
		_ = s.writeJSON(context.Background(), c, env)
	}
}

// DisconnectDevice closes all connections for a device id (after revoke).
// Returns how many client sockets were closed.
func (s *Server) DisconnectDevice(deviceID string) int {
	return s.DisconnectDevices([]string{deviceID})
}

// DisconnectDevices closes connections whose deviceID is in the list.
// Snapshot under lock, close outside to avoid holding s.mu during I/O.
func (s *Server) DisconnectDevices(deviceIDs []string) int {
	if len(deviceIDs) == 0 {
		return 0
	}
	want := make(map[string]struct{}, len(deviceIDs))
	for _, id := range deviceIDs {
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return 0
	}

	s.mu.Lock()
	var victims []*client
	for c := range s.clients {
		if _, ok := want[c.deviceID]; ok {
			victims = append(victims, c)
		}
	}
	s.mu.Unlock()

	for _, c := range victims {
		// CloseNow, not a graceful close: the coder/websocket close handshake
		// can take ~10s per unresponsive peer, which stalls the admin socket's
		// 2s request deadline — and a revoked device has earned no goodbye.
		s.logDisconnect(c, "revoked")
		c.shutdown()
		_ = c.conn.CloseNow()
	}
	return len(victims)
}

// logDisconnect records why a client was dropped at Info so operators can
// diagnose flapping without enabling debug (Phase 3.6 / 0009 B.3).
func (s *Server) logDisconnect(c *client, reason string) {
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	attrs := []any{slog.String("reason", reason)}
	if deviceID != "" {
		attrs = append(attrs, slog.String("device_id", deviceID))
	}
	s.log.Info("ws client disconnected", attrs...)
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
	dev, err := s.store.Validate(token)
	if err != nil {
		return false
	}
	// Enforce the client key here too, or a stolen token alone would still be
	// a live bearer credential for this recon endpoint (it discloses the
	// Headscale control URL) — the exact property client-key binding removes
	// everywhere else. Mirror the WS capture at handleWS.
	if s.requireClientKey {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			return false
		}
		if certs.SPKIFingerprint(r.TLS.PeerCertificates[0]) != dev.ClientKeyFP {
			return false
		}
	}
	return true
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
	s.mu.Lock()
	tlsMode, fellBack := s.tlsMode, s.tlsFellBack
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":               s.version,
		"listen":                s.listenAddr,
		"headscale_control_url": s.headscaleURL,
		"protocol":              protocol.Version,
		"tls_mode":              tlsMode,
		// A daemon serving its self-signed fallback because ACME failed. An
		// operator polling this can catch it before the 90-day cliff.
		"tls_fell_back": fellBack,
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.maxClients > 0 {
		s.mu.Lock()
		n := len(s.clients)
		s.mu.Unlock()
		if n >= s.maxClients {
			s.log.Warn("websocket rejected: client limit",
				slog.Int("clients", n),
				slog.Int("max", s.maxClients),
			)
			http.Error(w, "too many websocket clients", http.StatusServiceUnavailable)
			return
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Local/mesh clients; Flutter web may need origin flexibility later.
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionContextTakeover,
	})
	if err != nil {
		s.log.Warn("websocket accept failed", slog.String("err", err.Error()))
		return
	}
	conn.SetReadLimit(maxWSMessageBytes)

	c := &client{
		conn:   conn,
		out:    make(chan []byte, outboundQueueLen),
		closed: make(chan struct{}),
	}
	// Capture the presented client key before any message is read. With the
	// listener's tls.RequestClientCert, a presented certificate appears here
	// even though it is unverified — possession is what the handshake proves.
	// The auth and pair.claim handlers compare this fingerprint against the
	// store (ADR 0005).
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		c.clientKeyFP = certs.SPKIFingerprint(r.TLS.PeerCertificates[0])
	}
	s.mu.Lock()
	// Re-check under lock to avoid a thundering herd past the pre-accept check.
	if s.maxClients > 0 && len(s.clients) >= s.maxClients {
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusTryAgainLater, "too many clients")
		return
	}
	if !s.requireDeviceToken {
		// No-auth deployments (dev mode): treat every connection as authed so
		// event broadcasts reach it — otherwise session ops succeed but zero
		// events ever arrive.
		c.authed = true
		c.deviceID = "dev"
	}
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	go s.writeLoop(c)

	defer func() {
		c.shutdown()
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
	// after the user finishes typing an 8-char code. The absolute deadline on
	// the unauthenticated reads enforces the timeout by itself.
	authDeadline := time.Now().Add(30 * time.Second)
	for {
		var readCtx context.Context
		var cancel context.CancelFunc
		if s.requireDeviceToken && !c.authed {
			readCtx, cancel = context.WithDeadline(ctx, authDeadline)
		} else {
			readCtx, cancel = context.WithTimeout(ctx, s.readDeadline)
		}
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			// Idle timeout / peer close — log once at Info for ops diagnosis
			// (Phase 3.6). Skip if we already closed for slow_client/revoke.
			reason := "peer_closed"
			if errors.Is(err, context.DeadlineExceeded) {
				if s.requireDeviceToken && !c.authed {
					reason = "auth_timeout"
				} else {
					reason = "read_deadline"
				}
			}
			select {
			case <-c.closed:
			default:
				s.logDisconnect(c, reason)
			}
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
		// Session lifecycle ops spawn/kill agent processes and can take many
		// seconds (opencode = a full Bun engine per process). Run them off the
		// read goroutine: processed inline they starve ping replies, the phone
		// declares the link dead mid-create, and every other tap on this
		// connection queues behind them. Replies still correlate by request id
		// through the outbound queue, so ordering of the response frame is the
		// only thing that shifts. The manager's per-id create lock keeps
		// create/close races for one session serialized.
		return s.dispatchAsync(ctx, c, env, s.handleSessionCreate)
	case protocol.TypeSessionList:
		return s.handleSessionList(ctx, c, env)
	case protocol.TypeSessionClose:
		return s.dispatchAsync(ctx, c, env, s.handleSessionClose)
	case protocol.TypeSessionDelete:
		return s.dispatchAsync(ctx, c, env, s.handleSessionDelete)
	case protocol.TypeSessionPrompt:
		// Prompt (and slash builtins like /model, /reset) can block for seconds
		// on provider Start or HTTP submit. Same async treatment as create so
		// pings and cancel stay readable on the connection (Phase 1.3 / P1-1).
		return s.dispatchAsync(ctx, c, env, s.handleSessionPrompt)
	case protocol.TypeSessionCancel:
		// Cancel stays on the read loop: it must remain reachable while a
		// prompt or create is in flight on an async worker.
		return s.handleSessionCancel(ctx, c, env)
	case protocol.TypeSessionHistory:
		// History can marshal hundreds of events; keep it off the read loop.
		return s.dispatchAsync(ctx, c, env, s.handleSessionHistory)
	case protocol.TypeProvidersList:
		return s.handleProvidersList(ctx, c, env)
	case protocol.TypePermissionRespond:
		return s.handlePermissionRespond(ctx, c, env)
	default:
		t := env.Type
		if len(t) > 64 {
			// Never echo an arbitrary-length client string back at full size.
			t = t[:64] + "…"
		}
		return s.writeError(ctx, c, env.ID, "unknown_type", "unknown message type: "+t)
	}
}

// asyncHandler is a slow WS op that runs off the read loop. deviceID is a
// snapshot taken under Server.mu at dispatch time — handlers must use it
// instead of reading c.deviceID (which races setAuthed).
type asyncHandler func(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error

// dispatchAsync runs a slow handler on its own goroutine so the connection's
// read loop keeps servicing pings/cancels. It snapshots deviceID under s.mu
// (Phase 1.1), enforces maxAsyncPerClient (Phase 1.2), and logs handler errors
// like the inline path (handlers report failures via error frames themselves).
func (s *Server) dispatchAsync(
	ctx context.Context,
	c *client,
	env protocol.Envelope,
	h asyncHandler,
) error {
	s.mu.Lock()
	if c.asyncInFlight >= maxAsyncPerClient {
		s.mu.Unlock()
		return s.writeError(ctx, c, env.ID, "rate_limited",
			"too many in-flight operations; try again shortly")
	}
	deviceID := c.deviceID
	c.asyncInFlight++
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			c.asyncInFlight--
			s.mu.Unlock()
		}()
		// Detach from the HTTP/WebSocket request context so a brief network
		// blip mid-create does not cancel an already-started agent launch.
		// Handlers still bound their own work (provider start timeouts, etc.).
		opCtx := context.WithoutCancel(ctx)
		if err := h(opCtx, c, env, deviceID); err != nil {
			s.log.Debug("ws async op error",
				slog.String("type", env.Type),
				slog.String("err", err.Error()),
			)
		}
	}()
	return nil
}

// maxFailedAuths bounds token guesses per connection. Tokens are 256-bit so
// this is defense in depth, mirroring the pair.claim failure cap.
const maxFailedAuths = 10

func (s *Server) handleAuth(ctx context.Context, c *client, env protocol.Envelope) error {
	token := env.Token
	if token == "" {
		var p protocol.AuthPayload
		_ = protocol.DecodePayload(env, &p)
		token = p.Token
	}
	if c.failedAuths >= maxFailedAuths {
		_ = s.writeError(ctx, c, env.ID, "rate_limited", "too many failed auth attempts")
		_ = c.conn.Close(websocket.StatusPolicyViolation, "auth abuse")
		return nil
	}
	dev, err := s.authenticate(c, token)
	if err != nil {
		c.failedAuths++
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
	// Process-wide limit (new connections cannot reset failedClaims alone).
	if !s.allowPairClaim() {
		return s.writePairError(ctx, c, env.ID, "rate_limited", "pair claim rate limited")
	}

	var p protocol.PairClaimPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writePairError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if strings.TrimSpace(p.Code) == "" {
		c.failedClaims++
		return s.writePairError(ctx, c, env.ID, "invalid_code", "pair code required")
	}

	// Check the client-key requirement BEFORE taking the one-shot pair code:
	// a phone that failed to present a cert (misconfig, proxy) would otherwise
	// consume the operator's 5-minute code and then be told to retry — with a
	// code that no longer exists.
	if s.requireClientKey && c.clientKeyFP == "" {
		return s.writePairError(ctx, c, env.ID, "client_key_required", "a client key is required to pair")
	}

	// Take (not Claim): if device create fails we Restore so the code is not burned
	// (Phase 3.2).
	taken, err := s.pairCodes.Take(p.Code)
	if err != nil {
		c.failedClaims++
		code := "invalid_code"
		switch {
		case errors.Is(err, auth.ErrExpiredPairCode):
			code = "expired"
		case errors.Is(err, auth.ErrInvalidPairCode):
			code = "invalid_code"
		}
		return s.writePairError(ctx, c, env.ID, code, err.Error())
	}
	name := taken.Name
	if n := strings.TrimSpace(p.Name); n != "" {
		name = n
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}

	dev, token, err := s.store.CreateWithClientKey(name, c.clientKeyFP)
	if err != nil {
		if rerr := s.pairCodes.Restore(taken); rerr != nil {
			s.log.Warn("restore pair code after create_failed",
				slog.String("err", rerr.Error()),
			)
		}
		return s.writePairError(ctx, c, env.ID, "create_failed", err.Error())
	}
	if err := s.setAuthed(c, dev.ID); err != nil {
		// Device already exists on disk; do not restore the code (one-shot done).
		return s.writePairError(ctx, c, env.ID, "already_authed", err.Error())
	}
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
// errAlreadyAuthed is sticky-auth: a socket keeps one device identity for life
// (Phase 1.4 / D5).
var (
	errClientKeyRequired = errors.New("a client key is required to connect")
	errClientKeyMismatch = errors.New("client key does not match the enrolled key")
	errAlreadyAuthed     = errors.New("connection already authenticated as another device")
)

func (s *Server) authenticate(c *client, token string) (auth.Device, error) {
	if !s.requireDeviceToken {
		if err := s.setAuthed(c, "dev"); err != nil {
			return auth.Device{}, err
		}
		return auth.Device{ID: "dev", Name: "dev"}, nil
	}
	dev, err := s.store.Validate(token)
	if err != nil {
		return auth.Device{}, err
	}
	if err := s.verifyClientKey(c, dev); err != nil {
		return auth.Device{}, err
	}
	if err := s.setAuthed(c, dev.ID); err != nil {
		return auth.Device{}, err
	}
	s.log.Info("device authenticated", slog.String("device_id", dev.ID), slog.String("device_name", dev.Name))
	return dev, nil
}

// setAuthed marks the connection authenticated under s.mu. Sticky identity
// (Phase 1.4): a second successful auth as a *different* device is rejected so
// in-flight async ops cannot be re-attributed mid-flight.
func (s *Server) setAuthed(c *client, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.authed && c.deviceID != "" && c.deviceID != deviceID {
		return errAlreadyAuthed
	}
	c.authed = true
	c.deviceID = deviceID
	return nil
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

func (s *Server) handleSessionCreate(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionCreatePayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.Provider == "" {
		p.Provider = string(s.defaultProviderID())
	}
	// Functional fields are rejected over the cap (silent truncation would
	// start a session against the wrong path/model); the display name is
	// trimmed instead.
	switch {
	case len(p.CWD) > maxCWDLen:
		return s.writeError(ctx, c, env.ID, "bad_payload", "cwd too long")
	case len(p.Model) > maxModelLen:
		return s.writeError(ctx, c, env.ID, "bad_payload", "model too long")
	case len(p.AgentSessionID) > maxAgentSessionIDLen:
		return s.writeError(ctx, c, env.ID, "bad_payload", "agent_session_id too long")
	}
	if len(p.Name) > maxNameLen {
		p.Name = p.Name[:maxNameLen]
	}
	meta, err := s.sessions.Create(ctx, provider.ID(p.Provider), provider.StartOptions{
		Name:           p.Name,
		CWD:            p.CWD,
		Model:          p.Model,
		AgentSessionID: p.AgentSessionID,
		LocalSessionID: p.SessionID,
	}, deviceID)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_create_failed", err)
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
	// Read path: same goroutine as setAuthed for this connection after auth.
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	if err := s.sessions.RespondPermission(ctx, p.SessionID, p.PermissionID, p.OptionID, p.Cancelled, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "permission_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionList(ctx context.Context, c *client, env protocol.Envelope) error {
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	list := s.sessions.ListFor(deviceID)
	out, _ := protocol.NewEnvelope(protocol.TypeSessionListResult, env.ID, protocol.SessionListResultPayload{Sessions: list})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionClose(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Close(ctx, p.SessionID, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_close_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionDelete(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Delete(ctx, p.SessionID, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_delete_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionHistory(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	// Prefer SessionHistoryPayload (since_seq / limit); fall back to bare
	// session_id for older clients.
	var p protocol.SessionHistoryPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		var legacy protocol.SessionIDPayload
		if err := protocol.DecodePayload(env, &legacy); err == nil {
			p.SessionID = legacy.SessionID
		}
	}
	// History returns an empty (non-nil) slice for an unknown/never-active
	// session — replay is not an error. Forbidden owner is still an error.
	events, truncated, nextSeq, err := s.sessions.HistoryPageFor(p.SessionID, deviceID, p.SinceSeq, p.Limit)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_history_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionHistoryResult, env.ID, protocol.SessionHistoryResultPayload{
		SessionID:    p.SessionID,
		Events:       events,
		Truncated:    truncated,
		NextSinceSeq: nextSeq,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionPrompt(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionPromptPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.Prompt(ctx, p.SessionID, p.Text, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_prompt_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionCancel(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	if err := s.sessions.Cancel(ctx, p.SessionID, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_cancel_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

// writeSessionErr maps session package errors to stable protocol codes.
func (s *Server) writeSessionErr(ctx context.Context, c *client, id, fallbackCode string, err error) error {
	code := fallbackCode
	switch {
	case errors.Is(err, session.ErrForbidden):
		code = "session_forbidden"
	case errors.Is(err, session.ErrNotLive):
		code = "session_not_live"
	case errors.Is(err, session.ErrLimitReached):
		code = "session_limit"
	case errors.Is(err, session.ErrShuttingDown):
		code = "shutting_down"
	}
	msg := err.Error()
	if len(msg) > 300 {
		// Provider/store errors can drag along multi-line detail (paths, JSON);
		// the phone needs the headline, the journal has the rest.
		msg = msg[:300] + "…"
	}
	return s.writeError(ctx, c, id, code, msg)
}

// allowPairClaim returns false when the process-wide pair.claim budget is spent.
func (s *Server) allowPairClaim() bool {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	now := time.Now()
	if s.pairWindowStart.IsZero() || now.Sub(s.pairWindowStart) >= time.Minute {
		s.pairWindowStart = now
		s.pairWindowCount = 0
	}
	if s.pairWindowCount >= maxPairClaimsPerMinute {
		return false
	}
	s.pairWindowCount++
	return true
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
	case errors.Is(err, errAlreadyAuthed):
		code = "already_authed"
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

// writeJSON enqueues a frame for the client's writeLoop. It never blocks: a
// full queue means the peer has stopped reading, and the connection is dropped
// (R5=B) so no handler or event pump is ever wedged on a dead socket.
func (s *Server) writeJSON(_ context.Context, c *client, env protocol.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	select {
	case c.out <- b:
		return nil
	case <-c.closed:
		return errClientGone
	default:
	}
	s.logDisconnect(c, "slow_client")
	c.shutdown()
	// The peer isn't reading; a graceful close handshake would just block.
	_ = c.conn.CloseNow()
	return errClientGone
}

var errClientGone = errors.New("ws client closed or too slow")

// writeLoop drains c.out onto the socket, bounding each frame write. On any
// write failure the connection is closed — the holder of a dead socket must
// never be the sessions' event path.
func (s *Server) writeLoop(c *client) {
	for {
		select {
		case <-c.closed:
			return
		case b := <-c.out:
			ctx, cancel := context.WithTimeout(context.Background(), writeDeadline)
			err := c.conn.Write(ctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				s.mu.Lock()
				deviceID := c.deviceID
				s.mu.Unlock()
				attrs := []any{
					slog.String("reason", "write_failed"),
					slog.String("err", err.Error()),
				}
				if deviceID != "" {
					attrs = append(attrs, slog.String("device_id", deviceID))
				}
				s.log.Info("ws client disconnected", attrs...)
				c.shutdown()
				_ = c.conn.CloseNow()
				return
			}
		}
	}
}
