package certs

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
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
	// the caller falls back to the self-signed identity. It must cover
	// acmeRoute53SyncWait + acmePropagationTimeout + acmeCABudget (MADR 0231
	// D2). A run cut off while the record is published or awaited ends before
	// the challenge is submitted, so it costs no failed validation (0231 F4).
	ACMEStartupTimeout = 3 * time.Minute

	// acmeChallengeTTL is the TTL on the temporary _acme-challenge TXT record.
	acmeChallengeTTL = 5 * time.Minute

	// acmeRoute53SyncWait is the longest we wait for Route 53 to report the
	// TXT change INSYNC, "propagated to all Route 53 DNS servers managing the
	// hosted zone". The SDK's waiter checks at 30 s steps, so 2 minutes gives a
	// check between 60 s and 90 s, after Route 53's documented 60 seconds; the
	// library's 1-minute default stops checking at about 30 s (MADR 0231 D1, F3).
	acmeRoute53SyncWait = 2 * time.Minute

	// acmePropagationTimeout caps certmagic's poll of the zone's authoritative
	// nameservers. It is a cap, not a wait: the poll ends at the first
	// nameserver that has the record (0231 F1). After INSYNC every Route 53
	// server has it, so the poll confirms and does not wait (0231 D2).
	acmePropagationTimeout = 30 * time.Second

	// acmeCABudget is the allowance, in the startup budget only, for the CA's
	// validation, finalisation and download (MADR 0231 D2).
	acmeCABudget = 30 * time.Second
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

// newDNS01Solver builds EnsureACME's DNS-01 solver. certmagic's propagation
// check returns at the first authoritative nameserver that has the TXT record,
// so on its own it let the CA validate while other Route 53 servers still
// answered NXDOMAIN (issue #35). The provider therefore waits for Route 53 to
// report the change INSYNC before returning, and only then does certmagic
// confirm and the CA validate (MADR 0231 D1). There is no fixed
// PropagationDelay: INSYNC is the signal, and a fixed delay was rejected.
func newDNS01Solver(r Route53Options, logger *zap.Logger, providerLog *slog.Logger) *certmagic.DNS01Solver {
	return &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider: &route53.Provider{
				Region:       r.Region,
				Profile:      r.Profile,
				HostedZoneID: r.HostedZoneID,
				MaxRetries:   r.MaxRetries,
				// Wait for GetChange to report INSYNC when the record is written.
				// Removing it after validation needs no wait.
				WaitForRoute53Sync:      true,
				Route53MaxWait:          acmeRoute53SyncWait,
				SkipRoute53SyncOnDelete: true,
				Logger:                  providerLog,
			},
			TTL:                acmeChallengeTTL,
			PropagationTimeout: acmePropagationTimeout,
			Logger:             logger,
		},
	}
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

	// The Route 53 provider's own debug events ("waiting for Route53 sync",
	// "Route53 sync complete") reach stderr only when verbose; nil discards
	// them (MADR 0231 D3).
	var providerLog *slog.Logger
	if opts.Verbose {
		providerLog = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	solver := newDNS01Solver(opts.Route53, logger, providerLog)

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

	directory := strings.TrimSpace(opts.DirectoryURL)
	if directory == "" {
		directory = certmagic.DefaultACME.CA
	}

	// No new order while a recent validation failure for these names stands,
	// unless every name already has a certificate in storage (MADR 0231 D4).
	stored := func(ctx context.Context, d string) error {
		_, err := magic.CacheManagedCertificate(ctx, d)
		return err
	}
	if err := refuseDuringCooldown(mctx, opts.StorageDir, directory, domains, nowFunc(), stored); err != nil {
		cache.Stop()
		return nil, err
	}

	if err := magic.ManageSync(mctx, domains); err != nil {
		cache.Stop()
		err = fmt.Errorf("certs: acme: manage %s: %w", strings.Join(domains, ","), err)
		if problem, ok := cooldownProblem(err); ok {
			if rerr := recordFailure(opts.StorageDir, directory, domains, problem, nowFunc()); rerr != nil {
				err = errors.Join(err, fmt.Errorf("certs: acme: recording the failed validation: %w", rerr))
			}
		}
		return nil, err
	}
	if err := clearFailure(opts.StorageDir); err != nil {
		logger.Warn("could not remove the failed-validation record", zap.Error(err))
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
	// HTTP/1.1 only, first; whatever certmagic adds follows, minus any HTTP/2
	// (MADR 0201 D8, 0073 F-T8). The daemon serves WebSocket upgrades and small
	// GETs, and the relay strips h2 itself (0091 D10).
	protos := []string{"http/1.1"}
	for _, p := range cfg.NextProtos {
		if p != "h2" && p != "h2c" && p != "http/1.1" {
			protos = append(protos, p)
		}
	}
	cfg.NextProtos = protos
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
