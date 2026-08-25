package codex

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TransportMode selects the daemon-owned Codex app-server transport.
type TransportMode string

const (
	// TransportStdio is the default supervised JSONL child transport.
	TransportStdio TransportMode = "stdio"
	// TransportUnixWS uses WebSocket framing over a private Unix socket.
	TransportUnixWS TransportMode = "unix_ws"
	// TransportWS uses an authenticated loopback WebSocket listener.
	TransportWS TransportMode = "ws"
	// TransportManagedDaemonProxy leases Codex's Unix daemon and proxies it.
	TransportManagedDaemonProxy TransportMode = "managed_daemon_proxy"
)

// WSAuthMode selects Codex's native WebSocket bearer verifier.
type WSAuthMode string

const (
	// WSAuthCapabilityToken uses an opaque bearer stored in a private file.
	WSAuthCapabilityToken WSAuthMode = "capability_token"
	// WSAuthSignedBearer uses a short-lived HS256 bearer token.
	WSAuthSignedBearer WSAuthMode = "signed_bearer"
)

const (
	defaultPermissionTimeout = 900 * time.Second
	defaultStreamCoalesce    = 80 * time.Millisecond
	maxPendingChunkBytes     = 8 << 10
	engineStartTimeout       = 60 * time.Second
	engineStopTimeout        = 5 * time.Second
)

// Config holds user-supplied options for the Codex provider.
type Config struct {
	Bin               string
	AlwaysApprove     bool
	DefaultCWD        string
	Model             string
	PermissionTimeout time.Duration
	Prewarm           bool
	TurnStallNotice   time.Duration
	StreamCoalesce    *time.Duration
	ApprovalPolicy    string
	SandboxMode       string
	// AllowFullAccess advertises the "full-access" session mode (no approval
	// prompts and no sandbox). Off by default (MADR 0044 D5).
	AllowFullAccess bool
	// SandboxBrokenPolicy controls create behaviour when the Linux sandbox
	// cannot create a user namespace (MADR 0048). Valid: warn (default),
	// require_full_access, refuse.
	SandboxBrokenPolicy string
	Transport           TransportMode
	ListenAddress       string
	WSAuthMode          WSAuthMode
	ReconnectAttempts   int
	// ReconnectAttemptsConfigured distinguishes an explicit zero (disable
	// automatic replacement) from a directly constructed Config's zero value.
	ReconnectAttemptsConfigured   bool
	RuntimeDir                    string
	Environments                  []provider.ExecutionEnvironment
	StandaloneProcessesEnabled    bool
	StandaloneProcessEnvAllowlist []string
}

func (c Config) validated() (Config, error) {
	if c.Transport == "" {
		c.Transport = TransportStdio
	}
	if c.ReconnectAttempts == 0 && !c.ReconnectAttemptsConfigured {
		c.ReconnectAttempts = 3
	}
	if c.ReconnectAttempts < 0 || c.ReconnectAttempts > 3 {
		return Config{}, fmt.Errorf("reconnect_attempts must be 0..3")
	}
	switch c.Transport {
	case TransportStdio:
		if c.ListenAddress != "" || c.WSAuthMode != "" {
			return Config{}, fmt.Errorf("stdio transport rejects listen/auth fields")
		}
	case TransportUnixWS:
		if runtime.GOOS == "windows" {
			return Config{}, fmt.Errorf("unix_ws is unavailable on Windows")
		}
		if c.WSAuthMode != "" {
			return Config{}, fmt.Errorf("unix_ws does not use WebSocket bearer auth")
		}
		if c.ListenAddress != "" {
			if c.RuntimeDir == "" || !pathWithin(c.RuntimeDir, c.ListenAddress) {
				return Config{}, fmt.Errorf("unix_ws socket must be inside the daemon runtime directory")
			}
		}
	case TransportWS:
		if c.ListenAddress != "" {
			host, _, err := net.SplitHostPort(c.ListenAddress)
			if err != nil || !isLoopbackHost(host) {
				return Config{}, fmt.Errorf("ws listen address must be loopback host:port")
			}
		}
		if c.WSAuthMode != "" && c.WSAuthMode != WSAuthCapabilityToken && c.WSAuthMode != WSAuthSignedBearer {
			return Config{}, fmt.Errorf("unknown WebSocket auth mode %q", c.WSAuthMode)
		}
	case TransportManagedDaemonProxy:
		if runtime.GOOS == "windows" {
			return Config{}, fmt.Errorf("managed_daemon_proxy is Unix-only")
		}
		if c.ListenAddress != "" || c.WSAuthMode != "" {
			return Config{}, fmt.Errorf("managed daemon proxy owns its endpoint and auth")
		}
	default:
		return Config{}, fmt.Errorf("unknown Codex transport %q", c.Transport)
	}
	if err := validateExecutionConfig(c.Environments, c.StandaloneProcessEnvAllowlist); err != nil {
		return Config{}, err
	}
	return c, nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func pathWithin(root, path string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (c Config) streamCoalesceWindow() time.Duration {
	if c.StreamCoalesce == nil {
		return defaultStreamCoalesce
	}
	return *c.StreamCoalesce
}
