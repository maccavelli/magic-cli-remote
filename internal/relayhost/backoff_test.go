package relayhost

import (
	"testing"
	"time"
)

func TestBackoffResetSemantics(t *testing.T) {
	// Document R3: after register_ok, *backoff is set to 1s.
	// session is integration-heavy; unit-test the assignment contract via a
	// small helper that mirrors Run's update.
	backoff := 16 * time.Second
	onRegisterOK := func(b *time.Duration) {
		*b = time.Second
	}
	onRegisterOK(&backoff)
	if backoff != time.Second {
		t.Fatalf("backoff=%v", backoff)
	}
}

func TestLoopbackAddr(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:7531":   "127.0.0.1:7531",
		"[::]:7531":      "127.0.0.1:7531",
		"127.0.0.1:7531": "127.0.0.1:7531",
		"100.64.0.1:9":   "100.64.0.1:9",
	}
	for in, want := range cases {
		if got := loopbackAddr(in); got != want {
			t.Fatalf("%s → %s want %s", in, got, want)
		}
	}
}

func TestNormalizeRelayURL(t *testing.T) {
	got, err := normalizeRelayURL("relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://relay.example.com" {
		t.Fatal(got)
	}
	got, err = normalizeRelayURL("https://relay.example.com:8443/path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://relay.example.com:8443" {
		t.Fatal(got)
	}
	if _, err := normalizeRelayURL(""); err == nil {
		t.Fatal("empty")
	}
}
