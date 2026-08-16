package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

func TestRenderUnit(t *testing.T) {
	body, err := service.RenderUnit(service.Options{
		UnitName:   "mcremote",
		Binary:     "/home/mac/.local/bin/mcremote",
		ConfigPath: "/home/mac/.config/mcremote/config.yaml",
		DataDir:    "/home/mac/.local/share/mcremote",
		ListenHost: "0.0.0.0",
		ListenPort: 7531,
		LogLevel:   "info",
		LogFormat:  "text",
		PrintOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"ExecStart=/home/mac/.local/bin/mcremote serve",
		"--config /home/mac/.config/mcremote/config.yaml",
		"--data-dir /home/mac/.local/share/mcremote",
		"--listen-host 0.0.0.0",
		"--listen-port 7531",
		"WantedBy=default.target",
		"Restart=always",
		"RestartSec=5",
		"StartLimitIntervalSec=300",
		"StartLimitBurst=30",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"RestrictSUIDSGID=true",
		"LockPersonality=true",
		"RestrictRealtime=true",
		"ProtectKernelTunables=true",
		"ProtectControlGroups=true",
		"SystemCallArchitectures=native",
		"KillMode=control-group",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("unit missing %q\n%s", want, body)
		}
	}
	if hasUnitDirective(body, "UMask=0077") {
		t.Errorf("mcremote unit should not set UMask=0077\n%s", body)
	}
	for _, not := range []string{
		"PrivateDevices=true",
		"RestrictNamespaces=true",
		"KillMode=mixed",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"MemoryDenyWriteExecute=true",
	} {
		if hasUnitDirective(body, not) {
			t.Errorf("mcremote unit should not set %q\n%s", not, body)
		}
	}
}

func TestRenderUnitMcrelay(t *testing.T) {
	body, err := service.RenderUnit(service.Options{
		Product:    "mcrelay",
		UnitName:   "mcrelay",
		Binary:     "/home/mac/.local/bin/mcrelay",
		ConfigPath: "/home/mac/.config/mcrelay/config.yaml",
		PrintOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=/home/mac/.local/bin/mcrelay serve",
		"--config /home/mac/.config/mcrelay/config.yaml",
		"KillMode=mixed",
		"Restart=always",
		"RestartSec=5",
		"StartLimitIntervalSec=300",
		"StartLimitBurst=30",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"RestrictSUIDSGID=true",
		"LockPersonality=true",
		"RestrictRealtime=true",
		"ProtectKernelTunables=true",
		"ProtectControlGroups=true",
		"SystemCallArchitectures=native",
		"PrivateDevices=true",
		"RestrictNamespaces=true",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"MemoryDenyWriteExecute=true",
		"UMask=0077",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("mcrelay unit missing %q\n%s", want, body)
		}
	}
	if !strings.Contains(body, "Environment=PATH=") {
		t.Fatalf("mcrelay unit missing PATH:\n%s", body)
	}
	for _, agent := range []string{".grok/bin", ".opencode/bin", ".cache/kilo/bin", "flutter", "/go/bin"} {
		if strings.Contains(body, agent) {
			t.Errorf("mcrelay PATH must not include agent dir %q\n%s", agent, body)
		}
	}
	for _, not := range []string{
		"KillMode=control-group",
		"ProtectHome=true",
		"ProtectSystem=strict",
	} {
		if hasUnitDirective(body, not) {
			t.Errorf("mcrelay user unit should not set %q\n%s", not, body)
		}
	}
}

func hasUnitDirective(body, directive string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == directive {
			return true
		}
	}
	return false
}

func TestSetupWritesDefaultMcrelayConfig(t *testing.T) {
	// Asserts systemd unit directives, so pin the install OS rather than
	// inheriting the host's: on macOS Setup correctly writes a launchd
	// plist and these assertions read XML. Stub the service manager too —
	// this test is about unit-file content, and without the stub it drove
	// real launchctl on the developer's machine (MADR 0095 F8).
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideRunSystemctl(func(...string) error { return nil })()
	dir := t.TempDir()
	src := filepath.Join(dir, "mcrelay")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "mcrelay-config.yaml")
	res, err := service.Setup(service.Options{
		Product:    "mcrelay",
		UnitName:   "mcrelay-test",
		Binary:     src,
		UnitDir:    filepath.Join(dir, "units"),
		ConfigPath: cfgPath,
		Force:      true,
		NoEnable:   true,
		NoStart:    true,
		NoLinger:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.ConfigCreated {
		t.Fatal("expected mcrelay default config")
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "max_phones_per_host") {
		t.Fatalf("mcrelay defaults missing limits: %s", b)
	}
	if !strings.Contains(string(res.UnitBody), "--config") {
		t.Fatalf("unit should bake config path:\n%s", res.UnitBody)
	}
	if !strings.Contains(res.UnitBody, "KillMode=mixed") {
		t.Fatalf("mcrelay unit should use KillMode=mixed:\n%s", res.UnitBody)
	}
	if !strings.Contains(res.UnitBody, "PrivateDevices=true") {
		t.Fatalf("mcrelay unit missing PrivateDevices:\n%s", res.UnitBody)
	}
}

func TestSetupWritesUnitWithoutCopyingBinary(t *testing.T) {
	dir := t.TempDir()
	// Existing "installed" binary that setup-service should reference, not copy.
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(dir, "units")
	cfgPath := filepath.Join(dir, "config.yaml")

	res, err := service.Setup(service.Options{
		UnitName:   "mcremote-test",
		Binary:     src,
		UnitDir:    unitDir,
		ConfigPath: cfgPath,
		ListenHost: "127.0.0.1",
		ListenPort: 7531,
		Force:      true,
		NoEnable:   true,
		NoStart:    true,
		NoLinger:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UnitPath == "" {
		t.Fatal("empty unit path")
	}
	if res.Binary != src {
		t.Fatalf("Binary = %q, want %q", res.Binary, src)
	}
	if !res.ConfigCreated {
		t.Fatal("expected default config to be created")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config missing: %v", err)
	}
	cfgBody, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgBody), "listen:") {
		t.Fatalf("config body unexpected: %s", cfgBody)
	}
	// Second setup must not overwrite operator edits.
	if err := os.WriteFile(cfgPath, []byte("# kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res2, err := service.Setup(service.Options{
		UnitName:   "mcremote-test",
		Binary:     src,
		UnitDir:    unitDir,
		ConfigPath: cfgPath,
		Force:      true,
		NoEnable:   true,
		NoStart:    true,
		NoLinger:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.ConfigCreated {
		t.Fatal("must not recreate existing config")
	}
	kept, _ := os.ReadFile(cfgPath)
	if string(kept) != "# kept\n" {
		t.Fatalf("config was overwritten: %q", kept)
	}
	b, err := os.ReadFile(res.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), src) {
		t.Fatalf("unit should reference existing binary:\n%s", b)
	}

	// Without force should fail.
	_, err = service.Setup(service.Options{
		UnitName: "mcremote-test",
		Binary:   src,
		UnitDir:  unitDir,
		NoEnable: true,
		NoStart:  true,
		NoLinger: true,
	})
	if err == nil {
		t.Fatal("expected error without --force")
	}
}

func TestSetupRejectsMissingBinary(t *testing.T) {
	dir := t.TempDir()
	_, err := service.Setup(service.Options{
		UnitName: "mcremote-test",
		Binary:   filepath.Join(dir, "does-not-exist"),
		UnitDir:  filepath.Join(dir, "units"),
		// Real install path must refuse a missing ExecStart binary.
		Force:    true,
		NoEnable: true,
		NoStart:  true,
		NoLinger: true,
	})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "make install") {
		t.Fatalf("error should mention make install: %v", err)
	}
}

func TestRenderUnitAllowsMissingBinary(t *testing.T) {
	// Preview must work in CI without a preinstalled mcremote.
	body, err := service.RenderUnit(service.Options{
		UnitName:   "mcremote",
		Binary:     "/usr/local/bin/mcremote-does-not-exist-yet",
		ListenHost: "127.0.0.1",
		ListenPort: 7531,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "ExecStart=/usr/local/bin/mcremote-does-not-exist-yet serve") {
		t.Fatalf("unexpected unit:\n%s", body)
	}
}

func TestSystemdEscaping(t *testing.T) {
	body, err := service.RenderUnit(service.Options{
		UnitName:         "mcremote",
		Binary:           "/opt/100% tools/mcremote",
		WorkingDirectory: `/home/x/My Projects/tail\`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// % must be doubled everywhere or systemd expands it as a specifier.
	if !strings.Contains(body, `ExecStart="/opt/100%% tools/mcremote" serve`) {
		t.Errorf("percent/space not escaped in ExecStart:\n%s", body)
	}
	// Trailing backslash must not eat the closing quote.
	if !strings.Contains(body, `WorkingDirectory="/home/x/My Projects/tail\\"`) {
		t.Errorf("backslash not escaped in WorkingDirectory:\n%s", body)
	}
	// PATH is quoted like every other value.
	if !strings.Contains(body, "Environment=PATH=") {
		t.Errorf("missing PATH line:\n%s", body)
	}
}

func TestRenderOmitsListenAndRuntimeDirWhenUnset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	body, err := service.RenderUnit(service.Options{UnitName: "mcremote", Binary: "/usr/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "--listen-host") || strings.Contains(body, "--listen-port") {
		t.Errorf("listen flags baked in without being requested:\n%s", body)
	}
	if strings.Contains(body, "XDG_RUNTIME_DIR=") {
		t.Errorf("empty XDG_RUNTIME_DIR must be omitted (it would override systemd's own):\n%s", body)
	}
}

func TestExtraEnvironValidation(t *testing.T) {
	for _, bad := range []string{"garbage", "1BAD=x", "A=b\nInjected=1"} {
		_, err := service.RenderUnit(service.Options{UnitName: "m", Binary: "/usr/bin/true", ExtraEnviron: []string{bad}})
		if err == nil {
			t.Errorf("ExtraEnviron %q accepted, want error", bad)
		}
	}
	body, err := service.RenderUnit(service.Options{UnitName: "m", Binary: "/usr/bin/true", ExtraEnviron: []string{"FOO=a b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `Environment=FOO="a b"`) {
		t.Errorf("env value with space not quoted:\n%s", body)
	}
}

func TestUnitNameValidation(t *testing.T) {
	for _, bad := range []string{"has space", "pct%name", "a/b", "x.service"} {
		_, err := service.RenderUnit(service.Options{UnitName: bad, Binary: "/usr/bin/true"})
		if err == nil {
			t.Errorf("unit name %q accepted, want error", bad)
		}
	}
}

func TestSetupIdempotentRerun(t *testing.T) {
	// Asserts the systemd unit file at units/<name>.service, so pin the install
	// OS rather than inheriting the host's. On macOS Setup correctly writes a
	// launchd plist under a different name, and the assertion below read an
	// absent file and reported "force did not rewrite" against empty content.
	// Stub systemctl too — this test is about unit-file content, not about
	// driving a real service manager (mirrors TestDarwinSetupAgentOrder).
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideRunSystemctl(func(...string) error { return nil })()

	dir := t.TempDir()
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := service.Options{
		UnitName: "mcremote-idem",
		Binary:   src,
		UnitDir:  filepath.Join(dir, "units"),
		NoEnable: true, NoStart: true, NoLinger: true,
	}
	if _, err := service.Setup(opts); err != nil {
		t.Fatal(err)
	}
	// Identical rerun without --force: success, no rewrite.
	res, err := service.Setup(opts)
	if err != nil {
		t.Fatalf("identical rerun should succeed: %v", err)
	}
	if !res.AlreadyExisted || !res.Unchanged {
		t.Fatalf("res = %+v, want AlreadyExisted+Unchanged", res)
	}
	// Different content without --force: refused.
	opts2 := opts
	opts2.LogLevel = "debug"
	if _, err := service.Setup(opts2); err == nil {
		t.Fatal("differing rerun without force should error")
	}
	// --force overwrites.
	opts2.Force = true
	if _, err := service.Setup(opts2); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "units", "mcremote-idem.service"))
	if !strings.Contains(string(b), "--log-level debug") {
		t.Fatalf("force did not rewrite:\n%s", b)
	}
}

func TestSetupRejectsGoRunBinary(t *testing.T) {
	tmp := filepath.Join(os.TempDir(), "go-build12345", "b001", "exe", "mcremote")
	_, err := service.Setup(service.Options{
		UnitName: "m", Binary: tmp, UnitDir: t.TempDir(),
		NoEnable: true, NoStart: true, NoLinger: true,
	})
	if err == nil || !strings.Contains(err.Error(), "make install") {
		t.Fatalf("go-build temp binary must be rejected with install hint, got %v", err)
	}
}

func TestSetupSecretEnvTightensMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := service.Setup(service.Options{
		UnitName:     "mcremote-env",
		Binary:       src,
		UnitDir:      filepath.Join(dir, "units"),
		ExtraEnviron: []string{"TOKEN=secret"},
		NoEnable:     true, NoStart: true, NoLinger: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(res.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("unit with --env secrets is %o, want 0600", st.Mode().Perm())
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(dir, "units")
	if _, err := service.Setup(service.Options{
		UnitName: "mcremote-rm", Binary: src, UnitDir: unitDir,
		NoEnable: true, NoStart: true, NoLinger: true,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := service.Remove(service.Options{UnitName: "mcremote-rm", UnitDir: unitDir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Removed {
		t.Fatalf("res = %+v, want Removed", res)
	}
	if _, err := os.Stat(filepath.Join(unitDir, "mcremote-rm.service")); !os.IsNotExist(err) {
		t.Fatal("unit file still present after Remove")
	}
}

// A newline in any free-text unit field must be rejected: systemdQuote cannot
// escape a newline inside a unit line, so it would terminate the assignment and
// let the remainder inject arbitrary directives (e.g. an extra ExecStartPre=
// that runs before the daemon). Regression for the systemd unit-injection gap.
func TestRenderUnitRejectsControlChars(t *testing.T) {
	inject := "127.0.0.1\nExecStartPre=/bin/touch /tmp/pwned"
	fields := map[string]service.Options{
		"listen-host":       {ListenHost: inject},
		"service-config":    {ConfigPath: inject},
		"data-dir":          {DataDir: inject},
		"working-directory": {WorkingDirectory: inject},
		"log-level":         {LogLevel: inject},
		"binary":            {Binary: inject},
	}
	for name, opts := range fields {
		t.Run(name, func(t *testing.T) {
			body, err := service.RenderUnit(opts)
			if err == nil {
				t.Fatalf("RenderUnit accepted a newline in %s; rendered:\n%s", name, body)
			}
			if !strings.Contains(err.Error(), "control characters") {
				t.Fatalf("error = %v, want a control-characters rejection", err)
			}
		})
	}
}
