package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// The state of a runit, s6 or OpenRC user service, in the library's type
// (MADR 0212 D16), read from the supervisor's own report as the 0212 PLAN's
// P1 captured it. Stage 2 hands observe's state to libservice.NewSupervised
// as its State; until then the samplers and Inside read it.
//
// A definition that is missing is Installed false, not an error: the adapter
// takes any State error but ErrNoSupervisor as a failure. A service with no
// supervisor is an error wrapping ErrNoSupervisor, with Installed and
// NormallyUp still set, as the library's guide asks.

// observation is one State and what settle needs besides it: how long the
// service has been up, which SupervisedState does not carry.
type observation struct {
	state  libservice.SupervisedState
	uptime time.Duration
}

// observe reads kind's service. def locates it; name is the OpenRC service.
func observe(ctx context.Context, kind Kind, def scriptDef, name string) (observation, error) {
	switch kind {
	case KindRunit:
		return observeRunit(ctx, def)
	case KindS6:
		return observeS6(ctx, def)
	case KindOpenRCUser:
		return observeOpenRC(ctx, def, name)
	}
	return observation{}, fmt.Errorf("no supervisor state for %s", kind)
}

// scriptDefOf locates a result's definition, as setup recorded it.
func scriptDefOf(kind Kind, res Result) scriptDef {
	d := scriptDef{Kind: kind, Path: res.UnitPath}
	switch kind {
	case KindRunit, KindS6:
		d.Dir = filepath.Dir(res.UnitPath)
		d.ScanDir = filepath.Dir(d.Dir)
	case KindOpenRCUser:
		d.Dir = filepath.Dir(res.UnitPath)
		d.ScanDir = d.Dir
	}
	return d
}

// fileExists reports whether path exists, as a file or a link.
func fileExists(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, err
}

// definitionState is what the files say: the definition's existence, and,
// with downFile set, NormallyUp as no down file (runit and s6).
func definitionState(def scriptDef, downFile bool) (libservice.SupervisedState, error) {
	var st libservice.SupervisedState
	ok, err := fileExists(def.Path)
	if err != nil {
		return st, err
	}
	st.Installed = ok
	if downFile {
		down, err := fileExists(filepath.Join(def.Dir, "down"))
		if err != nil {
			return st, err
		}
		st.NormallyUp = !down
	}
	return st, nil
}

// observeRunit reads `sv status <dir>`: "run: <dir>: (pid N) Ns", with ",
// normally down", ", want down" or ", got TERM" after it; "down: <dir>: Ns",
// with ", normally up" or ", want up"; or "finish: …" while the finish script
// runs (P1). runit exits 1 for every failure, classified by message (P2).
func observeRunit(ctx context.Context, def scriptDef) (observation, error) {
	st, err := definitionState(def, true)
	if err != nil {
		return observation{state: st}, err
	}
	out, err := toolFor(KindRunit, def.Dir, "sv", "status", def.Dir).run(ctx)
	if err != nil {
		if !st.Installed && errors.Is(err, libservice.ErrNotInstalled) {
			return observation{state: st}, nil
		}
		return observation{state: st}, err
	}
	return parseRunitStatus(st, string(out.Stdout)), nil
}

// runitStatusRe reads a running service: "run: DIR: (pid 87) 7s".
var runitStatusRe = regexp.MustCompile(`^run: .*\(pid (\d+)\) (\d+)s`)

// parseRunitStatus reads one `sv status` line into st, for a service runsv
// answered for.
func parseRunitStatus(st libservice.SupervisedState, out string) observation {
	line := strings.TrimSpace(out)
	st.Supervised, st.Detail = true, line
	obs := observation{state: st}
	if m := runitStatusRe.FindStringSubmatch(line); m != nil {
		obs.state.Up = true
		obs.state.PID, _ = strconv.Atoi(m[1])
		secs, _ := strconv.Atoi(m[2])
		obs.uptime = time.Duration(secs) * time.Second
		obs.state.WantUp = !strings.Contains(line, "want down")
		return obs
	}
	obs.state.WantUp = strings.HasPrefix(line, "down") && strings.Contains(line, "want up")
	obs.state.Restarting = obs.state.WantUp
	return obs
}

// s6Fields are s6-svstat -o's fields, in the order it prints them (D7-A).
const s6Fields = "up,wantedup,normallyup,ready,pid,exitcode,updownfor"

// observeS6 reads `s6-svstat -o up,wantedup,normallyup,ready,pid,exitcode,updownfor <dir>`,
// such as "true true true false 18 -1 2" (P1). `ready` is read and not
// mapped: the service has no notification-fd, so s6 reports false for a
// healthy daemon, and Ready stays nil (D18). NormallyUp is the down file's
// absence, which also answers with no supervisor. s6 cannot tell a missing
// definition from a missing supervisor; Installed tells them apart.
func observeS6(ctx context.Context, def scriptDef) (observation, error) {
	st, err := definitionState(def, true)
	if err != nil {
		return observation{state: st}, err
	}
	out, err := toolFor(KindS6, def.Dir, "s6-svstat", "-o", s6Fields, def.Dir).run(ctx)
	if err != nil {
		return observation{state: st}, err
	}
	return parseS6Status(st, string(out.Stdout))
}

// parseS6Status reads s6-svstat -o's line into st, which s6-supervise
// answered.
func parseS6Status(st libservice.SupervisedState, out string) (observation, error) {
	f := strings.Fields(out)
	if len(f) != 7 {
		return observation{state: st}, fmt.Errorf("s6-svstat -o %s printed %q, not seven fields", s6Fields, out)
	}
	up, want := f[0] == "true", f[1] == "true"
	st.Supervised, st.Up, st.WantUp, st.Restarting = true, up, want, !up && want
	st.Detail = fmt.Sprintf("up=%s wantedup=%s normallyup=%s ready=%s pid=%s exitcode=%s for %ss",
		f[0], f[1], f[2], f[3], f[4], f[5], f[6])
	obs := observation{state: st}
	if up {
		obs.state.PID, _ = strconv.Atoi(f[4])
		secs, _ := strconv.Atoi(f[6])
		obs.uptime = time.Duration(secs) * time.Second
	}
	return obs, nil
}

// OpenRC's `rc-service --user <name> status` exit codes (P1).
const (
	openRCStarted = 0
	openRCStopped = 3
	openRCCrashed = 32
)

// observeOpenRC reads `rc-service --user -- <name> status`: started (0),
// stopped (3) or crashed (32, "status: crashed" on stderr). No user session
// and no runtime directory are ErrNoSupervisor (P2). NormallyUp is the
// service's link in the user's default runlevel. The PID is the pidfile's,
// in the runtime directory, with no /tmp fallback (D12-A).
func observeOpenRC(ctx context.Context, def scriptDef, name string) (observation, error) {
	st, err := definitionState(def, false)
	if err != nil {
		return observation{state: st}, err
	}
	if st.NormallyUp, err = fileExists(filepath.Join(filepath.Dir(def.ScanDir), "runlevels", "default", name)); err != nil {
		return observation{state: st}, err
	}
	out, err := toolFor(KindOpenRCUser, name, "rc-service", "--user", "--", name, "status").run(ctx)
	text := strings.TrimSpace(string(out.Stdout) + " " + string(out.Stderr))
	switch {
	case errors.Is(err, libservice.ErrNotInstalled) && !st.Installed:
		return observation{state: st}, nil
	case err != nil && out.ExitCode != openRCStopped && out.ExitCode != openRCCrashed:
		return observation{state: st}, err
	}
	st.Supervised, st.Detail = true, text
	switch out.ExitCode {
	case openRCStarted:
		st.Up, st.WantUp = true, true
		st.PID = openRCPID(name)
	case openRCCrashed:
		st.Failed = true
	}
	return observation{state: st}, nil
}

// openRCPID is the service's own process: the PID in
// $XDG_RUNTIME_DIR/<name>.pid, which start-stop-daemon writes for today's
// definition. Under supervise-daemon (D10-A) that file holds the
// supervisor's PID, and the daemon is its child (P1). 0 when it cannot be
// read: no runtime directory, no file, or no such process.
func openRCPID(name string) int {
	rd := os.Getenv("XDG_RUNTIME_DIR")
	if rd == "" {
		var ok bool
		if rd, ok = userRuntimeDir(); !ok {
			return 0
		}
	}
	raw, err := os.ReadFile(filepath.Join(rd, name+".pid"))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0
	}
	comm, err := os.ReadFile(filepath.Join(stateProcRoot, strconv.Itoa(pid), "comm"))
	if err != nil || strings.TrimSpace(string(comm)) != "supervise-daemon" {
		return pid
	}
	return childOf(pid)
}

// stateProcRoot is where processes are read. It is a variable for tests.
var stateProcRoot = "/proc"

// childOf returns a child of pid, read from each process's stat, or 0.
func childOf(pid int) int {
	entries, err := os.ReadDir(stateProcRoot)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		child, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if parent, err := statParent(filepath.Join(stateProcRoot, e.Name(), "stat")); err == nil && parent == pid {
			return child
		}
	}
	return 0
}

// statParent reads the parent from a /proc/<pid>/stat file. The command
// name, the second field, is in parentheses and may hold spaces or
// parentheses itself, so the fields are read after its last ')' (proc(5):
// state, then ppid).
func statParent(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, fmt.Errorf("%s: no command name", path)
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("%s: too few fields", path)
	}
	return strconv.Atoi(fields[1])
}

// startToken is a process's start token, for telling a reused PID from the
// process it once named (D12-A). It is a variable for tests.
var startToken = func(pid int) string {
	if pid <= 0 {
		return ""
	}
	t, _ := procutil.ProcessStartToken(pid)
	return t
}

// observeBounded is observe for a caller with no context, the samplers,
// bounded by cmdTimeout as runCmd was.
func observeBounded(kind Kind, def scriptDef, name string) (observation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	return observe(ctx, kind, def, name)
}
