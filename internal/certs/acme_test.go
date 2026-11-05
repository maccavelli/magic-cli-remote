package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/route53"
	"github.com/mholt/acmez/v3/acme"
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

// unreachableDirectory makes any ACME request fail with a connection error,
// so a test that sees ErrIssuanceCooldown instead knows no request was made.
const unreachableDirectory = "http://127.0.0.1:1/directory"

// TestEnsureACMERefusesDuringCooldown: with a fresh failure record and no
// certificate in storage, EnsureACME refuses at once and orders nothing
// (MADR 0231 D4).
func TestEnsureACMERefusesDuringCooldown(t *testing.T) {
	dir := t.TempDir()
	domain := "unlikely-acme-test.invalid"
	if err := recordFailure(dir, unreachableDirectory, []string{domain}, acme.ProblemTypeUnauthorized, time.Now()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := EnsureACME(context.Background(), ACMEOptions{
		Domains:      []string{domain},
		Email:        "test@example.com",
		DirectoryURL: unreachableDirectory,
		StorageDir:   dir,
		Timeout:      5 * time.Second,
	})
	if !errors.Is(err, ErrIssuanceCooldown) {
		t.Fatalf("err = %v, want ErrIssuanceCooldown: an order was attempted during the cooldown", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("refusal took %v, want under 1s", elapsed)
	}
	for _, want := range []string{domain, acme.ProblemTypeUnauthorized, cooldownFileName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestEnsureACMECooldownStillLoadsAStoredCertificate: a cooldown never blocks
// a name whose certificate is already in storage, and the success clears the
// record (MADR 0231 D4).
func TestEnsureACMECooldownStillLoadsAStoredCertificate(t *testing.T) {
	dir := t.TempDir()
	domain := "unlikely-acme-test.invalid"
	storeTestCertificate(t, dir, unreachableDirectory, domain)
	if err := recordFailure(dir, unreachableDirectory, []string{domain}, acme.ProblemTypeUnauthorized, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := EnsureACME(context.Background(), ACMEOptions{
		Domains:      []string{domain},
		Email:        "test@example.com",
		DirectoryURL: unreachableDirectory,
		StorageDir:   dir,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("EnsureACME with a stored certificate: %v", err)
	}
	defer b.Close()
	if _, err := os.Stat(cooldownPath(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the failure record was not cleared after a successful load: %v", err)
	}
}

// TestEnsureACMERecordsAFailedValidation: when ManageSync fails with a counted
// ACME problem, EnsureACME records it, and the next start refuses (MADR 0231
// D4). The CA is a local server that answers every request with an
// unauthorized problem, so nothing leaves the machine.
func TestEnsureACMERecordsAFailedValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"` + acme.ProblemTypeUnauthorized + `","detail":"test: no TXT record found"}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	opts := ACMEOptions{
		Domains:      []string{"unlikely-acme-test.invalid"},
		Email:        "test@example.com",
		DirectoryURL: srv.URL + "/directory",
		StorageDir:   dir,
		Timeout:      10 * time.Second,
	}
	if _, err := EnsureACME(context.Background(), opts); err == nil {
		t.Fatal("EnsureACME succeeded against a CA that refuses everything")
	} else if errors.Is(err, ErrIssuanceCooldown) {
		t.Fatalf("the first start refused: %v", err)
	}
	rec, _, ok := activeCooldown(dir, opts.DirectoryURL, opts.Domains, time.Now())
	if !ok {
		t.Fatal("no failure was recorded after an unauthorized problem")
	}
	if rec.Problem != acme.ProblemTypeUnauthorized {
		t.Errorf("recorded problem = %q, want %q", rec.Problem, acme.ProblemTypeUnauthorized)
	}
	if _, err := EnsureACME(context.Background(), opts); !errors.Is(err, ErrIssuanceCooldown) {
		t.Errorf("second start: err = %v, want ErrIssuanceCooldown", err)
	}
}

// storeTestCertificate writes a self-signed certificate for domain into
// certmagic's storage layout, under the issuer key EnsureACME's issuer uses
// for directory.
func storeTestCertificate(t *testing.T, dir, directory, domain string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	issuerKey := (&certmagic.ACMEIssuer{CA: directory}).IssuerKey()
	storage := &certmagic.FileStorage{Path: dir}
	ctx := context.Background()
	for k, v := range map[string][]byte{
		certmagic.StorageKeys.SiteCert(issuerKey, domain):       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		certmagic.StorageKeys.SitePrivateKey(issuerKey, domain): pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		certmagic.StorageKeys.SiteMeta(issuerKey, domain):       []byte(`{"sans":["` + domain + `"]}`),
	} {
		if err := storage.Store(ctx, k, v); err != nil {
			t.Fatal(err)
		}
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
