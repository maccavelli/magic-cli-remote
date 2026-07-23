package main

import (
	"os"

	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := relay.Execute(version, commit, date); err != nil {
		_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
		os.Exit(1)
	}
}
