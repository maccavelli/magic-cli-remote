package cli

import (
	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/updateclient"
)

// newUpdateCmd builds `mcremote update` (MADR 0065, canonicalized by 0005).
// The command body is shared with mcrelay; only the stamped identity differs.
func newUpdateCmd() *cobra.Command {
	return updateclient.NewCommand("mcremote", func() (string, string) {
		return version, buildKind
	})
}
