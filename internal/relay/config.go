package relay

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"strings"
	"time"
)

// Limits bound public-edge DoS (MADR 0015 S10 / H3).
type Limits struct {
	MaxHosts          int
	MaxPhonesPerHost  int
	MaxMessageBytes   int
	MaxConcurrentJoin int // pending joins waiting for host tunnel
	AcceptPerMinute   int // pre-auth WS upgrades per remote IP (all paths)
	// JoinPerMinute is per-IP join attempts (MADR 0016 R16).
	JoinPerMinute int
	// RegisterPerMinute is per-IP register attempts (R16).
	RegisterPerMinute int
	// JoinPerHostPerMinute caps joins for one host_id across all IPs (H3 / R10).
	JoinPerHostPerMinute int
	TunnelWait           time.Duration
	RegisterIdle         time.Duration // host-control app ping interval (MADR 0016 R5)
	// SpliceIdle ends an opaque splice after this much silence (MADR 0016 R15).
	// Zero after ResolvedLimits means default; negative disables idle kill.
	SpliceIdle time.Duration
	// SpliceMax is the absolute max lifetime of a splice (R15). Zero → default.
	SpliceMax time.Duration
}

// DefaultLimits returns production-leaning defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxHosts:             32,
		MaxPhonesPerHost:     8,
		MaxMessageBytes:      1 << 20, // 1 MiB
		MaxConcurrentJoin:    64,
		AcceptPerMinute:      120,
		JoinPerMinute:        30,
		RegisterPerMinute:    20,
		JoinPerHostPerMinute: 60,
		TunnelWait:           15 * time.Second,
		RegisterIdle:         30 * time.Second,
		SpliceIdle:           5 * time.Minute,
		SpliceMax:            12 * time.Hour,
	}
}

// ResolvedLimits fills zero fields from DefaultLimits without wiping
// operator-set non-zero fields (MADR 0016 R4 / D2).
// Negative SpliceIdle / SpliceMax means "disabled" and is preserved.
func ResolvedLimits(l Limits) Limits {
	d := DefaultLimits()
	if l.MaxHosts <= 0 {
		l.MaxHosts = d.MaxHosts
	}
	if l.MaxPhonesPerHost <= 0 {
		l.MaxPhonesPerHost = d.MaxPhonesPerHost
	}
	if l.MaxMessageBytes <= 0 {
		l.MaxMessageBytes = d.MaxMessageBytes
	}
	if l.MaxConcurrentJoin <= 0 {
		l.MaxConcurrentJoin = d.MaxConcurrentJoin
	}
	if l.AcceptPerMinute <= 0 {
		l.AcceptPerMinute = d.AcceptPerMinute
	}
	if l.JoinPerMinute <= 0 {
		l.JoinPerMinute = d.JoinPerMinute
	}
	if l.RegisterPerMinute <= 0 {
		l.RegisterPerMinute = d.RegisterPerMinute
	}
	if l.JoinPerHostPerMinute <= 0 {
		l.JoinPerHostPerMinute = d.JoinPerHostPerMinute
	}
	if l.TunnelWait <= 0 {
		l.TunnelWait = d.TunnelWait
	}
	if l.RegisterIdle <= 0 {
		l.RegisterIdle = d.RegisterIdle
	}
	if l.SpliceIdle == 0 {
		l.SpliceIdle = d.SpliceIdle
	}
	if l.SpliceMax == 0 {
		l.SpliceMax = d.SpliceMax
	}
	return l
}

// HostCredential is one allowed host registration (secret held as SHA-256).
type HostCredential struct {
	HostID     string
	SecretHash [32]byte
}

// Config configures the mcrelay server.
type Config struct {
	// ListenAddr is host:port, e.g. "0.0.0.0:8443" or "0.0.0.0:443".
	ListenAddr string
	// TLSCertFile / TLSKeyFile enable file-based TLS. Both empty and TLSConfig
	// nil → plaintext (tests / local only).
	TLSCertFile string
	TLSKeyFile  string
	// TLSConfig is an optional managed config (ACME HTTP-01). Takes precedence
	// over TLSCertFile/TLSKeyFile when non-nil.
	TLSConfig *tls.Config
	// Allow is the set of host_id → secret hash allowed to register.
	Allow  []HostCredential
	Limits Limits
}

// ParseAllowFlag parses "host_id:secret" into a credential.
func ParseAllowFlag(s string) (HostCredential, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return HostCredential{}, fmt.Errorf("allow: want host_id:secret, got %q", s)
	}
	id := strings.TrimSpace(s[:i])
	secret := s[i+1:]
	if err := validateHostID(id); err != nil {
		return HostCredential{}, err
	}
	if len(secret) < 16 {
		return HostCredential{}, fmt.Errorf("allow: secret for %q too short (min 16)", id)
	}
	return HostCredential{HostID: id, SecretHash: HashSecret(secret)}, nil
}

// HashSecret returns SHA-256 of the registration secret.
func HashSecret(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}

func validateHostID(id string) error {
	if id == "" {
		return fmt.Errorf("empty host_id")
	}
	if len(id) > 128 {
		return fmt.Errorf("host_id too long")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("host_id has invalid character %q", r)
		}
	}
	return nil
}
