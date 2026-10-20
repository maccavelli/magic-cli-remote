package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/spf13/pflag"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// updateHealth is mcrelay's health wiring for update: the probe requests the
// relay's own /healthz. The relay has no admin socket, so it sets no
// RequestStop (MADR 0054 A4; amendment S8).
func updateHealth() service.HealthOptions {
	return service.HealthOptions{Probe: healthzProbe}
}

// loadProbeConfig loads the configuration of the relay run with args: the
// file its --config names, or the default one, with its baked --listen-host,
// --listen-port and --data-dir, as serve binds them. It is a variable for
// tests.
var loadProbeConfig = func(args service.ServeArgs) (FileConfig, error) {
	fs := pflag.NewFlagSet("mcrelay-probe", pflag.ContinueOnError)
	fs.String("listen-host", "", "")
	fs.Int("listen-port", 0, "")
	fs.String("data-dir", "", "")
	set := func(name, value string) error {
		if value == "" {
			return nil
		}
		return fs.Set(name, value)
	}
	port := ""
	if args.ListenPort != 0 {
		port = strconv.Itoa(args.ListenPort)
	}
	if err := errors.Join(set("listen-host", args.ListenHost), set("listen-port", port), set("data-dir", args.DataDir)); err != nil {
		return FileConfig{}, err
	}
	return Load(LoadOptions{ConfigFile: args.ConfigPath, Flags: fs})
}

// probeHost is where the probe dials: the configured host, or loopback when
// the relay listens on every address or names none. Loopback alone would never
// reach a relay bound to one other address.
func probeHost(host string) string {
	switch host {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	}
	return host
}

// probeTLS is the client side of the relay's TLS, verified normally, never
// with InsecureSkipVerify. A Let's Encrypt relay is checked as its first
// domain against the system roots; a relay serving its own certificate file
// is checked against that file, as the first name it carries.
func probeTLS(t TLSConfig) (*tls.Config, error) {
	switch t.Mode {
	case TLSModeLetsEncrypt:
		domains := nonEmptyDomains(t.LetsEncrypt.Domains)
		if len(domains) == 0 {
			return nil, errors.New("tls.letsencrypt.domains is empty")
		}
		return &tls.Config{ServerName: domains[0], MinVersion: tls.VersionTLS12}, nil
	case TLSModeFiles:
		pemBytes, err := os.ReadFile(t.CertFile)
		if err != nil {
			return nil, fmt.Errorf("read tls.cert_file: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		var leaf *x509.Certificate
		for rest := pemBytes; ; {
			var block *pem.Block
			if block, rest = pem.Decode(rest); block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse tls.cert_file: %w", err)
			}
			if leaf == nil {
				leaf = c
			}
			roots.AddCert(c)
		}
		if leaf == nil {
			return nil, errors.New("tls.cert_file holds no certificate")
		}
		name := ""
		switch {
		case len(leaf.DNSNames) > 0:
			name = leaf.DNSNames[0]
		case len(leaf.IPAddresses) > 0:
			name = leaf.IPAddresses[0].String()
		default:
			return nil, errors.New("tls.cert_file's certificate names no host")
		}
		return &tls.Config{ServerName: name, RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
	}
	return nil, nil
}

// healthzProbe is ready when the relay answers GET /healthz with 200 and the
// body {"ok":true}. A request that fails is an error, which the backend keeps
// and reports if the wait times out; any other answer is not ready.
func healthzProbe(ctx context.Context, args service.ServeArgs) (libservice.Health, error) {
	fc, err := loadProbeConfig(args)
	if err != nil {
		return libservice.Health{}, fmt.Errorf("load the relay's configuration: %w", err)
	}
	tlsCfg, err := probeTLS(fc.TLS)
	if err != nil {
		return libservice.Health{}, err
	}
	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	url := scheme + "://" + net.JoinHostPort(probeHost(fc.Listen.Host), strconv.Itoa(fc.Listen.Port)) + "/healthz"
	// A fresh transport: no proxy from the environment, no kept connection.
	tr := &http.Transport{TLSClientConfig: tlsCfg, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return libservice.Health{}, err
	}
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return libservice.Health{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return libservice.Health{}, err
	}
	detail := fmt.Sprintf("GET %s: %d %s", url, res.StatusCode, bytes.TrimSpace(body))
	if res.StatusCode != http.StatusOK {
		return libservice.Health{Detail: detail}, nil
	}
	var reply struct {
		OK bool `json:"ok"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reply); err != nil || !reply.OK {
		return libservice.Health{Detail: detail}, nil
	}
	return libservice.Health{Ready: true, Detail: detail}, nil
}
