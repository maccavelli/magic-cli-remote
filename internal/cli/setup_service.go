package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/spf13/cobra"
)

// Shared setup-service options (root --setup-service and setup-service subcommand).
type setupServiceFlags struct {
	unitName   string
	binary     string
	configPath string
	dataDir    string
	listenHost string
	listenPort int
	workingDir string
	printOnly  bool
	force      bool
	noEnable   bool
	noStart    bool
	noLinger   bool
	remove     bool
	refresh    bool
	jsonOut    bool
	envPairs   []string
}

func (f *setupServiceFlags) toOptions() service.Options {
	cfg := f.configPath
	if cfg == "" {
		cfg = cfgFile
	}
	return service.Options{
		Product:          "mcremote",
		UnitName:         f.unitName,
		Binary:           f.binary,
		ConfigPath:       cfg,
		DataDir:          f.dataDir,
		ListenHost:       f.listenHost,
		ListenPort:       f.listenPort,
		LogLevel:         logLevel,
		LogFormat:        logFormat,
		WorkingDirectory: f.workingDir,
		ExtraEnviron:     f.envPairs,
		PrintOnly:        f.printOnly,
		Force:            f.force,
		NoEnable:         f.noEnable,
		NoStart:          f.noStart,
		NoLinger:         f.noLinger,
	}
}

func bindSetupServiceFlags(cmd *cobra.Command, f *setupServiceFlags) {
	// All long flags use double-dash form: --unit-name, --force, etc.
	// (Cobra also accepts -h for help. Do not use single-dash long names.)
	fs := cmd.Flags()
	if fs.Lookup("unit-name") != nil {
		// Already bound (shared root + subcommand careful call).
		return
	}
	fs.StringVar(&f.unitName, "unit-name", "mcremote", "service name (Linux unit without .service; macOS maps to a launchd Label)")
	fs.StringVar(&f.binary, "binary", "", "serve binary path (default: ~/.local/bin/mcremote if present, else this executable)")
	fs.StringVar(&f.configPath, "service-config", "", "config file path written into the service (falls back to --config)")
	// data-dir / listen-host / listen-port may already exist on serve; on root/setup they are setup-specific.
	if fs.Lookup("data-dir") == nil {
		fs.StringVar(&f.dataDir, "data-dir", "", "data directory passed to serve")
	}
	// Empty defaults on purpose: a value here is baked into ExecStart/ProgramArguments and CLI
	// flags beat config.yaml in serve — a non-empty default would silently pin
	// listen settings and override later config edits.
	if fs.Lookup("listen-host") == nil {
		fs.StringVar(&f.listenHost, "listen-host", "",
			"listen host baked into the service (default: follow config; \"tailscale\" binds the tailnet IPv4 only)")
	}
	if fs.Lookup("listen-port") == nil {
		fs.IntVar(&f.listenPort, "listen-port", 0, "listen port baked into the service (default: follow config)")
	}
	fs.StringVar(&f.workingDir, "working-directory", "", "working directory for the service (default: $HOME)")
	fs.BoolVar(&f.printOnly, "print-only", false, "print the unit/plist to stdout; do not install")
	fs.BoolVar(&f.force, "force", false, "overwrite an existing unit/plist file")
	fs.BoolVar(&f.noEnable, "no-enable", false, "do not enable the service (systemctl --user enable / launchctl enable)")
	fs.BoolVar(&f.noStart, "no-start", false, "do not start/restart the service")
	fs.BoolVar(&f.noLinger, "no-linger", false, "Linux: skip loginctl enable-linger. macOS: no effect (LaunchAgents are session-bound)")
	fs.BoolVar(&f.remove, "remove", false, "stop, disable, and delete the service definition (inverse of setup)")
	fs.BoolVar(&f.refresh, "refresh", false,
		"re-render the installed unit/plist from this binary's template, preserving the options baked into it; rewrites only what this binary wrote, and reloads the systemd user manager")
	fs.BoolVar(&f.jsonOut, "json", false, "with --refresh, print the result as one JSON object")
	fs.StringArrayVar(&f.envPairs, "env", nil, "extra environment entries (KEY=VALUE); repeatable")
}

func runSetupService(cmd *cobra.Command, f setupServiceFlags) error {
	out := cmd.OutOrStdout()
	opts := f.toOptions()

	if f.refresh {
		if f.remove {
			return fmt.Errorf("--refresh and --remove are mutually exclusive")
		}
		res, err := service.RefreshUnit(
			service.Options{Product: "mcremote", UnitName: f.unitName},
			service.RefreshOptions{PrintOnly: f.printOnly},
		)
		if err != nil {
			return err
		}
		if f.printOnly && res.Body != "" {
			fmt.Fprint(out, res.Body)
			return nil
		}
		return service.PrintRefreshResult(out, res, "mcremote", f.jsonOut)
	}

	if f.remove {
		res, err := service.Remove(opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Removed:           %s\n", res.UnitPath)
		fmt.Fprintln(out, "The daemon was stopped and the service disabled.")
		return nil
	}

	res, err := service.Setup(opts)
	if err != nil {
		// The definition may already be on disk (post-write step failed): say where.
		if res.UnitPath != "" && !f.printOnly {
			fmt.Fprintf(out, "Service file:      %s\n", res.UnitPath)
		}
		return err
	}

	if f.printOnly {
		fmt.Fprint(out, res.UnitBody)
		return nil
	}

	service.PrintSetupResult(out, res, f.noLinger, "mcremote")
	return nil
}

func newSetupServiceCmd() *cobra.Command {
	var f setupServiceFlags

	short := "Install mcremote as a background service and start it"
	if runtime.GOOS == "darwin" {
		short = "Install mcremote as a launchd LaunchAgent and start it"
	}
	long := strings.TrimSpace(`
Install mcremote as a managed background service (definition only — no binary copy):

  0. Prerequisite: install the binary with  make install  (→ ~/.local/bin/mcremote; also installs mcrelay)

Linux (systemd --user):
  1. Write ~/.config/systemd/user/mcremote.service
  2. systemctl --user daemon-reload && enable && start
  3. loginctl enable-linger (survives logout; skip with --no-linger)

macOS (launchd user LaunchAgent, no sudo):
  1. Write ~/Library/LaunchAgents/com.magiccliremote.mcremote.plist
  2. launchctl enable + bootstrap gui/$UID + kickstart
  3. Session-bound: stops on logout (no user-level linger)

--refresh re-renders an already-installed definition from this binary's
template, keeping the options baked into it (--listen-port, --service-config,
--env, ...). It rewrites only a definition this binary wrote and can reproduce,
backs the old one up as <unit>.prev, and reloads the systemd user manager.
mcremote update runs it after a swap so a release that fixes the unit can
actually deliver the fix; run it by hand to apply one without updating.

Also available as a root flag: mcremote --setup-service
`)

	cmd := &cobra.Command{
		Use:     "setup-service",
		Short:   short,
		Long:    long,
		Example: setupServiceExample,
		// No positional args: removal is `--remove`, so `setup-service remove`
		// is a mistake that must not fall through and (re)install + start.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupService(cmd, f)
		},
	}
	bindSetupServiceFlags(cmd, &f)
	return cmd
}
