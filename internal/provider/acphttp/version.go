package acphttp

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// reportEngineVersion logs one line per engine start: a warning when the
// version the agent reported differs from the Spec's pin, an info line when it
// matches, and nothing when either side is unknown.
//
// A mismatch WARNS AND NEVER REFUSES. The pin records which release the wire
// shapes were checked against, so drifting off it is a prompt to re-record the
// fixtures, not a reason to fail a start (MADR 0137 Phase 3).
//
// An empty version is not a disagreement. `agentInfo` is optional in ACP — grok
// omits it entirely — so an agent that does not name itself must produce no
// warning at all (MADR 0137, ninth amendment).
func (p *Provider) reportEngineVersion(v string) {
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
