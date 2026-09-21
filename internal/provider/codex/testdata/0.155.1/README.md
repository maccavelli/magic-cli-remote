# Codex 0.155.1 contract evidence

The executable inventory of the app-server protocol, embedded into the daemon by
`contract.go`. An invalid manifest fails engine start, so this is load-bearing.

Captured 2026-09-21 with `./scripts/codex-contract.ps1` — see
[docs/ops-codex-contract.md](../../../../docs/ops-codex-contract.md) for the
procedure and the five things that will mislead you.

- Version: `codex-cli 0.155.1`
- Recorded digest:
  `c54db6755e710c39703f7c37512f9e35ed41042d8080558d2b84b8d2694323c3`
- Source-watch commit: `be2951ea3` (tag `rust-v0.155.1`)
- Stable: 102 client requests, 82 server notifications, 10 server requests
- Experimental: 164 client requests, 82 server notifications, 11 server requests

## Three things about these numbers

**The digest is the npm shim's, not the engine's.** `PATH` resolves codex to a
~341-byte `.cmd` on this platform; the engine behind it is 307 MB with the digest
`eba0f32c…`. npm regenerates that shim identically across releases, so the digest
cannot distinguish versions and `EvidenceMatched` is really a version comparison.
The capture logs a NOTE when it sees this.

**Both surfaces record 82 notifications, and 22 of those are experimental.**
Codex's exporter never prunes experimental notifications, so the two bundles are
identical there even though the runtime suppresses 22 of them. These files mirror
the exporter rather than correcting it — a manifest that disagrees with its own
source would break the drift gate's exact mode — so the capture reports the real
split (60 stable / 22 experimental, from scanning `#[experimental]` in the
source's `server_notification_definitions!` invocation) and records all 82.
Persisting the split as its own field is a `schema_version` 2 change and is
deferred. On the wire the totals are 62 / 84, because `export.rs` additionally
excludes `rawResponseItem/completed` and `rawResponse/completed` from the JSON.

**`installed_delta` is empty, by observation.** Both source surfaces are
decompressed from the committed `.zst` blobs at the same tag as the installed
binary, so an empty delta means they agree. It is not
empty because nothing was compared — which is what it would have meant before
PLAN 0163 P4.

## What each file is

- `manifest.json` — the surface inventory plus the capability allowlist, with a
  `classification` per method derived from this package's own code: the route
  table for notifications, a call-argument literal for client requests, a `case`
  label for server requests.
- `fixtures.json` — schema identities and required field names, with no live
  paths, prompts, account data or credentials.
- `source-watch-manifest.json` — leading evidence from the pinned source commit.
  It never enables a runtime capability.

The previous pin, 0.149.1, is gone; `../0.149.1/` retains only its `doctor`
fixture, which pins parsing behaviour rather than protocol surface.
