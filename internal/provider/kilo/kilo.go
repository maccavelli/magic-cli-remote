// Package kilo implements the Kilo CLI provider as an httpagent dialect
// (MADR 0075): one daemon-owned shared `kilo serve` engine drives every
// session over HTTP + SSE, mirroring the OpenCode integration it was forked
// from. Kilo is an OpenCode fork, so the wire shapes match closely (same
// GlobalEvent SSE envelope, same /global/health and /global/event paths —
// live-proven on kilo 7.4.20, docs/kilo-spike-7.4.20/), but it stays a
// distinct dialect: config homes, model-id aliases, agents, and Kilo-only
// event types differ, and sharing one flavored dialect would rot as the fork
// drifts (MADR 0075 alternative G).
package kilo

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// Config configures the Kilo provider (shared HTTP agent config shape, same
// alias pattern as the OpenCode dialect).
type Config = httpagent.Config

// NewHTTP creates the Kilo HTTP-transport provider.
func NewHTTP(cfg Config) *httpagent.Provider {
	return NewHTTPWithLogger(cfg, nil)
}

// NewHTTPWithLogger is like NewHTTP but sets a logger.
func NewHTTPWithLogger(cfg Config, log *slog.Logger) *httpagent.Provider {
	return NewHTTPWithToolFrameHook(cfg, log, nil)
}

// RawToolPartFrame captures a message.part.updated tool frame for live
// probing (MADR 0034 Phase 0 parity; MADR 0076 L2).
type RawToolPartFrame struct {
	CallID string
	PartID string
	Tool   string
	Status string
	Title  string
	Input  string
	Output string
	Error  string
}

// NewHTTPWithToolFrameHook creates a Provider with a callback for raw tool
// frames (MADR 0034 Phase 0 parity; MADR 0076 L2 — dropped during the
// opencode fork, restored here so a live_kilo tool-stream test is possible).
func NewHTTPWithToolFrameHook(cfg Config, log *slog.Logger, hook func(RawToolPartFrame)) *httpagent.Provider {
	l := slog.Default()
	if log != nil {
		l = log
	}
	d := &httpDialect{
		log:  l,
		pure: cfg.Pure,
		// Seed the prompt fallback with the Gateway free auto-router (plan
		// PD4); P3's AfterBoot replaces it with the engine's own default.
		defaultModelProvider: "kilo",
		defaultModelID:       defaultModelID,
		onToolPartUpdated:    hook,
	}
	return httpagent.NewWithLogger(d, cfg, log)
}
