package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// fakeBackend answers ProbeStatus's probes. A managed update never reaches
// it, so its lifecycle actions fail.
type fakeBackend struct {
	installed, enabled, running bool
	err                         error
}

func (f fakeBackend) Installed(context.Context, string) (bool, error) { return f.installed, f.err }
func (f fakeBackend) Enabled(context.Context, string) (bool, error)   { return f.enabled, f.err }
func (f fakeBackend) Running(context.Context, string) (bool, error)   { return f.running, f.err }
func (fakeBackend) Stop(context.Context, string) error                { return errors.New("not in a probe") }
func (fakeBackend) Start(context.Context, string) error               { return errors.New("not in a probe") }
func (fakeBackend) WaitHealthy(context.Context, string) error         { return errors.New("not in a probe") }

func withProbeBackend(t *testing.T, b Backend, err error) {
	t.Helper()
	prev := newProbeBackend
	newProbeBackend = func(string, string) (Backend, error) { return b, err }
	t.Cleanup(func() { newProbeBackend = prev })
}

// TestProbeStatusReadsTheBackend: doctor reports what the update's backend
// reports, so the two cannot disagree (MADR 0054 D6). macOS reads "loaded"
// from the backend too: TestProbeStatusOnMacOSReadsOnce.
func TestProbeStatusReadsTheBackend(t *testing.T) {
	probeErr := errors.New("systemctl show failed")
	for _, tc := range []struct {
		name     string
		os       string
		backend  fakeBackend
		want     Status
		wantHint string
	}{
		{"absent", "linux", fakeBackend{},
			Status{}, "unit missing"},
		{"disabled and stopped", "linux", fakeBackend{installed: true},
			Status{PlistPresent: true, Loaded: true}, "not enabled, so an update leaves it stopped"},
		{"enabled and stopped", "linux", fakeBackend{installed: true, enabled: true},
			Status{PlistPresent: true, Loaded: true, Enabled: true}, "systemctl --user start mcremote.service"},
		{"running", "linux", fakeBackend{installed: true, enabled: true, running: true},
			Status{PlistPresent: true, Loaded: true, Enabled: true, Active: true}, ""},
		{"probe error", "linux", fakeBackend{installed: true, err: probeErr},
			Status{PlistPresent: true, Loaded: true}, "service probe failed: systemctl show failed"},
		{"task stopped", "windows", fakeBackend{installed: true, enabled: true},
			Status{PlistPresent: true, Loaded: true, Enabled: true}, "schtasks /run /tn mcremote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer OverrideInstallOS(tc.os)()
			withProbeBackend(t, tc.backend, nil)
			st := ProbeStatus("mcremote")
			got := Status{PlistPresent: st.PlistPresent, Loaded: st.Loaded, Enabled: st.Enabled, Active: st.Active}
			if got != tc.want {
				t.Errorf("status = %+v, want %+v", got, tc.want)
			}
			if tc.wantHint == "" && st.Hint != "" {
				t.Errorf("hint = %q, want none", st.Hint)
			}
			if !strings.Contains(st.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", st.Hint, tc.wantHint)
			}
		})
	}
}

// statusBackend is the Task Scheduler backend's shape: one Status read
// answers every state. Its per-state methods fail the test, so a probe that
// asks them separately is caught.
type statusBackend struct {
	fakeBackend
	t      *testing.T
	status taskscheduler.Status
	err    error
	reads  int
}

func (b *statusBackend) Status(context.Context, string) (taskscheduler.Status, error) {
	b.reads++
	return b.status, b.err
}

func (b *statusBackend) Installed(context.Context, string) (bool, error) {
	b.t.Error("ProbeStatus asked Installed separately")
	return false, nil
}

func (b *statusBackend) Enabled(context.Context, string) (bool, error) {
	b.t.Error("ProbeStatus asked Enabled separately")
	return false, nil
}

func (b *statusBackend) Running(context.Context, string) (bool, error) {
	b.t.Error("ProbeStatus asked Running separately")
	return false, nil
}

// TestProbeStatusOnWindowsReadsOnce is MADR 0207 D17: on Windows, doctor's
// status comes from one Task Scheduler read, not three.
func TestProbeStatusOnWindowsReadsOnce(t *testing.T) {
	readErr := errors.New("task scheduler: access denied")
	for _, tc := range []struct {
		name     string
		status   taskscheduler.Status
		err      error
		want     Status
		wantHint string
	}{
		{"running", taskscheduler.Status{Installed: true, Enabled: true, Running: true},
			nil, Status{PlistPresent: true, Loaded: true, Enabled: true, Active: true}, ""},
		{"stopped", taskscheduler.Status{Installed: true, Enabled: true},
			nil, Status{PlistPresent: true, Loaded: true, Enabled: true}, "schtasks /run /tn mcremote"},
		{"disabled", taskscheduler.Status{Installed: true, Disabled: true},
			nil, Status{PlistPresent: true, Loaded: true}, "not enabled"},
		{"absent", taskscheduler.Status{}, nil, Status{}, "scheduled task missing"},
		{"read error", taskscheduler.Status{}, readErr, Status{}, "service probe failed: task scheduler: access denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer OverrideInstallOS("windows")()
			b := &statusBackend{t: t, status: tc.status, err: tc.err}
			withProbeBackend(t, b, nil)
			st := ProbeStatus("mcremote")
			if b.reads != 1 {
				t.Errorf("Status was read %d times, want once", b.reads)
			}
			got := Status{PlistPresent: st.PlistPresent, Loaded: st.Loaded, Enabled: st.Enabled, Active: st.Active}
			if got != tc.want {
				t.Errorf("status = %+v, want %+v", got, tc.want)
			}
			if tc.wantHint == "" && st.Hint != "" {
				t.Errorf("hint = %q, want none", st.Hint)
			}
			if !strings.Contains(st.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", st.Hint, tc.wantHint)
			}
		})
	}
}

// launchdBackend is the launchd backend's shape: one Status probe answers
// every state, "loaded" included. Its per-state methods fail the test.
type launchdBackend struct {
	fakeBackend
	t      *testing.T
	status launchd.Status
	err    error
	reads  int
}

func (b *launchdBackend) Status(context.Context, string) (launchd.Status, error) {
	b.reads++
	return b.status, b.err
}

func (b *launchdBackend) Installed(context.Context, string) (bool, error) {
	b.t.Error("ProbeStatus asked Installed separately")
	return false, nil
}

func (b *launchdBackend) Enabled(context.Context, string) (bool, error) {
	b.t.Error("ProbeStatus asked Enabled separately")
	return false, nil
}

func (b *launchdBackend) Running(context.Context, string) (bool, error) {
	b.t.Error("ProbeStatus asked Running separately")
	return false, nil
}

// TestProbeStatusOnMacOSReadsOnce is MADR 0207 D19: on macOS, doctor's status,
// "loaded" included, comes from one launchd probe, and a probe that fails is
// reported, not read as "bootout left down".
func TestProbeStatusOnMacOSReadsOnce(t *testing.T) {
	probeErr := errors.New("launchctl list: exit 5")
	for _, tc := range []struct {
		name     string
		status   launchd.Status
		err      error
		want     Status
		wantHint string
	}{
		{"running", launchd.Status{Installed: true, Loaded: true, Enabled: true, Running: true, PID: 42},
			nil, Status{PlistPresent: true, Loaded: true, Enabled: true, Active: true}, ""},
		{"loaded, not running", launchd.Status{Installed: true, Loaded: true, Enabled: true},
			nil, Status{PlistPresent: true, Loaded: true, Enabled: true}, "launchctl kickstart -k"},
		{"bootout left down", launchd.Status{Installed: true, Enabled: true},
			nil, Status{PlistPresent: true, Enabled: true}, "bootout left down"},
		{"not enabled", launchd.Status{Installed: true, Loaded: true},
			nil, Status{PlistPresent: true, Loaded: true}, "not enabled"},
		{"loaded without its plist", launchd.Status{Loaded: true, Running: true, PID: 42},
			nil, Status{Loaded: true}, "plist missing"},
		{"absent", launchd.Status{}, nil, Status{}, "plist missing"},
		{"probe error", launchd.Status{}, probeErr, Status{}, "service probe failed: launchctl list: exit 5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer OverrideInstallOS("darwin")()
			defer OverrideRunLaunchctlCapture(func(args ...string) (string, error) {
				t.Errorf("ProbeStatus ran launchctl %v itself", args)
				return "", errors.New("not in this test")
			})()
			defer OverrideLaunchdLoaded(func(context.Context, string) (bool, error) {
				t.Error("ProbeStatus asked launchdLoaded")
				return false, errors.New("not in this test")
			})()
			b := &launchdBackend{t: t, status: tc.status, err: tc.err}
			withProbeBackend(t, b, nil)
			st := ProbeStatus("mcremote")
			if b.reads != 1 {
				t.Errorf("Status was read %d times, want once", b.reads)
			}
			got := Status{PlistPresent: st.PlistPresent, Loaded: st.Loaded, Enabled: st.Enabled, Active: st.Active}
			if got != tc.want {
				t.Errorf("status = %+v, want %+v", got, tc.want)
			}
			if tc.wantHint == "" && st.Hint != "" {
				t.Errorf("hint = %q, want none", st.Hint)
			}
			if !strings.Contains(st.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", st.Hint, tc.wantHint)
			}
		})
	}
}

// TestProbeStatusWithoutABackend: a service manager the backend cannot reach
// is reported, not taken for a missing service.
func TestProbeStatusWithoutABackend(t *testing.T) {
	defer OverrideInstallOS("linux")()
	withProbeBackend(t, nil, errors.New("user scope needs XDG_RUNTIME_DIR"))
	st := ProbeStatus("mcremote")
	if st.PlistPresent || st.Active {
		t.Errorf("status = %+v, want nothing claimed", st)
	}
	if !strings.Contains(st.Hint, "cannot reach the service manager: user scope needs XDG_RUNTIME_DIR") {
		t.Errorf("hint = %q", st.Hint)
	}
}
