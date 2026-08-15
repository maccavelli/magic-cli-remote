package relay

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/certs"
)

// ApplyTLS resolves managed TLS for the server config (ACME HTTP-01, ACME
// DNS-01, or files). On letsencrypt success, cfg.TLSConfig is set and a
// cleanup func is returned (stop certmagic maintenance). Caller must defer
// cleanup when non-nil.
func ApplyTLS(ctx context.Context, fc FileConfig, cfg *Config, log *slog.Logger) (cleanup func(), err error) {
	if log == nil {
		log = slog.Default()
	}
	tls := fc.TLS.Normalized()
	switch tls.Mode {
	case TLSModeLetsEncrypt:
		return applyLetsEncrypt(ctx, fc, cfg, log)
	case TLSModeFiles:
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			return nil, fmt.Errorf("tls.mode=files requires cert_file and key_file")
		}
		log.Info("TLS: certificate files",
			slog.String("cert", cfg.TLSCertFile),
			slog.String("key", cfg.TLSKeyFile),
		)
		return nil, nil
	default:
		return nil, nil
	}
}

func applyLetsEncrypt(ctx context.Context, fc FileConfig, cfg *Config, log *slog.Logger) (func(), error) {
	le := fc.TLS.LetsEncrypt
	domains := nonEmptyDomains(le.Domains)
	dir := le.Directory()
	challenge := le.ChallengeNormalized()
	cache := fc.ACMECacheDir()
	verbose := strings.EqualFold(fc.Log.Level, "debug")

	var bundle *certs.ACMEBundle
	var err error
	switch challenge {
	case ACMEChallengeHTTP01:
		httpPort := effectiveHTTPPort(le.HTTPPort)
		bundle, err = certs.EnsureACMEHTTP(ctx, certs.ACMEHTTPOptions{
			Domains:      domains,
			Email:        strings.TrimSpace(le.Email),
			DirectoryURL: dir,
			StorageDir:   cache,
			HTTPPort:     le.HTTPPort,
			Verbose:      verbose,
		})
		if err != nil {
			// R13: ACME HTTP-01 needs exclusive use of the challenge port.
			return nil, fmt.Errorf(
				"acme http-01 (ensure nothing else binds challenge port %d; set tls.letsencrypt.http_port if needed): %w",
				httpPort, err,
			)
		}
		cfg.TLSConfig = bundle.TLSConfig()
		stripHTTP2(cfg.TLSConfig)
		cfg.TLSCertFile = ""
		cfg.TLSKeyFile = ""
		log.Info("TLS: Let's Encrypt (HTTP-01)",
			slog.String("domains", strings.Join(domains, ",")),
			slog.String("directory", bundle.Directory),
			slog.String("cache", cache),
			slog.String("challenge", ACMEChallengeHTTP01),
			slog.Int("http_challenge_port", effectiveHTTPPort(le.HTTPPort)),
		)
		return func() { bundle.Close() }, nil

	case ACMEChallengeDNS01:
		bundle, err = certs.EnsureACME(ctx, certs.ACMEOptions{
			Domains:      domains,
			Email:        strings.TrimSpace(le.Email),
			DirectoryURL: dir,
			StorageDir:   cache,
			Route53: certs.Route53Options{
				HostedZoneID: strings.TrimSpace(le.Route53.HostedZoneID),
				Region:       strings.TrimSpace(le.Route53.Region),
				Profile:      strings.TrimSpace(le.Route53.Profile),
				MaxRetries:   le.Route53.MaxRetries,
			},
			Verbose: verbose,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"acme dns-01 (check Route 53 credentials/zone and tls.letsencrypt.route53.*): %w",
				err,
			)
		}
		cfg.TLSConfig = bundle.TLSConfig()
		stripHTTP2(cfg.TLSConfig)
		cfg.TLSCertFile = ""
		cfg.TLSKeyFile = ""
		log.Info("TLS: Let's Encrypt (DNS-01 / Route 53)",
			slog.String("domains", strings.Join(domains, ",")),
			slog.String("directory", bundle.Directory),
			slog.String("cache", cache),
			slog.String("challenge", ACMEChallengeDNS01),
			slog.String("route53_zone", strings.TrimSpace(le.Route53.HostedZoneID)),
			slog.String("route53_region", strings.TrimSpace(le.Route53.Region)),
		)
		return func() { bundle.Close() }, nil

	default:
		return nil, fmt.Errorf("tls.letsencrypt.challenge must be http-01 or dns-01 (got %q)", le.Challenge)
	}
}

// stripHTTP2 removes HTTP/2 ALPN from a join-plane tls.Config (0091 D10).
// The listener is HTTP/1.1 WebSocket upgrade; h2 is unused surface.
func stripHTTP2(cfg *tls.Config) {
	if cfg == nil || len(cfg.NextProtos) == 0 {
		return
	}
	out := make([]string, 0, len(cfg.NextProtos))
	for _, p := range cfg.NextProtos {
		if p == "h2" || p == "h2c" {
			continue
		}
		out = append(out, p)
	}
	cfg.NextProtos = out
}

func effectiveHTTPPort(p int) int {
	if p <= 0 {
		return 80
	}
	return p
}
