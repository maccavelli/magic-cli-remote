package opencode

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Session sharing (MADR 0112 A8, PLAN P9).
//
// Two properties carry the weight: a disabled daemon makes *zero* mutation
// requests, and a link that reaches the phone is one it is safe to open and
// forward.

// shareSessionWith builds a dialect session with the given policy.
func shareSessionWith(t *testing.T, allow bool, routes ...route) (*recorder, *httpSession) {
	t.Helper()
	h := newRecorder(routes...)
	d := &httpDialect{
		log:                  slog.Default(),
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
		allowRemoteShare:     allow,
	}
	return h, d.NewSession(h).(*httpSession)
}

func TestValidShareURL(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		ok       bool
	}{
		{"https", "https://opencode.ai/s/abc123", true},
		{"https with path and query", "https://opencode.ai/s/abc?v=2", true},
		{"uppercase scheme", "HTTPS://opencode.ai/s/abc", true},
		{"http is refused", "http://opencode.ai/s/abc", false},
		{"file is refused", "file:///tmp/x", false},
		{"javascript is refused", "javascript:alert(1)", false},
		{"userinfo is refused", "https://user:pw@opencode.ai/s/abc", false},
		{"bare userinfo is refused", "https://user@opencode.ai/s/abc", false},
		{"fragment is refused", "https://opencode.ai/s/abc#secret", false},
		{"empty fragment marker is refused", "https://opencode.ai/s/abc#", false},
		{"no host is refused", "https:///s/abc", false},
		{"whitespace is refused", "https://opencode.ai/s/a b", false},
		{"newline is refused", "https://opencode.ai/s/a\nb", false},
		{"empty is refused", "", false},
		{"blank is refused", "   ", false},
		{"over the cap is refused", "https://opencode.ai/" + strings.Repeat("a", maxShareURLBytes), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validShareURL(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("validShareURL(%q) err=%v, want ok=%v", tc.in, err, tc.ok)
			}
		})
	}
}

// TestShareRefusedBeforeAnyRequestWhenDisabled is the core policy rule: a
// daemon that has not been enabled makes no mutation call at all.
func TestShareRefusedBeforeAnyRequestWhenDisabled(t *testing.T) {
	h, s := shareSessionWith(t, false)
	if _, err := s.Share(context.Background()); !errors.Is(err, provider.ErrShareDisabled) {
		t.Fatalf("Share err = %v, want ErrShareDisabled", err)
	}
	if err := s.Unshare(context.Background()); !errors.Is(err, provider.ErrShareDisabled) {
		t.Fatalf("Unshare err = %v, want ErrShareDisabled", err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("a disabled daemon still called OpenCode: %v", h.paths())
	}
}

// TestShareStateIsReadableWhenMutationIsDisabled proves the read survives the
// policy: an existing public link must stay visible.
func TestShareStateIsReadableWhenMutationIsDisabled(t *testing.T) {
	h, s := shareSessionWith(t, false,
		route{"/session/", `{"id":"ses_1","share":{"url":"https://opencode.ai/s/abc"}}`})
	got, err := s.CurrentShare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Shared || got.URL != "https://opencode.ai/s/abc" {
		t.Fatalf("state = %+v", got)
	}
	if len(h.calls) != 1 || h.calls[0].method != "GET" {
		t.Fatalf("reading state used %v", h.paths())
	}
}

// TestUnsharedSessionReportsNotShared proves the absent case.
func TestUnsharedSessionReportsNotShared(t *testing.T) {
	_, s := shareSessionWith(t, true, route{"/session/", `{"id":"ses_1"}`})
	got, err := s.CurrentShare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Shared || got.URL != "" {
		t.Fatalf("state = %+v", got)
	}
}

// TestInvalidShareURLStillReportsShared is the honest-exposure rule: the
// transcript is public either way, so an unusable link must not read as private.
func TestInvalidShareURLStillReportsShared(t *testing.T) {
	for _, bad := range []string{
		`{"share":{"url":"http://opencode.ai/s/abc"}}`,
		`{"share":{"url":"https://user:pw@opencode.ai/s/abc"}}`,
		`{"share":{"url":"https://opencode.ai/s/abc#tok"}}`,
		`{"share":{"url":""}}`,
	} {
		_, s := shareSessionWith(t, true, route{"/session/", bad})
		got, err := s.CurrentShare(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", bad, err)
		}
		if !got.Shared {
			t.Fatalf("%s reported private; the transcript is still public", bad)
		}
		if got.URL != "" {
			t.Fatalf("%s leaked an unvalidated url %q", bad, got.URL)
		}
	}
}

// TestShareUsesTheDocumentedRoutes pins the wire calls.
func TestShareUsesTheDocumentedRoutes(t *testing.T) {
	h, s := shareSessionWith(t, true,
		route{"/share", `{"url":"https://opencode.ai/s/new"}`})
	got, err := s.Share(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Shared || got.URL != "https://opencode.ai/s/new" {
		t.Fatalf("share = %+v", got)
	}
	call := h.find(t, "POST", "/share")
	if !strings.Contains(call.path, "directory=") {
		t.Fatalf("share carried no directory scope: %q", call.path)
	}

	h.calls = nil
	if err := s.Unshare(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.find(t, "DELETE", "/share")
}

// TestShareAcceptsTheNestedShape proves both documented result shapes decode.
func TestShareAcceptsTheNestedShape(t *testing.T) {
	_, s := shareSessionWith(t, true,
		route{"/share", `{"share":{"url":"https://opencode.ai/s/nested"}}`})
	got, err := s.Share(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://opencode.ai/s/nested" {
		t.Fatalf("share = %+v", got)
	}
}

// TestShareNeverRetries proves one request per call. A retried share can
// publish twice, and a second public link is not something a retry may create.
func TestShareNeverRetries(t *testing.T) {
	h, s := shareSessionWith(t, true)
	var attempts int
	h.api = func(_ context.Context, method, path string, _, _ any) error {
		if strings.Contains(path, "/share") {
			attempts++
		}
		return errors.New("upstream refused")
	}
	if _, err := s.Share(context.Background()); err == nil {
		t.Fatal("expected the upstream failure to surface")
	}
	if attempts != 1 {
		t.Fatalf("share attempted %d times; it must never retry", attempts)
	}
}

// TestShareUpstreamFailureIsSurfaced proves a refusal is reported, not masked.
func TestShareUpstreamFailureIsSurfaced(t *testing.T) {
	want := errors.New("share: disabled")
	h, s := shareSessionWith(t, true)
	h.api = func(context.Context, string, string, any, any) error { return want }
	if _, err := s.Share(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if err := s.Unshare(context.Background()); !errors.Is(err, want) {
		t.Fatalf("unshare err = %v, want %v", err, want)
	}
}

// TestShareURLNeverReachesTheEventStream is the no-history rule: the link is
// answered on request, so a replay cannot resurface a revoked link.
func TestShareURLNeverReachesTheEventStream(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default(), allowRemoteShare: true}
	s := d.NewSession(h).(*httpSession)
	h.api = func(_ context.Context, _, path string, _ any, out any) error {
		if strings.Contains(path, "/share") {
			if p, ok := out.(*struct {
				URL   string `json:"url"`
				Share *struct {
					URL string `json:"url"`
				} `json:"share"`
			}); ok {
				p.URL = "https://opencode.ai/s/secret-link"
			}
		}
		return nil
	}
	if _, err := s.Share(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ev := range h.events {
		if strings.Contains(ev.Text, "secret-link") || strings.Contains(ev.Error, "secret-link") {
			t.Fatalf("a share url reached the event stream: %+v", ev)
		}
	}
}

// TestShareMutationPolicyIsReadable proves the capability source of truth.
func TestShareMutationPolicyIsReadable(t *testing.T) {
	_, off := shareSessionWith(t, false)
	if off.shareMutationAllowed() {
		t.Fatal("policy reported allowed while disabled")
	}
	_, on := shareSessionWith(t, true)
	if !on.shareMutationAllowed() {
		t.Fatal("policy reported disallowed while enabled")
	}
}
