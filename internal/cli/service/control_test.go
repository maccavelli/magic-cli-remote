package service

import (
	"errors"
	"strings"
	"testing"
)

func TestIsActiveLinux(t *testing.T) {
	defer OverrideInstallOS("linux")()
	prev := runSystemctlCapture
	defer func() { runSystemctlCapture = prev }()

	runSystemctlCapture = func(args ...string) (string, error) {
		if strings.Join(args, " ") == "--user is-active mcremote.service" {
			return "active\n", nil
		}
		return "", errors.New("unexpected")
	}
	ok, err := IsActive("mcremote")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}

	runSystemctlCapture = func(args ...string) (string, error) {
		return "inactive\n", errors.New("exit 3")
	}
	ok, err = IsActive("mcremote")
	if err != nil || ok {
		t.Fatalf("inactive: ok=%v err=%v", ok, err)
	}
}

func TestIsActiveDarwinExitedNotActive(t *testing.T) {
	// Mirrors install-binary.sh: state=exited / pid leftover after SIGTERM.
	defer OverrideInstallOS("darwin")()
	prev := runLaunchctlCapture
	defer func() { runLaunchctlCapture = prev }()

	runLaunchctlCapture = func(args ...string) (string, error) {
		return "state = exited\npid = 12345\n", nil
	}
	ok, err := IsActive("mcremote")
	if err != nil || ok {
		t.Fatalf("exited with residual pid: ok=%v err=%v", ok, err)
	}
	runLaunchctlCapture = func(args ...string) (string, error) {
		return "state = not running\n", nil
	}
	ok, err = IsActive("mcremote")
	if err != nil || ok {
		t.Fatalf("not running: ok=%v err=%v", ok, err)
	}
	runLaunchctlCapture = func(args ...string) (string, error) {
		return "state = running\npid = 42\n", nil
	}
	ok, err = IsActive("mcremote")
	if err != nil || !ok {
		t.Fatalf("running: ok=%v err=%v", ok, err)
	}
}

func TestStopStartLinux(t *testing.T) {
	defer OverrideInstallOS("linux")()
	var calls []string
	defer OverrideRunSystemctl(func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	})()
	if err := Stop("mcremote"); err != nil {
		t.Fatal(err)
	}
	if err := Start("mcremote"); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 2 {
		t.Fatalf("calls=%v", calls)
	}
}

// MADR 0072 P2: after bootout the job is not loaded — Start must bootstrap
// then kickstart, not only kickstart.
func TestStartDarwinBootstrapsWhenNotLoaded(t *testing.T) {
	defer OverrideInstallOS("darwin")()
	prevCap := runLaunchctlCapture
	defer func() { runLaunchctlCapture = prevCap }()

	var calls []string
	runLaunchctlCapture = func(args ...string) (string, error) {
		calls = append(calls, "cap:"+strings.Join(args, " "))
		// print fails → not loaded
		return "", errors.New("Could not find service")
	}
	defer OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	})()

	if err := Start("mcremote"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, " | ")
	if !strings.Contains(joined, "bootstrap") {
		t.Fatalf("expected bootstrap when not loaded, calls=%v", calls)
	}
	if !strings.Contains(joined, "kickstart") {
		t.Fatalf("expected kickstart after bootstrap, calls=%v", calls)
	}
}

func TestStartDarwinKickstartWhenLoaded(t *testing.T) {
	defer OverrideInstallOS("darwin")()
	prevCap := runLaunchctlCapture
	defer func() { runLaunchctlCapture = prevCap }()

	var calls []string
	runLaunchctlCapture = func(args ...string) (string, error) {
		calls = append(calls, "cap:"+strings.Join(args, " "))
		return "state = running\npid = 1\n", nil
	}
	defer OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	})()

	if err := Start("mcremote"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, " | ")
	if strings.Contains(joined, "bootstrap") {
		t.Fatalf("loaded job must not bootstrap, calls=%v", calls)
	}
	if !strings.Contains(joined, "kickstart") {
		t.Fatalf("expected kickstart, calls=%v", calls)
	}
}

func TestProbeStatusDarwinNotLoadedHint(t *testing.T) {
	defer OverrideInstallOS("darwin")()
	prevCap := runLaunchctlCapture
	defer func() { runLaunchctlCapture = prevCap }()
	runLaunchctlCapture = func(args ...string) (string, error) {
		return "", errors.New("not found")
	}
	// Plist may or may not exist on the real HOME; only assert loaded/active
	// and that a non-empty hint is produced when not active.
	st := ProbeStatus("mcremote")
	if st.Loaded || st.Active {
		t.Fatalf("want not loaded/active, got %+v", st)
	}
	if st.Hint == "" {
		t.Fatal("expected recovery hint when down")
	}
}
