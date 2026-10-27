package service

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// runit, s6 and OpenRC user services, as setup-service writes them (MADR 0054
// PLAN S5b, amendment S5-2). Each runs the same `serve` command, with the same
// arguments and environment, that the systemd unit renders for the same
// options, and lives where install.sh put its own (scripts/install.sh:299-303).

// isScriptKind reports whether k is a supervisor whose definition is a script.
func (k Kind) isScriptKind() bool {
	switch k {
	case KindRunit, KindS6, KindOpenRCUser:
		return true
	}
	return false
}

// scriptDef locates one supervisor definition.
type scriptDef struct {
	Kind Kind
	// ScanDir is the directory runsvdir or s6-svscan watches; for OpenRC it
	// is the init.d directory.
	ScanDir string
	// Dir is the service directory (runit, s6), or the init.d directory.
	Dir string
	// Path is the run script, or the init script.
	Path string
	// Backup is where a refresh keeps the previous definition. An OpenRC
	// backup stays out of init.d, where OpenRC would take it for a service.
	Backup string
}

// scriptDefFor returns where kind's definition named name lives. A non-empty
// root replaces the scan (or init.d) directory, for tests, as UnitDir does for
// systemd.
func scriptDefFor(kind Kind, name, root string) scriptDef {
	d := scriptDef{Kind: kind}
	switch kind {
	case KindRunit, KindS6:
		d.ScanDir = root
		if d.ScanDir == "" {
			sub := "runit"
			if kind == KindS6 {
				sub = "s6"
			}
			d.ScanDir = filepath.Join(xdgDataHome(), sub, "service")
		}
		d.Dir = filepath.Join(d.ScanDir, name)
		d.Path = filepath.Join(d.Dir, "run")
		d.Backup = d.Path + ".prev"
	case KindOpenRCUser:
		d.ScanDir = root
		if d.ScanDir == "" {
			d.ScanDir = filepath.Join(xdgConfigHome(), "rc", "init.d")
		}
		d.Dir = d.ScanDir
		d.Path = filepath.Join(d.Dir, name)
		d.Backup = filepath.Join(filepath.Dir(d.Dir), name+".prev")
	}
	return d
}

// shq quotes s as one POSIX shell word.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// scriptShebang is the first line of kind's definition.
func scriptShebang(kind Kind) string {
	if kind == KindOpenRCUser {
		return "#!/sbin/openrc-run"
	}
	return "#!/bin/sh"
}

// serveArgv is the command the systemd unit's ExecStart runs, as a list.
func serveArgv(opts Options) []string {
	argv := []string{opts.Binary, "serve"}
	add := func(flag, v string) {
		if v != "" {
			argv = append(argv, flag, v)
		}
	}
	add("--config", opts.ConfigPath)
	add("--data-dir", opts.DataDir)
	add("--listen-host", opts.ListenHost)
	if opts.ListenPort > 0 {
		argv = append(argv, "--listen-port", strconv.Itoa(opts.ListenPort))
	}
	add("--log-level", opts.LogLevel)
	add("--log-format", opts.LogFormat)
	return argv
}

func shJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shq(a)
	}
	return strings.Join(q, " ")
}

// renderScript renders kind's definition for opts. A nil penv computes the
// environment from this process, as Setup does; a non-nil one pins it.
func renderScript(kind Kind, opts Options, penv *renderEnv) (string, error) {
	if !kind.isScriptKind() {
		return "", fmt.Errorf("no script definition for %s", kind)
	}
	renv := currentRenderEnv(opts.Product)
	if penv != nil {
		renv = *penv
	}
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	line(scriptShebang(kind))
	line(managedMarker(opts.Product))
	line("# Do not hand-edit if you re-run setup (use --force to overwrite).")
	def := scriptDefFor(kind, opts.UnitName, "")
	switch kind {
	case KindRunit:
		line("# runit service: SVDIR=" + def.ScanDir + " sv status " + opts.UnitName)
	case KindS6:
		line("# s6 service: s6-svstat " + def.Dir)
	case KindOpenRCUser:
		line("# OpenRC user service: rc-service --user " + opts.UnitName + " status")
		line("description=" + shq(opts.Description))
		line(`pidfile="${XDG_RUNTIME_DIR:-/tmp}"` + shq("/"+opts.UnitName+".pid"))
	}
	// The environment the systemd unit sets with Environment=, in its order.
	// systemd names its runtime directory with %t when none was known; a
	// script has no such specifier, so it leaves the variable as the
	// supervisor's environment has it.
	env := []string{
		"HOME=" + renv.Home, "USER=" + renv.User, "LOGNAME=" + renv.User, "PATH=" + renv.Path,
		"XDG_CONFIG_HOME=" + renv.XDGConfigHome, "XDG_DATA_HOME=" + renv.XDGDataHome,
		"XDG_STATE_HOME=" + renv.XDGStateHome, "XDG_CACHE_HOME=" + renv.XDGCacheHome,
	}
	if renv.XDGRuntimeDir != "" {
		env = append(env, "XDG_RUNTIME_DIR="+renv.XDGRuntimeDir)
	}
	env = append(env, opts.ExtraEnviron...)
	switch kind {
	case KindRunit, KindS6:
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			line("export " + k + "=" + shq(v))
		}
		line("cd " + shq(opts.WorkingDirectory) + " || exit 1")
		line("exec " + shJoin(serveArgv(opts)) + " 2>&1")
	case KindOpenRCUser:
		// openrc-run sources this script, and so does its dependency
		// scan. An export here would replace the PATH it finds its own
		// helpers on (ebegin, eend, shell_var) and break every command,
		// which the 2026-10-19 live probe showed. The environment and the
		// working directory go to start-stop-daemon instead.
		ssd := []string{"start-stop-daemon", "--start", "--background", "--make-pidfile", "--pidfile", `"$pidfile"`,
			"--chdir", shq(opts.WorkingDirectory)}
		for _, kv := range env {
			ssd = append(ssd, "--env", shq(kv))
		}
		ssd = append(ssd, "--exec", shq(opts.Binary), "--", shJoin(serveArgv(opts)[1:]))
		line("")
		line("start() {")
		line(`	ebegin "Starting ${RC_SVCNAME}"`)
		line("	" + strings.Join(ssd, " "))
		line("	eend $?")
		line("}")
		line("")
		line("stop() {")
		line(`	ebegin "Stopping ${RC_SVCNAME}"`)
		line(`	start-stop-daemon --stop --pidfile "$pidfile"`)
		line("	eend $?")
		line("}")
	}
	return b.String(), nil
}

// shellWords splits one line the way renderScript quotes it: whitespace
// separates words, single quotes are literal, double quotes group without
// expansion, and a backslash outside quotes escapes the next character.
func shellWords(s string) ([]string, error) {
	var (
		words   []string
		cur     strings.Builder
		started bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated single quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			started = true
		case c == '"':
			j := strings.IndexByte(s[i+1:], '"')
			if j < 0 {
				return nil, errors.New("unterminated double quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			started = true
		case c == '\\':
			if i+1 >= len(s) {
				return nil, errors.New("trailing backslash")
			}
			cur.WriteByte(s[i+1])
			i++
			started = true
		case c == ' ' || c == '\t':
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if started {
		words = append(words, cur.String())
	}
	return words, nil
}

// recoverScript rebuilds the options and the pinned environment that
// rendered body, as recoverOptions does for a systemd unit. Any line
// renderScript never writes makes the definition one setup-service leaves
// alone: rewriting it would drop what an operator added.
func recoverScript(kind Kind, product, unitName, body string) (recovery, string, bool) {
	rec := recovery{Opts: Options{Product: product, UnitName: unitName}}
	lines := strings.Split(body, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != scriptShebang(kind) || strings.TrimSpace(lines[1]) != managedMarker(product) {
		return rec, "not written by " + product + " setup-service (no managed-by header)", false
	}
	seen := map[string]bool{}
	execs := 0
	for _, raw := range lines[2:] {
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "export "):
			k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
			if !ok || !envKeyRe.MatchString(k+"=") {
				return rec, "has a malformed export line", false
			}
			words, err := shellWords(v)
			if err != nil || len(words) != 1 {
				return rec, "has an export of " + k + " that is not one quoted word", false
			}
			if managedEnvKeys[k] {
				seen[k] = true
				assignManagedEnv(&rec.Env, k, words[0])
				continue
			}
			rec.Opts.ExtraEnviron = append(rec.Opts.ExtraEnviron, k+"="+words[0])
		case strings.HasPrefix(line, "cd "):
			// "cd DIR || exit 1" (or "return 1" in a function).
			words, err := shellWords(line)
			if err != nil || len(words) != 5 || words[2] != "||" ||
				(words[3] != "exit" && words[3] != "return") || words[4] != "1" {
				return rec, "has a cd line setup-service never writes", false
			}
			rec.Opts.WorkingDirectory = words[1]
		case kind != KindOpenRCUser && strings.HasPrefix(line, "exec "):
			execs++
			words, err := shellWords(strings.TrimSuffix(strings.TrimPrefix(line, "exec "), " 2>&1"))
			if err != nil {
				return rec, "has an exec line that does not parse: " + err.Error(), false
			}
			if why, ok := applyServeArgv(&rec.Opts, words, "exec line"); !ok {
				return rec, why, false
			}
		case kind == KindOpenRCUser && strings.HasPrefix(line, "start-stop-daemon --start "):
			execs++
			words, err := shellWords(line)
			if err != nil {
				return rec, "has a start-stop-daemon line that does not parse: " + err.Error(), false
			}
			var bin string
			argv := []string{}
			for i := range words {
				if words[i] == "--" {
					argv = words[i+1:]
					break
				}
				if i+1 >= len(words) {
					continue
				}
				switch words[i] {
				case "--exec":
					bin = words[i+1]
				case "--chdir":
					rec.Opts.WorkingDirectory = words[i+1]
				case "--env":
					k, v, ok := strings.Cut(words[i+1], "=")
					if !ok || !envKeyRe.MatchString(k+"=") {
						return rec, "has a malformed --env", false
					}
					if managedEnvKeys[k] {
						seen[k] = true
						assignManagedEnv(&rec.Env, k, v)
					} else {
						rec.Opts.ExtraEnviron = append(rec.Opts.ExtraEnviron, k+"="+v)
					}
				}
			}
			if bin == "" {
				return rec, "has a start-stop-daemon line with no --exec", false
			}
			if why, ok := applyServeArgv(&rec.Opts, append([]string{bin}, argv...), "start-stop-daemon line"); !ok {
				return rec, why, false
			}
		case kind == KindOpenRCUser && strings.HasPrefix(line, "description="):
			words, err := shellWords(strings.TrimPrefix(line, "description="))
			if err != nil || len(words) != 1 {
				return rec, "has a description that is not one quoted word", false
			}
			rec.Opts.Description = words[0]
		case kind == KindOpenRCUser && (strings.HasPrefix(line, "pidfile=") ||
			line == "start() {" || line == "stop() {" || line == "}" ||
			strings.HasPrefix(line, "ebegin ") || line == "eend $?" ||
			line == `start-stop-daemon --stop --pidfile "$pidfile"`):
			continue
		default:
			return rec, "carries a line setup-service never writes: " + line, false
		}
	}
	if execs != 1 {
		return rec, fmt.Sprintf("has %d command lines, want 1", execs), false
	}
	rec.Warnings = fillMissingEnv(product, &rec.Env, seen)
	return rec, "", true
}

// scriptMode is a definition's mode: executable, and private when --env
// values are in it, as the systemd unit is.
func scriptMode(opts Options) os.FileMode {
	if len(opts.ExtraEnviron) > 0 {
		return 0o700
	}
	return 0o755
}

// setupScript installs kind's definition, then enables and starts it.
func setupScript(kind Kind, opts Options, body string, res Result) (Result, error) {
	def := scriptDefFor(kind, opts.UnitName, opts.UnitDir)
	res.UnitPath = def.Path
	res.Scope = string(kind)
	res.Label = opts.UnitName
	if err := os.MkdirAll(def.Dir, 0o755); err != nil {
		return res, fmt.Errorf("create %s: %w", def.Dir, err)
	}
	if existing, err := os.ReadFile(def.Path); err == nil {
		res.AlreadyExisted = true
		if bytes.Equal(existing, []byte(body)) {
			res.Unchanged = true
		} else if !opts.Force {
			return res, fmt.Errorf("definition already exists at %s with different content (pass --force to overwrite)", def.Path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("stat definition: %w", err)
	}
	if !res.Unchanged {
		if err := writeUnitAtomic(def.Dir, def.Path, []byte(body), scriptMode(opts)); err != nil {
			return res, err
		}
	}
	res.written = true
	return startScript(def, opts, res)
}

// startScript enables and starts an installed definition.
//
// runit and s6 enable a service by its directory having no `down` file. They
// start it through the user's own runsvdir or s6-svscan when one watches the
// directory, and otherwise start one, detached, for this session, as install.sh
// did (:786-807): nothing then starts it at boot, and the result says so.
func startScript(def scriptDef, opts Options, res Result) (Result, error) {
	switch def.Kind {
	case KindRunit, KindS6:
		down := filepath.Join(def.Dir, "down")
		if opts.NoEnable {
			if err := os.WriteFile(down, nil, 0o644); err != nil {
				return res, fmt.Errorf("write %s: %w", down, err)
			}
		} else {
			if err := os.Remove(down); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return res, fmt.Errorf("remove %s: %w", down, err)
			}
			res.Enabled = true
		}
		if opts.NoStart {
			return res, nil
		}
		scanner, ctl := "runsvdir", toolFor(def.Kind, def.Dir, "sv", "restart", def.Dir)
		supervised := filepath.Join(def.Dir, "supervise", "ok")
		if def.Kind == KindS6 {
			scanner, ctl = "s6-svscan", toolFor(def.Kind, def.Dir, "s6-svc", "-r", def.Dir)
			supervised = filepath.Join(def.Dir, "supervise")
		}
		if !supervisorWatching(scanner, def.ScanDir) {
			pid, err := startSessionSupervisor(scanner, def.ScanDir)
			if err != nil {
				return res, fmt.Errorf("definition installed at %s, but %s could not be started: %w", def.Path, scanner, err)
			}
			res.SupervisorStarted, res.SupervisorPID = true, pid
			res.Started = !opts.NoEnable
			return res, nil
		}
		if def.Kind == KindS6 {
			// s6-svscan picks a new directory up on a rescan.
			if _, err := runBounded(toolFor(def.Kind, def.ScanDir, "s6-svscanctl", "-a", def.ScanDir)); err != nil {
				return res, fmt.Errorf("definition installed at %s, but s6-svscanctl -a failed: %w", def.Path, err)
			}
		}
		if _, err := os.Stat(supervised); err == nil {
			// s6-svc -r returns at once, and the old daemon drains after
			// SIGTERM, so a sample taken then sees it still up: the 2026-10-19
			// live probe confirmed a crash-looping definition that way. Note
			// the process the restart replaces (MADR 0054 PLAN S5c).
			if s, err := sampler(def.Kind, res)(); err == nil && s.up {
				res.replacedPid, res.replacedToken = s.pid, s.token
			}
			if _, err := runBounded(ctl); err != nil {
				return res, fmt.Errorf("definition installed at %s, but %s failed: %w", def.Path,
					strings.Join(append([]string{ctl.tool}, ctl.args...), " "), err)
			}
			if def.Kind == KindS6 {
				if _, err := runBounded(toolFor(def.Kind, def.Dir, "s6-svc", "-u", def.Dir)); err != nil {
					return res, fmt.Errorf("definition installed at %s, but s6-svc -u failed: %w", def.Path, err)
				}
			}
			res.Started = true
			return res, nil
		}
		// A new directory: the scanner starts its supervisor, which starts
		// the service unless it is down.
		res.Started = !opts.NoEnable
		return res, nil
	case KindOpenRCUser:
		return startOpenRC(def, opts, res)
	}
	return res, fmt.Errorf("no script definition for %s", def.Kind)
}

// removeScript stops, disables and deletes kind's definition.
func removeScript(kind Kind, opts Options) (Result, error) {
	def := scriptDefFor(kind, opts.UnitName, opts.UnitDir)
	res := Result{UnitName: opts.UnitName, UnitPath: def.Path, Scope: string(kind), Label: opts.UnitName}
	switch kind {
	case KindRunit, KindS6:
		// The supervisor keeps writing into supervise/ while it runs, which
		// is how the live probe's first removal failed ("directory not
		// empty"). A down file stops the scanner restarting the service
		// meanwhile; down then exit stop the service and its supervisor.
		ctl := []toolCall{toolFor(kind, def.Dir, "sv", "down", def.Dir), toolFor(kind, def.Dir, "sv", "exit", def.Dir)}
		supervised := filepath.Join(def.Dir, "supervise", "ok")
		if kind == KindS6 {
			ctl = []toolCall{toolFor(kind, def.Dir, "s6-svc", "-d", def.Dir), toolFor(kind, def.Dir, "s6-svc", "-x", def.Dir)}
			supervised = filepath.Join(def.Dir, "supervise")
		}
		if _, err := os.Stat(def.Dir); err == nil {
			_ = os.WriteFile(filepath.Join(def.Dir, "down"), nil, 0o644)
		}
		if _, err := os.Stat(supervised); err == nil {
			for _, c := range ctl {
				_, _ = runBounded(c)
			}
		}
		if err := removeAllRetry(def.Dir); err != nil {
			return res, fmt.Errorf("remove %s: %w", def.Dir, err)
		}
		if kind == KindS6 && supervisorWatching("s6-svscan", def.ScanDir) {
			// Reap the supervisor of the directory that is gone.
			_, _ = runBounded(toolFor(kind, def.ScanDir, "s6-svscanctl", "-an", def.ScanDir))
		}
	case KindOpenRCUser:
		_, _ = runBounded(toolFor(kind, opts.UnitName, "rc-service", "--user", "--", opts.UnitName, "stop"))
		_, _ = runBounded(toolFor(kind, opts.UnitName, "rc-update", "--user", "del", opts.UnitName, "default"))
		if err := os.Remove(def.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, fmt.Errorf("remove %s: %w", def.Path, err)
		}
		// Kept beside init.d, so it outlives the script (MADR 0210 D9).
		backup, err := removeBackup(def.Backup)
		if err != nil {
			return res, err
		}
		res.RemovedBackup = backup
	default:
		return res, fmt.Errorf("no script definition for %s", kind)
	}
	res.Removed = true
	return res, nil
}

// refreshScript is refreshSystemd for a script definition: re-render from this
// binary's template with the recovered options, and rewrite only what this
// package wrote and can reproduce. A supervisor has nothing to reload; the
// next start runs the new definition.
func refreshScript(kind Kind, opts Options, ro RefreshOptions) (RefreshResult, error) {
	def := scriptDefFor(kind, opts.UnitName, opts.UnitDir)
	res := RefreshResult{Verdict: VerdictNone, Path: def.Path}
	current, err := os.ReadFile(def.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, fmt.Errorf("read definition: %w", err)
	}
	text := string(current)
	rec, why, ok := recoverScript(kind, opts.Product, opts.UnitName, text)
	if !ok {
		res.Verdict, res.Reason = VerdictKept, why
		return res, nil
	}
	res.Warnings = append(res.Warnings, rec.Warnings...)
	res.Warnings = append(res.Warnings, pathDriftWarning(opts.Product, rec.Env)...)
	res.Warnings = append(res.Warnings, binaryWarning(rec.Opts.Binary)...)

	want, err := renderScript(kind, rec.Opts, &rec.Env)
	if err != nil {
		return res, err
	}
	res.Body = want
	if want == text {
		res.Verdict = VerdictUnchanged
		return res, nil
	}
	res.Verdict, res.Changed = VerdictRefreshed, true
	if ro.PrintOnly {
		return res, nil
	}
	mode := scriptMode(rec.Opts)
	if err := os.WriteFile(def.Backup, current, mode); err != nil {
		return res, fmt.Errorf("write %s: %w", def.Backup, err)
	}
	res.BackupPath = def.Backup
	if err := writeUnitAtomic(def.Dir, def.Path, []byte(want), mode); err != nil {
		return res, err
	}
	return res, nil
}

// The runlevels openrc-user.sh creates for a user session
// (/usr/libexec/rc/sh/openrc-user.sh in OpenRC 0.62).
var openRCUserRunlevels = []string{"boot", "default", "shutdown"}

// startOpenRC enables and starts an OpenRC user service.
//
// OpenRC user services run inside a user session: openrc-user.sh, which
// pam_openrc runs at login, creates the user's runlevels and runs
// `openrc --user boot` and `openrc --user default`. Without that session
// rc-update has no runlevel to add to and rc-service refuses to run, which the
// 2026-10-19 live probe showed. When none is running, setup starts it the same
// way, for this login, as runit and s6 start their scanner (owner decision,
// MADR 0054 PLAN S5b Deviation 2). Nothing then starts it at boot unless
// pam_openrc does, and the result says so.
func startOpenRC(def scriptDef, opts Options, res Result) (Result, error) {
	rd := os.Getenv("XDG_RUNTIME_DIR")
	if rd == "" {
		var ok bool
		if rd, ok = userRuntimeDir(); !ok {
			return res, fmt.Errorf("definition installed at %s, but OpenRC user services need XDG_RUNTIME_DIR, which is unset here", def.Path)
		}
	}
	envs := []string{"XDG_RUNTIME_DIR=" + rd}
	rcDir := filepath.Dir(def.ScanDir)
	for _, lvl := range openRCUserRunlevels {
		if err := os.MkdirAll(filepath.Join(rcDir, "runlevels", lvl), 0o755); err != nil {
			return res, fmt.Errorf("create the OpenRC user runlevel %s: %w", lvl, err)
		}
	}
	if !opts.NoEnable {
		if _, err := runBounded(toolFor(def.Kind, opts.UnitName, "rc-update", "--user", "add", opts.UnitName, "default").withEnv(envs...)); err != nil {
			return res, fmt.Errorf("definition installed at %s, but rc-update --user add failed: %w", def.Path, err)
		}
		res.Enabled = true
	}
	if opts.NoStart {
		return res, nil
	}
	if _, err := os.Stat(filepath.Join(rd, "openrc", "softlevel")); err != nil {
		// No session: start one. Its default runlevel starts the service
		// when it is enabled.
		for _, lvl := range []string{"boot", "default"} {
			if _, err := runBounded(toolFor(def.Kind, lvl, "openrc", "--user", lvl).withEnv(envs...)); err != nil {
				return res, fmt.Errorf("definition installed at %s, but openrc --user %s failed: %w", def.Path, lvl, err)
			}
		}
		res.SupervisorStarted = true
		if !opts.NoEnable {
			res.Started = true
			return res, nil
		}
	}
	verb := "restart"
	if res.SupervisorStarted {
		verb = "start"
	}
	if _, err := runBounded(toolFor(def.Kind, opts.UnitName, "rc-service", "--user", "--", opts.UnitName, verb).withEnv(envs...)); err != nil {
		return res, fmt.Errorf("definition installed at %s, but rc-service --user %s %s failed: %w", def.Path, opts.UnitName, verb, err)
	}
	res.Started = true
	return res, nil
}

// removeRetryTimeout bounds removeAllRetry. It is a variable for tests.
var removeRetryTimeout = 10 * time.Second

// removeAll is os.RemoveAll. It is a variable for tests.
var removeAll = os.RemoveAll

// removeAllRetry removes dir, retrying while a supervisor that is exiting
// still writes into it.
func removeAllRetry(dir string) error {
	deadline := time.Now().Add(removeRetryTimeout)
	for {
		err := removeAll(dir)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// OverrideSupervisorWatching replaces the probe for a running runsvdir or
// s6-svscan, for tests outside this package. Restore with the returned func.
func OverrideSupervisorWatching(fn func(scanner, dir string) bool) (restore func()) {
	prev := supervisorWatching
	supervisorWatching = fn
	return func() { supervisorWatching = prev }
}
