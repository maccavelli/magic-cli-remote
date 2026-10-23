package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// scriptBackend is the update's view of a runit, s6 or OpenRC user service:
// a selfupdate.Lifecycle and EnabledLifecycle built on the same control code
// setup-service uses (MADR 0054 amendment S5-3, PLAN S5f). The library has no
// backend for these supervisors, and without one `update` replaced the binary
// and left the supervisor running the old process.
type scriptBackend struct {
	kind Kind
	name string
	def  scriptDef
}

var _ Backend = (*scriptBackend)(nil)

func newScriptBackend(kind Kind, name string) *scriptBackend {
	return &scriptBackend{kind: kind, name: name, def: scriptDefFor(kind, name, "")}
}

// result is what the settle and its samplers read.
func (b *scriptBackend) result() Result {
	return Result{UnitName: b.name, Label: b.name, UnitPath: b.def.Path}
}

// stopTimeout bounds Stop's wait for the service to go down. It is a
// variable for tests.
var stopTimeout = 15 * time.Second

// Installed reports whether the definition exists.
func (b *scriptBackend) Installed(context.Context, string) (bool, error) {
	fi, err := os.Stat(b.def.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !fi.IsDir(), nil
}

// Enabled reports whether the service starts unattended: a runit or s6
// service directory with no `down` file, or an OpenRC service in the user's
// default runlevel, as setup-service enables them.
func (b *scriptBackend) Enabled(context.Context, string) (bool, error) {
	var path string
	want := false
	switch b.kind {
	case KindRunit, KindS6:
		path = filepath.Join(b.def.Dir, "down")
	case KindOpenRCUser:
		path, want = filepath.Join(filepath.Dir(b.def.ScanDir), "runlevels", "default", b.name), true
	}
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return want, nil
	case errors.Is(err, fs.ErrNotExist):
		return !want, nil
	}
	return false, err
}

// Running reports whether the service's process is up.
func (b *scriptBackend) Running(context.Context, string) (bool, error) {
	s, err := sampler(b.kind, b.result())()
	return s.up, err
}

// parentOf returns a process's parent PID: procParent, which reads /proc on
// Linux, the only OS a script backend runs on (backend.go), and is nil
// elsewhere. It is a variable for tests.
var parentOf = procParent

// maxAncestry bounds Inside's walk, in case a parent chain loops.
const maxAncestry = 4096

// supervisedPID is the PID of the service's own process, or 0 when it is not
// running or cannot be read. runit and s6 report it in their status, as the
// settle reads it. OpenRC's start-stop-daemon writes it to
// "${XDG_RUNTIME_DIR:-/tmp}/<name>.pid" (supervisor.go), resolved in the
// environment OpenRC started the service with, so each candidate directory is
// tried in turn.
func (b *scriptBackend) supervisedPID() int {
	if b.kind != KindOpenRCUser {
		s, err := sampler(b.kind, b.result())()
		if err != nil || !s.up {
			return 0
		}
		return s.pid
	}
	var dirs []string
	if rd := os.Getenv("XDG_RUNTIME_DIR"); rd != "" {
		dirs = append(dirs, rd)
	}
	if rd, ok := userRuntimeDir(); ok {
		dirs = append(dirs, rd)
	}
	for _, dir := range append(dirs, "/tmp") {
		raw, err := os.ReadFile(filepath.Join(dir, b.name+".pid"))
		if err != nil {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// Inside reports whether this process descends from the service's own
// process (MADR 0210 D4). An agent the daemon runs, and an update the agent
// starts, are its descendants. When the daemon stops, its teardown ends the
// agents' process groups (daemon.go, mgr.CloseAll), so such an update dies
// before it can start the service again, and the supervisor holds it down.
// The 0210 PLAN's R1 reproduced that under runit, s6 and OpenRC. A service
// with no running process has no inside, and off Linux there is no parent to
// read. An error reading the ancestry is returned, never read as "outside".
func (b *scriptBackend) Inside(context.Context) (bool, error) {
	if parentOf == nil {
		return false, nil
	}
	pid := b.supervisedPID()
	if pid <= 0 {
		return false, nil
	}
	p := os.Getpid()
	for range maxAncestry {
		if p == pid {
			return true, nil
		}
		if p <= 1 {
			return false, nil
		}
		parent, err := parentOf(p)
		if err != nil {
			return false, fmt.Errorf("read the parent of process %d: %w", p, err)
		}
		p = parent
	}
	return false, fmt.Errorf("process %d's ancestry is longer than %d", os.Getpid(), maxAncestry)
}

// Stop stops the service and waits for it to be down. From inside the
// service it refuses, as the library's backends do, with
// service.ErrInsideService, and stops nothing: the managed installer then
// leaves the service running and fails the update (MADR 0210 D4). runit, s6
// and OpenRC have no handoff yet (go-selfupdate-lib#2).
func (b *scriptBackend) Stop(ctx context.Context, product string) error {
	inside, err := b.Inside(ctx)
	if err != nil {
		return fmt.Errorf("%s: cannot tell whether this process runs inside it: %w", b.name, err)
	}
	if inside {
		return fmt.Errorf("%s: %w", b.name, libservice.ErrInsideService)
	}
	switch b.kind {
	case KindRunit:
		err = runCmd("sv", "down", b.def.Dir)
	case KindS6:
		err = runCmd("s6-svc", "-d", b.def.Dir)
	case KindOpenRCUser:
		err = runCmd("rc-service", "--user", b.name, "stop")
	}
	if err != nil {
		return err
	}
	deadline := time.Now().Add(stopTimeout)
	for {
		up, err := b.Running(ctx, product)
		if err == nil && !up {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is still running %s after it was stopped", b.name, stopTimeout)
		}
		time.Sleep(settleTick)
	}
}

// Start starts the service. With no supervisor running for it, it starts one
// for this session, as setup-service does: after a logout or a reboot the
// session's runsvdir, s6-svscan or OpenRC user session is gone, and the
// service could not otherwise be started.
func (b *scriptBackend) Start(context.Context, string) error {
	switch b.kind {
	case KindRunit, KindS6:
		scanner, up := "runsvdir", []string{"sv", "up", b.def.Dir}
		if b.kind == KindS6 {
			scanner, up = "s6-svscan", []string{"s6-svc", "-u", b.def.Dir}
		}
		if !supervisorWatching(scanner, b.def.ScanDir) {
			if _, err := startSessionSupervisor(scanner, b.def.ScanDir); err != nil {
				return err
			}
			// A new scanner starts the service unless it is down; a down
			// service needs its supervisor first, which it does not have yet.
			return nil
		}
		return runCmd(up[0], up[1:]...)
	case KindOpenRCUser:
		rd := os.Getenv("XDG_RUNTIME_DIR")
		if rd == "" {
			rd, _ = userRuntimeDir()
		}
		envs := []string{"XDG_RUNTIME_DIR=" + rd}
		if _, err := os.Stat(filepath.Join(rd, "openrc", "softlevel")); err != nil {
			for _, lvl := range []string{"boot", "default"} {
				if err := runCmdEnv(envs, "openrc", "--user", lvl); err != nil {
					return err
				}
			}
		}
		return runCmdEnv(envs, "rc-service", "--user", b.name, "start")
	}
	return fmt.Errorf("no start for %s", b.kind)
}

// WaitHealthy is S5c's settle: running, confirmed, or an error the update
// rolls back on.
func (b *scriptBackend) WaitHealthy(context.Context, string) error {
	secs, err := waitSeconds()
	if err != nil {
		return err
	}
	health, why := settle(b.kind, b.result(), secs)
	switch health {
	case HealthRunning:
		return nil
	case HealthStarting:
		return fmt.Errorf("%s was not confirmed running within %ds", b.name, secs)
	}
	return fmt.Errorf("%s is not running: %s", b.name, why)
}
