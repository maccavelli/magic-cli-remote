package grok

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

const sampleEntry = `{"https://auth.x.ai::b1a0":{"key":"K","auth_mode":"oauth",` +
	`"refresh_token":"RT","expires_at":"2026-08-21T12:00:00Z","email":"u@example.test"}}`

func TestGrokAdapterPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	t.Setenv("HOME", "/tmp/decoy")

	ad := NewCredentialAdapter("grok")
	live, err := ad.LivePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "auth.json"); live != want {
		t.Fatalf("live = %q, want %q", live, want)
	}
	lock, err := ad.NativeLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if lock != live+".lock" {
		t.Fatalf("lock = %q, want auth.json.lock — grok's own writer honors it", lock)
	}
	if env := ad.PendingEnv("/pending"); len(env) != 1 || env[0] != "GROK_HOME=/pending" {
		t.Fatalf("pending env = %v", env)
	}
}

// TestGrokAdapterValidate proves the issuer-keyed credential is understood and
// that a Grok session is reported as non-revocable: `grok logout` clears the
// local file through auth_manager.clear() and makes no revoke call, so a byte
// backup still works and a clone probe is safe (MADR 0074 §17.5).
func TestGrokAdapterValidate(t *testing.T) {
	ad := NewCredentialAdapter("grok")
	meta, err := ad.Validate(context.Background(), []byte(sampleEntry))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Mode != authModeOAuth {
		t.Errorf("mode = %q, want %q", meta.Mode, authModeOAuth)
	}
	if meta.Revocable {
		t.Error("grok logout does not revoke server-side; a clone probe is safe")
	}
	want, _ := time.Parse(time.RFC3339, "2026-08-21T12:00:00Z")
	if !meta.ExpiresAt.Equal(want) {
		t.Errorf("expires = %v, want %v", meta.ExpiresAt, want)
	}
}

func TestGrokAdapterRejectsUnusable(t *testing.T) {
	ad := NewCredentialAdapter("grok")
	for _, body := range []string{
		`nope`,
		`{}`,
		`{"issuer::c":{}}`,
		`{"issuer::c":{"key":"","refresh_token":""}}`,
	} {
		if _, err := ad.Validate(context.Background(), []byte(body)); err == nil {
			t.Errorf("accepted unusable credential %q", body)
		}
	}
}

// TestGrokAdapterValidateNeverEchoesSecrets proves a rejection cannot quote the
// credential it rejected.
func TestGrokAdapterValidateNeverEchoesSecrets(t *testing.T) {
	const sentinel = "SENTINELrefreshTOKEN"
	ad := NewCredentialAdapter("grok")
	_, err := ad.Validate(context.Background(), []byte(`{"bad`+sentinel+`"`))
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error leaked input: %v", err)
	}
}

// TestGrokAdapterFreshnessPrefersLaterExpiry mirrors grok's own rule: never
// persist an older expiry over a newer one (MADR 0074 F12).
func TestGrokAdapterFreshnessPrefersLaterExpiry(t *testing.T) {
	ad := NewCredentialAdapter("grok")
	ctx := context.Background()
	older, err := ad.Validate(ctx, []byte(strings.Replace(sampleEntry, "2026-08-21T12:00:00Z", "2026-08-20T12:00:00Z", 1)))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := ad.Validate(ctx, []byte(sampleEntry))
	if err != nil {
		t.Fatal(err)
	}
	if !newer.Fresher(older) {
		t.Error("a later expiry must be fresher")
	}
	if older.Fresher(newer) {
		t.Error("an older expiry must never roll the credential backward")
	}
}

func TestGrokAdapterSatisfiesInterface(t *testing.T) {
	var _ providerauth.Adapter = NewCredentialAdapter("grok")
}
