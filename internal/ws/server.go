// Package ws implements the mcremote.v1 WebSocket control plane.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Server hosts WebSocket clients and HTTP health endpoints.
type Server struct {
	store     *auth.Store
	pairCodes *auth.PairCodeStore
	sessions  *session.Manager
	registry  *provider.Registry
	prewarm   *provider.Controller
	// deviceFlows tracks in-progress provider device-auth flows (MADR 0074
	// Strategy A): expiry, cancellation, and per-device scoping.
	deviceFlows        *providerauth.Registry
	requireDeviceToken bool
	requireClientKey   bool
	// allowedOrigins is passed to the WS upgrade as OriginPatterns: an opt-in
	// allowlist of browser Origin hosts. Empty means native + same-origin only.
	allowedOrigins []string
	version        string
	listenAddr     string
	headscaleURL   string
	displayName    string
	log            *slog.Logger
	maxClients     int
	readDeadline   time.Duration
	resume         *resumeStore

	// TLS status, set once after the certificate is resolved (SetTLSStatus).
	// Guarded by mu because the listener goroutine sets it while requests read.
	tlsMode     string
	tlsFellBack bool

	// providerAuthTransactions gates the transactional capability bit.
	providerAuthTransactions bool

	// lifeCtx is cancelled by CloseClients / process shutdown so async work
	// and derived op contexts stop (MADR 0056 H-2).
	lifeCtx    context.Context
	lifeCancel context.CancelFunc

	// idem remembers mutating request results for reconnect/retry (H-2b).
	idem *idempotencyLedger

	// Process-wide pair.claim rate limit so new connections cannot reset
	// per-connection failedClaims cheaply (B6).
	pairMu          sync.Mutex
	pairWindowStart time.Time
	pairWindowCount int

	mu      sync.Mutex
	clients map[*client]struct{}
	// clientSeq assigns a monotonic admission order to each client so the
	// oldest still-unauthenticated connection can be evicted under slot pressure.
	clientSeq uint64

	// receiptMu guards receiptWaiters: one outstanding permission.receipt_request
	// per permission_id, correlated by id rather than by connection so a device
	// that reconnects mid-round-trip is still found (MADR 0077 P7).
	receiptMu      sync.Mutex
	receiptWaiters map[string]receiptWaiter
}

// receiptWaiter is one outstanding permission.receipt_request. deviceID pins
// which device's reply is accepted: without it, any OTHER authed device could
// race-deliver a garbage JWS for a permission id it observed, consume the
// waiter, and downgrade the real device's legitimate receipt to an
// invalid_signature marker — an audit-trail corruption the signature check
// alone cannot prevent, since it runs only on whatever reply won the channel.
type receiptWaiter struct {
	deviceID string
	ch       chan string
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
	// lifeCtx is cancelled when this connection shuts down or the server life
	// context ends (MADR 0056 H-2). Async handlers derive deadlines from it.
	lifeCtx    context.Context
	lifeCancel context.CancelFunc
	// seq is this client's admission order (Server.clientSeq), used to pick the
	// oldest unauthenticated connection to evict when slots are exhausted.
	seq uint64
	// failedClaims counts unsuccessful pair.claim attempts on this connection.
	failedClaims int
	// failedAuths counts unsuccessful auth attempts on this connection.
	failedAuths int
	// clientKeyFP is the SPKI fingerprint of the client certificate presented at
	// TLS handshake time (ADR 0005), captured once at upgrade. Empty means no
	// client certificate was presented (or TLS is not terminated here).
	clientKeyFP string
	// clientKeySPKI is the raw DER SubjectPublicKeyInfo clientKeyFP was hashed
	// from (MADR 0077 D9), captured alongside it. Needed to verify a signed
	// receipt later; the fingerprint alone is one-way.
	clientKeySPKI []byte
	// negotiated is the protocol version picked at auth/pair.claim
	// (MADR 0068 D1). Zero means "not negotiated yet" and is treated as V1.
	// Written on the read goroutine; read from writer paths under s.mu.
	negotiated int
	// codexSurfaceVersion is the client-advertised additive Codex surface.
	// It is meaningful only for negotiated protocol v2 connections.
	codexSurfaceVersion int
	// tlsResumed records whether this connection's TLS handshake resumed a
	// prior session (surfaced in the v2 capability block, 0068 Q3).
	tlsResumed bool
	// lastPong / lastData are unixnano marks feeding the v2 deadline
	// watchdog (0068 P1): the horizon is max(lastData, lastPong) +
	// readDeadline. Written by pingLoop / the read loop; zero = never.
	lastPong atomic.Int64
	lastData atomic.Int64
	// deadlineReaped marks a close initiated by the watchdog so the read
	// loop logs read_deadline rather than peer_closed.
	deadlineReaped atomic.Bool
	// pingerOnce guards the per-connection pinger+watchdog goroutines: auth
	// and pair.claim can both negotiate v2 on one connection's lifetime.
	pingerOnce sync.Once
}

// maxAsyncPerClient bounds concurrent dispatchAsync work per WebSocket (D3).
// The gate covers every slow op — create/close/prompt plus the catalog reads a
// screen fans out on open — so it has to leave room for ordinary use: sized at
// 2 it was one wedged handler away from rate-limiting the whole connection.
const maxAsyncPerClient = 8

// shutdown signals the writer loop to exit; safe to call more than once.
func (c *client) shutdown() {
	c.closeOnce.Do(func() {
		if c.lifeCancel != nil {
			c.lifeCancel()
		}
		close(c.closed)
	})
}

// Options configure the WS server.
type Options struct {
	Store              *auth.Store
	PairCodes          *auth.PairCodeStore
	Sessions           *session.Manager
	Registry           *provider.Registry
	Prewarm            *provider.Controller
	RequireDeviceToken bool
	RequireClientKey   bool
	// AllowedOrigins is an opt-in allowlist of browser Origin host patterns for
	// the WS upgrade. Empty (default) accepts native/same-origin only.
	AllowedOrigins []string
	Version        string
	ListenAddr     string
	HeadscaleURL   string
	// DisplayName is the operator-configured friendly host name reported
	// in auth_ok and pair_ok (MADR 0102). Empty = phones show the dialled
	// address.
	DisplayName string
	Log         *slog.Logger
	// MaxClients caps simultaneous WebSocket connections (0 = unlimited).
	MaxClients int
	// ReadDeadline determines how long the server will wait for a message from an
	// authenticated client before forcefully closing the socket to prevent leaks.
	ReadDeadline time.Duration
	// ProviderAuthTransactions advertises that provider logins run inside a
	// credential transaction (MADR 0074 D21/D27). Set by the daemon only after
	// coordinators, recovery, watchers, and shutdown hooks are installed.
	ProviderAuthTransactions bool

	// ResumeWindow bounds v2 resume-token validity (0 → 120s;
	// MADR 0068 D4).
	ResumeWindow time.Duration
}

// Compile-time check: *Server implements session.ReceiptTransport (MADR
// 0077 P7) — daemon.go wires it into Manager.SetReceiptSupport.
var _ session.ReceiptTransport = (*Server)(nil)

// New creates a Server.
func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	if opts.ReadDeadline == 0 {
		opts.ReadDeadline = 120 * time.Second
	}
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	return &Server{
		store:                    opts.Store,
		pairCodes:                opts.PairCodes,
		sessions:                 opts.Sessions,
		registry:                 opts.Registry,
		prewarm:                  opts.Prewarm,
		deviceFlows:              providerauth.NewRegistry(),
		requireDeviceToken:       opts.RequireDeviceToken,
		requireClientKey:         opts.RequireClientKey,
		allowedOrigins:           opts.AllowedOrigins,
		version:                  opts.Version,
		listenAddr:               opts.ListenAddr,
		headscaleURL:             opts.HeadscaleURL,
		displayName:              opts.DisplayName,
		log:                      log.With(slog.String("component", "ws")),
		maxClients:               opts.MaxClients,
		readDeadline:             opts.ReadDeadline,
		resume:                   newResumeStore(opts.ResumeWindow),
		providerAuthTransactions: opts.ProviderAuthTransactions,
		clients:                  make(map[*client]struct{}),
		lifeCtx:                  lifeCtx,
		lifeCancel:               lifeCancel,
		idem:                     newIdempotencyLedger(),
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
// 20 s (was 5 s; MADR 0072 D3): history replay and dense tool-stream bursts
// over a mesh or relay hop routinely take longer than 5 s on a phone
// mid-rejoin; the short deadline produced write_failed / broken_pipe storms
// that looked like freezes rather than a single slow peer.
const writeDeadline = 20 * time.Second

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
	maxThinkingLevelLen  = 64
	maxAgentLen          = 128
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

	if len(targets) == 0 {
		return
	}
	// Marshal the envelope once and fan the same buffer out to every recipient,
	// instead of re-encoding this identical event per client.
	b, err := json.Marshal(env)
	if err != nil {
		s.log.Error("broadcast: encoding event failed; dropping",
			slog.String("type", string(ev.Type)),
			slog.String("err", err.Error()),
		)
		return
	}
	for _, c := range targets {
		_ = s.writeBytes(c, b)
	}
}

// DisconnectDevice closes all connections for a device id (after revoke).
// Returns how many client sockets were closed.
func (s *Server) DisconnectDevice(deviceID string) int {
	return s.DisconnectDevices([]string{deviceID})
}

// CloseClients closes every connected client and returns how many it closed.
// http.Server.Shutdown does not touch hijacked connections (a WebSocket upgrade
// hijacks the socket), so without an explicit sweep a graceful shutdown or
// in-process restart leaks every per-client goroutine and socket. CloseNow (not
// a graceful handshake) so one unresponsive peer cannot stall shutdown.
func (s *Server) CloseClients() int {
	// Cancel and drain owned device flows before the process can exit and
	// destroy the in-memory ownership that is the only record of how to clean
	// them up (MADR 0074 F4/D27). Ordering matters: flows first, then sockets.
	if s.deviceFlows != nil {
		s.deviceFlows.CancelAll()
		if err := s.deviceFlows.WaitAll(context.Background(), providerauth.DrainTimeout); err != nil {
			// Report retained ownership; never force, and never delete
			// transaction state on the way out.
			s.log.Warn("device flows still owned at shutdown", slog.String("err", err.Error()))
		}
	}
	if s.lifeCancel != nil {
		s.lifeCancel()
	}
	s.mu.Lock()
	victims := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		victims = append(victims, c)
	}
	s.clients = make(map[*client]struct{})
	s.mu.Unlock()
	for _, c := range victims {
		c.shutdown()
		_ = c.conn.CloseNow()
	}
	return len(victims)
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
		// Full offer for v2-aware clients (MADR 0068 D1); "protocol" above
		// stays for older readers.
		"protocols": protocol.SupportedVersions,
		"tls_mode":  tlsMode,
		// A daemon serving its self-signed fallback because ACME failed. An
		// operator polling this can catch it before the 90-day cliff.
		"tls_fell_back": fellBack,
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Verify the browser Origin against the same-origin default plus any
	// configured allowlist. Native clients (Flutter, CLI) send no Origin and are
	// always accepted; a malicious cross-origin web page is rejected — otherwise,
	// with require_device_token=false, any site the operator visits could drive
	// agent sessions (RCE). Flutter-web deployments opt in via AllowedOrigins.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  s.allowedOrigins,
		CompressionMode: websocket.CompressionContextTakeover,
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
	// Per-connection lifecycle: cancelled on disconnect so async ops stop.
	if s.lifeCtx != nil {
		c.lifeCtx, c.lifeCancel = context.WithCancel(s.lifeCtx)
	} else {
		c.lifeCtx, c.lifeCancel = context.WithCancel(context.Background())
	}
	// Capture the presented client key before any message is read. With the
	// listener's tls.RequestClientCert, a presented certificate appears here
	// even though it is unverified — possession is what the handshake proves.
	// The auth and pair.claim handlers compare this fingerprint against the
	// store (ADR 0005).
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		c.clientKeyFP = certs.SPKIFingerprint(r.TLS.PeerCertificates[0])
		c.clientKeySPKI = r.TLS.PeerCertificates[0].RawSubjectPublicKeyInfo
	}
	if r.TLS != nil {
		c.tlsResumed = r.TLS.DidResume
	}
	s.mu.Lock()
	// At capacity, evict the oldest still-UNauthenticated connection to make
	// room: idle pre-auth sockets (which have up to 30s to authenticate) must
	// never be able to shut out a fresh connection — otherwise a tailnet peer
	// could hold every slot for 30s at a time (slot-exhaustion DoS). Only when
	// every slot is held by an AUTHENTICATED client is this genuine capacity,
	// and the new connection is refused.
	var evicted *client
	if s.maxClients > 0 && len(s.clients) >= s.maxClients {
		evicted = s.oldestUnauthedLocked()
		if evicted == nil {
			retry := s.capacityRetryAfterLocked(time.Now())
			s.mu.Unlock()
			// The refusal happens before any envelope exchange, so the hint
			// rides the close reason (0068 P6); the client parses it as a
			// backoff floor.
			_ = conn.Close(websocket.StatusTryAgainLater,
				fmt.Sprintf("too many clients; retry_after_ms=%d", retry.Milliseconds()))
			return
		}
		delete(s.clients, evicted)
		evicted.shutdown()
	}
	s.clientSeq++
	c.seq = s.clientSeq
	if !s.requireDeviceToken {
		// No-auth deployments (dev mode): treat every connection as authed so
		// event broadcasts reach it — otherwise session ops succeed but zero
		// events ever arrive.
		c.authed = true
		c.deviceID = "dev"
	}
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	if evicted != nil {
		// Force the evicted peer's blocked Read to return so its goroutine and
		// socket are released now, not at its 30s auth deadline.
		_ = evicted.conn.CloseNow()
		s.logDisconnect(evicted, "preauth_evicted")
	}

	go s.writeLoop(c)

	defer func() {
		c.shutdown()
		s.mu.Lock()
		delete(s.clients, c)
		last := c.authed && c.deviceID != "" && !s.deviceStillConnectedLocked(c.deviceID)
		dev := c.deviceID
		s.mu.Unlock()
		// A transient disconnect must not kill a login the user is still
		// completing on another device. Detach and arm the negotiated resume
		// window; expiry cancels, a resume reattaches (MADR 0074 D28).
		if last && s.deviceFlows != nil {
			s.deviceFlows.DetachDevice(dev, s.resumeWindow())
		}
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
		switch {
		case s.requireDeviceToken && !c.authed:
			readCtx, cancel = context.WithDeadline(ctx, authDeadline)
		case c.negotiated >= protocol.V2:
			// v2: the deadline watchdog owns the reap (0068 P1). A ctx
			// deadline here would close the connection the moment it
			// expired — coder/websocket closes on read-ctx cancellation —
			// even if a transport pong had just extended the horizon.
			readCtx, cancel = context.WithCancel(ctx)
		default:
			readCtx, cancel = context.WithTimeout(ctx, s.readDeadline)
		}
		msgType, data, err := conn.Read(readCtx)
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
			if c.deadlineReaped.Load() {
				reason = "read_deadline"
			}
			select {
			case <-c.closed:
			default:
				s.logDisconnect(c, reason)
			}
			return
		}
		c.lastData.Store(time.Now().UnixNano())
		// Application contract is JSON text frames (MADR 0056 M-2).
		if msgType != websocket.MessageText {
			_ = s.writeError(ctx, c, "", protocol.ErrBadPayload, "control plane requires JSON text frames")
			continue
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
	// Strictness preserved (MADR 0056 M-2), widened by negotiation
	// (MADR 0068 D1): before auth negotiates anything the connection speaks
	// V1 exactly — omit or non-1 is rejected byte-for-byte as before. After
	// negotiating V2, envelopes are accepted for any version in [V1, V2]:
	// the client stamps the negotiated version, while shared fan-out frames
	// from the server may still carry V1 (see protocol-v2.md).
	maxV := c.negotiated
	if maxV == 0 {
		maxV = protocol.V1
	}
	if env.V < protocol.V1 || env.V > maxV {
		return s.writeError(ctx, c, env.ID, "bad_version", fmt.Sprintf("unsupported protocol version %d", env.V))
	}

	if s.requireDeviceToken && !c.authed &&
		env.Type != protocol.TypeAuth && env.Type != protocol.TypePairClaim {
		return s.writeError(ctx, c, env.ID, "unauthorized", "authenticate first")
	}
	if handled, err := s.handleCodexPhoneOperation(ctx, c, env); handled {
		return err
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
		// seconds (a grok ACP subprocess, or an opencode engine cold boot when
		// prewarm is off / the engine died). Run them off the
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
	case protocol.TypeSessionRelease:
		return s.dispatchAsync(ctx, c, env, s.handleSessionRelease)
	case protocol.TypeSessionClaim:
		return s.dispatchAsync(ctx, c, env, s.handleSessionClaim)
	case protocol.TypeSessionPrompt:
		// Prompt (and slash builtins like /model, /reset) can block for seconds
		// on provider Start or HTTP submit. Same async treatment as create so
		// pings and cancel stay readable on the connection (Phase 1.3 / P1-1).
		return s.dispatchAsync(ctx, c, env, s.handleSessionPrompt)
	case protocol.TypeSessionSetMode:
		return s.handleSessionSetMode(ctx, c, env)
	case protocol.TypeSessionSetConfig:
		return s.handleSessionSetConfig(ctx, c, env)
	case protocol.TypeSessionCancel:
		// Cancel stays on the read loop: it must remain reachable while a
		// prompt or create is in flight on an async worker.
		return s.handleSessionCancel(ctx, c, env)
	case protocol.TypeSessionHistory:
		// History can marshal hundreds of events; keep it off the read loop.
		return s.dispatchAsync(ctx, c, env, s.handleSessionHistory)
	case protocol.TypeSessionPendingAsks:
		// Same read path as handlePermissionRespond: c.deviceID is guarded by
		// s.mu everywhere else in this file, so take it here too rather than
		// leave one handler outside the discipline (MADR 0046 I-2).
		s.mu.Lock()
		deviceID := c.deviceID
		s.mu.Unlock()
		return s.handleSessionPendingAsks(ctx, c, env, deviceID)
	case protocol.TypeProvidersList:
		// Async since MADR 0074: for a capability-advertising client this now
		// probes each provider's credential state, and kilo's probe talks to
		// its engine over HTTP. Same reason models.list is async.
		return s.dispatchAsync(ctx, c, env, s.handleProvidersList)
	case protocol.TypeProvidersSetPrewarm:
		return s.dispatchAsync(ctx, c, env, s.handleProvidersSetPrewarm)
	case protocol.TypeProviderAuthCatalog:
		// May boot an engine to read its vendor list, like models.list.
		return s.dispatchAsync(ctx, c, env, s.handleAuthCatalog)
	case protocol.TypeProviderSetCredential:
		// Writes a file or spawns a CLI; always off the read loop.
		return s.dispatchAsync(ctx, c, env, s.handleSetCredential)
	case protocol.TypeProviderClearCredential:
		return s.dispatchAsync(ctx, c, env, s.handleClearCredential)
	case protocol.TypeProviderSetActiveUpstrm:
		return s.dispatchAsync(ctx, c, env, s.handleSetActiveUpstream)
	case protocol.TypeProviderStartAuth:
		return s.dispatchAsync(ctx, c, env, s.handleStartAuth)
	case protocol.TypeOAuthCancel:
		s.mu.Lock()
		cancelDevice := c.deviceID
		s.mu.Unlock()
		return s.handleOAuthCancel(ctx, c, env, cancelDevice)
	case protocol.TypeModelsList:
		// May boot a shared engine (OpenCode HTTP) to fetch a live catalog.
		return s.dispatchAsync(ctx, c, env, s.handleModelsList)
	case protocol.TypeAgentsList:
		// May boot a shared engine (OpenCode HTTP) for GET /agent.
		return s.dispatchAsync(ctx, c, env, s.handleAgentsList)
	case protocol.TypeAgentSessionsList:
		// May boot an ACP engine and query provider-native durable sessions.
		return s.dispatchAsync(ctx, c, env, s.handleAgentSessionsList)
	// Workspace reads talk to the engine over HTTP, so they take the async
	// path: a slow listing must not stall the connection's read loop.
	case protocol.TypeWorkspaceList:
		return s.dispatchAsync(ctx, c, env, s.handleWorkspaceList)
	case protocol.TypeWorkspaceRead:
		return s.dispatchAsync(ctx, c, env, s.handleWorkspaceRead)
	case protocol.TypeWorkspaceSearch:
		return s.dispatchAsync(ctx, c, env, s.handleWorkspaceSearch)
	case protocol.TypeProjectsList:
		// May boot a shared engine (OpenCode HTTP) for GET /project.
		return s.dispatchAsync(ctx, c, env, s.handleProjectsList)
	case protocol.TypeCommandsList:
		return s.dispatchAsync(ctx, c, env, s.handleCommandsList)
	case protocol.TypeSessionFork:
		return s.dispatchAsync(ctx, c, env, s.handleSessionFork)
	case protocol.TypeSessionRevert:
		return s.dispatchAsync(ctx, c, env, s.handleSessionRevert)
	case protocol.TypeSessionUnrevert:
		return s.dispatchAsync(ctx, c, env, s.handleSessionUnrevert)
	case protocol.TypeSessionDiff:
		return s.dispatchAsync(ctx, c, env, s.handleSessionDiff)
	case protocol.TypeSessionRename:
		return s.dispatchAsync(ctx, c, env, s.handleSessionRename)
	case protocol.TypeSessionDiagnostics:
		return s.dispatchAsync(ctx, c, env, s.handleSessionDiagnostics)
	case protocol.TypePermissionRespond:
		return s.handlePermissionRespond(ctx, c, env)
	case protocol.TypePermissionReceipt:
		return s.handlePermissionReceipt(ctx, c, env)
	case protocol.TypeReceiptsList:
		return s.handleReceiptsList(ctx, c, env)
	case protocol.TypeDevicesList:
		return s.handleDevicesList(ctx, c, env)
	case protocol.TypeQuestionRespond:
		return s.handleQuestionRespond(ctx, c, env)
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
		deviceID := c.deviceID
		s.mu.Unlock()
		// Log it: a handler that never returns turns this into a permanent
		// "the host is rate-limiting" for every op on the connection, and
		// silence here made that indistinguishable from a phone-side fault.
		s.log.Warn("async slots exhausted",
			slog.String("type", env.Type),
			slog.String("device_id", deviceID),
			slog.Int("limit", maxAsyncPerClient),
		)
		return s.writeError(ctx, c, env.ID, "rate_limited",
			"too many in-flight operations; try again shortly")
	}
	deviceID := c.deviceID
	c.asyncInFlight++
	s.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("ws async handler panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		defer func() {
			s.mu.Lock()
			c.asyncInFlight--
			s.mu.Unlock()
		}()
		// Bound work to connection lifecycle + per-op deadline (MADR 0056 H-2).
		// Detach from the read-loop deadline only: c.lifeCtx still cancels on
		// disconnect and daemon shutdown.
		base := c.lifeCtx
		if base == nil {
			base = context.Background()
		}
		opCtx, cancel := context.WithTimeout(base, asyncOpTimeout(env.Type))
		defer cancel()

		// Idempotent replay for mutating ops with a client request id.
		//
		// Every path here must leave the client with a frame: a retry that
		// gets silence is indistinguishable from a dead link and burns the
		// client's whole timeout again (MADR 0095 D6/F5).
		if s.idem != nil && deviceID != "" && env.ID != "" && isMutatingAsync(env.Type) {
			frame, wait, action := s.idem.begin(deviceID, env.ID)
			switch action {
			case idemReplay:
				if len(frame) > 0 {
					_ = s.writeBytes(c, frame)
					return
				}
				// The original succeeded but captured no frame. Guessing
				// `ok` would invent a result the handler never produced.
				_ = s.writeError(opCtx, c, env.ID, protocol.ErrRetryNoResult,
					"the original request completed but its response is unavailable")
				return
			case idemWait:
				if wait != nil {
					if f := wait(opCtx); len(f) > 0 {
						_ = s.writeBytes(c, f)
						return
					}
				}
				// The wait yielded nothing: either the original failed
				// (fail() drops the key so a retry may execute) or opCtx
				// died. Re-begin once — never loop — and fall through to
				// the handler when the ledger hands us the work.
				if _, _, again := s.idem.begin(deviceID, env.ID); again != idemExecute {
					_ = s.writeError(opCtx, c, env.ID, protocol.ErrRetryNoResult,
						"the original request completed but its response is unavailable")
					return
				}
			}
		}

		if err := h(opCtx, c, env, deviceID); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(opCtx.Err(), context.DeadlineExceeded) {
				_ = s.writeError(context.Background(), c, env.ID, protocol.ErrDeadlineExceeded,
					"operation deadline exceeded")
			}
			s.log.Debug("ws async op error",
				slog.String("type", env.Type),
				slog.String("err", err.Error()),
			)
			if s.idem != nil && deviceID != "" && env.ID != "" && isMutatingAsync(env.Type) {
				s.idem.fail(deviceID, env.ID)
			}
			return
		}
		// Handlers normally complete the ledger via writeJSON capture. This is
		// a fallback when the success path wrote nothing (should be rare).
		if s.idem != nil && deviceID != "" && env.ID != "" && isMutatingAsync(env.Type) {
			s.idem.complete(deviceID, env.ID, nil)
		}
	}()
	return nil
}

// asyncOpTimeout is the server-side deadline for an async-dispatched op.
//
// These values are mirrored in internal/protocol/op_timeouts.json, which the
// phone's request timeouts are checked against: the daemon's allowance is
// authoritative for how long an operation may take, and the phone's timeout
// must strictly exceed it so the authoritative failure is always the
// daemon's own error frame (MADR 0095 D7). Pinned by
// TestAsyncOpTimeoutMatchesSharedTable — update the JSON with any change
// here.
func asyncOpTimeout(typ string) time.Duration {
	switch typ {
	case protocol.TypeSessionCreate:
		return 120 * time.Second
	case protocol.TypeSessionPrompt:
		return 60 * time.Second
	case protocol.TypeSessionDelete, protocol.TypeSessionClose,
		protocol.TypeSessionFork, protocol.TypeCodexExecutionWrite:
		// Lifecycle ops tear down or fork a provider subprocess. The
		// opencode/kilo purge alone budgets 15s for the engine-side delete
		// after local teardown (httpagent session.Purge), and at 30s the
		// phone's own timeout expired in the same instant as this one —
		// turning a would-be error frame into a client timeout plus an
		// idempotent retry (MADR 0095 F9).
		return 60 * time.Second
	case protocol.TypeSessionHistory:
		return 30 * time.Second
	case protocol.TypeModelsList, protocol.TypeAgentsList, protocol.TypeAgentSessionsList:
		return 60 * time.Second
	default:
		return 30 * time.Second
	}
}

func isMutatingAsync(typ string) bool {
	switch typ {
	case protocol.TypeSessionCreate, protocol.TypeSessionPrompt,
		protocol.TypeSessionClose, protocol.TypeSessionDelete,
		protocol.TypeSessionRename, protocol.TypeSessionFork,
		protocol.TypeSessionSetCollaboration:
		return true
	default:
		return false
	}
}

// maxFailedAuths bounds token guesses per connection. Tokens are 256-bit so
// this is defense in depth, mirroring the pair.claim failure cap.
const maxFailedAuths = 10

func (s *Server) handleAuth(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.AuthPayload
	_ = protocol.DecodePayload(env, &p)
	token := env.Token
	if token == "" {
		token = p.Token
	}
	// Version negotiation (MADR 0068 D1) is settled before auth is
	// attempted: a client offering only versions we do not speak gets
	// bad_version — not a failed-auth strike — and can retry with v1.
	negotiated := protocol.NegotiateVersion(p.Protocols)
	if negotiated == 0 {
		return s.writeError(ctx, c, env.ID, protocol.ErrBadVersion,
			"no mutually supported protocol version")
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
	s.mu.Lock()
	c.negotiated = negotiated
	if negotiated >= protocol.V2 && p.CodexSurfaceVersion >= 1 {
		c.codexSurfaceVersion = 1
	}
	s.mu.Unlock()
	home, _ := os.UserHomeDir()
	payload := protocol.AuthOKPayload{
		DeviceID:    dev.ID,
		DeviceName:  dev.Name,
		HomeDir:     home,
		DisplayName: s.displayName,
	}
	if negotiated >= protocol.V2 {
		payload.Protocol = negotiated
		payload.Caps = s.capsFor(c)
		// Resume fast path (MADR 0068 D4): validate the previous token
		// BEFORE issuing — issue rotates it. Failure is not an auth
		// failure; the client falls back to the full reconcile.
		if p.Resume != nil {
			if s.resume.validate(dev.ID, p.Resume.Token) {
				// A successful same-device resume reattaches any device flow
				// detached by the disconnect, disarming its expiry timer.
				if s.deviceFlows != nil {
					s.deviceFlows.ResumeDevice(dev.ID)
				}
				payload.Resumed = s.resumedFor(dev.ID, p.Resume.Sessions)
			} else {
				payload.ResumeFailed = true
			}
		}
		requested := time.Duration(p.ResumeWindowMS) * time.Millisecond
		token, window, mintOK := s.resume.issue(dev.ID, requested)
		if mintOK {
			payload.ResumeToken = token
			payload.Caps.Resume = &protocol.ResumeCaps{WindowMS: window.Milliseconds()}
		} else {
			// 0070 F3: omit empty token / caps.resume rather than advertise
			// a broken resume surface; auth still succeeds.
			s.log.Warn("resume token mint failed; resume disabled for this auth",
				slog.String("device_id", dev.ID))
		}
		// v2 grants ws_ping_resets_deadline — start honouring it (0068 P1).
		s.startV2Liveness(c)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeAuthOK, env.ID, payload)
	return s.writeJSON(ctx, c, out)
}

// resumedFor builds the per-session retained-seq windows for a successful
// resume (MADR 0068 D4). Only sessions the daemon knows about and this
// device may access appear; the client must reconcile anything absent the
// ordinary way. Ownership mirrors the event fan-out rule: empty owner
// (legacy) is visible to all devices.
func (s *Server) resumedFor(deviceID string, sessions map[string]uint64) *protocol.ResumedPayload {
	out := &protocol.ResumedPayload{Sessions: map[string]protocol.SeqBoundsPayload{}}
	if s.sessions == nil {
		return out
	}
	for id := range sessions {
		owner, known := s.sessions.OwnerOf(id)
		if !known || (owner != "" && owner != deviceID) {
			continue
		}
		first, latest := s.sessions.SeqBounds(id)
		if latest == 0 {
			continue
		}
		out.Sessions[id] = protocol.SeqBoundsPayload{FirstSeq: first, LatestSeq: latest}
	}
	return out
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
	// Version negotiation (MADR 0068 D1): reject an impossible offer before
	// the one-shot pair code is consumed — the client can retry with v1 and
	// the operator's code survives.
	if protocol.NegotiateVersion(p.Protocols) == 0 {
		return s.writePairError(ctx, c, env.ID, protocol.ErrBadVersion,
			"no mutually supported protocol version")
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
		// Stable machine code + fixed message only; the raw error adds nothing
		// beyond the sentinel and never reaches this unauthenticated peer.
		code, msg := "invalid_code", "invalid pair code"
		if errors.Is(err, auth.ErrExpiredPairCode) {
			code, msg = "expired", "pair code expired"
		}
		return s.writePairError(ctx, c, env.ID, code, msg)
	}
	name := taken.Name
	if n := strings.TrimSpace(p.Name); n != "" {
		name = n
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}

	dev, token, err := s.store.CreateWithClientKey(name, c.clientKeyFP, c.clientKeySPKI)
	if err != nil {
		if rerr := s.pairCodes.Restore(taken); rerr != nil {
			s.log.Warn("restore pair code after create_failed",
				slog.String("err", rerr.Error()),
			)
		}
		// A raw store error embeds the data-dir path and daemon username; never
		// send it to this unauthenticated peer — log the detail, return generic.
		s.log.Warn("device create failed during pairing", slog.String("err", err.Error()))
		return s.writePairError(ctx, c, env.ID, "create_failed", "could not complete pairing")
	}
	// MADR 0072 D4: one active device per enrolled client key. Re-pair with
	// the same SPKI must not leave twin rows (24 s22+ devices in forensics).
	if c.clientKeyFP != "" {
		if twins, rerr := s.store.RevokeByClientKeyFP(c.clientKeyFP, dev.ID); rerr != nil {
			s.log.Warn("revoke prior devices for client key failed",
				slog.String("err", rerr.Error()),
			)
		} else if len(twins) > 0 {
			ids := make([]string, 0, len(twins))
			for _, d := range twins {
				ids = append(ids, d.ID)
			}
			n := s.DisconnectDevices(ids)
			s.log.Info("revoked prior devices for same client key",
				slog.Int("count", len(twins)),
				slog.Int("kicked", n),
				slog.String("keep_device_id", dev.ID),
			)
		}
	}
	if err := s.setAuthed(c, dev.ID); err != nil {
		// Device already exists on disk; do not restore the code (one-shot done).
		s.log.Warn("set authed failed after pairing", slog.String("err", err.Error()))
		return s.writePairError(ctx, c, env.ID, "already_authed", "device already registered")
	}
	s.log.Info("device paired via short code",
		slog.String("device_id", dev.ID),
		slog.String("device_name", dev.Name),
	)
	// Same negotiation as auth (MADR 0068 D1); a claim's offer with no
	// mutual version was rejected before the one-shot code was taken.
	pairOK := protocol.PairOKPayload{
		Token:       token,
		DeviceID:    dev.ID,
		DeviceName:  dev.Name,
		DisplayName: s.displayName,
	}
	if negotiated := protocol.NegotiateVersion(p.Protocols); negotiated >= protocol.V2 {
		s.mu.Lock()
		c.negotiated = negotiated
		if p.CodexSurfaceVersion >= 1 {
			c.codexSurfaceVersion = 1
		}
		s.mu.Unlock()
		pairOK.Protocol = negotiated
		pairOK.Caps = s.capsFor(c)
		s.startV2Liveness(c)
	}
	out, _ := protocol.NewEnvelope(protocol.TypePairOK, env.ID, pairOK)
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
	s.replaceElders(c, dev.ID)
	return dev, nil
}

// replaceElders closes this device's other authenticated connections with
// the typed CloseReplaced code (MADR 0068 D3): one live socket per device,
// so a reconnect-heavy client's own half-open zombies can never exhaust
// MaxWSClients against it (A1 F11). Skipped when device tokens are off —
// every dev-mode client shares one identity and replacement would kick
// them all on each auth.
func (s *Server) replaceElders(c *client, deviceID string) {
	if !s.requireDeviceToken {
		return
	}
	s.mu.Lock()
	var elders []*client
	for other := range s.clients {
		if other != c && other.authed && other.deviceID == deviceID {
			elders = append(elders, other)
			// Free the slot under the same lock the capacity check takes
			// (mirroring pre-auth eviction): the device that triggered this
			// replacement is often about to dial again, and an async
			// removal would let that dial race the elder's read-loop exit
			// into `too many clients` — the exact A1 F11 shape this
			// decision removes. The elder's own deferred delete is a no-op.
			delete(s.clients, other)
		}
	}
	s.mu.Unlock()
	for _, e := range elders {
		s.logDisconnect(e, "replaced")
		// Off this goroutine: a graceful close waits for the peer's close
		// echo, and a suspended elder answers nothing. The typed code must
		// still be *sent* (a live elder parks on it instead of
		// reconnect-fighting), so try Close first with CloseNow as the
		// backstop; shutdown() is idempotent either way.
		go func(e *client) {
			_ = e.conn.Close(websocket.StatusCode(protocol.CloseReplaced), protocol.CloseReplacedReason)
			e.shutdown()
			_ = e.conn.CloseNow()
		}(e)
	}
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
	// Both rejections are logged here — the one place with the device and
	// both fingerprints in scope (MADR 0066 D8). Without this a phone whose
	// storage reset regenerated its key can hammer auth for days with no
	// host-side trace; the prefixes match `pair list`'s KEY column and the
	// phone's Client-identity tile for a by-eye diff.
	if c.clientKeyFP == "" {
		s.log.Warn("client key rejected",
			slog.String("device_id", dev.ID),
			slog.String("device_name", dev.Name),
			slog.String("reason", "missing"),
			slog.String("enrolled_fp", fpPrefix(dev.ClientKeyFP)),
			slog.String("presented_fp", fpPrefix(c.clientKeyFP)),
		)
		return errClientKeyRequired
	}
	if c.clientKeyFP != dev.ClientKeyFP {
		s.log.Warn("client key rejected",
			slog.String("device_id", dev.ID),
			slog.String("device_name", dev.Name),
			slog.String("reason", "mismatch"),
			slog.String("enrolled_fp", fpPrefix(dev.ClientKeyFP)),
			slog.String("presented_fp", fpPrefix(c.clientKeyFP)),
		)
		return errClientKeyMismatch
	}
	// Self-healing backfill (MADR 0077 D9): a device enrolled before
	// ClientKeySPKI existed has an enrolled fingerprint but no persisted
	// public key. The fingerprint match just above already proves this
	// connection's key is the enrolled one, so it's safe to persist it now
	// without forcing a re-pair. Routine and silent — Debug, not Warn/Info.
	if len(dev.ClientKeySPKI) == 0 && len(c.clientKeySPKI) > 0 {
		if err := s.store.BackfillClientKeySPKI(dev.ID, c.clientKeySPKI); err != nil {
			s.log.Debug("client key SPKI backfill failed",
				slog.String("device_id", dev.ID),
				slog.String("err", err.Error()),
			)
		}
	}
	return nil
}

// fpPrefix truncates an SPKI fingerprint for logs and listings: enough to
// match by eye against the full value shown elsewhere, without reproducing
// whole fingerprints in every log line. "-" stands for a keyless record.
func fpPrefix(fp string) string {
	if fp == "" {
		return "-"
	}
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
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
	case len(p.ThinkingLevel) > maxThinkingLevelLen:
		return s.writeError(ctx, c, env.ID, "bad_payload", "thinking_level too long")
	case len(p.PermissionProfileID) > 256:
		return s.writeError(ctx, c, env.ID, "bad_payload", "permission_profile_id too long")
	case len(p.ApprovalsReviewer) > 32:
		return s.writeError(ctx, c, env.ID, "bad_payload", "approvals_reviewer too long")
	case len(p.Agent) > maxAgentLen:
		return s.writeError(ctx, c, env.ID, "bad_payload", "agent too long")
	case len(p.AgentSessionID) > maxAgentSessionIDLen:
		return s.writeError(ctx, c, env.ID, "bad_payload", "agent_session_id too long")
	}
	if len(p.Name) > maxNameLen {
		p.Name = p.Name[:maxNameLen]
	}
	meta, err := s.sessions.Create(ctx, provider.ID(p.Provider), provider.StartOptions{
		Name:                p.Name,
		CWD:                 p.CWD,
		Model:               p.Model,
		ThinkingLevel:       p.ThinkingLevel,
		PermissionProfileID: p.PermissionProfileID,
		ApprovalsReviewer:   p.ApprovalsReviewer,
		Agent:               p.Agent,
		AgentSessionID:      p.AgentSessionID,
		LocalSessionID:      p.SessionID,
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

// handleReceiptsList returns the calling device's OWN receipt chain (MADR
// 0078 D8). Scoped strictly by the connection's authenticated device id — a
// device can never read another device's chain, the exact analog of session
// ownership (§1). Empty when receipts are off or the device has no chain.
func (s *Server) handleReceiptsList(ctx context.Context, c *client, env protocol.Envelope) error {
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	entries, err := s.sessions.ReceiptEntriesFor(deviceID)
	if err != nil {
		return s.writeError(ctx, c, env.ID, protocol.ErrReceiptsListFailed, err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeReceiptsListResult, env.ID, protocol.ReceiptsListResultPayload{
		Entries: entries,
	})
	return s.writeJSON(ctx, c, out)
}

// handleDevicesList returns the paired-device roster (MADR 0078), so a device
// can pick a handoff target. Every paired device is listed, the caller's own
// row flagged Self. Only identity fields (id, name) — never keys. This is a
// fleet roster (unlike receipts, which are strictly own-device): any paired
// device may enumerate its fleetmates to hand a session to one.
func (s *Server) handleDevicesList(ctx context.Context, c *client, env protocol.Envelope) error {
	s.mu.Lock()
	me := c.deviceID
	s.mu.Unlock()
	if s.store == nil {
		out, _ := protocol.NewEnvelope(protocol.TypeDevicesListResult, env.ID, protocol.DevicesListResultPayload{})
		return s.writeJSON(ctx, c, out)
	}
	list, err := s.store.List()
	if err != nil {
		return s.writeError(ctx, c, env.ID, protocol.ErrDevicesListFailed, err.Error())
	}
	devices := make([]protocol.DeviceInfo, 0, len(list))
	for _, d := range list {
		devices = append(devices, protocol.DeviceInfo{
			DeviceID: d.ID,
			Name:     d.Name,
			Self:     d.ID == me,
		})
	}
	out, _ := protocol.NewEnvelope(protocol.TypeDevicesListResult, env.ID, protocol.DevicesListResultPayload{
		Devices: devices,
	})
	return s.writeJSON(ctx, c, out)
}

// handlePermissionReceipt delivers a device's signed receipt to whichever
// goroutine is waiting on it in RequestReceipt, correlated by PermissionID
// (the correlation id) rather than by this connection — the device may have
// reconnected since the receipt_request was sent (MADR 0077 P7 step 4).
// Answers with TypeOK regardless of whether a waiter was still around to
// receive it (a slow phone past the 10s window is not the client's error to
// see): this is what lets the phone use the same request()/await-response
// path every other outbound message already uses, instead of needing a
// bespoke fire-and-forget send.
func (s *Server) handlePermissionReceipt(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.PermissionReceiptPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.PermissionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "permission_id required")
	}
	// Read path: same goroutine as setAuthed for this connection after auth.
	s.mu.Lock()
	senderDeviceID := c.deviceID
	s.mu.Unlock()
	s.receiptMu.Lock()
	w, ok := s.receiptWaiters[p.PermissionID]
	s.receiptMu.Unlock()
	// Only the device that was asked to sign may deliver the reply (see
	// receiptWaiter's doc comment). A mismatched sender still gets ok — the
	// same don't-leak-waiter-state answer a late reply gets — but its JWS
	// never reaches the verifier.
	if ok && w.deviceID == senderDeviceID {
		select {
		case w.ch <- p.JWS:
		default:
			// Already delivered (or the waiter gave up) — never block the
			// read loop on a channel nobody is receiving from anymore.
		}
	} else if ok {
		s.log.Warn("permission.receipt from a device that was not asked to sign; ignored",
			slog.String("permission_id", p.PermissionID),
			slog.String("sender_device_id", senderDeviceID),
			slog.String("expected_device_id", w.deviceID),
		)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

// RequestReceipt implements session.ReceiptTransport: sends a receipt
// request to deviceID's live connection(s) and waits for a matching signed
// reply, up to ctx's deadline (MADR 0077 D8's 10s, set by the caller).
// Correlated by correlationID, not connection identity, so a device that
// reconnects mid-round-trip is still found when it replies. correlationID is
// an opaque string — a permission id for a permission-decision receipt, a
// handoff nonce for a session-handoff receipt (MADR 0078 D5).
//
// The permission-receipt wire message (permission.receipt_request / .receipt)
// carries correlationID in its permission_id field; the payload shape is
// unchanged from 0077. Handoff receipts (P4) ride the same waiter map under a
// generalized message.
func (s *Server) RequestReceipt(ctx context.Context, deviceID, sessionID, correlationID string, statement json.RawMessage) (string, error) {
	env, err := protocol.NewEnvelope(protocol.TypePermissionReceiptRequest, "", protocol.PermissionReceiptRequestPayload{
		SessionID:    sessionID,
		PermissionID: correlationID,
		Statement:    statement,
	})
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(env)
	if err != nil {
		return "", err
	}

	ch := make(chan string, 1)
	s.receiptMu.Lock()
	if s.receiptWaiters == nil {
		s.receiptWaiters = make(map[string]receiptWaiter)
	}
	s.receiptWaiters[correlationID] = receiptWaiter{deviceID: deviceID, ch: ch}
	s.receiptMu.Unlock()
	defer func() {
		s.receiptMu.Lock()
		delete(s.receiptWaiters, correlationID)
		s.receiptMu.Unlock()
	}()

	if !s.sendToDevice(deviceID, b) {
		return "", fmt.Errorf("device %s has no live connection", deviceID)
	}

	select {
	case jws := <-ch:
		return jws, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// sendToDevice enqueues b on every live, authenticated connection belonging
// to deviceID (ordinarily one; more than one only across a brief overlap
// during reconnect). Reports whether any connection was found — the caller
// treats "no connection" as an immediate failure rather than waiting out the
// full round-trip timeout for a device that is not even online.
func (s *Server) sendToDevice(deviceID string, b []byte) bool {
	s.mu.Lock()
	var targets []*client
	for c := range s.clients {
		if c.authed && c.deviceID == deviceID {
			targets = append(targets, c)
		}
	}
	s.mu.Unlock()
	if len(targets) == 0 {
		return false
	}
	for _, c := range targets {
		_ = s.writeBytes(c, b)
	}
	return true
}

func (s *Server) handleQuestionRespond(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.QuestionRespondPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	defer p.ClearAnswers()
	if p.SessionID == "" || p.QuestionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id and question_id required")
	}
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	if err := s.sessions.RespondQuestion(ctx, p.SessionID, p.QuestionID, p.Answers, p.Cancelled, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "question_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionList(ctx context.Context, c *client, env protocol.Envelope) error {
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	snap, err := s.sessions.ListSnapshot(deviceID)
	if err != nil {
		return s.writeError(ctx, c, env.ID, protocol.ErrSessionListFailed, "session list store error")
	}
	// Gap-scaling surface (MADR 0068 P3): the retained-seq window per
	// session plus the seq-lineage epoch. Additive; v1 clients ignore both.
	seqs := make(map[string]protocol.SeqBoundsPayload, len(snap.Sessions))
	for _, meta := range snap.Sessions {
		first, latest := s.sessions.SeqBounds(meta.ID)
		if latest == 0 {
			continue
		}
		seqs[meta.ID] = protocol.SeqBoundsPayload{FirstSeq: first, LatestSeq: latest}
	}
	if len(seqs) == 0 {
		seqs = nil
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionListResult, env.ID, protocol.SessionListResultPayload{
		Sessions: snap.Sessions,
		Complete: snap.Complete,
		Degraded: snap.Degraded,
		Skipped:  snap.Skipped,
		Epoch:    s.sessions.Epoch(),
		Seqs:     seqs,
	})
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

// handleSessionRelease hands a session off (MADR 0078). Only the owner may
// release; the reply is a plain ok, and the releasing client drops the
// session from its own list on success (it is no longer the owner, so no
// broadcast reaches it — the reply is the signal).
func (s *Server) handleSessionRelease(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionReleasePayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	if _, err := s.sessions.Release(p.SessionID, deviceID, p.ToDeviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, protocol.ErrSessionReleaseFailed, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

// handleSessionClaim takes ownership of a released session (MADR 0078). The
// reply is the claimer's Meta in the same shape session.create returns, so
// the claiming client adds the session exactly as if it had created it.
func (s *Server) handleSessionClaim(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	meta, err := s.sessions.Claim(p.SessionID, deviceID)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, protocol.ErrSessionClaimFailed, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionCreated, env.ID, meta)
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
	first, latest := s.sessions.SeqBounds(p.SessionID)
	out, _ := protocol.NewEnvelope(protocol.TypeSessionHistoryResult, env.ID, protocol.SessionHistoryResultPayload{
		SessionID:    p.SessionID,
		Events:       events,
		Truncated:    truncated,
		NextSinceSeq: nextSeq,
		FirstSeq:     first,
		LatestSeq:    latest,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionPendingAsks(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	out, _ := protocol.NewEnvelope(
		protocol.TypeSessionPendingAsksResult,
		env.ID,
		protocol.SessionPendingAsksResultPayload{Events: s.sessions.PendingAsks(deviceID)},
	)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionPrompt(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionPromptPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	var attachments []provider.Content
	if len(p.Attachments) > 0 {
		attachments = make([]provider.Content, 0, len(p.Attachments))
		for _, a := range p.Attachments {
			attachments = append(attachments, provider.Content{
				Type:     a.Kind,
				MimeType: a.MimeType,
				Data:     a.Data,
				Filename: a.Filename,
			})
		}
	}
	if err := s.sessions.Prompt(ctx, p.SessionID, p.Text, attachments, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_prompt_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionSetMode(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionSetModePayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	if err := s.sessions.SetMode(ctx, p.SessionID, p.ModeID, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_set_mode_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionSetCollaboration(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionSetCollaborationPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if err := s.sessions.SetCollaborationMode(ctx, p.SessionID, p.ModeID, deviceID); err != nil {
		code := protocol.ErrSetCollaborationModeFailed
		if errors.Is(err, provider.ErrCollaborationUnsupported) {
			code = protocol.ErrCollaborationModeUnsupported
		} else if errors.Is(err, provider.ErrCollaborationInvalid) {
			code = protocol.ErrCollaborationModeInvalid
		} else if errors.Is(err, provider.ErrTurnBusy) {
			code = protocol.ErrTurnBusy
		}
		return s.writeSessionErr(ctx, c, env.ID, code, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionSetConfig(ctx context.Context, c *client, env protocol.Envelope) error {
	var p protocol.SessionSetConfigPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	if err := s.sessions.SetConfigOption(ctx, p.SessionID, p.OptionID, p.Kind, p.Value, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_set_config_failed", err)
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
	case errors.Is(err, session.ErrNotReleased):
		// MADR 0078: claim of a session whose owner has not released it.
		code = protocol.ErrSessionNotReleased
	case errors.Is(err, session.ErrLimitReached):
		code = "session_limit"
	case errors.Is(err, session.ErrShuttingDown):
		code = "shutting_down"
	case errors.Is(err, session.ErrPersist):
		code = protocol.ErrPersistFailed
	case errors.Is(err, provider.ErrTurnBusy):
		// MADR 0020: second prompt while a turn is active (not a generic fail).
		code = "turn_busy"
	case errors.Is(err, provider.ErrInvalidAgent):
		code = "bad_agent"
	case errors.Is(err, fs.ErrPermission):
		// OS permission denial (EPERM/EACCES — file modes, sandbox policy,
		// or macOS TCC). Stable code so the phone can render actionable
		// copy instead of the raw errno string (MADR 0069 D4).
		code = protocol.ErrPermissionDenied
	}
	msg := err.Error()
	if len(msg) > 300 {
		// Provider/store errors can drag along multi-line detail (paths, JSON);
		// the phone needs the headline, the journal has the rest.
		msg = msg[:300] + "…"
	}
	// Guidance rides after the clip so the actionable part survives a long
	// provider message (MADR 0069 D4.5).
	if code == protocol.ErrPermissionDenied {
		msg = permissionGuidance(err, msg)
	}
	return s.writeError(ctx, c, id, code, msg)
}

// oldestUnauthedLocked returns the earliest-admitted client that has not yet
// authenticated, or nil if every client is authenticated. Caller holds s.mu.
func (s *Server) oldestUnauthedLocked() *client {
	var oldest *client
	for c := range s.clients {
		if c.authed {
			continue
		}
		if oldest == nil || c.seq < oldest.seq {
			oldest = c
		}
	}
	return oldest
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

func (s *Server) handleProvidersList(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	// MADR 0074 D6: only a client that negotiated the capability gets the auth
	// block, and only for such a client does the daemon pay for the probes.
	withAuth := s.clientWantsProviderAuth(c)
	var list []provider.Info
	if withAuth {
		list = s.registry.ListWithAuth(ctx)
	} else {
		list = s.registry.List()
	}
	providers := make([]protocol.ProviderInfoPayload, 0, len(list))
	for _, info := range list {
		entry := protocol.ProviderInfoPayload{
			ID:    string(info.ID),
			Ready: info.Ready,
		}
		if s.prewarm != nil {
			if v, ok := s.prewarm.Current(info.ID); ok {
				entry.Prewarm = v
			}
		}
		if withAuth {
			deviceOK := false
			if inst, err := s.registry.Get(info.ID); err == nil {
				deviceOK = deviceAuthWired(inst)
			}
			entry.Auth = authStatePayload(info.Auth, deviceOK)
		}
		providers = append(providers, entry)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeProvidersResult, env.ID, protocol.ProvidersResultPayload{
		Providers: providers,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleProvidersSetPrewarm(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	if s.prewarm == nil {
		return s.writeError(ctx, c, env.ID, "unsupported", "prewarm control is not available")
	}
	var req protocol.ProvidersSetPrewarmPayload
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "invalid providers.set_prewarm payload")
	}
	if req.ProviderID == "" || !config.KnownProvider(req.ProviderID) {
		return s.writeError(ctx, c, env.ID, "unknown_provider", "unknown provider")
	}
	engine, err := s.prewarm.Set(ctx, provider.ID(req.ProviderID), req.Prewarm)
	if err != nil {
		switch {
		case errors.Is(err, config.ErrUnknownProvider):
			return s.writeError(ctx, c, env.ID, "unknown_provider", err.Error())
		case errors.Is(err, provider.ErrEngineNotStartable):
			// The flag is on disk; only the engine could not be booted. Say so
			// rather than reporting a write failure the operator cannot find.
			return s.writeError(ctx, c, env.ID, "provider_not_ready", err.Error())
		default:
			return s.writeError(ctx, c, env.ID, "config_write_failed", err.Error())
		}
	}
	body := protocol.ProvidersPrewarmPayload{
		ProviderID: req.ProviderID,
		Prewarm:    req.Prewarm,
		Engine:     engine,
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, body)
	if err := s.writeJSON(ctx, c, out); err != nil {
		return err
	}
	s.broadcastPrewarm(body)
	return nil
}

// broadcastPrewarm pushes providers.prewarm to every authenticated client,
// including the one that asked, so a second phone's switch tracks the change
// without a re-list (MADR 0089 D7).
func (s *Server) broadcastPrewarm(body protocol.ProvidersPrewarmPayload) {
	env, err := protocol.NewEnvelope(protocol.TypeProvidersPrewarm, "", body)
	if err != nil {
		s.log.Error("broadcast: encoding providers.prewarm failed; dropping",
			slog.String("err", err.Error()))
		return
	}
	s.mu.Lock()
	targets := make([]*client, 0, len(s.clients))
	for cl := range s.clients {
		if cl.authed {
			targets = append(targets, cl)
		}
	}
	s.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	// One marshal, one buffer fanned out — same as BroadcastEvent.
	b, err := json.Marshal(env)
	if err != nil {
		s.log.Error("broadcast: encoding providers.prewarm failed; dropping",
			slog.String("err", err.Error()))
		return
	}
	for _, cl := range targets {
		_ = s.writeBytes(cl, b)
	}
}

// clientWantsProviderAuth reports whether this connection negotiated v2 and
// the daemon advertised the provider_auth capability to it (MADR 0074 D6).
func (s *Server) clientWantsProviderAuth(c *client) bool {
	if s.registry == nil {
		return false
	}
	s.mu.Lock()
	negotiated := c.negotiated
	s.mu.Unlock()
	return negotiated >= protocol.V2
}

// authStatePayload converts the domain auth state to its wire form. Returns
// nil for a provider that reported nothing, so the entry stays byte-identical
// to v1 (D4).
func authStatePayload(st *provider.AuthState, deviceOK bool) *protocol.ProviderAuthPayload {
	if st == nil {
		return nil
	}
	out := &protocol.ProviderAuthPayload{
		Status:         st.Status,
		ActiveUpstream: st.ActiveUpstream,
	}
	if out.Status == "" {
		out.Status = protocol.AuthStatusMissing
	}
	for _, up := range st.Upstreams {
		out.Upstreams = append(out.Upstreams, upstreamAuthPayload(up, deviceOK))
	}
	return out
}

// deviceAuthWired reports whether p can actually run a device flow. The
// transports declare StartDeviceAuth unconditionally, so the type assertion
// alone would promise flows that return ErrAuthUnsupported (MADR 0083 D4);
// DeviceAuthCapable is the ground truth where it exists.
func deviceAuthWired(p any) bool {
	if c, ok := p.(provider.DeviceAuthCapable); ok {
		return c.SupportsDeviceAuth()
	}
	_, ok := p.(provider.DeviceAuth)
	return ok
}

// upstreamAuthPayload converts one upstream to its wire shape. Shared by the
// status block and the on-demand catalog (MADR 0074 D16) so the two can never
// describe the same method differently.
func upstreamAuthPayload(up provider.UpstreamAuth, deviceOK bool) protocol.UpstreamAuthPayload {
	u := protocol.UpstreamAuthPayload{
		ID:     up.ID,
		Label:  up.Label,
		Status: up.Status,
	}
	if u.Status == "" {
		u.Status = protocol.AuthStatusMissing
	}
	for _, m := range up.Methods {
		pm := protocol.AuthMethodPayload{
			ID:    m.ID,
			Type:  m.Type,
			Label: m.Label,
		}
		// MADR 0083 D4: say up front what this host cannot drive, instead of
		// failing after the user typed a secret. Provider-specific knowledge
		// (goose's keyring) arrives on the method; the transport-generic
		// classes are annotated here so every provider gets them for free.
		reason := ""
		switch {
		case m.Unavailable:
			reason = m.Reason
		case m.Type == provider.AuthMethodOAuthBrowser:
			reason = protocol.AuthReasonBrowserOnly
		case m.Type == provider.AuthMethodOAuthDevice && !deviceOK:
			reason = protocol.AuthReasonDeviceUnsupported
		}
		if reason != "" {
			f := false
			pm.Available = &f
			pm.Reason = reason
		}
		if m.ConfiguredKnown {
			c := m.Configured
			pm.Configured = &c
		}
		for _, in := range m.Inputs {
			pi := protocol.AuthInputPayload{
				Key:         in.Key,
				Type:        in.Type,
				Message:     in.Message,
				Placeholder: in.Placeholder,
				Required:    in.Required,
			}
			for _, o := range in.Options {
				pi.Options = append(pi.Options, protocol.AuthInputOptionPayload{
					Value: o.Value,
					Label: o.Label,
					Hint:  o.Hint,
				})
			}
			if in.When != nil {
				pi.When = &protocol.AuthInputConditionPayload{
					Key:   in.When.Key,
					Op:    in.When.Op,
					Value: in.When.Value,
				}
			}
			pm.Inputs = append(pm.Inputs, pi)
		}
		u.Methods = append(u.Methods, pm)
	}
	return u
}

// Catalog paging (MADR 0074 D16). The engines advertise ~185 vendors; sent in
// one frame that is ~30 KB, which is both slow on a cellular link and close to
// the frame ceiling some clients set (coder/websocket defaults to 32 KiB on
// read). So the catalog pages, and a page that is not the last one says so.
const (
	defaultCatalogPage = 100
	maxCatalogPage     = 200
)

// handleAuthCatalog answers with every upstream an agent can authenticate,
// configured or not (MADR 0074 D16).
//
// This is a separate request rather than a field on providers.list because of
// the size difference: the status block is a handful of upstreams and rides on
// every listing and every status push, while the catalog is ~185 vendors and
// is wanted only when the user opens "add a credential".
func (s *Server) handleAuthCatalog(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	if !s.clientWantsProviderAuth(c) {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider auth capability not negotiated")
	}
	var p protocol.AuthCatalogRequestPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "bad payload")
	}
	if s.registry == nil {
		return s.writeError(ctx, c, env.ID, "provider_unavailable", "no providers registered")
	}
	prov, err := s.registry.Get(provider.ID(p.ProviderID))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", "unknown provider")
	}
	cataloger, ok := prov.(provider.AuthCataloger)
	if !ok {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider has no upstream catalog")
	}
	cat, err := cataloger.AuthCatalogList(ctx)
	if err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}

	matched := filterUpstreams(cat.Upstreams, p.Query)
	out := protocol.AuthCatalogPayload{
		ProviderID: p.ProviderID,
		Total:      len(matched),
		Source:     cat.Source,
	}
	page := p.Limit
	switch {
	case page <= 0:
		page = defaultCatalogPage
	case page > maxCatalogPage:
		page = maxCatalogPage
	}
	start := p.Offset
	if start < 0 {
		start = 0
	}
	if start > len(matched) {
		start = len(matched)
	}
	end := start + page
	if end > len(matched) {
		end = len(matched)
	}
	out.Offset = start
	out.Truncated = end < len(matched)
	out.Upstreams = make([]protocol.UpstreamAuthPayload, 0, end-start)
	deviceOK := deviceAuthWired(prov)
	for _, up := range matched[start:end] {
		out.Upstreams = append(out.Upstreams, upstreamAuthPayload(up, deviceOK))
	}
	// Encoding cannot fail for this shape (plain structs, no channels or
	// funcs); the same `_` as every other result frame in this file.
	frame, _ := protocol.NewEnvelope(protocol.TypeProviderAuthCatalogRes, env.ID, out)
	return s.writeJSON(ctx, c, frame)
}

// filterUpstreams narrows a catalog by a case-insensitive substring of the id
// or label, so a phone searching "together" pulls three rows instead of 185.
func filterUpstreams(ups []provider.UpstreamAuth, query string) []provider.UpstreamAuth {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return ups
	}
	out := make([]provider.UpstreamAuth, 0, len(ups))
	for _, up := range ups {
		if strings.Contains(strings.ToLower(up.ID), query) ||
			strings.Contains(strings.ToLower(up.Label), query) {
			out = append(out, up)
		}
	}
	return out
}

// handleSetCredential stores a credential in the agent's own store
// (MADR 0074 D1/D2). The secret is never logged, never echoed back, and never
// written anywhere by mcremote itself.
func (s *Server) handleSetCredential(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	if !s.clientWantsProviderAuth(c) {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider auth capability not negotiated")
	}
	var p protocol.SetCredentialPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "bad payload")
	}
	w, errFrame := s.authWriterFor(ctx, c, env, p.ProviderID)
	if w == nil {
		return errFrame
	}
	err := w.SetCredential(ctx, p.UpstreamID, p.MethodID, p.Secret, p.Inputs)
	// Drop the local copy as soon as the write returns. Go strings are
	// immutable so this cannot scrub the bytes, but it does stop the payload
	// from staying reachable for the rest of the handler (D11).
	p.Secret = ""
	if err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}
	s.log.Info("provider credential set",
		slog.String("provider", p.ProviderID),
		slog.String("upstream", p.UpstreamID))
	s.pushProviderAuthStatus(ctx, p.ProviderID)
	return s.writeOKFrame(ctx, c, env.ID)
}

// handleClearCredential removes a stored credential.
func (s *Server) handleClearCredential(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	if !s.clientWantsProviderAuth(c) {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider auth capability not negotiated")
	}
	var p protocol.ClearCredentialPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "bad payload")
	}
	w, errFrame := s.authWriterFor(ctx, c, env, p.ProviderID)
	if w == nil {
		return errFrame
	}
	// An empty method id preserves the legacy aggregate clear. A non-empty one
	// requires AuthMethodClearer and must never fall back to the aggregate:
	// falling back would remove a credential the caller did not name, which on
	// Grok means signing out of a browser login when the user asked to delete a
	// pasted key (MADR 0074 P20 step 8).
	if p.MethodID != "" {
		mc, ok := w.(provider.AuthMethodClearer)
		if !ok {
			return s.writeError(ctx, c, env.ID, "unsupported",
				"provider cannot clear an individual auth method")
		}
		if err := mc.ClearCredentialMethod(ctx, p.UpstreamID, p.MethodID); err != nil {
			return s.writeAuthErr(ctx, c, env, err)
		}
	} else if err := w.ClearCredential(ctx, p.UpstreamID); err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}
	s.log.Info("provider credential cleared",
		slog.String("provider", p.ProviderID),
		slog.String("upstream", p.UpstreamID),
		slog.String("method", p.MethodID))
	s.pushProviderAuthStatus(ctx, p.ProviderID)
	return s.writeOKFrame(ctx, c, env.ID)
}

// handleSetActiveUpstream repoints an agent at another configured upstream
// (MADR 0074 D14) — the MADR 0073 quota mitigation.
func (s *Server) handleSetActiveUpstream(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	if !s.clientWantsProviderAuth(c) {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider auth capability not negotiated")
	}
	var p protocol.SetActiveUpstreamPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "bad payload")
	}
	if s.registry == nil {
		return s.writeError(ctx, c, env.ID, "provider_unavailable", "no providers registered")
	}
	prov, err := s.registry.Get(provider.ID(p.ProviderID))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", "unknown provider")
	}
	sw, ok := prov.(provider.UpstreamSwitcher)
	if !ok {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider cannot switch upstream")
	}
	if err := sw.SetActiveUpstream(ctx, p.UpstreamID); err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}
	s.log.Info("provider active upstream switched",
		slog.String("provider", p.ProviderID),
		slog.String("upstream", p.UpstreamID))
	s.pushProviderAuthStatus(ctx, p.ProviderID)
	return s.writeOKFrame(ctx, c, env.ID)
}

// handleStartAuth begins an interactive device flow (MADR 0074 Strategy A).
//
// The request returns as soon as the code is known; completion arrives later
// as oauth.device_flow_result, because a device flow is bounded by how long a
// human takes to type a code into another device.
func (s *Server) handleStartAuth(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	if !s.clientWantsProviderAuth(c) {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider auth capability not negotiated")
	}
	var p protocol.StartAuthPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "bad payload")
	}
	if s.registry == nil {
		return s.writeError(ctx, c, env.ID, "provider_unavailable", "no providers registered")
	}
	prov, err := s.registry.Get(provider.ID(p.ProviderID))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", "unknown provider")
	}
	// Prefer the owned, transactional contract when the provider offers it.
	// It is not a fallback relationship: a provider that has adopted owned
	// flows must never be driven through the legacy path, because that path
	// is the one that can orphan a child (MADR 0074 D27).
	if owned, ok := prov.(provider.OwnedDeviceAuth); ok {
		return s.startOwnedDeviceAuth(ctx, c, env, deviceID, owned, p)
	}
	starter, ok := prov.(provider.DeviceAuth)
	if !ok {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider has no device sign-in")
	}

	// Detached from the request context: the flow outlives this round trip by
	// design, and cancelling it when the request returns would kill every
	// flow the instant it started.
	flowCtx := context.WithoutCancel(ctx)
	df, wait, err := starter.StartDeviceAuth(flowCtx, p.UpstreamID, p.MethodID, p.Inputs, p.ConfirmDestructive)
	if err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}

	flow, runCtx, err := s.deviceFlows.Add(flowCtx, &providerauth.Flow{
		ProviderID:      p.ProviderID,
		UpstreamID:      p.UpstreamID,
		DeviceID:        deviceID,
		VerificationURI: df.VerificationURI,
		UserCode:        df.UserCode,
		ExpiresAt:       expiryFrom(df.ExpiresIn),
		Interval:        time.Duration(df.Interval) * time.Second,
	})
	if err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}

	if err := s.writeOKFrame(ctx, c, env.ID); err != nil {
		s.deviceFlows.Finish(flow.ID)
		return err
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOAuthDeviceFlow, "", protocol.DeviceFlowPayload{
		FlowID:          flow.ID,
		ProviderID:      p.ProviderID,
		UpstreamID:      p.UpstreamID,
		VerificationURI: flow.VerificationURI,
		UserCode:        flow.UserCode,
		ExpiresIn:       flow.ExpiresIn(),
		Interval:        int(flow.Interval.Seconds()),
	})
	if err := s.writeJSON(ctx, c, out); err != nil {
		s.deviceFlows.Finish(flow.ID)
		return err
	}

	go s.awaitDeviceFlow(runCtx, c, flow, wait)
	return nil
}

// awaitDeviceFlow blocks on the provider's completion and reports the outcome.
func (s *Server) awaitDeviceFlow(ctx context.Context, c *client, flow *providerauth.Flow, wait func(context.Context) error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("device flow wait panic", slog.Any("recover", r))
		}
	}()
	err := wait(ctx)
	s.deviceFlows.Finish(flow.ID)

	payload := protocol.DeviceFlowResultPayload{FlowID: flow.ID, OK: err == nil}
	if err != nil {
		payload.Error = clipAuthErr(err.Error())
		payload.ErrorKind = string(agenterr.Present(err.Error(), time.Now()).Kind)
	}
	out, encErr := protocol.NewEnvelope(protocol.TypeOAuthDeviceFlowResult, "", payload)
	if encErr == nil {
		// Best-effort: the phone may be gone, in which case the status push
		// below is what a reconnecting client will see instead.
		_ = s.writeJSON(ctx, c, out)
	}
	if err != nil {
		s.log.Info("device flow finished",
			slog.String("provider", flow.ProviderID),
			slog.String("upstream", flow.UpstreamID),
			slog.Bool("ok", false),
			slog.String("err", clipAuthErr(err.Error())),
			slog.String("error_kind", payload.ErrorKind))
	} else {
		s.log.Info("device flow finished",
			slog.String("provider", flow.ProviderID),
			slog.String("upstream", flow.UpstreamID),
			slog.Bool("ok", true))
	}
	s.pushProviderAuthStatus(ctx, flow.ProviderID)
}

// handleOAuthCancel aborts a flow. Only its owning device may cancel it.
func (s *Server) handleOAuthCancel(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	if !s.clientWantsProviderAuth(c) {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider auth capability not negotiated")
	}
	var p protocol.OAuthCancelPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "bad payload")
	}
	// Owned flows first; both paths keep unknown and not-yours
	// indistinguishable so a flow id cannot probe another device's activity.
	if res, err := s.deviceFlows.Reservation(p.FlowID, deviceID); err == nil {
		// Idempotent by construction: a phone that retries a cancel after a
		// reconnect must not see a failure (MADR 0074 D28).
		res.Cancel()
		return s.writeOKFrame(ctx, c, env.ID)
	}
	if err := s.deviceFlows.Cancel(p.FlowID, deviceID); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "no such flow")
	}
	return s.writeOKFrame(ctx, c, env.ID)
}

// expiryFrom turns a provider's expires_in into an absolute deadline. Zero
// means "unstated"; the registry then applies its own ceiling.
func expiryFrom(seconds int) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

// authWriterFor resolves a provider's credential writer, writing the
// appropriate error frame and returning nil when it cannot.
func (s *Server) authWriterFor(ctx context.Context, c *client, env protocol.Envelope, providerID string) (provider.AuthWriter, error) {
	if s.registry == nil {
		return nil, s.writeError(ctx, c, env.ID, "provider_unavailable", "no providers registered")
	}
	prov, err := s.registry.Get(provider.ID(providerID))
	if err != nil {
		return nil, s.writeError(ctx, c, env.ID, "unknown_provider", "unknown provider")
	}
	w, ok := prov.(provider.AuthWriter)
	if !ok {
		return nil, s.writeError(ctx, c, env.ID, "unsupported", "provider credentials are read-only")
	}
	return w, nil
}

// writeAuthErr maps the auth sentinels to client-actionable frames. ErrAuthBusy
// in particular must read as "retry later", not as a failure the user should
// try to fix (MADR 0074 D9).
func (s *Server) writeAuthErr(ctx context.Context, c *client, env protocol.Envelope, err error) error {
	code, msg := authErrCode(err)
	return s.writeError(ctx, c, env.ID, code, msg)
}

// authErrCode maps a provider-auth failure to its wire code and a message the
// phone can act on (MADR 0083 D5). The residual credential_failed message can
// quote an agent's stderr; it must never quote a key, which is why no write
// path puts the secret in its error text.
func authErrCode(err error) (code, msg string) {
	switch {
	case errors.Is(err, provider.ErrAuthBusy):
		return protocol.ErrProviderBusy,
			"a turn is running on this provider; try again when it finishes"
	case errors.Is(err, provider.ErrAuthUnsupported):
		return "unsupported", "unsupported for this provider"
	case errors.Is(err, provider.ErrAuthConfirmRequired):
		return protocol.ErrConfirmRequired, "this flow needs explicit confirmation"
	case errors.Is(err, credstore.ErrGooseKeyringManaged):
		return protocol.ErrKeyringManaged,
			"this agent keeps its keys in the host's OS keyring; add the key on the host"
	case errors.Is(err, provider.ErrAuthMethodUnsupported):
		return protocol.ErrMethodUnsupported,
			"this sign-in method can't be driven from the phone for this agent"
	case errors.Is(err, credstore.ErrEmptySecret),
		errors.Is(err, credstore.ErrSecretTooLarge):
		return protocol.ErrInvalidKey, clipAuthErr(err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return protocol.ErrEngineUnavailable,
			"the agent's engine did not answer; is it running on the host?"
	case errors.Is(err, provider.ErrCredentialNotAccepted):
		return protocol.ErrCredentialNotAccepted,
			"the agent stored the value but is not using it; this vendor needs a different sign-in method"
	default:
		return protocol.ErrCredentialFailed, clipAuthErr(err.Error())
	}
}

// pushProviderAuthStatus re-probes one provider and pushes its state to every
// connection that can use it (MADR 0074 D6/D10). Called after a credential
// change so all of a user's devices converge, including the ones that did not
// make the change.
func (s *Server) pushProviderAuthStatus(ctx context.Context, providerID string) {
	if s.registry == nil {
		return
	}
	p, err := s.registry.Get(provider.ID(providerID))
	if err != nil {
		return
	}
	auth, ok := p.(provider.Auth)
	if !ok {
		return
	}
	st, err := auth.AuthStatus(ctx)
	payload := protocol.ProviderInfoPayload{ID: providerID, Ready: p.Ready()}
	if err != nil {
		payload.Auth = &protocol.ProviderAuthPayload{Status: protocol.AuthStatusError}
	} else {
		payload.Auth = authStatePayload(&st, deviceAuthWired(p))
	}
	out, err := protocol.NewEnvelope(protocol.TypeProviderAuthStatus, "", payload)
	if err != nil {
		return
	}
	// Snapshot under the lock, write outside it — the same discipline every
	// other fan-out in this file follows, so a slow socket cannot stall the
	// server mutex.
	s.mu.Lock()
	targets := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		if !c.authed || c.negotiated < protocol.V2 {
			continue
		}
		targets = append(targets, c)
	}
	s.mu.Unlock()

	for _, c := range targets {
		if err := s.writeJSON(ctx, c, out); err != nil {
			s.log.Debug("provider auth status push failed", slog.String("err", err.Error()))
		}
	}
}

// maxCatalogOptions bounds one models.list reply. Nothing reaches it today —
// the largest single model provider on this host is 60 models — but the reply
// is a single WebSocket frame and the relay caps a message at 1 MiB
// (internal/relay/config.go). The cap is the guard for a future catalog that
// grows past the frame, where the failure mode without it is not a long list
// but a dropped connection (MADR 0043 D4).
const maxCatalogOptions = 500

// handleModelsList returns a picker catalog for one provider (models.list).
//
// Four routes, tried in order: session-scoped, model-provider enumeration,
// one model provider's models, and the provider's default set. The routes are
// selected by the request, not guessed — an unrecognised scope is a payload
// error, because silently answering a different question than the one asked
// produces a picker that looks right and is wrong.
func (s *Server) handleModelsList(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var req protocol.ModelsListPayload
	if err := protocol.DecodePayload(env, &req); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "invalid models.list payload")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "provider is required")
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = provider.CatalogScopeModels
	}
	if scope != provider.CatalogScopeModels && scope != provider.CatalogScopeProviders {
		return s.writeError(ctx, c, env.ID, "bad_payload", "scope must be \"models\" or \"providers\"")
	}
	p, err := s.registry.Get(provider.ID(req.Provider))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", err.Error())
	}

	modelProvider := strings.TrimSpace(req.ModelProvider)
	// Empty allow-custom catalog: the reply when a provider advertises nothing,
	// so free-text always remains possible.
	cat := picker.SingleCatalog(picker.SourceStatic, nil, "", true)

	if sid := strings.TrimSpace(req.SessionID); sid != "" {
		listed, listErr := s.sessions.ModelCatalog(ctx, sid, deviceID, scope)
		if errors.Is(listErr, session.ErrForbidden) {
			return s.writeError(ctx, c, env.ID, "session_forbidden", "not the session owner")
		}
		if listErr != nil {
			// A session that is closed, or a provider that reports no
			// session-scoped catalog, is not an error the user can act on —
			// fall back to the provider-wide answer rather than an empty
			// picker.
			s.log.Debug("models.list: session scope unavailable; provider catalog",
				slog.String("session_id", sid),
				slog.String("err", listErr.Error()))
		} else {
			cat = listed
			// The session resolved its own scope; echo the one actually
			// applied rather than the empty one requested, since the client
			// labels the picker with it (ModelsResultPayload.ModelProvider).
			if modelProvider == "" && scope == provider.CatalogScopeModels {
				modelProvider = commonOptionGroup(cat.Options)
			}
			return s.writeModelsResult(ctx, c, env.ID, req.Provider, modelProvider, cat)
		}
	}

	switch {
	case scope == provider.CatalogScopeProviders:
		mpc, ok := p.(provider.ModelProviderCatalog)
		if !ok {
			// One implicit model provider. Answering with an empty list would
			// make the client hide its provider step for the wrong reason;
			// answering with one option says "there is exactly one" truthfully.
			cat = picker.SingleCatalog(picker.SourceStatic, []picker.Option{{
				ID:    req.Provider,
				Label: req.Provider,
				Meta:  map[string]string{picker.MetaConnected: "true"},
			}}, req.Provider, false)
			break
		}
		listed, listErr := mpc.ListModelProviders(ctx)
		if listErr != nil {
			s.log.Debug("models.list providers failed",
				slog.String("provider", req.Provider),
				slog.String("err", listErr.Error()))
		} else {
			cat = listed
		}
	case modelProvider != "":
		mpc, ok := p.(provider.ModelProviderCatalog)
		if !ok {
			// The provider has one implicit model provider, so a filter for it
			// is the default catalog and a filter for anything else is empty.
			if mc, isCat := p.(provider.ModelCatalog); isCat && modelProvider == req.Provider {
				cat = s.listModelsOrLog(ctx, req.Provider, mc)
			}
			break
		}
		listed, listErr := mpc.ListModelsFor(ctx, modelProvider)
		if listErr != nil {
			s.log.Debug("models.list for model provider failed",
				slog.String("provider", req.Provider),
				slog.String("model_provider", modelProvider),
				slog.String("err", listErr.Error()))
		} else {
			cat = listed
		}
	default:
		if mc, ok := p.(provider.ModelCatalog); ok {
			cat = s.listModelsOrLog(ctx, req.Provider, mc)
		}
	}
	return s.writeModelsResult(ctx, c, env.ID, req.Provider, modelProvider, cat)
}

// commonOptionGroup returns the group every option shares, or "" when they
// differ or there are none. Model catalogs set Group to the model provider id,
// so a single-group catalog names the scope it covers.
func commonOptionGroup(opts []picker.Option) string {
	if len(opts) == 0 {
		return ""
	}
	g := opts[0].Group
	for _, o := range opts[1:] {
		if o.Group != g {
			return ""
		}
	}
	return g
}

// listModelsOrLog is the shared "ask, and keep the free-text fallback on
// failure" path. Still returns an allow-custom empty catalog so a user who
// knows the model id is never blocked by a catalog outage.
func (s *Server) listModelsOrLog(ctx context.Context, providerID string, mc provider.ModelCatalog) picker.Catalog {
	listed, err := mc.ListModels(ctx)
	if err != nil {
		s.log.Debug("models.list failed",
			slog.String("provider", providerID),
			slog.String("err", err.Error()))
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true)
	}
	return listed
}

// capCatalogOptions trims body to maxCatalogOptions, marking it Truncated and
// returning how many options were dropped. Truncation is never silent: a
// catalog that quietly loses rows reads to a user as "my model does not exist",
// so the flag travels with the reply and the client says so (MADR 0043 D4).
// A catalog that arrived already marked Truncated keeps that flag: the
// provider dropped rows before this layer ever saw them.
func capCatalogOptions(body *protocol.ModelsResultPayload) int {
	if len(body.Options) <= maxCatalogOptions {
		return 0
	}
	dropped := len(body.Options) - maxCatalogOptions
	body.Options = body.Options[:maxCatalogOptions]
	body.Truncated = true
	return dropped
}

// writeModelsResult applies the option cap and emits models.list_result.
func (s *Server) writeModelsResult(ctx context.Context, c *client, envID, providerID, modelProvider string, cat picker.Catalog) error {
	body := protocol.ModelsResultFromCatalog(providerID, cat)
	body.ModelProvider = modelProvider
	if dropped := capCatalogOptions(&body); dropped > 0 {
		s.log.Debug("models.list truncated",
			slog.String("provider", providerID),
			slog.Int("dropped", dropped))
	}
	out, _ := protocol.NewEnvelope(protocol.TypeModelsResult, envID, body)
	return s.writeJSON(ctx, c, out)
}

// handleAgentsList returns an agent-name picker catalog (agents.list).
func (s *Server) handleAgentsList(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	_ = deviceID
	var req protocol.AgentsListPayload
	if err := protocol.DecodePayload(env, &req); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "invalid agents.list payload")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "provider is required")
	}
	p, err := s.registry.Get(provider.ID(req.Provider))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", err.Error())
	}
	cat := picker.SingleCatalog(picker.SourceStatic, nil, "", true)
	if ac, ok := p.(provider.AgentCatalog); ok {
		listed, listErr := ac.ListAgents(ctx)
		if listErr != nil {
			s.log.Debug("agents.list failed",
				slog.String("provider", req.Provider),
				slog.String("err", listErr.Error()))
		} else {
			cat = listed
		}
	}
	out, _ := protocol.NewEnvelope(protocol.TypeAgentsResult, env.ID,
		protocol.AgentsResultFromCatalog(req.Provider, cat))
	return s.writeJSON(ctx, c, out)
}

// handleAgentSessionsList discovers provider-native sessions for the
// requesting device. This is a direct request/response only: results are never
// broadcast and no daemon session/ownership record is created by listing.
func (s *Server) handleAgentSessionsList(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	_ = deviceID
	var req protocol.AgentSessionsListPayload
	if err := protocol.DecodePayload(env, &req); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "invalid agent_sessions.list payload")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "provider is required")
	}
	p, err := s.registry.Get(provider.ID(req.Provider))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", err.Error())
	}
	if !p.Ready() {
		return s.writeError(ctx, c, env.ID, "provider_unavailable", "provider is not ready")
	}
	lister, ok := p.(provider.AgentSessionLister)
	if !ok {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider does not support native session discovery")
	}
	sessions, err := lister.ListAgentSessions(ctx)
	if err != nil {
		return s.writeError(ctx, c, env.ID, "agent_sessions_list_failed", err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeAgentSessionsResult, env.ID,
		protocol.AgentSessionsResultPayload{Provider: req.Provider, Sessions: sessions})
	return s.writeJSON(ctx, c, out)
}

// handleProjectsList returns the engine-known project list (projects.list).
//
// Provider-scoped, not session-scoped: it answers "where could a new session
// run", so it must work before any session exists. That is also why it does not
// take an ownership check — there is no session to own — while still requiring
// the normal authenticated owner connection every handler runs behind.
func (s *Server) handleProjectsList(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	_ = deviceID
	var req protocol.ProjectsListPayload
	if err := protocol.DecodePayload(env, &req); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "invalid projects.list payload")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "provider is required")
	}
	p, err := s.registry.Get(provider.ID(req.Provider))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", err.Error())
	}
	if !p.Ready() {
		return s.writeError(ctx, c, env.ID, "provider_unavailable", "provider is not ready")
	}
	cat, ok := p.(provider.ProjectCatalog)
	if !ok {
		return s.writeError(ctx, c, env.ID, "unsupported", "provider does not support project discovery")
	}
	projects, err := cat.ListProjects(ctx)
	if err != nil {
		return s.writeError(ctx, c, env.ID, protocol.ErrProjectsListFailed, err.Error())
	}
	out, _ := protocol.NewEnvelope(protocol.TypeProjectsResult, env.ID,
		protocol.ProjectsResultPayload{Provider: req.Provider, Projects: projects})
	return s.writeJSON(ctx, c, out)
}

// workspaceErrCode maps a dialect validation failure onto a stable client code.
//
// The upstream message is never copied verbatim into a phone-visible error: it
// can contain host paths, and the code alone is what a client branches on.
func workspaceErrCode(err error) (code, msg string) {
	switch {
	case errorTextHas(err, "invalid_path"):
		return protocol.ErrInvalidPath, "the requested path is not valid"
	case errorTextHas(err, "path_escape"):
		return protocol.ErrPathEscape, "the requested path leaves the session directory"
	case errorTextHas(err, "path_symlink"):
		return protocol.ErrPathSymlink, "the requested path contains a symbolic link"
	case errorTextHas(err, "binary_content"):
		return protocol.ErrBinaryContent, "that file is not UTF-8 text"
	case errorTextHas(err, "result_too_large"):
		return protocol.ErrResultTooLarge, "the result is too large to return"
	case errorTextHas(err, "invalid_query"):
		return protocol.ErrInvalidQuery, "the search query is not valid"
	default:
		return protocol.ErrWorkspaceFailed, "the workspace request failed"
	}
}

// errorTextHas reports whether err's chain mentions a sentinel name.
func errorTextHas(err error, name string) bool {
	return err != nil && strings.Contains(err.Error(), name)
}

// handleWorkspaceList lists one directory of an owned session's workspace.
func (s *Server) handleWorkspaceList(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.WorkspaceListPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	entries, err := s.sessions.ListWorkspace(ctx, p.SessionID, p.Path, deviceID)
	if err != nil {
		if code, msg := workspaceErrCode(err); code != protocol.ErrWorkspaceFailed {
			return s.writeError(ctx, c, env.ID, code, msg)
		}
		return s.writeSessionErr(ctx, c, env.ID, protocol.ErrWorkspaceFailed, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeWorkspaceListResult, env.ID,
		protocol.WorkspaceListResultPayload{SessionID: p.SessionID, Path: p.Path, Entries: entries})
	return s.writeJSON(ctx, c, out)
}

// handleWorkspaceRead returns one bounded text file.
func (s *Server) handleWorkspaceRead(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.WorkspaceReadPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	content, err := s.sessions.ReadWorkspace(ctx, p.SessionID, p.Path, deviceID)
	if err != nil {
		if code, msg := workspaceErrCode(err); code != protocol.ErrWorkspaceFailed {
			return s.writeError(ctx, c, env.ID, code, msg)
		}
		return s.writeSessionErr(ctx, c, env.ID, protocol.ErrWorkspaceFailed, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeWorkspaceReadResult, env.ID,
		protocol.WorkspaceReadResultPayload{
			SessionID: p.SessionID, Path: content.Path,
			Text: content.Text, Bytes: content.Bytes,
		})
	return s.writeJSON(ctx, c, out)
}

// handleWorkspaceSearch runs one bounded workspace search.
func (s *Server) handleWorkspaceSearch(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.WorkspaceSearchPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	res, err := s.sessions.SearchWorkspace(ctx, p.SessionID, p.Kind, p.Query, deviceID)
	if err != nil {
		if code, msg := workspaceErrCode(err); code != protocol.ErrWorkspaceFailed {
			return s.writeError(ctx, c, env.ID, code, msg)
		}
		return s.writeSessionErr(ctx, c, env.ID, protocol.ErrWorkspaceFailed, err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeWorkspaceSearchResult, env.ID,
		protocol.WorkspaceSearchResultPayload{
			SessionID: p.SessionID, Kind: res.Kind, Matches: res.Matches,
			Cap: res.Cap, Truncated: res.Truncated,
		})
	return s.writeJSON(ctx, c, out)
}

// handleCommandsList returns a slash-command catalog (commands.list).
func (s *Server) handleCommandsList(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	_ = deviceID
	var req protocol.CommandsListPayload
	if err := protocol.DecodePayload(env, &req); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", "invalid commands.list payload")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "provider is required")
	}
	p, err := s.registry.Get(provider.ID(req.Provider))
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unknown_provider", err.Error())
	}
	cat := picker.SingleCatalog(picker.SourceStatic, nil, "", true)
	if cc, ok := p.(provider.CommandCatalog); ok {
		listed, listErr := cc.ListCommands(ctx)
		if listErr != nil {
			s.log.Debug("commands.list failed",
				slog.String("provider", req.Provider),
				slog.String("err", listErr.Error()))
		} else {
			cat = listed
		}
	}
	// The canonical commands the daemon offers come first: they are the ones a
	// user can rely on across CLIs (MADR 0023). The agent's own catalog follows.
	cat.Options = append(
		s.sessions.CanonicalCommandOptions(req.SessionID, provider.ID(req.Provider)),
		cat.Options...)
	out, _ := protocol.NewEnvelope(protocol.TypeCommandsResult, env.ID,
		protocol.CommandsResultFromCatalog(req.Provider, cat))
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionFork(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionForkPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	boundary := p.LastTurnID
	if p.MessageID != "" && p.LastTurnID != "" && p.MessageID != p.LastTurnID {
		return s.writeError(ctx, c, env.ID, "bad_payload", "message_id and last_turn_id conflict")
	}
	if boundary == "" {
		boundary = p.MessageID
	}
	meta, err := s.sessions.Fork(ctx, p.SessionID, boundary, deviceID)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_fork_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionCreated, env.ID, meta)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionRevert(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionRevertPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" || p.MessageID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id and message_id required")
	}
	if err := s.sessions.Revert(ctx, p.SessionID, p.MessageID, p.PartID, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_revert_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionUnrevert(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionUnrevertPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	if err := s.sessions.Unrevert(ctx, p.SessionID, deviceID); err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_unrevert_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeOK, env.ID, nil)
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionDiff(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionDiffPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	res, err := s.sessions.Diff(ctx, p.SessionID, p.MessageID, deviceID)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_diff_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionDiffResult, env.ID, protocol.SessionDiffResultPayload{
		SessionID: p.SessionID,
		Summary:   res.Summary,
		BaseSHA:   res.BaseSHA,
		Scope:     res.Scope,
		Truncated: res.Truncated,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionRename(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionRenamePayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" || strings.TrimSpace(p.Name) == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id and name required")
	}
	if len(p.Name) > maxNameLen {
		return s.writeError(ctx, c, env.ID, "bad_payload", "name too long")
	}
	meta, err := s.sessions.Rename(ctx, p.SessionID, p.Name, deviceID)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_rename_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionRenameResult, env.ID,
		protocol.SessionRenameResultPayload{Session: meta})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleSessionDiagnostics(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var p protocol.SessionIDPayload
	if err := protocol.DecodePayload(env, &p); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	if p.SessionID == "" {
		return s.writeError(ctx, c, env.ID, "bad_payload", "session_id required")
	}
	diagnostics, err := s.sessions.Diagnostics(ctx, p.SessionID, deviceID)
	if err != nil {
		return s.writeSessionErr(ctx, c, env.ID, "session_diagnostics_failed", err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeSessionDiagnosticsResult, env.ID,
		protocol.SessionDiagnosticsResultPayload{SessionID: p.SessionID, Diagnostics: diagnostics})
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
	// The peer is unauthenticated here; surface a stable code and a fixed,
	// safe message only. Known auth outcomes have their own message; anything
	// unmapped is likely an internal/store error whose text can leak the
	// data-dir path or username, so it is logged and reported generically.
	code, msg := "auth_failed", "authentication failed"
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		code, msg = "invalid_token", "invalid or revoked token"
	case errors.Is(err, errClientKeyRequired):
		code, msg = "client_key_required", "a client key is required"
	case errors.Is(err, errClientKeyMismatch):
		code, msg = "client_key_mismatch", "client key mismatch"
	case errors.Is(err, errAlreadyAuthed):
		code, msg = "already_authed", "already authenticated"
	default:
		s.log.Warn("authentication error", slog.String("err", err.Error()))
	}
	out, _ := protocol.NewEnvelope(protocol.TypeAuthError, id, protocol.ErrorPayload{
		Message: msg,
		Code:    code,
	})
	return s.writeJSON(ctx, c, out)
}

func (s *Server) writeError(ctx context.Context, c *client, id, code, msg string) error {
	// Phone-visible errors are operator-visible (MADR 0069 D7): before
	// this, most error frames reached the phone with no daemon log line at
	// any level, so diagnosing meant a screenshot of the chat instead of a
	// grep. Bounded — msg is what the wire carries (callers clip).
	var device string
	if c != nil {
		s.mu.Lock()
		device = c.deviceID
		s.mu.Unlock()
	}
	s.log.Info("ws error frame",
		slog.String("code", code),
		slog.String("req_id", id),
		slog.String("device_id", device),
		slog.String("msg", msg),
	)
	out, _ := protocol.NewEnvelope(protocol.TypeError, id, protocol.ErrorPayload{Message: msg, Code: code})
	return s.writeJSON(ctx, c, out)
}

// writeJSON enqueues a frame for the client's writeLoop. It never blocks: a
// full queue means the peer has stopped reading, and the connection is dropped
// (R5=B) so no handler or event pump is ever wedged on a dead socket.
// maxOutboundFrameBytes caps every response/event frame (MADR 0056 M-1).
const maxOutboundFrameBytes = 1 << 20 // 1 MiB

func (s *Server) writeJSON(_ context.Context, c *client, env protocol.Envelope) error {
	// Direct responses carry the connection's negotiated version
	// (MADR 0068 D1). v1 connections are untouched (env.V is already V1);
	// shared fan-out buffers bypass this via writeBytes and stay V1, which
	// the v2 accept rule permits.
	if c != nil {
		s.mu.Lock()
		v := c.negotiated
		s.mu.Unlock()
		if v > env.V {
			env.V = v
		}
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if len(b) > maxOutboundFrameBytes {
		s.log.Warn("outbound frame exceeds max size; dropping",
			slog.String("type", env.Type),
			slog.Int("bytes", len(b)),
		)
		// Prefer a typed error envelope when the oversize frame is not already
		// an error response.
		if env.Type != protocol.TypeError {
			return s.writeError(context.Background(), c, env.ID, protocol.ErrBadPayload,
				"outbound frame too large")
		}
		return fmt.Errorf("outbound frame too large: %d bytes", len(b))
	}
	// Capture response frames for in-flight mutating requests so a client
	// retry with the same id gets the real envelope (MADR 0056 H-2b).
	if s.idem != nil && c != nil && env.ID != "" {
		s.mu.Lock()
		deviceID := c.deviceID
		s.mu.Unlock()
		if deviceID != "" {
			s.idem.capture(deviceID, env.ID, b)
		}
	}
	return s.writeBytes(c, b)
}

// writeBytes enqueues an already-marshaled frame for the client's writeLoop.
// The slice is treated as read-only, so one broadcast buffer can be shared
// across every recipient (marshal once, fan out). Never blocks: a full queue
// means the peer has stopped reading, and the connection is dropped.
func (s *Server) writeBytes(c *client, b []byte) error {
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

// writeOKFrame acknowledges a command that has no result body.
func (s *Server) writeOKFrame(ctx context.Context, c *client, id string) error {
	out, _ := protocol.NewEnvelope(protocol.TypeOK, id, nil)
	return s.writeJSON(ctx, c, out)
}

// clipAuthErr bounds an agent-sourced error before it reaches the wire. No
// write path puts a credential into its error text (MADR 0074 D2), but this
// also stops an unbounded child's output becoming an error frame.
func clipAuthErr(s string) string {
	const max = 300
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
