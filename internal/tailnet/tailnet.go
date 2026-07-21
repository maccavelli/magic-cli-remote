// Package tailnet detects the host's Tailscale addressing.
//
// It is the single source of truth for "which 100.x address is this host?" —
// used both by the daemon to resolve the listen.host "tailscale" sentinel and
// by `mcremote pair` to pick the address advertised in the pair QR.
package tailnet

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// detectTimeout bounds the `tailscale ip` exec. A wedged tailscaled socket can
// hang the CLI indefinitely, and this runs on the daemon startup path — under
// systemd that would present as a silent startup hang.
const detectTimeout = 5 * time.Second

// IPv4 returns the host's Tailscale IPv4 address, or "" when Tailscale is not
// installed, not running, or has no IPv4 assigned.
//
// It is a variable so tests can stub the detection without a tailnet.
var IPv4 = detectIPv4
var execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func detectIPv4() string {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	cmd := execCommand(ctx, "tailscale", "ip", "-4")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return ""
	}
	// Multi-address output: the first line is the primary IPv4.
	if i := strings.IndexByte(ip, '\n'); i >= 0 {
		ip = strings.TrimSpace(ip[:i])
	}
	return ip
}
