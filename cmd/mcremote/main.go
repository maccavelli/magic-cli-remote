package main

import (
	"errors"
	"os"

	"github.com/maccavelli/magic-cli-remote/internal/cli"
	"github.com/maccavelli/magic-cli-remote/internal/update"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.SetVersionInfo(version, commit, date)
	if err := cli.Execute(); err != nil {
		if errors.Is(err, update.ErrUpdateAvailable) {
			// mcremote update --check: scriptable exit 10 (0065 F5).
			os.Exit(10)
		}
		// Cobra SilenceErrors is set; print the error for operators.
		_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
		os.Exit(1)
	}
}
