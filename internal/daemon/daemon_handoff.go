package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// handOffCleanupTimeout bounds the start-up cleanup's launchctl calls.
const handOffCleanupTimeout = 10 * time.Second

// handOffCleaner is a backend that clears its finished handoff jobs:
// launchd's, which loads a one-shot job for each handoff and leaves it loaded
// once it has run (go-selfupdate-lib 0011-MADR amendment A3).
type handOffCleaner interface {
	CleanupHandOffs(ctx context.Context) error
}

// newHandOffBackend is this product's service backend, as the update
// command's handoff builds it.
func newHandOffBackend() (any, error) {
	return service.NewBackend("mcremote", "")
}

// cleanupHandOffs clears the finished handoff jobs an agent-run update left,
// once, at start (MADR 0210 D5). Only launchd leaves them, so it builds no
// backend elsewhere. A failure is logged: the daemon serves either way.
func cleanupHandOffs(ctx context.Context, goos string, newBackend func() (any, error), log *slog.Logger) {
	if goos != "darwin" {
		return
	}
	b, err := newBackend()
	if err != nil {
		log.Warn("self-update handoff cleanup: no service backend", slog.String("err", err.Error()))
		return
	}
	c, ok := b.(handOffCleaner)
	if !ok {
		return
	}
	if err := c.CleanupHandOffs(ctx); err != nil {
		log.Warn("self-update handoff cleanup failed", slog.String("err", err.Error()))
	}
}
