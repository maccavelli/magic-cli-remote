// Package pairuri builds and parses mcremote://pair deep-link payloads
// used for QR-based device onboarding.
package pairuri

import (
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
	Host  string // e.g. "100.64.0.1:7531" (no scheme)
	Token string // e.g. "mcr_…" (optional if Code set)
	Code  string // e.g. "K7M29X4P" or "K7M2-9X4P" (optional if Token set)
}

// Encode returns mcremote://pair?host=…&token=… and/or &code=…
func Encode(p Payload) (string, error) {
	host := strings.TrimSpace(p.Host)
	token := strings.TrimSpace(p.Token)
	code := strings.TrimSpace(p.Code)
	if host == "" {
		return "", fmt.Errorf("pairuri: host is required")
	}
	if token == "" && code == "" {
		return "", fmt.Errorf("pairuri: token or code is required")
	}
	host = stripURLScheme(host)
	u := url.URL{
		Scheme: Scheme,
		Host:   Host,
	}
	q := url.Values{}
	q.Set("host", host)
	if token != "" {
		q.Set("token", token)
	}
	if code != "" {
		q.Set("code", code)
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
	if host == "" {
		return Payload{}, fmt.Errorf("pairuri: missing host query param")
	}
	if token == "" && code == "" {
		return Payload{}, fmt.Errorf("pairuri: missing token or code query param")
	}
	return Payload{Host: stripURLScheme(host), Token: token, Code: code}, nil
}

func stripURLScheme(host string) string {
	for _, p := range []string{"ws://", "wss://", "http://", "https://"} {
		if strings.HasPrefix(strings.ToLower(host), p) {
			host = host[len(p):]
			break
		}
	}
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	return strings.TrimSpace(host)
}
