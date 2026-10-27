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

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider/launch"
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
	// Default: the installed per-user binary if present, else this process's
	// executable — ~/.local/bin/<product> on Unix,
	// %LOCALAPPDATA%\Programs\<product>\<product>.exe on Windows
	// (MADR 0116 D13).
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
	// Health is the product's health wiring, the one the update passes: the
	// start check runs Health.Probe, and a Windows --remove sends
	// Health.RequestStop before it ends the task (MADR 0207 D40).
	Health HealthOptions
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
	// RemovedBackup is the definition's backup Remove deleted, the one a
	// refresh keeps and which can hold --env secrets; empty when there was
	// none (MADR 0210 D9).
	RemovedBackup string
	// KeptLogs is where a removed runit, s6 or OpenRC service's logs stay;
	// Remove keeps them (MADR 0212 PLAN P4b). Empty when there are none.
	KeptLogs string
	// StopFailure is why Remove's stop failed, when it went on to remove the
	// definition anyway; empty when the service stopped, or was not running
	// (MADR 0207 PLAN U6b).
	StopFailure string
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
	// Scope is "systemd-user", "launchd-agent" or "windows-task", or, when
	// NotConfigured, the Kind detected.
	Scope string
	// NotConfigured is set when the host has no service manager setup-service
	// installs under: nothing was written, and Advice says what to do instead.
	// It is a success (MADR 0054 amendment 4).
	NotConfigured bool
	// Advice is printed when NotConfigured.
	Advice []string
	// SupervisorStarted is set when setup started runsvdir or s6-svscan
	// itself, for this session only; SupervisorPID is its pid.
	SupervisorStarted bool
	SupervisorPID     int
	// Health is the verified start's outcome (HealthRunning or
	// HealthStarting); empty when nothing was started (MADR 0054 PLAN S5c).
	Health string
	// Warnings are printed after the result, without failing it.
	Warnings []string
	// written is set once the definition is on disk, so a setup that fails
	// afterwards still records it and --remove can find it.
	written bool
	// started is the backend that started the service, which the start check
	// waits on: its Stop recorded the instance a restart replaces (MADR 0207
	// D41).
	started *startedBy
}

// startedBy is the backend that started a service, and the product it was
// asked for.
type startedBy struct {
	b       Backend
	product string
}

// setupTimeout bounds setup's stop and start through a backend. Each backend
// bounds its own wait first, at most twice its poll timeout for a stop that
// fails and puts the service back. It is a variable for tests.
var setupTimeout = 5 * time.Minute

// taskEnabler is the Task Scheduler backend's Enable.
type taskEnabler interface {
	Enable(ctx context.Context, product string) error
}

// startThrough starts opts' service through kind's backend, and keeps that
// backend on res for the start check (MADR 0207 D4). With stop set it stops a
// running service first, through the same backend, whose Stop records the
// instance the start replaces, so the check does not take that one for the new
// one (D41). On Windows it enables the task before the start: Task.Start runs
// no task that another process disabled, an earlier stop included (D18, D30).
func startThrough(kind Kind, opts Options, res Result, stop bool) (Result, error) {
	b, err := setupBackend(kind, opts.Product, opts.UnitName, opts.Health)
	if err != nil {
		return res, fmt.Errorf("its service manager could not be reached: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	if stop {
		if err := b.Stop(ctx, opts.Product); err != nil {
			return res, fmt.Errorf("the running service could not be stopped: %w", err)
		}
	}
	if kind == KindWindowsTask {
		en, ok := b.(taskEnabler)
		if !ok {
			return res, fmt.Errorf("the %s backend cannot enable the task", kind)
		}
		if err := en.Enable(ctx, opts.Product); err != nil {
			return res, fmt.Errorf("the task could not be enabled: %w", err)
		}
	}
	if err := b.Start(ctx, opts.Product); err != nil {
		return res, fmt.Errorf("the service could not be started: %w", err)
	}
	res.Started = true
	res.started = &startedBy{b: b, product: opts.Product}
	return res, nil
}

// watchThrough keeps kind's backend on res for the start check of a service
// that a supervisor setup has just started brings up by itself: a new runsvdir
// or s6-svscan, a scanner taking up a new directory, or a new OpenRC session's
// default runlevel. Nothing ran before, so no instance is replaced, and the
// check counts the first one up.
func watchThrough(kind Kind, opts Options, res Result) (Result, error) {
	b, err := setupBackend(kind, opts.Product, opts.UnitName, opts.Health)
	if err != nil {
		return res, fmt.Errorf("its service manager could not be reached: %w", err)
	}
	res.Started = true
	res.started = &startedBy{b: b, product: opts.Product}
	return res, nil
}

// stopForRemoval stops opts' service through kind's backend before its
// definition is deleted (MADR 0207 D4). Two failures stop the removal before
// anything is changed, since the service would keep running from a definition
// that is gone: a backend that cannot be built, as for a wait it refuses, and a
// stop refused because this process runs inside the service. A stop that fails
// otherwise is tolerated, as Remove always has: the definition goes either way.
// That failure is returned as failure, for the result to report, so the result
// does not say the service was stopped (MADR 0207 PLAN U6b). A service that is
// not installed had nothing to stop, and is no failure.
func stopForRemoval(kind Kind, opts Options) (failure string, err error) {
	b, err := setupBackend(kind, opts.Product, opts.UnitName, opts.Health)
	if err != nil {
		return "", fmt.Errorf("not removed: its service manager could not be reached: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	switch err := b.Stop(ctx, opts.Product); {
	case err == nil, errors.Is(err, libservice.ErrNotInstalled):
		return "", nil
	case errors.Is(err, libservice.ErrInsideService):
		return "", fmt.Errorf("not removed: %w; run setup-service --remove from a shell outside the service", err)
	default:
		return err.Error(), nil
	}
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
//
// It also pins the detected kind to that OS's default (systemd-user on linux),
// so a test that pins the OS never probes the host's service manager. Use
// OverrideDetectedKind after it for another kind.
func OverrideInstallOS(osName string) (restore func()) {
	prev, prevDetect := installOS, detectKind
	installOS = osName
	detectKind = func() Kind { return osDefaultKind(osName) }
	return func() { installOS, detectKind = prev, prevDetect }
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

// OverrideRunLaunchctlCapture replaces the capturing launchctl runner for
// tests. Restore with the returned func.
//
// The three overrides above drive commands whose *error* matters. This one
// drives `launchctl print`, whose *output* decides control flow: whether a job
// is loaded, and therefore whether a teardown is needed and when it finished
// (MADR 0125 D1). Without it the launchd sequencing cannot be tested off
// Darwin at all, which is why the teardown race reached a user.
func OverrideRunLaunchctlCapture(fn func(args ...string) (string, error)) (restore func()) {
	prev := runLaunchctlCapture
	if fn == nil {
		fn = func(args ...string) (string, error) { return runCmdOutput("launchctl", args...) }
	}
	runLaunchctlCapture = fn
	return func() { runLaunchctlCapture = prev }
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

	// A host with no service manager setup-service installs under gets advice
	// and success, and nothing is written: not the definition, not a default
	// config, not a record (MADR 0054 amendment 4, PLAN S5a).
	kind := osDefaultKind(installOS)
	if opts.PrintOnly {
		// Preview what this host would get; a host with nothing to supervise
		// previews the OS default.
		if k := detectKind(); k.installable() {
			kind = k
		}
	} else {
		kind = detectKind()
		if !kind.installable() {
			res.Scope = string(kind)
			res.NotConfigured = true
			res.Advice = unsupervisedAdvice(kind, detectEnvironment(), pid1Comm(), opts.Product, opts.Binary, opts.UnitName)
			return res, nil
		}
	}

	// A wait the start check cannot use is refused before anything is
	// written (MADR 0054 PLAN S5c, answer A3; MADR 0207 D42).
	if !opts.PrintOnly && !opts.NoStart {
		poll, err := setupPoll(kind)
		if err != nil {
			return res, err
		}
		// Task Scheduler refuses a drain that is not shorter than the
		// timeout (taskscheduler/task.go:149-150): mcremote's is 30 s.
		if kind == KindWindowsTask && opts.Health.RequestStop != nil && poll.Timeout > 0 && poll.Timeout <= opts.Health.StopGrace {
			return res, fmt.Errorf("%s=%d is not above the %s the daemon is given to drain", waitEnv, int(poll.Timeout/time.Second), opts.Health.StopGrace)
		}
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
		if kind.isScriptKind() {
			body, err = renderScript(kind, opts, nil)
		} else {
			body, err = render(opts)
		}
		res.Scope = string(kind)
		res.Label = opts.UnitName
	case "windows":
		body, err = renderTaskXML(opts, currentTaskUser())
		res.Scope = "windows-task"
		res.Label = taskNameFor(opts.Product)
	default:
		if opts.PrintOnly {
			// Preview systemd unit text on unsupported install platforms.
			body, err = render(opts)
			res.Scope = "systemd-user"
			res.Label = opts.UnitName
		} else {
			return res, fmt.Errorf("setup-service install is only supported on Linux, macOS and Windows (running on %s); use --print-only to preview", installOS)
		}
	}
	if err != nil {
		return res, err
	}
	res.UnitBody = body

	if opts.PrintOnly {
		return res, nil
	}

	// The name the definition is installed under is recorded, so every later
	// control and refresh path finds it (MADR 0201 D30). Windows registers the
	// task under the product's name whatever --unit-name says.
	recorded := opts.UnitName
	switch installOS {
	case "linux":
		if kind.isScriptKind() {
			res, err = setupScript(kind, opts, body, res)
		} else {
			res, err = setupSystemd(opts, body, res)
		}
	case "darwin":
		res, err = setupLaunchdAgent(opts, body, res)
	case "windows":
		recorded = taskNameFor(opts.Product)
		res, err = setupSchtasks(opts, body, res)
	default:
		return res, fmt.Errorf("setup-service install is only supported on Linux, macOS and Windows (running on %s)", installOS)
	}
	if err != nil {
		if res.written {
			// The definition exists: record it, so --remove and --refresh
			// find it after this failure (MADR 0054 PLAN S5b, Deviation 3).
			_ = recordUnitName(opts.Product, recorded)
			_ = recordBackend(opts.Product, kind)
		}
		return res, err
	}
	if err := recordUnitName(opts.Product, recorded); err != nil {
		return res, fmt.Errorf("the service is set up, but its unit name was not recorded: %w", err)
	}
	if err := recordBackend(opts.Product, kind); err != nil {
		return res, fmt.Errorf("the service is set up, but its backend was not recorded: %w", err)
	}
	if res.Started && !opts.NoStart {
		return verifyStart(kind, res)
	}
	return res, nil
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
		// A stop, then a start, as `systemctl restart` did: the library's
		// Start on a unit already active changes nothing, and its WaitHealthy
		// would then wait for a new invocation that never comes (MADR 0207
		// amendment D, F4).
		var err error
		if res, err = startThrough(KindSystemdUser, opts, res, true); err != nil {
			return res, fmt.Errorf("unit installed at %s, but %w (%s)", unitPath, err, manual)
		}
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

	// --no-start writes the definition and touches no running instance: the
	// teardown used to run regardless, so --no-start stopped a running job
	// and left it stopped (MADR 0201 D22).
	if opts.NoStart {
		if !res.Unchanged {
			if loaded, err := launchdLoaded(svc); err == nil && loaded {
				fmt.Fprintf(os.Stderr, "note: %s is loaded; the running job keeps its current definition until restarted\n", svc)
			}
		}
		if !opts.NoEnable {
			if err := runLaunchctl("enable", svc); err != nil {
				return res, fmt.Errorf("plist installed at %s, but launchctl enable failed: %w (%s)", plistPath, err, manual)
			}
			res.Enabled = true
		}
		return res, nil
	}

	// The library's Job.Stop boots the job out and waits until it and its
	// process have gone; teardown is asynchronous, and bootstrapping into it
	// returned 5 and 37 on a real Mac (MADR 0125 D1). Job.Start then enables
	// the label, which a bootstrap needs, bootstraps it, and kickstarts it. A
	// plist that did not change goes the same way: no library call restarts a
	// loaded job in place (MADR 0207 D43, replacing 0125 D4 here).
	var serr error
	if res, serr = startThrough(KindLaunchdAgent, opts, res, true); serr != nil {
		// The hint reads launchctl's words, and every launchd error names the
		// gui/ target, so it is for a bootstrap that failed only.
		hint := ""
		if strings.Contains(serr.Error(), "bootstrap") {
			hint = bootstrapFailHint(serr)
		}
		return res, fmt.Errorf("plist installed at %s, but %w%s (%s)", plistPath, serr, hint, manual)
	}
	res.Enabled = true
	// Best-effort: surface a non-zero last exit if the job failed immediately.
	if note := launchdLastExitNote(svc); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	return res, nil
}

// launchdLastExitNote returns a short diagnostic when launchctl print shows a
// recent non-zero exit (best-effort; empty when print is unavailable).
func launchdLastExitNote(svc string) string {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := procutil.Command(ctx, "launchctl", "print", svc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	text := string(out)
	// Typical lines: "last exit code = 1:" or "last exit code = (never exited)"
	for line := range strings.SplitSeq(text, "\n") {
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
	cmd := procutil.Command(ctx, "plutil", "-lint", path)
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
		// The name setup-service recorded, else the product (MADR 0201 D30).
		name, err := ResolveUnitName(opts.Product)
		if err != nil {
			return Result{}, err
		}
		opts.UnitName = name
	}
	if !unitNameRe.MatchString(opts.UnitName) || strings.HasSuffix(opts.UnitName, ".service") {
		return Result{}, fmt.Errorf("unit name must be a bare name (got %q)", opts.UnitName)
	}

	var (
		res Result
		err error
	)
	recorded := opts.UnitName
	switch installOS {
	case "linux":
		kind, kerr := ResolveBackend(opts.Product)
		if kerr != nil {
			return Result{}, kerr
		}
		if kind.isScriptKind() {
			res, err = removeScript(kind, opts)
		} else {
			res, err = removeSystemd(opts)
		}
	case "darwin":
		res, err = removeLaunchdAgent(opts)
	case "windows":
		recorded = taskNameFor(opts.Product)
		res, err = removeSchtasks(opts)
	default:
		return Result{}, fmt.Errorf("setup-service --remove is only supported on Linux, macOS and Windows (running on %s)", installOS)
	}
	if err != nil {
		return res, err
	}
	// Removing the service removes its records (MADR 0201 D30, 0054 PLAN
	// S5a); removing another instance leaves them.
	owned, err := ResolveUnitName(opts.Product)
	if err != nil {
		return res, err
	}
	if err := forgetUnitName(opts.Product, recorded); err != nil {
		return res, fmt.Errorf("the service is removed, but its unit name record was not: %w", err)
	}
	if owned == recorded {
		if err := forgetBackend(opts.Product); err != nil {
			return res, fmt.Errorf("the service is removed, but its backend record was not: %w", err)
		}
	}
	return res, nil
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
	var err error
	if res.StopFailure, err = stopForRemoval(KindSystemdUser, opts); err != nil {
		return res, err
	}
	_ = runSystemctl("--user", "disable", svc)

	if err := os.Remove(unitPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove unit: %w", err)
	}
	wants := filepath.Join(unitDir, "default.target.wants", svc)
	if err := os.Remove(wants); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove enable symlink: %w", err)
	}
	backup, err := removeBackup(unitPath + ".prev")
	if err != nil {
		return res, err
	}
	res.RemovedBackup = backup
	if err := runSystemctl("--user", "daemon-reload"); err != nil {
		return res, fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	res.Removed = true
	return res, nil
}

// removeBackup deletes the backup a refresh kept of a definition being
// removed, and returns its path, or "" when there was none (MADR 0210 D9).
func removeBackup(path string) (string, error) {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("remove the kept definition: %w", err)
	}
	return path, nil
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

	// The same Job.Stop as setup and the update: one teardown path to reason
	// about, not two.
	if res.StopFailure, err = stopForRemoval(KindLaunchdAgent, opts); err != nil {
		return res, err
	}
	_ = runLaunchctl("disable", svc)

	if err := os.Remove(plistPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, fmt.Errorf("remove plist: %w", err)
	}
	if res.RemovedBackup, err = removeBackup(plistPath + ".prev"); err != nil {
		return res, err
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
	cmd := procutil.Command(ctx, "systemctl", "--user", "is-system-running")
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
	if installOS == "windows" {
		// The task is always named after the product, and every lifecycle
		// probe looks it up by product, so a different name cannot be honoured
		// (MADR 0159 F17, D13). Refuse rather than accept and ignore it.
		if opts.UnitName != opts.Product {
			return opts, fmt.Errorf("--unit-name is not supported on Windows: the task is always named %s (MADR 0159 D13)", opts.Product)
		}
		// Task Scheduler cannot carry environment in the task XML (MADR 0159
		// probe 9); until --env has a Windows delivery path (D6), refuse it
		// instead of dropping it silently (F9).
		if len(opts.ExtraEnviron) > 0 {
			return opts, fmt.Errorf("--env is not yet supported on Windows (MADR 0159 D6)")
		}
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
		userBin := defaultInstalledBinary(home, opts.Product)
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

// servicePathEnv builds PATH for the service.
//
// mcrelay (0091 D1) does not exec coding CLIs: a closed set only
// (~/.local/bin, /usr/local/bin, /usr/bin, /bin). mcremote prepends
// user tool prefixes and Homebrew to the ambient PATH (deduplicated).
func servicePathEnv(home, product string) string {
	extras := servicePathExtras(home, product)
	if product == "mcrelay" {
		return strings.Join(extras, ":")
	}
	pathEnv := os.Getenv("PATH")
	for _, e := range extras {
		if e != "" && !strings.Contains(":"+pathEnv+":", ":"+e+":") {
			pathEnv = e + ":" + pathEnv
		}
	}
	return pathEnv
}

// servicePathExtras are the prefixes this release wants on the service PATH.
// For mcrelay that is the whole PATH; for mcremote they are prepended to the
// ambient one. A refresh compares them against the installed definition to
// report what a new release would add (MADR 0100 F4).
func servicePathExtras(home, product string) []string {
	if product == "mcrelay" {
		return []string{
			filepath.Join(home, ".local", "bin"),
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
		}
	}
	return []string{
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
}

func unitTemplateFor(product string) string {
	if product == "mcrelay" {
		return unitTemplateMcrelay
	}
	return unitTemplateMcremote
}

// renderEnv pins the environment-derived values a unit was rendered with.
// render otherwise reads them from the process doing the rendering, which makes
// mcremote's PATH a function of whoever runs it (servicePathEnv builds on
// os.Getenv("PATH")). A refresh must reproduce the installed values instead of
// recomputing them, or an update run over ssh rewrites the daemon's PATH to the
// ssh session's (MADR 0100 F4).
type renderEnv struct {
	Home          string
	User          string
	Path          string
	XDGConfigHome string
	XDGDataHome   string
	XDGStateHome  string
	XDGCacheHome  string
	// XDGRuntimeDir empty makes the unit name systemd's %t, the user manager's
	// runtime directory, so the service never falls back to its private /tmp
	// (MADR 0201 D27).
	XDGRuntimeDir string
}

// currentRenderEnv reads the render inputs from this process.
func currentRenderEnv(product string) renderEnv {
	home, _ := os.UserHomeDir()
	u, _ := user.Current()
	username := ""
	if u != nil {
		username = u.Username
	}
	return renderEnv{
		Home:          home,
		User:          username,
		Path:          servicePathEnv(home, product),
		XDGConfigHome: xdgConfigHome(),
		XDGDataHome:   xdgDataHome(),
		XDGStateHome:  xdgStateHome(),
		XDGCacheHome:  xdgCacheHome(),
		XDGRuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
	}
}

func render(opts Options) (string, error) {
	return renderWith(opts, nil)
}

// renderWith renders the unit for opts. A nil penv computes the environment
// block from this process (what Setup does); a non-nil penv pins it.
func renderWith(opts Options, penv *renderEnv) (string, error) {
	renv := currentRenderEnv(opts.Product)
	if penv != nil {
		renv = *penv
	}
	home := renv.Home
	username := renv.User
	pathEnv := renv.Path

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
		XDGConfigHome:    systemdQuote(renv.XDGConfigHome),
		XDGDataHome:      systemdQuote(renv.XDGDataHome),
		XDGStateHome:     systemdQuote(renv.XDGStateHome),
		XDGCacheHome:     systemdQuote(renv.XDGCacheHome),
		XDGRuntimeDir:    systemdQuote(renv.XDGRuntimeDir),
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
	// EnsurePrivateDir, not MkdirAll+Chmod: the 0700 is ignored by Windows, so
	// the old pair made this directory private on POSIX only while the config
	// inside it may hold a registration secret (MADR 0155 D1/F4). The primitive
	// is converging — it repairs a pre-existing directory, which is what the
	// Chmod below it used to do on one platform — and on Windows installing a
	// private DACL re-propagates to the files already inside (0155 F7).
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		return path, false, fmt.Errorf("create config dir %s: %w", dir, err)
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
	// Delegates so there is ONE answer to "can this platform run that file".
	// The inline Mode()&0o111 test this replaces is always false on Windows —
	// Go derives the mode from file attributes, so a real mcremote.exe reported
	// as not executable and setup-service refused its own binary
	// (MADR 0116 F23c/D24).
	return launch.IsExecutableFile(path)
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
	cmd := procutil.Command(ctx, name, args...)
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
// The supervisor tools run through toolCall instead (MADR 0212 D2-A).
func runCmdOutput(name string, args ...string) (string, error) {
	return runCmdOutputEnv(nil, name, args...)
}

// runCmdOutputEnv is runCmdOutput with extra KEY=VALUE entries, which win over
// the inherited ones (os/exec uses the last value of a duplicated key).
func runCmdOutputEnv(extra []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	var buf bytes.Buffer
	cmd := procutil.Command(ctx, name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Env = append(withUserRuntimeEnv(os.Environ()), extra...)
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
	if rd, ok := userRuntimeDir(); ok {
		return append(base, "XDG_RUNTIME_DIR="+rd)
	}
	return base
}

// userRuntimeRoot is where systemd-logind makes each user's runtime
// directory. It is a variable for tests.
var userRuntimeRoot = "/run/user"

// userRuntimeDir returns the calling user's runtime directory, when it exists.
func userRuntimeDir() (string, bool) {
	u, err := user.Current()
	if err != nil {
		return "", false
	}
	rd := filepath.Join(userRuntimeRoot, u.Uid)
	if st, err := os.Stat(rd); err == nil && st.IsDir() {
		return rd, true
	}
	return "", false
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

// defaultInstalledBinary is where the per-user installer puts <product>.
//
// Unix: ~/.local/bin/<product>, what `make install` and install.sh write.
// Windows: %LOCALAPPDATA%\Programs\magic-cli-remote\<product>.exe, the
// library installer's folder (MADR 0054 D9, PLAN S5g). It replaces install.ps1's
// %LOCALAPPDATA%\Programs\<product>\<product>.exe (MADR 0116 D13), which a
// refresh moves installs out of.
//
// Without the Windows branch, setup-service would bake whatever path the user
// happened to invoke into the scheduled task — a downloads folder, a temp
// extract — and the task would break the moment that file moved.
func defaultInstalledBinary(home, product string) string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "Programs", installerName, product+".exe")
	}
	return filepath.Join(home, ".local", "bin", product)
}
