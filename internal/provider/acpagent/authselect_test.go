package acpagent

import (
	"errors"
	"strings"
	"testing"
)

func TestSelectACPAuthMethod(t *testing.T) {
	safe := []string{acpAuthCachedToken, acpAuthAPIKey}
	host := []string{acpAuthCachedToken, "grok.com"}
	cold := []string{"grok.com"}
	coldKey := []string{acpAuthAPIKey, "grok.com"}
	all := []string{acpAuthAPIKey, acpAuthCachedToken, "grok.com"}

	tests := []struct {
		name       string
		advertised []string
		pin        string
		hasKey     bool
		want       string
		wantErr    bool
	}{
		{name: "launchagent", advertised: host, want: acpAuthCachedToken},
		{name: "cold", advertised: cold},
		{name: "cold+file key", advertised: cold, hasKey: true, want: acpAuthAPIKey},
		{name: "cold+advertised key", advertised: coldKey, want: acpAuthAPIKey},
		{name: "pin cached_token", advertised: host, pin: acpAuthCachedToken, want: acpAuthCachedToken},
		{name: "pin grok.com", advertised: cold, pin: "grok.com", wantErr: true},
		{name: "pin unknown", advertised: host, pin: "envauth", wantErr: true},
		{name: "pin unadvertised cached_token", advertised: cold, pin: acpAuthCachedToken, wantErr: true},
		{name: "three methods prefer token", advertised: all, hasKey: true, want: acpAuthCachedToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectACPAuthMethod(tc.advertised, tc.pin, safe, tc.hasKey)
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
	s := &session{authRequired: true, advertisedAuth: []string{"grok.com"}}
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
