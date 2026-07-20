// Package certs manages the daemon's self-signed TLS identity.
//
// mcremote is dialled by name-less mesh addresses (Tailscale 100.64/10, LAN
// IPs, localhost), so a publicly trusted certificate is neither obtainable nor
// useful. Instead the daemon mints one long-lived self-signed P-256 leaf on
// first run and advertises its SHA-256 fingerprint in the pair QR; the phone
// pins that fingerprint and refuses anything else. Trust is established once,
// out-of-band, by the same scan that carries the pair code.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// DefaultCertName / DefaultKeyName live under the daemon data dir.
	DefaultCertName = "tls.crt"
	DefaultKeyName  = "tls.key"

	// DefaultValidity is deliberately long: rotation means re-pairing every
	// phone, so a short lifetime buys nothing for a pinned self-signed leaf.
	DefaultValidity = 10 * 365 * 24 * time.Hour

	// RenewBefore triggers regeneration while the old cert still works.
	RenewBefore = 30 * 24 * time.Hour

	fileMode = 0o600
)

// Bundle is a loaded (or freshly minted) TLS identity.
type Bundle struct {
	Certificate tls.Certificate
	Leaf        *x509.Certificate
	CertPath    string
	KeyPath     string
	// Generated reports whether this run created the files.
	Generated bool
}

// Options configures Ensure.
type Options struct {
	// Dir is the daemon data dir; used when CertFile/KeyFile are empty.
	Dir string
	// CertFile / KeyFile override the default paths under Dir.
	CertFile string
	KeyFile  string
	// ExtraHosts adds SAN entries beyond the auto-detected set (hostname,
	// localhost, loopback and every non-link-local interface address).
	ExtraHosts []string
	// Validity defaults to DefaultValidity.
	Validity time.Duration
	// Now defaults to time.Now (test seam).
	Now func() time.Time
}

func (o Options) paths() (certPath, keyPath string) {
	certPath, keyPath = o.CertFile, o.KeyFile
	if certPath == "" {
		certPath = filepath.Join(o.Dir, DefaultCertName)
	}
	if keyPath == "" {
		keyPath = filepath.Join(o.Dir, DefaultKeyName)
	}
	return certPath, keyPath
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Ensure loads the persisted certificate, regenerating it when missing,
// unreadable, or within RenewBefore of expiry.
func Ensure(opts Options) (*Bundle, error) {
	certPath, keyPath := opts.paths()

	if b, err := load(certPath, keyPath); err == nil {
		now := opts.now()
		if now.Before(b.Leaf.NotAfter.Add(-RenewBefore)) && !now.Before(b.Leaf.NotBefore) {
			return b, nil
		}
	}

	b, err := generate(opts, certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Load reads an existing pair without ever regenerating it.
func Load(certPath, keyPath string) (*Bundle, error) {
	return load(certPath, keyPath)
}

func load(certPath, keyPath string) (*Bundle, error) {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	if len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("certs: %s contains no certificate", certPath)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("certs: parse %s: %w", certPath, err)
	}
	pair.Leaf = leaf
	return &Bundle{
		Certificate: pair,
		Leaf:        leaf,
		CertPath:    certPath,
		KeyPath:     keyPath,
	}, nil
}

func generate(opts Options, certPath, keyPath string) (*Bundle, error) {
	validity := opts.Validity
	if validity <= 0 {
		validity = DefaultValidity
	}
	now := opts.now()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("certs: generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("certs: serial: %w", err)
	}

	dnsNames, ips := SANs(opts.ExtraHosts)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "mcremote",
			Organization: []string{"mcremote"},
		},
		NotBefore: now.Add(-1 * time.Hour), // tolerate phone/host clock skew
		NotAfter:  now.Add(validity),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment |
			x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("certs: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("certs: marshal key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return nil, fmt.Errorf("certs: mkdir: %w", err)
	}
	// Key first, and always at 0600 — a cert without a key is inert.
	if err := writeFile(keyPath, keyPEM); err != nil {
		return nil, err
	}
	if err := writeFile(certPath, certPEM); err != nil {
		return nil, err
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("certs: reload generated pair: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("certs: parse generated leaf: %w", err)
	}
	pair.Leaf = leaf

	return &Bundle{
		Certificate: pair,
		Leaf:        leaf,
		CertPath:    certPath,
		KeyPath:     keyPath,
		Generated:   true,
	}, nil
}

// writeFile creates path with fileMode, truncating any predecessor. The
// explicit Chmod covers the case where the file already existed with looser
// permissions (os.OpenFile only applies mode on create).
func writeFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("certs: write %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("certs: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("certs: write %s: %w", path, err)
	}
	if err := os.Chmod(path, fileMode); err != nil {
		return fmt.Errorf("certs: chmod %s: %w", path, err)
	}
	return nil
}

// SANs returns the DNS names and IPs the daemon certificate should cover:
// the machine hostname, "localhost", loopback, and every non-link-local
// address on an up interface (LAN plus Tailscale/Headscale 100.64.0.0/10).
func SANs(extra []string) (dnsNames []string, ips []net.IP) {
	dnsSet := map[string]bool{"localhost": true}
	ipSet := map[string]net.IP{}

	addIP := func(ip net.IP) {
		if ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() {
			return
		}
		ipSet[ip.String()] = ip
	}
	addIP(net.IPv4(127, 0, 0, 1))
	addIP(net.IPv6loopback)

	if h, err := os.Hostname(); err == nil && h != "" {
		dnsSet[h] = true
	}

	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				switch v := a.(type) {
				case *net.IPNet:
					addIP(v.IP)
				case *net.IPAddr:
					addIP(v.IP)
				}
			}
		}
	}

	for _, h := range extra {
		h = trimHost(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			addIP(ip)
			continue
		}
		dnsSet[h] = true
	}

	for name := range dnsSet {
		dnsNames = append(dnsNames, name)
	}
	sort.Strings(dnsNames)

	keys := make([]string, 0, len(ipSet))
	for k := range ipSet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ips = append(ips, ipSet[k])
	}
	return dnsNames, ips
}

func trimHost(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return h
}

// FingerprintSHA256 is the SHA-256 digest of the leaf certificate DER — the
// same bytes a client sees in the TLS handshake, so both sides can compare.
func (b *Bundle) FingerprintSHA256() [32]byte {
	return sha256.Sum256(b.Leaf.Raw)
}

// FingerprintHex is the lowercase hex digest (64 chars).
func (b *Bundle) FingerprintHex() string {
	sum := b.FingerprintSHA256()
	return hex.EncodeToString(sum[:])
}

// FingerprintColonHex matches `openssl x509 -fingerprint -sha256` output.
func (b *Bundle) FingerprintColonHex() string {
	sum := b.FingerprintSHA256()
	out := make([]byte, 0, 95)
	for i, x := range sum {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hexUpper[x>>4], hexUpper[x&0x0f])
	}
	return string(out)
}

const hexUpper = "0123456789ABCDEF"

// FingerprintBase64 is the unpadded base64url digest — the compact form put
// into the pair QR (43 chars vs 64 for hex).
func (b *Bundle) FingerprintBase64() string {
	sum := b.FingerprintSHA256()
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// TLSConfig returns a server config serving this bundle.
func (b *Bundle) TLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{b.Certificate},
		MinVersion:   tls.VersionTLS12,
	}
}
