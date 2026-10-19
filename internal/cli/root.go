// Package cli implements the mcremote Cobra command tree.
package cli

import (
	"os"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/spf13/cobra"
)

var (
	cfgFile   string
	logLevel  string
	logFormat string
)

// VersionString returns this build's identity line, buildinfo.Identity():
// "v1.2.3 (release) 0123456789ab" for a release, "<stamp> (local) …"
// otherwise. `version` and `--version` print it first, because the library's
// build workflow and installers run them for it (MADR 0054 S2).
func VersionString() string {
	return buildinfo.Identity().String()
}

// runningVersion is the version this build reports to clients and in logs:
// the stamped version, verbatim, so a release reports its tag with its v
// (MADR 0054 D13).
func runningVersion() string {
	return buildinfo.Identity().Current()
}

// Execute runs the root command.
func Execute() error {
	root := newRootCmd()
	return root.Execute()
}

func newRootCmd() *cobra.Command {
	var (
		setupService bool
		setupFlags   setupServiceFlags
	)

	cmd := &cobra.Command{
		Use:   "mcremote",
		Short: "Multi-CLI remote control orchestrator daemon",
		Long: `mcremote attaches to coding agent CLIs and exposes a secure remote control plane
for Flutter clients over Headscale/Tailscale.

All long flags use a double dash (--help, --config, --setup-service, --listen-host, …).
Short -h is help only. See docs/guides/config.md for the full flag and MCREMOTE_* env reference.`,
		Example:       rootExample,
		Version:       VersionString(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// Accept flags after subcommands consistently; long form is always --flag.
		TraverseChildren: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			if setupService {
				return runSetupService(cmd, setupFlags)
			}
			return cmd.Help()
		},
	}
	// --version uses double-dash via Cobra's built-in version flag.
	cmd.SetVersionTemplate("{{.Version}}\n")

	// Global long flags (--config, --log-level, --log-format, --help).
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: $XDG_CONFIG_HOME/mcremote/config.yaml or MCREMOTE_CONFIG)")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level (debug|info|warn|error); env MCREMOTE_LOG_LEVEL")
	cmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "log format (text|json); env MCREMOTE_LOG_FORMAT")

	// Root --setup-service (same as `mcremote setup-service`).
	cmd.Flags().BoolVar(&setupService, "setup-service", false, "install and start the background service (Linux: systemd --user + linger; macOS: launchd LaunchAgent; does not install the binary)")
	bindSetupServiceFlags(cmd, &setupFlags)

	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newPairCmd())
	cmd.AddCommand(newSetupServiceCmd())
	cmd.AddCommand(newEnginesCmd())
	cmd.AddCommand(newPathsCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newReceiptsCmd())
	cmd.AddCommand(newAuthRecoveryCmd())

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	// Disable the default completion command noise in help? Keep completion (cobra default).
	return cmd
}
