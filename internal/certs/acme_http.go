package certs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
)

// ACMEHTTPOptions configures HTTP-01 ACME (public edge, e.g. mcrelay).
//
// Unlike [EnsureACME] (DNS-01 for mesh-only mcremote), this path expects the
// host to be reachable on HTTPPort (default 80) so the CA can fetch
// /.well-known/acme-challenge/. TLS-ALPN and DNS-01 are disabled.
type ACMEHTTPOptions struct {
	// Domains to request; the first is the primary. Must be public DNS names.
	Domains []string
	// Email is the ACME account contact (required by Let's Encrypt).
	Email string
	// DirectoryURL overrides the CA directory ("" = Let's Encrypt production).
	DirectoryURL string
	// StorageDir holds certmagic account keys and issued certificates.
	StorageDir string
	// HTTPPort is the port for HTTP-01 (0 = 80 / certmagic default). Use only
	// when 80 is unavailable and the CA is configured for that port (staging
	// / custom CAs); public Let's Encrypt requires 80 from the internet.
	HTTPPort int
	// Timeout bounds synchronous first issuance; 0 uses ACMEStartupTimeout.
	Timeout time.Duration
	// Verbose raises certmagic log level from warn to info.
	Verbose bool
}

// EnsureACMEHTTP obtains (or loads / renews) certificates using the ACME
// HTTP-01 challenge. certmagic opens a temporary listener on HTTPPort during
// Obtain/Renew; keep that port free (or set HTTPPort to an alternate for
// non-public CAs).
//
// Returns an [ACMEBundle] whose TLSConfig serves the certs and whose Close
// stops renewal maintenance.
func EnsureACMEHTTP(ctx context.Context, opts ACMEHTTPOptions) (*ACMEBundle, error) {
	domains := make([]string, 0, len(opts.Domains))
	for _, d := range opts.Domains {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("certs: acme-http: no domains configured")
	}
	if strings.TrimSpace(opts.Email) == "" {
		return nil, fmt.Errorf("certs: acme-http: no account email configured")
	}
	if strings.TrimSpace(opts.StorageDir) == "" {
		return nil, fmt.Errorf("certs: acme-http: no storage dir configured")
	}
	if err := os.MkdirAll(opts.StorageDir, 0o700); err != nil {
		return nil, fmt.Errorf("certs: acme-http: storage dir: %w", err)
	}

	logger := acmeLogger(opts.Verbose)
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

	issuerTemplate := certmagic.ACMEIssuer{
		CA:                      strings.TrimSpace(opts.DirectoryURL),
		Email:                   strings.TrimSpace(opts.Email),
		Agreed:                  true,
		DisableHTTPChallenge:    false,
		DisableTLSALPNChallenge: true,
		// No DNS01Solver — HTTP-01 only.
		Logger: logger,
	}
	if opts.HTTPPort > 0 && opts.HTTPPort != certmagic.HTTPChallengePort {
		issuerTemplate.AltHTTPPort = opts.HTTPPort
	}

	magic.Issuers = []certmagic.Issuer{
		certmagic.NewACMEIssuer(magic, issuerTemplate),
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = ACMEStartupTimeout
	}
	mctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := magic.ManageSync(mctx, domains); err != nil {
		cache.Stop()
		return nil, fmt.Errorf("certs: acme-http: manage %s: %w", strings.Join(domains, ","), err)
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

// HTTPLetsEncryptStaging is the Let's Encrypt staging ACME directory.
const HTTPLetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
