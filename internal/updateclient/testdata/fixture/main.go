// Command fixture is the release binary internal/updateclient's tests serve.
// The update's guards refuse a release binary that is not an executable image
// for the host, or that does not run and report the tag being installed
// (MADR 0207 D16), so the tests need a real program, and this is the smallest
// one that answers as `mcremote version` does.
//
// Run with the single argument "version", it prints the identity line of a
// release build of v0.2.0, or of MCREMOTE_UPDATECLIENT_FIXTURE_VERSION when
// that is set, and exits 0, or with MCREMOTE_UPDATECLIENT_FIXTURE_EXIT when
// that is set. Run any other way, or with an exit status that is not a
// number, it exits 2, so a mistaken test fails loudly.
//
// When its own file is named MCREMOTE_UPDATECLIENT_FIXTURE_FAIL_NAMED, it
// prints nothing and exits 1. The update stages a release under another name
// and installs it under the product's, so a test can make it fail only once
// installed: the post-install probe's case (MADR 0207 D34).
//
// It is a module of its own and imports only the standard library, so the
// tests build it the same way whatever flags they run under.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

const (
	versionEnv   = "MCREMOTE_UPDATECLIENT_FIXTURE_VERSION"
	exitEnv      = "MCREMOTE_UPDATECLIENT_FIXTURE_EXIT"
	failNamedEnv = "MCREMOTE_UPDATECLIENT_FIXTURE_FAIL_NAMED"
	defaultTag   = "v0.2.0"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the program, with its arguments and streams as parameters.
func run(args []string, stdout, stderr io.Writer) int {
	if !slices.Equal(args, []string{"version"}) {
		fmt.Fprintf(stderr, "fixture: want the single argument version, got %q\n", args)
		return 2
	}
	if name, ok := os.LookupEnv(failNamedEnv); ok {
		self, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "fixture: %v\n", err)
			return 2
		}
		if filepath.Base(self) == name {
			fmt.Fprintf(stderr, "fixture: refusing to run as %s\n", name)
			return 1
		}
	}
	tag := defaultTag
	if v, ok := os.LookupEnv(versionEnv); ok {
		tag = v
	}
	// buildinfo.Info.String() for a release with no revision.
	fmt.Fprintf(stdout, "%s (release)\n", tag)
	raw, ok := os.LookupEnv(exitEnv)
	if !ok {
		return 0
	}
	code, err := strconv.Atoi(raw)
	if err != nil {
		fmt.Fprintf(stderr, "fixture: %s=%q is not an exit status\n", exitEnv, raw)
		return 2
	}
	return code
}
