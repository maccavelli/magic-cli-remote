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
type SessionPayload struct {
	SessionID string `json:"session_id"`
	HostID    string `json:"host_id,omitempty"`
}

// TunnelPayload claims a pending phone session (host → relay on /v1/tunnel).
type TunnelPayload struct {
	SessionID string `json:"session_id"`
	HostID    string `json:"host_id"`
	Secret    string `json:"secret"`
}

// ErrorPayload is a typed join-plane error.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
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
