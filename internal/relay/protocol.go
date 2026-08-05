// Package relay implements mcrelay: the join-plane router and opaque splice
// between phones and registered mcremote hosts (MADR 0015).
package relay

import "encoding/json"

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
