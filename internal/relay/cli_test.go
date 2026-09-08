package relay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// 0115 P8: in-process cobra execution for the command tree — the audit
// measured cli.go's constructors at 0%.

func runCLI(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func hermeticConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "mcrelay")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "hosts:\n  - id: h1\n    secret: 0123456789abcdef\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCLIVersionCommand(t *testing.T) {
	out, err := runCLI(t, nil, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "mcrelay ") {
		t.Fatalf("version output %q", out)
	}
	if VersionString() == "" {
		t.Fatal("VersionString empty")
	}
}

func TestSetBuildKind(t *testing.T) {
	prev := cliBuildKind
	t.Cleanup(func() { cliBuildKind = prev })
	SetBuildKind("")
	if cliBuildKind != prev {
		t.Fatal("empty kind must not overwrite")
	}
	SetBuildKind("release")
	if cliBuildKind != "release" {
		t.Fatalf("got %q", cliBuildKind)
	}
}

func TestCLIPathsJSON(t *testing.T) {
	testexec.SkipIfNoXDG(t)
	home := hermeticConfig(t)
	out, err := runCLI(t, map[string]string{"XDG_CONFIG_HOME": home}, "paths", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("paths --json not JSON: %v\n%s", err, out)
	}
	if got["product"] != "mcrelay" {
		t.Fatalf("product=%v", got["product"])
	}
	if got["data_dir"] == "" {
		t.Fatal("data_dir empty")
	}
}

func TestCLIPathsText(t *testing.T) {
	testexec.SkipIfNoXDG(t)
	home := hermeticConfig(t)
	out, err := runCLI(t, map[string]string{"XDG_CONFIG_HOME": home}, "paths")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "product:            mcrelay") {
		t.Fatalf("paths output %q", out)
	}
}

// TestCLIServeInvalidConfig: serve must fail fast on a config with no hosts —
// the error path, no listener ever started.
func TestCLIServeInvalidConfig(t *testing.T) {
	home := t.TempDir() // empty: no config file, no hosts anywhere
	_, err := runCLI(t, map[string]string{
		"XDG_CONFIG_HOME": home,
		"MCRELAY_HOSTS":   "",
	}, "serve")
	if err == nil || !strings.Contains(err.Error(), "at least one host") {
		t.Fatalf("err=%v; want the no-hosts refusal", err)
	}
}

// TestCLIPathsWithNoHostsConfigured is the regression test for MADR 0154.
//
// paths is a diagnostic: it answers "where does this thing keep its files",
// which does not depend on whether any host may register. It used to inherit
// serve's precondition through Load and exit 1 on a relay with no hosts
// configured -- the state an operator is in precisely when they are most
// likely to ask.
//
// The config is passed with --config rather than placed under
// XDG_CONFIG_HOME, and that is deliberate: Windows resolves its config
// directory from a Known Folders syscall, so XDG_CONFIG_HOME cannot redirect
// it there (MADR 0116 D3, and why TestCLIPathsJSON skips on Windows). A test
// that relied on discovery would pass on this host only because no real
// mcrelay config happens to exist, and would start passing for the wrong
// reason the moment one did. --config is honoured identically on every
// platform.
//
// The assertion covers the output as well as the error: a command that exited
// 0 and printed nothing would satisfy the weaker half.
func TestCLIPathsWithNoHostsConfigured(t *testing.T) {
	// The directory is created through the product's own primitive rather than
	// t.TempDir() alone. Load refuses a config any other trustee can read, and
	// on Windows that is an ACL test, not a mode test: a file under %TEMP%
	// inherits access for SYSTEM and Administrators and is correctly rejected.
	// EnsurePrivateDir is what the daemon uses for its own directories, so this
	// asks of the fixture exactly the privacy the product demands of itself.
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	// Valid in every respect except that it configures no hosts.
	body := "listen:\n  port: 8443\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, map[string]string{"MCRELAY_HOSTS": ""}, "paths", "--config", cfg)
	if err != nil {
		t.Fatalf("paths failed on a config with no hosts: %v", err)
	}
	if !strings.Contains(out, "data_dir:") {
		t.Fatalf("paths printed no data_dir; got: %s", out)
	}
}

// TestCLIRootHelp: bare root prints help (RunE fallthrough).
func TestCLIRootHelp(t *testing.T) {
	out, err := runCLI(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mcrelay is a public-edge join router") {
		t.Fatalf("help output %q", out[:min(len(out), 120)])
	}
}
