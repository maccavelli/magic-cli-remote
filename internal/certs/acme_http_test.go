package certs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureACMEHTTPValidation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	cases := []struct {
		name string
		opts ACMEHTTPOptions
		want string
	}{
		{
			name: "no domains",
			opts: ACMEHTTPOptions{Email: "a@b.c", StorageDir: dir},
			want: "no domains",
		},
		{
			name: "whitespace-only domains",
			opts: ACMEHTTPOptions{
				Domains:    []string{"  ", ""},
				Email:      "a@b.c",
				StorageDir: dir,
			},
			want: "no domains",
		},
		{
			name: "no email",
			opts: ACMEHTTPOptions{
				Domains:    []string{"example.com"},
				StorageDir: dir,
			},
			want: "no account email",
		},
		{
			name: "whitespace email",
			opts: ACMEHTTPOptions{
				Domains:    []string{"example.com"},
				Email:      "   ",
				StorageDir: dir,
			},
			want: "no account email",
		},
		{
			name: "no storage",
			opts: ACMEHTTPOptions{
				Domains: []string{"example.com"},
				Email:   "a@b.c",
			},
			want: "no storage dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EnsureACMEHTTP(ctx, tc.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestEnsureACMEHTTPTrimsDomains(t *testing.T) {
	// Invalid public name + short timeout: fails after validation, proving
	// whitespace domains were accepted into the manage list (not rejected as empty).
	ctx := context.Background()
	dir := t.TempDir()
	_, err := EnsureACMEHTTP(ctx, ACMEHTTPOptions{
		Domains:      []string{"  unlikely-mcrelay-test.invalid  "},
		Email:        "test@example.com",
		DirectoryURL: HTTPLetsEncryptStaging,
		StorageDir:   filepath.Join(dir, "acme"),
		Timeout:      1500 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected manage failure")
	}
	if !strings.Contains(err.Error(), "manage") {
		t.Fatalf("want manage error, got %v", err)
	}
}

func TestEnsureACMEHTTPManageFailsCleanly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	_, err := EnsureACMEHTTP(ctx, ACMEHTTPOptions{
		Domains:      []string{"unlikely-mcrelay-test.invalid"},
		Email:        "test@example.com",
		DirectoryURL: HTTPLetsEncryptStaging,
		StorageDir:   filepath.Join(dir, "acme"),
		Timeout:      2 * time.Second,
		HTTPPort:     18765, // non-standard; still won't complete for .invalid
	})
	if err == nil {
		t.Fatal("expected manage failure for invalid domain")
	}
	// Must not leave the process wedged; a second call with bad input still validates.
	_, err2 := EnsureACMEHTTP(ctx, ACMEHTTPOptions{
		Domains:    nil,
		Email:      "a@b.c",
		StorageDir: dir,
	})
	if err2 == nil || !strings.Contains(err2.Error(), "no domains") {
		t.Fatalf("expected validation after failed manage, got %v (first=%v)", err2, err)
	}
	if errors.Is(err, context.Canceled) {
		// timeout is fine; just ensure we got *an* error
	}
}

func TestHTTPLetsEncryptStagingConstant(t *testing.T) {
	if !strings.Contains(HTTPLetsEncryptStaging, "staging") {
		t.Fatalf("staging URL unexpected: %s", HTTPLetsEncryptStaging)
	}
}
