---
status: in-progress
date: 2026-11-01
associated-madr: "0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement re-pinning the agent CLIs to the installed releases

Associated MADR: [0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)

## Goal

grok, kilo and codex each pin the release installed on this host, with a capture of that release
and a passing pin test; codex's contract manifest and its `KnownGoodVersion` name the same
release; a kilo `scheduled` status is seen as idle; and opencode is handled as the owner answers
the MADR's Q1.

## Scope

* **R1, grok (D1):** `internal/provider/grok/version.go`.
* **R2, kilo (D2):** `internal/provider/kilo/version.go`, `internal/provider/kilo/lifecycle.go`,
  `internal/provider/kilo/dialect_test.go`, a test for `scheduled`, and
  `internal/provider/kilo/testdata/wire/7.8.18/` (new).
* **R3, codex (D3):** `internal/provider/codex/contract.go` (three embed paths),
  `internal/provider/codex/version.go`, `internal/provider/codex/testdata/0.166.0/` (generated,
  new), `internal/provider/codex/testdata/0.155.1/` (deleted),
  `internal/provider/codex/testdata/wire/0.166.0/frames.jsonl` and `meta.json`,
  `internal/provider/codex/runtime_passthrough_test.go` (frame selection by shape), and any comment
  or test that names the 0.155.1 manifest's path.
* **R4, opencode (D4):** set by Q1. Under option A: `internal/provider/opencode/version.go`,
  `internal/provider/opencode/http.go` (where the boot path reads health and the version), and
  their tests.
* **Records:** this pair, and `docs/README.md`'s index rows for it.
* **Not in scope:** 0214's migration (unless Q1 is B, which takes its own PLAN); issue #31's other
  workstreams; changing any provider's behaviour beyond F4's `scheduled`; the source trees under
  `~/gitrepos`, which are read and never written.

## Constraints

* **C1 — One commit per phase.** The agent stages; the owner commits with `git commit --no-edit`.
* **C2 — Live runs only on the owner's word,** each named below: R2's capture turn and
  `make live-kilo`; R3's capture turn, `make live-codex-contract` (both modes) and
  `make live-codex`. R1 needs none.
* **C3 — Every new test is seen failing first,** on a scratch copy, never in the tree.
* **C4 — Captures are read before they are committed.** Values are judged by shape first,
  identifiers become placeholders, and the guard's redact preview runs on the tree before staging.
  Captures go to a scratch directory, never the tree.
* **C5 — No push without an explicit ask in the same turn.**
* **C6 — The installed versions are checked at each phase's start.** A binary that moved since this
  record is a stop.

## Implementation Steps

### R1 — grok to 1.0.70

1. `grok --version` must still print `1.0.70 (f50904eaee6c)`.
2. `KnownGoodVersion = "1.0.70"`. The comment names `testdata/wire/1.0.70/` (selected frames,
   initialize answer included), 0207-PLAN C3's `make live-grok` (105 passed), and that `grok-build`
   is at 1.0.45 (MADR F2).
3. Checks:
   * `TestPinIsCorroboratedByItsFixture` passes against 1.0.70. Seen failing on a scratch copy with
     the constant set to a release that has no capture.
   * `go test -race ./internal/provider/grok/ ./internal/provider/acpagent/`; `make pre-add-check`;
     `make vet-tags`.

### R2 — kilo to 7.8.18

1. `kilo --version` must still print `7.8.18`.
2. **The capture (live, C2):** a scratch-only test in a scratch copy of the tree runs one short kilo
   turn with `MCREMOTE_WIRE_CAPTURE_DIR` set to a scratch directory. Its frames must have the
   7.5.6 capture's shape (`directory`, `project`, `payload`), and include `sync` frames and a
   message carrying `cache` tokens. A different shape is a stop.
3. The frames are read under C4 and committed as `testdata/wire/7.8.18/` with a `meta.json` in the
   7.5.6 one's form.
4. **`scheduled`:** a test feeds `handleSessionStatus` a `scheduled` status for the parent session
   after a busy one, and requires the turn to end as an `idle` would. Seen failing on today's code.
   Then `case "idle", "scheduled":`.
5. `KnownGoodVersion = "7.8.18"`, its comment naming the new capture, and
   `TestKnownGoodVersionIs756` renamed and pointed at 7.8.18.
6. Checks: the kilo package's tests, including `TestFixtureSyncFramesAdvanceContiguously` and
   `TestFixtureConfirmsKiloReportsACacheRead` against the new capture;
   `go test -race ./internal/provider/kilo/ ./internal/provider/httpagent/`; `make pre-add-check`;
   `make vet-tags`; `make live-kilo` (C2).

### R3 — codex to 0.166.0

1. `codex --version` must still print `codex-cli 0.166.0`.
2. **The source tree:** `git clone --no-checkout` of `~/gitrepos/codex` into a scratch directory,
   then `git -C <scratch> checkout rust-v0.166.0`. The owner's checkout is read, never written.
3. **The contract:** `pwsh ./scripts/codex-contract.ps1 -Version 0.166.0 -SourceCommit <the tag's
   commit> -SourceTree <scratch> -DryRun`, then without `-DryRun`. The script was last run on
   Windows; a failure here is a stop, not a local patch.
4. The three embed directives point at `testdata/0.166.0/`. `KnownGoodVersion = "0.166.0"`, with
   its comment naming the new wire capture. `testdata/0.155.1/` is deleted; its README's three notes
   are carried into the new README where they still hold, each re-checked against 0.166.0.
5. **The capture (live, C2):** one short turn, as P5's codex capture was taken, with the
   initialize answer and the turn's frames. They are appended to `testdata/wire/0.166.0/` after
   P5's two frames are moved to follow them, read under C4. `capture0166` in
   `runtime_passthrough_test.go` selects its frames by method and by result shape, so the added
   initialize answer cannot be taken for the rate-limits answer.
6. Checks: `TestPinMatchesTheFixtureItCites` and `tokenusage_test.go` against 0.166.0;
   `go test -race ./internal/provider/codex/`; `make pre-add-check`; `make vet-tags`;
   `make live-codex-contract` and `CODEX_CONTRACT_EXACT=1 make live-codex-contract`;
   `make live-codex` (C2).

### R4 — opencode, as Q1 answers

* **Option A:** a test with a fake engine whose health answers `200 text/html`, and one whose
  version's major is above the pin's major. Each must leave the provider not ready, with a message
  naming both versions and 0214. Both are seen failing on today's code, which boots and fails later
  with a 405. A 1.18.x engine, and an engine reporting no version over a JSON health answer, behave
  as today.
* **Option B:** this phase becomes 0214's PLAN, written from 0220's breakdown and presented on its
  own.
* **Option C:** nothing.

### R5 — Records

Each phase's execution record, with output quoted and deviations dated. Index rows. The guard's
redact preview before staging, and the guard over the outgoing range before the owner pushes.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | grok's pin test passes against 1.0.70 | D1 |
| V2 | kilo's pin, `sync` and cache tests pass against 7.8.18; `make live-kilo` passes | D2 |
| V3 | The `scheduled` test failed on today's code and passes | D2, F4 |
| V4 | codex's manifest and `KnownGoodVersion` are both 0.166.0; the pin and token-usage tests pass; both contract modes and `make live-codex` pass | D3 |
| V5 | R4 meets Q1's option | D4 |
| V6 | `go test -race ./...`, `make vet-tags`, `make lint`, `make staticcheck` pass at each phase | all |

## Rollout and Rollback

* **Rollout:** a new pin warns on every other release, as today, and refuses only under Q1 A's
  rule.
* **Rollback:** revert the phase's commit. The previous pin and its capture return.

## Owner decisions needed at approval

* **Q1** (the MADR): opencode, A, B or C.
* **The live runs in C2,** approved together or one at a time.

## The owner's answers

*Answered by the owner, 2026-11-01:* Q1 "Do 0214 now" (option B), so R4 is 0214's PLAN, presented
on its own; and "Approve, all live runs", which covers every run named in C2.

## Execution record

### R1 — 2026-11-01: grok to 1.0.70

1. `grok --version`: `grok 1.0.70 (f50904eaee6c) [stable]`, the build 0207 C3 ran.
2. `KnownGoodVersion = "1.0.70"`. The comment names 0207 C3's run and the 1.0.70 capture as the
   evidence, says `grok-build` stops at 1.0.45, and keeps the 1.0.13 capture as the whole-turn
   fixture other tests read by name. Its `agentInfo` sentence now covers both captures: 0 of the
   1.0.70 capture's 18 frames carry `agentInfo`.
3. **Checks.**
   * `TestPinIsCorroboratedByItsFixture`, on scratch copies (`repin/r1_plant.py` in the session
     scratchpad; removed): pin `1.0.71`, "its fixture is unreadable … no such file or directory";
     pin `1.0.13x`, the same; the 1.0.70 capture edited to report `1.0.69`, "fixture reports
     agentVersion \"1.0.69\" but KnownGoodVersion is \"1.0.70\"". Each exit 1.
   * `go test -race -count=1 ./internal/provider/grok/ ./internal/provider/acpagent/`: both ok.
   * `make pre-add-check FILES=internal/provider/grok/version.go`: "1 file(s) clean";
     `make vet-tags`: exit 0; `make lint`: exit 0.
   * No other code names the grok pin: `grok.go:52` passes it to the Spec, and
     `acpagent/version.go` compares against whatever the Spec carries.

**Files for this commit:** `internal/provider/grok/version.go`, this pair, and `docs/README.md`'s
two index rows for it.

*The owner committed R1 as `b6022f4d`. The owner then had the grok sources checked: the local clone,
the owner's fork and upstream `xai-org/grok-build` are the same commit, `2bdd1d6a63` (1.0.45,
2026-09-29). Upstream has one branch, no tags and no releases, so no newer grok source exists to
pull. R1's comment stands.*

### R2 Deviation 1, 2026-11-01 — `version_test.go` names the pin. The owner decided: "Swap the literal to 7.8.18"

- **Found** after the pin moved: `TestKnownGoodPinComparesFullSemver`
  (`internal/provider/kilo/version_test.go:20-25`) lists `7.5.6`, `v7.5.6` and `7.5.6+build` as
  releases that must not warn. With the pin at 7.8.18 they warn: "drift warning logged = true, want
  false". Every other kilo test passed.
- **Resolution:** `version_test.go` joins R2. Its cases become `7.8.18`, `v7.8.18` and
  `7.8.18+build` (no warning), and `7.8.18-rc.1` and `7.8.19` (warning). `nonsense` is unchanged.
- **Not taken:** deriving the cases from `KnownGoodVersion`, which would spare the next re-pin this
  edit.
- **The MADR is unchanged.**

### R2 Deviation 2, 2026-11-01 — `make live-kilo` failed four tests. The owner decided: "Re-pin them to 7.8.18", "Test skips the current", "Re-run it alone"

`make live-kilo` against kilo 7.8.18: exit 2, 182 passed, 4 failed, 3 skipped (203.5 s). None of
the four comes from R2's changes.

- **a. `TestLiveKilo7423Surface` and `TestLiveKilo7423ACPInitialize`** (`live_surface_test.go:25`,
  `:324`, `:534`): "kilo --version = \"7.8.18\", want \"7.4.23\"" and "ACP agent info = {Name:Kilo
  Version:7.8.18}, want Kilo 7.4.23". They pin 0108's 7.4.23 surface (version, 255/680/119 OpenAPI
  paths/schemas/events, required event types, ACP capabilities and auth), stop at the version line,
  and have failed on every kilo after 7.4.23; the 7.5.6 re-pin did not touch them. **Resolution:**
  `live_surface_test.go` joins R2. The tests compare against `KnownGoodVersion`, take the
  7.8.18 values from what the engine reports, and lose "7423" from their names.
- **b. `TestLiveRecommendedRoutersRankFirst`** (`live_catalog_test.go:126-138`): "recommended
  kilo/kilo-auto/efficient ranks below unrecommended kilo/kilo-auto/balanced". A catalog read
  (`repin/kilocap/run_probe.py`, no turn) shows 321 options with default
  `kilo/kilo-auto/balanced`, which the gateway sends with no `recommendedIndex`, then the nine
  recommended ones in index order 0 to 8. The picker puts the current model first by design
  (`internal/picker/order.go:43`); the test does not allow for it. The catalog comes from kilo's
  gateway, so this tracks the gateway's data, not the binary. **Resolution:**
  `live_catalog_test.go` joins R2, and the check starts after the catalog's current option. The
  ordering code is unchanged.
- **c. `TestLivePromptStream`** (`live_test.go:55`): "second create took 2.659783s, want <2s on a
  shared engine". `make pre-add-check` (with govulncheck) and `make lint` ran during the suite.
  **Resolution:** the test is re-run alone on a quiet machine. A pass records contention as the
  cause; a failure is a stop.
- **The MADR is unchanged.**

### R2 — 2026-11-01: kilo to 7.8.18

1. `kilo --version`: `7.8.18`.
2. **The capture** (`repin/kilocap/run_capture.py`; a scratch-only test in a scratch copy of the
   tree, removed afterwards). One turn, "Reply with exactly 'hello'", on
   `openrouter/openrouter/free`: `end_turn` in 7.3 s, 108 frames.
3. **Read before committing** (`scan.py`, `scan2.py`). The frames have the 7.5.6 capture's shape
   (`directory`, `project`, `payload`), 18 `sync` frames carrying a `syncEvent`, and 18 lines with
   `"cache"`. Every `project` is `global`. The only identifier is the macOS per-user temp root:
   132 occurrences, 122 of them in the `/private/var/folders` form, none URL-encoded.
   `write_capture.py` replaced its two segments as the grok captures do
   (`/var/folders/<id>/<user>/T`) and wrote all 108 frames to `testdata/wire/7.8.18/`, with a
   `meta.json`. The guard's redact preview: "would edit 0 file(s)";
   `TestCommittedFixturesCarryNoIdentifiers`: ok.
4. **`scheduled`.** `TestSessionStatusScheduledEndsTheTurnLikeIdle` (`lifecycle_test.go`) sends busy
   then `scheduled`, and requires what busy then `idle` gives. On today's code: "node status after
   scheduled = \"busy\", want \"idle\"", "EndTurn calls after scheduled = 0, want 1", and one event
   where idle gives three. Then `case "idle", "scheduled":` in `lifecycle.go`, with a comment
   citing F4: the test passes.
5. `KnownGoodVersion = "7.8.18"`, with the comment naming the new capture and the surface
   comparison. `TestKnownGoodVersionIs756` became `TestKnownGoodVersionIs7818`. `version_test.go`'s
   cases moved to 7.8.18 (Deviation 1).
6. **Checks.**
   * Plants on scratch copies (`repin/r2_plant.py`): with the capture's `sync` frames removed,
     `TestFixtureSyncFramesAdvanceContiguously` fails, "no sync frames in the fixture"; with the
     `scheduled` case undone, the new test fails as in step 4. With the `cache` lines removed,
     `TestFixtureConfirmsKiloReportsACacheRead` skips rather than fails. That is its existing design
     (`usagedetail_test.go:116-119`); against the real capture it runs and passes.
   * **`make live-kilo`, first run:** exit 2; 182 passed, 4 failed (Deviation 2).
   * After Deviation 2: `TestLivePromptStream` alone, on a quiet machine, passed in 6.47 s. The
     surface was measured live with only the version constant moved (`r2_surface_measure.py`):
     paths, schemas and events 280/748/121. The v7.8.18 tag's `packages/sdk/openapi.json` has the
     same 280 paths and 748 schemas. The ACP initialize and read-only-boundaries tests passed
     unchanged apart from the version.
   * **`make live-kilo`, second run:** exit 0. 186 passed, 0 failed, 3 skipped in 108.0 s, among
     them `TestLiveKiloSurface`, `TestLiveKiloACPInitialize`, `TestLiveRecommendedRoutersRankFirst`
     and `TestLivePromptStream`. The three skips each need an opt-in variable:
     `MCREMOTE_KILO_ENGINE`, `MCREMOTE_LIVE_AUTH_WRITE` and `MCREMOTE_RAW_OUTPUT_DIR`.
   * `go test -race -count=1 ./...`: exit 0, 45 packages ok. `make pre-add-check` on the seven Go
     files: "7 file(s) clean". `make lint`, `make staticcheck`, `make vet-tags`,
     `make check-records`: exit 0 each.

**Not done:** `TestLiveKilo7423ReadOnlyAgentBoundaries` keeps its name. It passed against 7.8.18 and
names no version; Deviation 2 renamed only the two that failed. `kilo-spike-7.4.23/README.md` still
names the old tests, as the record of the 0108 run it is.

**Files for this commit:** `internal/provider/kilo/version.go`, `lifecycle.go`,
`lifecycle_test.go`, `dialect_test.go`, `version_test.go`, `live_catalog_test.go`,
`live_surface_test.go`, `testdata/wire/7.8.18/frames.jsonl` and `meta.json` (new), and this plan.
