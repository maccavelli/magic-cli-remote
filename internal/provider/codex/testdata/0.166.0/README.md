# Codex 0.166.0 contract evidence

The executable inventory of the app-server protocol, embedded into the daemon by
`contract.go`. An invalid manifest fails engine start, so this is load-bearing.

Captured 2026-11-01 on macOS with `./scripts/codex-contract.ps1`, and regenerated at
`schema_version` 2 on 2026-11-07
([0236-PLAN-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md](../../../../../docs/decisions/0236-PLAN-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md)
P1). The regeneration changed nothing but the new fields. See
[docs/guides/ops-codex-contract.md](../../../../../docs/guides/ops-codex-contract.md) for the
procedure and the things that will mislead you. Decision:
[0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](../../../../../docs/decisions/0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)
D3.

- Version: `codex-cli 0.166.0`
- Recorded digest:
  `e14fc83739c13c1074b12bd1acce4010c893c7f6fbfc1593b4a31d74cabb57af`
- Source-watch commit: `6ff1e1ca3b30fc8b914720681fcf5fe62e95f856` (tag `rust-v0.166.0`), from a
  scratch clone checked out at the tag
- Stable: 106 client requests, 85 server notifications, 10 server requests
- Experimental: 179 client requests, 85 server notifications, 11 server requests

Against 0.155.1's 102/82/10: `thread/rollback` is gone (Magic never sent it), five client
requests are new (`account/gatewayOAuth/{cancel,login,read}`, `feedback/save`,
`thread/attachmentOwner/list`), and three notifications are new
(`account/gatewayOAuth/changed`, `thread/prediction/updated`, `thread/readState/changed`), which
`routing.go` declares and drops.

## Three things about these numbers

**The digest is the npm launcher's, not the engine's.** On this macOS host `PATH` resolves codex
to `/opt/homebrew/bin/codex`, which the generator follows to the npm package's
`bin/codex.js`. That launcher is what gets hashed, as on Windows the ~341-byte `.cmd` shim was
for 0.155.1. Version equality is the real evidence; the digest is a tripwire against an
accidental re-capture.

**Both surfaces record 85 notifications, and 24 of those are experimental.** Codex's exporter
never prunes experimental notifications, so the two bundles are identical there even though the
runtime suppresses them. The surfaces mirror the exporter rather than correcting it, and record
all 85. The real split (61 stable / 24 experimental, from scanning `#[experimental]` in the
source) is kept beside them in `manifest.json`'s `experimental_notifications`.

**`installed_delta` is empty, by observation.** Both source surfaces are decompressed from the
committed `.zst` blobs at the same tag as the installed binary, so an empty delta means they
agree.

## What each file is

- `manifest.json` — the surface inventory plus the capability allowlist, with a
  `classification` per method derived from this package's own code. Each surface carries its
  string enums (521 stable, 611 experimental). The manifest also carries the notification split
  and the export's 1,055 property names, which `decoded_names_test.go` checks this package's JSON
  tags against.
- `fixtures.json` — schema identities and required field names, with no live paths, prompts,
  account data or credentials. Every response type is read from the `response:` declarations in
  `codex-rs/app-server-protocol/src/protocol/common.rs` at the tag. This file is byte-identical
  to the one the earlier name-guessing and override table produced.
- `source-watch-manifest.json` — leading evidence from the pinned source commit. It never
  enables a runtime capability.

The previous pin, 0.155.1, is gone. `../0.149.1/` and `../0.147.0/` retain only fixtures that pin
parsing behaviour rather than protocol surface.
