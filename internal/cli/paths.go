package cli

import (
	"encoding/json"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/spf13/cobra"
)

func newPathsCmd() *cobra.Command {
	var asJSON bool
	var dataDir string
	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Print resolved XDG path layout (no mutation)",
		Long: `Print the effective config, data, state, cache, runtime, and admin socket
paths for mcremote after applying --config, MCREMOTE_*, and --data-dir.

Does not create directories or start the daemon.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.LoadOptions{
				ConfigFile: cfgFile,
				Flags:      cmd.Flags(),
			})
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("data-dir") {
				cfg.DataDir = dataDir
				if err := cfg.RecomputePaths(); err != nil {
					return err
				}
			}
			return printPaths(cmd, pathReport{
				Product:           "mcremote",
				ConfigFile:        cfg.ConfigFile,
				ConfigDir:         cfg.Paths.ConfigDir,
				DataDir:           cfg.DataDir,
				StateDir:          cfg.Paths.StateDir,
				CacheDir:          cfg.Paths.CacheDir,
				RuntimeDir:        cfg.Paths.RuntimeDir,
				AdminSocket:       cfg.Paths.AdminSocket,
				EngineRegistryDir: cfg.Paths.EngineRegistryDir,
				InstanceKey:       cfg.Paths.InstanceKey,
				LogDir:            cfg.Paths.LogDir,
				Diagnostics:       cfg.Diagnostics,
			}, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory (overrides config)")
	return cmd
}

type pathReport struct {
	Product           string `json:"product"`
	ConfigFile        string `json:"config_file,omitempty"`
	ConfigDir         string `json:"config_dir"`
	DataDir           string `json:"data_dir"`
	StateDir          string `json:"state_dir"`
	CacheDir          string `json:"cache_dir"`
	RuntimeDir        string `json:"runtime_dir"`
	AdminSocket       string `json:"admin_socket"`
	EngineRegistryDir string `json:"engine_registry_dir"`
	InstanceKey       string `json:"instance_key"`
	LogDir            string `json:"log_dir,omitempty"`
	Diagnostics       any    `json:"diagnostics,omitempty"`
}

func printPaths(cmd *cobra.Command, r pathReport, asJSON bool) error {
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	_, _ = fmt.Fprintf(out, "product:            %s\n", r.Product)
	if r.ConfigFile != "" {
		_, _ = fmt.Fprintf(out, "config_file:        %s\n", r.ConfigFile)
	}
	_, _ = fmt.Fprintf(out, "config_dir:         %s\n", r.ConfigDir)
	_, _ = fmt.Fprintf(out, "data_dir:           %s\n", r.DataDir)
	_, _ = fmt.Fprintf(out, "state_dir:          %s\n", r.StateDir)
	_, _ = fmt.Fprintf(out, "cache_dir:          %s\n", r.CacheDir)
	_, _ = fmt.Fprintf(out, "runtime_dir:        %s\n", r.RuntimeDir)
	_, _ = fmt.Fprintf(out, "admin_socket:       %s\n", r.AdminSocket)
	_, _ = fmt.Fprintf(out, "engine_registry:    %s\n", r.EngineRegistryDir)
	_, _ = fmt.Fprintf(out, "instance_key:       %s\n", r.InstanceKey)
	if r.LogDir != "" {
		_, _ = fmt.Fprintf(out, "log_dir:            %s\n", r.LogDir)
	}
	return nil
}
