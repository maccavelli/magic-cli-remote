package daemon

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/xdg"
)

// EnsureCerts resolves the daemon's self-signed TLS identity for cfg,
// generating the managed pair under cfg.DataDir on first run.
//
// It is exported so `mcremote pair` can advertise the same fingerprint the
// listener will present without having to start the daemon. It never performs
// ACME work — Let's Encrypt certificates are not pinned, so pairing does not
// need them.
func EnsureCerts(cfg config.Config) (*certs.Bundle, error) {
	if err := xdg.EnsureDir(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if !cfg.TLS.Managed() {
		b, err := certs.Load(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tls: load operator certificate: %w", err)
		}
		return b, nil
	}
	extra := []string{cfg.Listen.Host}
	extra = append(extra, cfg.TLS.LetsEncrypt.TrimmedDomains()...)
	b, err := certs.Ensure(certs.Options{
		Dir:        cfg.DataDir,
		ExtraHosts: extra,
	})
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	return b, nil
}

// Identity is the resolved TLS material for a listener.
type Identity struct {
	// Mode is the mode actually in force — which may not be the configured
	// mode, since ACME failures downgrade to selfsigned.
	Mode string
	// Config is nil when Mode is off.
	Config *tls.Config
	// SelfSigned is set for the selfsigned mode (including ACME fallback).
	SelfSigned *certs.Bundle
	// ACME is set when Let's Encrypt issuance succeeded.
	ACME *certs.ACMEBundle
	// FellBack records that letsencrypt was requested but selfsigned is served.
	FellBack bool
}

// Close releases any background renewal machinery.
func (i *Identity) Close() {
	if i != nil && i.ACME != nil {
		i.ACME.Close()
	}
}

// EnsureTLS resolves cfg into a serving TLS identity.
//
// Let's Encrypt failures are never fatal. A daemon that cannot reach Route 53
// or the ACME CA — an expired IAM key, a network partition, a rate limit —
// must still come up, so issuance errors are logged loudly and the listener
// falls back to the self-signed identity. Phones that were paired in
// letsencrypt mode will then fail to validate the certificate; that is a
// visible, recoverable failure, whereas a daemon that refuses to start is not.
func EnsureTLS(ctx context.Context, cfg config.Config, log *slog.Logger) (*Identity, error) {
	if log == nil {
		log = slog.Default()
	}
	switch cfg.TLS.ResolvedMode() {
	case config.TLSModeOff:
		return &Identity{Mode: config.TLSModeOff}, nil

	case config.TLSModeLetsEncrypt:
		le := cfg.TLS.LetsEncrypt
		acme, err := certs.EnsureACME(ctx, certs.ACMEOptions{
			Domains:      le.TrimmedDomains(),
			Email:        le.Email,
			DirectoryURL: le.Directory(),
			StorageDir:   cfg.ACMECacheDir(),
			Route53: certs.Route53Options{
				HostedZoneID: le.Route53.HostedZoneID,
				Region:       le.Route53.Region,
				Profile:      le.Route53.Profile,
				MaxRetries:   le.Route53.MaxRetries,
			},
			Verbose: cfg.Log.Level == "debug",
		})
		if err == nil {
			return &Identity{
				Mode:   config.TLSModeLetsEncrypt,
				Config: acme.TLSConfig(),
				ACME:   acme,
			}, nil
		}
		log.Error("Let's Encrypt issuance FAILED; falling back to a self-signed "+
			"certificate. Phones paired against the public name will refuse to "+
			"connect until this is fixed (check AWS credentials, the Route 53 "+
			"hosted zone, and tls.letsencrypt.domains)",
			slog.String("domains", strings.Join(le.TrimmedDomains(), ",")),
			slog.String("acme_directory", le.Directory()),
			slog.Any("error", err),
		)
		id, ferr := selfSignedIdentity(cfg)
		if ferr != nil {
			return nil, fmt.Errorf("tls: letsencrypt failed (%v) and self-signed fallback failed: %w", err, ferr)
		}
		id.FellBack = true
		return id, nil

	default:
		return selfSignedIdentity(cfg)
	}
}

func selfSignedIdentity(cfg config.Config) (*Identity, error) {
	b, err := EnsureCerts(cfg)
	if err != nil {
		return nil, err
	}
	return &Identity{
		Mode:       config.TLSModeSelfSigned,
		Config:     b.TLSConfig(),
		SelfSigned: b,
	}, nil
}
