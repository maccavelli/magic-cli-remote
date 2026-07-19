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
	cmd := &cobra.Command{
		Use:           "mcremote",
		Short:         "Multi-CLI remote control orchestrator daemon",
		Long:          "mcremote attaches to coding agent CLIs and exposes a secure remote control plane for Flutter clients over Headscale/Tailscale.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: $XDG_CONFIG_HOME/mcremote/config.yaml)")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level (debug|info|warn|error)")
	cmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "log format (text|json)")

	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newPairCmd())

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	return cmd
}
