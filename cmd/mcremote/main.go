package main

import (
	"os"

	"github.com/maccavelli/mcplib/selfupdate"

	"github.com/maccavelli/magic-cli-remote/internal/cli"
)

// Set via -ldflags at build time. buildKind is "release" only for a tag build.
var (
	version   = "dev"
	commit    = "none"
	date      = "unknown"
	buildKind = "local"
)

func main() {
	cli.SetVersionInfo(version, commit, date)
	cli.SetBuildKind(buildKind)
	err := cli.Execute()
	if err == nil {
		return
	}
	// The library never exits the process; this main owns exit mapping.
	// selfupdate.ExitCode returns 10 for `update --check` finding an
	// actionable target and 1 for every other failure (MADR 0005).
	code := selfupdate.ExitCode(selfupdate.Result{}, err)
	if code != 10 {
		// Cobra SilenceErrors is set; print the error for operators.
		_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
	}
	os.Exit(code)
}
