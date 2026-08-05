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
