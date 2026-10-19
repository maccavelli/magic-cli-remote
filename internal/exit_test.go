package internal

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestUpdateFailureIsReportedOnce runs each product's real `update` with
// contradictory flags, which the library refuses before it builds an updater:
// no network, on every host. It reports the failure as one `update failed:`
// line, and main must exit 1 without adding its own `error:` line (MADR 0054
// PLAN S3, Deviation 1).
func TestUpdateFailureIsReportedOnce(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), product)
			if runtime.GOOS == "windows" {
				bin += ".exe"
			}
			if out, err := exec.Command("go", "build", "-o", bin, "../cmd/"+product).CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
			cmd := exec.Command(bin, "update", "--check", "--yes")
			var stderr strings.Builder
			cmd.Stderr = &stderr
			stdout, err := cmd.Output()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("exit = %v, want status 1\nstderr:\n%s", err, stderr.String())
			}
			if len(stdout) != 0 {
				t.Errorf("stdout = %q, want it empty without --json", stdout)
			}
			if n := strings.Count(stderr.String(), "update failed:"); n != 1 {
				t.Errorf("stderr has %d `update failed:` lines, want 1:\n%s", n, stderr.String())
			}
			if strings.Contains(stderr.String(), "error: ") {
				t.Errorf("main added its own error line:\n%s", stderr.String())
			}
		})
	}
}
