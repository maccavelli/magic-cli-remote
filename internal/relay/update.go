package relay

import (
	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/updateclient"
)

// newUpdateCmd builds `mcrelay update`. Both products share one command body
// and one adapter; only the stamped identity differs (MADR 0005).
func newUpdateCmd() *cobra.Command {
	return updateclient.NewCommand("mcrelay", func() (string, string) {
		return cliVersion, cliBuildKind
	})
}
