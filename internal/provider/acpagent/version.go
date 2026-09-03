package acpagent

import (
	"log/slog"

	acp "github.com/coder/acp-go-sdk"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// engineVersionOf returns the version an ACP agent reported at initialize.
//
// Two sources, in that order. `agentInfo.version` is the standard field and is
// what goose sends; the protocol types it as optional and says a future
// version will require it, so it is preferred wherever present. metaVersion is
// the transport's vendor fallback — grok's `_meta.agentVersion` — because grok
// 1.0.13 sends no agentInfo at all, verified across the whole of its 247-frame
// fixture (MADR 0137, ninth amendment).
//
// "" means the agent reported nothing, which is a valid answer and must never
// be turned into a warning: an agent that does not name itself has not
// disagreed with the pin.
func engineVersionOf(resp *acp.InitializeResponse, metaVersion string) string {
	if resp != nil && resp.AgentInfo != nil && resp.AgentInfo.Version != "" {
		return resp.AgentInfo.Version
	}
	return metaVersion
}

// reportEngineVersion records the engine version and logs one line per start:
// a warning when it differs from the Spec's pin, an info line when it matches,
// and nothing at all when either side is unknown.
//
// It never returns an error and never blocks a start. The pin says which
// release the wire shapes were checked against; drifting off it is a prompt to
// re-check the fixtures, not a fault (MADR 0137 Phase 3).
func (p *Provider) reportEngineVersion(resp *acp.InitializeResponse, metaVersion string) {
	v := engineVersionOf(resp, metaVersion)
	p.versionMu.Lock()
	p.engineVersion = v
	p.versionMu.Unlock()
	if v == "" || p.spec.KnownGoodVersion == "" {
		return
	}
	if provider.SameVersion(v, p.spec.KnownGoodVersion) {
		p.log.Info("engine version", slog.String("provider", string(p.spec.ID)),
			slog.String("version", v))
		return
	}
	p.log.Warn("engine version differs from known-good pin",
		slog.String("provider", string(p.spec.ID)),
		slog.String("version", v),
		slog.String("known_good", p.spec.KnownGoodVersion),
		slog.String("hint", "wire shapes were checked against the pinned release; "+
			"re-record testdata/wire fixtures after upgrades"),
	)
}

// EngineVersion returns the last version the agent reported at initialize, or
// "" when it reported none.
func (p *Provider) EngineVersion() string {
	p.versionMu.Lock()
	defer p.versionMu.Unlock()
	return p.engineVersion
}
