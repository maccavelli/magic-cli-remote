---
status: accepted
date: 2026-11-01
decision-makers: Project Owner
consulted: "the installed grok 1.0.70, kilo 7.8.18, codex-cli 0.166.0 and opencode v2.2.3 on this host; their source trees under ~/gitrepos (grok-build at 1.0.45, kilocode tags v7.5.6 and v7.8.18, codex tags rust-v0.152.1 and rust-v0.166.0, opencode at v2.2.2 plus 30 commits); 0207-PLAN C3's live-grok run on 1.0.70; 0220-REPORT; scratch probes repin/kilo_api_diff.py, repin/kilo_used_ops.py, repin/kilo_component_detail.py"
informed: anyone working on a provider
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Re-pin grok, kilo and codex to the installed releases, and decide opencode's pin separately

## Context and Problem Statement

Each provider's `KnownGoodVersion` names the agent CLI release its wire shapes were checked against,
and a committed capture named after that release corroborates it. All four pins are behind the
binaries installed on this host. A version off its pin warns and never refuses, so nothing stops,
but every check since then has been against a release that is no longer what runs.

The owner asked that the pins be updated before work on the providers continues
([0208-PLAN-make-the-provider-layer-agent-neutral.md](0208-PLAN-make-the-provider-layer-agent-neutral.md)
P5).

### What is pinned and what is installed [observed 2026-11-01]

| Provider | `KnownGoodVersion` | Installed | Source tree |
| --- | --- | --- | --- |
| grok | 1.0.13 (`grok/version.go:16`) | 1.0.70 (`f50904eaee6c`) | `grok-build` at 1.0.45, synced 2026-09-29; no tags |
| kilo | 7.5.6 (`kilo/version.go:22`) | 7.8.18 | `kilocode`, tags `v7.5.6` and `v7.8.18` |
| codex | 0.152.1 (`codex/version.go:17`); contract manifest 0.155.1 (`contract.go:121-127`) | 0.166.0 | `codex`, tags `rust-v0.152.1` and `rust-v0.166.0` |
| opencode | 1.18.26 (`opencode/version.go:29`) | v2.2.3 | `opencode` at v2.2.2 plus 30 commits; no `v2.2.3` tag |

Each pin test reads the capture named by the pin: grok's `_meta.agentVersion` in the initialize
answer (`grok/version_test.go`), codex's `userAgent` and its token-usage frames
(`codex/version_test.go:50`, `tokenusage_test.go:23`), kilo's `sync` and usage frames
(`kilo/sync_test.go:123`, `usagedetail_test.go:101`) and a constant check
(`kilo/dialect_test.go:176`).

### What each release changes [observed]

* **F1 — grok 1.0.70 is already evidenced.** On 2026-10-28,
  [0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md)
  C3 ran `make live-grok` against this same build: exit 0, 105 top-level tests passed, none failed,
  one skipped (`TestLiveRawOutputSamples`). Its capture, committed as `grok/testdata/wire/1.0.70/`,
  holds the initialize answer with `"agentVersion":"1.0.70"`. The run took 596 s against the
  target's 600 s timeout.
* **F2 — grok's source cannot show the change.** `grok-build` stops at 1.0.45, 25 releases short
  of the binary. For grok, the live suite is the evidence; a source diff from 1.0.13 is not
  possible here.
* **F3 — kilo's surface grew, and shrank only where Magic does not go.** From
  `packages/sdk/openapi.json` at each tag: operations 294 to 311. The seven removed are
  `/interactive-terminal` (five) and `/kilo/claw/*` (two); Magic's kilo and `httpagent` code names
  none of them. Event types: the three `interactive_terminal.*` removed, unused by Magic; five added
  (`mcp.auth.url`, `session.drain.interrupted`, `session.drained`, `session.host.availability`,
  `session.wakeup`).
* **F4 — The operations Magic uses changed additively, with one new status.** Of the 28 operations
  whose paths Magic's code names, the changes are added properties (`Config`, `OAuth.baseURL`,
  `MCPStatusNeedsAuth.error`, `QuestionInfo.default`), a revert `reason` value `not-a-git-repo`,
  `GET /provider`'s new `failures` array, and a new `SessionStatus` variant
  `{"type":"scheduled","scheduledAt":…}`.
  * Kilo derives `scheduled` only for a session that is doing nothing now and has a future wakeup
    (`packages/opencode/src/kilocode/session/scheduled.ts:49-51`), and its own clients treat it as
    idle (`tool/board.ts:65`, `cli/cmd/run/stream.transport.ts:805`).
  * Magic's `handleSessionStatus` (`kilo/lifecycle.go:95-181`) handles `busy`, `retry` and `idle`
    and drops anything else, so a `scheduled` status ends nothing. The tree check treats an unknown
    status as idle (`lifecycle.go:269-272`), so this is not an outage, but the event path misses
    it.
* **F5 — codex's surface, 0.155.1 to 0.166.0** (the issue #31 assessment, from each release's
  `generate-json-schema`): client requests 102 to 106. `thread/rollback` is removed, and Magic does
  not send it; five are added. Server notifications 82 to 85, all added. Server requests are
  unchanged. Magic already runs against 0.166.0 for P5's codex step, from a capture of that
  release.
* **F6 — codex's two pins disagree.** `KnownGoodVersion` is 0.152.1 while the embedded contract
  manifest is 0.155.1. Re-pinning both to one release removes the split.
* **F7 — codex's re-pin is a procedure, and it needs a tree at the tag.**
  [docs/guides/ops-codex-contract.md](../guides/ops-codex-contract.md) runs
  `scripts/codex-contract.ps1 -Version -SourceCommit -SourceTree`, which refuses a source tree whose
  `HEAD` differs from the commit. The checkout here is at `b91cedc080`, past `rust-v0.166.0`. `pwsh`
  and Node 26 (the script needs Node 23.8 or later for zstd) are installed.
* **F8 — opencode v2 is a different engine, and Magic's provider does not work against it.**
  [0220-REPORT-opencode-v2-parity-assessment.md](../reports/0220-REPORT-opencode-v2-parity-assessment.md)
  probed v2.2.3: the 1.x HTTP API is gone (`POST /session?directory=` answers 405), yet the boot
  path is silent. `MinVersion` 1.18.0 passes, `/global/health` answers `200 text/html`, and the
  empty version skips the minimum check. Moving opencode to v2 is
  [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md),
  which is `proposed`. 0220 sizes it as rewriting the 1.x provider: about 6,755 production lines
  and 14,249 test lines.

## Decision Drivers

* **D-1 — A pin names what was checked.** A new pin needs a capture of that release, and the live
  suite that exercises it.
* **D-2 — Quota is the owner's.** Each live run is named, and waits for the owner's word.
* **D-3 — No change beyond what the new release requires.** Behaviour changes only where a release
  requires it (F4).
* **D-4 — A provider that cannot work fails loudly** rather than walking into a missing API
  (F8).

## Considered Options

* **A — Re-pin grok, kilo and codex now, one commit each; decide opencode separately (Q1).**
* **B — Re-pin all four now,** which for opencode means executing 0214 first.
* **C — Re-pin only the pins already evidenced (grok); leave the rest until each is touched.**

## Decision Outcome

Chosen option: "A", because grok, kilo and codex each re-pin with a bounded amount of evidence (F1,
F3 to F7), while opencode's pin cannot move without 0214's migration (F8). Bundling that migration
in would hold three small re-pins behind a large rewrite. C leaves P5 working against releases that
no longer run.

### The decisions

* **D1 — grok to 1.0.70.** `KnownGoodVersion = "1.0.70"`, with its comment naming the 1.0.70
  capture and 0207 C3's run (F1) as the evidence, and saying that the source tree is behind (F2).
  No new live run is required. Tests that read the 1.0.13 capture by name keep reading it.
* **D2 — kilo to 7.8.18.**
  * A new capture, `kilo/testdata/wire/7.8.18/`, of one live turn, with the frames the pin tests
    read (`sync`, usage).
  * `KnownGoodVersion = "7.8.18"`, and `TestKnownGoodVersionIs756` becomes a check for the new pin.
  * `handleSessionStatus` treats `scheduled` as `idle`, as kilo's own clients do (F4), with a test
    that fails on today's code.
  * The 7.5.6 capture stays as history, as the 7.4.x spikes do.
* **D3 — codex to 0.166.0.**
  * The contract is regenerated by the documented procedure (F7), from a scratch clone checked out
    at `rust-v0.166.0`. The owner's checkout is not touched.
  * The three `//go:embed` directives point at `testdata/0.166.0/`, `KnownGoodVersion` becomes
    0.166.0 (F6), and `testdata/0.155.1/` is deleted, as the procedure's step 5 says.
  * `codex/testdata/wire/0.166.0/` gains one live turn's frames, including the initialize answer
    and token usage. P5's two frames stay, and the P5 tests select their frames by shape.
  * `make live-codex-contract`, in both its normal and its exact mode, must pass.
* **D4 — opencode: the owner's choice (Q1).**
* **D5 — Order.** grok, then kilo, then codex, each its own commit, then opencode as Q1 says.
  0208-PLAN P5 continues after.

### Q1 — opencode

* **A — Hold the pin at 1.18.26, and refuse v2 loudly (recommended).** The provider reports
  not-ready, with a message naming the installed version, the pinned one and 0214, whenever the
  engine's major version is above the pin's, instead of failing on a 405. This is the one
  exception to "warn and never refuse", for the one case where nothing can work. 0214 stays its
  own piece of work.
* **B — Execute 0214 now:** accept it, write its PLAN from 0220's breakdown, and do it before P5
  continues.
* **C — Leave opencode as it is:** the pin stays at 1.18.26, and the silent failure stays.

### Consequences

* Good, because every pin then names a release that runs here, with a capture of it.
* Good, because codex's two pins agree (F6).
* Good, because a kilo session that schedules a wakeup is seen as idle (F4).
* Neutral, because grok's re-pin rests on 0207's run rather than on a new one (F1).
* Bad, because kilo's and codex's captures, and their live suites, spend quota (D-2).
* Bad, under Q1 option A, because a v2 opencode is refused until 0214 lands.

### Confirmation

* Each provider's pin test passes against its new capture: grok's against 1.0.70, codex's
  `userAgent` against 0.166.0, kilo's `sync` and usage frames against 7.8.18.
* `make live-kilo`, `make live-codex-contract` (both modes) and `make live-codex` pass, each on the
  owner's word.
* The `scheduled` test fails on today's code and passes after the change.
* `go test -race ./...`, `make vet-tags`, `make lint`, `make staticcheck`; the guard's preview on
  each capture.

## Pros and Cons of the Options

### A — Re-pin three now, opencode by Q1

* Good, because each re-pin is small and has its evidence.
* Bad, because opencode stays behind its binary until Q1 is acted on.

### B — All four, with 0214

* Good, because every pin moves.
* Bad, because 0214 is a provider rewrite (F8), and it holds the other three behind it.

### C — grok only

* Good, because it needs no quota.
* Bad, because P5's kilo and codex steps would check against releases that no longer run.

## More Information

### The owner's answers

*Answered by the owner, 2026-11-01:* Q1 "Do 0214 now" (option B), and the record and its PLAN
approved with "all live runs" (C2). Under option B, D4 is: accept
[0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md),
write its PLAN from 0220's breakdown, and present it on its own before any opencode code changes.
The opencode pin moves under that PLAN, not this one. 0208-PLAN P5 waits for it.

### Note, 2026-11-01: 0217 D10 was not cited

[0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)
D10, accepted on 2026-10-29, says the engine pins are "each its own decision, with its own live
run", on the owner's answer "Each its own record". This record moved grok, kilo and codex under one
record, though each in its own phase, commit and live run. It was written and approved without
citing D10; that was found after R3, while R4 was being prepared. *The owner, 2026-11-01: "Keep
0226 as is".* Each pin kept its own phase, commit and live run.

### Related records

* [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md):
  F11 listed these same four pins as behind, and D10 left each to its own record (see the note
  above).
* [0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md](0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md):
  the codex re-pin procedure this follows (D3). Its PLAN is still `in-progress`.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
  and [0220-REPORT-opencode-v2-parity-assessment.md](../reports/0220-REPORT-opencode-v2-parity-assessment.md):
  opencode v2 (Q1).
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md):
  where the pin-and-capture rule began.
* GitHub issue #31: its first recommendation is codex's re-pin (D3).
