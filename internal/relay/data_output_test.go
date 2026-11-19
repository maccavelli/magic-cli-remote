package relay

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// failOnWriter refuses any write that contains marker with errDiskFull.
type failOnWriter struct {
	marker string
	buf    bytes.Buffer
}

var errDiskFull = errors.New("disk full")

func (w *failOnWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		return 0, errDiskFull
	}
	return w.buf.Write(p)
}

// mcrelay's --print-only writes the unit or plist body, which is a command's
// result and not a status line: a failed write fails the command (0131 P5, R2).

func TestRelaySetupServicePrintOnlyReturnsAFailedWrite(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var f setupServiceFlags
	f.printOnly = true
	cfg, lvl, fmtv := "", "", ""
	w := &failOnWriter{marker: "[Unit]"}
	cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
	cmd.SetOut(w)

	err := runSetupService(cmd, f, cfg, lvl, fmtv)
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the service definition") {
		t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
	}
}

func TestRelaySetupServiceRefreshPrintOnlyReturnsAFailedWrite(t *testing.T) {
	testexec.SkipIfNoUnixServiceManager(t) // Windows refreshes a Task Scheduler task, covered by schtasks_test.go
	defer service.OverrideInstallOS("linux")()
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dir := t.TempDir()
	body, err := service.RenderUnit(service.Options{
		Product: "mcrelay", UnitName: "mcrelay", Description: "d",
		Binary: filepath.Join(dir, "mcrelay"), WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(cfgHome, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "mcrelay.service"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	var f setupServiceFlags
	f.refresh = true
	f.printOnly = true
	cfg, lvl, fmtv := "", "", ""

	var healthy bytes.Buffer
	cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
	cmd.SetOut(&healthy)
	if err := runSetupService(cmd, f, cfg, lvl, fmtv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(healthy.String(), "\n[Unit]\n") {
		t.Fatalf("refresh --print-only wrote %d bytes without a [Unit] section, want the unit body", healthy.Len())
	}

	w := &failOnWriter{marker: "[Unit]"}
	cmd = newSetupServiceCmd(&cfg, &lvl, &fmtv)
	cmd.SetOut(w)
	err = runSetupService(cmd, f, cfg, lvl, fmtv)
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the refreshed service definition") {
		t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
	}
}
