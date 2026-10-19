package main

import (
	"os"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"

	"github.com/maccavelli/magic-cli-remote/internal/cli"
)

// The build identity is go-selfupdate-lib's buildinfo, which the linker
// stamps and the commands read where they report it (MADR 0054 S2).
func main() {
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
