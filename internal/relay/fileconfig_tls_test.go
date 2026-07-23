package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTLSNormalizedMatrix(t *testing.T) {
	cases := []struct {
		name string
		in   TLSConfig
		want string
	}{
		{
			name: "le domains+email",
			in: TLSConfig{LetsEncrypt: LetsEncryptConfig{
				Domains: []string{"a.example.com"},
				Email:   "ops@example.com",
			}},
			want: TLSModeLetsEncrypt,
		},
		{
			name: "files when PEMs",
			in:   TLSConfig{CertFile: "c", KeyFile: "k"},
			want: TLSModeFiles,
		},
		{
			name: "off default",
			in:   TLSConfig{},
			want: TLSModeOff,
		},
		{
			name: "explicit off wins over PEMs",
			in:   TLSConfig{Mode: TLSModeOff, CertFile: "c", KeyFile: "k"},
			want: TLSModeOff,
		},
		{
			name: "explicit files",
			in:   TLSConfig{Mode: TLSModeFiles, CertFile: "c", KeyFile: "k"},
			want: TLSModeFiles,
		},
		{
			name: "domains without email not auto-le",
			in: TLSConfig{LetsEncrypt: LetsEncryptConfig{
				Domains: []string{"a.example.com"},
			}},
			want: TLSModeOff,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalized().Mode
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestLetsEncryptChallengeNormalized(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ACMEChallengeHTTP01},
		{"http-01", ACMEChallengeHTTP01},
		{"HTTP", ACMEChallengeHTTP01},
		{"http01", ACMEChallengeHTTP01},
		{"dns-01", ACMEChallengeDNS01},
		{"DNS01", ACMEChallengeDNS01},
		{"dns", ACMEChallengeDNS01},
		{"bogus", "bogus"},
	}
	for _, tc := range cases {
		got := (LetsEncryptConfig{Challenge: tc.in}).ChallengeNormalized()
		if got != tc.want {
			t.Fatalf("in %q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateRejectsBadACMEChallenge(t *testing.T) {
	cfg := DefaultsFile()
	cfg.Hosts = []HostEntry{{ID: "h1", Secret: "sixteen-chars-min-1"}}
	cfg.TLS.Mode = TLSModeLetsEncrypt
	cfg.TLS.LetsEncrypt.Domains = []string{"relay.example.com"}
	cfg.TLS.LetsEncrypt.Email = "ops@example.com"
	cfg.TLS.LetsEncrypt.Challenge = "tls-alpn-01"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected challenge reject")
	}
	cfg.TLS.LetsEncrypt.Challenge = ACMEChallengeDNS01
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLetsEncryptDirectory(t *testing.T) {
	if d := (LetsEncryptConfig{}).Directory(); d != "" {
		t.Fatalf("empty should be production default empty: %q", d)
	}
	if d := (LetsEncryptConfig{Staging: true}).Directory(); !strings.Contains(d, "staging") {
		t.Fatalf("staging: %q", d)
	}
	if d := (LetsEncryptConfig{
		Staging:      true,
		DirectoryURL: "https://custom.example/dir",
	}).Directory(); d != "https://custom.example/dir" {
		t.Fatalf("custom wins: %q", d)
	}
}

func TestACMECacheDir(t *testing.T) {
	fc := FileConfig{DataDir: "/data/mcrelay"}
	if got := fc.ACMECacheDir(); got != filepath.Join("/data/mcrelay", "acme") {
		t.Fatal(got)
	}
	fc.TLS.LetsEncrypt.CacheDir = "/custom/acme"
	if got := fc.ACMECacheDir(); got != "/custom/acme" {
		t.Fatal(got)
	}
}

func TestLoadLetsEncryptYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
listen:
  host: 0.0.0.0
  port: 443
tls:
  mode: letsencrypt
  letsencrypt:
    domains:
      - relay.example.com
      - "  alt.example.com  "
    email: ops@example.com
    staging: true
    http_port: 80
hosts:
  - id: devbox-1
    secret: sixteen-chars-min-1
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.Normalized().Mode != TLSModeLetsEncrypt {
		t.Fatalf("mode=%s", cfg.TLS.Mode)
	}
	if cfg.TLS.LetsEncrypt.Email != "ops@example.com" {
		t.Fatal(cfg.TLS.LetsEncrypt.Email)
	}
	if !cfg.TLS.LetsEncrypt.Staging {
		t.Fatal("staging")
	}
	if len(cfg.TLS.LetsEncrypt.Domains) < 1 {
		t.Fatal(cfg.TLS.LetsEncrypt.Domains)
	}
	if !strings.Contains(cfg.TLS.LetsEncrypt.Directory(), "staging") {
		t.Fatal(cfg.TLS.LetsEncrypt.Directory())
	}
}

func TestValidateLetsEncryptHTTPPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
tls:
  mode: letsencrypt
  letsencrypt:
    domains: [relay.example.com]
    email: ops@example.com
    http_port: 99999
hosts:
  - id: h
    secret: sixteen-chars-min-h
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{ConfigFile: path}); err == nil {
		t.Fatal("expected bad http_port")
	}
}

func TestValidateFilesModeNeedsPEMs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
tls:
  mode: files
hosts:
  - id: h
    secret: sixteen-chars-min-h
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{ConfigFile: path}); err == nil {
		t.Fatal("expected files mode PEM error")
	}
}

func TestValidateUnknownTLSMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
tls:
  mode: acme
hosts:
  - id: h
    secret: sixteen-chars-min-h
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{ConfigFile: path}); err == nil {
		t.Fatal("expected unknown mode")
	}
}

func TestValidateMismatchedCertKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
tls:
  cert_file: only-cert.pem
hosts:
  - id: h
    secret: sixteen-chars-min-h
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{ConfigFile: path}); err == nil {
		t.Fatal("expected cert/key pair error")
	}
}

func TestExpandDomainList(t *testing.T) {
	got := expandDomainList([]string{"a.com, b.com", "  c.com  ", ""})
	if len(got) != 3 || got[0] != "a.com" || got[1] != "b.com" || got[2] != "c.com" {
		t.Fatalf("%v", got)
	}
}

func TestParseAllowFlag(t *testing.T) {
	c, err := ParseAllowFlag("devbox-1:sixteen-chars-min-x")
	if err != nil {
		t.Fatal(err)
	}
	if c.HostID != "devbox-1" {
		t.Fatal(c.HostID)
	}
	if _, err := ParseAllowFlag("no-secret"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseAllowFlag("x:short"); err == nil {
		t.Fatal("expected short secret")
	}
	if _, err := ParseAllowFlag("bad id!:sixteen-chars-min-x"); err == nil {
		t.Fatal("expected bad id")
	}
}

func TestNonEmptyDomains(t *testing.T) {
	got := nonEmptyDomains([]string{" a.com ", "", "  ", "b.com"})
	if len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Fatalf("%v", got)
	}
}
