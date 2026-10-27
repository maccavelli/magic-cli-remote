//go:build unix

package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

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
		for _, notify := range []bool{true, false} {
			if kind != KindOpenRCUser && !notify {
				continue
			}
			withOpenRCNotify(t, notify)
			body, err := renderScript(kind, opts, &env)
			if err != nil {
				t.Fatal(err)
			}
			name := string(kind)
			if kind == KindOpenRCUser && !notify {
				name += ".no-notify"
			}
			out[name] = body
		}
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
// (D9-A), and the OpenRC script under supervise-daemon, with and without
// notify (D10-A).
func TestDefinitionsMatchTheirGoldens(t *testing.T) {
	got := goldenDefinitions(t)
	want := []string{"runit", "runit.log-run", "s6", "s6.log-run", "s6.timeout-kill", "openrc-user", "openrc-user.no-notify"}
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
// mistake cannot drop one unseen.
func TestOpenRCDefinitionIsD10A(t *testing.T) {
	logDir := "'/home/u/.local/state/mcremote/log"
	for _, notify := range []bool{true, false} {
		withOpenRCNotify(t, notify)
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
		} {
			if !strings.Contains(body, "\n"+want+"\n") {
				t.Errorf("notify %v: no line %q in:\n%s", notify, want, body)
			}
		}
		if got := strings.Contains(body, "\nnotify=\"socket:ready\"\n"); got != notify {
			t.Errorf("notify line present = %v, want %v", got, notify)
		}
		for _, gone := range []string{"start-stop-daemon", "start() {", "stop() {", ":-/tmp"} {
			if strings.Contains(body, gone) {
				t.Errorf("notify %v: the supervise-daemon form still has %q", notify, gone)
			}
		}
	}
}

// TestRecoverScriptReadsBothForms is D15-A: each form setup wrote before P4,
// and each form it writes now, recovers to the same options and environment,
// so a refresh moves an install onto the new form.
func TestRecoverScriptReadsBothForms(t *testing.T) {
	withOpenRCNotify(t, false)
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
				if b, _ := os.ReadFile(def.Path); string(b) != readGolden(t, "openrc-user.no-notify") {
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

// TestOpenRCNotifyFollowsTheVersion is P4 Deviation 1: notify only from
// OpenRC 0.62, read from `openrc --version`, and never when that cannot be
// read.
func TestOpenRCNotifyFollowsTheVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  libservice.Output
		want bool
	}{
		{"0.62.6", libservice.Output{Stdout: []byte("openrc (OpenRC) 0.62.6 (Alpine Linux)\n")}, true},
		{"0.63", libservice.Output{Stdout: []byte("openrc (OpenRC) 0.63\n")}, true},
		{"1.0.1", libservice.Output{Stdout: []byte("openrc (OpenRC) 1.0.1\n")}, true},
		{"0.61.3", libservice.Output{Stdout: []byte("openrc (OpenRC) 0.61.3\n")}, false},
		{"0.60", libservice.Output{Stdout: []byte("openrc (OpenRC) 0.60 (Gentoo)\n")}, false},
		{"unreadable", libservice.Output{Stdout: []byte("openrc: unrecognised option\n")}, false},
		{"failed", libservice.Output{ExitCode: 1, Stderr: []byte("openrc: no\n")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dirs := toolDirs(t, 1)
			testexec.WriteShellStub(t, filepath.Join(dirs[0], "openrc"), "exit 0")
			recordRunner(t, tc.out)
			if got := openRCSupportsNotify(); got != tc.want {
				t.Fatalf("openRCSupportsNotify = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("no openrc", func(t *testing.T) {
		toolDirs(t, 1)
		if openRCSupportsNotify() {
			t.Fatal("notify with no openrc to ask")
		}
	})
}
