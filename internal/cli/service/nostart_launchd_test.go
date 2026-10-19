//go:build unix

package service_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// TestNoStartLeavesARunningLaunchdJobAlone is MADR 0201 D22 (0073 §6.2).
// setup-service --no-start tore the running job down unconditionally and
// gated only the bootstrap, so it stopped a running service and left it
// stopped. Under --no-start the plist is written and the job is not touched.
func TestNoStartLeavesARunningLaunchdJobAlone(t *testing.T) {
	restore := service.OverrideInstallOS("darwin")
	defer restore()

	var calls [][]string
	booted := false
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		if len(args) > 0 && args[0] == "bootout" {
			booted = true
		}
		return nil
	})
	defer restoreLC()
	// The job is loaded and running until something boots it out.
	restoreCap := service.OverrideRunLaunchctlCapture(func(...string) (string, error) {
		if booted {
			return "", errors.New("launchctl: no such service")
		}
		return "state = running\n", nil
	})
	defer restoreCap()

	dir := t.TempDir()
	src := testexec.WriteShellStub(t, filepath.Join(dir, "mcremote"), "")
	plistDir := filepath.Join(dir, "LaunchAgents")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	res, err := service.Setup(service.Options{
		UnitName:   "mcremote",
		Binary:     src,
		UnitDir:    plistDir,
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Force:      true,
		NoStart:    true,
	})
	os.Stderr = stderr
	_ = w.Close()
	printed, _ := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range calls {
		if len(c) > 0 && (c[0] == "bootout" || c[0] == "bootstrap" || c[0] == "kickstart") {
			t.Errorf("--no-start ran launchctl %s", strings.Join(c, " "))
		}
	}
	if res.Started {
		t.Error("Started = true under --no-start")
	}
	if _, err := os.Stat(res.UnitPath); err != nil {
		t.Errorf("plist not written under --no-start: %v", err)
	}
	if !strings.Contains(string(printed), "keeps its current definition until restarted") {
		t.Errorf("no note that the running job keeps its old definition; stderr:\n%s", printed)
	}
}
