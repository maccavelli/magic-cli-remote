package relay

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/update"
)

func newUpdateCmd() *cobra.Command {
	var check, yes, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest GitHub release for mcrelay",
		Long: `Check GitHub releases for a newer mcrelay binary, verify SHA256SUMS,
and swap it into place (optionally restarting the user service). Exit codes
with --check: 0 = up to date, 10 = update available, 1 = error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			return update.Run(ctx, update.RunOpts{
				Product:      "mcrelay",
				LocalVersion: strings.TrimPrefix(cliVersion, "v"),
				Check:        check,
				Yes:          yes,
				Force:        force,
				Out:          cmd.OutOrStdout(),
				Err:          cmd.ErrOrStderr(),
				In:           cmd.InOrStdin(),
				Service: update.FuncService{
					IsActiveFn: service.IsActive,
					StopFn:     service.Stop,
					StartFn:    service.Start,
				},
				CodesignIdentity: strings.TrimSpace(os.Getenv("MC_CODESIGN_IDENTITY")),
			})
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report only; exit 0 up-to-date, 10 available, 1 error")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall the latest release even when not newer, and over a dev-suffixed local build")
	return cmd
}
