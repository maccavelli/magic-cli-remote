package service

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Refresh verdicts. Every one of them is a successful outcome: a refresh that
// declines to rewrite is reporting, not failing (MADR 0100 D3).
const (
	// VerdictNone means no service definition is installed for the product.
	VerdictNone = "none"
	// VerdictUnchanged means the installed definition already matches this
	// binary's template.
	VerdictUnchanged = "unchanged"
	// VerdictRefreshed means the definition was rewritten from this binary's
	// template, preserving the options baked into the old one.
	VerdictRefreshed = "refreshed"
	// VerdictKept means the definition was left alone because this binary did
	// not write it, or could not reproduce it.
	VerdictKept = "kept"
)

// RefreshOptions tunes a refresh. The zero value is the update-path behaviour.
type RefreshOptions struct {
	// PrintOnly renders and reports without writing or reloading.
	PrintOnly bool
	// NoReload skips systemctl daemon-reload (tests).
	NoReload bool
}

// RefreshResult is the outcome of a refresh. The JSON field names are a
// cross-version contract: `update` in release N parses what `setup-service
// --refresh --json` prints in release N+1. Add fields; never rename one.
type RefreshResult struct {
	Verdict    string   `json:"verdict"`
	Path       string   `json:"path"`
	BackupPath string   `json:"backup,omitempty"`
	Changed    bool     `json:"changed"`
	Reloaded   bool     `json:"reloaded"`
	Reason     string   `json:"reason,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	// Body is the re-rendered definition, for --print-only. Never serialized:
	// the JSON form is a status report, not a transport for the unit itself.
	Body string `json:"-"`
}

// managedEnvKeys are the environment keys the templates emit themselves. Every
// other Environment= entry came from --env and is carried over as-is.
var managedEnvKeys = map[string]bool{
	"HOME":            true,
	"USER":            true,
	"LOGNAME":         true,
	"PATH":            true,
	"XDG_CONFIG_HOME": true,
	"XDG_DATA_HOME":   true,
	"XDG_STATE_HOME":  true,
	"XDG_CACHE_HOME":  true,
	"XDG_RUNTIME_DIR": true,
}

// handEditedDirectives are directives setup-service never writes. One of them
// in an installed unit means an operator put it there, and a rewrite would
// silently drop it — so the refresh declines instead.
var handEditedDirectives = map[string]bool{
	"ExecStartPre":    true,
	"ExecStartPost":   true,
	"ExecStop":        true,
	"ExecStopPost":    true,
	"ExecReload":      true,
	"ExecCondition":   true,
	"EnvironmentFile": true,
}

// recovery is what recoverOptions rebuilt from an installed definition.
type recovery struct {
	Opts     Options
	Env      renderEnv
	Warnings []string
}

// RefreshUnit re-renders the installed service definition from this binary's
// own template, preserving the options baked into the file it replaces, and
// rewrites it only when this package wrote it and can reproduce it.
//
// It exists because the process performing an update is the OLD binary and
// carries the OLD template: only the newly installed binary can render the
// definition a release ships (MADR 0100).
func RefreshUnit(opts Options, ro RefreshOptions) (RefreshResult, error) {
	res := RefreshResult{Verdict: VerdictNone}
	if opts.Product == "" {
		opts.Product = "mcremote"
	}
	switch opts.Product {
	case "mcremote", "mcrelay":
	default:
		return res, fmt.Errorf("product must be mcremote or mcrelay (got %q)", opts.Product)
	}
	if opts.UnitName == "" {
		opts.UnitName = opts.Product
	}
	if !unitNameRe.MatchString(opts.UnitName) || strings.HasSuffix(opts.UnitName, ".service") {
		return res, fmt.Errorf("unit name must be a bare systemd name (got %q)", opts.UnitName)
	}

	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "linux":
		return refreshSystemd(opts, ro)
	case "darwin":
		return refreshLaunchd(opts, ro)
	default:
		return res, fmt.Errorf("setup-service --refresh is only supported on Linux and macOS (running on %s)", osName)
	}
}

// RestoreUnitBackup puts a .prev definition back and reloads the manager. It
// renders nothing, so the old binary can call it while rolling an update back.
func RestoreUnitBackup(path, backup string) error {
	if path == "" || backup == "" {
		return fmt.Errorf("path and backup are required")
	}
	if err := os.Rename(backup, path); err != nil {
		return fmt.Errorf("restore %s from %s: %w", path, backup, err)
	}
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	if osName == "linux" {
		if err := runSystemctl("--user", "daemon-reload"); err != nil {
			return fmt.Errorf("restored %s, but systemctl daemon-reload failed: %w", path, err)
		}
	}
	return nil
}

func refreshSystemd(opts Options, ro RefreshOptions) (RefreshResult, error) {
	unitDir := opts.UnitDir
	if unitDir == "" {
		unitDir = filepath.Join(xdgConfigHome(), "systemd", "user")
	}
	unitPath := filepath.Join(unitDir, opts.UnitName+".service")
	res := RefreshResult{Verdict: VerdictNone, Path: unitPath}

	current, err := os.ReadFile(unitPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil // nothing installed: nothing to refresh, nothing to reload
		}
		return res, fmt.Errorf("read unit: %w", err)
	}
	text := string(current)

	// A definition we will not rewrite still deserves a reload: it may have been
	// changed on disk by something else, and a plain `systemctl start` would run
	// the manager's cached copy (MADR 0100 F2).
	softReload := func() {
		if ro.NoReload || ro.PrintOnly {
			return
		}
		if rerr := runSystemctl("--user", "daemon-reload"); rerr != nil {
			res.Warnings = append(res.Warnings, "systemctl --user daemon-reload failed: "+rerr.Error())
			return
		}
		res.Reloaded = true
	}

	if ok, why := unitIsManaged(opts.Product, text); !ok {
		res.Verdict, res.Reason = VerdictKept, why
		softReload()
		return res, nil
	}
	rec, why, ok := recoverOptions(opts.Product, opts.UnitName, text)
	if !ok {
		res.Verdict, res.Reason = VerdictKept, why
		softReload()
		return res, nil
	}
	res.Warnings = append(res.Warnings, rec.Warnings...)
	res.Warnings = append(res.Warnings, pathDriftWarning(opts.Product, rec.Env)...)
	res.Warnings = append(res.Warnings, binaryWarning(rec.Opts.Binary)...)

	want, err := renderWith(rec.Opts, &rec.Env)
	if err != nil {
		return res, err
	}
	res.Body = want
	if want == text {
		res.Verdict = VerdictUnchanged
		softReload()
		return res, nil
	}

	res.Verdict, res.Changed = VerdictRefreshed, true
	if ro.PrintOnly {
		return res, nil
	}

	mode := refreshedMode(unitPath, rec.Opts)
	backup := unitPath + ".prev"
	if err := os.WriteFile(backup, current, mode); err != nil {
		return res, fmt.Errorf("write %s: %w", backup, err)
	}
	res.BackupPath = backup
	if err := writeUnitAtomic(unitDir, unitPath, []byte(want), mode); err != nil {
		return res, err
	}
	if !ro.NoReload {
		if rerr := runSystemctl("--user", "daemon-reload"); rerr != nil {
			return res, fmt.Errorf("unit refreshed at %s, but systemctl daemon-reload failed: %w (finish with: systemctl --user daemon-reload)", unitPath, rerr)
		}
		res.Reloaded = true
	}
	return res, nil
}

func refreshLaunchd(opts Options, ro RefreshOptions) (RefreshResult, error) {
	label, err := LaunchdLabel(opts.Product, opts.UnitName)
	if err != nil {
		return RefreshResult{Verdict: VerdictNone}, err
	}
	plistDir := opts.UnitDir
	if plistDir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return RefreshResult{Verdict: VerdictNone}, fmt.Errorf("home dir: %w", herr)
		}
		plistDir = filepath.Join(home, "Library", "LaunchAgents")
	}
	plistPath := filepath.Join(plistDir, label+".plist")
	res := RefreshResult{Verdict: VerdictNone, Path: plistPath}

	current, err := os.ReadFile(plistPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, fmt.Errorf("read plist: %w", err)
	}
	text := string(current)

	rec, why, ok := recoverPlistOptions(opts.Product, opts.UnitName, label, text)
	if !ok {
		res.Verdict, res.Reason = VerdictKept, why
		return res, nil
	}
	res.Warnings = append(res.Warnings, rec.Warnings...)
	res.Warnings = append(res.Warnings, pathDriftWarning(opts.Product, rec.Env)...)
	res.Warnings = append(res.Warnings, binaryWarning(rec.Opts.Binary)...)

	want, err := renderPlistWith(rec.Opts, &rec.Env)
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

	mode := refreshedMode(plistPath, rec.Opts)
	backup := plistPath + ".prev"
	if err := os.WriteFile(backup, current, mode); err != nil {
		return res, fmt.Errorf("write %s: %w", backup, err)
	}
	res.BackupPath = backup
	if err := writeUnitAtomic(plistDir, plistPath, []byte(want), mode); err != nil {
		return res, err
	}
	// launchd reads the plist on bootstrap, so there is no reload step; a
	// malformed file would only surface at the next load (MADR 0058).
	if err := lintPlist(plistPath); err != nil {
		return res, err
	}
	return res, nil
}

// refreshedMode picks the mode for a rewritten definition: the Setup rule
// (0600 when the definition carries extra environment, else 0644), never
// widening what is already on disk.
func refreshedMode(path string, opts Options) os.FileMode {
	mode := os.FileMode(0o644)
	if len(opts.ExtraEnviron) > 0 {
		mode = 0o600
	}
	if fi, err := os.Stat(path); err == nil {
		if perm := fi.Mode().Perm(); perm&^mode == 0 {
			mode = perm
		}
	}
	return mode
}

// unitIsManaged reports whether body is a unit this package wrote and can
// reproduce. Anything else is reported and left alone.
func unitIsManaged(product, body string) (bool, string) {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != managedMarker(product) {
		return false, "not written by " + product + " setup-service (no managed-by header)"
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			switch line {
			case "[Unit]", "[Service]", "[Install]":
				continue
			default:
				return false, "carries an unexpected section " + line
			}
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if handEditedDirectives[strings.TrimSpace(key)] {
			return false, "carries " + strings.TrimSpace(key) + "=, which setup-service never writes"
		}
	}
	return true, ""
}

// managedMarker is the first line every rendered unit carries.
func managedMarker(product string) string {
	return "# Managed by `" + product + " setup-service` / `" + product + " --setup-service`."
}

// recoverOptions rebuilds the Options and the pinned environment that rendered
// body, so a refresh can re-render with the current template without losing
// what the operator baked in at setup time.
func recoverOptions(product, unitName, body string) (recovery, string, bool) {
	rec := recovery{Opts: Options{Product: product, UnitName: unitName}}
	seen := map[string]bool{}
	execs := 0

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Description":
			rec.Opts.Description = val
		case "WorkingDirectory":
			rec.Opts.WorkingDirectory = unquoteSystemd(val)
		case "ExecStart":
			execs++
			if execs > 1 {
				return rec, "has more than one ExecStart=", false
			}
			if why, ok := applyExecStart(&rec.Opts, val); !ok {
				return rec, why, false
			}
		case "Environment":
			k, v, ok := strings.Cut(val, "=")
			if !ok {
				return rec, "has a malformed Environment= line", false
			}
			uv := unquoteSystemd(v)
			if managedEnvKeys[k] {
				seen[k] = true
				assignManagedEnv(&rec.Env, k, uv)
				continue
			}
			rec.Opts.ExtraEnviron = append(rec.Opts.ExtraEnviron, k+"="+uv)
		}
	}
	if execs == 0 {
		return rec, "has no ExecStart=", false
	}
	rec.Warnings = fillMissingEnv(product, &rec.Env, seen)
	return rec, "", true
}

// applyExecStart parses the rendered `<binary> serve [flags]` command line.
func applyExecStart(opts *Options, val string) (string, bool) {
	toks, err := splitExecStart(val)
	if err != nil {
		return err.Error(), false
	}
	if len(toks) < 2 {
		return "has an ExecStart= too short to be ours", false
	}
	// systemd exec prefixes (-, @, :, !, +) change the semantics of the line and
	// the template never emits one; re-rendering would silently drop it.
	if strings.ContainsAny(toks[0][:1], "-@:!+") {
		return "has an ExecStart= prefix (" + toks[0][:1] + ") that setup-service never writes", false
	}
	opts.Binary = toks[0]
	if toks[1] != "serve" {
		return fmt.Sprintf("has an ExecStart= that does not run `serve` (found %q)", toks[1]), false
	}
	for i := 2; i < len(toks); i++ {
		flag := toks[i]
		if i+1 >= len(toks) {
			return fmt.Sprintf("has an ExecStart= flag %s with no value", flag), false
		}
		v := toks[i+1]
		i++
		switch flag {
		case "--config":
			opts.ConfigPath = v
		case "--data-dir":
			opts.DataDir = v
		case "--listen-host":
			opts.ListenHost = v
		case "--listen-port":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Sprintf("has an ExecStart= --listen-port %q that is not a port", v), false
			}
			opts.ListenPort = n
		case "--log-level":
			opts.LogLevel = v
		case "--log-format":
			opts.LogFormat = v
		default:
			return fmt.Sprintf("has an unrecognised ExecStart= argument %q", flag), false
		}
	}
	return "", true
}

// assignManagedEnv pins one template-emitted environment value.
func assignManagedEnv(env *renderEnv, key, val string) {
	switch key {
	case "HOME":
		env.Home = val
	case "USER":
		env.User = val
	case "LOGNAME":
		if env.User == "" {
			env.User = val
		}
	case "PATH":
		env.Path = val
	case "XDG_CONFIG_HOME":
		env.XDGConfigHome = val
	case "XDG_DATA_HOME":
		env.XDGDataHome = val
	case "XDG_STATE_HOME":
		env.XDGStateHome = val
	case "XDG_CACHE_HOME":
		env.XDGCacheHome = val
	case "XDG_RUNTIME_DIR":
		env.XDGRuntimeDir = val
	}
}

// fillMissingEnv falls back to computed values for keys an older template did
// not emit, and names each one. XDG_RUNTIME_DIR is exempt: the template omits
// it whenever the value is empty, so its absence is normal.
func fillMissingEnv(product string, env *renderEnv, seen map[string]bool) []string {
	cur := currentRenderEnv(product)
	var missing []string
	for _, k := range []string{"HOME", "USER", "PATH", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		if seen[k] || (k == "USER" && env.User != "") {
			continue
		}
		assignManagedEnv(env, k, envValue(cur, k))
		missing = append(missing, k)
	}
	if len(missing) == 0 {
		return nil
	}
	return []string{"the installed definition had no " + strings.Join(missing, ", ") +
		"; this host's current value was used"}
}

func envValue(env renderEnv, key string) string {
	switch key {
	case "HOME":
		return env.Home
	case "USER", "LOGNAME":
		return env.User
	case "PATH":
		return env.Path
	case "XDG_CONFIG_HOME":
		return env.XDGConfigHome
	case "XDG_DATA_HOME":
		return env.XDGDataHome
	case "XDG_STATE_HOME":
		return env.XDGStateHome
	case "XDG_CACHE_HOME":
		return env.XDGCacheHome
	case "XDG_RUNTIME_DIR":
		return env.XDGRuntimeDir
	}
	return ""
}

// pathDriftWarning reports prefixes this release wants on the service PATH that
// the installed definition does not have. The pinned value is kept either way:
// re-deriving PATH during an update would rewrite it to the updating process's
// environment (MADR 0100 F4).
func pathDriftWarning(product string, env renderEnv) []string {
	home := env.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	have := map[string]bool{}
	for _, p := range filepath.SplitList(env.Path) {
		have[p] = true
	}
	var missing []string
	for _, p := range servicePathExtras(home, product) {
		if p != "" && !have[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []string{"PATH entries this release adds are not applied (" + strings.Join(missing, ", ") +
		"); re-derive with: " + product + " setup-service --force"}
}

// binaryWarning flags a definition that runs a different binary than the one
// this process is — the case where an update swaps a binary the service does
// not run.
func binaryWarning(unitBinary string) []string {
	if unitBinary == "" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
		exe = resolved
	}
	if exe == unitBinary {
		return nil
	}
	return []string{"the definition runs " + unitBinary + ", not this binary (" + exe + ")"}
}

// splitExecStart tokenises a systemd command line: double quotes group, and a
// backslash escapes the next character. The inverse of systemdQuote's output.
func splitExecStart(s string) ([]string, error) {
	var (
		toks    []string
		cur     strings.Builder
		inQuote bool
		started bool
		esc     bool
	)
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
			started = true
		case r == '"':
			inQuote = !inQuote
			started = true
		case !inQuote && (r == ' ' || r == '\t'):
			if started {
				toks = append(toks, pctUnescape(cur.String()))
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if esc || inQuote {
		return nil, errors.New("has an ExecStart= with an unterminated quote or escape")
	}
	if started {
		toks = append(toks, pctUnescape(cur.String()))
	}
	return toks, nil
}

// unquoteSystemd inverts systemdQuote for a single assignment value.
func unquoteSystemd(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == '"':
			// wrapping quote
		default:
			b.WriteRune(r)
		}
	}
	return pctUnescape(b.String())
}

// pctUnescape undoes the %-doubling systemdQuote applies to every value.
func pctUnescape(s string) string {
	return strings.ReplaceAll(s, "%%", "%")
}

// recoverPlistOptions is the launchd twin of recoverOptions. A plist has no
// comment line to carry the managed-by marker, so provenance is structural:
// our Label, and ProgramArguments that start with `<binary> serve`.
func recoverPlistOptions(product, unitName, label, body string) (recovery, string, bool) {
	rec := recovery{Opts: Options{Product: product, UnitName: unitName}}
	top, err := parsePlist(body)
	if err != nil {
		return rec, "is not a plist this binary can read (" + err.Error() + ")", false
	}
	if got := top["Label"].str; got != label {
		return rec, "has Label " + got + ", not " + label, false
	}
	args := top["ProgramArguments"]
	if args.kind != "array" {
		return rec, "has no ProgramArguments array", false
	}
	argv := make([]string, 0, len(args.array))
	for _, a := range args.array {
		argv = append(argv, a.str)
	}
	if why, ok := applyProgramArguments(&rec.Opts, argv); !ok {
		return rec, why, false
	}
	rec.Opts.WorkingDirectory = top["WorkingDirectory"].str

	seen := map[string]bool{}
	envNode := top["EnvironmentVariables"]
	if envNode.kind == "dict" {
		for _, k := range envNode.order {
			v := envNode.dict[k].str
			if managedEnvKeys[k] {
				seen[k] = true
				assignManagedEnv(&rec.Env, k, v)
				continue
			}
			rec.Opts.ExtraEnviron = append(rec.Opts.ExtraEnviron, k+"="+v)
		}
	}
	rec.Warnings = fillMissingEnv(product, &rec.Env, seen)
	return rec, "", true
}

// applyProgramArguments parses the plist form of the same command line.
func applyProgramArguments(opts *Options, argv []string) (string, bool) {
	if len(argv) < 2 {
		return "has ProgramArguments too short to be ours", false
	}
	opts.Binary = argv[0]
	if argv[1] != "serve" {
		return fmt.Sprintf("has ProgramArguments that do not run `serve` (found %q)", argv[1]), false
	}
	for i := 2; i < len(argv); i++ {
		flag := argv[i]
		if i+1 >= len(argv) {
			return fmt.Sprintf("has a ProgramArguments flag %s with no value", flag), false
		}
		v := argv[i+1]
		i++
		switch flag {
		case "--config":
			opts.ConfigPath = v
		case "--data-dir":
			opts.DataDir = v
		case "--listen-host":
			opts.ListenHost = v
		case "--listen-port":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Sprintf("has a ProgramArguments --listen-port %q that is not a port", v), false
			}
			opts.ListenPort = n
		case "--log-level":
			opts.LogLevel = v
		case "--log-format":
			opts.LogFormat = v
		default:
			return fmt.Sprintf("has an unrecognised ProgramArguments entry %q", flag), false
		}
	}
	return "", true
}

// plistNode is the subset of plist values renderPlist emits.
type plistNode struct {
	kind  string // string | array | dict | other
	str   string
	array []plistNode
	dict  map[string]plistNode
	order []string
}

// parsePlist reads the top-level dict of a property list we wrote.
func parsePlist(body string) (map[string]plistNode, error) {
	dec := xml.NewDecoder(strings.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("no top-level dict: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dict" {
			node, err := readPlistDict(dec)
			if err != nil {
				return nil, err
			}
			return node.dict, nil
		}
	}
}

func readPlistDict(dec *xml.Decoder) (plistNode, error) {
	out := plistNode{kind: "dict", dict: map[string]plistNode{}}
	key := ""
	haveKey := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return out, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "key" {
				s, err := readPlistText(dec, t)
				if err != nil {
					return out, err
				}
				key, haveKey = s, true
				continue
			}
			node, err := readPlistNode(dec, t)
			if err != nil {
				return out, err
			}
			if haveKey {
				if _, dup := out.dict[key]; !dup {
					out.order = append(out.order, key)
				}
				out.dict[key] = node
				haveKey = false
			}
		case xml.EndElement:
			if t.Name.Local == "dict" {
				return out, nil
			}
		}
	}
}

func readPlistNode(dec *xml.Decoder, start xml.StartElement) (plistNode, error) {
	switch start.Name.Local {
	case "string":
		s, err := readPlistText(dec, start)
		return plistNode{kind: "string", str: s}, err
	case "array":
		out := plistNode{kind: "array"}
		for {
			tok, err := dec.Token()
			if err != nil {
				return out, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				n, err := readPlistNode(dec, t)
				if err != nil {
					return out, err
				}
				out.array = append(out.array, n)
			case xml.EndElement:
				if t.Name.Local == "array" {
					return out, nil
				}
			}
		}
	case "dict":
		return readPlistDict(dec)
	default:
		// integer / true / false / data: present but not needed for recovery.
		if err := dec.Skip(); err != nil {
			return plistNode{kind: "other"}, err
		}
		return plistNode{kind: "other"}, nil
	}
}

func readPlistText(dec *xml.Decoder, start xml.StartElement) (string, error) {
	var s string
	if err := dec.DecodeElement(&s, &start); err != nil {
		return "", err
	}
	return s, nil
}
