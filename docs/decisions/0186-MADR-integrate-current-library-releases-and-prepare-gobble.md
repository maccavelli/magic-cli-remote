---
status: accepted
date: 2026-10-13
decision-makers: Project Owner
consulted: none
informed: maintainers of go-selfupdate-lib, acp-go-sdk2 and gobble-cli
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Integrate the current `go-selfupdate-lib` and `acp-go-sdk2` releases as two independent tracks, then make gobble the native agent

## Context and Problem Statement

The owner's direction (2026-10-13): implement the current releases of `go-selfupdate-lib` and
`acp-go-sdk2`, remove every `mcplib` dependency, and refactor wherever possible so that Magic's core
offloads as much as it can to those libraries and stays compatible with them. Both libraries are
still under active development, and Magic pins their latest tags. `gobble-cli` is to become the
native agent CLI for this platform, as designed.

Each library already has a decision record in this repository, and neither is accepted:

* [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
  is `proposed`. Its 2026-10-13 amendment decides full adoption at `v1.15.2` (O1–O4, D1–D14), and
  its PLAN carries source phases S1–S8. Only R1, a report, has run.
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
  is `proposed`. It decides full adoption at `v0.17.1` (D1–D13), and its PLAN carries phases A1–A5.
* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md), the
  native-agent contract, is `proposed`. It still names the agent `pigo`. Its PLAN ran only R1,
  which produced
  [0179-REPORT-gobble-native-acp-integration-assessment.md](../reports/0179-REPORT-gobble-native-acp-integration-assessment.md).
* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md)
  is `accepted`, and keeps `go-llmprovider-sdk` out of this work.

So no source phase can start today. This record answers three questions:

* What has to happen to begin?
* In what order, and against which tags?
* What else in the core can be handed to the libraries?

It records the evidence gathered on 2026-10-13. Where that evidence changes a fact that 0054, 0184
or 0179 asserts, it names the amendment those records need. It does not restate their decisions.

### Baselines

Read on 2026-10-13. Every library was read from the Go module cache at its tag, and each sibling
clone was read and never written.

| Repository | State | Pins |
| :--- | :--- | :--- |
| `magic-cli-remote` | `master` `64e3b21`, clean tree | `coder/acp-go-sdk v0.13.5`, replaced by the fork `v0.13.6-mcr.1`; `mcplib v1.4.1` |
| `go-selfupdate-lib` | `main` `5c0ab0d`, clean. `v1.15.2` → `436a7f5976a6815491c7384527fef90869574a1b`; the two commits past it change documentation only | — |
| `acp-go-sdk2` | `main` `e91d1b8`. `v0.17.1` → `c77f25a45e3345c40f954dd80add9f09a1fd5ae1`; the two commits past it add records only. Uncommitted edits to `connection.go`, `errors.go` and tests are the work of its `0019-PLAN-behaviour-fixes.md` | — |
| `gobble-cli` | `main` `007b63f`, clean, **no tags** | `acp-go-sdk2 v0.17.1`, `go-selfupdate-lib v1.15.2`, `go-llmprovider-sdk v1.12.0` |

### What was measured, not assumed

**A combined probe on a scratch copy** of `64e3b21`, with Go 1.27.2 on darwin/arm64. The working
tree was not touched. Both libraries were swapped at once: `go get` of both tags, the fork `replace`
and the Coder requirement removed, `go mod tidy`, then build, vet and test.

* **`go.mod`** loses `coder/acp-go-sdk`, `mcplib` and the `replace`. It gains `acp-go-sdk2 v0.17.1`,
  `go-selfupdate-lib v1.15.2`, and two `// indirect` lines (`davecgh/go-spew`,
  `pmezard/go-difflib`) that `mcplib` used to supply. No other module moves. The MCP go-sdk leaves
  the module graph.
* **The self-update side compiles unchanged.** The 11 `mcplib/selfupdate` importers need only the
  path change. `internal/updateclient`, `internal/cli/service` and `internal/relay` pass their
  tests on the new library. The behavior trap recorded in 0054's amendment still holds: an
  installed, enabled, stopped daemon is no longer restarted by an update, and nothing fails.
* **The ACP side is the whole compile cost:** 21 diagnostics, all in `internal/provider/acpagent`.
  * `acpagent.go`: 4, from the missing `CompleteElicitation` and from `AuthMethodId`;
  * `session.go`: 6, from `CompleteElicitation`, usage `uint64`, and file line and limit `uint32`;
  * `sessioncaps.go`: 2, from `UnstableDeleteSession`;
  * `terminal.go`: 9, from `TerminalId`, the output limit `uint64`, and the exit status `*uint32`.

  These are 0184 D3's groups. The test files are not compiled until the package compiles, so their
  diagnostics are still hidden; 0184 counted 25 in 8 files including tests.
* **What stops building:** `cmd/mcremote`, `internal/cli`, `internal/daemon` and
  `internal/provider/grok` depend on `acpagent`. The test binaries of `internal/command`,
  `internal/provider` and `internal/session` also stop building. Every other package builds, and
  its tests pass.
* **Two failures are not the swap's.**
  * `internal/modguard_test.go`'s fixture test failed because the probe also rewrote the
    Coder-path string literal at `:55`. 0184 D1 rewrites that test.
  * `internal/ws`'s `TestSessionHandoffReceiptsEndToEnd` and `TestReceiptEndToEndOverRealWS`
    failed once under full-suite load. Rerun alone, they passed 3 of 3 on the copy and 1 of 1 on
    the untouched tree, and `internal/ws` imports neither library.

**`acp-go-sdk2 v0.17.1` ships defects its owner has already decided to fix.** Its
`0019-MADR-remediate-debugging-pass-findings.md` (accepted 2026-10-13) records a debugging pass at
the `v0.17.1` commit. Phases 1 and 2 of that record ship together as `v0.18.0`, which is not tagged.
`v0.18.0` also adds exported API:

* `Close`;
* the `SetClientConnection` hand-off;
* `WithMaxConcurrentRequests`;
* a writer goroutine;
* `ErrPeerDisconnected`, with `Unwrap`.

It also removes identity `MarshalJSON` methods, and gates extension messages received before
`initialize`. The findings that reach Magic as an ACP client:

| SDK finding | What it does | Where Magic meets it |
| :--- | :--- | :--- |
| B16 (gap) | A write holds the write lock across a blocking `Write`, ignoring the caller's context. A probe's 200 ms deadline was still blocked after 1.5 s | Magic's close sends `CloseSession` under a 2-second context, then kills the process tree (`internal/provider/acpagent/session.go:1228-1254`). Against an agent that has stopped reading, with the pipe full, that bound does not hold **[derived]** |
| B4 (medium) | `ClientSideConnection.CloseSession` does not cancel the session's pending permission requests, as `Cancel` does | the same close path. 0184 D8 closes the phone's permission cards on cancel, but not on close |
| B9 (low) | A typed request is written even when its context has already ended | any Magic call made with an expired context |
| B19 (idiom) | Local failures come back as `*RequestError` codes. A local cancel or disconnect is indistinguishable from the peer's, and `errors.Is(err, context.DeadlineExceeded)` is false | 0184 D5's error classification |
| B1 (high) | A request sent from inside a notification handler deadlocks | **not reached:** Magic's `SessionUpdate` only emits events and sends no request (`session.go:1608-1788`) **[derived]** |

**Two releases are unusable, and a gate cannot see one of them.** GitHub advisory
GHSA-579q-9j9v-7w8w affects `acp-go-sdk2 v0.15.0` exactly: it accepted an empty `cwd`. The Go
vulnerability database does not carry it, so `govulncheck`, Magic's pre-add gate, reports nothing
for that pin. `v0.17.0` races its own constructor, and a fast `initialize` can crash the process.
`v0.17.1` fixes that. gobble measured both (gobble `0019-MADR-sibling-libraries-current-pins-and-integration.md`,
"The advisory" and F15).

**The host path rule reaches Magic's Windows CI.** From `v0.16.0`, the SDK checks 17 path
properties: with a portable rule when they are sent, and with the host's `filepath.IsAbs` when they
are received. So a POSIX path such as `/work` is refused when received on Windows.

* Magic's `go-native` CI job runs the whole Go suite on `windows-latest`
  (`.github/workflows/ci.yml:287-300`, test step `:360`).
* `acpagent`'s tests hold 15 POSIX path literals in six files (`/tmp`, `/tmp/cwd`, `/tmp/a.go`,
  `/etc/hosts`, `/bin/echo`), and `internal/provider/grok`'s tests hold a few more (`/tmp/decoy`,
  `/opt/fake/grok`, `/usr/bin/true`).
* The committed grok `1.0.13` wire capture, `internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`,
  holds 383 POSIX path values.
* Which of these reach the SDK's receive check is **[unverified]** until `acpagent` compiles.
  gobble's bump produced 27 Windows test failures at `v0.17.0`, 22 of them from exactly this.
  gobble repaired them with host-absolute fixtures and a recorder that writes a placeholder root
  (gobble 0019-MADR F3 and D4).

**gobble has caught up, and the remaining gaps are on Magic's side.**

* gobble now pins both releases Magic will pin. 0184 D12's "gobble is at `v0.14.0` today" no longer
  holds, and the 0179 report's version risks R5 and R7 are closed.
* Its advertisement (`acpserver/agent.go:212-243`) covers:
  * `loadSession`;
  * MCP over HTTP;
  * embedded context;
  * the session capabilities `additionalDirectories`, `close`, `list` and `resume`;
  * `_meta.gobble.extensions`.

  It advertises no auth methods, so the 0179 report's R6 (credentials) is still open. It has
  published no extension-types package, so R3 is still open. R4 (whether Magic advertises `fs` and
  `terminal` to gobble) is Magic's decision.
* gobble has no release tag. Magic's version pin compares major.minor.patch only, and ignores any
  prerelease part (`internal/provider/version.go:8-59`; kilo and opencode keep older copies,
  `:32-34`). So a pin has no gobble release to name, and a prerelease build would match it (gobble
  M7).
* gobble's running list of findings about Magic, `0009-REPORT-magic-cli-remote-findings.md`, has
  grown to M14, all open. The ones added since the 0179 report concern plans and todos:
  * M11: a replayed plan is deduplicated away whatever its entries;
  * M12: a cold-started phone loses the session's plan;
  * M13: grok's cancelled todos show as done, because `acpcommon.PlanEntries` drops `_meta`;
  * M14: ACP providers have no todo resync, and gobble will offer `_gobble/get_todos`.

  M8, the fork's `SetLogger` race, is resolved by leaving the fork (0184 D2).
* gobble's own pin decision makes "magic-cli-remote's ACP session runs against the bumped agent" a
  condition of its SDK bump. Each repository's ACP suite therefore becomes the other's acceptance
  gate.

**gobble already does the checking this work needs.** Its `scripts/check_libs.py` takes each
library's latest semver tag and works on a scratch clone:

* `go get` and `go mod tidy`;
* `go build`, and `go vet` for linux, darwin and windows;
* `go test`;
* apidiff, with the reverse run as its negative control;
* `govulncheck`, plus the library's GitHub advisories;
* the `go.mod` and `go.sum` diff.

It needs the network, so it stays out of preflight (gobble 0019-MADR D14).

### Where the core can hand work to the libraries

`acp-go-sdk2` is a single package, `acp`, with types and the connection. `go-selfupdate-lib`
publishes `buildinfo`, `selfupdate`, `selfupdate/cli`, `archive`, `codesign`, `releasespec`,
`selfupdatetest`, `service`, and the `systemd`, `launchd`, `scm` and `taskscheduler` backends. Set
against Magic's packages:

| Magic code | Size (non-test lines) | Library home | Owning record |
| :--- | ---: | :--- | :--- |
| `internal/updateclient` (`client.go`, `lifecycle.go`, `legacy.go`, `codesign_*.go`) | 596 | `selfupdate/cli` (`Flags`, `Run`, `Exit`), the backends' `Lifecycle`, `selfupdate/codesign` | 0054 D2–D7, S1–S3 |
| `internal/cli/service` lifecycle, health and refresh plumbing (`control.go`, `control_schtasks.go`, `exec_refresher.go`, `launchd_wait.go`) | about 560 | `service/systemd`, `service/launchd`, `service/taskscheduler`, `service.NewExecReconciler`, `service.PollHealthy` | 0054 D2–D6 |
| build stamps, release spec, publish and build workflows, the hand-written installers | — | `buildinfo`, `releasespec`, the library's reusable workflows and generated installers | 0054 D8, D14, S2–S4, S7 |
| `acpagent`'s copied capability flags, MCP transport filter, path checks, `-32601` and `-32800` literals, raw `session/set_model`, the `unsafe` connection cast | spread over `acpagent.go`, `session.go`, `sessioncaps.go`, `thinking.go` | `Negotiated()`, `Allows`, the SDK's gates and validation, typed calls, `Connection()` | 0184 D2–D8 |
| version pins (`internal/provider/version.go`, and kilo's and opencode's older copies) | 59 plus copies | neither library exports a comparison. `golang.org/x/mod/semver`, already in the module graph, compares full SemVer | this record, D8 |

These stay Magic's, because no library owns them:

* the ACP terminal host (`terminal.go`) and the file handlers;
* the service definitions and their refresh child, and the runit, s6 and OpenRC supervisors (0054
  O1, O2);
* the HTTP and app-server providers (codex, opencode, kilo);
* the model catalogs (0185).

## Decision Drivers

* **Begin from accepted records.** Nothing mutates until 0054 and 0184 are accepted and a phase is
  approved by name.
* **Pin released tags only.** Never a pseudo-version, never `acp-go-sdk2 v0.15.0`, and nothing
  below `v0.17.1`.
* **One library per change,** so a regression bisects to one library.
* **Windows CI stays green at every commit,** with no test skipped and no assertion loosened.
* **Accept a known defect only with a named mitigation,** and delete the mitigation when the fix
  ships.
* **One record decides each thing.** This record sequences and adds what is new. 0054 and 0184
  keep their decisions.
* **Make gobble native without special cases:** a `Spec`, a command table and a small extension
  surface, after the ACP track.

## Considered Options

* **A. Two independent tracks on the current tags, gobble after the ACP track, and `v0.18.0` as a
  planned bump** (recommended).
* **B. Self-update now, and hold the ACP track until `acp-go-sdk2 v0.18.0`.**
* **C. One combined change for both libraries.**
* **D. Pin the libraries' unreleased `main`** to get the debugging-pass fixes now.

## Decision Outcome

Chosen option: "**A. Two independent tracks on the current tags, gobble after the ACP track, and
`v0.18.0` as a planned bump**", because it meets the owner's direction on the releases that exist
today. It keeps each library's change reviewable on its own. Every known `v0.17.1` defect that
reaches Magic either has a mitigation or is not reached.

* B delays the larger half of the work for a release that is not tagged and has no date.
* C makes any regression ambiguous between two libraries.
* D pins code the owner has not released, which gobble's records and 0184 D1 rule out.

### The decisions

**D1 — The pins are `go-selfupdate-lib v1.15.2` and `acp-go-sdk2 v0.17.1`.**

* Both are tags, resolved to the commits in "Baselines". Each workflow pin uses the peeled commit.
* `acp-go-sdk2 v0.15.0` is never pinned, and nothing below `v0.17.1` is.
* This confirms 0054 D1 and 0184 D1 against today's tags. It adds the floor and the advisory.

**D2 — Two tracks, each landing its own library in its own commit.**

* The self-update track is 0054's PLAN, S1 to S8. The ACP track is 0184's PLAN, A1 to A5.
* Neither waits for the other. Their commits never mix the two libraries.
* They may ship in the same release.
* S1 removes `mcplib` entirely. After S1, `go.mod` names no `mcplib` module, and a file scan finds
  `mcplib` only under `docs/`.

**D3 — The process begins with acceptance, not code.** The first steps, in order:

1. The owner accepts this record.
2. The owner accepts 0054, confirming its D9 Windows folder name.
3. The owner accepts 0184, with the D4 amendments below.
4. The owner names phases ("do S1", "do A1").

This record authorizes no source change.

**D4 — Amendments this record asks of 0184** (written under 0184 once this record is accepted, as
dated amendments, without rewriting its text):

* **D12's fact:** gobble already pins `v0.17.1`, so the one-pin rule holds today.
* **The `v0.17.1` defects and their mitigations** (D6 of this record), and the `v0.18.0` bump as a
  named later phase.
* **The host path rule** (D5 of this record) as a rule for every ACP phase.
* **A1's verification:** it includes the `windows/amd64` leg and the Windows local gate
  (`make ci-windows`,
  [0145-MADR-local-windows-ci-style-tests.md](0145-MADR-local-windows-ci-style-tests.md)) before A1
  is called done.

0054 needs no amendment from this record. Its 2026-10-13 amendment already matches what the probe
found.

**D5 — ACP fixtures are absolute on the host they run on, and recorded wire is host-independent.**

* A test path that reaches the SDK (`cwd`, additional directories, file paths, terminal `cwd`) comes
  from `t.TempDir()`, or from a helper that maps a fixed POSIX fixture path to an absolute path on
  the running host.
* A wire capture replayed through the SDK has its recorded root rewritten to the host's on read. A
  capture is never edited to suit a platform.
* No test is skipped on Windows to get there. This follows gobble's 0019-MADR D4, which met the same
  rule first.

**D6 — Accept `v0.17.1`'s known defects with these mitigations, and delete each when `v0.18.0`
lands.**

* **B16:** the close path stops relying on a context to bound a write. `CloseSession` runs bounded
  by a timer that does not wait for the write, and the process-tree kill follows when the timer
  fires. A test with an agent that stops reading shows close returning within the bound. It is seen
  failing first against today's code.
* **B4:** closing a session also ends its pending permission requests, and closes their phone cards
  as 0184 D8 does on cancel.
* **B9:** Magic does not start a typed call with a context it knows has ended. The turn and close
  paths check it first.
* **B19:** until `v0.18.0`, errors are classified by the SDK's code constants and `errors.As`, as
  0184 D5 says. At `v0.18.0`, a disconnect and a local cancel are recognized with `errors.Is`.
* **B1:** `SessionUpdate` keeps sending no request. A comment at the handler states the rule and why.
* **The `v0.18.0` bump** is its own 0184 phase. It starts only after its release notes are read
  against 0184's D4–D8 and this list, because it removes exported methods and gates extension
  messages received before `initialize`.

**D7 — A library-compatibility check runs before every pin and on demand.**

* `scripts/check_libs.py`, with `make check-libs`, does what gobble's does for the two libraries
  Magic imports:
  * the latest semver tag, on a scratch clone, never in the working tree;
  * `go get`, `tidy`, `build`, and `vet` for linux, darwin and windows;
  * `go test`;
  * apidiff, with its reverse run as the negative control;
  * `govulncheck` and the library's GitHub advisories;
  * the `go.mod` and `go.sum` diff, failing on a new module.
* It is not in `make preflight`, which runs offline.
* It extends the rule of
  [0174-MADR-keep-dependencies-free-of-known-advisories.md](0174-MADR-keep-dependencies-free-of-known-advisories.md)
  D1 (no required module with a known advisory that a newer release fixes) to advisories that
  0174's `govulncheck` gate cannot see, such as GHSA-579q-9j9v-7w8w.
* It is seen failing on a planted incompatible tag before it is trusted.
* Whether it is copied from gobble or shared is an owner question (Q6).

**D8 — Version pins compare full SemVer.**

* `provider.SameVersion` and kilo's and opencode's copies become one comparison over
  `golang.org/x/mod/semver`. A prerelease or pseudo-version ranks below its release, and never
  matches a release pin.
* This closes gobble finding M7.
* It lands before any gobble version is pinned. A test shows `1.3.0-rc.1` failing a `1.3.0` pin,
  and it is seen failing on today's comparison first.

**D9 — gobble becomes native after the ACP track, under its own record.**

* Once 0184's A2 has landed (an honest `initialize`, and capabilities read from `Negotiated()`), a
  new MADR supersedes 0179 for gobble on schema 1.25.
* It covers:
  * the rename (gobble M1, M3);
  * 0179 D2 and D3, capability reporting and strict fallback, with failing-first tests (M2);
  * `SessionOps` on gobble's frozen `_gobble/*` table, including `customInstructions` (M9);
  * steer and follow-up through `_gobble/steer` and `_gobble/follow_up` in place of a second
    `session/prompt` (M10);
  * the plan and todo findings, M11–M14;
  * the 0179 report's registration gates.
* gobble is registered only when that record's gates pass, and only against a gobble release tag.

### Consequences

* Good, because the work can start the day the records are accepted, against releases with no
  known defect that Magic cannot mitigate.
* Good, because `mcplib` leaves in one phase (0054 S1), and with it the fork, the `replace`, the
  `unsafe` cast and the MCP go-sdk in the module graph.
* Good, because each library lands in its own commit, so a regression bisects to one library, and
  `check-libs` makes the next bump's cost visible before it is taken.
* Good, because the Windows leg tests the same ACP behavior as POSIX, instead of failing on fixture
  paths after the bump.
* Bad, because `v0.17.1` is a stop on the way. The `v0.18.0` bump repeats part of the ACP work: the
  close path, error identity, and possibly code that relies on the removed identity `MarshalJSON`
  methods.
* Bad, because the D6 mitigations are code written to be deleted.
* Bad, because gobble's registration waits on two of this repository's phases and on a gobble
  release. gobble has no tag yet.
* Neutral, because the self-update track changes no compile surface. Its risk is the behavior trap,
  which 0054 D2 already closes.

### Confirmation

* Acceptance of 0054 and 0184 precedes the first source commit of either track. Each phase's
  execution record names its approval.
* `go list -m github.com/maccavelli/go-selfupdate-lib github.com/maccavelli/acp-go-sdk2` reports
  `v1.15.2` and `v0.17.1`. `go.mod` names no `mcplib`, no `coder/acp-go-sdk` and no ACP `replace`.
* Each library's swap is a separate commit. `git log --stat` shows no commit touching both import
  paths.
* The `go-native` `windows/amd64` leg and `make ci-windows` pass after A1, with no new
  Windows-specific skip.
* The close test of D6 (B16) and the version test of D8 are each seen failing before their change.
* `make check-libs` reports both libraries at their latest tag with no incompatible change and no
  new module. It reports a failure for a planted incompatible tag.

## Pros and Cons of the Options

### A. Two independent tracks on the current tags, gobble after the ACP track, and `v0.18.0` as a planned bump

* Good, because both tracks can start on accepted records and released tags.
* Good, because the tracks are independent: S1 needs no ACP change, and A1 needs no self-update
  change.
* Good, because every known `v0.17.1` defect is mitigated, or not reached by Magic.
* Bad, because the ACP track is touched twice, at `v0.17.1` and at `v0.18.0`.

### B. Self-update now, and hold the ACP track until `acp-go-sdk2 v0.18.0`

* Good, because the ACP work is done once, on the release with the close, writer and
  error-identity fixes.
* Bad, because `v0.18.0` is not tagged and has no date. The fork, its `unsafe` cast and gobble's
  blocked registration all wait with it.
* Bad, because it departs from the owner's "tag to the latest releases".

### C. One combined change for both libraries

* Good, because `go.mod` changes once.
* Bad, because a regression after the change cannot be told apart between the libraries.
* Bad, because the self-update behavior trap and the ACP compile work would be reviewed together.

### D. Pin the libraries' unreleased `main`

* Good, because `acp-go-sdk2`'s debugging-pass fixes would arrive sooner.
* Bad, because a pseudo-version names a commit, not a reviewed release. 0184 D1's guard test and
  gobble's records both refuse it.
* Bad, because `acp-go-sdk2`'s `main` holds uncommitted work, and its phase 1 is not to be released
  on its own (its 0019-MADR, decision 12).

## More Information

### Questions for the owner

The owner answered all seven on 2026-10-13. The answers are in "Amendment — 2026-10-13: the owner's
answers" at the end of this record.

* **Q1:** The Windows install folder of 0054 D9: `%LOCALAPPDATA%\Programs\magic-cli-remote`
  (proposed) or `…\Programs\mcremote`.
* **Q2:** Supersede 0179 with a gobble record (recommended, D9), or amend it in place.
* **Q3:** Whether Magic advertises `fs` and `terminal` to gobble. The 0179 report's R4 recommends a
  per-`Spec` override that advertises neither.
* **Q4:** Where gobble's model-provider credentials live on a Magic host: a per-provider
  environment, or gobble implementing ACP `authenticate` (R6).
* **Q5:** Whether Magic ever runs `gobble update`, or only warns through its version pin (R7).
* **Q6:** Whether `check_libs.py` is copied from gobble or shared between the repositories.
* **Q7:** Provider quota for 0184's live grok probes and `make live-grok` at acceptance. It is asked
  for separately, when that phase is reached.

### Evidence index

* Probe: a scratch copy of `64e3b21`, Go 1.27.2 darwin/arm64, both libraries swapped. The steps and
  results are summarized under "What was measured". The probe script and its full log stayed in the
  session scratchpad and are not committed.
* Magic:
  * `internal/provider/acpagent/session.go:1228-1254` (the bounded close, then the kill);
  * `session.go:1608-1788` (`SessionUpdate` only emits);
  * `internal/modguard_test.go:55`;
  * `internal/provider/version.go:8-59`;
  * `.github/workflows/ci.yml:287-300` and `:360`;
  * `internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`.
* `acp-go-sdk2`: `docs/decisions/0019-MADR-remediate-debugging-pass-findings.md` (B1, B4, B9, B16,
  B19, decisions 4 and 12).
* `go-selfupdate-lib`: `docs/guides/migrating-from-mcplib-selfupdate.md` §2–§5 at `v1.15.2`.
* `gobble-cli`:
  * `docs/decisions/0019-MADR-sibling-libraries-current-pins-and-integration.md` (the advisory, F3,
    F15, D4, D14);
  * `docs/reports/0009-REPORT-magic-cli-remote-findings.md` (M1–M14);
  * `acpserver/agent.go:212-243`;
  * `scripts/check_libs.py`.

Repository names are given for cross-repository records, because a relative link cannot reach
another repository.

### Related records

* [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
  and its PLAN: the self-update track.
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
  and its PLAN: the ACP track.
* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) and
  [0179-REPORT-gobble-native-acp-integration-assessment.md](../reports/0179-REPORT-gobble-native-acp-integration-assessment.md):
  the native-agent contract that D9's record supersedes.
* [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md):
  the overflow policy that 0184 D2 carries onto the new SDK.
* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md):
  the third library, kept out.

## Amendment — 2026-10-13: the owner's answers

The owner answered Q1–Q7 on 2026-10-13. The record stays `proposed`. Where an answer changes a
decision above, this amendment governs, and the original text stays as what was first proposed.

* **Q1 — the Windows folder is `%LOCALAPPDATA%\Programs\magic-cli-remote`.** 0054 D9 stands as
  written.
* **Q2 — 0179 is renamed to gobble, not superseded by a new number.** This replaces D9's "a new MADR
  supersedes 0179".
  * The owner's rule: nothing in `gobble-cli` references `pi-go` or `pigo`. The project is
    `gobble-cli`, and the binary is `gobble`.
  * A scan on 2026-10-13 found no such reference in gobble's code. Its only ones are in seven of its
    records, and every one cites or quotes this repository's `0179-MADR-gobble-native-acp-provider.md`.
    So this repository's filename is the source.
  * The 0179 pair is renamed with `git mv`, to `0179-MADR-gobble-native-acp-provider.md` and
    `0179-PLAN-gobble-native-acp-provider.md`. Every link to them is repaired, and the index is
    regenerated. The rename is its own records change, so gobble can repoint its citations without
    waiting.
  * The renamed MADR is then rewritten as the gobble contract on schema 1.25: `IDGobble`, wire id
    `"gobble"`, binary `gobble`, and gobble's frozen `_gobble/*` table. It carries D9's scope. A dated
    note records the rename and the earlier name. The rewrite follows 0184's A2, as D9 says.
  * Repointing gobble's records is gobble's work, under gobble's records. This repository does not
    write to `gobble-cli`.
* **Q3 — Magic advertises neither `fs` nor `terminal` to gobble.**
  * Client capabilities become a per-`Spec` setting. gobble's `Spec` advertises neither, and
    grok's keeps today's advertisement.
  * 0184 D4's "keep `fs.readTextFile`, `fs.writeTextFile` and `terminal`" applies to grok's `Spec`
    only. This joins the 0184 amendments D4 asks for.
  * The registration gate "no `fs/*` or `terminal/*` request arrives from gobble" (0179 report) is
    kept.
* **Q4 — gobble implements ACP authentication.**
  * gobble advertises an ACP auth method and implements `authenticate`. That is gobble's work, on
    `go-llmprovider-sdk`'s `TokenStore` and logins (gobble 0019-MADR D11).
  * Magic drives it through its existing phone provider-auth flow,
    [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md),
    as it does for grok's auth methods.
  * Magic adds no per-provider environment for gobble's keys.
  * gobble is not registered until it advertises an auth method.
* **Q5 — Magic only warns about gobble's version.** The version pin, on D8's full-SemVer comparison,
  reports a gobble that does not match its known-good release. The operator runs `gobble update`,
  and Magic never runs it.
* **Q6 — the library-compatibility check lives in a new shared repository, `go-shared`.** This
  replaces D7's "copied from gobble or shared".
  * `go-shared` holds scripts and tools shared across the owner's Go repositories. It sits beside
    the other Go repositories, and is scaffolded to the workspace standard.
  * `check_libs.py` lives in its `scripts/` directory, generalized from gobble's so each consuming
    repository names its own libraries.
  * D7's behavior is unchanged: latest tag, scratch clone, three systems, apidiff with a reverse
    control, advisories, module diff, and kept out of preflight. Only the script's home changes.
  * `go-shared`'s first records decide how a repository runs the script: a sibling checkout, or a
    pinned download. Magic's `make check-libs` then follows that decision, under this record's
    PLAN.
* **Q7 — provider quota is pre-approved for the ACP track's grok runs:** 0184's live grok probes
  (its A3) and `make live-grok` at 0184's acceptance. Any other provider's live run, and any further
  grok run, is still asked for first.
