package opencode

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// maxShareURLBytes bounds a forwarded share link.
const maxShareURLBytes = 2048

// validShareURL accepts only a bounded HTTPS URL with no userinfo and no
// fragment (MADR 0112 A8, PLAN P9 step 4).
//
// The link is something a user will open and may forward to others, so the
// rules are about what it is safe to hand onward: http is downgradeable,
// userinfo smuggles credentials into a shareable string, and a fragment can
// carry state the user cannot see in a preview.
func validShareURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty share url")
	}
	if len(trimmed) > maxShareURLBytes {
		return "", fmt.Errorf("share url exceeds %d bytes", maxShareURLBytes)
	}
	if strings.ContainsAny(trimmed, " \t\r\n\x00") {
		return "", fmt.Errorf("share url has unexpected characters")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("share url is unparseable")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("share url scheme %q is not https", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("share url carries userinfo")
	}
	if u.Host == "" {
		return "", fmt.Errorf("share url has no host")
	}
	if u.Fragment != "" || strings.Contains(trimmed, "#") {
		return "", fmt.Errorf("share url carries a fragment")
	}
	return u.String(), nil
}

// CurrentShare implements [provider.ShareSession].
//
// This is a read and is deliberately available even when mutation policy is
// off: a session shared from the desktop is public whether or not this daemon
// may change that, and hiding it would be the more dangerous silence.
func (o *httpSession) CurrentShare(ctx context.Context) (provider.ShareState, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var info struct {
		Share *struct {
			URL string `json:"url"`
		} `json:"share"`
	}
	if err := o.h.API()(callCtx, "GET", "/session/"+o.h.AgentSessionID()+o.dir(), nil, &info); err != nil {
		return provider.ShareState{}, err
	}
	if info.Share == nil {
		return provider.ShareState{}, nil
	}
	// An unusable link is reported as shared-without-a-link rather than
	// discarded: the transcript is still public, and saying otherwise would
	// understate the exposure.
	link, err := validShareURL(info.Share.URL)
	if err != nil {
		o.h.Log().Warn("opencode share url failed validation; reporting shared without a link")
		return provider.ShareState{Shared: true}, nil
	}
	return provider.ShareState{Shared: true, URL: link}, nil
}

// Share implements [provider.ShareSession].
//
// Refused before any HTTP call when the operator has not enabled it, so a
// disabled daemon makes zero mutation requests. There is no retry: a retried
// share can publish twice, and the caller can always ask again deliberately.
func (o *httpSession) Share(ctx context.Context) (provider.ShareState, error) {
	if !o.d.allowRemoteShare {
		return provider.ShareState{}, provider.ErrShareDisabled
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var out struct {
		URL   string `json:"url"`
		Share *struct {
			URL string `json:"url"`
		} `json:"share"`
	}
	if err := o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/share"+o.dir(), map[string]any{}, &out); err != nil {
		return provider.ShareState{}, err
	}
	raw := out.URL
	if raw == "" && out.Share != nil {
		raw = out.Share.URL
	}
	link, err := validShareURL(raw)
	if err != nil {
		// Never surface an unvalidated link. The share may well have happened
		// upstream, so this reports shared-without-a-link rather than failing
		// in a way that invites a retry that would publish again.
		o.h.Log().Warn("opencode share returned a url that failed validation")
		return provider.ShareState{Shared: true}, nil
	}
	return provider.ShareState{Shared: true, URL: link}, nil
}

// Unshare implements [provider.ShareSession].
func (o *httpSession) Unshare(ctx context.Context) error {
	if !o.d.allowRemoteShare {
		return provider.ErrShareDisabled
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return o.h.API()(callCtx, "DELETE",
		"/session/"+o.h.AgentSessionID()+"/share"+o.dir(), nil, nil)
}

// shareMutationAllowed reports the operator policy for this provider.
func (o *httpSession) shareMutationAllowed() bool { return o.d.allowRemoteShare }

// Compile-time check: the dialect half satisfies the share contract httpagent
// forwards.
var _ interface {
	CurrentShare(context.Context) (provider.ShareState, error)
	Share(context.Context) (provider.ShareState, error)
	Unshare(context.Context) error
} = (*httpSession)(nil)
