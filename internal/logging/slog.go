// Package logging configures process-wide structured logging via log/slog.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options control logger setup.
type Options struct {
	Level  string // debug|info|warn|error
	Format string // text|json
	// Out defaults to os.Stderr when nil.
	Out io.Writer
}

// Setup configures and installs the default slog logger.
func Setup(opts Options) *slog.Logger {
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}

	level := parseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(opts.Format) {
	case "json":
		handler = slog.NewJSONHandler(out, handlerOpts)
	default:
		handler = slog.NewTextHandler(out, handlerOpts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
