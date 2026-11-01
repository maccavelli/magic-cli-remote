package kilo

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// KnownGoodVersion is the kilo CLI release every wire shape in this package
// was live-probed against. Kilo is a fast-moving OpenCode fork, so a version
// drifting from this pin is worth a log line even though nothing refuses to
// run yet — a minimum-version gate only becomes real when session_tree needs
// one (plan PD2 / MADR Q7).
//
// Evidence for 7.8.18: internal/provider/kilo/testdata/wire/7.8.18/, every
// frame of one live turn from the /global/event stream — 108 frames, among
// them 59 message.part.delta, 18 `sync` and the new session.turn.open/close
// (MADR 0226 D2). Its HTTP surface was compared with 7.5.6's from the tagged
// sources: what was removed Magic never called, and the one new state Magic
// must read, `scheduled`, is handled in lifecycle.go (MADR 0226 F3, F4). The
// 7.5.6 capture (MADR 0137 Phase 1) and the 7.4.x spikes (MADR 0108) remain as
// history.
const KnownGoodVersion = "7.8.18"

// OnHealthy implements [httpagent.HealthyHook]: records the engine version
// from GET /global/health ({"healthy":true,"version":"7.4.23"} on the 0108
// probe host) for doctor output and future gating.
func (d *httpDialect) OnHealthy(body []byte) error {
	var h struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &h); err != nil {
		// Non-JSON health is unexpected but not fatal: keep boot path open.
		d.log.Debug("kilo health body not JSON", slog.String("err", err.Error()))
		return nil
	}
	v := strings.TrimSpace(h.Version)
	d.mu.Lock()
	d.engineVersion = v
	d.mu.Unlock()
	if v == "" {
		return nil
	}
	// Full SemVer, as every pin compares (MADR 0186 D8; 0207 D5, Q1): "v7.5.6"
	// and "7.5.6+build" are the pinned release, and "7.5.6-rc.1" is not.
	if !provider.SameVersion(v, KnownGoodVersion) {
		d.log.Warn("kilo engine version differs from known-good pin",
			slog.String("version", v),
			slog.String("known_good", KnownGoodVersion),
			slog.String("hint", "wire shapes were live-probed on the pinned version; re-run live_kilo tests after upgrades"),
		)
	} else {
		d.log.Info("kilo engine version", slog.String("version", v))
	}
	return nil
}

// EngineVersion returns the last health-reported version, or "".
func (d *httpDialect) EngineVersion() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.engineVersion
}
