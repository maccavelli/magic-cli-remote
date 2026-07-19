package cli

import (
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/spf13/cobra"
)

func newPairCmd() *cobra.Command {
	var name string

	createFn := func(cmd *cobra.Command, args []string) error {
		store, cfg, err := openStoreFromFlags(cmd)
		if err != nil {
			return err
		}
		dev, token, err := store.Create(name)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Device: %s (id=%s)\n", dev.Name, dev.ID)
		fmt.Fprintf(out, "Token:  %s\n", token)
		fmt.Fprintf(out, "WS URL (local):  ws://%s/v1/ws\n", cfg.Addr())
		fmt.Fprintf(out, "WS URL (mesh):   ws://<tailnet-host>:%d/v1/ws\n", cfg.Listen.Port)
		fmt.Fprintln(out, "Store this token; it will not be shown again.")
		return nil
	}

	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Manage device pairing tokens",
		Long:  "Create, list, or revoke device tokens used to authenticate WebSocket clients.\nRunning pair without a subcommand creates a new device.",
		RunE:  createFn,
	}
	cmd.Flags().StringVar(&name, "name", "device", "device label")
	cmd.Flags().String("data-dir", "", "data directory (overrides config)")

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new device token (shown once)",
		RunE:  createFn,
	}
	createCmd.Flags().StringVar(&name, "name", "device", "device label")
	createCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List paired devices (metadata only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openStoreFromFlags(cmd)
			if err != nil {
				return err
			}
			devices, err := store.List()
			if err != nil {
				return err
			}
			if len(devices) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No devices paired.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tCREATED\tLAST_USED")
			for _, d := range devices {
				last := "-"
				if d.LastUsedAt != nil {
					last = d.LastUsedAt.Format("2006-01-02T15:04:05Z")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					d.ID, d.Name, d.CreatedAt.Format("2006-01-02T15:04:05Z"), last)
			}
			return w.Flush()
		},
	}
	listCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	revokeCmd := &cobra.Command{
		Use:   "revoke <device-id-or-name>",
		Short: "Revoke a paired device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openStoreFromFlags(cmd)
			if err != nil {
				return err
			}
			dev, err := store.Revoke(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked device %s (%s)\n", dev.Name, dev.ID)
			return nil
		},
	}
	revokeCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	cmd.AddCommand(createCmd, listCmd, revokeCmd)
	return cmd
}

func openStoreFromFlags(cmd *cobra.Command) (*auth.Store, config.Config, error) {
	cfg, err := config.Load(config.LoadOptions{
		ConfigFile: cfgFile,
		Flags:      cmd.Flags(),
	})
	if err != nil {
		return nil, config.Config{}, err
	}
	if f := cmd.Flags().Lookup("data-dir"); f != nil && cmd.Flags().Changed("data-dir") {
		cfg.DataDir = f.Value.String()
	}
	// Also check parent flags.
	if cmd.Parent() != nil {
		if f := cmd.Parent().Flags().Lookup("data-dir"); f != nil && cmd.Parent().Flags().Changed("data-dir") {
			cfg.DataDir = f.Value.String()
		}
	}
	store, err := auth.OpenStore(filepath.Join(cfg.DataDir, "devices.json"))
	if err != nil {
		return nil, config.Config{}, err
	}
	return store, cfg, nil
}
