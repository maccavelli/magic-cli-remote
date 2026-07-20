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
		"Restart=on-failure",
		"NoNewPrivileges=true",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("unit missing %q\n%s", want, body)
		}
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

	res, err := service.Setup(service.Options{
		UnitName:   "mcremote-test",
		Binary:     src,
		UnitDir:    unitDir,
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
