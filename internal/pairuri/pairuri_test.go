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

func TestEncodeStripsScheme(t *testing.T) {
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
	if p.Host != "100.64.0.1:7531" {
		t.Fatalf("host=%q", p.Host)
	}
}

func TestParseRejectsBad(t *testing.T) {
	for _, s := range []string{"", "mcr_only", "https://evil/pair?host=a&token=b", "mcremote://pair?host=a"} {
		if _, err := pairuri.Parse(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}
