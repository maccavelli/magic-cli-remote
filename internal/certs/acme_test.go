package certs

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureACMEValidation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	cases := []struct {
		name string
		opts ACMEOptions
		want string
	}{
		{
			name: "no domains",
			opts: ACMEOptions{
				Email:      "a@b.c",
				StorageDir: dir,
			},
			want: "no domains",
		},
		{
			name: "whitespace-only domains",
			opts: ACMEOptions{
				Domains:    []string{"  ", ""},
				Email:      "a@b.c",
				StorageDir: dir,
			},
			want: "no domains",
		},
		{
			name: "no email",
			opts: ACMEOptions{
				Domains:    []string{"example.com"},
				StorageDir: dir,
			},
			want: "no account email",
		},
		{
			name: "whitespace email",
			opts: ACMEOptions{
				Domains:    []string{"example.com"},
				Email:      "   ",
				StorageDir: dir,
			},
			want: "no account email",
		},
		{
			name: "no storage",
			opts: ACMEOptions{
				Domains: []string{"example.com"},
				Email:   "a@b.c",
			},
			want: "no storage dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EnsureACME(ctx, tc.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestEnsureACMEManageFailsCleanly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	_, err := EnsureACME(ctx, ACMEOptions{
		Domains:      []string{"  unlikely-acme-test.invalid  "},
		Email:        "test@example.com",
		DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
		StorageDir:   filepath.Join(dir, "acme"),
		Timeout:      1500 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected manage failure for invalid domain")
	}
	if !strings.Contains(err.Error(), "manage") {
		t.Fatalf("want manage error, got %v", err)
	}
}

func TestACMELoggerNonNil(t *testing.T) {
	logger := acmeLogger(false)
	if logger == nil {
		t.Fatal("acmeLogger(false) returned nil")
	}
}

func TestACMELoggerVerboseNonNil(t *testing.T) {
	logger := acmeLogger(true)
	if logger == nil {
		t.Fatal("acmeLogger(true) returned nil")
	}
}

func TestACMEBundleTLSConfig(t *testing.T) {
	b := &ACMEBundle{}
	cfg := b.TLSConfig()
	if cfg == nil {
		t.Fatal("TLSConfig() returned nil")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS12)
	}
	if len(cfg.NextProtos) == 0 || cfg.NextProtos[0] != "h2" {
		t.Fatalf("NextProtos = %v, want h2 first", cfg.NextProtos)
	}
}

func TestACMEBundleCloseNil(t *testing.T) {
	var b *ACMEBundle
	b.Close()
}

func TestACMEBundleCloseNilCache(t *testing.T) {
	b := &ACMEBundle{Domains: []string{"example.com"}}
	b.Close()
}
