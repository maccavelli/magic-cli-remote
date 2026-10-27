//go:build unix

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fixture is one of P1's captures (0212 PLAN P1): a supervisor tool's stdout
// as it printed it, and its exit code and stderr.
func fixture(t *testing.T, dir, state string) libservice.Output {
	t.Helper()
	base := filepath.Join("testdata", "supervisor", dir, state)
	stdout, err := os.ReadFile(base + ".stdout")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(base + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Exit   int    `json:"exit"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	return libservice.Output{Stdout: stdout, Stderr: []byte(meta.Stderr), ExitCode: meta.Exit}
}

// stateHost lays out kind's definition in scratch directories: present
// unless absent, with a down file or the default-runlevel link as asked.
func stateHost(t *testing.T, kind Kind, absent, down, runlevel bool) scriptDef {
	t.Helper()
	root := t.TempDir()
	var def scriptDef
	if kind == KindOpenRCUser {
		def = scriptDefFor(kind, "mcremote", filepath.Join(root, "rc", "init.d"))
	} else {
		def = scriptDefFor(kind, "mcremote", root)
	}
	if err := os.MkdirAll(def.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !absent {
		if err := os.WriteFile(def.Path, []byte("definition\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if down {
		if err := os.WriteFile(filepath.Join(def.Dir, "down"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if runlevel {
		lvl := filepath.Join(filepath.Dir(def.ScanDir), "runlevels", "default")
		if err := os.MkdirAll(lvl, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(def.Path, filepath.Join(lvl, "mcremote")); err != nil {
			t.Fatal(err)
		}
	}
	return def
}

// answer makes kind's status tool resolve, and every run of it print out.
func answer(t *testing.T, kind Kind, out libservice.Output) {
	t.Helper()
	dirs := toolDirs(t, 1)
	tool := map[Kind]string{KindRunit: "sv", KindS6: "s6-svstat", KindOpenRCUser: "rc-service"}[kind]
	testexec.WriteShellStub(t, filepath.Join(dirs[0], tool), "exit 0")
	recordRunner(t, out)
}

// fakeProcesses points stateProcRoot at a scratch /proc: each pid with its
// command name and parent.
func fakeProcesses(t *testing.T, procs map[int]struct {
	comm   string
	parent int
}) {
	t.Helper()
	root := t.TempDir()
	for pid, p := range procs {
		dir := filepath.Join(root, fmt.Sprint(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1", pid, p.comm, p.parent)
		for name, body := range map[string]string{"comm": p.comm + "\n", "stat": stat} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	prev := stateProcRoot
	stateProcRoot = root
	t.Cleanup(func() { stateProcRoot = prev })
}

// TestStateReplaysTheProbes is MADR 0212 D16, D7-A and D18: each supervisor's
// own report, as P1 captured it, read into the library's SupervisedState.
// Installed is the definition's existence and NormallyUp its down file or
// runlevel link. No supervisor is ErrNoSupervisor with those still set, and a
// missing definition is Installed false, not an error.
func TestStateReplaysTheProbes(t *testing.T) {
	type want struct {
		st     libservice.SupervisedState
		uptime time.Duration
		class  error
	}
	up := func(pid int, wantUp bool) libservice.SupervisedState {
		return libservice.SupervisedState{Installed: true, NormallyUp: true, Supervised: true, Up: true, PID: pid, WantUp: wantUp}
	}
	down := libservice.SupervisedState{Installed: true, NormallyUp: true, Supervised: true}
	for _, tc := range []struct {
		kind                   Kind
		dir, state             string
		absent, down, runlevel bool
		want                   want
	}{
		{KindRunit, "runit-2.3.1", "up", false, false, false, want{st: up(20, true), uptime: 2 * time.Second}},
		{KindRunit, "runit-2.3.1", "down", false, false, false, want{st: down}},
		{KindRunit, "runit-2.3.1", "down-wantup", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true, Supervised: true, WantUp: true, Restarting: true}}},
		{KindRunit, "runit-2.3.1", "up-normallydown", false, true, false,
			want{st: libservice.SupervisedState{Installed: true, Supervised: true, Up: true, PID: 42, WantUp: true}, uptime: time.Second}},
		{KindRunit, "runit-2.3.1", "down-downfile", false, true, false,
			want{st: libservice.SupervisedState{Installed: true, Supervised: true}}},
		{KindRunit, "runit-2.3.1", "run-wantdown", false, false, false, want{st: up(49, false), uptime: 4 * time.Second}},
		{KindRunit, "runit-2.3.1", "nosupervisor", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true}, class: libservice.ErrNoSupervisor}},
		{KindRunit, "runit-2.3.1", "absent", true, false, false, want{st: libservice.SupervisedState{NormallyUp: true}}},
		{KindRunit, "runit-2.3.1", "denied", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true}, class: libservice.ErrPermission}},

		{KindS6, "s6-2.13.2.0", "o-up", false, false, false, want{st: up(18, true), uptime: 2 * time.Second}},
		{KindS6, "s6-2.13.2.0", "o-down", false, false, false, want{st: down}},
		{KindS6, "s6-2.13.2.0", "o-down-wantup", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true, Supervised: true, WantUp: true, Restarting: true}}},
		{KindS6, "s6-2.13.2.0", "o-up-normallydown", false, true, false,
			want{st: libservice.SupervisedState{Installed: true, Supervised: true, Up: true, PID: 39, WantUp: true}, uptime: time.Second}},
		{KindS6, "s6-2.13.2.0", "o-down-downfile", false, true, false,
			want{st: libservice.SupervisedState{Installed: true, Supervised: true}}},
		{KindS6, "s6-2.13.2.0", "o-up-wantdown", false, false, false, want{st: up(46, false), uptime: 4 * time.Second}},
		{KindS6, "s6-2.13.2.0", "o-nosupervisor", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true}, class: libservice.ErrNoSupervisor}},
		{KindS6, "s6-2.13.2.0", "absent", true, false, false,
			want{st: libservice.SupervisedState{NormallyUp: true}, class: libservice.ErrNoSupervisor}},
		{KindS6, "s6-2.13.2.0", "denied", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true}, class: libservice.ErrPermission}},

		{KindOpenRCUser, "openrc-0.62.6", "stopped", false, false, true, want{st: down}},
		{KindOpenRCUser, "openrc-0.62.6", "sd-stopped", false, false, false,
			want{st: libservice.SupervisedState{Installed: true, Supervised: true}}},
		{KindOpenRCUser, "openrc-0.62.6", "crashed", false, false, true,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true, Supervised: true, Failed: true}}},
		{KindOpenRCUser, "openrc-0.62.6", "nosession", false, false, true,
			want{st: libservice.SupervisedState{Installed: true, NormallyUp: true}, class: libservice.ErrNoSupervisor}},
		{KindOpenRCUser, "openrc-0.62.6", "noruntime", false, false, false,
			want{st: libservice.SupervisedState{Installed: true}, class: libservice.ErrNoSupervisor}},
		{KindOpenRCUser, "openrc-0.62.6", "absent", true, false, false, want{}},
	} {
		t.Run(tc.dir+"/"+tc.state, func(t *testing.T) {
			def := stateHost(t, tc.kind, tc.absent, tc.down, tc.runlevel)
			answer(t, tc.kind, fixture(t, tc.dir, tc.state))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // no pidfile: the started cases are below
			obs, err := observe(context.Background(), tc.kind, def, "mcremote")
			if tc.want.class == nil && err != nil {
				t.Fatalf("observe: %v", err)
			}
			if tc.want.class != nil && !errors.Is(err, tc.want.class) {
				t.Fatalf("observe: err %v, want %v", err, tc.want.class)
			}
			got := obs.state
			got.Detail = ""
			if got != tc.want.st || obs.uptime != tc.want.uptime {
				t.Errorf("state %+v, up %s\nwant  %+v, up %s", got, obs.uptime, tc.want.st, tc.want.uptime)
			}
			if obs.state.Ready != nil {
				t.Errorf("Ready = %v; D18 leaves it nil", *obs.state.Ready)
			}
		})
	}
}

// TestOpenRCStartedReadsThePIDFile: a started OpenRC service's PID is the
// pidfile's in the runtime directory. Under supervise-daemon (D10-A) the
// file names the supervisor, and the daemon is its child (P1).
func TestOpenRCStartedReadsThePIDFile(t *testing.T) {
	for _, tc := range []struct {
		state   string
		pidfile int
		want    int
	}{
		{"started", 180, 180}, // start-stop-daemon: the daemon's own PID
		{"sd-started", 306, 307},
		{"sd-restarted", 306, 368},
	} {
		t.Run(tc.state, func(t *testing.T) {
			def := stateHost(t, KindOpenRCUser, false, false, true)
			answer(t, KindOpenRCUser, fixture(t, "openrc-0.62.6", tc.state))
			rd := t.TempDir()
			t.Setenv("XDG_RUNTIME_DIR", rd)
			if err := os.WriteFile(filepath.Join(rd, "mcremote.pid"), []byte(fmt.Sprintf("%d\n", tc.pidfile)), 0o644); err != nil {
				t.Fatal(err)
			}
			fakeProcesses(t, map[int]struct {
				comm   string
				parent int
			}{
				180:     {"python3", 1},
				306:     {"supervise-daemon", 1},
				tc.want: {"mcremote", tc.pidfile},
			})
			obs, err := observe(context.Background(), KindOpenRCUser, def, "mcremote")
			if err != nil {
				t.Fatal(err)
			}
			if !obs.state.Up || !obs.state.WantUp || obs.state.PID != tc.want {
				t.Fatalf("state %+v; want up, pid %d", obs.state, tc.want)
			}
		})
	}
}

// TestOpenRCPIDFileHasNoTmpFallback is MADR 0212 D12-A: the pidfile is read
// in the runtime directory only. A file of that name in /tmp is someone
// else's, not this service's.
func TestOpenRCPIDFileHasNoTmpFallback(t *testing.T) {
	name := fmt.Sprintf("p3probe%d", os.Getpid())
	def := stateHost(t, KindOpenRCUser, false, false, true)
	def = scriptDefFor(KindOpenRCUser, name, def.ScanDir)
	if err := os.WriteFile(def.Path, []byte("definition\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	answer(t, KindOpenRCUser, fixture(t, "openrc-0.62.6", "started"))
	tmp := filepath.Join("/tmp", name+".pid")
	if err := os.WriteFile(tmp, []byte("4242\n"), 0o644); err != nil {
		t.Skipf("cannot write %s: %v", tmp, err)
	}
	t.Cleanup(func() { _ = os.Remove(tmp) })
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	prev := userRuntimeRoot
	userRuntimeRoot = t.TempDir()
	t.Cleanup(func() { userRuntimeRoot = prev })
	obs, err := observe(context.Background(), KindOpenRCUser, def, name)
	if err != nil {
		t.Fatal(err)
	}
	if obs.state.PID != 0 {
		t.Fatalf("PID = %d, read from %s; want 0: there is no /tmp fallback", obs.state.PID, tmp)
	}
}

// TestSettleTellsAReusedPIDByItsStartToken is MADR 0212 D12-A: a process with
// the PID of the one before it, but another start token, is a new process.
// While watched, that is a restart; and the process a restart replaces is
// skipped only when PID and token both match (P3 Deviation 2).
func TestSettleTellsAReusedPIDByItsStartToken(t *testing.T) {
	fastSettle(t)
	samples := func(ss ...sample) {
		i := 0
		prev := newSampler
		newSampler = func(Kind, Result) sampleFunc {
			return func() (sample, error) {
				s := ss[min(i, len(ss)-1)]
				i++
				return s, nil
			}
		}
		t.Cleanup(func() { newSampler = prev })
	}
	t.Run("a restart while watched", func(t *testing.T) {
		samples(sample{up: true, pid: 81, token: "old"}, sample{up: true, pid: 81, token: "new"})
		health, why := settle(KindRunit, Result{UnitName: "mcremote"}, 5)
		if health != HealthFailed || !strings.Contains(why, "restarted while watched") {
			t.Fatalf("settle = %s, %q; want a restart while watched", health, why)
		}
	})
	t.Run("a new process with the replaced one's PID", func(t *testing.T) {
		samples(sample{up: true, pid: 81, token: "new", uptime: 3 * time.Second})
		res := Result{UnitName: "mcremote", replacedPid: 81, replacedToken: "old"}
		if health, why := settle(KindRunit, res, 5); health != HealthRunning {
			t.Fatalf("settle = %s, %q; want running: PID 81 is a new process", health, why)
		}
	})
	t.Run("the replaced process itself", func(t *testing.T) {
		samples(sample{up: true, pid: 81, token: "old", uptime: 3 * time.Second})
		res := Result{UnitName: "mcremote", replacedPid: 81, replacedToken: "old"}
		if health, _ := settle(KindRunit, res, 3); health == HealthRunning {
			t.Fatal("settle took the process the restart replaces for the new one")
		}
	})
}

// TestSamplerCarriesTheStartToken: the sample settle reads is the state's,
// with the up process's start token, so settle's comparisons see it.
func TestSamplerCarriesTheStartToken(t *testing.T) {
	def := stateHost(t, KindRunit, false, false, false)
	answer(t, KindRunit, fixture(t, "runit-2.3.1", "up"))
	prev := startToken
	startToken = func(pid int) string { return fmt.Sprintf("token-of-%d", pid) }
	t.Cleanup(func() { startToken = prev })
	s, err := sampler(KindRunit, Result{UnitName: "mcremote", UnitPath: def.Path})()
	if err != nil {
		t.Fatal(err)
	}
	if want := (sample{up: true, pid: 20, uptime: 2 * time.Second, token: "token-of-20"}); s != want {
		t.Fatalf("sample %+v, want %+v", s, want)
	}
}

// TestReadServeArgsReadsScriptDefinitions is MADR 0212 D4-A: the probe reads
// what setup baked into a runit, s6 or OpenRC definition, with the refresh's
// parser, as it does for a systemd unit.
func TestReadServeArgsReadsScriptDefinitions(t *testing.T) {
	opts := bakedOptions("mcrelay")
	for _, kind := range []Kind{KindRunit, KindS6, KindOpenRCUser} {
		t.Run(string(kind), func(t *testing.T) {
			t.Cleanup(OverrideInstallOS("linux"))
			t.Cleanup(OverrideDetectedKind(kind))
			withUnitRecord(t, "mcrelay", "")
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			def := scriptDefFor(kind, "mcrelay", "")
			body, err := renderScript(kind, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(def.Path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(def.Path, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			got, why, err := readServeArgs("mcrelay", "")
			if err != nil || why != "" || got != wantBaked {
				t.Fatalf("readServeArgs = %+v, %q, %v; want %+v", got, why, err, wantBaked)
			}
		})
	}
}
