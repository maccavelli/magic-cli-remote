package acpagent

import (
	"fmt"
	"log/slog"

	acp "github.com/maccavelli/acp-go-sdk2"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// engineVersionOf returns the version an ACP agent reported at initialize.
//
// Two sources, in that order. `agentInfo.version` is the standard field; the protocol types it as optional and says a future
// version will require it, so it is preferred wherever present. metaVersion is
// the Spec's InitializeMeta reading of the agent's `_meta`, for an agent that
// sends no agentInfo at all (MADR 0137, ninth amendment).
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

// checkMinVersion refuses an engine below the Spec's MinVersion, or one that
// named no version when the Spec has a floor, since an engine of unknown version
// is how an incompatible one gets half-driven (0214 MADR D3, D12). A prerelease
// of the floor is below it (provider.CompareVersions).
func (p *Provider) checkMinVersion(v string) error {
	floor := p.spec.MinVersion
	if floor == "" {
		return nil
	}
	if v == "" {
		return fmt.Errorf("%s reported no version at initialize; this provider needs %s or later (%s)",
			p.spec.ID, floor, p.spec.UpgradeURL)
	}
	if provider.CompareVersions(v, floor) < 0 {
		return fmt.Errorf("%s engine version %s is below the minimum %s; upgrade: %s",
			p.spec.ID, v, floor, p.spec.UpgradeURL)
	}
	return nil
}

// EngineVersion returns the last version the agent reported at initialize, or
// "" when it reported none.
func (p *Provider) EngineVersion() string {
	p.versionMu.Lock()
	defer p.versionMu.Unlock()
	return p.engineVersion
}
