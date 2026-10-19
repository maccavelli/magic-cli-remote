package main

import (
	"errors"
	"os"

	"github.com/maccavelli/magic-cli-remote/internal/cli"
	"github.com/maccavelli/magic-cli-remote/internal/updateclient"
)

// The build identity is go-selfupdate-lib's buildinfo, which the linker
// stamps and the commands read where they report it (MADR 0054 S2).
func main() {
	err := cli.Execute()
	if err == nil {
		return
	}
	// The update command has reported its own outcome on its streams, and
	// carries its exit status: 10 when --check found an update, 1 otherwise
	// (MADR 0054 PLAN S3, Deviation 1).
	var update *updateclient.ExitError
	if errors.As(err, &update) {
		os.Exit(update.Code)
	}
	// Cobra SilenceErrors is set; print the error for operators.
	_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
	os.Exit(1)
}
