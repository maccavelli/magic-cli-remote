package main

import (
	"errors"
	"os"

	"github.com/maccavelli/magic-cli-remote/internal/relay"
	"github.com/maccavelli/magic-cli-remote/internal/update"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := relay.Execute(version, commit, date); err != nil {
		if errors.Is(err, update.ErrUpdateAvailable) {
			os.Exit(10)
		}
		_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
		os.Exit(1)
	}
}
