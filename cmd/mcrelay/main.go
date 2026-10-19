package main

import (
	"os"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"

	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// The build identity is go-selfupdate-lib's buildinfo, which the linker
// stamps and the commands read where they report it (MADR 0054 S2).
func main() {
	err := relay.Execute()
	if err == nil {
		return
	}
	code := selfupdate.ExitCode(selfupdate.Result{}, err)
	if code != 10 {
		_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
	}
	os.Exit(code)
}
