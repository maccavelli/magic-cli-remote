package cli

import "testing"

func TestPairAdvertisesTheBoundHost(t *testing.T) {
	// A daemon listening on loopback that advertised its tailnet address
	// printed a QR no phone could dial: connections to the advertised tailnet
	// address were refused while the loopback address answered (MADR 0138 F11).
	// 100.64.7.7 is an example address in the tailnet (CGNAT) range, not a
	// real host.
	for _, tc := range []struct {
		name       string
		listenHost string
		want       string
	}{
		{"loopback ipv4", "127.0.0.1", "127.0.0.1:7531"},
		{"loopback ipv6", "::1", "[::1]:7531"},
		{"a specific lan address", "192.168.1.40", "192.168.1.40:7531"},
		{"a specific tailnet address", "100.64.7.7", "100.64.7.7:7531"},
		{"a hostname", "devbox.local", "devbox.local:7531"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectAdvertiseHost(tc.listenHost, 7531); got != tc.want {
				t.Fatalf("detectAdvertiseHost(%q) = %q, want %q — a QR must name an address the daemon listens on",
					tc.listenHost, got, tc.want)
			}
		})
	}
}

func TestPairStillAutoDetectsForFollowTheConfigBinds(t *testing.T) {
	// "tailscale", empty and the wildcards mean "work it out", and their
	// existing behaviour — Tailscale IPv4, else loopback — is unchanged. The
	// detection is environment-dependent, so this asserts that a plausible
	// address comes back rather than which one.
	for _, host := range []string{"", "tailscale", "0.0.0.0", "::"} {
		got := detectAdvertiseHost(host, 7531)
		if got == "" {
			t.Fatalf("detectAdvertiseHost(%q) returned nothing", host)
		}
		// Whatever it picked, it must carry the port and must not be the
		// literal bind spec.
		if got == host+":7531" && host != "" {
			t.Fatalf("detectAdvertiseHost(%q) = %q; a wildcard bind is not a dialable address", host, got)
		}
	}
}
