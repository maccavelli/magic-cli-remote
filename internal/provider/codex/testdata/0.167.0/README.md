# Codex 0.167.0 contract evidence

The executable inventory of the app-server protocol, embedded into the daemon by
`contract.go`. An invalid manifest fails engine start, so this is load-bearing.

Captured 2026-11-07 on macOS with `./scripts/codex-contract.ps1`, at `schema_version` 2 — see
[docs/guides/ops-codex-contract.md](../../../../../docs/guides/ops-codex-contract.md) for the
procedure and the things that will mislead you. Decision:
[0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md](../../../../../docs/decisions/0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md)
D1.

- Version: `codex-cli 0.167.0`
- Recorded digest:
  `e14fc83739c13c1074b12bd1acce4010c893c7f6fbfc1593b4a31d74cabb57af`
- Source-watch commit: `086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a` (tag `rust-v0.167.0`), from a
  scratch clone checked out at the tag
- Stable: 106 client requests, 86 server notifications, 10 server requests
- Experimental: 180 client requests, 86 server notifications, 11 server requests

Against 0.166.0's 106/85/10 and 179/85/11, nothing is gone:

- One stable notification is new, `thread/section/changed`, which `routing.go` declares and drops
  (0236 D2).
- One experimental client request is new, `thread/guardianReview`, which Magic does not send.
- `CommandExecutionApprovalKind` gains `humanCommand`. Codex sends it only to a client that
  declared `requiredHumanApproval`, which Magic does not (0236 D5).

## Three things about these numbers

**The digest is the npm launcher's, not the engine's, and it did not change.** On this macOS host
`PATH` resolves codex to `/opt/homebrew/bin/codex`, which the generator follows to the npm
package's `bin/codex.js`. That launcher is what gets hashed. Its bytes are the same in 0.166.0 and
0.167.0, so the digest cannot tell those releases apart: version equality is the only evidence,
and the digest is a tripwire against a capture from somewhere else.

**Both surfaces record 86 notifications, and 24 of those are experimental.** Codex's exporter
never prunes experimental notifications, so the two bundles are identical there even though the
runtime suppresses them. The surfaces mirror the exporter rather than correcting it, and record
all 86. The real split (62 stable / 24 experimental, from scanning `#[experimental]` in the
source) is kept beside them in `manifest.json`'s `experimental_notifications`.

**`installed_delta` is empty, by observation.** Both source surfaces are decompressed from the
committed `.zst` blobs at the same tag as the installed binary, so an empty delta means they
agree.

## What each file is

- `manifest.json` — the surface inventory plus the capability allowlist, with a
  `classification` per method derived from this package's own code. Each surface carries its
  string enums (522 stable, 614 experimental). The manifest also carries the notification split
  and the export's 1,059 property names, which `decoded_names_test.go` checks this package's JSON
  tags against.
- `fixtures.json` — schema identities and required field names, with no live paths, prompts,
  account data or credentials. Every response type is read from the `response:` declarations in
  `codex-rs/app-server-protocol/src/protocol/common.rs` at the tag.
- `source-watch-manifest.json` — leading evidence from the pinned source commit. It never
  enables a runtime capability.

The previous pin, 0.166.0, is gone. `../0.149.1/` and `../0.147.0/` retain only fixtures that pin
parsing behaviour rather than protocol surface.
