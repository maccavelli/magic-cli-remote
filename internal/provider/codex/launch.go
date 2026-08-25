package codex

import (
	"fmt"
	"net"
	"runtime"
	"strings"
)

func launchArguments(cfg Config, endpoint, secretFile string) ([]string, error) {
	validated, err := cfg.validated()
	if err != nil {
		return nil, err
	}
	if endpoint == "off" {
		return []string{"app-server", "--listen", "off"}, nil
	}
	switch validated.Transport {
	case TransportStdio:
		return []string{"app-server", "--listen", "stdio://"}, nil
	case TransportUnixWS:
		if endpoint == "" || !strings.HasPrefix(endpoint, "/") {
			return nil, fmt.Errorf("unix_ws requires an absolute daemon-owned socket path")
		}
		return []string{"app-server", "--listen", "unix://" + endpoint}, nil
	case TransportWS:
		host, _, splitErr := net.SplitHostPort(endpoint)
		if splitErr != nil || !isLoopbackHost(host) {
			return nil, fmt.Errorf("ws endpoint must be loopback host:port")
		}
		args := []string{"app-server", "--listen", "ws://" + endpoint}
		if validated.WSAuthMode != "" {
			if secretFile == "" {
				return nil, fmt.Errorf("WebSocket auth requires a daemon-owned secret file")
			}
			switch validated.WSAuthMode {
			case WSAuthCapabilityToken:
				args = append(args, "--ws-auth", "capability-token", "--ws-token-file", secretFile)
			case WSAuthSignedBearer:
				args = append(args, "--ws-auth", "signed-bearer-token", "--ws-shared-secret-file", secretFile,
					"--ws-issuer", "mcremote", "--ws-audience", "codex-app-server")
			}
		}
		return args, nil
	case TransportManagedDaemonProxy:
		if runtime.GOOS == "windows" {
			return nil, fmt.Errorf("managed_daemon_proxy is Unix-only")
		}
		args := []string{"app-server", "proxy"}
		if endpoint != "" {
			args = append(args, "--sock", endpoint)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", validated.Transport)
	}
}
