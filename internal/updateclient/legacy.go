// Package updateclient adapts mcplib/selfupdate to mcremote and mcrelay.
//
// It is the only place this repository knows about GitHub releases, version
// comparison, download, verification, or executable replacement: that logic
// lives once in github.com/maccavelli/mcplib/selfupdate (MADR 0005). What
// stays here is what is genuinely product-specific — the stamped identity of
// an already-installed legacy binary, user-service lifecycle, unit
// reconciliation, and the optional macOS codesign transform.
package updateclient

import (
	"strconv"
	"strings"

	"github.com/maccavelli/mcplib/selfupdate"
)

// ReleaseBuildKind is the exact stamped value that marks a release build.
// Anything else is a local build. A bool cannot be set with the Go linker's
// -X flag, so the stamp is a string and only this one value counts.
const ReleaseBuildKind = "release"

// NormalizeInstalled converts a stamped mcremote/mcrelay identity into the
// strict SemVer tag the shared updater compares, plus its build kind.
//
// This is the bridge for binaries installed before MADR 0005. Magic Remote
// used to publish a four-field BASE.N build serial under a reused release tag
// (MADR 0103), and local builds appended .gHASH. Neither is SemVer, so neither
// can be handed to the shared strict version policy directly:
//
//	"0.15.3"             -> "v0.15.3", ReleaseBuild
//	"v0.15.3"            -> "v0.15.3", ReleaseBuild
//	"0.15.3.7"           -> "v0.15.3", ReleaseBuild   // legacy BASE.N
//	"0.15.3.7.gdeadbee"  -> "v0.15.3", LocalBuild     // legacy local build
//	"dev", "debug", ""   -> unchanged, LocalBuild
//	anything malformed   -> unchanged, LocalBuild
//
// Only the three-part base survives, because only the base was ever a real
// release identity. This normalizer is deliberately not exported from mcplib:
// no other product should inherit the BASE.N exception (MADR 0005 F3).
func NormalizeInstalled(rawVersion, rawBuildKind string) (string, selfupdate.BuildKind) {
	raw := strings.TrimSpace(rawVersion)
	base, ok := legacyBase(raw)
	if !ok {
		return raw, selfupdate.LocalBuild
	}
	if strings.TrimSpace(rawBuildKind) != ReleaseBuildKind {
		return base, selfupdate.LocalBuild
	}
	return base, selfupdate.ReleaseBuild
}

// legacyBase returns "vMAJOR.MINOR.PATCH" for any recognized stamped identity.
// The second result is false when the value cannot be ordered at all, which
// the caller must treat as a local build rather than guessing an ordering.
func legacyBase(raw string) (string, bool) {
	s := strings.TrimPrefix(raw, "v")
	if s == "" || s == "dev" || s == "debug" {
		return "", false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return "", false
	}
	for _, p := range parts[:3] {
		if !isCanonicalNumber(p) {
			return "", false
		}
	}
	return "v" + parts[0] + "." + parts[1] + "." + parts[2], true
}

// isCanonicalNumber accepts a non-empty decimal field without a leading zero,
// matching the numeric-identifier rule the shared strict policy enforces.
func isCanonicalNumber(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	_, err := strconv.Atoi(s)
	return err == nil
}
