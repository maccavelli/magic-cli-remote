//go:build !debugpprof

// Package debugserve exposes pprof endpoints in debug builds; this is the
// release twin, where Start compiles to nothing so the profiling surface
// does not exist in the binary at all (MADR 0068 P6).
package debugserve

import (
	"context"
	"log/slog"
)

// Start is a no-op in release builds (no debugpprof build tag).
func Start(context.Context, *slog.Logger) {}
