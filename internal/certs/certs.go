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
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// DefaultCertName is the managed certificate basename under the daemon data dir.
	DefaultCertName = "tls.crt"
	// DefaultKeyName is the managed private key basename under the daemon data dir.
	DefaultKeyName = "tls.key"

	// DefaultValidity is deliberately long: rotation means re-pairing every
	// phone, so a short lifetime buys nothing for a pinned self-signed leaf.
	DefaultValidity = 10 * 365 * 24 * time.Hour

	// RenewBefore triggers regeneration while the old cert still works.
	RenewBefore = 30 * 24 * time.Hour

	// BackdateSkew backdates a freshly minted leaf's NotBefore so a host clock
	// that later reads earlier — a backward NTP step, a suspend/resume, a boot
	// before time sync — does not make the persisted identity look "not yet
	// valid". The leaf is trusted by its pinned SHA-256 fingerprint, never by
	// its validity window (the phone accepts a pin match regardless of the
	// window), so a wide backdate costs nothing and avoids re-keying — which
	// would change the fingerprint and silently lock out every paired device.
	BackdateSkew = 30 * 24 * time.Hour

	fileMode = 0o600
)

// Reasons reported by Bundle.GeneratedReason.
const (
	// ReasonFirstRun: neither file existed, so this is a new identity.
	ReasonFirstRun = "first_run"
	// ReasonExpiring: the existing leaf is at, or within RenewBefore of, genuine
	// (forward) expiry. A clock that merely reads before NotBefore is
	// deliberately NOT a regeneration reason — see Ensure.
	ReasonExpiring = "expiring"
)

// Bundle is a loaded (or freshly minted) TLS identity.
type Bundle struct {
	Certificate tls.Certificate
	Leaf        *x509.Certificate
	CertPath    string
	KeyPath     string
	// Generated reports whether this run created the files.
	Generated bool
	// GeneratedReason is ReasonFirstRun or ReasonExpiring when Generated is
	// set, and empty otherwise. Ensure never generates for any other reason —
	// an existing-but-unreadable pair is an error, not a re-key.
	GeneratedReason string
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

// Ensure loads the persisted certificate, generating one only when the pair is
// genuinely absent (first run) or within RenewBefore of expiry.
//
// A pair that is *present but unreadable* — bad permissions, a truncated write,
// a half-written key, a full disk — is an error, never a reason to re-key.
// Minting a new identity there would silently invalidate every paired device
// and present the phone with a fingerprint it must reject, so the failure that
// started as "one unreadable file" would end as "re-pair every device". Failing
// loudly keeps the identity recoverable: the operator fixes the file, or moves
// it aside deliberately to force a new one.
func Ensure(opts Options) (*Bundle, error) {
	certPath, keyPath := opts.paths()

	// Serialize (re)generation across processes: a daemon start and a concurrent
	// CLI `pair` (which also materializes the bundle to print its fingerprint)
	// both call Ensure, and two first-run generates would otherwise race a
	// mismatched key/cert into place. The dir must exist to hold the lock file.
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return nil, fmt.Errorf("certs: create dir %s: %w", filepath.Dir(certPath), err)
	}
	unlock, err := lockCertDir(filepath.Dir(certPath))
	if err != nil {
		return nil, err
	}
	defer unlock()

	// Complete a crash mid-install: both *.new files present and valid.
	if err := promoteNewPair(certPath, keyPath); err == nil {
		// Fall through to load the promoted pair.
	} else {
		cleanupOrphanNew(certPath, keyPath)
	}

	certExists, err := exists(certPath)
	if err != nil {
		return nil, err
	}
	keyExists, err := exists(keyPath)
	if err != nil {
		return nil, err
	}

	// Only a fully absent pair is a first run. One file without the other is a
	// damaged identity: generating would overwrite the survivor.
	if !certExists && !keyExists {
		return generate(opts, certPath, keyPath, ReasonFirstRun)
	}

	b, err := load(certPath, keyPath)
	if err != nil {
		// Last chance: promote a staged pair left after a partial rename.
		if perr := promoteNewPair(certPath, keyPath); perr == nil {
			if b, err = load(certPath, keyPath); err == nil {
				return b, nil
			}
		}
		return nil, fmt.Errorf("certs: %s/%s exist but could not be loaded; refusing "+
			"to mint a new identity (that would invalidate every paired device). "+
			"Fix the files, or move them aside to deliberately re-key: %w",
			certPath, keyPath, err)
	}

	// Regenerate only when the leaf is at, or within RenewBefore of, genuine
	// forward expiry. A clock reading *before* NotBefore (a backward jump, or a
	// host not yet NTP-synced at boot) must never trigger a re-key: identity is
	// the pinned fingerprint, not the validity window, so re-minting there would
	// change the fingerprint and lock out every paired phone for no security
	// gain. The wide BackdateSkew on NotBefore keeps ordinary clock wobble far
	// from the window edge; true forward expiry still rotates as before.
	now := opts.now()
	if now.Before(b.Leaf.NotAfter.Add(-RenewBefore)) {
		return b, nil
	}
	return generate(opts, certPath, keyPath, ReasonExpiring)
}

// exists reports whether path is present. A stat failure that is not
// "not exist" (EACCES on the directory, EIO) is returned as an error rather
// than being read as absence — see Ensure.
func exists(path string) (bool, error) {
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("certs: stat %s: %w", path, err)
	}
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

func generate(opts Options, certPath, keyPath, reason string) (*Bundle, error) {
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
		NotBefore: now.Add(-BackdateSkew), // wide skew tolerance; see BackdateSkew
		NotAfter:  now.Add(validity),
		// Server auth only. The cert is self-signed but deliberately NOT a CA:
		// the natural way to make curl or a browser work is to install it in a
		// trust store, and a 10-year CA there could sign for any name at all.
		// A leaf with IsCA: false is only ever trusted for its own SANs.
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
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
	// Stage both files as *.new, then rename into place. Writing the live
	// paths one-at-a-time could leave a new key with an old cert (or the
	// reverse) after a crash mid-renewal — and Ensure refuses to re-key over
	// unreadable pairs, which would strand every paired device (Phase 3.1).
	if err := writePairAtomic(certPath, keyPath, certPEM, keyPEM); err != nil {
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
		Certificate:     pair,
		Leaf:            leaf,
		CertPath:        certPath,
		KeyPath:         keyPath,
		Generated:       true,
		GeneratedReason: reason,
	}, nil
}

// writeFile replaces path atomically (tmp + fsync + rename) at fileMode, so a
// crash mid-write can never leave a truncated key/cert behind. The explicit
// Chmod covers a pre-existing tmp with looser permissions.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("certs: write %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("certs: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("certs: sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("certs: write %s: %w", path, err)
	}
	if err := os.Chmod(tmp, fileMode); err != nil {
		return fmt.Errorf("certs: chmod %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("certs: replace %s: %w", path, err)
	}
	return nil
}

// writePairAtomic stages cert and key as path+".new", then renames both into
// place. Staging is complete before any live path is replaced, so a crash
// during the first rename leaves either the old pair or one new file plus the
// other as *.new — recoverable via promoteNewPair / cleanupNewPair.
func writePairAtomic(certPath, keyPath string, certPEM, keyPEM []byte) error {
	certNew, keyNew := certPath+".new", keyPath+".new"
	if err := writeFile(keyNew, keyPEM); err != nil {
		return err
	}
	if err := writeFile(certNew, certPEM); err != nil {
		_ = os.Remove(keyNew)
		return err
	}
	// Prefer installing the key first: a new cert with an old key is harder
	// to diagnose than a new key briefly without a matching cert (Load fails
	// closed either way; promoteNewPair recovers a full *.new pair).
	if err := os.Rename(keyNew, keyPath); err != nil {
		_ = os.Remove(certNew)
		_ = os.Remove(keyNew)
		return fmt.Errorf("certs: install key: %w", err)
	}
	if err := os.Rename(certNew, certPath); err != nil {
		// Key is new; cert is still old or missing. Leave key.new-less and
		// cert.new for the next Ensure to attempt promote, or fail loudly.
		return fmt.Errorf("certs: install cert: %w", err)
	}
	return nil
}

// promoteNewPair renames a complete tls.crt.new / tls.key.new pair into place
// when both staging files exist. Used after a crash mid-install.
func promoteNewPair(certPath, keyPath string) error {
	certNew, keyNew := certPath+".new", keyPath+".new"
	if _, err := os.Stat(certNew); err != nil {
		return err
	}
	if _, err := os.Stat(keyNew); err != nil {
		return err
	}
	// Validate the staged pair loads before overwriting the live paths.
	if _, err := Load(certNew, keyNew); err != nil {
		return err
	}
	if err := os.Rename(keyNew, keyPath); err != nil {
		return err
	}
	return os.Rename(certNew, certPath)
}

// cleanupOrphanNew resolves a lone staging file left when a crash interrupted
// writePairAtomic between the two renames. It promotes the orphan when it
// completes the install, and only removes it when it is genuinely stale.
//
// The dangerous case: writePairAtomic installs the key first, so a crash after
// the key rename but before the cert rename leaves the NEW key live, the OLD
// cert live, and the NEW cert as cert.new with no key.new. Blindly deleting
// that cert.new (the old behavior) strands the new key against the mismatched
// old cert — load then fails and Ensure refuses to re-key, so the daemon will
// not start until an operator hand-repairs the files and re-pairs every device.
// Instead, if the orphan loads against the surviving live counterpart, promote
// it to finish the interrupted install; otherwise it is stale staging, remove it.
func cleanupOrphanNew(certPath, keyPath string) {
	certNew, keyNew := certPath+".new", keyPath+".new"
	_, certErr := os.Stat(certNew)
	_, keyErr := os.Stat(keyNew)
	if certErr == nil && keyErr != nil {
		if _, err := Load(certNew, keyPath); err == nil && os.Rename(certNew, certPath) == nil {
			return
		}
		_ = os.Remove(certNew)
	}
	if keyErr == nil && certErr != nil {
		if _, err := Load(certPath, keyNew); err == nil && os.Rename(keyNew, keyPath) == nil {
			return
		}
		_ = os.Remove(keyNew)
	}
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

// SPKIFingerprint is the client-key identity used by the public-key allowlist
// (ADR 0005): the unpadded base64url SHA-256 digest of the certificate's
// RawSubjectPublicKeyInfo.
//
// It fingerprints the *public key*, not the certificate DER, so a client may
// regenerate the self-signed certificate wrapping a key without changing its
// identity — the key is the identity, exactly as with an SSH authorized key.
// This scheme must match byte-for-byte what the Dart client computes over the
// same SPKI bytes.
func SPKIFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
