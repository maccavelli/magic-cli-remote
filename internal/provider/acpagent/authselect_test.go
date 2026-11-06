package acpagent

import (
	"errors"
	"strings"
	"testing"
)

func TestSelectACPAuthMethod(t *testing.T) {
	// A Spec's policy, in neutral names: a token first, then a key, and a
	// browser login that is never headless-safe.
	const (
		token   = "vendor.token"
		key     = "vendor.key"
		browser = "vendor.browser"
	)
	safe := []string{token, key}
	preference := []string{token, key}
	host := []string{token, browser}
	cold := []string{browser}
	coldKey := []string{key, browser}
	all := []string{key, token, browser}

	tests := []struct {
		name       string
		advertised []string
		pin        string
		hasKey     bool
		want       string
		wantErr    bool
	}{
		{name: "launchagent", advertised: host, want: token},
		{name: "cold", advertised: cold},
		// A key whose method was not advertised is never selected (MADR 0234 D2).
		{name: "cold+unadvertised key", advertised: cold, hasKey: true},
		{name: "cold+advertised key", advertised: coldKey, want: key},
		{name: "pin token", advertised: host, pin: token, want: token},
		{name: "pin browser", advertised: cold, pin: browser, wantErr: true},
		{name: "pin unknown", advertised: host, pin: "envauth", wantErr: true},
		{name: "pin unadvertised token", advertised: cold, pin: token, wantErr: true},
		{name: "three methods prefer token", advertised: all, hasKey: true, want: token},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectACPAuthMethod(tc.advertised, tc.pin, safe, preference, key, tc.hasKey)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRejectUnauthenticated(t *testing.T) {
	if err := rejectUnauthenticated(nil); err != nil {
		t.Fatalf("nil session: %v", err)
	}
	if err := rejectUnauthenticated(&session{}); err != nil {
		t.Fatalf("authenticated: %v", err)
	}
	s := &session{authRequired: true, advertisedAuth: []string{"vendor.browser"}}
	err := rejectUnauthenticated(s)
	if !errors.Is(err, ErrACPAuthRequired) {
		t.Fatalf("err = %v, want ErrACPAuthRequired", err)
	}
	if err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("message %v must contain authentication required", err)
	}
}

func TestShouldKeepWarm(t *testing.T) {
	if shouldKeepWarm(nil) {
		t.Fatal("nil")
	}
	if shouldKeepWarm(&session{authRequired: true}) {
		t.Fatal("authRequired spare must be dropped")
	}
	if !shouldKeepWarm(&session{}) {
		t.Fatal("usable spare must be kept")
	}
}
