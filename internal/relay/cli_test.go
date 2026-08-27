package relay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
