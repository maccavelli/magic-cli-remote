package main

import (
	"context"
	"errors"
	"os"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli"
	"github.com/maccavelli/magic-cli-remote/internal/updateclient"
)

// The build identity is go-selfupdate-lib's buildinfo, which the linker
// stamps and the commands read where they report it (MADR 0054 S2).
func main() {
	// First, before anything reads the environment: a detached update run
	// applies its private environment file, and a Windows handoff hop starts
	// the real run and exits here (MADR 0210 D2). 15 s bounds the real run's
	// wait for the hop, which the library caps at 10 s itself. In any other
	// run this does nothing.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := libservice.LoadHandOffEnvContext(ctx)
	cancel()
	if err != nil {
		_, _ = os.Stderr.WriteString("error: handoff environment: " + err.Error() + "\n")
		os.Exit(1)
	}
	err = cli.Execute()
	if err == nil {
		return
	}
	// The update command has reported its own outcome on its streams, and
	// carries its exit status: 10 when --check found an update, 1 otherwise
	// (MADR 0054 PLAN S3, Deviation 1).
	if update, ok := errors.AsType[*updateclient.ExitError](err); ok {
		os.Exit(update.Code)
	}
	// Cobra SilenceErrors is set; print the error for operators.
	_, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
	os.Exit(1)
}
