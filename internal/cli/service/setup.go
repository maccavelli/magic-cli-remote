// Package service installs and manages the mcremote/mcrelay background service:
// systemd --user units on Linux and launchd user LaunchAgents on macOS.
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

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

//go:embed mcremote.user.service.tmpl
var unitTemplateMcremote string

//go:embed mcrelay.user.service.tmpl
var unitTemplateMcrelay string

//go:embed defaults_mcremote.yaml
var defaultConfigMcremote []byte

//go:embed defaults_mcrelay.yaml
var defaultConfigMcrelay []byte

// cmdTimeout bounds every systemctl/loginctl invocation: a hung user bus must
// not wedge the CLI forever.
const cmdTimeout = 30 * time.Second

// Options configure setup-service.
type Options struct {
	// Product is the binary/product name: "mcremote" (default) or "mcrelay".
	// Selects default unit name, binary path, description, and docs hint.
	Product string
	// Description overrides the unit Description= line.
	Description string
	// UnitName without .service suffix (default: Product).
	UnitName string
	// Binary is the absolute path written into ExecStart.
	// Default: ~/.local/bin/<product> if present, else this process's executable.
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
	// ConfigPath is the config file path used/ensured for this product.
	ConfigPath string
	// ConfigCreated is true when setup wrote a new default config.yaml.
	ConfigCreated bool
	// Label is the launchd Label (darwin) or unit basename for display.
	Label string
	// Domain is the launchd domain target (e.g. "gui/501"); empty on Linux.
	Domain string
	// LogDir is the macOS log directory; empty on Linux (journald).
	LogDir string
	// Scope is "systemd-user" or "launchd-agent".
	Scope string
}

type templateData struct {
	Product          string
	Description      string
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
	XDGStateHome     string
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

// launchdLabelRe matches reverse-DNS style launchd Labels.
var launchdLabelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

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

// LaunchdLabel returns the launchd Label for product/unitName.
// Bare defaults ("mcremote", "mcrelay", empty) map to com.magiccliremote.<product>.
// Names containing '.' are treated as reverse-DNS Labels after validation.
// Other bare names become com.magiccliremote.<unitName>.
func LaunchdLabel(product, unitName string) (string, error) {
	if product == "" {
		product = "mcremote"
	}
	switch product {
	case "mcremote", "mcrelay":
	default:
		return "", fmt.Errorf("product must be mcremote or mcrelay (got %q)", product)
	}
	if unitName == "" || unitName == product {
		label := "com.magiccliremote." + product
		return label, nil
	}
	if strings.Contains(unitName, ".") {
		if !launchdLabelRe.MatchString(unitName) {
			return "", fmt.Errorf("launchd label %q is invalid", unitName)
		}
		return unitName, nil
	}
	if !launchdLabelRe.MatchString(unitName) {
		return "", fmt.Errorf("unit name %q is not a valid launchd label component", unitName)
	}
	return "com.magiccliremote." + unitName, nil
}

// installOS is the OS used for Setup/Remove dispatch. Overridable in tests.
var installOS = runtime.GOOS

// runLaunchctl runs launchctl with args. Overridable in tests.
var runLaunchctl = func(args ...string) error {
	return runCmd("launchctl", args...)
}

// OverrideInstallOS sets the OS used by Setup/Remove for tests. Restore with the returned func.
func OverrideInstallOS(osName string) (restore func()) {
	prev := installOS
	installOS = osName
	return func() { installOS = prev }
}

// OverrideRunLaunchctl replaces the launchctl runner for tests. Restore with the returned func.
func OverrideRunLaunchctl(fn func(args ...string) error) (restore func()) {
	prev := runLaunchctl
	if fn == nil {
		fn = func(args ...string) error { return runCmd("launchctl", args...) }
	}
	runLaunchctl = fn
	return func() { runLaunchctl = prev }
}

// OverrideRunSystemctl replaces the systemctl runner for tests. Restore with
// the returned func. Symmetric to OverrideRunLaunchctl: it lets a test drive
// the systemd path on a host that has no systemctl, the same way the launchd
// path can be driven off Darwin.
func OverrideRunSystemctl(fn func(args ...string) error) (restore func()) {
	prev := runSystemctl
	if fn == nil {
		fn = func(args ...string) error { return runCmd("systemctl", args...) }
	}
	runSystemctl = fn
	return func() { runSystemctl = prev }
}

// Setup installs the user service definition only (never the binary), then
// enables and starts it. On Linux this is a systemd --user unit; on macOS a
// launchd user LaunchAgent. Re-running against a byte-identical existing file
// is a no-op for the file (enable/start still converge).
//
// When no --service-config is given, Setup ensures a default config.yaml under
// the product XDG config dir (~/.config/mcremote or ~/.config/mcrelay) and
// bakes that path into the service so it is never "defaults-only by accident".
func Setup(opts Options) (Result, error) {
	opts, err := normalize(opts)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Binary:   opts.Binary,
		UnitName: opts.UnitName,
	}

	// Ensure default config before rendering so argv can include
	// --config <xdg>/config.yaml when the operator did not pass a path.
	if !opts.PrintOnly {
		cfgPath, created, err := ensureDefaultConfig(opts)
		if err != nil {
			return res, err
		}
		res.ConfigPath = cfgPath
		res.ConfigCreated = created
		if opts.ConfigPath == "" && cfgPath != "" {
			opts.ConfigPath = cfgPath
		}
	} else if opts.ConfigPath == "" {
		if p, err := defaultConfigPath(opts.Product); err == nil {
			res.ConfigPath = p
			opts.ConfigPath = p
		}
	} else {
		res.ConfigPath = opts.ConfigPath
	}

	var body string
	switch installOS {
	case "darwin":
		body, err = renderPlist(opts)
		res.Scope = "launchd-agent"
		if label, lerr := LaunchdLabel(opts.Product, opts.UnitName); lerr == nil {
			res.Label = label
		}
	case "linux":
		body, err = render(opts)
		res.Scope = "systemd-user"
		res.Label = opts.UnitName
	default:
		if opts.PrintOnly {
			// Preview systemd unit text on unsupported install platforms.
			body, err = render(opts)
			res.Scope = "systemd-user"
			res.Label = opts.UnitName
		} else {
			return res, fmt.Errorf("setup-service install is only supported on Linux and macOS (running on %s); use --print-only to preview", installOS)
		}
	}
	if err != nil {
		return res, err
	}
	res.UnitBody = body

	if opts.PrintOnly {
		return res, nil
	}

	switch installOS {
	case "linux":
		return setupSystemd(opts, body, res)
	case "darwin":
		return setupLaunchdAgent(opts, body, res)
	default:
		return res, fmt.Errorf("setup-service install is only supported on Linux and macOS (running on %s)", installOS)
	}
}

func setupSystemd(opts Options, body string, res Result) (Result, error) {
	if err := preflightLinux(); err != nil {
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
	res.Scope = "systemd-user"
	res.Label = opts.UnitName

	mode := os.FileMode(0o644)
	if len(opts.ExtraEnviron) > 0 {
		mode = 0o600
	}

	if existing, err := os.ReadFile(unitPath); err == nil {
		res.AlreadyExisted = true
		if bytes.Equal(existing, []byte(body)) {
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
		if err := runSystemctl("--user", "restart", opts.UnitName+".service"); err != nil {
			return res, fmt.Errorf("unit installed at %s, but systemctl restart failed: %w (%s)", unitPath, err, manual)
		}
		res.Started = true
	}

	if !opts.NoLinger {
		if u, err := user.Current(); err == nil {
			res.LingerEnabled = runCmd("loginctl", "enable-linger", u.Username) == nil
		}
	}

	return res, nil
}

func setupLaunchdAgent(opts Options, body string, res Result) (Result, error) {
	if err := preflightDarwin(); err != nil {
		return res, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return res, fmt.Errorf("home dir: %w", err)
	}
	label, err := LaunchdLabel(opts.Product, opts.UnitName)
	if err != nil {
		return res, err
	}
	res.Label = label
	res.Scope = "launchd-agent"
	res.LingerEnabled = false

	logDir := filepath.Join(home, "Library", "Logs", opts.Product)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return res, fmt.Errorf("create log dir: %w", err)
	}
	res.LogDir = logDir

	plistDir := opts.UnitDir
	if plistDir == "" {
		plistDir = filepath.Join(home, "Library", "LaunchAgents")
	}
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return res, fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	plistPath := filepath.Join(plistDir, label+".plist")
	res.UnitPath = plistPath

	mode := os.FileMode(0o644)
	if len(opts.ExtraEnviron) > 0 {
		mode = 0o600
	}

	if existing, err := os.ReadFile(plistPath); err == nil {
		res.AlreadyExisted = true
		if bytes.Equal(existing, []byte(body)) {
			res.Unchanged = true
		} else if !opts.Force {
			return res, fmt.Errorf("plist already exists at %s with different content (pass --force to overwrite)", plistPath)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("stat plist: %w", err)
	}

	if !res.Unchanged {
		if err := writeUnitAtomic(plistDir, plistPath, []byte(body), mode); err != nil {
			return res, err
		}
	}

	if err := lintPlist(plistPath); err != nil {
		return res, err
	}

	uid, err := currentUID()
	if err != nil {
		return res, err
	}
	domain := "gui/" + uid
	res.Domain = domain
	svc := domain + "/" + label
	manual := fmt.Sprintf("finish manually with: launchctl bootout %s; launchctl enable %s; launchctl bootstrap %s %s; launchctl kickstart -k %s",
		svc, svc, domain, plistPath, svc)

	// Ignore bootout failures (not loaded yet).
	_ = runLaunchctl("bootout", svc)

	if !opts.NoEnable {
		if err := runLaunchctl("enable", svc); err != nil {
			return res, fmt.Errorf("plist installed at %s, but launchctl enable failed: %w (%s)", plistPath, err, manual)
		}
		res.Enabled = true
	}

	if !opts.NoStart {
		if err := runLaunchctl("bootstrap", domain, plistPath); err != nil {
			// Common when already bootstrapped after enable-only path; try kickstart.
			if err2 := runLaunchctl("kickstart", "-k", svc); err2 != nil {
				hint := bootstrapFailHint(err)
				return res, fmt.Errorf("plist installed at %s, but launchctl bootstrap failed: %w%s (%s)", plistPath, err, hint, manual)
			}
		} else if err := runLaunchctl("kickstart", "-k", svc); err != nil {
			return res, fmt.Errorf("plist installed at %s, but launchctl kickstart failed: %w (%s)", plistPath, err, manual)
		}
		res.Started = true
		// Best-effort: surface a non-zero last exit if the job failed immediately.
		if note := launchdLastExitNote(svc); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
	}

	return res, nil
}

// launchdLastExitNote returns a short diagnostic when launchctl print shows a
// recent non-zero exit (best-effort; empty when print is unavailable).
func launchdLastExitNote(svc string) string {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "launchctl", "print", svc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	text := string(out)
	// Typical lines: "last exit code = 1:" or "last exit code = (never exited)"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "last exit code") {
			continue
		}
		if strings.Contains(line, "(never exited)") || strings.Contains(line, "= 0") {
			return ""
		}
		return "warning: launchd reports " + line + " — check StandardErrorPath / Console.app"
	}
	return ""
}

func bootstrapFailHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "disabled") || strings.Contains(msg, "input/output") {
		return " — if the service was disabled in System Settings → General → Login Items, re-enable it or run launchctl enable first"
	}
	if strings.Contains(msg, "domain") || strings.Contains(msg, "gui/") {
		return " — log in via the GUI or Screen Sharing so the gui/$UID domain exists, then retry"
	}
	return ""
}

func lintPlist(path string) error {
	if _, err := exec.LookPath("plutil"); err != nil {
		return nil // plutil not available (e.g. Linux CI) — skip
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "plutil", "-lint", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("plutil -lint %s failed: %w (%s)", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func currentUID() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("current user: %w", err)
	}
	if u.Uid == "" {
		return "", fmt.Errorf("current user has empty Uid")
	}
	return u.Uid, nil
}

// Remove stops, disables, and deletes the service definition (inverse of Setup).
// Stop/disable failures are tolerated; binary, config, and logs are left intact.
func Remove(opts Options) (Result, error) {
	if opts.Product == "" {
		opts.Product = "mcremote"
	}
	if opts.UnitName == "" {
		opts.UnitName = opts.Product
	}
	if !unitNameRe.MatchString(opts.UnitName) || strings.HasSuffix(opts.UnitName, ".service") {
		return Result{}, fmt.Errorf("unit name must be a bare name (got %q)", opts.UnitName)
	}

	switch installOS {
	case "linux":
		return removeSystemd(opts)
	case "darwin":
		return removeLaunchdAgent(opts)
	default:
		return Result{}, fmt.Errorf("setup-service --remove is only supported on Linux and macOS (running on %s)", installOS)
	}
}

func removeSystemd(opts Options) (Result, error) {
	if err := preflightLinux(); err != nil {
		return Result{}, err
	}

	unitDir := opts.UnitDir
	if unitDir == "" {
		unitDir = filepath.Join(xdgConfigHome(), "systemd", "user")
	}
	unitPath := filepath.Join(unitDir, opts.UnitName+".service")
	res := Result{UnitName: opts.UnitName, UnitPath: unitPath, Scope: "systemd-user", Label: opts.UnitName}

	svc := opts.UnitName + ".service"
	_ = runSystemctl("--user", "stop", svc)
	_ = runSystemctl("--user", "disable", svc)

	if err := os.Remove(unitPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove unit: %w", err)
	}
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

func removeLaunchdAgent(opts Options) (Result, error) {
	if err := preflightDarwin(); err != nil {
		return Result{}, err
	}
	label, err := LaunchdLabel(opts.Product, opts.UnitName)
	if err != nil {
		return Result{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, fmt.Errorf("home dir: %w", err)
	}
	plistDir := opts.UnitDir
	if plistDir == "" {
		plistDir = filepath.Join(home, "Library", "LaunchAgents")
	}
	plistPath := filepath.Join(plistDir, label+".plist")
	uid, err := currentUID()
	if err != nil {
		return Result{}, err
	}
	domain := "gui/" + uid
	svc := domain + "/" + label
	res := Result{
		UnitName: opts.UnitName,
		UnitPath: plistPath,
		Label:    label,
		Domain:   domain,
		Scope:    "launchd-agent",
	}

	_ = runLaunchctl("bootout", svc)
	_ = runLaunchctl("disable", svc)

	if err := os.Remove(plistPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove plist: %w", err)
	}
	res.Removed = true
	return res, nil
}

// preflightLinux fails fast when systemd --user cannot work.
func preflightLinux() error {
	if installOS != "linux" {
		return fmt.Errorf("setup-service systemd path requires Linux (running on %s); use --print-only to preview", installOS)
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		// Allow tests that pin installOS="linux" on a non-Linux host, where
		// systemctl cannot exist. Symmetric to preflightDarwin, which already
		// tolerates a missing launchctl off Darwin. A real Linux host with no
		// systemctl still fails below.
		if runtime.GOOS != "linux" {
			return nil
		}
		return fmt.Errorf("systemctl not found in PATH — this host does not appear to run systemd; use --print-only to preview the unit")
	}
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

// preflightDarwin fails fast when launchctl is missing.
func preflightDarwin() error {
	if installOS != "darwin" {
		return fmt.Errorf("setup-service launchd path requires macOS (running on %s); use --print-only to preview", installOS)
	}
	if _, err := exec.LookPath("launchctl"); err != nil {
		// Allow tests that inject runLaunchctl without a real launchctl binary
		// only when installOS was overridden on a non-darwin host.
		if runtime.GOOS != "darwin" {
			return nil
		}
		return fmt.Errorf("launchctl not found in PATH — cannot manage LaunchAgents")
	}
	return nil
}

func normalize(opts Options) (Options, error) {
	if opts.Product == "" {
		opts.Product = "mcremote"
	}
	switch opts.Product {
	case "mcremote", "mcrelay":
	default:
		return opts, fmt.Errorf("product must be mcremote or mcrelay (got %q)", opts.Product)
	}
	if opts.UnitName == "" {
		opts.UnitName = opts.Product
	}
	if opts.Description == "" {
		switch opts.Product {
		case "mcrelay":
			opts.Description = "mcrelay outbound join-plane relay for mcremote"
		default:
			opts.Description = "mcremote multi-CLI remote control daemon"
		}
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
		userBin := filepath.Join(home, ".local", "bin", opts.Product)
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
			"binary %s is a temporary go-run/go-build artifact and will disappear.\nInstall a real binary first with: make install / make build-relay\nOr pass --binary /path/to/%s",
			opts.Binary, opts.Product,
		)
	}
	// Require a real binary only when installing the unit (not --print-only / RenderUnit).
	if !opts.PrintOnly && !isExecutableFile(opts.Binary) {
		return opts, fmt.Errorf(
			"binary not found or not executable: %s\nInstall first with: make install / make build-relay\nOr pass --binary /path/to/%s",
			opts.Binary, opts.Product,
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

// servicePathEnv builds PATH for the service: user tool prefixes, Homebrew
// prefixes, then the ambient PATH (deduplicated).
func servicePathEnv(home string) string {
	pathEnv := os.Getenv("PATH")
	extras := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".grok", "bin"),
		filepath.Join(home, ".opencode", "bin"),
		// Kilo's self-managed updates land under the cache dir (`kilo debug
		// paths` bin — MADR 0075 §4.9); npm/brew installs are covered by the
		// Homebrew/system prefixes below.
		filepath.Join(home, ".cache", "kilo", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".local", "go", "bin"),
		filepath.Join(home, ".local", "flutter", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	for _, e := range extras {
		if e != "" && !strings.Contains(":"+pathEnv+":", ":"+e+":") {
			pathEnv = e + ":" + pathEnv
		}
	}
	return pathEnv
}

func unitTemplateFor(product string) string {
	if product == "mcrelay" {
		return unitTemplateMcrelay
	}
	return unitTemplateMcremote
}

func render(opts Options) (string, error) {
	home, _ := os.UserHomeDir()
	u, _ := user.Current()
	username := ""
	if u != nil {
		username = u.Username
	}

	pathEnv := servicePathEnv(home)

	portStr := ""
	if opts.ListenPort > 0 {
		portStr = fmt.Sprintf("%d", opts.ListenPort)
	}

	env := make([]string, 0, len(opts.ExtraEnviron))
	for _, kv := range opts.ExtraEnviron {
		k, v, _ := strings.Cut(kv, "=")
		env = append(env, k+"="+systemdQuote(v))
	}

	docsApp := opts.Product
	data := templateData{
		Product:          opts.Product,
		Description:      opts.Description,
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
		XDGStateHome:     systemdQuote(xdgStateHome()),
		XDGCacheHome:     systemdQuote(xdgCacheHome()),
		XDGRuntimeDir:    systemdQuote(os.Getenv("XDG_RUNTIME_DIR")),
		ExtraEnviron:     env,
		DocsHint:         systemdQuote(filepath.Join(home, ".config", docsApp)),
	}

	tmpl, err := template.New("unit").Parse(unitTemplateFor(opts.Product))
	if err != nil {
		return "", fmt.Errorf("parse unit template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render unit: %w", err)
	}
	return buf.String(), nil
}

// defaultConfigPath returns the XDG config.yaml path for product.
func defaultConfigPath(product string) (string, error) {
	p, err := resolveProductPaths(product, "")
	if err != nil {
		return "", err
	}
	return p.ConfigFile, nil
}

// resolveProductPaths returns appdirs.Paths for product and optional dataDir.
func resolveProductPaths(product, dataDir string) (appdirs.Paths, error) {
	if product == "" {
		product = "mcremote"
	}
	prod, ok := appdirs.ProductByName(product)
	if !ok {
		prod = appdirs.Product{Name: product, LaunchLabel: "com.magiccliremote." + product}
	}
	if dataDir != "" && !filepath.IsAbs(dataDir) {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return appdirs.Paths{}, err
		}
		dataDir = abs
	}
	paths, _, err := appdirs.SystemPaths(prod, dataDir)
	return paths, err
}

// defaultConfigBody returns the embedded default YAML for product.
func defaultConfigBody(product string) []byte {
	switch product {
	case "mcrelay":
		return defaultConfigMcrelay
	default:
		return defaultConfigMcremote
	}
}

// ensureDefaultConfig creates the product XDG config dir and writes the
// embedded default config.yaml when missing. Never overwrites an existing
// file. Returns the absolute config path and whether a new file was written.
//
// If opts.ConfigPath is already set, that path is used: missing parent dirs
// are created and the default body is written only if the file is absent.
func ensureDefaultConfig(opts Options) (path string, created bool, err error) {
	path = strings.TrimSpace(opts.ConfigPath)
	if path == "" {
		path, err = defaultConfigPath(opts.Product)
		if err != nil {
			return "", false, err
		}
	}
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return "", false, err
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return path, false, fmt.Errorf("create config dir %s: %w", dir, err)
	}
	// Tighten pre-existing dir (MkdirAll is a no-op on 0755).
	if st, err := os.Stat(dir); err == nil && st.IsDir() && st.Mode().Perm() != 0o700 {
		_ = os.Chmod(dir, 0o700)
	}

	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return path, false, nil // already present — never overwrite
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, false, fmt.Errorf("stat config: %w", err)
	}

	body := defaultConfigBody(opts.Product)
	if len(body) == 0 {
		return path, false, fmt.Errorf("no embedded default config for product %q", opts.Product)
	}
	// 0600: config may hold registration secrets (mcrelay) or operational detail.
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return path, false, fmt.Errorf("write config: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return path, false, fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return path, false, fmt.Errorf("chmod config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return path, false, fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return path, false, fmt.Errorf("install config: %w", err)
	}
	ok = true
	return path, true, nil
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

var runSystemctl = func(args ...string) error {
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

// runCmdOutput runs a command capturing combined stdout+stderr (no terminal
// stream). Used by service control queries (is-active / launchctl print).
func runCmdOutput(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Env = withUserRuntimeEnv(os.Environ())
	err := cmd.Run()
	return buf.String(), err
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

// xdgConfigHome returns the absolute XDG config home (not product leaf).
func xdgConfigHome() string {
	p, err := resolveProductPaths("mcremote", "")
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config")
	}
	return filepath.Dir(p.ConfigDir)
}

func xdgDataHome() string {
	p, err := resolveProductPaths("mcremote", "")
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share")
	}
	return filepath.Dir(p.DataDir)
}

func xdgCacheHome() string {
	p, err := resolveProductPaths("mcremote", "")
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cache")
	}
	return filepath.Dir(p.CacheDir)
}

func xdgStateHome() string {
	p, err := resolveProductPaths("mcremote", "")
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "state")
	}
	return filepath.Dir(p.StateDir)
}
