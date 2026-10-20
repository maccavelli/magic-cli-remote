package cli

import (
	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/updateclient"
)

// newUpdateCmd builds `mcremote update` (MADR 0065, canonicalized by 0005).
// The command body is shared with mcrelay; each reports its own build
// identity (MADR 0054 S2), and its own health wiring (D5).
func newUpdateCmd() *cobra.Command {
	return updateclient.NewCommand("mcremote", updateHealth())
}
