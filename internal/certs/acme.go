package certs

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/route53"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ACME issuance uses the DNS-01 challenge exclusively.
//
// mcremote daemons sit on a mesh (Tailscale/Headscale) address that is not
// reachable from the public internet, and their MagicDNS names are not in
// public DNS — so an ACME validator can never complete HTTP-01 or
// TLS-ALPN-01 against them. DNS-01 needs no inbound reachability at all: the
// daemon writes a _acme-challenge TXT record into a public Route 53 zone and
// the CA reads it. Both other challenge types are explicitly disabled below
// so certmagic never wastes a validation attempt on them.

const (
	// ACMEStartupTimeout bounds first-run issuance. Exceeding it is not fatal:
	// the caller falls back to the self-signed identity.
	ACMEStartupTimeout = 3 * time.Minute

	// acmeChallengeTTL is the TTL on the temporary _acme-challenge TXT record.
	acmeChallengeTTL = 5 * time.Minute

	// acmePropagationTimeout bounds how long we wait for the TXT record to be
	// visible to public resolvers before asking the CA to validate.
	acmePropagationTimeout = 2 * time.Minute
)

// Route53Options selects the Route 53 zone used for DNS-01. Credentials are
// resolved by the ambient AWS chain (env vars, shared config/profile,
// instance/role credentials) — mcremote never reads or stores them.
type Route53Options struct {
	HostedZoneID string
	Region       string
	Profile      string
	MaxRetries   int
}

// ACMEOptions configures EnsureACME.
type ACMEOptions struct {
	// Domains to request; the first is the primary. Must be DNS names.
	Domains []string
	// Email is the ACME account contact.
	Email string
	// DirectoryURL overrides the CA directory ("" = Let's Encrypt production).
	DirectoryURL string
	// StorageDir holds certmagic's account keys and issued certificates.
	StorageDir string
	// Route53 configures the DNS-01 solver.
	Route53 Route53Options
	// Timeout bounds the synchronous first issuance; 0 uses ACMEStartupTimeout.
	Timeout time.Duration
	// Verbose raises certmagic's own log level from warn to info.
	Verbose bool
}

// ACMEBundle is a live certmagic-managed certificate set. certmagic owns
// renewal (it runs a maintenance goroutine off the cache) and on-disk storage;
// we only hand its tls.Config to the listener.
type ACMEBundle struct {
	Domains []string
	// Directory is the ACME endpoint actually used, for logging.
	Directory string

	magic *certmagic.Config
	cache *certmagic.Cache
}

// EnsureACME obtains (or loads from storage, or renews) certificates for
// opts.Domains and returns a bundle whose TLSConfig serves them.
//
// It is synchronous by design: the caller needs to know at startup whether to
// fall back to the self-signed identity. Errors are returned, never fatal —
// see daemon.EnsureTLS.
func EnsureACME(ctx context.Context, opts ACMEOptions) (*ACMEBundle, error) {
	domains := make([]string, 0, len(opts.Domains))
	for _, d := range opts.Domains {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("certs: acme: no domains configured")
	}
	if strings.TrimSpace(opts.Email) == "" {
		return nil, fmt.Errorf("certs: acme: no account email configured")
	}
	if strings.TrimSpace(opts.StorageDir) == "" {
		return nil, fmt.Errorf("certs: acme: no storage dir configured")
	}
	if err := os.MkdirAll(opts.StorageDir, 0o700); err != nil {
		return nil, fmt.Errorf("certs: acme: storage dir: %w", err)
	}

	logger := acmeLogger(opts.Verbose)

	solver := &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider: &route53.Provider{
				Region:       opts.Route53.Region,
				Profile:      opts.Route53.Profile,
				HostedZoneID: opts.Route53.HostedZoneID,
				MaxRetries:   opts.Route53.MaxRetries,
			},
			TTL:                acmeChallengeTTL,
			PropagationTimeout: acmePropagationTimeout,
			Logger:             logger,
		},
	}

	storage := &certmagic.FileStorage{Path: opts.StorageDir}

	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
			return magic, nil
		},
		Logger: logger,
	})
	magic = certmagic.New(cache, certmagic.Config{
		Storage: storage,
		Logger:  logger,
	})
	magic.Issuers = []certmagic.Issuer{
		certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
			CA:                      strings.TrimSpace(opts.DirectoryURL),
			Email:                   strings.TrimSpace(opts.Email),
			Agreed:                  true,
			DNS01Solver:             solver,
			DisableHTTPChallenge:    true,
			DisableTLSALPNChallenge: true,
			Logger:                  logger,
		}),
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = ACMEStartupTimeout
	}
	mctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := magic.ManageSync(mctx, domains); err != nil {
		cache.Stop()
		return nil, fmt.Errorf("certs: acme: manage %s: %w", strings.Join(domains, ","), err)
	}

	directory := strings.TrimSpace(opts.DirectoryURL)
	if directory == "" {
		directory = certmagic.DefaultACME.CA
	}
	return &ACMEBundle{
		Domains:   domains,
		Directory: directory,
		magic:     magic,
		cache:     cache,
	}, nil
}

// TLSConfig returns a server config backed by the managed certificates.
// NextProtos keeps the HTTP protocols first; the TLS-ALPN entry certmagic adds
// is harmless but unused since that challenge is disabled.
func (a *ACMEBundle) TLSConfig() *tls.Config {
	cfg := a.magic.TLSConfig()
	cfg.MinVersion = tls.VersionTLS12
	cfg.NextProtos = append([]string{"h2", "http/1.1"}, cfg.NextProtos...)
	return cfg
}

// Close stops the renewal/maintenance goroutine and releases storage locks.
func (a *ACMEBundle) Close() {
	if a != nil && a.cache != nil {
		a.cache.Stop()
	}
}

// acmeLogger adapts certmagic's mandatory *zap.Logger onto stderr. certmagic
// is chatty at info; default to warn so ordinary runs stay quiet while
// issuance/renewal failures are still visible.
func acmeLogger(verbose bool) *zap.Logger {
	level := zapcore.WarnLevel
	if verbose {
		level = zapcore.InfoLevel
	}
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "time"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	return zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(encCfg),
		zapcore.Lock(os.Stderr),
		level,
	)).Named("certmagic")
}
