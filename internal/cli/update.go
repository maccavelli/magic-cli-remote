package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/update"
)

// newUpdateCmd builds `mcremote update` (MADR 0065).
func newUpdateCmd() *cobra.Command {
	return newProductUpdateCmd("mcremote", func() string {
		return strings.TrimPrefix(version, "v")
	})
}

func newProductUpdateCmd(product string, localVersion func() string) *cobra.Command {
	var check, yes, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest GitHub release for " + product,
		Long: `Check GitHub releases for a newer ` + product + ` binary, verify SHA256SUMS,
and swap it into place (optionally restarting the user service).

Exit codes with --check: 0 = up to date, 10 = update available, 1 = error.
Set GITHUB_TOKEN to raise API rate limits. Set MC_CODESIGN_IDENTITY on macOS
to re-sign the staged binary before swap (preserves TCC grants; MADR 0069).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			err := update.Run(ctx, update.RunOpts{
				Product:      product,
				LocalVersion: localVersion(),
				Check:        check,
				Yes:          yes,
				Force:        force,
				Out:          cmd.OutOrStdout(),
				Err:          cmd.ErrOrStderr(),
				In:           cmd.InOrStdin(),
				Service: update.FuncService{
					IsActiveFn:    service.IsActive,
					StopFn:        service.Stop,
					StartFn:       service.Start,
					IsInstalledFn: service.IsInstalled,
				},
				// Runs `<new binary> setup-service --refresh` after the swap:
				// the definition a release ships can only be rendered by the
				// binary that ships it (MADR 0100).
				Refresher:        service.ExecRefresher{},
				CodesignIdentity: strings.TrimSpace(os.Getenv("MC_CODESIGN_IDENTITY")),
			})
			if errors.Is(err, update.ErrUpdateAvailable) {
				return err // main maps to exit 10
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report only; exit 0 up-to-date, 10 available, 1 error")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall the latest release even when not newer, and over a dev-suffixed local build")
	return cmd
}
