// Package pairuri builds and parses mcremote://pair deep-link payloads
// used for QR-based device onboarding.
package pairuri

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

const (
	// Scheme is the URI scheme scanned by the Flutter client.
	Scheme = "mcremote"
	// Host is the URI host component (path-style: mcremote://pair?…).
	Host = "pair"
)

// Payload is host:port plus either a durable token or a short pair code.
type Payload struct {
	// Host is host:port, optionally prefixed with ws:// or wss:// to pin the
	// transport the client must dial. A bare host means "client default".
	Host string // e.g. "wss://100.64.0.1:7531"
	// Token is a durable device token, e.g. "mcr_…" (optional if Code set).
	Token string
	// Code is a short pair code, e.g. "K7M29X4P" (optional if Token set).
	Code string
	// Fingerprint is the unpadded base64url SHA-256 of the daemon's TLS leaf
	// certificate. The client pins it and refuses any other certificate, which
	// is what makes a self-signed cert safe to trust.
	Fingerprint string
}

// Encode returns mcremote://pair?host=…&token=… and/or &code=… (&fp=…).
func Encode(p Payload) (string, error) {
	host := strings.TrimSpace(p.Host)
	token := strings.TrimSpace(p.Token)
	code := strings.TrimSpace(p.Code)
	fp := strings.TrimSpace(p.Fingerprint)
	if host == "" {
		return "", fmt.Errorf("pairuri: host is required")
	}
	if token == "" && code == "" {
		return "", fmt.Errorf("pairuri: token or code is required")
	}
	if fp != "" {
		norm, err := NormalizeFingerprint(fp)
		if err != nil {
			return "", err
		}
		fp = norm
	}
	scheme, authority := splitScheme(host)
	if authority == "" {
		return "", fmt.Errorf("pairuri: host is required")
	}
	u := url.URL{
		Scheme: Scheme,
		Host:   Host,
	}
	q := url.Values{}
	q.Set("host", scheme+authority)
	if token != "" {
		q.Set("token", token)
	}
	if code != "" {
		q.Set("code", code)
	}
	if fp != "" {
		q.Set("fp", fp)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Parse accepts mcremote://pair?… URIs with host + token and/or code.
func Parse(raw string) (Payload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Payload{}, fmt.Errorf("pairuri: empty payload")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Payload{}, fmt.Errorf("pairuri: parse: %w", err)
	}
	if !strings.EqualFold(u.Scheme, Scheme) {
		return Payload{}, fmt.Errorf("pairuri: unsupported scheme %q (want %s)", u.Scheme, Scheme)
	}
	h := strings.Trim(u.Host, "/")
	path := strings.Trim(u.Path, "/")
	if h == "" && path != "" {
		h = path
	}
	if !strings.EqualFold(h, Host) {
		return Payload{}, fmt.Errorf("pairuri: unsupported path %q (want %s)", h, Host)
	}
	q := u.Query()
	host := strings.TrimSpace(q.Get("host"))
	token := strings.TrimSpace(q.Get("token"))
	code := strings.TrimSpace(q.Get("code"))
	fp := strings.TrimSpace(q.Get("fp"))
	if host == "" {
		return Payload{}, fmt.Errorf("pairuri: missing host query param")
	}
	if token == "" && code == "" {
		return Payload{}, fmt.Errorf("pairuri: missing token or code query param")
	}
	if fp != "" {
		norm, err := NormalizeFingerprint(fp)
		if err != nil {
			return Payload{}, err
		}
		fp = norm
	}
	scheme, authority := splitScheme(host)
	if authority == "" {
		return Payload{}, fmt.Errorf("pairuri: missing host query param")
	}
	return Payload{
		Host:        scheme + authority,
		Token:       token,
		Code:        code,
		Fingerprint: fp,
	}, nil
}

// NormalizeFingerprint accepts hex (with or without colons) or base64url and
// returns the canonical unpadded base64url form used on the wire.
func NormalizeFingerprint(fp string) (string, error) {
	s := strings.TrimSpace(fp)
	if s == "" {
		return "", fmt.Errorf("pairuri: empty fingerprint")
	}
	if strings.HasPrefix(strings.ToLower(s), "sha256:") {
		s = s[len("sha256:"):]
	}
	compact := strings.ReplaceAll(strings.ReplaceAll(s, ":", ""), " ", "")
	if len(compact) == 64 {
		if raw, err := hex.DecodeString(strings.ToLower(compact)); err == nil {
			return base64.RawURLEncoding.EncodeToString(raw), nil
		}
	}
	// base64url or base64, padded or not.
	trimmed := strings.TrimRight(s, "=")
	if raw, err := base64.RawURLEncoding.DecodeString(trimmed); err == nil && len(raw) == 32 {
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil && len(raw) == 32 {
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	return "", fmt.Errorf("pairuri: fingerprint must be a SHA-256 digest in hex or base64url")
}

// splitScheme separates a recognised transport prefix from the authority,
// normalising http(s) to ws(s). Unlike a plain strip, the prefix is returned
// rather than discarded so the QR can pin the transport the client must dial.
func splitScheme(host string) (scheme, authority string) {
	h := strings.TrimSpace(host)
	lower := strings.ToLower(h)
	for _, p := range []struct{ prefix, canon string }{
		{"wss://", "wss://"},
		{"ws://", "ws://"},
		{"https://", "wss://"},
		{"http://", "ws://"},
	} {
		if strings.HasPrefix(lower, p.prefix) {
			scheme = p.canon
			h = h[len(p.prefix):]
			break
		}
	}
	if i := strings.Index(h, "/"); i >= 0 {
		h = h[:i]
	}
	return scheme, strings.TrimSpace(h)
}
