//go:build unix

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// toolDirs points the resolver at fresh directories, for one test.
func toolDirs(t *testing.T, n int) []string {
	t.Helper()
	dirs := make([]string, n)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}
	t.Cleanup(OverrideSupervisorToolDirs(dirs...))
	return dirs
}

// recordRunner answers every tool with out and records the command.
func recordRunner(t *testing.T, out libservice.Output) *[]libservice.Command {
	t.Helper()
	var got []libservice.Command
	prev := toolRunner
	toolRunner = libservice.RunnerFunc(func(_ context.Context, c libservice.Command) (libservice.Output, error) {
		got = append(got, c)
		return out, nil
	})
	t.Cleanup(func() { toolRunner = prev })
	return &got
}

// TestResolveToolNeverReadsPATH is MADR 0212 D2-A: a supervisor tool resolves
// in the fixed directories, in their order, or not at all. A tool only on
// PATH is not found, and the error names every directory searched.
func TestResolveToolNeverReadsPATH(t *testing.T) {
	dirs := toolDirs(t, 2)
	onPath := t.TempDir()
	t.Setenv("PATH", onPath)
	testexec.WriteShellStub(t, filepath.Join(dirs[1], "sv"), "exit 0")
	testexec.WriteShellStub(t, filepath.Join(onPath, "s6-svc"), "exit 0")
	testexec.WriteShellStub(t, filepath.Join(onPath, "sv"), "exit 0")
	// Not executable: skipped, so the next directory's copy is the one found.
	// Never run, so it is not a script (MADR 0193 D5).
	if err := os.WriteFile(filepath.Join(dirs[0], "sv"), []byte("not executable\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveTool("sv"); err != nil || got != filepath.Join(dirs[1], "sv") {
		t.Errorf("resolveTool(sv) = %q, %v; want %s", got, err, filepath.Join(dirs[1], "sv"))
	}
	got, err := resolveTool("s6-svc")
	if err == nil {
		t.Fatalf("resolveTool(s6-svc) = %q; want an error: it is only on PATH", got)
	}
	for _, d := range dirs {
		if !strings.Contains(err.Error(), d) {
			t.Errorf("error %q does not name %s", err, d)
		}
	}
	if _, err := resolveTool("sh"); err == nil {
		t.Error("resolveTool(sh) resolved a program that is not a supervisor tool")
	}
}

// TestToolRunnerBuildsItsEnvironment: a tool runs at its absolute path with
// a built environment, never the caller's: PATH, LC_ALL=C, the runtime
// directory, HOME and USER, and XDG_CONFIG_HOME when it is set (0212 PLAN P2
// Deviation 2). Nothing else of the caller's reaches it.
func TestToolRunnerBuildsItsEnvironment(t *testing.T) {
	dirs := toolDirs(t, 1)
	testexec.WriteShellStub(t, filepath.Join(dirs[0], "rc-service"), "exit 0")
	t.Setenv("PATH", "/caller/bin")
	t.Setenv("HOME", "/home/u")
	t.Setenv("USER", "u")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("MCREMOTE_SECRET_ENV", "leak")
	for _, tc := range []struct {
		name, configHome string
		want             []string
	}{
		{"XDG_CONFIG_HOME set", "/home/u/cfg", []string{"PATH=" + toolPath, "LC_ALL=C",
			"XDG_RUNTIME_DIR=/run/user/1000", "HOME=/home/u", "USER=u", "XDG_CONFIG_HOME=/home/u/cfg", "EXTRA=1"}},
		{"XDG_CONFIG_HOME unset", "", []string{"PATH=" + toolPath, "LC_ALL=C",
			"XDG_RUNTIME_DIR=/run/user/1000", "HOME=/home/u", "USER=u", "EXTRA=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tc.configHome)
			got := recordRunner(t, libservice.Output{})
			c := toolFor(KindOpenRCUser, "mcremote", "rc-service", "--user", "--", "mcremote", "status")
			if _, err := c.withEnv("EXTRA=1").run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(*got) != 1 {
				t.Fatalf("ran %d commands, want 1", len(*got))
			}
			cmd := (*got)[0]
			if cmd.Path != filepath.Join(dirs[0], "rc-service") {
				t.Errorf("Path = %q, want the resolved tool", cmd.Path)
			}
			if !slices.Equal(cmd.Env, tc.want) {
				t.Errorf("the tool's environment is not the built one: %s", envDiff(cmd.Env, tc.want))
			}
		})
	}
}

// envDiff says how got differs from want. An entry the test did not set is
// named, never quoted: an environment that leaks the caller's would otherwise
// print the caller's secrets into the test log.
func envDiff(got, want []string) string {
	var missing, extra []string
	for _, w := range want {
		if !slices.Contains(got, w) {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !slices.Contains(want, g) {
			name, _, _ := strings.Cut(g, "=")
			extra = append(extra, name)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return "the same entries, in another order"
	}
	return fmt.Sprintf("missing %q; unexpected (names only) %q", missing, extra)
}

// TestToolRunHonoursItsContext: a tool still running when the caller's
// context ends is killed, and the run returns the context's error.
func TestToolRunHonoursItsContext(t *testing.T) {
	dirs := toolDirs(t, 1)
	testexec.WriteShellStub(t, filepath.Join(dirs[0], "sv"), "exec sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	_, err := toolFor(KindRunit, "/svc", "sv", "status", "/svc").run(ctx)
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("the run took %s after its context ended", took)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's error", err)
	}
}

// TestToolOutputNeverReachesTheTerminal is 0212-MADR M9: what a tool prints
// is captured and returned, and none of it reaches this process's stdout or
// stderr, where the update's --json output goes.
func TestToolOutputNeverReachesTheTerminal(t *testing.T) {
	dirs := toolDirs(t, 1)
	testexec.WriteShellStub(t, filepath.Join(dirs[0], "rc-service"),
		`echo " * Starting mcremote ... [ ok ]"; echo " * warning" >&2; exit 0`)
	capture := func(f **os.File) (restore func() string) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		prev := *f
		*f = w
		return func() string {
			*f = prev
			_ = w.Close()
			b, _ := io.ReadAll(r)
			return string(b)
		}
	}
	stdout, stderr := capture(&os.Stdout), capture(&os.Stderr)
	out, err := toolFor(KindOpenRCUser, "mcremote", "rc-service", "--user", "--", "mcremote", "start").run(context.Background())
	printed, warned := stdout(), stderr()
	if err != nil {
		t.Fatal(err)
	}
	if printed != "" || warned != "" {
		t.Errorf("the tool's output reached the terminal: stdout %q, stderr %q", printed, warned)
	}
	if !bytes.Contains(out.Stdout, []byte("Starting mcremote")) || !bytes.Contains(out.Stderr, []byte("warning")) {
		t.Errorf("the output was not captured: stdout %q, stderr %q", out.Stdout, out.Stderr)
	}
}

// TestToolFailuresAreClassified is the 0212 PLAN P1 table: a tool that exits
// non-zero is a *libservice.CommandError naming the kind, the step and what
// it acted on, wrapped with ErrNotInstalled, ErrNoSupervisor, ErrPermission or
// ErrTimeout when the exit says which. The messages are P1's, verbatim.
func TestToolFailuresAreClassified(t *testing.T) {
	toolDirs(t, 1)
	for _, tc := range []struct {
		name           string
		kind           Kind
		tool           string
		args           []string
		subject        string
		code           int
		stdout, stderr string
		class          error
		step           string
	}{
		{"runit: no definition", KindRunit, "sv", []string{"status", "/svc"}, "/svc", 1,
			"fail: /svc: unable to change to service directory: file does not exist\n", "",
			libservice.ErrNotInstalled, "sv status"},
		{"runit: no supervisor, status", KindRunit, "sv", []string{"status", "/svc"}, "/svc", 1,
			"warning: /svc: unable to open supervise/ok: file does not exist\n", "",
			libservice.ErrNoSupervisor, "sv status"},
		{"runit: no supervisor, up", KindRunit, "sv", []string{"up", "/svc"}, "/svc", 1,
			"warning: /svc: unable to open supervise/control: file does not exist\n", "",
			libservice.ErrNoSupervisor, "sv up"},
		{"runit: permission", KindRunit, "sv", []string{"status", "/svc"}, "/svc", 1,
			"fail: /svc: unable to change to service directory: access denied\n", "",
			libservice.ErrPermission, "sv status"},
		{"runit: a wait timed out", KindRunit, "sv", []string{"restart", "/svc"}, "/svc", 1,
			"timeout: down: /svc: 1s, normally up, want up\n", "", libservice.ErrTimeout, "sv restart"},
		{"runit: unclassified", KindRunit, "sv", []string{"status", "/svc"}, "/svc", 1,
			"fail: /svc: something new\n", "", nil, "sv status"},
		{"s6: no supervisor, svstat", KindS6, "s6-svstat", []string{"/svc"}, "/svc", 1, "",
			"s6-svstat: fatal: unable to read status for /svc: s6-supervise not running\n",
			libservice.ErrNoSupervisor, "s6-svstat"},
		{"s6: no supervisor, svc", KindS6, "s6-svc", []string{"-u", "/svc"}, "/svc", 100, "",
			"s6-svc: fatal: unable to control /svc: supervisor not listening\n", libservice.ErrNoSupervisor, "s6-svc -u"},
		{"s6: permission", KindS6, "s6-svstat", []string{"/svc"}, "/svc", 111, "",
			"s6-svstat: fatal: unable to check /svc: Permission denied\n", libservice.ErrPermission, "s6-svstat"},
		{"OpenRC: no definition", KindOpenRCUser, "rc-service", []string{"--user", "--", "mcremote", "status"},
			"mcremote", 1, "", " * rc-service: service `mcremote' does not exist\n", libservice.ErrNotInstalled,
			"rc-service --user status"},
		{"OpenRC: no session", KindOpenRCUser, "rc-service", []string{"--user", "--", "mcremote", "start"},
			"mcremote", 1, "", " * You are attempting to run an openrc service on a\n * system which openrc did not boot.\n",
			libservice.ErrNoSupervisor, "rc-service --user start"},
		{"OpenRC: no runtime directory", KindOpenRCUser, "rc-service", []string{"--user", "--", "mcremote", "start"},
			"mcremote", 1, "", "XDG_RUNTIME_DIR unset.\n", libservice.ErrNoSupervisor, "rc-service --user start"},
		{"OpenRC: stopped is a failure of status, unclassified", KindOpenRCUser, "rc-service",
			[]string{"--user", "--", "mcremote", "status"}, "mcremote", 3, " * status: stopped\n", "", nil,
			"rc-service --user status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testexec.WriteShellStub(t, filepath.Join(supervisorToolDirs[0], tc.tool), "exit 0")
			recordRunner(t, libservice.Output{ExitCode: tc.code, Stdout: []byte(tc.stdout), Stderr: []byte(tc.stderr)})
			_, err := toolCall{kind: tc.kind, tool: tc.tool, args: tc.args, subject: tc.subject}.run(context.Background())
			var ce *libservice.CommandError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want a *libservice.CommandError", err)
			}
			want := strings.TrimSpace(tc.stderr)
			if want == "" {
				want = strings.TrimSpace(tc.stdout)
			}
			if ce.Backend != string(tc.kind) || ce.Step != tc.step || ce.Name != tc.subject || ce.ExitCode != tc.code ||
				ce.Message != want {
				t.Errorf("CommandError = %+v; want backend %s, step %q, name %s, exit %d, message %q",
					*ce, tc.kind, tc.step, tc.subject, tc.code, want)
			}
			for _, class := range []error{libservice.ErrNotInstalled, libservice.ErrNoSupervisor,
				libservice.ErrPermission, libservice.ErrTimeout} {
				if got := errors.Is(err, class); got != (class == tc.class) {
					t.Errorf("errors.Is(err, %v) = %v; err = %v", class, got, err)
				}
			}
		})
	}
}

// TestDetectionAndTheUpdateResolveAlike: detectLinux finds a supervisor
// through the resolver the update runs its tools with, so a host where PATH
// holds one supervisor's tools and the fixed directories another's is
// detected as the one the update will drive.
func TestDetectionAndTheUpdateResolveAlike(t *testing.T) {
	dirs := toolDirs(t, 1)
	stub := fakeStub(t)
	onPath := t.TempDir()
	for _, n := range []string{"runsvdir", "sv"} {
		if err := os.Symlink(stub, filepath.Join(dirs[0], n)); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"s6-svscan", "s6-svc", "s6-svstat"} {
		if err := os.Symlink(stub, filepath.Join(onPath, n)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", onPath)
	if got := detectLinux(); got != KindRunit {
		t.Fatalf("detectLinux = %s; want runit, whose tools are where the update resolves them", got)
	}
	ran := recordRunner(t, libservice.Output{})
	if _, err := toolFor(KindRunit, "/svc", "sv", "status", "/svc").run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 1 || (*ran)[0].Path != filepath.Join(dirs[0], "sv") {
		t.Fatalf("the update ran %+v; want %s", *ran, filepath.Join(dirs[0], "sv"))
	}
}
