//go:build unix

package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// regexpCache drops the XDG_CACHE_HOME --env from an OpenRC script's
// supervise_daemon_args (MADR 0212 D10-A).
var regexpCache = regexp.MustCompile(` --env XDG_CACHE_HOME=[^ ']*`)

// scriptKinds are the supervisors S5b writes a script for.
var scriptKinds = []Kind{KindRunit, KindS6, KindOpenRCUser}

// pinnedEnv is a render environment with a runtime directory, so the systemd
// unit names a path rather than its %t specifier.
func pinnedEnv() renderEnv {
	return renderEnv{
		Home: "/home/u", User: "u", Path: "/home/u/.local/bin:/usr/bin:/bin",
		XDGConfigHome: "/home/u/.config", XDGDataHome: "/home/u/.local/share",
		XDGStateHome: "/home/u/.local/state", XDGCacheHome: "/home/u/.cache",
		XDGRuntimeDir: "/run/user/1000",
	}
}

func parityOpts() Options {
	return Options{
		Product: "mcremote", UnitName: "mcremote", Description: "mcremote multi-CLI remote control daemon",
		Binary: "/opt/my apps/it's/mcremote", ConfigPath: "/home/u/.config/mcremote/config.yaml",
		DataDir: "/home/u/data dir", ListenHost: "127.0.0.1", ListenPort: 7531,
		LogLevel: "debug", LogFormat: "json", WorkingDirectory: "/home/u/work",
		ExtraEnviron: []string{"TOKEN=a 'quoted' $value", "EMPTY="},
	}
}

// scriptEnvAndArgv reads a rendered script back the way a shell would: its
// exports, its working directory and the command it runs.
func scriptEnvAndArgv(t *testing.T, kind Kind, body string) (map[string]string, string, []string) {
	t.Helper()
	env := map[string]string{}
	var dir string
	var argv, bin, args []string
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimSpace(raw)
		words, err := shellWords(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		switch {
		case strings.HasPrefix(line, "export "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "export "), "=")
			w, err := shellWords(v)
			if err != nil || len(w) != 1 {
				t.Fatalf("export %s is not one word: %q", k, v)
			}
			env[k] = w[0]
		case strings.HasPrefix(line, "cd "):
			dir = words[1]
		case kind != KindOpenRCUser && strings.HasPrefix(line, "exec "):
			argv = words[1 : len(words)-1] // drop "exec" and "2>&1"
		case kind == KindOpenRCUser && strings.HasPrefix(line, "supervise_daemon_args="):
			// supervise-daemon.sh evals these values (MADR 0212 D10-A).
			inner, ok := evalWords(strings.TrimPrefix(line, "supervise_daemon_args="))
			if !ok {
				t.Fatalf("supervise_daemon_args do not parse: %s", line)
			}
			for i := 0; i+1 < len(inner); i += 2 {
				k, v, _ := strings.Cut(inner[i+1], "=")
				env[k] = v
			}
		case kind == KindOpenRCUser && strings.HasPrefix(line, "directory="):
			inner, _ := evalWords(strings.TrimPrefix(line, "directory="))
			dir = strings.Join(inner, " ")
		case kind == KindOpenRCUser && strings.HasPrefix(line, "command="):
			inner, _ := evalWords(strings.TrimPrefix(line, "command="))
			bin = inner
		case kind == KindOpenRCUser && strings.HasPrefix(line, "command_args="):
			args, _ = evalWords(strings.TrimPrefix(line, "command_args="))
		}
	}
	if kind == KindOpenRCUser {
		argv = append(bin, args...)
	}
	return env, dir, argv
}

// TestScriptMatchesTheSystemdUnit is S5-2's parity: for the same options and
// environment, each script exports what the unit's Environment= sets, changes
// to its WorkingDirectory=, and runs its ExecStart=.
func TestScriptMatchesTheSystemdUnit(t *testing.T) {
	opts, env := parityOpts(), pinnedEnv()
	unit, err := renderWith(opts, &env)
	if err != nil {
		t.Fatal(err)
	}
	wantEnv := map[string]string{}
	var wantDir string
	var wantArgv []string
	for raw := range strings.SplitSeq(unit, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok {
			continue
		}
		switch k {
		case "Environment":
			ek, ev, _ := strings.Cut(v, "=")
			wantEnv[ek] = unquoteSystemd(ev)
		case "WorkingDirectory":
			wantDir = unquoteSystemd(v)
		case "ExecStart":
			if wantArgv, err = splitExecStart(v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			body, err := renderScript(kind, opts, &env)
			if err != nil {
				t.Fatal(err)
			}
			if kind == KindOpenRCUser && (strings.Contains(body, "\nexport ") || strings.Contains(body, "\ncd ")) {
				t.Errorf("the OpenRC script exports or changes directory at the top level, which breaks openrc-run:\n%s", body)
			}
			gotEnv, gotDir, gotArgv := scriptEnvAndArgv(t, kind, body)
			if !reflect.DeepEqual(gotEnv, wantEnv) {
				t.Errorf("exports %v\nwant the unit's Environment= %v", gotEnv, wantEnv)
			}
			if gotDir != wantDir {
				t.Errorf("cd %q, want WorkingDirectory=%q", gotDir, wantDir)
			}
			if !slices.Equal(gotArgv, wantArgv) {
				t.Errorf("runs %q\nwant ExecStart=%q", gotArgv, wantArgv)
			}
		})
	}
}

// TestScriptRoundTrips: a refresh recovers every option and environment value
// from a rendered script, quotes and spaces included, and re-renders it byte
// for byte.
func TestScriptRoundTrips(t *testing.T) {
	opts, env := parityOpts(), pinnedEnv()
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			body, err := renderScript(kind, opts, &env)
			if err != nil {
				t.Fatal(err)
			}
			rec, why, ok := recoverScript(kind, "mcremote", "mcremote", body)
			if !ok {
				t.Fatalf("recoverScript refused its own output: %s\n%s", why, body)
			}
			if len(rec.Warnings) != 0 {
				t.Errorf("warnings %v on a complete script", rec.Warnings)
			}
			again, err := renderScript(kind, rec.Opts, &rec.Env)
			if err != nil {
				t.Fatal(err)
			}
			if again != body {
				t.Fatalf("re-render differs:\n%s", diffLines(body, again))
			}
		})
	}
}

// TestRecoverScriptRefusesWhatItDidNotWrite: a refresh leaves alone a script
// it cannot reproduce, so an operator's addition is never dropped.
func TestRecoverScriptRefusesWhatItDidNotWrite(t *testing.T) {
	opts, env := parityOpts(), pinnedEnv()
	for _, kind := range scriptKinds {
		body, err := renderScript(kind, opts, &env)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(body, "\n")
		cases := map[string]string{
			"an added line":     body + "ulimit -n 1024\n",
			"no managed marker": strings.Join(append(lines[:1:1], lines[2:]...), "\n"),
			"another shebang":   strings.Replace(body, scriptShebang(kind), "#!/bin/bash", 1),
		}
		for name, edited := range cases {
			t.Run(string(kind)+"/"+name, func(t *testing.T) {
				if _, why, ok := recoverScript(kind, "mcremote", "mcremote", edited); ok {
					t.Fatalf("recoverScript accepted %s", name)
				} else if why == "" {
					t.Fatal("refusal gave no reason")
				}
			})
		}
	}
}

// s6Logged makes an s6 service's daemon already write to its log service, so
// a setup over it has nothing to adopt (0212 PLAN P4 Deviations 3 and 4b).
func s6Logged(t *testing.T) {
	t.Helper()
	prev := s6LoggerConnected
	s6LoggerConnected = func(scriptDef) bool { return true }
	t.Cleanup(func() { s6LoggerConnected = prev })
}

// supervisorHost is a Linux host with kind detected, the fakes on PATH, no
// scanner running, and the records and definitions in scratch directories.
// It returns the scan root and the placeholder binary.
func supervisorHost(t *testing.T, kind Kind) (string, string) {
	t.Helper()
	t.Cleanup(OverrideInstallOS("linux"))
	t.Cleanup(OverrideDetectedKind(kind))
	withUnitRecord(t, "mcremote", "")
	fakeHost(t, fakeStub(t), map[string]map[string]int{
		"sv": {}, "s6-svc": {}, "s6-svscanctl": {}, "s6-svstat": {}, "rc-service": {}, "rc-update": {}, "openrc": {},
	})
	fakeProc(t)
	// Setup checks only the executable bit; the stub is never run here.
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	return t.TempDir(), bin
}

// fakeProc points procRoot at a scratch /proc holding one process per argv,
// owned by this user.
func fakeProc(t *testing.T, argvs ...[]string) {
	t.Helper()
	dir := t.TempDir()
	for i, argv := range argvs {
		p := filepath.Join(dir, strconv.Itoa(100+i))
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prev := procRoot
	procRoot = dir
	t.Cleanup(func() { procRoot = prev })
}

// recordSessionStarts replaces the detached start with a recorder.
func recordSessionStarts(t *testing.T) *[]string {
	t.Helper()
	var starts []string
	prev := startSessionSupervisor
	startSessionSupervisor = func(scanner, dir string) (int, error) {
		starts = append(starts, scanner+" "+dir)
		return 4242, nil
	}
	t.Cleanup(func() { startSessionSupervisor = prev })
	return &starts
}

func scriptSetup(t *testing.T, root, bin string, mut func(*Options)) (Result, error) {
	t.Helper()
	o := Options{Product: "mcremote", Binary: bin, UnitDir: root, ConfigPath: filepath.Join(t.TempDir(), "config.yaml")}
	if mut != nil {
		mut(&o)
	}
	return Setup(o)
}

func TestSetupRunitStartsASessionSupervisor(t *testing.T) {
	root, bin := supervisorHost(t, KindRunit)
	starts := recordSessionStarts(t)
	res, err := scriptSetup(t, root, bin, nil)
	if err != nil {
		t.Fatal(err)
	}
	def := scriptDefFor(KindRunit, "mcremote", root)
	if res.UnitPath != def.Path || res.Scope != string(KindRunit) {
		t.Fatalf("res = %+v, want the runit run script %s", res, def.Path)
	}
	fi, err := os.Stat(def.Path)
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("run script: %v, mode %v; want 0755", err, fi.Mode().Perm())
	}
	if !slices.Equal(*starts, []string{"runsvdir " + root}) || !res.SupervisorStarted || res.SupervisorPID != 4242 {
		t.Fatalf("starts %v, res %+v; want one session runsvdir on %s", *starts, res, root)
	}
	if !res.Enabled || !res.Started {
		t.Fatalf("res = %+v, want enabled and started", res)
	}
	if _, err := os.Stat(filepath.Join(def.Dir, "down")); !os.IsNotExist(err) {
		t.Fatalf("an enabled service has a down file: %v", err)
	}
	if got, err := ResolveBackend("mcremote"); err != nil || got != KindRunit {
		t.Fatalf("recorded backend %s, %v; want runit", got, err)
	}
	if calls := fakeCalls(t); len(calls) != 0 {
		t.Fatalf("calls %v, want none: the new runsvdir starts the service", calls)
	}
}

func TestSetupRunitRestartsThroughTheUsersSupervisor(t *testing.T) {
	root, bin := supervisorHost(t, KindRunit)
	starts := recordSessionStarts(t)
	fakeProc(t, []string{"/usr/bin/runsvdir", "-P", root})
	def := scriptDefFor(KindRunit, "mcremote", root)
	if err := os.MkdirAll(filepath.Join(def.Dir, "supervise"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(def.Dir, "supervise", "ok"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := scriptSetup(t, root, bin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*starts) != 0 || res.SupervisorStarted {
		t.Fatalf("started a session runsvdir %v although one watches %s", *starts, root)
	}
	// The status first notes the process the restart replaces (PLAN S5c). The
	// log service answers, so there is nothing to adopt (0212 PLAN P4
	// Deviation 3).
	want := []string{"sv status " + def.Dir, "sv status " + def.Dir + "/log", "sv restart " + def.Dir}
	if calls := fakeCalls(t); !slices.Equal(calls, want) || !res.Started {
		t.Fatalf("calls %v, started %v; want %v", calls, res.Started, want)
	}
}

func TestSetupRunitNoEnableAndNoStart(t *testing.T) {
	root, bin := supervisorHost(t, KindRunit)
	starts := recordSessionStarts(t)
	res, err := scriptSetup(t, root, bin, func(o *Options) { o.NoEnable, o.NoStart = true, true })
	if err != nil {
		t.Fatal(err)
	}
	def := scriptDefFor(KindRunit, "mcremote", root)
	if _, err := os.Stat(filepath.Join(def.Dir, "down")); err != nil {
		t.Fatalf("--no-enable wrote no down file: %v", err)
	}
	if res.Enabled || res.Started || len(*starts) != 0 || len(fakeCalls(t)) != 0 {
		t.Fatalf("res %+v, starts %v, calls %v; want nothing enabled or started", res, *starts, fakeCalls(t))
	}
}

func TestSetupS6RescansAndRestarts(t *testing.T) {
	root, bin := supervisorHost(t, KindS6)
	starts := recordSessionStarts(t)
	fakeProc(t, []string{"s6-svscan", root})
	def := scriptDefFor(KindS6, "mcremote", root)
	if err := os.MkdirAll(filepath.Join(def.Dir, "supervise"), 0o755); err != nil {
		t.Fatal(err)
	}
	s6Logged(t)
	if _, err := scriptSetup(t, root, bin, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"s6-svscanctl -a " + root, "s6-svstat -o " + s6Fields + " " + def.Dir, "s6-svc -r " + def.Dir, "s6-svc -u " + def.Dir}
	if calls := fakeCalls(t); !slices.Equal(calls, want) || len(*starts) != 0 {
		t.Fatalf("calls %v, starts %v; want %v", calls, *starts, want)
	}
}

func TestSetupS6StartsASessionSupervisor(t *testing.T) {
	root, bin := supervisorHost(t, KindS6)
	starts := recordSessionStarts(t)
	if _, err := scriptSetup(t, root, bin, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*starts, []string{"s6-svscan " + root}) {
		t.Fatalf("starts %v, want s6-svscan %s", *starts, root)
	}
}

// openRCRoot is an init.d directory under its own rc directory, as
// $XDG_CONFIG_HOME/rc/init.d is.
func openRCRoot(t *testing.T, root string) string {
	t.Helper()
	initd := filepath.Join(root, "rc", "init.d")
	if err := os.MkdirAll(initd, 0o755); err != nil {
		t.Fatal(err)
	}
	return initd
}

// openRCSession marks the user's OpenRC session running, as openrc --user
// does by writing its softlevel.
func openRCSession(t *testing.T) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "openrc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "softlevel"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSetupOpenRCStartsTheUserSession: with no user session running, setup
// creates the user's runlevels and starts the session as openrc-user.sh does
// at login; the session's default runlevel starts the service (PLAN S5b,
// Deviation 2).
func TestSetupOpenRCStartsTheUserSession(t *testing.T) {
	root, bin := supervisorHost(t, KindOpenRCUser)
	initd := openRCRoot(t, root)
	res, err := scriptSetup(t, initd, bin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.UnitPath != filepath.Join(initd, "mcremote") {
		t.Fatalf("init script %s, want %s", res.UnitPath, filepath.Join(initd, "mcremote"))
	}
	for _, lvl := range []string{"boot", "default", "shutdown"} {
		if fi, err := os.Stat(filepath.Join(root, "rc", "runlevels", lvl)); err != nil || !fi.IsDir() {
			t.Errorf("runlevel %s was not created: %v", lvl, err)
		}
	}
	want := []string{"rc-update --user add mcremote default", "openrc --user boot", "openrc --user default"}
	if calls := fakeCalls(t); !slices.Equal(calls, want) || !res.Enabled || !res.Started || !res.SupervisorStarted {
		t.Fatalf("calls %v, res %+v; want %v and the session started", calls, res, want)
	}
}

func TestSetupOpenRCRestartsInARunningSession(t *testing.T) {
	root, bin := supervisorHost(t, KindOpenRCUser)
	initd := openRCRoot(t, root)
	openRCSession(t)
	res, err := scriptSetup(t, initd, bin, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"rc-update --user add mcremote default", "rc-service --user -- mcremote restart"}
	if calls := fakeCalls(t); !slices.Equal(calls, want) || res.SupervisorStarted {
		t.Fatalf("calls %v, res %+v; want %v in the running session", calls, res, want)
	}
}

func TestSetupOpenRCNoEnable(t *testing.T) {
	root, bin := supervisorHost(t, KindOpenRCUser)
	initd := openRCRoot(t, root)
	openRCSession(t)
	if _, err := scriptSetup(t, initd, bin, func(o *Options) { o.NoEnable = true }); err != nil {
		t.Fatal(err)
	}
	if calls := fakeCalls(t); !slices.Equal(calls, []string{"rc-service --user -- mcremote restart"}) {
		t.Fatalf("calls %v, want only the start", calls)
	}
}

// TestSetupRecordsADefinitionItCouldNotStart: once the definition is written,
// a failure after it still records the backend and the name, so --remove finds
// what is on disk (PLAN S5b, Deviation 3; the live probe's --remove fell back
// to systemd without it).
func TestSetupRecordsADefinitionItCouldNotStart(t *testing.T) {
	root, bin := supervisorHost(t, KindOpenRCUser)
	initd := openRCRoot(t, root)
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_CFG"), "rc-update --user add mcremote default"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scriptSetup(t, initd, bin, nil); err == nil || !strings.Contains(err.Error(), "rc-update --user add failed") {
		t.Fatalf("setup with a failing rc-update: %v, want that failure", err)
	}
	t.Cleanup(OverrideDetectedKind(KindNone))
	if got, err := ResolveBackend("mcremote"); err != nil || got != KindOpenRCUser {
		t.Fatalf("after the failed setup the backend is %s, %v; want the recorded openrc-user", got, err)
	}
	if _, err := Remove(Options{Product: "mcremote", UnitDir: initd}); err != nil {
		t.Fatalf("Remove after the failed setup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(initd, "mcremote")); !os.IsNotExist(err) {
		t.Fatalf("the init script survived Remove: %v", err)
	}
}

// TestResolveBackendFallsBackToDetection: with no record, the backend is the
// detected one when setup installs under it, else the OS default.
func TestResolveBackendFallsBackToDetection(t *testing.T) {
	defer OverrideInstallOS("linux")()
	withUnitRecord(t, "mcremote", "")
	for detected, want := range map[Kind]Kind{
		KindRunit: KindRunit, KindS6: KindS6, KindOpenRCUser: KindOpenRCUser,
		KindSystemdBroken: KindSystemdUser, KindNone: KindSystemdUser,
	} {
		restore := OverrideDetectedKind(detected)
		got, err := ResolveBackend("mcremote")
		restore()
		if err != nil || got != want {
			t.Errorf("detected %s: ResolveBackend = %s, %v; want %s", detected, got, err, want)
		}
	}
}

// TestRemoveAllRetryWaitsForTheSupervisor: a removal that fails while the
// exiting supervisor still writes is retried until it succeeds.
func TestRemoveAllRetryWaitsForTheSupervisor(t *testing.T) {
	fails := 2
	prev, prevT := removeAll, removeRetryTimeout
	removeAll = func(string) error {
		if fails > 0 {
			fails--
			return os.ErrExist
		}
		return nil
	}
	removeRetryTimeout = 5 * time.Second
	t.Cleanup(func() { removeAll, removeRetryTimeout = prev, prevT })
	if err := removeAllRetry("/dir"); err != nil || fails != 0 {
		t.Fatalf("removeAllRetry: %v with %d failures left, want success after two", err, fails)
	}
	removeAll = func(string) error { return os.ErrExist }
	removeRetryTimeout = 300 * time.Millisecond
	if err := removeAllRetry("/dir"); err == nil {
		t.Fatal("removeAllRetry succeeded although every removal failed")
	}
}

// TestSetupScriptFollowsTheUnitsRules: identical content is a no-op, different
// content needs --force, and --env makes the script private.
func TestSetupScriptFollowsTheUnitsRules(t *testing.T) {
	root, bin := supervisorHost(t, KindRunit)
	recordSessionStarts(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	same := func(o *Options) { o.ConfigPath = cfg }
	if _, err := scriptSetup(t, root, bin, same); err != nil {
		t.Fatal(err)
	}
	again, err := scriptSetup(t, root, bin, same)
	if err != nil || !again.Unchanged {
		t.Fatalf("identical setup: %+v, %v; want Unchanged", again, err)
	}
	if _, err := scriptSetup(t, root, bin, func(o *Options) { o.ConfigPath, o.ListenPort = cfg, 9 }); err == nil ||
		!strings.Contains(err.Error(), "--force") {
		t.Fatalf("a different setup without --force: %v, want a refusal naming --force", err)
	}
	if _, err := scriptSetup(t, root, bin, func(o *Options) {
		o.ConfigPath, o.Force, o.ExtraEnviron = cfg, true, []string{"TOKEN=secret"}
	}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(scriptDefFor(KindRunit, "mcremote", root).Path)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("a script with --env: %v, mode %v; want 0700", err, fi.Mode().Perm())
	}
}

func TestRefreshScript(t *testing.T) {
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
			ro := RefreshOptions{}
			got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: root}, ro)
			if err != nil || got.Verdict != VerdictUnchanged {
				t.Fatalf("fresh refresh: %+v, %v; want unchanged", got, err)
			}

			// An older script with no XDG_CACHE_HOME is rewritten, and says so.
			body, _ := os.ReadFile(def.Path)
			var kept []string
			for l := range strings.SplitSeq(string(body), "\n") {
				if !strings.HasPrefix(l, "export XDG_CACHE_HOME=") {
					kept = append(kept, l)
				}
			}
			old := strings.Join(kept, "\n")
			if kind == KindOpenRCUser {
				old = regexpCache.ReplaceAllString(string(body), "")
			}
			if err := os.WriteFile(def.Path, []byte(old), 0o755); err != nil {
				t.Fatal(err)
			}
			got, err = RefreshUnit(Options{Product: "mcremote", UnitDir: root}, ro)
			if err != nil || got.Verdict != VerdictRefreshed || got.BackupPath != def.Backup {
				t.Fatalf("refresh of an older script: %+v, %v; want refreshed with backup %s", got, err, def.Backup)
			}
			if !strings.Contains(strings.Join(got.Warnings, "\n"), "XDG_CACHE_HOME") {
				t.Errorf("warnings %v do not name the missing XDG_CACHE_HOME", got.Warnings)
			}
			if b, _ := os.ReadFile(def.Backup); string(b) != old {
				t.Error("the backup is not the old script")
			}
			if kind == KindOpenRCUser && filepath.Dir(def.Backup) == def.Dir {
				t.Errorf("the OpenRC backup %s is in init.d, where OpenRC would run it", def.Backup)
			}

			// A hand edit is kept.
			if err := os.WriteFile(def.Path, []byte(string(body)+"ulimit -n 1024\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			got, err = RefreshUnit(Options{Product: "mcremote", UnitDir: root}, ro)
			if err != nil || got.Verdict != VerdictKept {
				t.Fatalf("refresh of a hand-edited script: %+v, %v; want kept", got, err)
			}
		})
	}
}

func TestRemoveScript(t *testing.T) {
	cases := map[Kind][]string{
		KindRunit:      {"sv down DIR", "sv exit DIR"},
		KindS6:         {"s6-svc -d DIR", "s6-svc -x DIR", "s6-svscanctl -an ROOT"},
		KindOpenRCUser: {"rc-service --user -- mcremote stop", "rc-update --user del mcremote default"},
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			root, bin := supervisorHost(t, kind)
			if kind == KindOpenRCUser {
				root = openRCRoot(t, root)
			}
			recordSessionStarts(t)
			if _, err := scriptSetup(t, root, bin, func(o *Options) { o.NoStart, o.NoEnable = true, true }); err != nil {
				t.Fatal(err)
			}
			def := scriptDefFor(kind, "mcremote", root)
			switch kind {
			case KindRunit:
				_ = os.MkdirAll(filepath.Join(def.Dir, "supervise"), 0o755)
				_ = os.WriteFile(filepath.Join(def.Dir, "supervise", "ok"), nil, 0o600)
			case KindS6:
				_ = os.MkdirAll(filepath.Join(def.Dir, "supervise"), 0o755)
				fakeProc(t, []string{"s6-svscan", root})
			}
			// The backup a refresh kept (MADR 0210 D9). runit's and s6's sits
			// in the service directory, which Remove deletes whole, as before;
			// OpenRC's sits beside init.d, and is named in the result.
			if err := os.WriteFile(def.Backup, []byte("the previous definition\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(filepath.Join(os.Getenv("FAKE_CFG"), "calls"))
			res, err := Remove(Options{Product: "mcremote", UnitDir: root})
			if err != nil || !res.Removed {
				t.Fatalf("Remove: %+v, %v", res, err)
			}
			if _, err := os.Stat(def.Backup); !os.IsNotExist(err) {
				t.Fatalf("%s survived Remove: %v", def.Backup, err)
			}
			wantBackup := ""
			if kind == KindOpenRCUser {
				wantBackup = def.Backup
			}
			if res.RemovedBackup != wantBackup {
				t.Errorf("RemovedBackup = %q, want %q", res.RemovedBackup, wantBackup)
			}
			for i := range want {
				want[i] = strings.NewReplacer("DIR", def.Dir, "ROOT", root).Replace(want[i])
			}
			if calls := fakeCalls(t); !slices.Equal(calls, want) {
				t.Fatalf("calls %v, want %v", calls, want)
			}
			if _, err := os.Stat(def.Path); !os.IsNotExist(err) {
				t.Fatalf("%s survived Remove: %v", def.Path, err)
			}
			rec, err := backendRecordPath("mcremote")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(rec); !os.IsNotExist(err) {
				t.Fatalf("the backend record %s survived Remove: %v", rec, err)
			}
		})
	}
}

func TestSupervisorWatching(t *testing.T) {
	fakeProc(t,
		[]string{"/usr/bin/runsvdir", "-P", "/home/u/.local/share/runit/service/"},
		[]string{"s6-svscan", "/elsewhere"},
	)
	cases := []struct {
		scanner, dir string
		want         bool
	}{
		{"runsvdir", "/home/u/.local/share/runit/service", true},
		{"runsvdir", "/home/u/.local/share/s6/service", false},
		{"s6-svscan", "/home/u/.local/share/runit/service", false},
		{"s6-svscan", "/elsewhere", true},
	}
	for _, tc := range cases {
		if got := supervisorWatching(tc.scanner, tc.dir); got != tc.want {
			t.Errorf("supervisorWatching(%s, %s) = %v, want %v", tc.scanner, tc.dir, got, tc.want)
		}
	}
}

// TestStartSessionSupervisorRunsTheScanner runs the real detached start
// against a fake scanner, which records its arguments.
func TestStartSessionSupervisorRunsTheScanner(t *testing.T) {
	fakeHost(t, fakeStub(t), map[string]map[string]int{"runsvdir": {}})
	pid, err := startSessionSupervisor("runsvdir", "/scan/dir")
	if err != nil || pid <= 0 {
		t.Fatalf("startSessionSupervisor: pid %d, %v", pid, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if calls := fakeCalls(t); slices.Equal(calls, []string{"runsvdir /scan/dir"}) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the scanner did not run with the scan directory: calls %v", fakeCalls(t))
}

// TestPrintScriptResult: the result says when the supervisor runs only for
// this session, as install.sh's summary did, and how to control the service.
func TestPrintScriptResult(t *testing.T) {
	root := t.TempDir()
	def := scriptDefFor(KindRunit, "mcremote", root)
	var out bytes.Buffer
	PrintSetupResult(&out, Result{Scope: string(KindRunit), UnitName: "mcremote", UnitPath: def.Path,
		Binary: "/b/mcremote", Enabled: true, Started: true, SupervisorStarted: true, SupervisorPID: 4242}, false, "mcremote")
	for _, want := range []string{
		"Definition:        " + def.Path,
		"started runsvdir for this login session (pid 4242)",
		"NOT configured",
		"Status:  sv status " + def.Dir,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("runit result lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "systemctl") {
		t.Errorf("runit result names systemctl:\n%s", out.String())
	}
	out.Reset()
	PrintSetupResult(&out, Result{Scope: string(KindOpenRCUser), UnitName: "mcremote",
		UnitPath: filepath.Join(root, "mcremote"), Enabled: true, Started: true}, false, "mcremote")
	if !strings.Contains(out.String(), "user default runlevel") || !strings.Contains(out.String(), "rc-service --user mcremote status") {
		t.Errorf("OpenRC result:\n%s", out.String())
	}
}

// TestSetupMcrelayOnRunit: mcrelay takes the same path, under its own name.
func TestSetupMcrelayOnRunit(t *testing.T) {
	root, _ := supervisorHost(t, KindRunit)
	recordSessionStarts(t)
	withUnitRecord(t, "mcrelay", "")
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcrelay"), "")
	res, err := Setup(Options{Product: "mcrelay", Binary: bin, UnitDir: root, ConfigPath: filepath.Join(t.TempDir(), "c.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "mcrelay", "run"); res.UnitPath != want {
		t.Fatalf("mcrelay run script %s, want %s", res.UnitPath, want)
	}
	body, _ := os.ReadFile(res.UnitPath)
	if !strings.Contains(string(body), "exec "+shq(bin)+" 'serve'") || !strings.Contains(string(body), managedMarker("mcrelay")) {
		t.Fatalf("mcrelay run script:\n%s", body)
	}
}

// TestRestoreScriptBackupRunsNoSystemctl: rolling a script back renames it, and
// asks no service manager to reload.
func TestRestoreScriptBackupRunsNoSystemctl(t *testing.T) {
	defer OverrideInstallOS("linux")()
	calls := 0
	defer OverrideRunSystemctl(func(...string) error { calls++; return nil })()
	dir := t.TempDir()
	run, prev := filepath.Join(dir, "run"), filepath.Join(dir, "run.prev")
	if err := os.WriteFile(run, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prev, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RestoreUnitBackup(run, prev); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(run); string(b) != "old" || calls != 0 {
		t.Fatalf("restored %q with %d systemctl calls; want old and none", b, calls)
	}
}

// TestSetupNotesTheProcessARestartReplaces: the pid the user's supervisor
// reports before the restart is kept, so the verified start cannot take the
// old daemon, still draining, for the restarted one (PLAN S5c; the live
// probe's s6 crash loop was confirmed running that way).
func TestSetupNotesTheProcessARestartReplaces(t *testing.T) {
	root, bin := supervisorHost(t, KindS6)
	recordSessionStarts(t)
	fakeProc(t, []string{"s6-svscan", root})
	def := scriptDefFor(KindS6, "mcremote", root)
	if err := os.MkdirAll(filepath.Join(def.Dir, "supervise"), 0o755); err != nil {
		t.Fatal(err)
	}
	// s6-svstat -o's form, which the state reads (MADR 0212 D7-A).
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_CFG"), "s6-svstat.out"),
		[]byte("true true true false 81 -1 40\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := startToken
	startToken = func(pid int) string { return fmt.Sprintf("token-of-%d", pid) }
	t.Cleanup(func() { startToken = prev })
	s6Logged(t)
	res, err := scriptSetup(t, root, bin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.replacedPid != 81 {
		t.Fatalf("replacedPid = %d, want the 81 s6-svstat reported before the restart", res.replacedPid)
	}
	// Its start token too, so a new process that reuses PID 81 still counts
	// (MADR 0212 D12-A, PLAN P3 Deviation 2).
	if res.replacedToken != "token-of-81" {
		t.Fatalf("replacedToken = %q, want process 81's", res.replacedToken)
	}
}

// TestOpenRCCrashedIsFailed: OpenRC restarts nothing, so "status: crashed" is
// the only sign of a daemon that died, and it is a failure.
func TestOpenRCCrashedIsFailed(t *testing.T) {
	fakeHost(t, fakeStub(t), map[string]map[string]int{"rc-service": {"--user -- mcremote status": 32}})
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_CFG"), "rc-service.out"),
		[]byte(" * status: crashed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := sampler(KindOpenRCUser, Result{UnitName: "mcremote"})()
	if s.up || !s.failed {
		t.Fatalf("sample %+v, want failed and not up", s)
	}
}
