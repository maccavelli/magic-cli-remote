package pairuri_test

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/pairuri"
)

func TestEncodeParseRoundTrip(t *testing.T) {
	raw, err := pairuri.Encode(pairuri.Payload{
		Host:  "100.64.0.1:7531",
		Token: "mcr_deadbeef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "mcremote://pair?") {
		t.Fatalf("uri=%q", raw)
	}
	p, err := pairuri.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "100.64.0.1:7531" || p.Token != "mcr_deadbeef" {
		t.Fatalf("got %+v", p)
	}
	if p.HasRelay() {
		t.Fatal("expected no relay")
	}
}

func TestEncodeParseRelayAndHostID(t *testing.T) {
	raw, err := pairuri.Encode(pairuri.Payload{
		Host:   "wss://100.64.0.1:7531",
		Code:   "K7M29X4P",
		Relay:  "relay.example.com",
		HostID: "devbox-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := pairuri.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasRelay() {
		t.Fatal("expected relay")
	}
	if p.Relay != "wss://relay.example.com" {
		t.Fatalf("relay=%q", p.Relay)
	}
	if p.HostID != "devbox-1" {
		t.Fatalf("hid=%q", p.HostID)
	}
	// Host remains mcremote identity peer.
	if p.Host != "wss://100.64.0.1:7531" {
		t.Fatalf("host=%q", p.Host)
	}
}

func TestRelayHidMustPair(t *testing.T) {
	if _, err := pairuri.Encode(pairuri.Payload{
		Host:  "h:1",
		Token: "mcr_x",
		Relay: "wss://r",
	}); err == nil {
		t.Fatal("expected error for relay without hid")
	}
	if _, err := pairuri.Parse("mcremote://pair?host=h%3A1&token=mcr_x&relay=wss%3A%2F%2Fr"); err == nil {
		t.Fatal("expected parse error for relay without hid")
	}
	if _, err := pairuri.Encode(pairuri.Payload{
		Host:   "h:1",
		Token:  "mcr_x",
		HostID: "bad id!",
		Relay:  "wss://r",
	}); err == nil {
		t.Fatal("expected invalid hid")
	}
}

func TestEncodeParseCode(t *testing.T) {
	raw, err := pairuri.Encode(pairuri.Payload{
		Host: "100.64.0.1:7531",
		Code: "K7M2-9X4P",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := pairuri.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "K7M2-9X4P" || p.Token != "" {
		t.Fatalf("got %+v", p)
	}
}

func TestEncodeStripsPathKeepsScheme(t *testing.T) {
	raw, err := pairuri.Encode(pairuri.Payload{
		Host:  "ws://100.64.0.1:7531/v1/ws",
		Token: "mcr_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := pairuri.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	// The path goes, but the transport must survive: dropping ws:// here would
	// silently upgrade an opt-out plaintext daemon to wss on a client that now
	// defaults to TLS.
	if p.Host != "ws://100.64.0.1:7531" {
		t.Fatalf("host=%q", p.Host)
	}
}

func TestEncodeNormalisesHTTPSchemes(t *testing.T) {
	for input, want := range map[string]string{
		"https://h:7531":     "wss://h:7531",
		"http://h:7531":      "ws://h:7531",
		"wss://h:7531/v1/ws": "wss://h:7531",
		"h:7531":             "h:7531",
	} {
		raw, err := pairuri.Encode(pairuri.Payload{Host: input, Token: "mcr_x"})
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		p, err := pairuri.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if p.Host != want {
			t.Fatalf("%s: host=%q want %q", input, p.Host, want)
		}
	}
}

func TestFingerprintRoundTrip(t *testing.T) {
	const hexFP = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	const wantB64 = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"

	// Hex, colon-hex, sha256:-prefixed and base64url all normalise to the same
	// canonical wire form, so operators can paste openssl output verbatim.
	inputs := []string{hexFP, strings.ToUpper(hexFP), wantB64, "sha256:" + hexFP}
	for _, in := range inputs {
		raw, err := pairuri.Encode(pairuri.Payload{
			Host:        "wss://h:7531",
			Code:        "K7M29X4P",
			Fingerprint: in,
		})
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		p, err := pairuri.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if p.Fingerprint != wantB64 {
			t.Fatalf("%s: fp=%q want %q", in, p.Fingerprint, wantB64)
		}
	}
}

func TestColonHexFingerprintNormalises(t *testing.T) {
	colon := "01:02:03:04:05:06:07:08:09:0A:0B:0C:0D:0E:0F:10:" +
		"11:12:13:14:15:16:17:18:19:1A:1B:1C:1D:1E:1F:20"
	got, err := pairuri.NormalizeFingerprint(colon)
	if err != nil {
		t.Fatal(err)
	}
	if got != "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA" {
		t.Fatalf("got %q", got)
	}
}

func TestRejectsBadFingerprint(t *testing.T) {
	for _, fp := range []string{"", "deadbeef", strings.Repeat("!", 43), "AQID", strings.Repeat("z", 44)} {
		if _, err := pairuri.NormalizeFingerprint(fp); err == nil {
			t.Fatalf("expected error for %q", fp)
		}
	}
	if _, err := pairuri.Encode(pairuri.Payload{
		Host: "h:1", Token: "t", Fingerprint: "nope",
	}); err == nil {
		t.Fatal("expected Encode to reject a malformed fingerprint")
	}
	if _, err := pairuri.Parse("mcremote://pair?host=h%3A1&token=t&fp=nope"); err == nil {
		t.Fatal("expected Parse to reject a malformed fingerprint")
	}
}

func TestParseRejectsBad(t *testing.T) {
	for _, s := range []string{"", "mcr_only", "https://evil/pair?host=a&token=b", "mcremote://pair?host=a"} {
		if _, err := pairuri.Parse(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestModeRoundTrip(t *testing.T) {
	for _, mode := range []string{pairuri.ModeSelfSigned, pairuri.ModeLetsEncrypt, pairuri.ModeOff} {
		t.Run(mode, func(t *testing.T) {
			raw, err := pairuri.Encode(pairuri.Payload{
				Host:        "wss://devbox.example.com:7531",
				Code:        "K7M29X4P",
				Fingerprint: strings.Repeat("a", 43),
				Mode:        mode,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(raw, "mode="+mode) {
				t.Fatalf("uri %q missing mode=%s", raw, mode)
			}
			got, err := pairuri.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got.Mode != mode {
				t.Fatalf("mode=%q want %q", got.Mode, mode)
			}
			if got.Fingerprint == "" {
				t.Fatal("fingerprint must survive in every mode")
			}
		})
	}
}

// mode= selects a trust rule, so an unrecognised one must fail closed rather
// than be guessed at.
func TestUnknownModeRejected(t *testing.T) {
	if _, err := pairuri.Encode(pairuri.Payload{
		Host: "wss://h:1", Code: "K7M29X4P", Mode: "trustme",
	}); err == nil {
		t.Fatal("Encode accepted an unknown mode")
	}
	if _, err := pairuri.Parse("mcremote://pair?host=wss%3A%2F%2Fh%3A1&code=K7M29X4P&mode=trustme"); err == nil {
		t.Fatal("Parse accepted an unknown mode")
	}
}

// Pre-mode= URIs must keep parsing: the client falls back to "fp present means
// selfsigned", which is exactly the old behaviour.
func TestMissingModeIsAccepted(t *testing.T) {
	got, err := pairuri.Parse("mcremote://pair?host=wss%3A%2F%2Fh%3A1&code=K7M29X4P")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "" {
		t.Fatalf("mode=%q want empty", got.Mode)
	}
}
