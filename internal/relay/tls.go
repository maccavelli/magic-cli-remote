package relay

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/certs"
)

// ApplyTLS resolves managed TLS for the server config (ACME HTTP-01 or files).
// On letsencrypt success, cfg.TLSConfig is set and a cleanup func is returned
// (stop certmagic maintenance). Caller must defer cleanup when non-nil.
func ApplyTLS(ctx context.Context, fc FileConfig, cfg *Config, log *slog.Logger) (cleanup func(), err error) {
	if log == nil {
		log = slog.Default()
	}
	tls := fc.TLS.Normalized()
	switch tls.Mode {
	case TLSModeLetsEncrypt:
		domains := nonEmptyDomains(tls.LetsEncrypt.Domains)
		dir := tls.LetsEncrypt.Directory()
		httpPort := effectiveHTTPPort(tls.LetsEncrypt.HTTPPort)
		bundle, err := certs.EnsureACMEHTTP(ctx, certs.ACMEHTTPOptions{
			Domains:      domains,
			Email:        strings.TrimSpace(tls.LetsEncrypt.Email),
			DirectoryURL: dir,
			StorageDir:   fc.ACMECacheDir(),
			HTTPPort:     tls.LetsEncrypt.HTTPPort,
			Verbose:      strings.EqualFold(fc.Log.Level, "debug"),
		})
		if err != nil {
			// R13: ACME HTTP-01 needs exclusive use of the challenge port.
			return nil, fmt.Errorf(
				"acme http-01 (ensure nothing else binds challenge port %d; set tls.letsencrypt.http_port if needed): %w",
				httpPort, err,
			)
		}
		cfg.TLSConfig = bundle.TLSConfig()
		cfg.TLSCertFile = ""
		cfg.TLSKeyFile = ""
		log.Info("TLS: Let's Encrypt (HTTP-01)",
			slog.String("domains", strings.Join(domains, ",")),
			slog.String("directory", bundle.Directory),
			slog.String("cache", fc.ACMECacheDir()),
			slog.Int("http_challenge_port", effectiveHTTPPort(tls.LetsEncrypt.HTTPPort)),
		)
		return func() { bundle.Close() }, nil
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

func effectiveHTTPPort(p int) int {
	if p <= 0 {
		return 80
	}
	return p
}
