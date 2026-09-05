package relay

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ParseTrustedProxies parses CIDR or bare IP strings into networks for
// MADR 0017 E1. Bare IPs become /32 (IPv4) or /128 (IPv6).
func ParseTrustedProxies(entries []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for i, raw := range entries {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("trusted_proxies[%d]: invalid IP %q", i, raw)
			}
			if ip4 := ip.To4(); ip4 != nil {
				out = append(out, &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)})
			} else {
				out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
			}
			continue
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("trusted_proxies[%d]: %w", i, err)
		}
		ones, bits := n.Mask.Size()
		switch bits {
		case 32:
			if ones < 8 {
				return nil, fmt.Errorf("trusted_proxies[%d]: IPv4 mask must be /8 or narrower", i)
			}
		case 128:
			if ones < 32 {
				return nil, fmt.Errorf("trusted_proxies[%d]: IPv6 mask must be /32 or narrower", i)
			}
		}
		out = append(out, n)
	}
	return out, nil
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// rateIPKey canonicalizes a client address for rate-limit buckets (0142 F36).
// IPv4 (including IPv4-mapped IPv6) is the dotted-quad /32. IPv6 is the /64
// prefix so one subscriber cannot mint a fresh window per ephemeral address.
func rateIPKey(host string) string {
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	v6 := ip.To16()
	if v6 == nil {
		return host
	}
	out := make(net.IP, net.IPv6len)
	copy(out, v6)
	for i := 8; i < 16; i++ {
		out[i] = 0
	}
	return out.String()
}

// clientIP returns the client address for rate limiting (MADR 0017 E1 / R36).
//
// Default (no trusted proxies): RemoteAddr only — X-Forwarded-For is ignored
// so untrusted clients cannot spoof rate-limit identity.
//
// When RemoteAddr is inside TrustedProxies, the rightmost non-trusted hop in
// X-Forwarded-For is used (then X-Real-IP if present and non-trusted).
func (s *Server) clientIP(r *http.Request) string {
	remote := remoteIP(r.RemoteAddr)
	if len(s.trustedProxies) == 0 {
		return rateIPKey(remote)
	}
	rip := net.ParseIP(remote)
	if rip == nil || !ipInNets(rip, s.trustedProxies) {
		// Direct client or untrusted hop: never honor forwarded headers.
		return rateIPKey(remote)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue
			}
			if !ipInNets(ip, s.trustedProxies) {
				return rateIPKey(ip.String())
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil && !ipInNets(ip, s.trustedProxies) {
			return rateIPKey(ip.String())
		}
	}
	return rateIPKey(remote)
}
