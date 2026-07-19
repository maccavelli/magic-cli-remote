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

func TestSetupPrintOnlyAndWrite(t *testing.T) {
	dir := t.TempDir()
	// Fake binary to install.
	src := filepath.Join(dir, "mcremote-src")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(dir, "units")
	installTo := filepath.Join(dir, "bin", "mcremote")

	res, err := service.Setup(service.Options{
		UnitName:       "mcremote-test",
		Binary:         src,
		InstallBin:     true,
		InstallBinPath: installTo,
		UnitDir:        unitDir,
		ListenHost:     "127.0.0.1",
		ListenPort:     7531,
		Force:          true,
		NoEnable:       true,
		NoStart:        true,
		NoLinger:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UnitPath == "" {
		t.Fatal("empty unit path")
	}
	b, err := os.ReadFile(res.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), installTo) {
		t.Fatalf("unit should reference installed binary:\n%s", b)
	}
	if _, err := os.Stat(installTo); err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}

	// Without force should fail.
	_, err = service.Setup(service.Options{
		UnitName:       "mcremote-test",
		Binary:         src,
		InstallBin:     false,
		UnitDir:        unitDir,
		NoEnable:       true,
		NoStart:        true,
		NoLinger:       true,
	})
	if err == nil {
		t.Fatal("expected error without --force")
	}
}
