// Package providerauth orchestrates interactive provider auth flows
// (MADR 0074 Strategy A).
//
// It owns two things the per-agent code should not each reinvent: deciding
// what kind of flow an authorize response describes, and tracking the flows in
// progress so they expire, cancel, and stay bound to the device that started
// them.
package providerauth

import (
	"net/url"
	"regexp"
	"strings"
)

// FlowKind is what an authorize response turned out to be.
type FlowKind int

const (
	// FlowUnknown means the response yielded neither a loopback redirect nor a
	// user code. Treated as unusable rather than guessed at.
	FlowUnknown FlowKind = iota
	// FlowDevice is an RFC 8628-style flow: show a URL and a code, the engine
	// or CLI polls to completion. No callback, no tunnel.
	FlowDevice
	// FlowBrowser is a loopback OAuth whose redirect_uri points at the host's
	// own localhost. A phone browser cannot reach that, so it needs the
	// reverse tunnel of workstream W3.
	FlowBrowser
)

func (k FlowKind) String() string {
	switch k {
	case FlowDevice:
		return "device"
	case FlowBrowser:
		return "browser"
	default:
		return "unknown"
	}
}

// Classification is the result of inspecting an authorize response.
type Classification struct {
	Kind     FlowKind
	UserCode string
	// VerificationURI is where the user completes the flow. For a device flow
	// this is the page that accepts the code.
	VerificationURI string
}

// codePattern matches the user code in an instruction string. Every device
// flow observed on kilo 7.4.20 renders it as `Enter code: XXXX-XXXXX`:
// Kilo Gateway, ChatGPT headless, and GitHub Copilot all agree on the shape.
var codePattern = regexp.MustCompile(`(?i)\bcode:\s*([A-Z0-9]{4,8}-[A-Z0-9]{4,8})\b`)

// bareCodePattern is the fallback for an instruction that names a code without
// the `code:` lead-in.
var bareCodePattern = regexp.MustCompile(`\b([A-Z0-9]{4}-[A-Z0-9]{4,6})\b`)

// Classify decides what a provider's authorize response actually is
// (MADR 0074 D7).
//
// It deliberately ignores any `method` field the provider returned. The live
// probe that produced D7 found kilo answers `"auto"` for a Kilo Gateway device
// authorization, a headless ChatGPT device flow, a GitHub Copilot device flow,
// **and** a browser loopback OAuth — so that field carries no routing
// information at all. Trusting it would send every browser flow down the
// device path, where it would display a code that does not exist and hang.
//
// The URL is the discriminator. A `redirect_uri` aimed at loopback means the
// completion happens on the host, out of the phone's reach.
func Classify(rawURL, instructions string) Classification {
	if isLoopbackRedirect(rawURL) {
		return Classification{Kind: FlowBrowser, VerificationURI: rawURL}
	}
	if code := extractCode(rawURL, instructions); code != "" {
		return Classification{
			Kind:            FlowDevice,
			UserCode:        code,
			VerificationURI: verificationURI(rawURL),
		}
	}
	return Classification{Kind: FlowUnknown, VerificationURI: rawURL}
}

// isLoopbackRedirect reports whether the authorize URL sends the browser back
// to the host's own machine.
func isLoopbackRedirect(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	redirect := u.Query().Get("redirect_uri")
	if redirect == "" {
		return false
	}
	r, err := url.Parse(redirect)
	if err != nil {
		// A redirect_uri we cannot parse but which mentions loopback is still
		// a browser flow; guessing "device" would be the dangerous direction.
		return strings.Contains(redirect, "localhost") || strings.Contains(redirect, "127.0.0.1")
	}
	host := r.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// extractCode pulls the user code from the instructions, then from the URL's
// own query — kilo's Gateway flow puts it in both.
func extractCode(rawURL, instructions string) string {
	if m := codePattern.FindStringSubmatch(instructions); len(m) == 2 {
		return m[1]
	}
	if u, err := url.Parse(rawURL); err == nil {
		for _, key := range []string{"code", "user_code", "userCode"} {
			if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
				return v
			}
		}
	}
	if m := bareCodePattern.FindStringSubmatch(instructions); len(m) == 2 {
		return m[1]
	}
	return ""
}

// verificationURI strips the embedded code from the URL when the provider put
// one there, so the phone shows a clean link beside the code it displays.
// Kilo's Gateway URL carries `?code=…`; the page works with or without it, and
// leaving it in would let a shoulder-surfer read the code off the link.
func verificationURI(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if q.Get("code") == "" {
		return rawURL
	}
	q.Del("code")
	u.RawQuery = q.Encode()
	return u.String()
}
