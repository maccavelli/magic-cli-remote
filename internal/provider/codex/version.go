package codex

import (
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// KnownGoodVersion is the codex CLI release this provider's wire shapes were
// checked against: internal/provider/codex/testdata/wire/0.152.1/, one live
// `hi` turn over the app-server transport (MADR 0137 Phase 1).
//
// A drifting version WARNS AND NEVER REFUSES. Codex ships often, and a version
// mismatch is a prompt to re-check the fixtures, not a reason to refuse to
// start. Do not harden this into a gate without a decision record saying why.
const KnownGoodVersion = "0.152.1"

// versionFromUserAgent extracts codex's own version from the `userAgent` string
// in its `initialize` result.
//
// Codex composes it as `<originator>/<CARGO_PKG_VERSION> (<os>; <arch>) …`
// (codex-rs/login/src/auth/default_client.rs:164-170), so the version is the
// token between the first "/" and the following space. The originator varies —
// it is the client name mcremote sends — so only the shape after it is relied
// on.
//
// Returns "" for anything that does not match, which the caller must treat as
// "the engine reported no version" and warn about nothing. A pin that guesses
// is worse than a pin that stays quiet (MADR 0137, ninth amendment).
func versionFromUserAgent(ua string) string {
	ua = strings.TrimSpace(ua)
	slash := strings.Index(ua, "/")
	if slash < 0 {
		return ""
	}
	rest := ua[slash+1:]
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	// Only a plausible version is returned: the userAgent's first token is not
	// guaranteed to be one, and "codex-test-agent" (no slash) or a path-like
	// value must not be reported as a release.
	if _, _, _, ok := provider.ParseSemver(rest); !ok {
		return ""
	}
	return rest
}

// reportEngineVersion logs one line per engine start: a warning when the
// version codex reported differs from the pin, an info line when it matches,
// and nothing when the userAgent carried no readable version.
//
// A mismatch WARNS AND NEVER REFUSES. Codex ships often; drifting off the pin
// is a prompt to re-record the fixtures, not a reason to fail a start
// (MADR 0137 Phase 3).
func (p *Provider) reportEngineVersion(userAgent string) {
	v := versionFromUserAgent(userAgent)
	p.versionMu.Lock()
	p.engineVersion = v
	p.versionMu.Unlock()
	if v == "" {
		return
	}
	if provider.SameVersion(v, KnownGoodVersion) {
		p.log.Info("codex engine version", slog.String("version", v))
		return
	}
	p.log.Warn("codex engine version differs from known-good pin",
		slog.String("version", v),
		slog.String("known_good", KnownGoodVersion),
		slog.String("hint", "wire shapes were checked against the pinned release; "+
			"re-record internal/provider/codex/testdata/wire after upgrades"),
	)
}

// EngineVersion returns the version read from the last initialize response, or
// "" when none was readable.
func (p *Provider) EngineVersion() string {
	p.versionMu.Lock()
	defer p.versionMu.Unlock()
	return p.engineVersion
}
