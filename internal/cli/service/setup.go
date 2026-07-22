// Package service installs and manages the mcremote systemd --user unit.
package service

import (
	"bytes"
	"context"
	_ "embed" // unit template via //go:embed
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"
	"time"
)

//go:embed mcremote.user.service.tmpl
var unitTemplate string

// cmdTimeout bounds every systemctl/loginctl invocation: a hung user bus must
// not wedge the CLI forever.
const cmdTimeout = 30 * time.Second

// Options configure setup-service.
type Options struct {
	// UnitName without .service suffix (default "mcremote").
	UnitName string
	// Binary is the absolute path written into ExecStart.
	// Default: ~/.local/bin/mcremote if present, else this process's executable.
	// setup-service never copies or overwrites the binary — use `make install`.
	Binary string
	// ConfigPath optional --config for serve.
	ConfigPath string
	// DataDir optional --data-dir for serve.
	DataDir string
	// ListenHost / ListenPort optional serve overrides. Empty/zero means "not
	// baked into the unit": serve then follows config.yaml, which is what you
	// want — a baked flag would silently override later config edits.
	ListenHost string
	ListenPort int
	// LogLevel / LogFormat optional serve overrides.
	LogLevel  string
	LogFormat string
	// WorkingDirectory for the unit (default $HOME).
	WorkingDirectory string
	// ExtraEnviron is raw KEY=VALUE lines (no "Environment=" prefix).
	ExtraEnviron []string
	// PrintOnly writes the unit to stdout and does not install.
	PrintOnly bool
	// Force overwrite an existing unit file whose content differs.
	Force bool
	// NoEnable skips systemctl enable.
	NoEnable bool
	// NoStart skips systemctl start.
	NoStart bool
	// NoLinger skips loginctl enable-linger.
	NoLinger bool
	// UnitDir overrides ~/.config/systemd/user (tests).
	UnitDir string
}

// Result is what Setup wrote/did.
type Result struct {
	UnitPath      string
	Binary        string
	UnitName      string
	Enabled       bool
	Started       bool
	LingerEnabled bool
	UnitBody      string
	// AlreadyExisted: the unit file was already present. If Unchanged is also
	// true the existing content was byte-identical and nothing was rewritten.
	AlreadyExisted bool
	Unchanged      bool
	// Removed is set by Remove.
	Removed bool
}

type templateData struct {
	UnitName         string
	Binary           string
	ConfigPath       string
	DataDir          string
	ListenHost       string
	ListenPort       string
	LogLevel         string
	LogFormat        string
	WorkingDirectory string
	Home             string
	User             string
	Path             string
	XDGConfigHome    string
	XDGDataHome      string
	XDGCacheHome     string
	XDGRuntimeDir    string
	ExtraEnviron     []string
	DocsHint         string
}

// unitNameRe follows systemd unit-name rules (letters, digits, and :-_.\@);
// anything looser gets written to disk and then rejected by systemctl,
// leaving partial state behind.
var unitNameRe = regexp.MustCompile(`^[A-Za-z0-9:_.@\\-]+$`)

// envKeyRe is a POSIX environment variable name.
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// RenderUnit returns the systemd unit text for opts (no side effects).
// Binary path is not required to exist on disk (print/preview only).
func RenderUnit(opts Options) (string, error) {
	opts.PrintOnly = true
	opts, err := normalize(opts)
	if err != nil {
		return "", err
	}
	return render(opts)
}

// Setup installs the user unit only (never the binary), then enables and
// starts it. Re-running against a byte-identical existing unit is a no-op for
// the file (the enable/start steps still run so state converges).
func Setup(opts Options) (Result, error) {
	opts, err := normalize(opts)
	if err != nil {
		return Result{}, err
	}

	body, err := render(opts)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Binary:   opts.Binary,
		UnitName: opts.UnitName,
		UnitBody: body,
	}

	if opts.PrintOnly {
		return res, nil
	}

	if err := preflight(); err != nil {
		return res, err
	}

	unitDir := opts.UnitDir
	if unitDir == "" {
		unitDir = filepath.Join(xdgConfigHome(), "systemd", "user")
	}
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return res, fmt.Errorf("create unit dir: %w", err)
	}
	unitPath := filepath.Join(unitDir, opts.UnitName+".service")
	res.UnitPath = unitPath

	// Environment= lines may carry secrets; keep the unit private then.
	mode := os.FileMode(0o644)
	if len(opts.ExtraEnviron) > 0 {
		mode = 0o600
	}

	if existing, err := os.ReadFile(unitPath); err == nil {
		res.AlreadyExisted = true
		if bytes.Equal(existing, []byte(body)) {
			// Identical: no rewrite, but still converge enable/start below.
			res.Unchanged = true
		} else if !opts.Force {
			return res, fmt.Errorf("unit already exists at %s with different content (pass --force to overwrite)", unitPath)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("stat unit: %w", err)
	}

	if !res.Unchanged {
		if err := writeUnitAtomic(unitDir, unitPath, []byte(body), mode); err != nil {
			return res, err
		}
	}

	// From here the file is on disk: wrap failures with how to finish by hand
	// so a partial install is never a mystery.
	manual := fmt.Sprintf("finish manually with: systemctl --user daemon-reload && systemctl --user enable --now %s.service", opts.UnitName)
	if err := runSystemctl("--user", "daemon-reload"); err != nil {
		return res, fmt.Errorf("unit installed at %s, but systemctl daemon-reload failed: %w (%s)", unitPath, err, manual)
	}

	if !opts.NoEnable {
		if err := runSystemctl("--user", "enable", opts.UnitName+".service"); err != nil {
			return res, fmt.Errorf("unit installed at %s, but systemctl enable failed: %w (%s)", unitPath, err, manual)
		}
		res.Enabled = true
	}

	if !opts.NoStart {
		// restart also starts an inactive unit, so no separate start fallback.
		if err := runSystemctl("--user", "restart", opts.UnitName+".service"); err != nil {
			return res, fmt.Errorf("unit installed at %s, but systemctl restart failed: %w (%s)", unitPath, err, manual)
		}
		res.Started = true
	}

	if !opts.NoLinger {
		if u, err := user.Current(); err == nil {
			// Non-fatal: unit works while logged in.
			res.LingerEnabled = runCmd("loginctl", "enable-linger", u.Username) == nil
		}
	}

	return res, nil
}

// Remove stops, disables, and deletes the unit (the inverse of Setup). Stop
// and disable failures are tolerated (unit may not be running or enabled);
// file removal and daemon-reload are not.
func Remove(opts Options) (Result, error) {
	if opts.UnitName == "" {
		opts.UnitName = "mcremote"
	}
	if !unitNameRe.MatchString(opts.UnitName) || strings.HasSuffix(opts.UnitName, ".service") {
		return Result{}, fmt.Errorf("unit name must be a bare systemd name (got %q)", opts.UnitName)
	}
	if err := preflight(); err != nil {
		return Result{}, err
	}

	unitDir := opts.UnitDir
	if unitDir == "" {
		unitDir = filepath.Join(xdgConfigHome(), "systemd", "user")
	}
	unitPath := filepath.Join(unitDir, opts.UnitName+".service")
	res := Result{UnitName: opts.UnitName, UnitPath: unitPath}

	svc := opts.UnitName + ".service"
	_ = runSystemctl("--user", "stop", svc)
	// disable removes the [Install] symlinks (default.target.wants/…).
	_ = runSystemctl("--user", "disable", svc)

	if err := os.Remove(unitPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove unit: %w", err)
	}
	// Belt and braces: disable can miss a hand-made symlink.
	wants := filepath.Join(unitDir, "default.target.wants", svc)
	if err := os.Remove(wants); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove enable symlink: %w", err)
	}
	if err := runSystemctl("--user", "daemon-reload"); err != nil {
		return res, fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	res.Removed = true
	return res, nil
}

// preflight fails fast — before any file is written — on hosts where the
// install cannot work, with an actionable message.
func preflight() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("setup-service manages a systemd user unit and requires Linux (running on %s); use --print-only to preview the unit", runtime.GOOS)
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found in PATH — this host does not appear to run systemd; use --print-only to preview the unit")
	}
	// Probe the user bus. is-system-running exits non-zero for "degraded",
	// which is fine — only a bus connection failure means we cannot proceed.
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "is-system-running")
	cmd.Env = withUserRuntimeEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil && strings.Contains(strings.ToLower(string(out)), "connect to bus") {
		return fmt.Errorf("cannot reach the systemd user bus (%s): no user session is running — run `sudo loginctl enable-linger $USER`, re-log-in, then retry", strings.TrimSpace(string(out)))
	}
	return nil
}

func normalize(opts Options) (Options, error) {
	if opts.UnitName == "" {
		opts.UnitName = "mcremote"
	}
	if !unitNameRe.MatchString(opts.UnitName) || strings.HasSuffix(opts.UnitName, ".service") {
		return opts, fmt.Errorf("unit name must be a bare systemd name (letters, digits, :-_.@; got %q)", opts.UnitName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return opts, fmt.Errorf("home dir: %w", err)
	}

	if opts.Binary == "" {
		// Prefer a stable make-install path so ExecStart does not point at a
		// build-tree binary that may be replaced or removed.
		userBin := filepath.Join(home, ".local", "bin", "mcremote")
		if isExecutableFile(userBin) {
			opts.Binary = userBin
		} else {
			exe, err := os.Executable()
			if err != nil {
				return opts, fmt.Errorf("resolve executable: %w", err)
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
				exe = resolved
			}
			opts.Binary = exe
		}
	}
	if !filepath.IsAbs(opts.Binary) {
		abs, err := filepath.Abs(opts.Binary)
		if err != nil {
			return opts, err
		}
		opts.Binary = abs
	}
	// `go run` compiles into a temp dir that vanishes after the run: the unit
	// would install fine and then 203/EXEC on the next start.
	if !opts.PrintOnly && isEphemeralBuildPath(opts.Binary) {
		return opts, fmt.Errorf(
			"binary %s is a temporary go-run/go-build artifact and will disappear.\nInstall a real binary first with: make install\nOr pass --binary /path/to/mcremote",
			opts.Binary,
		)
	}
	// Require a real binary only when installing the unit (not --print-only / RenderUnit).
	if !opts.PrintOnly && !isExecutableFile(opts.Binary) {
		return opts, fmt.Errorf(
			"binary not found or not executable: %s\nInstall first with: make install\nOr pass --binary /path/to/mcremote",
			opts.Binary,
		)
	}

	for _, kv := range opts.ExtraEnviron {
		if !envKeyRe.MatchString(kv) {
			return opts, fmt.Errorf("--env %q is not KEY=VALUE with a valid variable name", kv)
		}
		if strings.ContainsAny(kv, "\n\r\x00") {
			return opts, fmt.Errorf("--env %q contains control characters", kv)
		}
	}

	// Every free-text field below is rendered verbatim into a systemd unit
	// assignment. systemdQuote doubles %-specifiers and escapes quotes, but a
	// newline (or NUL) is not escapable inside a unit line: it would terminate
	// the assignment and let the remainder inject arbitrary directives
	// (e.g. ExecStartPre=). Reject control characters at the boundary.
	for _, f := range []struct{ name, val string }{
		{"--binary", opts.Binary},
		{"--service-config", opts.ConfigPath},
		{"--data-dir", opts.DataDir},
		{"--listen-host", opts.ListenHost},
		{"--log-level", opts.LogLevel},
		{"--log-format", opts.LogFormat},
		{"--working-directory", opts.WorkingDirectory},
	} {
		if strings.ContainsAny(f.val, "\n\r\x00") {
			return opts, fmt.Errorf("%s %q contains control characters", f.name, f.val)
		}
	}

	if opts.WorkingDirectory == "" {
		opts.WorkingDirectory = home
	}
	if opts.ConfigPath != "" && !filepath.IsAbs(opts.ConfigPath) {
		abs, err := filepath.Abs(opts.ConfigPath)
		if err != nil {
			return opts, err
		}
		opts.ConfigPath = abs
	}
	if opts.DataDir != "" && !filepath.IsAbs(opts.DataDir) {
		abs, err := filepath.Abs(opts.DataDir)
		if err != nil {
			return opts, err
		}
		opts.DataDir = abs
	}
	return opts, nil
}

func isEphemeralBuildPath(path string) bool {
	tmp := os.TempDir()
	if tmp != "" && strings.HasPrefix(path, filepath.Clean(tmp)+string(os.PathSeparator)) {
		return strings.Contains(path, "go-build") || strings.Contains(path, string(os.PathSeparator)+"exe"+string(os.PathSeparator))
	}
	return strings.Contains(path, string(os.PathSeparator)+"go-build")
}

func render(opts Options) (string, error) {
	home, _ := os.UserHomeDir()
	u, _ := user.Current()
	username := ""
	if u != nil {
		username = u.Username
	}

	pathEnv := os.Getenv("PATH")
	// Ensure installed bin + common tool dirs are present for the service.
	extras := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".grok", "bin"),
		filepath.Join(home, ".opencode", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".local", "go", "bin"),
		filepath.Join(home, ".local", "flutter", "bin"),
	}
	for _, e := range extras {
		if e != "" && !strings.Contains(":"+pathEnv+":", ":"+e+":") {
			pathEnv = e + ":" + pathEnv
		}
	}

	portStr := ""
	if opts.ListenPort > 0 {
		portStr = fmt.Sprintf("%d", opts.ListenPort)
	}

	env := make([]string, 0, len(opts.ExtraEnviron))
	for _, kv := range opts.ExtraEnviron {
		k, v, _ := strings.Cut(kv, "=")
		env = append(env, k+"="+systemdQuote(v))
	}

	data := templateData{
		UnitName:         opts.UnitName,
		Binary:           systemdQuote(opts.Binary),
		ConfigPath:       systemdQuote(opts.ConfigPath),
		DataDir:          systemdQuote(opts.DataDir),
		ListenHost:       systemdQuote(opts.ListenHost),
		ListenPort:       portStr,
		LogLevel:         systemdQuote(opts.LogLevel),
		LogFormat:        systemdQuote(opts.LogFormat),
		WorkingDirectory: systemdQuote(opts.WorkingDirectory),
		Home:             systemdQuote(home),
		User:             systemdQuote(username),
		Path:             systemdQuote(pathEnv),
		XDGConfigHome:    systemdQuote(xdgConfigHome()),
		XDGDataHome:      systemdQuote(xdgDataHome()),
		XDGCacheHome:     systemdQuote(xdgCacheHome()),
		XDGRuntimeDir:    systemdQuote(os.Getenv("XDG_RUNTIME_DIR")),
		ExtraEnviron:     env,
		DocsHint:         systemdQuote(filepath.Join(home, ".config", "mcremote")),
	}

	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse unit template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render unit: %w", err)
	}
	return buf.String(), nil
}

// systemdQuote escapes a value for a systemd unit assignment: `%` doubled
// (systemd expands specifiers like %h everywhere), and backslash/quote
// escaped inside double quotes when the value needs quoting at all.
func systemdQuote(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0o111 != 0
}

// writeUnitAtomic writes body to unitPath via a unique temp file + rename, so
// concurrent invocations cannot interleave partial writes.
func writeUnitAtomic(unitDir, unitPath string, body []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(unitDir, ".mcremote-unit-*")
	if err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	tmp := f.Name()
	cleanup := func() {
		f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(body); err != nil {
		cleanup()
		return fmt.Errorf("write unit: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod unit: %w", err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync unit: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write unit: %w", err)
	}
	if err := os.Rename(tmp, unitPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install unit: %w", err)
	}
	return nil
}

func runSystemctl(args ...string) error {
	return runCmd("systemctl", args...)
}

// runCmd executes a management command with a timeout, streaming output to the
// terminal while capturing stderr into the returned error so failures are
// diagnosable from the error alone.
func runCmd(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	cmd.Env = withUserRuntimeEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func withUserRuntimeEnv(base []string) []string {
	has := false
	for _, e := range base {
		if strings.HasPrefix(e, "XDG_RUNTIME_DIR=") && len(e) > len("XDG_RUNTIME_DIR=") {
			has = true
			break
		}
	}
	if has {
		return base
	}
	if u, err := user.Current(); err == nil {
		rd := filepath.Join("/run/user", u.Uid)
		if st, err := os.Stat(rd); err == nil && st.IsDir() {
			return append(base, "XDG_RUNTIME_DIR="+rd)
		}
	}
	return base
}

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func xdgCacheHome() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
}
