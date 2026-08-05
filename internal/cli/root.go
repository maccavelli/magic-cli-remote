// Package cli implements the mcremote Cobra command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"

	cfgFile   string
	logLevel  string
	logFormat string
)

// SetVersionInfo injects build-time version metadata.
func SetVersionInfo(v, c, d string) {
	if v != "" {
		version = v
	}
	if c != "" {
		commit = c
	}
	if d != "" {
		date = d
	}
}

// VersionString returns a human-readable version line.
func VersionString() string {
	return fmt.Sprintf("mcremote %s (%s) %s", version, commit, date)
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
Short -h is help only. See docs/config.md for the full flag and MCREMOTE_* env reference.`,
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

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	// Disable the default completion command noise in help? Keep completion (cobra default).
	return cmd
}
