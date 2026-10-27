//go:build unix

package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// The definitions of MADR 0212 D9-A and D10-A, and D15-A's reading of the
// forms before them. Goldens are in testdata/definitions; run with -update
// only to re-capture a deliberate change. *.pre-0212.golden are the forms
// setup wrote before P4, captured from its parent commit, and never
// re-captured.

// placeholderHome makes this process's home and XDG directories the pinned
// environment's. The runit and s6 scripts' comment names the service
// directory from this process's directories, not the pinned ones, so a
// golden would otherwise carry the home of whoever renders it
// (internal/homepaths_test.go).
func placeholderHome(t *testing.T) {
	t.Helper()
	env := pinnedEnv()
	t.Setenv("HOME", env.Home)
	t.Setenv("XDG_CONFIG_HOME", env.XDGConfigHome)
	t.Setenv("XDG_DATA_HOME", env.XDGDataHome)
	t.Setenv("XDG_STATE_HOME", env.XDGStateHome)
	t.Setenv("XDG_CACHE_HOME", env.XDGCacheHome)
}

// goldenDefinitions renders each form with the parity options and the pinned
// environment.
func goldenDefinitions(t *testing.T) map[string]string {
	t.Helper()
	placeholderHome(t)
	opts, env := parityOpts(), pinnedEnv()
	out := map[string]string{}
	for _, kind := range scriptKinds {
		body, err := renderScript(kind, opts, &env)
		if err != nil {
			t.Fatal(err)
		}
		out[string(kind)] = body
		for _, a := range scriptAuxFor(kind, scriptDefFor(kind, "mcremote", "/svc"), "mcremote", env) {
			out[string(kind)+"."+strings.ReplaceAll(strings.TrimPrefix(a.path, "/svc/mcremote/"), "/", "-")] = a.body
		}
	}
	return out
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "definitions", name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDefinitionsMatchTheirGoldens pins each form byte for byte: runit's and
// s6's run scripts, their log services' run scripts and s6's timeout-kill
// (D9-A), and the OpenRC script under supervise-daemon (D10-A).
func TestDefinitionsMatchTheirGoldens(t *testing.T) {
	got := goldenDefinitions(t)
	want := []string{"runit", "runit.log-run", "s6", "s6.log-run", "s6.timeout-kill", "openrc-user"}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("forms %v, want %v", names, want)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "definitions", name+".golden")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got[name]), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if w := readGolden(t, name); got[name] != w {
				t.Fatalf("%s differs from its golden:\n%s", name, diffLines(w, got[name]))
			}
		})
	}
}

// TestOpenRCDefinitionIsD10A names D10-A's lines, so a golden re-captured by
// mistake cannot drop one unseen. It has no notify: OpenRC's supervise-daemon
// never removes the service's notify socket, so only a first start works with
// it (MADR 0212 amendment C, C1).
func TestOpenRCDefinitionIsD10A(t *testing.T) {
	logDir := "'/home/u/.local/state/mcremote/log"
	opts, env := parityOpts(), pinnedEnv()
	body, err := renderScript(KindOpenRCUser, opts, &env)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"supervisor=supervise-daemon", "respawn_delay=5", "respawn_max=30", "respawn_period=300",
		`retry="TERM/45/KILL/5"`, `pidfile="${XDG_RUNTIME_DIR}"'/mcremote.pid'`,
		"output_log=" + logDir + "/mcremote.log'", "error_log=" + logDir + "/mcremote.log'",
		"start_pre() {", "\tcheckpath -d -m 0700 " + logDir + "'",
		// checkpath makes no parent: a fresh home has no ~/.local/state (P6
		// Deviation 2).
		"\tmkdir -p '/home/u/.local/state/mcremote'",
	} {
		if !strings.Contains(body, "\n"+want+"\n") {
			t.Errorf("no line %q in:\n%s", want, body)
		}
	}
	if strings.Index(body, "\tmkdir -p ") > strings.Index(body, "\tcheckpath ") {
		t.Errorf("start_pre runs checkpath before it makes the parent:\n%s", body)
	}
	for _, gone := range []string{"notify", "ready=", "start-stop-daemon", "start() {", "stop() {", ":-/tmp"} {
		if strings.Contains(body, gone) {
			t.Errorf("the supervise-daemon form has %q", gone)
		}
	}
}

// TestRecoverScriptReadsBothForms is D15-A: each form setup wrote before P4,
// and each form it writes now, recovers to the same options and environment,
// so a refresh moves an install onto the new form.
func TestRecoverScriptReadsBothForms(t *testing.T) {
	placeholderHome(t)
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			before := readGolden(t, string(kind)+".pre-0212")
			old, why, ok := recoverScript(kind, "mcremote", "mcremote", before)
			if !ok {
				t.Fatalf("the form from before P4 does not recover: %s\n%s", why, before)
			}
			now, err := renderScript(kind, old.Opts, &old.Env)
			if err != nil {
				t.Fatal(err)
			}
			opts, env := parityOpts(), pinnedEnv()
			want, err := renderScript(kind, opts, &env)
			if err != nil {
				t.Fatal(err)
			}
			if now != want {
				t.Fatalf("the form from before P4 re-renders to:\n%s", diffLines(want, now))
			}
			again, why, ok := recoverScript(kind, "mcremote", "mcremote", now)
			if !ok {
				t.Fatalf("the new form does not recover: %s", why)
			}
			if !slices.Equal(again.Opts.ExtraEnviron, old.Opts.ExtraEnviron) || again.Env != old.Env ||
				again.Opts.Binary != old.Opts.Binary || again.Opts.WorkingDirectory != old.Opts.WorkingDirectory {
				t.Errorf("the two forms recover differently:\nbefore %+v %+v\nnow    %+v %+v", old.Opts, old.Env, again.Opts, again.Env)
			}
		})
	}
}

// TestSetupWritesTheFilesBesideTheScript is D9-A at setup: runit and s6 get
// the log service and s6 its timeout-kill. A second setup changes nothing; a
// log/run setup did not write is refused without --force and replaced with.
func TestSetupWritesTheFilesBesideTheScript(t *testing.T) {
	for _, kind := range []Kind{KindRunit, KindS6} {
		t.Run(string(kind), func(t *testing.T) {
			root, bin := supervisorHost(t, kind)
			recordSessionStarts(t)
			cfg := filepath.Join(t.TempDir(), "config.yaml")
			same := func(o *Options) { o.ConfigPath = cfg }
			if _, err := scriptSetup(t, root, bin, same); err != nil {
				t.Fatal(err)
			}
			def := scriptDefFor(kind, "mcremote", root)
			for _, a := range scriptAuxFor(kind, def, "mcremote", currentRenderEnv("mcremote")) {
				got, err := os.ReadFile(a.path)
				if err != nil || string(got) != a.body {
					t.Fatalf("%s: %q, %v; want the rendered file", a.path, got, err)
				}
			}
			if again, err := scriptSetup(t, root, bin, same); err != nil || !again.Unchanged {
				t.Fatalf("a second setup: %+v, %v; want Unchanged", again, err)
			}
			logRun := filepath.Join(def.Dir, "log", "run")
			// An operator's logger, never run here (MADR 0193 D5: no shebang).
			if err := os.WriteFile(logRun, []byte("exec logger -t mine\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := scriptSetup(t, root, bin, same); err == nil || !strings.Contains(err.Error(), "--force") {
				t.Fatalf("setup over an operator's log/run: %v; want a refusal naming --force", err)
			}
			if _, err := scriptSetup(t, root, bin, func(o *Options) { o.ConfigPath, o.Force = cfg, true }); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(logRun); !strings.Contains(string(b), managedMarker("mcremote")) {
				t.Fatalf("--force left the operator's log/run: %s", b)
			}
		})
	}
}

// TestRefreshBringsAnInstallFromBefore0212On is D15-A through the refresh the
// update's reconcile runs: runit and s6 gain the files beside the run script,
// which itself is unchanged; OpenRC's start-stop-daemon script becomes the
// supervise-daemon form, with the old one kept as the backup. A refresh after
// that changes nothing.
func TestRefreshBringsAnInstallFromBefore0212On(t *testing.T) {
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			root, bin := supervisorHost(t, kind)
			if kind == KindOpenRCUser {
				root = openRCRoot(t, root)
				openRCSession(t)
			}
			recordSessionStarts(t)
			if _, err := scriptSetup(t, root, bin, nil); err != nil {
				t.Fatal(err)
			}
			def := scriptDefFor(kind, "mcremote", root)
			var before string
			if kind == KindOpenRCUser {
				before = readGolden(t, "openrc-user.pre-0212")
				if err := os.WriteFile(def.Path, []byte(before), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, p := range []string{filepath.Join(def.Dir, "log"), filepath.Join(def.Dir, "timeout-kill")} {
					if err := os.RemoveAll(p); err != nil {
						t.Fatal(err)
					}
				}
			}
			got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: root}, RefreshOptions{})
			if err != nil || got.Verdict != VerdictRefreshed {
				t.Fatalf("refresh of an install from before P4: %+v, %v; want refreshed", got, err)
			}
			if kind == KindOpenRCUser {
				if b, _ := os.ReadFile(def.Path); string(b) != readGolden(t, "openrc-user") {
					t.Fatalf("the refreshed script is not the supervise-daemon form:\n%s", b)
				}
				if b, _ := os.ReadFile(def.Backup); string(b) != before || got.BackupPath != def.Backup {
					t.Fatalf("backup %q at %s; want the start-stop-daemon script", got.BackupPath, def.Backup)
				}
			} else {
				if got.BackupPath != "" {
					t.Errorf("BackupPath = %q; the run script did not change", got.BackupPath)
				}
				for _, a := range scriptAuxFor(kind, def, "mcremote", currentRenderEnv("mcremote")) {
					if b, err := os.ReadFile(a.path); err != nil || string(b) != a.body {
						t.Fatalf("%s after the refresh: %q, %v", a.path, b, err)
					}
				}
			}
			if again, err := RefreshUnit(Options{Product: "mcremote", UnitDir: root}, RefreshOptions{}); err != nil ||
				again.Verdict != VerdictUnchanged {
				t.Fatalf("a second refresh: %+v, %v; want unchanged", again, err)
			}
		})
	}
}

// TestRemoveStopsS6sLogService: s6 supervises the log service on its own, so
// removal stops it and its supervisor too, and the service directory goes
// with the log service in it.
func TestRemoveStopsS6sLogService(t *testing.T) {
	root, bin := supervisorHost(t, KindS6)
	recordSessionStarts(t)
	if _, err := scriptSetup(t, root, bin, func(o *Options) { o.NoStart, o.NoEnable = true, true }); err != nil {
		t.Fatal(err)
	}
	def := scriptDefFor(KindS6, "mcremote", root)
	logDir := filepath.Join(def.Dir, "log")
	for _, d := range []string{filepath.Join(def.Dir, "supervise"), filepath.Join(logDir, "supervise")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Remove(filepath.Join(os.Getenv("FAKE_CFG"), "calls"))
	if _, err := Remove(Options{Product: "mcremote", UnitDir: root}); err != nil {
		t.Fatal(err)
	}
	want := []string{"s6-svc -d " + def.Dir, "s6-svc -x " + def.Dir, "s6-svc -d " + logDir, "s6-svc -x " + logDir}
	if calls := fakeCalls(t); !slices.Equal(calls, want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}
	if _, err := os.Stat(def.Dir); !os.IsNotExist(err) {
		t.Fatalf("%s survived Remove: %v", def.Dir, err)
	}
}

// writeLogRun puts a log service beside def's run script. It is never run
// here (MADR 0193 D5: no shebang).
func writeLogRun(t *testing.T, def scriptDef) {
	t.Helper()
	path := logServiceDef(def).Path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exec svlogd .\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// adoptionSeams stands in for the supervisor while the fakes run. The log
// service is supervised (runit), or has its s6-supervise (s6), once the call
// after is among the fakes' calls. s6's supervisor of the service is on the
// log service's pipe once the call renew is. An empty call never happens.
func adoptionSeams(t *testing.T, after, renew string) {
	t.Helper()
	called := func(c string) bool { return c != "" && slices.Contains(fakeCalls(t), c) }
	prevLog, prevPID, prevPipe := logServiceSupervised, s6SupervisorPID, s6LoggerConnected
	logServiceSupervised = func(context.Context, scriptDef) (bool, error) { return called(after), nil }
	s6SupervisorPID = func(def scriptDef) int {
		if filepath.Base(def.Dir) == "log" && called(after) {
			return 9
		}
		return 0
	}
	s6LoggerConnected = func(scriptDef) bool { return called(renew) }
	t.Cleanup(func() { logServiceSupervised, s6SupervisorPID, s6LoggerConnected = prevLog, prevPID, prevPipe })
}

// adoptTestTimeout bounds an adoption that is meant to time out. The bound
// covers the fakes' own runs too, so it must hold them: with 100 ms the fake
// s6-svstat itself ran out of time, and the test passed without the wait it
// pins (P4 Deviation 3's plants).
const adoptTestTimeout = 2 * time.Second

// adoptHost is a supervised kind service with a log service beside it.
func adoptHost(t *testing.T, kind Kind) scriptDef {
	t.Helper()
	root, _ := supervisorHost(t, kind)
	def := scriptDefFor(kind, "mcremote", root)
	if err := os.MkdirAll(def.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(def.Path, []byte("exec true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLogRun(t, def)
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_CFG"), "s6-svstat.out"), []byte("true true true false 18 -1 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return def
}

// TestAdoptLogService is P4 Deviation 3: a log service its supervisor does
// not run is taken up by restarting the service's supervision, runit's by
// `sv exit`, s6's by a rescan and then `s6-svc -dx`, in that order, and each
// waits for the supervisor to have done it.
func TestAdoptLogService(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []Kind{KindRunit, KindS6} {
		t.Run(string(kind), func(t *testing.T) {
			adopt := "sv exit "
			if kind == KindS6 {
				adopt = "s6-svscanctl -an "
			}
			t.Run("adopts", func(t *testing.T) {
				def := adoptHost(t, kind)
				want := []string{"sv exit " + def.Dir}
				adoptionSeams(t, "sv exit "+def.Dir, "")
				if kind == KindS6 {
					want = []string{"s6-svscanctl -an " + def.ScanDir, "s6-svc -dx " + def.Dir, "s6-svstat -o " + s6Fields + " " + def.Dir}
					adoptionSeams(t, "s6-svscanctl -an "+def.ScanDir, "s6-svc -dx "+def.Dir)
				}
				adopted, err := adoptLogService(ctx, def)
				if err != nil || !adopted {
					t.Fatalf("adoptLogService = %v, %v; want adopted", adopted, err)
				}
				if calls := fakeCalls(t); !slices.Equal(calls, want) {
					t.Fatalf("calls %v, want %v", calls, want)
				}
			})
			t.Run("no log service", func(t *testing.T) {
				def := adoptHost(t, kind)
				adoptionSeams(t, "", "")
				if err := os.Remove(logServiceDef(def).Path); err != nil {
					t.Fatal(err)
				}
				if adopted, err := adoptLogService(ctx, def); err != nil || adopted || len(fakeCalls(t)) != 0 {
					t.Fatalf("adoptLogService = %v, %v, calls %v; want nothing", adopted, err, fakeCalls(t))
				}
			})
			t.Run("already supervised", func(t *testing.T) {
				def := adoptHost(t, kind)
				adoptionSeams(t, "", "")
				logServiceSupervised = func(context.Context, scriptDef) (bool, error) { return kind == KindRunit, nil }
				s6LoggerConnected = func(scriptDef) bool { return kind == KindS6 }
				if adopted, err := adoptLogService(ctx, def); err != nil || adopted || len(fakeCalls(t)) != 0 {
					t.Fatalf("adoptLogService = %v, %v, calls %v; want nothing", adopted, err, fakeCalls(t))
				}
			})
			t.Run("the supervisor never takes it up", func(t *testing.T) {
				def := adoptHost(t, kind)
				prev := adoptTimeout
				adoptTimeout = adoptTestTimeout
				t.Cleanup(func() { adoptTimeout = prev })
				adoptionSeams(t, "", "")
				if _, err := adoptLogService(ctx, def); err == nil || !strings.Contains(err.Error(), "did not start its log service") {
					t.Fatalf("adoptLogService: %v; want a timeout naming the log service", err)
				}
				if calls := fakeCalls(t); len(calls) == 0 || !strings.HasPrefix(calls[0], adopt) {
					t.Fatalf("calls %v; want %s first", calls, adopt)
				}
			})
		})
	}
	t.Run("s6's log service runs, but not for the daemon", func(t *testing.T) {
		// Taken up by some other rescan (P4 Deviation 4b): adopt all the same.
		def := adoptHost(t, KindS6)
		adoptionSeams(t, "s6-svscanctl -an "+def.ScanDir, "s6-svc -dx "+def.Dir)
		logServiceSupervised = func(context.Context, scriptDef) (bool, error) { return true, nil }
		if adopted, err := adoptLogService(ctx, def); err != nil || !adopted {
			t.Fatalf("adoptLogService = %v, %v; want adopted", adopted, err)
		}
	})
	t.Run("s6's s6-supervise never comes back", func(t *testing.T) {
		def := adoptHost(t, KindS6)
		prev := adoptTimeout
		adoptTimeout = adoptTestTimeout
		t.Cleanup(func() { adoptTimeout = prev })
		adoptionSeams(t, "s6-svscanctl -an "+def.ScanDir, "")
		if _, err := adoptLogService(ctx, def); err == nil || !strings.Contains(err.Error(), "did not start its s6-supervise again onto the log service") {
			t.Fatalf("adoptLogService: %v; want a timeout naming s6-supervise", err)
		}
	})
}

// TestLogServiceSupervisedReadsTheSupervisor: the log service's status is
// read as the service's is, and "not supervised" is false, not an error.
func TestLogServiceSupervisedReadsTheSupervisor(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		kind Kind
		name string
		args string
		code int
		out  string
		want bool
		err  bool
	}{
		{KindRunit, "runs", "status", 0, "run: x/log: (pid 54) 3s", true, false},
		{KindRunit, "no runsv for it", "status", 1, "warning: x/log: unable to open supervise/ok: file does not exist", false, false},
		{KindRunit, "denied", "status", 1, "warning: x/log: access denied", false, true},
		{KindS6, "runs", "-o " + s6Fields, 0, "true true true false 38 -1 0", true, false},
		{KindS6, "no s6-supervise for it", "-o " + s6Fields, 1, "s6-svstat: fatal: unable to read status for x/log: s6-supervise not running", false, false},
		{KindS6, "denied", "-o " + s6Fields, 111, "s6-svstat: fatal: unable to read status for x/log: Permission denied", false, true},
	} {
		t.Run(string(tc.kind)+" "+tc.name, func(t *testing.T) {
			def := adoptHost(t, tc.kind)
			tool := "sv"
			if tc.kind == KindS6 {
				tool = "s6-svstat"
			}
			cfg := os.Getenv("FAKE_CFG")
			if err := os.WriteFile(filepath.Join(cfg, tool+".out"), []byte(tc.out+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := tool + " " + tc.args + " " + logServiceDef(def).Dir
			// The fake reads its exit status from a file named by its arguments,
			// which hold a path here.
			code := filepath.Join(cfg, args)
			if err := os.MkdirAll(filepath.Dir(code), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(code, []byte(strconv.Itoa(tc.code)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := logServiceSupervised(ctx, def)
			if got != tc.want || (err != nil) != tc.err {
				t.Fatalf("logServiceSupervised = %v, %v; want %v, error %v", got, err, tc.want, tc.err)
			}
			if calls := fakeCalls(t); !slices.Equal(calls, []string{args}) {
				t.Fatalf("calls %v, want %v", calls, []string{args})
			}
		})
	}
}

// TestS6SupervisorPIDIsTheServicesOwn: the s6-supervise working in the
// service directory, not the log service's, nor another process there.
func TestS6SupervisorPIDIsTheServicesOwn(t *testing.T) {
	def := scriptDefFor(KindS6, "mcremote", t.TempDir())
	if err := os.MkdirAll(logServiceDef(def).Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	proc := t.TempDir()
	prev := stateProcRoot
	stateProcRoot = proc
	t.Cleanup(func() { stateProcRoot = prev })
	add := func(pid int, comm, cwd string) {
		dir := filepath.Join(proc, strconv.Itoa(pid))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
	add(10, "s6-supervise", logServiceDef(def).Dir)
	add(11, "mcremote", def.Dir)
	if got := s6SupervisorPID(def); got != 0 {
		t.Fatalf("s6SupervisorPID = %d with only the log's and another process; want 0", got)
	}
	add(12, "s6-supervise", def.Dir)
	if got := s6SupervisorPID(def); got != 12 {
		t.Fatalf("s6SupervisorPID = %d, want 12", got)
	}
}

// TestS6LoggerConnectedComparesThePipes is P4 Deviation 4b: connected only
// when the service's s6-supervise writes into the very pipe the log
// service's s6-supervise reads, not merely when both run.
func TestS6LoggerConnectedComparesThePipes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		out, in    string
		withLogger bool
		want       bool
	}{
		{"the same pipe", "pipe:[4242]", "pipe:[4242]", true, true},
		{"another pipe", "pipe:[4242]", "pipe:[77]", true, false},
		{"no pipe at all", "/dev/null", "/dev/null", true, false},
		{"no log service", "pipe:[4242]", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := scriptDefFor(KindS6, "mcremote", t.TempDir())
			if err := os.MkdirAll(logServiceDef(def).Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			proc := t.TempDir()
			prev := stateProcRoot
			stateProcRoot = proc
			t.Cleanup(func() { stateProcRoot = prev })
			add := func(pid int, cwd, fd, target string) {
				dir := filepath.Join(proc, strconv.Itoa(pid))
				if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("s6-supervise\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				for link, to := range map[string]string{"cwd": cwd, filepath.Join("fd", fd): target} {
					if err := os.Symlink(to, filepath.Join(dir, link)); err != nil {
						t.Fatal(err)
					}
				}
			}
			add(20, def.Dir, "1", tc.out)
			if tc.withLogger {
				add(21, logServiceDef(def).Dir, "0", tc.in)
			}
			if got := s6LoggerConnected(def); got != tc.want {
				t.Fatalf("s6LoggerConnected = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSetupLeavesTheLogServiceToTheAdaptersUp is P4 Deviation 3 and P4b
// Deviation 1 on setup's path, as MADR 0207 D41 moved it: setup restarts a
// supervised service through the adapter, whose Up adopts a log service a
// refresh added, or restarts one it moved, before the up command
// (TestSupervisedUpAdoptsALogService, TestSupervisedUpRestartsAStaleLogService).
// Setup itself runs none of it.
func TestSetupLeavesTheLogServiceToTheAdaptersUp(t *testing.T) {
	for _, kind := range []Kind{KindRunit, KindS6} {
		t.Run(string(kind), func(t *testing.T) {
			root, bin := supervisorHost(t, kind)
			recordSessionStarts(t)
			def := scriptDefFor(kind, "mcremote", root)
			sup := filepath.Join(def.Dir, "supervise")
			if err := os.MkdirAll(sup, 0o755); err != nil {
				t.Fatal(err)
			}
			var want []string
			if kind == KindRunit {
				fakeProc(t, []string{"/usr/bin/runsvdir", "-P", root})
				if err := os.WriteFile(filepath.Join(sup, "ok"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				fakeProc(t, []string{"s6-svscan", root})
				want = []string{"s6-svscanctl -a " + root}
			}
			// Up for 100 s, so the log/run setup writes now is newer.
			fakeOutput(t, "sv", "run: x: (pid 20) 100s")
			f := &setupFake{running: true}
			withSetupBackend(t, f)
			res, err := scriptSetup(t, root, bin, nil)
			if err != nil || !res.Started {
				t.Fatalf("setup: %+v, %v", res, err)
			}
			if calls := fakeCalls(t); !slices.Equal(calls, want) {
				t.Fatalf("calls %v, want %v: the log service is the adapter's", calls, want)
			}
			if !slices.Equal(f.got(), []string{"stop", "start"}) {
				t.Fatalf("adapter calls %q, want a stop and a start", f.got())
			}
		})
	}
}

// TestEachUnitLogsIntoItsOwnDirectory is the PLAN's P4b: two runit or s6
// services of one product log into two directories below the product's, as
// svlogd and s6-log each lock theirs (step 1 reproduced the second logging
// nowhere). OpenRC's <unit>.log stays in the product's directory.
func TestEachUnitLogsIntoItsOwnDirectory(t *testing.T) {
	env := pinnedEnv()
	logs := filepath.Join(env.XDGStateHome, "mcremote", "log")
	for _, kind := range []Kind{KindRunit, KindS6} {
		var bodies []string
		for _, unit := range []string{"mcremote-a", "mcremote-b"} {
			body := scriptAuxFor(kind, scriptDefFor(kind, unit, "/svc"), "mcremote", env)[0].body
			dir := filepath.Join(logs, unit)
			if !strings.Contains(body, "\nmkdir -p -m 0700 "+shq(logs)+" "+shq(dir)+" || exit 1\n") ||
				!strings.HasSuffix(body, " "+shq(dir)+"\n") {
				t.Errorf("%s %s: log/run does not make and log into %s:\n%s", kind, unit, dir, body)
			}
			bodies = append(bodies, body)
		}
		if bodies[0] == bodies[1] {
			t.Errorf("%s: both units' log/run are the same", kind)
		}
	}
	opts := parityOpts()
	opts.UnitName = "mcremote-a"
	body, err := renderScript(KindOpenRCUser, opts, &env)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\noutput_log=" + shq(filepath.Join(logs, "mcremote-a.log")) + "\n"; !strings.Contains(body, want) {
		t.Errorf("OpenRC: no %q in:\n%s", want, body)
	}
}

// TestRefreshMovesLogRunToTheUnitsDirectory is P4b: a refresh rewrites the
// log/run P4 wrote, into the product's shared directory, to the unit's own,
// and a second refresh changes nothing. *.log-run.p4.golden are P4's, kept
// from before P4b.
func TestRefreshMovesLogRunToTheUnitsDirectory(t *testing.T) {
	for _, kind := range []Kind{KindRunit, KindS6} {
		t.Run(string(kind), func(t *testing.T) {
			root, bin := supervisorHost(t, kind)
			recordSessionStarts(t)
			if _, err := scriptSetup(t, root, bin, nil); err != nil {
				t.Fatal(err)
			}
			def := scriptDefFor(kind, "mcremote", root)
			logRun := logServiceDef(def).Path
			if err := os.WriteFile(logRun, []byte(readGolden(t, string(kind)+".log-run.p4")), 0o755); err != nil {
				t.Fatal(err)
			}
			got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: root}, RefreshOptions{})
			if err != nil || got.Verdict != VerdictRefreshed {
				t.Fatalf("refresh of P4's log/run: %+v, %v; want refreshed", got, err)
			}
			want := scriptAuxFor(kind, def, "mcremote", currentRenderEnv("mcremote"))[0].body
			if b, _ := os.ReadFile(logRun); string(b) != want {
				t.Fatalf("log/run after the refresh:\n%s", diffLines(want, string(b)))
			}
			if again, err := RefreshUnit(Options{Product: "mcremote", UnitDir: root}, RefreshOptions{}); err != nil ||
				again.Verdict != VerdictUnchanged {
				t.Fatalf("a second refresh: %+v, %v; want unchanged", again, err)
			}
		})
	}
}

// TestRestartStaleLogService is P4b Deviation 1: the log service alone is
// restarted when its run script is newer than its start, and not when it is
// older, when the logger is not up, or when there is no log service.
func TestRestartStaleLogService(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	prev := logClock
	logClock = func() time.Time { return now }
	t.Cleanup(func() { logClock = prev })
	for _, kind := range []Kind{KindRunit, KindS6} {
		up, down := "run: x/log: (pid 19) 100s", "down: x/log: 3s, normally up"
		tool, restart := "sv", "sv restart "
		if kind == KindS6 {
			up, down = "true true true false 19 -1 100", "false false true false -1 0 3"
			tool, restart = "s6-svstat", "s6-svc -r "
		}
		for _, tc := range []struct {
			name    string
			out     string
			age     time.Duration // of log/run; 0 means no log/run
			restart bool
		}{
			{"newer than the logger's start", up, 10 * time.Second, true},
			{"older than the logger's start", up, 200 * time.Second, false},
			// Newer than any start, so only the logger being down stops it.
			{"the logger not up", down, -10 * time.Second, false},
			{"no log service", up, 0, false},
		} {
			t.Run(string(kind)+" "+tc.name, func(t *testing.T) {
				def := supervisedHost(t, kind)
				fakeOutput(t, tool, tc.out)
				logRun := logServiceDef(def).Path
				if tc.age != 0 {
					writeLogRun(t, def)
					if err := os.Chtimes(logRun, now.Add(-tc.age), now.Add(-tc.age)); err != nil {
						t.Fatal(err)
					}
				}
				got, err := restartStaleLogService(ctx, def)
				if err != nil || got != tc.restart {
					t.Fatalf("restartStaleLogService = %v, %v; want %v", got, err, tc.restart)
				}
				restarted := slices.Contains(fakeCalls(t), restart+logServiceDef(def).Dir)
				if restarted != tc.restart {
					t.Fatalf("calls %v; want a restart of the log service: %v", fakeCalls(t), tc.restart)
				}
				if slices.ContainsFunc(fakeCalls(t), func(c string) bool { return c == restart+def.Dir }) {
					t.Fatalf("calls %v: the daemon's service was restarted", fakeCalls(t))
				}
			})
		}
	}
}

// TestRemoveNamesTheKeptLogs is the owner's decision recorded in the PLAN's
// P4b: --remove keeps the logs and says where they are; with none, it says
// nothing about them.
func TestRemoveNamesTheKeptLogs(t *testing.T) {
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			// A scratch home: the logs are found from the environment setup
			// pins in the definition, which is this process's.
			uninstallHost(t, "linux")
			fakeHost(t, fakeStub(t), map[string]map[string]int{
				"sv": {}, "s6-svc": {}, "s6-svscanctl": {}, "s6-svstat": {}, "rc-service": {}, "rc-update": {}, "openrc": {},
			})
			t.Cleanup(OverrideDetectedKind(kind))
			bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
			setup := func() {
				t.Helper()
				o := Options{Product: "mcremote", Binary: bin, NoStart: true, NoEnable: true,
					ConfigPath: filepath.Join(t.TempDir(), "config.yaml")}
				if _, err := Setup(o); err != nil {
					t.Fatal(err)
				}
			}
			setup()
			if res, err := Remove(Options{Product: "mcremote"}); err != nil || res.KeptLogs != "" {
				t.Fatalf("with no logs: %+v, %v; want no kept logs", res, err)
			}
			setup()
			renv := currentRenderEnv("mcremote")
			kept := unitLogDir("mcremote", "mcremote", renv)
			if kind == KindOpenRCUser {
				kept = filepath.Join(scriptLogDir("mcremote", renv), "mcremote.log")
				if err := os.MkdirAll(filepath.Dir(kept), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(kept, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(kept, 0o700); err != nil {
				t.Fatal(err)
			}
			res, err := Remove(Options{Product: "mcremote"})
			if err != nil || res.KeptLogs != kept {
				t.Fatalf("Remove: KeptLogs %q, %v; want %s", res.KeptLogs, err, kept)
			}
			if _, err := os.Stat(kept); err != nil {
				t.Fatalf("the logs at %s went: %v", kept, err)
			}
			var out strings.Builder
			PrintRemoveResult(&out, res, "daemon")
			if !strings.Contains(out.String(), "Logs kept:         "+kept) {
				t.Fatalf("output %q does not name %s", out.String(), kept)
			}
		})
	}
}

// TestRefreshMovesAScriptOffNotify is P4 Deviation 2: a script P4 first wrote,
// with notify="socket:ready", recovers, and the refresh rewrites it without
// the line, keeping the old one as the backup.
func TestRefreshMovesAScriptOffNotify(t *testing.T) {
	root, bin := supervisorHost(t, KindOpenRCUser)
	root = openRCRoot(t, root)
	openRCSession(t)
	recordSessionStarts(t)
	if _, err := scriptSetup(t, root, bin, nil); err != nil {
		t.Fatal(err)
	}
	def := scriptDefFor(KindOpenRCUser, "mcremote", root)
	current, err := os.ReadFile(def.Path)
	if err != nil {
		t.Fatal(err)
	}
	withNotify := strings.Replace(string(current), "retry=\"TERM/45/KILL/5\"\n", "retry=\"TERM/45/KILL/5\"\nnotify=\"socket:ready\"\n", 1)
	if withNotify == string(current) {
		t.Fatalf("no retry line to put notify after:\n%s", current)
	}
	if err := os.WriteFile(def.Path, []byte(withNotify), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: root}, RefreshOptions{})
	if err != nil || got.Verdict != VerdictRefreshed {
		t.Fatalf("refresh of a script with notify: %+v, %v; want refreshed", got, err)
	}
	if b, _ := os.ReadFile(def.Path); string(b) != string(current) {
		t.Fatalf("the refreshed script:\n%s", diffLines(string(current), string(b)))
	}
	if b, _ := os.ReadFile(def.Backup); string(b) != withNotify {
		t.Fatalf("the backup is not the script with notify:\n%s", b)
	}
}
