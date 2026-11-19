package codex

import (
	"fmt"
	"net"
	"runtime"
	"strings"
)

// Names this package repeats (MADR 0131, P4).
const (
	// subcommandAppServer: the codex subcommand that serves the app-server protocol.
	subcommandAppServer = "app-server"
)

// Names this package repeats (MADR 0131, P4).
const (
	// flagListen: the app-server listen flag.
	flagListen = "--listen"
)

func launchArguments(cfg Config, endpoint, secretFile string) ([]string, error) {
	validated, err := cfg.validated()
	if err != nil {
		return nil, err
	}
	if endpoint == "off" {
		return []string{subcommandAppServer, flagListen, "off"}, nil
	}
	switch validated.Transport {
	case TransportStdio:
		return []string{subcommandAppServer, flagListen, "stdio://"}, nil
	case TransportUnixWS:
		if endpoint == "" || !strings.HasPrefix(endpoint, "/") {
			return nil, fmt.Errorf("unix_ws requires an absolute daemon-owned socket path")
		}
		return []string{subcommandAppServer, flagListen, "unix://" + endpoint}, nil
	case TransportWS:
		host, _, splitErr := net.SplitHostPort(endpoint)
		if splitErr != nil || !isLoopbackHost(host) {
			return nil, fmt.Errorf("ws endpoint must be loopback host:port")
		}
		args := []string{subcommandAppServer, flagListen, "ws://" + endpoint}
		if validated.WSAuthMode != "" {
			if secretFile == "" {
				return nil, fmt.Errorf("WebSocket auth requires a daemon-owned secret file")
			}
			switch validated.WSAuthMode {
			case WSAuthCapabilityToken:
				args = append(args, "--ws-auth", "capability-token", "--ws-token-file", secretFile)
			case WSAuthSignedBearer:
				args = append(args, "--ws-auth", "signed-bearer-token", "--ws-shared-secret-file", secretFile,
					"--ws-issuer", wsJWTIssuer, "--ws-audience", wsJWTAudience)
			}
		}
		return args, nil
	case TransportManagedDaemonProxy:
		if runtime.GOOS == goosWindows {
			// See config.go: Codex supports the managed daemon on Windows from
			// 0.155.1; we refuse it for want of live coverage, not for want of
			// platform support (MADR 0163 F20/D12).
			return nil, fmt.Errorf(
				"managed_daemon_proxy is not enabled on Windows (MADR 0163 D12)")
		}
		args := []string{subcommandAppServer, "proxy"}
		if endpoint != "" {
			args = append(args, "--sock", endpoint)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", validated.Transport)
	}
}
