package kilo

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// KnownGoodVersion is the kilo CLI release every wire shape in this package
// was live-probed against. Kilo is a fast-moving OpenCode fork, so a version
// drifting from this pin is worth a log line even though nothing refuses to
// run yet — a minimum-version gate only becomes real when session_tree needs
// one (plan PD2 / MADR Q7).
//
// Evidence for 7.5.6: internal/provider/kilo/testdata/wire/7.5.6/, a live turn
// captured from the SSE stream — 56 frames covering message.part.delta,
// message.part.updated, message.updated and 18 `sync` frames (MADR 0137 Phase
// 1). The 7.4.23 spike (MADR 0108, docs/kilo-spike-7.4.23/) remains as
// historical evidence for the previous pin.
const KnownGoodVersion = "7.5.6"

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
	if v != KnownGoodVersion {
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
