package relay

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

// Limit ceilings (MADR 0017 D9). Defaults stay far below these.
const (
	MaxLimitHosts           = 1024
	MaxLimitPhonesPerHost   = 256
	MaxLimitMessageBytes    = 16 << 20 // 16 MiB
	MaxLimitConcurrentJoin  = 4096
	MaxLimitConns           = 8192
	MaxLimitPerMinute       = 100_000
	MaxLimitDurationSeconds = 7 * 24 * 3600 // 7 days
	ControlReadLimitBytes   = 64 << 10      // 64 KiB host-control frames (D16)
	MaxSessionIDLen         = 64
	LogHostIDMaxRunes       = 64
)

// Limits bound public-edge DoS (MADR 0015 S10 / H3).
type Limits struct {
	MaxHosts          int
	MaxPhonesPerHost  int
	MaxMessageBytes   int
	MaxConcurrentJoin int // pending joins waiting for host tunnel
	// MaxConns caps concurrent accepted TCP connections (0142 F19).
	MaxConns        int
	AcceptPerMinute int // pre-auth WS upgrades per remote IP (all paths)
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
		MaxConns:             1024,
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
// operator-set non-zero fields (MADR 0016 R4 / D2), then clamps to ceilings
// (MADR 0017 D9).
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
	if l.MaxConns <= 0 {
		l.MaxConns = d.MaxConns
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
	return ClampLimits(l)
}

// ClampLimits enforces MADR 0017 D9 ceilings (defense in depth for Config
// constructed outside FileConfig.Validate).
func ClampLimits(l Limits) Limits {
	l.MaxHosts = clampInt(l.MaxHosts, 1, MaxLimitHosts)
	l.MaxPhonesPerHost = clampInt(l.MaxPhonesPerHost, 1, MaxLimitPhonesPerHost)
	l.MaxMessageBytes = clampInt(l.MaxMessageBytes, 1, MaxLimitMessageBytes)
	l.MaxConcurrentJoin = clampInt(l.MaxConcurrentJoin, 1, MaxLimitConcurrentJoin)
	l.MaxConns = clampInt(l.MaxConns, 1, MaxLimitConns)
	l.AcceptPerMinute = clampInt(l.AcceptPerMinute, 1, MaxLimitPerMinute)
	l.JoinPerMinute = clampInt(l.JoinPerMinute, 1, MaxLimitPerMinute)
	l.RegisterPerMinute = clampInt(l.RegisterPerMinute, 1, MaxLimitPerMinute)
	l.JoinPerHostPerMinute = clampInt(l.JoinPerHostPerMinute, 1, MaxLimitPerMinute)
	maxDur := time.Duration(MaxLimitDurationSeconds) * time.Second
	if l.TunnelWait > maxDur {
		l.TunnelWait = maxDur
	}
	if l.RegisterIdle > maxDur {
		l.RegisterIdle = maxDur
	}
	// Negative idle/max = disabled; only clamp positive overshoots.
	if l.SpliceIdle > maxDur {
		l.SpliceIdle = maxDur
	}
	if l.SpliceMax > maxDur {
		l.SpliceMax = maxDur
	}
	return l
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
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
	Allow []HostCredential
	// AllowLegacyTunnelSecret permits /v1/tunnel claims with the long-lived
	// registration secret when the dial token is omitted (MADR 0017 D13).
	// Default false — hosts must use the short-lived tunnel_token from dial.
	AllowLegacyTunnelSecret bool
	// TrustedProxies are CIDRs (or bare IPs) of reverse proxies that may set
	// X-Forwarded-For / X-Real-IP (MADR 0017 E1). Empty → RemoteAddr only;
	// forwarded headers are ignored (safe default for a public edge).
	TrustedProxies []*net.IPNet
	Limits         Limits
	// AllowPlaintext permits a non-loopback listen with no TLS (0091 D5).
	// Tests and --allow-plaintext set this; production must use TLS.
	AllowPlaintext bool
}

// parseAllowParts splits one "host_id:secret" entry. It is the single parse
// for --allow and MCRELAY_HOSTS (0115 F3): the whole entry is trimmed once,
// so surrounding whitespace — easy to pick up in a systemd Environment= line —
// can never end up hashed into the stored secret while a differently-sliced
// copy passed validation.
func parseAllowParts(s string) (id, secret string, err error) {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return "", "", fmt.Errorf("allow: want host_id:secret")
	}
	id = strings.TrimSpace(s[:i])
	secret = s[i+1:]
	if err := validateHostID(id); err != nil {
		return "", "", err
	}
	if len(secret) < 16 {
		return "", "", fmt.Errorf("allow: secret for %q too short (min 16)", id)
	}
	return id, secret, nil
}

// ParseAllowFlag parses "host_id:secret" into a credential.
func ParseAllowFlag(s string) (HostCredential, error) {
	id, secret, err := parseAllowParts(s)
	if err != nil {
		return HostCredential{}, err
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

// validateSessionID bounds tunnel session_id (MADR 0017 R29 / A2).
// Accepts UUID-shaped and other short hex/dash ids from the join plane.
func validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("empty session_id")
	}
	if len(id) > MaxSessionIDLen {
		return fmt.Errorf("session_id too long")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("session_id has invalid character %q", r)
		}
	}
	return nil
}

// slogHostID truncates attacker-controlled host_id for logs (MADR 0017 R35).
func slogHostID(id string) string {
	if id == "" {
		return ""
	}
	var b strings.Builder
	n := 0
	truncated := false
	for _, r := range id {
		if n >= LogHostIDMaxRunes {
			truncated = true
			break
		}
		if r < 32 || r == 0x7f {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
		n++
	}
	if truncated {
		b.WriteString("…")
	}
	return b.String()
}
