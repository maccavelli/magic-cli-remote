// Package service installs and manages the mcremote systemd --user unit.
package service

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed mcremote.user.service.tmpl
var unitTemplate string

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
	// ListenHost / ListenPort optional serve overrides.
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
	// Force overwrite an existing unit file.
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
	UnitPath       string
	Binary         string
	UnitName       string
	Enabled        bool
	Started        bool
	LingerEnabled  bool
	UnitBody       string
	AlreadyExisted bool
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

// Setup installs the user unit only (never the binary), then enables and starts it.
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

	unitDir := opts.UnitDir
	if unitDir == "" {
		cfgHome := xdgConfigHome()
		unitDir = filepath.Join(cfgHome, "systemd", "user")
	}
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return res, fmt.Errorf("create unit dir: %w", err)
	}
	unitPath := filepath.Join(unitDir, opts.UnitName+".service")
	res.UnitPath = unitPath

	if _, err := os.Stat(unitPath); err == nil && !opts.Force {
		res.AlreadyExisted = true
		return res, fmt.Errorf("unit already exists at %s (pass --force to overwrite)", unitPath)
	}

	tmp := unitPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return res, fmt.Errorf("write unit: %w", err)
	}
	if err := os.Rename(tmp, unitPath); err != nil {
		_ = os.Remove(tmp)
		return res, fmt.Errorf("install unit: %w", err)
	}

	if err := runSystemctl("--user", "daemon-reload"); err != nil {
		return res, fmt.Errorf("systemctl daemon-reload: %w", err)
	}

	if !opts.NoEnable {
		if err := runSystemctl("--user", "enable", opts.UnitName+".service"); err != nil {
			return res, fmt.Errorf("systemctl enable: %w", err)
		}
		res.Enabled = true
	}

	if !opts.NoStart {
		if err := runSystemctl("--user", "restart", opts.UnitName+".service"); err != nil {
			// First install: restart may fail if never started; try start.
			if err2 := runSystemctl("--user", "start", opts.UnitName+".service"); err2 != nil {
				return res, fmt.Errorf("systemctl start: %w", err2)
			}
		}
		res.Started = true
	}

	if !opts.NoLinger {
		u, err := user.Current()
		if err == nil {
			if err := runCmd("loginctl", "enable-linger", u.Username); err != nil {
				// Non-fatal: unit works while logged in.
				res.LingerEnabled = false
			} else {
				res.LingerEnabled = true
			}
		}
	}

	return res, nil
}

func normalize(opts Options) (Options, error) {
	if opts.UnitName == "" {
		opts.UnitName = "mcremote"
	}
	if strings.Contains(opts.UnitName, "/") || strings.HasSuffix(opts.UnitName, ".service") {
		return opts, fmt.Errorf("unit name must be a bare name (got %q)", opts.UnitName)
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
	// Require a real binary only when installing the unit (not --print-only / RenderUnit).
	if !opts.PrintOnly && !isExecutableFile(opts.Binary) {
		return opts, fmt.Errorf(
			"binary not found or not executable: %s\nInstall first with: make install\nOr pass --binary /path/to/mcremote",
			opts.Binary,
		)
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

	data := templateData{
		UnitName:         opts.UnitName,
		Binary:           shellQuote(opts.Binary),
		ConfigPath:       shellQuote(opts.ConfigPath),
		DataDir:          shellQuote(opts.DataDir),
		ListenHost:       opts.ListenHost,
		ListenPort:       portStr,
		LogLevel:         opts.LogLevel,
		LogFormat:        opts.LogFormat,
		WorkingDirectory: shellQuote(opts.WorkingDirectory),
		Home:             shellQuote(home),
		User:             username,
		Path:             pathEnv,
		XDGConfigHome:    shellQuote(xdgConfigHome()),
		XDGDataHome:      shellQuote(xdgDataHome()),
		XDGCacheHome:     shellQuote(xdgCacheHome()),
		XDGRuntimeDir:    shellQuote(os.Getenv("XDG_RUNTIME_DIR")),
		ExtraEnviron:     opts.ExtraEnviron,
		DocsHint:         shellQuote(filepath.Join(home, ".config", "mcremote")),
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

// shellQuote is minimal quoting for paths with spaces in systemd unit values.
func shellQuote(s string) string {
	if s == "" {
		return s
	}
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0o111 != 0
}

func runSystemctl(args ...string) error {
	return runCmd("systemctl", args...)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withUserRuntimeEnv(os.Environ())
	if err := cmd.Run(); err != nil {
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
