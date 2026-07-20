package cli

import (
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/config"
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
	envPairs   []string
}

func (f *setupServiceFlags) toOptions() service.Options {
	cfg := f.configPath
	if cfg == "" {
		cfg = cfgFile
	}
	return service.Options{
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
	fs.StringVar(&f.unitName, "unit-name", "mcremote", "systemd user unit name (without .service)")
	fs.StringVar(&f.binary, "binary", "", "ExecStart binary path (default: ~/.local/bin/mcremote if present, else this executable)")
	fs.StringVar(&f.configPath, "service-config", "", "config file path written into the unit (falls back to --config)")
	// data-dir / listen-host / listen-port may already exist on serve; on root/setup they are setup-specific.
	if fs.Lookup("data-dir") == nil {
		fs.StringVar(&f.dataDir, "data-dir", "", "data directory passed to serve")
	}
	if fs.Lookup("listen-host") == nil {
		fs.StringVar(&f.listenHost, "listen-host", config.ListenHostTailscale,
			"listen host for the service (\"tailscale\" binds the tailnet IPv4 only; 0.0.0.0 is an explicit opt-in)")
	}
	if fs.Lookup("listen-port") == nil {
		fs.IntVar(&f.listenPort, "listen-port", 7531, "listen port for the service")
	}
	fs.StringVar(&f.workingDir, "working-directory", "", "WorkingDirectory for the unit (default: $HOME)")
	fs.BoolVar(&f.printOnly, "print-only", false, "print the unit file to stdout; do not install")
	fs.BoolVar(&f.force, "force", false, "overwrite an existing unit file")
	fs.BoolVar(&f.noEnable, "no-enable", false, "do not systemctl --user enable")
	fs.BoolVar(&f.noStart, "no-start", false, "do not systemctl --user start/restart")
	fs.BoolVar(&f.noLinger, "no-linger", false, "do not loginctl enable-linger (service stops on logout)")
	fs.StringArrayVar(&f.envPairs, "env", nil, "extra Environment= entries (KEY=VALUE); repeatable")
}

func runSetupService(cmd *cobra.Command, f setupServiceFlags) error {
	opts := f.toOptions()
	res, err := service.Setup(opts)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if f.printOnly {
		fmt.Fprint(out, res.UnitBody)
		return nil
	}

	fmt.Fprintf(out, "ExecStart binary:  %s\n", res.Binary)
	fmt.Fprintf(out, "Unit file:         %s\n", res.UnitPath)
	fmt.Fprintf(out, "Unit name:         %s.service\n", res.UnitName)
	if res.Enabled {
		fmt.Fprintln(out, "Enabled:           yes (systemctl --user enable)")
	} else {
		fmt.Fprintln(out, "Enabled:           skipped")
	}
	if res.Started {
		fmt.Fprintln(out, "Started:           yes (systemctl --user restart/start)")
	} else {
		fmt.Fprintln(out, "Started:           skipped")
	}
	if res.LingerEnabled {
		fmt.Fprintln(out, "Linger:            yes (survives logout)")
	} else if !f.noLinger {
		fmt.Fprintln(out, "Linger:            not enabled (run: loginctl enable-linger $USER)")
	} else {
		fmt.Fprintln(out, "Linger:            skipped")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Note: setup-service does not install the binary.")
	fmt.Fprintln(out, "      Install/update it with: make install")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Status:  systemctl --user status %s\n", res.UnitName)
	fmt.Fprintf(out, "Logs:    journalctl --user -u %s -f\n", res.UnitName)
	fmt.Fprintf(out, "Stop:    systemctl --user stop %s\n", res.UnitName)
	fmt.Fprintf(out, "Disable: systemctl --user disable --now %s\n", res.UnitName)
	return nil
}

func newSetupServiceCmd() *cobra.Command {
	var f setupServiceFlags

	cmd := &cobra.Command{
		Use:   "setup-service",
		Short: "Install a systemd --user unit and start mcremote",
		Long: strings.TrimSpace(`
Install mcremote as a fully managed systemd user service (unit only — no binary copy):

  0. Prerequisite: install the binary with  make install  (→ ~/.local/bin/mcremote)
  1. Write ~/.config/systemd/user/mcremote.service from the embedded template
  2. systemctl --user daemon-reload && enable && start
  3. loginctl enable-linger (so the daemon survives logout)

ExecStart defaults to ~/.local/bin/mcremote when that file exists; override with --binary.

Also available as a root flag: mcremote --setup-service
`),
		Example: setupServiceExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupService(cmd, f)
		},
	}
	bindSetupServiceFlags(cmd, &f)
	return cmd
}
