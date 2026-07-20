// Package tailnet detects the host's Tailscale addressing.
//
// It is the single source of truth for "which 100.x address is this host?" —
// used both by the daemon to resolve the listen.host "tailscale" sentinel and
// by `mcremote pair` to pick the address advertised in the pair QR.
package tailnet

import (
	"os/exec"
	"strings"
)

// IPv4 returns the host's Tailscale IPv4 address, or "" when Tailscale is not
// installed, not running, or has no IPv4 assigned.
//
// It is a variable so tests can stub the detection without a tailnet.
var IPv4 = detectIPv4
var execCommand = exec.Command

func detectIPv4() string {
	out, err := execCommand("tailscale", "ip", "-4").Output()
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
