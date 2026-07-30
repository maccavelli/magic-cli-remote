// Package relayhost is the mcremote-side client for mcrelay (MADR 0015 E2):
// outbound register on /v1/host, and per-session tunnels on /v1/tunnel bridged
// to the local control-plane TCP listener.
package relayhost

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// bridgeBufSize matches mcrelay splice CopyBuffer (MADR 0017 E2).
const bridgeBufSize = 32 * 1024

// Default max WebSocket frame from mcrelay splice (1 MiB default limit).
const bridgeWSReadLimit = 1 << 20

var bridgeBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, bridgeBufSize)
		return &b
	},
}

// Config drives the host registration client.
type Config struct {
	// URL is the mcrelay base (wss://host[:port]).
	URL string
	// HostID / Secret match mcrelay's allowlist.
	HostID string
	Secret string
	// LocalAddr is the mcremote TCP listen address to bridge tunnels into
	// (typically the resolved listener, rewritten to loopback when needed).
	LocalAddr string
	// InsecureSkipVerify skips verification of the relay TLS certificate.
	InsecureSkipVerify bool
}

// Client maintains registration and opens tunnels on dial.
type Client struct {
	cfg Config
	log *slog.Logger

	mu       sync.Mutex
	local    string // may be updated after listen
	tunnels  sync.WaitGroup
	rootCtx  context.Context
	rootStop context.CancelFunc
}

// New returns a client. Call [Client.Run] in a goroutine.
func New(cfg Config, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		cfg:   cfg,
		log:   log.With(slog.String("component", "relayhost")),
		local: cfg.LocalAddr,
	}
}

// SetLocalAddr updates the bridge target after the daemon has bound its listener.
func (c *Client) SetLocalAddr(addr string) {
	c.mu.Lock()
	c.local = loopbackAddr(addr)
	c.mu.Unlock()
}

func (c *Client) localAddr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.local
}

// Run registers and reconnects until ctx is cancelled.
// Backoff resets after each successful register_ok (MADR 0016 R3 / D3).
// Tunnels are bound to this Run's lifecycle (MADR 0056 M-5): they may outlive
// one control reconnect but not daemon shutdown / Run return.
func (c *Client) Run(ctx context.Context) error {
	root, stop := context.WithCancel(ctx)
	c.mu.Lock()
	c.rootCtx = root
	c.rootStop = stop
	c.mu.Unlock()
	defer func() {
		stop()
		// Drain active tunnels with a bounded wait.
		done := make(chan struct{})
		go func() {
			c.tunnels.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			c.log.Warn("relayhost tunnel drain timed out")
		}
	}()

	backoff := time.Second
	for {
		if root.Err() != nil {
			return root.Err()
		}
		err := c.session(root, &backoff)
		if root.Err() != nil {
			return root.Err()
		}
		c.log.Warn("relay registration ended; reconnecting",
			slog.String("err", errString(err)),
			slog.Duration("backoff", backoff),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// session dials, registers, and reads control until the connection drops.
// On register_ok it sets *backoff to 1s so the next reconnect is prompt.
func (c *Client) session(ctx context.Context, backoff *time.Duration) error {
	base, err := normalizeRelayURL(c.cfg.URL)
	if err != nil {
		return err
	}
	hostURL := base + "/v1/host"
	conn, _, err := websocket.Dial(ctx, hostURL, &websocket.DialOptions{
		HTTPClient: c.httpClient(),
	})
	if err != nil {
		return fmt.Errorf("dial host control: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	reg, err := relay.NewEnvelope(relay.TypeRegister, "1", relay.RegisterPayload{
		HostID: c.cfg.HostID,
		Secret: c.cfg.Secret,
	})
	if err != nil {
		return err
	}
	if err := writeEnv(ctx, conn, reg); err != nil {
		return err
	}
	env, err := readEnv(ctx, conn)
	if err != nil {
		return err
	}
	if env.Type != relay.TypeRegisterOK {
		var ep relay.ErrorPayload
		_ = relay.DecodePayload(env, &ep)
		return fmt.Errorf("register failed type=%s code=%s msg=%s", env.Type, ep.Code, ep.Message)
	}
	// Successful registration: next failure should retry quickly (MADR 0016 R3).
	if backoff != nil {
		*backoff = time.Second
	}
	c.log.Info("registered with mcrelay",
		slog.String("host_id", c.cfg.HostID),
		slog.String("relay", base),
	)

	for {
		env, err := readEnv(ctx, conn)
		if err != nil {
			return err
		}
		switch env.Type {
		case relay.TypeDial:
			var sess relay.SessionPayload
			if err := relay.DecodePayload(env, &sess); err != nil {
				c.log.Warn("bad dial payload", slog.String("err", err.Error()))
				continue
			}
			if sess.SessionID == "" {
				continue
			}
			// Use root lifecycle ctx so tunnels survive control reconnect but
			// not daemon shutdown (MADR 0056 M-5).
			c.mu.Lock()
			tctx := c.rootCtx
			c.mu.Unlock()
			if tctx == nil {
				tctx = ctx
			}
			c.tunnels.Add(1)
			go func(sessionID, token string) {
				defer c.tunnels.Done()
				c.openTunnel(tctx, base, sessionID, token)
			}(sess.SessionID, sess.TunnelToken)
		default:
			// Ignore unknown control frames (forward-compatible).
			c.log.Debug("relay control frame", slog.String("type", env.Type))
		}
	}
}

func (c *Client) openTunnel(ctx context.Context, base, sessionID, tunnelToken string) {
	log := c.log.With(slog.String("session_id", sessionID))
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tunURL := base + "/v1/tunnel"
	conn, _, err := websocket.Dial(tctx, tunURL, &websocket.DialOptions{
		HTTPClient: c.httpClient(),
	})
	if err != nil {
		log.Warn("tunnel dial failed", slog.String("err", err.Error()))
		return
	}
	// After auth, bridge runs until either side closes; use parent ctx.
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Prefer short-lived dial token (MADR 0016 R12); fall back to registration
	// secret only if the relay omitted the token (older mcrelay).
	payload := relay.TunnelPayload{
		SessionID: sessionID,
		HostID:    c.cfg.HostID,
		Token:     tunnelToken,
	}
	if tunnelToken == "" {
		payload.Secret = c.cfg.Secret
	}
	tenv, err := relay.NewEnvelope(relay.TypeTunnel, "t", payload)
	if err != nil {
		return
	}
	if err := writeEnv(tctx, conn, tenv); err != nil {
		log.Warn("tunnel auth write failed", slog.String("err", err.Error()))
		return
	}
	resp, err := readEnv(tctx, conn)
	if err != nil {
		log.Warn("tunnel auth read failed", slog.String("err", err.Error()))
		return
	}
	if resp.Type != relay.TypeTunnelOK {
		var ep relay.ErrorPayload
		_ = relay.DecodePayload(resp, &ep)
		log.Warn("tunnel rejected", slog.String("type", resp.Type), slog.String("code", ep.Code))
		return
	}

	localAddr := c.localAddr()
	if localAddr == "" {
		log.Warn("tunnel open but local listen addr unknown")
		return
	}
	local, err := net.DialTimeout("tcp", localAddr, 10*time.Second)
	if err != nil {
		log.Warn("local dial failed", slog.String("addr", localAddr), slog.String("err", err.Error()))
		return
	}
	defer local.Close()

	log.Info("tunnel bridged to local listener", slog.String("local", localAddr))
	bridge(ctx, conn, local, log)
}

// bridge copies WebSocket binary/text frames ↔ TCP bytes.
// Text frames are written as-is to TCP (HTTP/WS upgrade); binary likewise.
// WS→TCP uses Reader + pooled CopyBuffer (MADR 0017 E2 host side).
func bridge(ctx context.Context, wsConn *websocket.Conn, tcp net.Conn, log *slog.Logger) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wsConn.SetReadLimit(int64(bridgeWSReadLimit))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("tcp-to-ws bridge panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				cancel()
			}
		}()
		defer wg.Done()
		defer cancel()
		bufPtr := bridgeBufPool.Get().(*[]byte)
		buf := *bufPtr
		defer bridgeBufPool.Put(bufPtr)
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				// Prefer binary for opaque TLS records; HTTP upgrade is text-safe too.
				if werr := wsConn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("ws-to-tcp bridge panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				cancel()
			}
		}()
		defer wg.Done()
		defer cancel()
		bufPtr := bridgeBufPool.Get().(*[]byte)
		buf := *bufPtr
		defer bridgeBufPool.Put(bufPtr)
		for {
			_, r, err := wsConn.Reader(ctx)
			if err != nil {
				return
			}
			if _, err := io.CopyBuffer(tcp, r, buf); err != nil {
				return
			}
		}
	}()
	wg.Wait()
	_ = tcp.Close()
	_ = wsConn.Close(websocket.StatusNormalClosure, "")
}

func (c *Client) httpClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if c.cfg.InsecureSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit dev flag
	}
	return &http.Client{Transport: tr}
}

func normalizeRelayURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty relay url")
	}
	if !strings.Contains(raw, "://") {
		raw = "wss://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "ws", "wss", "http", "https":
	default:
		return "", fmt.Errorf("relay url scheme must be ws(s) or http(s)")
	}
	// Normalize http(s) → ws(s)
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	// Base URL only: paths are appended by the client (/v1/host, /v1/tunnel).
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	if u.Host == "" {
		return "", fmt.Errorf("relay url missing host")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// loopbackAddr rewrites wildcard binds so the host can dial itself.
func loopbackAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func writeEnv(ctx context.Context, c *websocket.Conn, env relay.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func readEnv(ctx context.Context, c *websocket.Conn) (relay.Envelope, error) {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return relay.Envelope{}, err
	}
	if typ != websocket.MessageText {
		return relay.Envelope{}, fmt.Errorf("expected text frame, got %v", typ)
	}
	var env relay.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return relay.Envelope{}, err
	}
	return env, nil
}

func errString(err error) string {
	if err == nil {
		return "eof"
	}
	return err.Error()
}
