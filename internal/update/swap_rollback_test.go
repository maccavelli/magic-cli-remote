package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// MADR 0125 D5 / C2 — the reported harm.
//
// A failed update used to leave the daemon stopped while the process exited as
// though the rollback had worked: the restart error was written to a log line
// and dropped. "Rolled back and running" and "rolled back and stopped" are
// materially different outcomes and must be distinguishable to the caller.

func stagedAndDest(t *testing.T) (staged, dest string) {
	t.Helper()
	dir := t.TempDir()
	staged = filepath.Join(dir, "staged")
	dest = filepath.Join(dir, "mcremote")
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return staged, dest
}

func TestRollbackRestoresBinaryAndReportsAStoppedService(t *testing.T) {
	staged, dest := stagedAndDest(t)

	startErr := errors.New("bootstrap: exit status 37")
	var logs []string
	svc := FuncService{
		IsActiveFn:    func(string) (bool, error) { return true, nil },
		IsInstalledFn: func(string) (bool, error) { return true, nil },
		StopFn:        func(string) error { return nil },
		StartFn:       func(string) error { return startErr },
	}

	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		WasActive:      true,
		Service:        svc,
		Log:            func(s string) { logs = append(logs, s) },
	})

	if err == nil {
		t.Fatal("a failed start must surface, not be logged and forgotten")
	}
	// The distinguishing claim: the caller can tell the daemon is down.
	if !strings.Contains(err.Error(), "NOT running") {
		t.Fatalf("error must say the service is not running, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("error must say the binary was rolled back, got: %v", err)
	}

	// The binary half of the rollback still has to have happened.
	body, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(body) != "old" {
		t.Fatalf("previous binary not restored: %q", body)
	}

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "setup-service --force") {
		t.Errorf("the user needs to be told how to recover:\n%s", joined)
	}
}

func TestRollbackThatSucceedsSaysTheServiceIsRunning(t *testing.T) {
	staged, dest := stagedAndDest(t)

	starts := 0
	var logs []string
	svc := FuncService{
		IsActiveFn:    func(string) (bool, error) { return true, nil },
		IsInstalledFn: func(string) (bool, error) { return true, nil },
		StopFn:        func(string) error { return nil },
		StartFn: func(string) error {
			starts++
			if starts == 1 {
				return errors.New("first start fails, forcing the rollback")
			}
			return nil // the rollback's restart succeeds
		},
	}

	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		WasActive:      true,
		Service:        svc,
		Log:            func(s string) { logs = append(logs, s) },
	})

	// Still an error — the update did not succeed — but the daemon is up, and
	// the message must not claim otherwise.
	if err == nil {
		t.Fatal("the update failed; that must still be reported")
	}
	if strings.Contains(err.Error(), "NOT running") {
		t.Fatalf("the rollback restarted the service; the error must not say otherwise: %v", err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "running again") {
		t.Errorf("a successful rollback should say so:\n%s", joined)
	}
}

// C1: the 300ms "settle" sleep is gone. Stop now waits on the real condition,
// and a constant standing in for that wait is this bug being reintroduced.
func TestSwapDoesNotSleepToSettleTheService(t *testing.T) {
	staged, dest := stagedAndDest(t)

	var slept []int64
	svc := FuncService{
		IsActiveFn:    func(string) (bool, error) { return true, nil },
		IsInstalledFn: func(string) (bool, error) { return true, nil },
		StopFn:        func(string) error { return nil },
		StartFn:       func(string) error { return nil },
	}

	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		WasActive:      true,
		Service:        svc,
		Sleep:          func(d time.Duration) { slept = append(slept, int64(d/time.Millisecond)) },
		Log:            func(string) {},
	})
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	for _, ms := range slept {
		if ms >= 300 {
			t.Fatalf("a %dms settle sleep is back; Stop must wait on the condition instead", ms)
		}
	}
}
