package certs

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libdns/route53"
	"go.uber.org/zap"
)

func TestEnsureACMEValidation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	cases := []struct {
		name string
		opts ACMEOptions
		want string
	}{
		{
			name: "no domains",
			opts: ACMEOptions{
				Email:      "a@b.c",
				StorageDir: dir,
			},
			want: "no domains",
		},
		{
			name: "whitespace-only domains",
			opts: ACMEOptions{
				Domains:    []string{"  ", ""},
				Email:      "a@b.c",
				StorageDir: dir,
			},
			want: "no domains",
		},
		{
			name: "no email",
			opts: ACMEOptions{
				Domains:    []string{"example.com"},
				StorageDir: dir,
			},
			want: "no account email",
		},
		{
			name: "whitespace email",
			opts: ACMEOptions{
				Domains:    []string{"example.com"},
				Email:      "   ",
				StorageDir: dir,
			},
			want: "no account email",
		},
		{
			name: "no storage",
			opts: ACMEOptions{
				Domains: []string{"example.com"},
				Email:   "a@b.c",
			},
			want: "no storage dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EnsureACME(ctx, tc.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestEnsureACMEManageFailsCleanly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	_, err := EnsureACME(ctx, ACMEOptions{
		Domains:      []string{"  unlikely-acme-test.invalid  "},
		Email:        "test@example.com",
		DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
		StorageDir:   filepath.Join(dir, "acme"),
		Timeout:      1500 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected manage failure for invalid domain")
	}
	if !strings.Contains(err.Error(), "manage") {
		t.Fatalf("want manage error, got %v", err)
	}
}

// TestDNS01SolverWaitsForRoute53Sync locks issue #35's fix: the provider waits
// for Route 53 to report INSYNC, long enough to pass Route 53's documented 60
// seconds, and certmagic adds no fixed delay (MADR 0231 D1, D2).
func TestDNS01SolverWaitsForRoute53Sync(t *testing.T) {
	r := Route53Options{HostedZoneID: "Z0", Region: "us-east-1", Profile: "p", MaxRetries: 7}
	s := newDNS01Solver(r, zap.NewNop(), nil)

	p, ok := s.DNSProvider.(*route53.Provider)
	if !ok {
		t.Fatalf("DNSProvider is %T, want *route53.Provider", s.DNSProvider)
	}
	if !p.WaitForRoute53Sync {
		t.Error("WaitForRoute53Sync is false: the CA can validate before every Route 53 server has the record (0231 D1)")
	}
	if p.Route53MaxWait != 2*time.Minute {
		t.Errorf("Route53MaxWait = %v, want 2m: the waiter must check after Route 53's 60 s (0231 D1, F3)", p.Route53MaxWait)
	}
	if !p.SkipRoute53SyncOnDelete {
		t.Error("SkipRoute53SyncOnDelete is false: removing the record after validation should not wait (0231 D1)")
	}
	if p.HostedZoneID != "Z0" || p.Region != "us-east-1" || p.Profile != "p" || p.MaxRetries != 7 {
		t.Errorf("provider = zone %q region %q profile %q retries %d, want the options passed through",
			p.HostedZoneID, p.Region, p.Profile, p.MaxRetries)
	}
	if s.PropagationDelay != 0 {
		t.Errorf("PropagationDelay = %v, want 0: INSYNC is the wait, not a fixed delay (0231 D1)", s.PropagationDelay)
	}
	if s.PropagationTimeout != 30*time.Second {
		t.Errorf("PropagationTimeout = %v, want 30s (0231 D2)", s.PropagationTimeout)
	}
	if s.TTL != 5*time.Minute {
		t.Errorf("TTL = %v, want 5m", s.TTL)
	}
}

// TestDNS01BudgetFitsTheStartBound: the waits fit inside ACMEStartupTimeout,
// which the unit's TimeoutStartSec must exceed (TestUnitIsTypeNotify), and the
// INSYNC wait is long enough for a GetChange check after Route 53's 60 s given
// the waiter's 30 s steps (MADR 0231 D2, F3).
func TestDNS01BudgetFitsTheStartBound(t *testing.T) {
	if sum := acmeRoute53SyncWait + acmePropagationTimeout + acmeCABudget; sum > ACMEStartupTimeout {
		t.Errorf("Route 53 sync %v + propagation %v + CA %v = %v, more than ACMEStartupTimeout %v (0231 D2)",
			acmeRoute53SyncWait, acmePropagationTimeout, acmeCABudget, sum, ACMEStartupTimeout)
	}
	if acmeRoute53SyncWait < 90*time.Second {
		t.Errorf("acmeRoute53SyncWait = %v: under 90 s the waiter never checks after Route 53's 60 s (0231 F3)",
			acmeRoute53SyncWait)
	}
}

func TestACMELoggerNonNil(t *testing.T) {
	logger := acmeLogger(false)
	if logger == nil {
		t.Fatal("acmeLogger(false) returned nil")
	}
}

func TestACMELoggerVerboseNonNil(t *testing.T) {
	logger := acmeLogger(true)
	if logger == nil {
		t.Fatal("acmeLogger(true) returned nil")
	}
}

func TestACMEBundleTLSConfig(t *testing.T) {
	b := &ACMEBundle{}
	cfg := b.TLSConfig()
	if cfg == nil {
		t.Fatal("TLSConfig() returned nil")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS12)
	}
	// The daemon serves HTTP/1.1 only (MADR 0201 D8): http/1.1 first, and
	// no h2 anywhere in the list.
	if len(cfg.NextProtos) == 0 || cfg.NextProtos[0] != "http/1.1" {
		t.Fatalf("NextProtos = %v, want http/1.1 first", cfg.NextProtos)
	}
	for _, p := range cfg.NextProtos {
		if p == "h2" || p == "h2c" {
			t.Fatalf("NextProtos = %v offers HTTP/2", cfg.NextProtos)
		}
	}
}

func TestACMEBundleCloseNil(t *testing.T) {
	var b *ACMEBundle
	b.Close()
}

func TestACMEBundleCloseNilCache(t *testing.T) {
	b := &ACMEBundle{Domains: []string{"example.com"}}
	b.Close()
}
