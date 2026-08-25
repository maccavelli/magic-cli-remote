// Package relay implements mcrelay: the join-plane router and opaque splice
// between phones and registered mcremote hosts (MADR 0015).
package relay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
)

// Version is the protocol version for join-plane envelopes (not protocol-v1
// session traffic).
const Version = 1

// Join-plane message types.
const (
	TypeRegister      = "register"
	TypeRegisterOK    = "register_ok"
	TypeRegisterError = "register_error"
	TypeJoin          = "join"
	TypeJoinOK        = "join_ok"
	TypeJoinError     = "join_error"
	TypeDial          = "dial"   // server → host: open a tunnel for session_id
	TypeTunnel        = "tunnel" // host → server on /v1/tunnel: claim session
	TypeTunnelOK      = "tunnel_ok"
	TypeTunnelError   = "tunnel_error"
	TypeError         = "error"
)

// Envelope is the join-plane JSON wrapper (text frames only, before splice).
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RegisterPayload is sent by mcremote on /v1/host.
type RegisterPayload struct {
	HostID string `json:"host_id"`
	Secret string `json:"secret"`
}

// JoinPayload is sent by the phone on /v1/phone.
type JoinPayload struct {
	HostID string `json:"host_id"`
}

// SessionPayload carries a session id after join/dial.
// TunnelToken is a short-lived single-use claim for /v1/tunnel (MADR 0016 R12);
// hosts must not re-send the long-lived registration secret on every dial.
type SessionPayload struct {
	SessionID   string `json:"session_id"`
	HostID      string `json:"host_id,omitempty"`
	TunnelToken string `json:"tunnel_token,omitempty"`
}

// TunnelPayload claims a pending phone session (host → relay on /v1/tunnel).
// Prefer Token (from dial) over Secret (legacy); new hosts send Token only.
type TunnelPayload struct {
	SessionID string `json:"session_id"`
	HostID    string `json:"host_id"`
	// Token is the short-lived claim from TypeDial (R12).
	Token string `json:"token,omitempty"`
	// Secret is the long-lived registration secret (legacy fallback).
	Secret string `json:"secret,omitempty"`
}

// ErrorPayload is a typed join-plane error.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	// RetryAfterMS hints when a refused peer may usefully try again
	// (0068 P6): the rate-limit window remainder for `rate_limited`, a
	// fixed courtesy delay for capacity `limit`. Advisory — the server
	// still enforces its limits regardless of client behaviour.
	RetryAfterMS int64 `json:"retry_after_ms,omitempty"`
}

// NewEnvelope builds a versioned envelope.
func NewEnvelope(typ, id string, payload any) (Envelope, error) {
	env := Envelope{V: Version, Type: typ, ID: id}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		env.Payload = b
	}
	return env, nil
}

// DecodePayload unmarshals env.Payload into dest.
func DecodePayload(env Envelope, dest any) error {
	if len(env.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(env.Payload, dest)
}

// WriteEnvelope / ReadEnvelope are the canonical join-plane frame I/O for
// both mcrelay and relayhost (0115 F14). ReadEnvelope enforces text framing
// and rejects unsupported envelope versions.
func WriteEnvelope(ctx context.Context, c *websocket.Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

// ReadEnvelope reads one join-plane envelope. Callers bound the wait through
// ctx; the connection's read limit bounds the size.
func ReadEnvelope(ctx context.Context, c *websocket.Conn) (Envelope, error) {
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
