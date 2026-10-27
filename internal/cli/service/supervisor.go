package service

import (
	"bytes"
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
		// OpenRC's default functions under supervise-daemon, which restarts
		// a daemon that dies and kills one that will not stop (MADR 0212
		// D10-A, measured in its PLAN's P1).
		//
		// There is no notify="socket:ready". supervise-daemon never removes
		// the service's notify socket, so every start after the first fails
		// with "bind: Address in use", and it waits for READY=1 with no
		// timeout (MADR 0212 amendment C, C1; OpenRC 0.62.6 and master).
		// Readiness is setup's check after the start, as on runit and s6.
		//
		// openrc-run sources this script, and so does its dependency scan.
		// An export here would replace the PATH it finds its own helpers on
		// (ebegin, eend, shell_var) and break every command, which the
		// 2026-10-19 live probe showed. The environment goes to
		// supervise-daemon instead.
		//
		// supervise-daemon.sh passes command, command_args, directory, the
		// logs and supervise_daemon_args through eval, so each value is
		// shell words, quoted once more for the assignment.
		var envArgs []string
		for _, kv := range env {
			envArgs = append(envArgs, "--env", kv)
		}
		argv := serveArgv(opts)
		logFile := filepath.Join(scriptLogDir(opts.Product, renv), opts.UnitName+".log")
		line("supervisor=supervise-daemon")
		line("command=" + shq(shWord(argv[0])))
		line("command_args=" + shq(shWords(argv[1:])))
		line("directory=" + shq(shWord(opts.WorkingDirectory)))
		line("supervise_daemon_args=" + shq(shWords(envArgs)))
		line(`pidfile="${XDG_RUNTIME_DIR}"` + shq("/"+opts.UnitName+".pid"))
		line("output_log=" + shq(shWord(logFile)))
		line("error_log=" + shq(shWord(logFile)))
		// The unit's StartLimitBurst=30 in StartLimitIntervalSec=300.
		line("respawn_delay=5")
		line("respawn_max=30")
		line("respawn_period=300")
		line(`retry="TERM/45/KILL/5"`)
		// The log directory, made at each start, as runit's and s6's
		// log/run make theirs.
		line("")
		line("start_pre() {")
		line("	checkpath -d -m 0700 " + shq(scriptLogDir(opts.Product, renv)))
		line("}")
	}
	return b.String(), nil
}

// safeWordRe is a word the shell reads as itself, unquoted.
var safeWordRe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

// shWord is s as one shell word: as it is when the shell reads it so, and
// quoted otherwise, so a plain path stays readable.
func shWord(s string) string {
	if safeWordRe.MatchString(s) {
		return s
	}
	return shq(s)
}

// shWords is words as shell words, for a value eval reads.
func shWords(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = shWord(w)
	}
	return strings.Join(q, " ")
}

// scriptLogDir is where a runit, s6 or OpenRC service of product logs, as
// the systemd unit's journal stands in for: $XDG_STATE_HOME/<product>/log
// (MADR 0212 D9-A, D10-A).
func scriptLogDir(product string, renv renderEnv) string {
	return filepath.Join(renv.XDGStateHome, product, "log")
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
	addEnv := func(kv string) (string, bool) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !envKeyRe.MatchString(k+"=") {
			return "has a malformed --env", false
		}
		if managedEnvKeys[k] {
			seen[k] = true
			assignManagedEnv(&rec.Env, k, v)
		} else {
			rec.Opts.ExtraEnviron = append(rec.Opts.ExtraEnviron, k+"="+v)
		}
		return "", true
	}
	// The supervise-daemon form (MADR 0212 D10-A), read beside the
	// start-stop-daemon form it replaces (D15-A).
	var sdBin string
	var sdArgs []string
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
					if why, ok := addEnv(words[i+1]); !ok {
						return rec, why, false
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
		case kind == KindOpenRCUser && strings.HasPrefix(line, "command="):
			execs++
			words, ok := evalWords(strings.TrimPrefix(line, "command="))
			if !ok || len(words) != 1 {
				return rec, "has a command that is not one word", false
			}
			sdBin = words[0]
		case kind == KindOpenRCUser && strings.HasPrefix(line, "command_args="):
			words, ok := evalWords(strings.TrimPrefix(line, "command_args="))
			if !ok {
				return rec, "has command_args that do not parse", false
			}
			sdArgs = words
		case kind == KindOpenRCUser && strings.HasPrefix(line, "directory="):
			words, ok := evalWords(strings.TrimPrefix(line, "directory="))
			if !ok || len(words) != 1 {
				return rec, "has a directory that is not one word", false
			}
			rec.Opts.WorkingDirectory = words[0]
		case kind == KindOpenRCUser && strings.HasPrefix(line, "supervise_daemon_args="):
			words, ok := evalWords(strings.TrimPrefix(line, "supervise_daemon_args="))
			if !ok || len(words)%2 != 0 {
				return rec, "has supervise_daemon_args that are not --env pairs", false
			}
			for i := 0; i < len(words); i += 2 {
				if words[i] != "--env" {
					return rec, "has supervise_daemon_args other than --env", false
				}
				if why, ok := addEnv(words[i+1]); !ok {
					return rec, why, false
				}
			}
		case kind == KindOpenRCUser && (strings.HasPrefix(line, "output_log=") || strings.HasPrefix(line, "error_log=") ||
			openRCFixedLines[line] || line == "start_pre() {" || strings.HasPrefix(line, "checkpath -d -m 0700 ")):
			// What setup writes the same for every install: the logs follow
			// the recovered environment's XDG_STATE_HOME.
			continue
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
	if sdBin != "" {
		if why, ok := applyServeArgv(&rec.Opts, append([]string{sdBin}, sdArgs...), "command line"); !ok {
			return rec, why, false
		}
	}
	rec.Warnings = fillMissingEnv(product, &rec.Env, seen)
	return rec, "", true
}

// openRCFixedLines are the supervise-daemon form's lines that hold no option
// (MADR 0212 D10-A). notify is no longer written, but a script P4 first wrote
// has it, and recovering that script lets a refresh move it off (amendment C).
var openRCFixedLines = map[string]bool{
	"supervisor=supervise-daemon": true,
	"respawn_delay=5":             true,
	"respawn_max=30":              true,
	"respawn_period=300":          true,
	`retry="TERM/45/KILL/5"`:      true,
	`notify="socket:ready"`:       true,
}

// evalWords reads an assignment's value as supervise-daemon.sh's eval does:
// one quoted word, which holds shell words.
func evalWords(value string) ([]string, bool) {
	outer, err := shellWords(value)
	if err != nil || len(outer) != 1 {
		return nil, false
	}
	inner, err := shellWords(outer[0])
	if err != nil {
		return nil, false
	}
	return inner, true
}

// scriptAux is a file setup-service writes beside a definition (MADR 0212
// D9-A): runit's and s6's log service, and s6's timeout-kill.
type scriptAux struct {
	path string
	body string
	mode os.FileMode
	// managed reports whether an existing body is one setup-service wrote,
	// which it may replace.
	managed func(existing string) bool
}

// scriptAuxFor is kind's files beside def, for product's environment.
//
//   - log/run: the service's own logger, svlogd for runit and s6-log for s6,
//     into scriptLogDir. The run script's output, 2>&1, is its input, as the
//     unit's goes to the journal.
//   - timeout-kill, for s6: s6 kills a daemon still alive 45000 ms after
//     s6-svc -d (P1: 45.3 s). runit has no such file; its kill is the
//     adapter's, in stage 2.
func scriptAuxFor(kind Kind, def scriptDef, product string, renv renderEnv) []scriptAux {
	if kind != KindRunit && kind != KindS6 {
		return nil
	}
	logger := "svlogd -tt"
	if kind == KindS6 {
		logger = "s6-log T"
	}
	marker := managedMarker(product)
	logRun := scriptAux{
		path: filepath.Join(def.Dir, "log", "run"),
		body: "#!/bin/sh\n" + marker + "\n# Do not hand-edit if you re-run setup (use --force to overwrite).\n" +
			// The supervisor tools' fixed directories (MADR 0212 D2-A).
			"PATH=" + toolPath + "\nexport PATH\n" +
			// Made at each start, so a log directory removed since still
			// logs, and setup writes nothing outside the definition.
			"mkdir -p -m 0700 " + shq(scriptLogDir(product, renv)) + " || exit 1\n" +
			"exec " + logger + " " + shq(scriptLogDir(product, renv)) + "\n",
		mode: 0o755,
		managed: func(s string) bool {
			lines := strings.SplitN(s, "\n", 3)
			return len(lines) >= 2 && lines[1] == marker
		},
	}
	aux := []scriptAux{logRun}
	if kind == KindS6 {
		aux = append(aux, scriptAux{
			path: filepath.Join(def.Dir, "timeout-kill"), body: "45000\n", mode: 0o644,
			managed: func(string) bool { return true },
		})
	}
	return aux
}

// pendingAux returns the files of aux that differ from what is on disk, and
// the first that exists but setup-service did not write.
func pendingAux(aux []scriptAux) (pending []scriptAux, foreign string, err error) {
	for _, a := range aux {
		existing, err := os.ReadFile(a.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			pending = append(pending, a)
		case err != nil:
			return nil, "", fmt.Errorf("read %s: %w", a.path, err)
		case string(existing) == a.body:
		case !a.managed(string(existing)):
			if foreign == "" {
				foreign = a.path
			}
			pending = append(pending, a)
		default:
			pending = append(pending, a)
		}
	}
	return pending, foreign, nil
}

// writeScriptFiles writes the pending files, each atomically. The log
// directory is the definition's to make, at each start.
func writeScriptFiles(pending []scriptAux) error {
	for _, a := range pending {
		dir := filepath.Dir(a.path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := writeUnitAtomic(dir, a.path, []byte(a.body), a.mode); err != nil {
			return err
		}
	}
	return nil
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
	renv := currentRenderEnv(opts.Product)
	pending, foreign, err := pendingAux(scriptAuxFor(kind, def, opts.Product, renv))
	if err != nil {
		return res, err
	}
	if foreign != "" && !opts.Force {
		return res, fmt.Errorf("%s already exists and setup-service did not write it (pass --force to overwrite)", foreign)
	}
	mainSame := false
	if existing, err := os.ReadFile(def.Path); err == nil {
		res.AlreadyExisted = true
		if bytes.Equal(existing, []byte(body)) {
			mainSame = true
		} else if !opts.Force {
			return res, fmt.Errorf("definition already exists at %s with different content (pass --force to overwrite)", def.Path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("stat definition: %w", err)
	}
	if !mainSame {
		if err := writeUnitAtomic(def.Dir, def.Path, []byte(body), scriptMode(opts)); err != nil {
			return res, err
		}
	}
	if err := writeScriptFiles(pending); err != nil {
		return res, err
	}
	res.Unchanged = mainSame && len(pending) == 0
	res.written = true
	return startScript(def, opts, res)
}

// adoptTimeout bounds adoptLogService: the daemon's 45 s drain when it runs,
// then runsvdir's scan, every 5 s (MADR 0212 PLAN P4 Deviation 3: a new runsv
// in 3.8 s with the daemon down, 5.1 s with it up). It is a variable for tests.
var adoptTimeout = 60 * time.Second

// logServiceDef locates the log service beside def as a service of its own.
func logServiceDef(def scriptDef) scriptDef {
	dir := filepath.Join(def.Dir, "log")
	return scriptDef{Kind: def.Kind, ScanDir: def.Dir, Dir: dir, Path: filepath.Join(dir, "run")}
}

// logServiceSupervised reports whether the log service beside def answers its
// supervisor's status. A supervisor that is not running it is false, not an
// error. On runit that settles adoption, since the one runsv runs both; s6
// needs s6LoggerConnected. It is a variable for tests.
var logServiceSupervised = func(ctx context.Context, def scriptDef) (bool, error) {
	obs, err := observe(ctx, def.Kind, logServiceDef(def), "")
	if errors.Is(err, libservice.ErrNoSupervisor) {
		return false, nil
	}
	return obs.state.Supervised, err
}

// s6SupervisorPID is the PID of the s6-supervise that runs def's service, or
// 0: the process named s6-supervise whose working directory is the service
// directory, which s6-supervise changes into. The log service's has the log
// directory. It is a variable for tests.
var s6SupervisorPID = func(def scriptDef) int {
	want, err := filepath.EvalSymlinks(def.Dir)
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir(stateProcRoot)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(stateProcRoot, e.Name())
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "s6-supervise" {
			continue
		}
		if cwd, err := filepath.EvalSymlinks(filepath.Join(dir, "cwd")); err == nil && cwd == want {
			return pid
		}
	}
	return 0
}

// s6LoggerConnected reports whether the daemon's s6-supervise writes into the
// pipe its log service's s6-supervise reads. s6-svscan makes that pipe when
// it takes the log service up, and hands it to the service's s6-supervise
// only when it starts one; a supervised log service is no proof, since any
// rescan takes it up (0212 PLAN P4 Deviation 4b). It is a variable for tests.
var s6LoggerConnected = func(def scriptDef) bool {
	svc, lg := s6SupervisorPID(def), s6SupervisorPID(logServiceDef(def))
	if svc == 0 || lg == 0 {
		return false
	}
	out, err := os.Readlink(filepath.Join(stateProcRoot, strconv.Itoa(svc), "fd", "1"))
	if err != nil {
		return false
	}
	in, err := os.Readlink(filepath.Join(stateProcRoot, strconv.Itoa(lg), "fd", "0"))
	return err == nil && strings.HasPrefix(out, "pipe:") && out == in
}

// pollUntil checks cond every settleTick until it holds, or ctx ends.
func pollUntil(ctx context.Context, cond func() (bool, error)) error {
	for {
		ok, err := cond()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(settleTick):
		}
	}
}

// adoptLogService has the supervisor take up a log service setup added to a
// service it already supervises, and reports whether it had to. runsv and
// s6-svscan read a service's log directory only when they first take the
// service up (MADR 0212 amendment C, C2), so a refreshed install from before
// P4 would never log. The starts Magic drives call it, never the refresh: the
// update runs the refresh between its stop and its start (C3).
//
//   - runit, when no runsv runs the log service: `sv exit`. runsv stops the
//     daemon and exits, and runsvdir starts a new runsv, which runs the
//     service and its log.
//   - s6, when the daemon's s6-supervise does not write into the log
//     service's pipe: a rescan first, which takes the log service and its
//     pipe up, then `s6-svc -dx`, so s6-svscan starts the service's
//     s6-supervise again, onto that pipe. In the other order the daemon is
//     back before the logger exists, and logs nothing (P4 Deviation 3).
//
// Either way the new supervisor starts the daemon unless a down file says
// not to; the caller then brings it up as before.
func adoptLogService(ctx context.Context, def scriptDef) (bool, error) {
	if def.Kind != KindRunit && def.Kind != KindS6 {
		return false, nil
	}
	if ok, err := fileExists(logServiceDef(def).Path); err != nil || !ok {
		return false, err
	}
	if def.Kind == KindRunit {
		if ok, err := logServiceSupervised(ctx, def); err != nil || ok {
			return false, err
		}
	} else if s6LoggerConnected(def) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, adoptTimeout)
	defer cancel()
	switch def.Kind {
	case KindRunit:
		if _, err := toolFor(def.Kind, def.Dir, "sv", "exit", def.Dir).run(ctx); err != nil {
			return false, err
		}
		logUp := func() (bool, error) { return logServiceSupervised(ctx, def) }
		if err := pollUntil(ctx, logUp); err != nil {
			return true, fmt.Errorf("%s: runsvdir did not start its log service within %s: %w", def.Dir, adoptTimeout, err)
		}
	case KindS6:
		if _, err := toolFor(def.Kind, def.ScanDir, "s6-svscanctl", "-an", def.ScanDir).run(ctx); err != nil {
			return false, err
		}
		// The log service's s6-supervise, by its process: its status can read
		// "No such file or directory" while it starts (P4 Deviation 4b).
		logUp := func() (bool, error) { return s6SupervisorPID(logServiceDef(def)) != 0, nil }
		if err := pollUntil(ctx, logUp); err != nil {
			return false, fmt.Errorf("%s: s6-svscan did not start its log service within %s: %w", def.Dir, adoptTimeout, err)
		}
		if _, err := toolFor(def.Kind, def.Dir, "s6-svc", "-dx", def.Dir).run(ctx); err != nil {
			return false, err
		}
		// An s6-supervise on the log service's pipe, which only one started
		// after the rescan has, and answering, so the caller's s6-svc -u
		// reaches it.
		renewed := func() (bool, error) {
			if !s6LoggerConnected(def) {
				return false, nil
			}
			obs, err := observe(ctx, def.Kind, def, "")
			return err == nil && obs.state.Supervised, nil
		}
		if err := pollUntil(ctx, renewed); err != nil {
			return true, fmt.Errorf("%s: s6-svscan did not start its s6-supervise again onto the log service within %s: %w", def.Dir, adoptTimeout, err)
		}
	}
	return true, nil
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
			// A log service this setup added is taken up by restarting the
			// supervisor, which restarts the daemon too: then it is only
			// brought up, not restarted again.
			adopted, err := adoptLogService(context.Background(), def)
			if err != nil {
				return res, fmt.Errorf("definition installed at %s, but its log service was not started: %w", def.Path, err)
			}
			if adopted {
				ctl = toolFor(def.Kind, def.Dir, "sv", "up", def.Dir)
				if def.Kind == KindS6 {
					ctl = toolFor(def.Kind, def.Dir, "s6-svc", "-u", def.Dir)
				}
			}
			if _, err := runBounded(ctl); err != nil {
				return res, fmt.Errorf("definition installed at %s, but %s failed: %w", def.Path,
					strings.Join(append([]string{ctl.tool}, ctl.args...), " "), err)
			}
			if def.Kind == KindS6 && !adopted {
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
		// s6 supervises the log service on its own (MADR 0212 D9-A): stop it
		// and its supervisor too. runit's runsv runs both, and exits with
		// both.
		logDir := filepath.Join(def.Dir, "log")
		if _, err := os.Stat(filepath.Join(logDir, "supervise")); kind == KindS6 && err == nil {
			_, _ = runBounded(toolFor(kind, logDir, "s6-svc", "-d", logDir))
			_, _ = runBounded(toolFor(kind, logDir, "s6-svc", "-x", logDir))
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
	// The files beside it too: an install from before MADR 0212 has none,
	// and the refresh brings them (D15-A).
	pending, foreign, err := pendingAux(scriptAuxFor(kind, def, opts.Product, rec.Env))
	if err != nil {
		return res, err
	}
	if foreign != "" {
		res.Verdict, res.Reason = VerdictKept, foreign+" was not written by "+opts.Product+" setup-service"
		return res, nil
	}
	if want == text && len(pending) == 0 {
		res.Verdict = VerdictUnchanged
		return res, nil
	}
	res.Verdict, res.Changed = VerdictRefreshed, true
	if ro.PrintOnly {
		return res, nil
	}
	if want != text {
		mode := scriptMode(rec.Opts)
		if err := os.WriteFile(def.Backup, current, mode); err != nil {
			return res, fmt.Errorf("write %s: %w", def.Backup, err)
		}
		res.BackupPath = def.Backup
		if err := writeUnitAtomic(def.Dir, def.Path, []byte(want), mode); err != nil {
			return res, err
		}
	}
	if err := writeScriptFiles(pending); err != nil {
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
