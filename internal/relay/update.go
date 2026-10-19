package relay

import (
	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/updateclient"
)

// newUpdateCmd builds `mcrelay update`. Both products share one command body
// and one adapter, and each reports its own build identity (MADR 0005, 0054
// S2).
func newUpdateCmd() *cobra.Command {
	return updateclient.NewCommand("mcrelay")
}
