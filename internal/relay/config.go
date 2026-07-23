package relay

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// Limits bound public-edge DoS (MADR 0015 S10).
type Limits struct {
	MaxHosts          int
	MaxPhonesPerHost  int
	MaxMessageBytes   int
	MaxConcurrentJoin int // pending joins waiting for host tunnel
	AcceptPerMinute   int // pre-auth WS upgrades per remote IP
	TunnelWait        time.Duration
	RegisterIdle      time.Duration // ping host control if quiet
}

// DefaultLimits returns production-leaning defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxHosts:          32,
		MaxPhonesPerHost:  8,
		MaxMessageBytes:   1 << 20, // 1 MiB
		MaxConcurrentJoin: 64,
		AcceptPerMinute:   120,
		TunnelWait:        15 * time.Second,
		RegisterIdle:      30 * time.Second,
	}
}

// HostCredential is one allowed host registration (secret held as SHA-256).
type HostCredential struct {
	HostID     string
	SecretHash [32]byte
}

// Config configures the mcrelay server.
type Config struct {
	// ListenAddr is host:port, e.g. "0.0.0.0:8443".
	ListenAddr string
	// TLSCertFile / TLSKeyFile enable TLS. Both empty → plaintext (tests / local only).
	TLSCertFile string
	TLSKeyFile  string
	// Allow is the set of host_id → secret hash allowed to register.
	Allow []HostCredential
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
