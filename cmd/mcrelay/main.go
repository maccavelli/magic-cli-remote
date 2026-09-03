package main

import (
	"os"

	"github.com/maccavelli/mcplib/selfupdate"

	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// Set via -ldflags at build time. buildKind is "release" only for a tag build.
var (
	version   = "dev"
	commit    = "none"
	date      = "unknown"
	buildKind = "local"
)

func main() {
	relay.SetBuildKind(buildKind)
	err := relay.Execute(version, commit, date)
	if err == nil {
		return
	}
	code := selfupdate.ExitCode(selfupdate.Result{}, err)
	if code != 10 {
		_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
	}
	os.Exit(code)
}
