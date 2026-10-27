package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// setupFake is a backend for setup's tests. It records each call, and models
// the two things setup must not get wrong (MADR 0207 D4):
//
//   - like the library's systemd, launchd and adapter backends, its Start
//     leaves a running service alone, and its WaitHealthy then never sees a
//     new instance: it times out;
//   - like the library's Task, its Start refuses a task another process
//     disabled until Enable is called.
type setupFake struct {
	mu    sync.Mutex
	calls []string
	// running is whether the service runs; Stop clears it, Start sets it.
	running bool
	// disabled is a task another process disabled.
	disabled bool
	// stale is set when Start found the service running: nothing restarted.
	stale bool
	// stopErr, startErr and waitErr are what those calls return.
	stopErr, startErr, waitErr error
	// onStop, when set, runs as Stop is called, so a test can see what the
	// stop came before.
	onStop func()
}

func (f *setupFake) note(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

// got is the calls made, in order.
func (f *setupFake) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *setupFake) Installed(context.Context, string) (bool, error) { return true, nil }
func (f *setupFake) Enabled(context.Context, string) (bool, error)   { return true, nil }

func (f *setupFake) Running(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running, nil
}

func (f *setupFake) Stop(context.Context, string) error {
	f.note("stop")
	if f.onStop != nil {
		f.onStop()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	f.running = false
	return nil
}

func (f *setupFake) Enable(context.Context, string) error {
	f.note("enable")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled = false
	return nil
}

func (f *setupFake) Start(context.Context, string) error {
	f.note("start")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.startErr != nil:
		return f.startErr
	case f.disabled:
		return errors.New("selfupdate: taskscheduler: the task is disabled")
	case f.running:
		f.stale = true
	}
	f.running = true
	return nil
}

func (f *setupFake) WaitHealthy(context.Context, string) error {
	f.note("wait")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stale {
		return libservice.ErrTimeout
	}
	return f.waitErr
}

// setupBuild is one backend setup asked for.
type setupBuild struct {
	Kind    Kind
	Product string
	Unit    string
	Health  HealthOptions
}

// withSetupBackend makes every backend setup builds f, and returns what each
// build asked for.
func withSetupBackend(t *testing.T, f *setupFake) *[]setupBuild {
	t.Helper()
	var builds []setupBuild
	prev := setupBackend
	setupBackend = func(kind Kind, product, unit string, h HealthOptions) (Backend, error) {
		builds = append(builds, setupBuild{kind, product, unit, h})
		return f, nil
	}
	t.Cleanup(func() { setupBackend = prev })
	return &builds
}

// TestWaitSeconds is answer A3: a set value is honoured, and anything but a
// positive whole number is refused, naming the variable and the value.
func TestWaitSeconds(t *testing.T) {
	t.Setenv(waitEnv, "7")
	if n, err := waitSeconds(); err != nil || n != 7 {
		t.Fatalf("7: %d, %v; want 7", n, err)
	}
	for _, bad := range []string{"0", "-1", "abc", "", "1.5"} {
		t.Setenv(waitEnv, bad)
		_, err := waitSeconds()
		if err == nil || !strings.Contains(err.Error(), waitEnv) || !strings.Contains(err.Error(), `"`+bad+`"`) {
			t.Errorf("%q: %v, want a refusal naming %s and the value", bad, err, waitEnv)
		}
	}
}

// TestSetupPollIsD42: unset leaves the backends' own bounds; a set value is
// the timeout, and must be above the 10 s settle window; the script kinds take
// the adapter's bounds, 75 s, with runit's 45 s kill on top (MADR 0212 D17).
func TestSetupPollIsD42(t *testing.T) {
	t.Setenv(waitEnv, "")
	if err := os.Unsetenv(waitEnv); err != nil {
		t.Fatal(err)
	}
	if p, err := setupPoll(KindSystemdUser); err != nil || p != (libservice.PollOptions{}) {
		t.Fatalf("unset, systemd: %+v, %v; want the backend's defaults", p, err)
	}
	if p, err := setupPoll(KindS6); err != nil || p.Timeout != supervisedTimeout || p.Settle != supervisedSettle {
		t.Fatalf("unset, s6: %+v, %v; want the adapter's bounds", p, err)
	}
	t.Setenv(waitEnv, "61")
	for _, k := range []Kind{KindSystemdUser, KindLaunchdAgent, KindWindowsTask, KindRunit} {
		if p, err := setupPoll(k); err != nil || p.Timeout != 61*time.Second {
			t.Errorf("61, %s: %+v, %v; want a 61 s timeout", k, p, err)
		}
	}
	t.Setenv(waitEnv, "10")
	for _, k := range []Kind{KindSystemdUser, KindLaunchdAgent, KindWindowsTask, KindS6, KindOpenRCUser} {
		if _, err := setupPoll(k); err == nil || !strings.Contains(err.Error(), "settle window") {
			t.Errorf("10, %s: %v; want a refusal naming the settle window", k, err)
		}
	}
	t.Setenv(waitEnv, "45")
	if _, err := setupPoll(KindRunit); err == nil {
		t.Error("45 on runit: accepted; want a refusal, since runit kills at 45 s")
	}
}

// TestVerifyStart: the start check waits on the backend that started the
// service. Healthy is running; a wait that ran out is a warning; anything else
// is an error that says how to see why (MADR 0207 D4).
func TestVerifyStart(t *testing.T) {
	cases := []struct {
		name     string
		waitErr  error
		want     string
		wantErr  bool
		contains string
	}{
		{"healthy", nil, HealthRunning, false, ""},
		{"timed out", libservice.ErrTimeout, HealthStarting, false, "has not reported running yet"},
		{"unhealthy", libservice.ErrUnhealthy, HealthFailed, true, "journalctl --user -u mcremote"},
		{"another failure", errors.New("permission denied"), HealthFailed, true, "permission denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &setupFake{waitErr: tc.waitErr}
			res := Result{UnitName: "mcremote", Label: "mcremote", started: &startedBy{b: f, product: "mcremote"}}
			got, err := realVerifyStart(KindSystemdUser, res)
			if got.Health != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("health %q, err %v; want %q, error %v", got.Health, err, tc.want, tc.wantErr)
			}
			text := strings.Join(got.Warnings, "\n")
			if err != nil {
				text = err.Error()
			}
			if !strings.Contains(text, tc.contains) {
				t.Errorf("%q does not say %q", text, tc.contains)
			}
			if !slices.Equal(f.got(), []string{"wait"}) {
				t.Errorf("calls %q, want one wait", f.got())
			}
		})
	}
	t.Run("errors.Is reaches the cause", func(t *testing.T) {
		f := &setupFake{waitErr: libservice.ErrUnhealthy}
		_, err := realVerifyStart(KindSystemdUser, Result{Label: "mcremote", started: &startedBy{b: f, product: "mcremote"}})
		if !errors.Is(err, libservice.ErrUnhealthy) {
			t.Fatalf("%v does not wrap ErrUnhealthy", err)
		}
	})
	t.Run("nothing started it", func(t *testing.T) {
		if _, err := realVerifyStart(KindSystemdUser, Result{Label: "mcremote"}); err == nil {
			t.Fatal("a start with no backend was confirmed")
		}
	})
}

// TestPrintSetupResultReportsTheHealth: the result says whether the start was
// confirmed, for the unit and for a script kind, and prints the warnings.
func TestPrintSetupResultReportsTheHealth(t *testing.T) {
	for _, scope := range []string{string(KindSystemdUser), string(KindRunit)} {
		var out bytes.Buffer
		PrintSetupResult(&out, Result{Scope: scope, UnitName: "mcremote", Label: "mcremote", UnitPath: "/x/mcremote/run",
			Started: true, Health: HealthRunning}, false, "mcremote")
		if !strings.Contains(out.String(), "Running:           yes, confirmed after the start") {
			t.Errorf("%s running result:\n%s", scope, out.String())
		}
		out.Reset()
		PrintSetupResult(&out, Result{Scope: scope, UnitName: "mcremote", Label: "mcremote", UnitPath: "/x/mcremote/run",
			Started: true, Health: HealthStarting, Warnings: []string{"mcremote has not reported running yet: timed out"}}, false, "mcremote")
		if !strings.Contains(out.String(), "not yet confirmed") || !strings.Contains(out.String(), "warning: mcremote has not reported running yet: timed out") {
			t.Errorf("%s starting result:\n%s", scope, out.String())
		}
	}
}
