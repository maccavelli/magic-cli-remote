//go:build unix

package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The update's runit, s6 and OpenRC backend: the library's adapter over this
// package's state and commands (MADR 0212 P5). These tests run the functions
// Magic hands the adapter; supervisedbackend_linux_test.go builds the
// adapter itself, which the library refuses off Linux.

// supervisedHost is a scratch Linux host with kind's definition of mcremote
// at its default path, the fakes on PATH, and a fast settle.
func supervisedHost(t *testing.T, kind Kind) scriptDef {
	t.Helper()
	uninstallHost(t, "linux")
	fakeHost(t, fakeStub(t), map[string]map[string]int{
		"sv": {}, "s6-svc": {}, "s6-svscanctl": {}, "s6-svstat": {}, "rc-service": {}, "openrc": {},
	})
	fastSettle(t)
	writeDefinition(t, kind, "mcremote", "mcremote", "/opt/mc/mcremote")
	return scriptDefFor(kind, "mcremote", "")
}

// fakeOutput makes the named fake print out.
func fakeOutput(t *testing.T, name, out string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_CFG"), name+".out"), []byte(out+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSupervisedPollIsD17 is D17: a 75 s bound and the library's 10 s
// settle window. MCREMOTE_SVC_WAIT_SECS, when set, is the bound, refused when
// it is not above the settle window, or on runit not above KillAfter's 45 s.
func TestSupervisedPollIsD17(t *testing.T) {
	t.Setenv(waitEnv, "")
	if err := os.Unsetenv(waitEnv); err != nil {
		t.Fatal(err)
	}
	for _, kind := range scriptKinds {
		p, err := supervisedPoll(kind)
		if err != nil || p.Timeout != 75*time.Second || p.Settle != 10*time.Second {
			t.Fatalf("%s, unset: %+v, %v; want 75 s and 10 s", kind, p, err)
		}
	}
	for _, tc := range []struct {
		kind  Kind
		value string
		want  time.Duration // 0 means refused
	}{
		{KindRunit, "90", 90 * time.Second},
		{KindRunit, "46", 46 * time.Second},
		{KindRunit, "45", 0},
		{KindRunit, "30", 0},
		{KindS6, "30", 30 * time.Second},
		{KindS6, "11", 11 * time.Second},
		{KindS6, "10", 0},
		{KindOpenRCUser, "10", 0},
		{KindOpenRCUser, "3", 0},
		{KindS6, "soon", 0},
	} {
		t.Setenv(waitEnv, tc.value)
		p, err := supervisedPoll(tc.kind)
		switch {
		case tc.want == 0 && (err == nil || !strings.Contains(err.Error(), waitEnv)):
			t.Errorf("%s, %s=%s: %+v, %v; want a refusal naming %s", tc.kind, waitEnv, tc.value, p, err, waitEnv)
		case tc.want != 0 && (err != nil || p.Timeout != tc.want):
			t.Errorf("%s, %s=%s: %+v, %v; want %s", tc.kind, waitEnv, tc.value, p, err, tc.want)
		}
	}
}

// TestSupervisedOptionsPerKind is D17 and D3-B: runit alone gets a Kill, `sv
// kill`, at 45 s; s6 and OpenRC kill the daemon themselves. OpenRC alone gets
// the DetachCheck. The application check is the product's, as on every
// backend.
func TestSupervisedOptionsPerKind(t *testing.T) {
	ctx := context.Background()
	probe := HealthOptions{Probe: func(context.Context, ServeArgs) (libservice.Health, error) {
		return libservice.Health{Ready: true}, nil
	}}
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			def := supervisedHost(t, kind)
			o, err := supervisedOptions(kind, "mcremote", probe, "mcremote", "")
			if err != nil {
				t.Fatal(err)
			}
			if o.Name != "mcremote" || o.State == nil || o.Up == nil || o.Down == nil || o.Probe == nil {
				t.Fatalf("options %+v: want the name, State, Up, Down and the probe", o)
			}
			if got := o.Kill != nil; got != (kind == KindRunit) {
				t.Errorf("Kill set = %v; want it on runit only", got)
			}
			if kind == KindRunit && o.Kill != nil {
				if o.KillAfter != 45*time.Second {
					t.Errorf("KillAfter = %s, want 45 s", o.KillAfter)
				}
				if err := o.Kill(ctx); err != nil {
					t.Fatal(err)
				}
				if calls := fakeCalls(t); !slices.Equal(calls, []string{"sv kill " + def.Dir}) {
					t.Errorf("Kill ran %v, want sv kill %s", calls, def.Dir)
				}
			} else if o.KillAfter != 0 {
				t.Errorf("KillAfter = %s, want none", o.KillAfter)
			}
			if got := o.DetachCheck != nil; got != (kind == KindOpenRCUser) {
				t.Errorf("DetachCheck set = %v; want it on OpenRC only", got)
			}
			if bare, err := supervisedOptions(kind, "mcremote", HealthOptions{}, "mcremote", ""); err != nil || bare.Probe != nil {
				t.Errorf("with no product check: Probe set %v, %v; want none", bare.Probe != nil, err)
			}
		})
	}
}

// TestSupervisedDownStopsAndDoesNotWait: Down is the stop command alone; the
// adapter does the waiting.
func TestSupervisedDownStopsAndDoesNotWait(t *testing.T) {
	ctx := context.Background()
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			def := supervisedHost(t, kind)
			if err := supervisedDown(ctx, def, "mcremote"); err != nil {
				t.Fatal(err)
			}
			want := map[Kind]string{
				KindRunit: "sv down " + def.Dir, KindS6: "s6-svc -d " + def.Dir,
				KindOpenRCUser: "rc-service --user -- mcremote stop",
			}[kind]
			if calls := fakeCalls(t); !slices.Equal(calls, []string{want}) {
				t.Fatalf("Down ran %v, want %q", calls, want)
			}
		})
	}
}

// TestSupervisedUp: through the user's own supervisor when one watches,
// otherwise by starting one for the session; OpenRC starts the user session
// when none runs.
func TestSupervisedUp(t *testing.T) {
	ctx := context.Background()
	t.Run("runit, watched", func(t *testing.T) {
		def := supervisedHost(t, KindRunit)
		t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return true }))
		fakeOutput(t, "sv", "down: "+def.Dir+": 1s, normally up")
		if err := supervisedUp(ctx, def, "mcremote"); err != nil {
			t.Fatal(err)
		}
		if calls := fakeCalls(t); !slices.Equal(calls, []string{"sv status " + def.Dir, "sv up " + def.Dir}) {
			t.Fatalf("calls %v, want the status, then sv up", calls)
		}
	})
	t.Run("s6, not watched", func(t *testing.T) {
		def := supervisedHost(t, KindS6)
		t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return false }))
		starts := recordSessionStarts(t)
		if err := supervisedUp(ctx, def, "mcremote"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(*starts, []string{"s6-svscan " + def.ScanDir}) || len(fakeCalls(t)) != 0 {
			t.Fatalf("starts %v, calls %v; want a session s6-svscan and no s6-svc", *starts, fakeCalls(t))
		}
	})
	t.Run("OpenRC, no session", func(t *testing.T) {
		def := supervisedHost(t, KindOpenRCUser)
		if err := supervisedUp(ctx, def, "mcremote"); err != nil {
			t.Fatal(err)
		}
		want := []string{"openrc --user boot", "openrc --user default", "rc-service --user -- mcremote start"}
		if calls := fakeCalls(t); !slices.Equal(calls, want) {
			t.Fatalf("calls %v, want %v", calls, want)
		}
	})
}

// TestSupervisedUpAdoptsALogService is MADR 0212 D21: Up adopts a log
// service a refresh added, on a service that is down, then brings it up. On a
// service that is up, as in the adapter's restore after a failed stop, it
// adopts nothing. A second Up adopts nothing again.
func TestSupervisedUpAdoptsALogService(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []Kind{KindRunit, KindS6} {
		t.Run(string(kind), func(t *testing.T) {
			d := func(def scriptDef) (adopt []string, up, status string) {
				if kind == KindRunit {
					return []string{"sv exit " + def.Dir}, "sv up " + def.Dir, "sv status " + def.Dir
				}
				return []string{"s6-svscanctl -an " + def.ScanDir, "s6-svc -dx " + def.Dir},
					"s6-svc -u " + def.Dir, "s6-svstat -o " + s6Fields + " " + def.Dir
			}
			down := func(t *testing.T, def scriptDef) {
				if kind == KindRunit {
					fakeOutput(t, "sv", "down: "+def.Dir+": 1s, normally up")
				} else {
					fakeOutput(t, "s6-svstat", "false false true false -1 0 1")
				}
			}
			host := func(t *testing.T) scriptDef {
				def := supervisedHost(t, kind)
				t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return true }))
				writeLogRun(t, def)
				return def
			}
			seams := func(def scriptDef) {
				adopt, _, _ := d(def)
				renew := ""
				if kind == KindS6 {
					renew = adopt[1]
				}
				adoptionSeams(t, adopt[0], renew)
			}

			t.Run("down: adopted, then up", func(t *testing.T) {
				def := host(t)
				down(t, def)
				seams(def)
				if err := supervisedUp(ctx, def, "mcremote"); err != nil {
					t.Fatal(err)
				}
				adopt, up, status := d(def)
				want := append([]string{status}, adopt...)
				if kind == KindS6 {
					want = append(want, status) // the new s6-supervise answers
				}
				want = append(want, up)
				if calls := fakeCalls(t); !slices.Equal(calls, want) {
					t.Fatalf("calls %v, want %v", calls, want)
				}
			})
			t.Run("a second Up adopts nothing", func(t *testing.T) {
				def := host(t)
				down(t, def)
				seams(def)
				for range 2 {
					if err := supervisedUp(ctx, def, "mcremote"); err != nil {
						t.Fatal(err)
					}
				}
				adopt, _, _ := d(def)
				n := 0
				for _, c := range fakeCalls(t) {
					if c == adopt[0] {
						n++
					}
				}
				if n != 1 {
					t.Fatalf("calls %v: %q %d times, want once", fakeCalls(t), adopt[0], n)
				}
			})
			t.Run("up: the restore adopts nothing", func(t *testing.T) {
				def := host(t)
				if kind == KindRunit {
					fakeOutput(t, "sv", "run: "+def.Dir+": (pid 4242) 40s")
				} else {
					fakeOutput(t, "s6-svstat", "true true true false 4242 -1 40")
				}
				adoptionSeams(t, "", "")
				if err := supervisedUp(ctx, def, "mcremote"); err != nil {
					t.Fatal(err)
				}
				_, up, status := d(def)
				if calls := fakeCalls(t); !slices.Equal(calls, []string{status, up}) {
					t.Fatalf("calls %v, want the status, then the up command alone", calls)
				}
			})
		})
	}
}

// TestSupervisedUpRestartsAStaleLogService is the PLAN's P4b Deviation 1 on
// the update's path: Up, on a service that is down and whose log service
// needs no adoption, restarts a log service a refresh moved, then brings the
// service up. On a service that is up, the restore, it does not.
func TestSupervisedUpRestartsAStaleLogService(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []Kind{KindRunit, KindS6} {
		for _, up := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{false: " down", true: " up"}[up], func(t *testing.T) {
				def := supervisedHost(t, kind)
				t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return true }))
				writeLogRun(t, def)
				switch {
				case kind == KindRunit && up:
					fakeOutput(t, "sv", "run: "+def.Dir+": (pid 4242) 40s")
				case kind == KindRunit:
					fakeOutput(t, "sv", "down: "+def.Dir+": 1s, normally up")
				case up:
					fakeOutput(t, "s6-svstat", "true true true false 4242 -1 40")
				default:
					fakeOutput(t, "s6-svstat", "false false true false -1 0 1")
				}
				// The logger runs and the daemon writes to it: nothing to adopt.
				adoptionSeams(t, "", "")
				logServiceSupervised = func(context.Context, scriptDef) (bool, error) { return true, nil }
				s6LoggerConnected = func(scriptDef) bool { return true }
				var restarts []string
				prev := restartStaleLogService
				restartStaleLogService = func(_ context.Context, d scriptDef) (bool, error) {
					restarts = append(restarts, d.Dir)
					return true, nil
				}
				t.Cleanup(func() { restartStaleLogService = prev })
				if err := supervisedUp(ctx, def, "mcremote"); err != nil {
					t.Fatal(err)
				}
				want := []string{def.Dir}
				if up {
					want = nil
				}
				if !slices.Equal(restarts, want) {
					t.Fatalf("log service restarts %v, want %v", restarts, want)
				}
			})
		}
	}
}

// TestSupervisedCommandsPrintNothing: the update's stdout carries only
// --json's output, so a supervisor tool's chatter never reaches it.
func TestSupervisedCommandsPrintNothing(t *testing.T) {
	ctx := context.Background()
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			def := supervisedHost(t, kind)
			t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return true }))
			for _, tool := range []string{"sv", "s6-svc", "s6-svstat", "rc-service", "openrc"} {
				fakeOutput(t, tool, "chatter from "+tool)
			}
			o, err := supervisedOptions(kind, "mcremote", HealthOptions{}, "mcremote", "")
			if err != nil {
				t.Fatal(err)
			}
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout := os.Stdout
			os.Stdout = w
			_ = supervisedUp(ctx, def, "mcremote")
			_ = supervisedDown(ctx, def, "mcremote")
			_, _ = o.State(ctx)
			if o.Kill != nil {
				_ = o.Kill(ctx)
			}
			os.Stdout = stdout
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(r)
			if len(got) != 0 {
				t.Fatalf("stdout got %q", got)
			}
		})
	}
}

// fakeCgroups lays out a scratch /proc: this process's cgroup file, and the
// OpenRC daemon's as pid 4242, with its command name.
func fakeCgroups(t *testing.T, mine, theirs string) {
	t.Helper()
	root := t.TempDir()
	for dir, files := range map[string]map[string]string{
		"self": {"cgroup": mine},
		"4242": {"cgroup": theirs, "comm": "mcremote\n"},
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(root, dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	prev := stateProcRoot
	stateProcRoot = root
	t.Cleanup(func() { stateProcRoot = prev })
}

// TestOpenRCDetachCheckIsD3B is MADR 0212 D3-B: the handoff is refused when
// this process shares the daemon's cgroup and that cgroup is OpenRC's,
// whatever rc.conf says, and allowed otherwise. P1 measured a user service in
// `0::/`, which allows.
func TestOpenRCDetachCheckIsD3B(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name          string
		mine, theirs  string
		running, deny bool
	}{
		{"a shared OpenRC cgroup", "0::/openrc.mcremote\n", "0::/openrc.mcremote\n", true, true},
		{"a shared OpenRC cgroup, v1 hybrid", "1:name=openrc:/openrc.mcremote\n0::/user.slice\n", "1:name=openrc:/openrc.mcremote\n0::/system.slice\n", true, true},
		{"the root cgroup, as P1 measured", "0::/\n", "0::/\n", true, false},
		{"another OpenRC service's cgroup", "0::/openrc.sshd\n", "0::/openrc.mcremote\n", true, false},
		{"a shared cgroup that is not OpenRC's", "0::/user.slice/session-3.scope\n", "0::/user.slice/session-3.scope\n", true, false},
		{"the daemon not running", "0::/openrc.mcremote\n", "0::/openrc.mcremote\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := supervisedHost(t, KindOpenRCUser)
			fakeCgroups(t, tc.mine, tc.theirs)
			if tc.running {
				if err := os.WriteFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "mcremote.pid"), []byte("4242\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				fakeHost(t, fakeStub(t), map[string]map[string]int{"rc-service": {"--user -- mcremote status": openRCStopped}})
			}
			err := openRCDetachCheck(ctx, def, "mcremote")
			if got := err != nil; got != tc.deny {
				t.Fatalf("openRCDetachCheck = %v; want refused %v", err, tc.deny)
			}
			if tc.deny && !strings.Contains(err.Error(), "run the update from a shell") {
				t.Fatalf("the refusal %q does not say what to do", err)
			}
		})
	}
}

// TestIsInstalledReadsTheScriptDefinition: IsInstalled, which an update asks
// when no backend can be built, finds the script definition at its default
// path, and reports none once it is gone.
func TestIsInstalledReadsTheScriptDefinition(t *testing.T) {
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			def := supervisedHost(t, kind)
			if err := recordBackend("mcremote", kind); err != nil {
				t.Fatal(err)
			}
			if ok, err := IsInstalled("mcremote"); err != nil || !ok {
				t.Fatalf("IsInstalled = %v, %v; want the %s definition found", ok, err, kind)
			}
			if err := os.Remove(def.Path); err != nil {
				t.Fatal(err)
			}
			if ok, err := IsInstalled("mcremote"); err != nil || ok {
				t.Fatalf("IsInstalled after removal = %v, %v; want false", ok, err)
			}
		})
	}
}
