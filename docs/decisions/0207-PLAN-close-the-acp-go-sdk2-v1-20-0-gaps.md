---
status: in-progress
date: 2026-10-28
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the fullest use of `acp-go-sdk2 v1.20.0` (0207 amendment B, D25 to D27)

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its "Amendment — 2026-10-23 (B)": D24 to D27, with the owner's answer to question 11 ("Once,
after D25").

Sibling plan: [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md).
The two touch no file in common. This one runs after 0210's L2 (that plan's "Order across plans").

## Goal

* ~~The pin stays `acp-go-sdk2 v1.20.0` (D24); nothing here moves it.~~ *Revised 2026-10-28:* the
  pin moves to `v1.25.0` (amendment H, D45, proposed). See "Revision for amendment H".
* A turn's standard token usage reaches the phone once, and never beside grok's xAI usage (D25).
* A missing file answers -32002; the SDK constructs the protocol values Magic builds by hand; a
  truncated wire capture and a protocol-version refusal are named; the unreachable subagent
  branches are gone (D26).
* The records that state facts the SDK has since changed carry dated notes (D27), and 0184's
  PLAN can close.

## Scope

* **In scope:** D25, D26 and D27, and the one quota run the owner approved in principle.
* **Not in scope:** the product features of D28 (`ResumeSession`, resource prompt blocks,
  `Roots()`, URL elicitation, stdio MCP, grouped options). Each takes its own MADR.

## Facts the steps rely on [read 2026-10-23, at `88d37d1c`]

| Fact | Where |
| :--- | :--- |
| `PromptResponse.Usage`, unstable | SDK `types_gen.go:11105-11120` |
| Magic reads only `StopReason` from the answer | `internal/provider/acpagent/session.go:537-543` |
| grok's per-turn usage comes from `_meta` | `xaiusage.go:14-18`, `:78`, `:133`; `subagents.go:100-102` |
| A missing file returns the `PathError` | `session.go:2653-2665` |
| Hand-built MCP entries, permission outcomes, config requests | `acpagent.go:639-649`; `session.go:2471-2480`, `:2611-2620`, `:1108-1112`, `:1168-1177` |
| The capture's close; `initialize` | `acpagent.go:606-607`; `:541-545` |
| The subagent decline branches | `session.go:2070-2075` |
| 0184's last open item, wire captures | `internal/provider/grok/testdata/wire/` holds `1.0.13` and `1.0.50` only |

Every line number is re-read before its phase starts, since the `go-selfupdate-lib` plan and
0210 may have landed first.

## Revision for amendment H (~~proposed~~ approved 2026-10-28)

The MADR's amendment H reads `v1.21.0` to `v1.25.0`, and re-proposes D25 and D26 there with D45
and D46. ~~If the owner approves it as proposed, this plan runs C0, C1, C1b, C2 and C3 below, in
that order. C4 is done. If the owner chooses `v1.22.0` (question 13), C0 pins that tag and skips
C0's step 3, and C2 needs no exception. If the owner chooses to stay on `v1.20.0`, C0 and C1b do not
run.~~ *Approved 2026-10-28, as proposed* (amendment H's answers: D45's exception, auto-approval
answers cancelled, Magic before gobble). This plan runs C0, C1, C1b, C2 and C3 below, in that
order; C4 is done. C3 still waits for the owner's word for its run.

**Facts re-read 2026-10-28, at `ac964b31`.** The table above holds, except these lines, which have
moved: permission outcomes at `session.go:2470-2483` and `:2610-2623`; the second config request
at `:1165-1180`. Each phase re-reads its lines again at its start.

### C0 — The pin and the unstable-name exception (D45)

**Files:** `go.mod`, `go.sum`; `internal/provider/acpagent/acpagent.go`, `plans.go`,
`plans_test.go`, `advertise_test.go`, `session.go` (the suppressions only);
`0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md` (a dated
amendment naming the exception); this plan. *Added by Deviation 1:* `Makefile` (the `staticcheck`
target) and `scripts/check_staticcheck_ignored.py` (new).

1. `go get github.com/maccavelli/acp-go-sdk2@v1.25.0`, then `go mod tidy`; a second `tidy` changes
   nothing.
2. `make staticcheck` lists the SA1019 uses. They must be amendment H's H-2 list: 23 outside
   tests, 35 in them. A use not on it is a deviation.
3. Each gets its suppression, as D45 bounds it: `//lint:file-ignore SA1019 …` in `plans.go` and
   `plans_test.go`, and `//lint:ignore SA1019 …` on each other line. Each reason names "unstable
   ACP schema", the feature's MADR (0192 for plans, 0195 for subagents), and 0207 amendment H.
   The three uses in `session.go:2070-2075` are left for C1, which deletes them; until C1, they
   take the line form.
4. **Seen to fail,** on scratch copies:
   * a use of another deprecated name in `plans.go` still fails `make staticcheck`;
   * so does one in `acpagent.go` beside a suppressed line.
5. The 0170 MADR gets its dated amendment, which cites D45 and says D5 otherwise stands.
6. Gates: the sibling plan's set, with `make staticcheck` for all three GOOS.

### C1, revised (D26)

As written above, at the moved lines. Steps 1 and 2 gain:

* the missing-file test asserts `acp.IsResourceNotFound(err)`;
* removing the decline branches removes their suppressions too, and `make staticcheck` shows 3
  fewer uses.

### C1b — The helpers that fix and replace (D46)

**Files:** `internal/provider/acpagent/session.go`, `session_meta.go`, `sessioncaps.go`,
`runtime.go`, `compaction.go`; `internal/provider/grok/fallback.go`; `internal/wirecap/wirecap.go`
(or wherever `tapWriter` lives, measured first); their tests; this plan.

1. **Tests first,** each seen failing on a scratch copy:
   * an option named "Don't allow" of kind `reject_once`, listed first, is not auto-selected;
     `allow_once` is preferred to `allow_always`;
   * options with no allow kind answer cancelled (question 14, answered 2026-10-28);
   * `pickAllowOptionID` agrees with `autoAllow` on the same options;
   * each H-6 replacement keeps its current test green, and its changed case (the decline names
     of an unknown variant; any repeated cursor) gets one.
2. **Code** as tested: `PickPermissionOption`; `ConfigOptionByCategory`; `MetaObject`; `IsMethodNotFound`, `IsAuthRequired`, `IsCancelled`;
   `ContentBlock.Kind`, `SessionUpdate.Kind`; `AllSessions`, `SessionInfo.Updated`;
   `agentproc.LineTap`; `Process.ExitedFirst`. Grouped select values are not here: 0208's P7
   carries them (amendment H, H-5).
3. Gates.

### C2, revised (D25)

As written above. Its reads of `PromptResponse.Usage` take D45's line exception. Step 2 gains:
`make staticcheck` passes with them.

### C3

Unchanged: one capture-enabled `make live-grok`, on the owner's word. It runs after C2.

## Implementation Steps

### Rules for every phase

The rules of the sibling plan apply unchanged: files named at the start, every new test seen
failing first on a scratch copy, the gates, the guard's redact preview, and each phase staged for
the owner with its execution record. No phase runs an agent CLI except C3, on the owner's word.

### C1 — Conformance and the SDK's helpers (D26)

**Files:** `internal/provider/acpagent/session.go`, `acpagent.go`, and their tests (measured
first); this plan.

1. **Tests first:**
   * `ReadTextFile` of a missing path returns an `*acp.RequestError` with code -32002; a
     permission error keeps today's message.
   * The MCP entries, permission outcomes and config requests marshal to the same JSON as today
     (golden bytes from today's code, captured before the change).
   * A capture that dropped tap bytes logs the count at close.
   * A `ProtocolVersionError` at `initialize` reports the agent's and Magic's versions.
   * A `subagent_update` that reaches the client is dropped by the SDK before Magic's handler
     (a test through `acp.NewClientSideConnection`), so the branches can go.
2. **Code** as tested: `acp.NewResourceNotFound`; `NewMcpServerHttp`, `NewMcpServerSse`,
   `NewRequestPermissionOutcomeCancelled`, `…Selected`, `NewSetSessionConfigOptionRequestValueId`,
   `…Boolean`; `proc.DroppedTapBytes()`; `errors.As(err, *acp.ProtocolVersionError)`; the
   branches removed.
3. Gates.

### C2 — Per-turn token usage (D25)

**Files:** `internal/provider/acpagent/session.go`, `xaiusage.go`, their tests; this plan.

1. **Measure** from the committed captures (`grok/testdata/wire/1.0.50`) whether grok's `_meta`
   usage arrives before the prompt's answer, which decides how "the turn already reported xAI
   usage" is known.
2. **Tests first:**
   * A shape-pin test: `PromptResponse.Usage` decodes from a fixture with every field.
   * A turn whose answer carries usage emits one `event.Usage` with input, output, reasoning and
     both cache counts.
   * A turn that reported xAI usage, and whose answer also carries usage, emits one usage event,
     the xAI one.
   * A turn with neither emits none, as today.
   * The context gauge (`usage_update`'s used and size) is not overwritten.
3. **Code** as tested; `xaiusage.go`'s comment no longer says ACP's usage has no cache fields.
4. Gates.

### C3 — One live grok run, with wire capture (owner's word for the run)

**Files:** `internal/provider/grok/testdata/wire/<grok version>/` (new captures), this plan, and
`0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md` (its status).

1. Ask the owner for the run. Then run `make live-grok` with the wire capture enabled, as 0184's
   confirmation names it (the exact switch is read from the Makefile and `wirecap` first).
2. **Expected:**
   * the suite passes;
   * the capture holds permission, cancel, terminal, file-system, delete and resume frames;
   * whether grok's prompt answers carry the standard `usage`, and that the session's usage
     events are not doubled.
3. The captures are redacted by `wirecap`'s rules and the guard's preview, then committed under
   the grok version the run reports. 0184's PLAN becomes `complete` with a dated note.

### C4 — Record corrections (D27)

**Files:** `0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md`,
`docs/reports/0167-REPORT-acp-go-sdk2-integration-assessment.md`, the 0207 MADR; this plan.

1. 0195: a dated note that the SDK gates the subagent variants since `v1.15.2`; the deferral
   stands.
2. 0167-REPORT: dated notes on L3 (`providers/*` configures an agent's LLM providers; the outcome
   stands) and L4 (corrected by 0184 PLAN A5).
3. 0207 MADR: G6 and the Consequences' one-pin line annotated as no longer true, citing gobble's
   `go.mod`.
4. `make check-records`, `make markdownlint-docs`, the guard's preview.

## Verification

* C1, C2: their fail-first tests, quoted in the records; the gates.
* C3: the live run's result and the committed captures.
* C4: the notes, and `make check-records`.

## Rollout and Rollback

* **Rollout.** With the next tag.
* **Rollback.** Revert C1 or C2 alone; neither depends on the other. C3 adds test data only. C4 is
  records only.

## Execution record

Nothing has run. The plan waits for the owner's approval.

### C4 alone, approved 2026-10-27; C1 to C3 deferred

The owner asked "what is pending in the madr?", and answered two questions:

* **D24 to D26:** "Assess v1.21–v1.24 first (Recommended)". `acp-go-sdk2` has shipped `v1.21.0`
  to `v1.24.0` since this plan was read at `v1.20.0`. So C1, C2 and C3 do not run until a later
  amendment of the MADR has read those releases, and this plan is re-read against the newest one.
* **D27:** "Do it now, records only (Recommended)", with a re-check of gobble's pin. That is C4,
  which depends on no SDK version.

### C4, 2026-10-27: record corrections

* **gobble's pins,** re-read through `gh api repos/maccavelli/gobble-cli/contents/go.mod` at its
  default branch, `6ee20140` (2026-10-23): `acp-go-sdk2 v1.20.0`, as Magic; `go-selfupdate-lib
  v1.17.0`.
* **Steps 1 to 3, the dated notes:**
  * [0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md](0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md),
    under "The SDK does not gate these variants": no longer true since `v1.15.2` (0184 G3); the
    deferral stands.
  * [0167-REPORT-acp-go-sdk2-integration-assessment.md](../reports/0167-REPORT-acp-go-sdk2-integration-assessment.md):
    L3's `providers/*` row, and L4's "Outbound requests are not validated" (0184 PLAN A5).
  * The MADR: the one-pin fact under "acp-go-sdk2, `v1.15.0` → `v1.19.0`", G6, and the
    Consequences' one-pin line.
* **Step 4:** `make check-records`, `make markdownlint-docs` and the guard's preview, with the 0207
  records' gates (the sibling plan's U6b record).

The plan is `in-progress`: C4 is done, and C1 to C3 wait for the assessment.

### C0 start, 2026-10-28

The owner committed amendment H and this revision as `f2e5aa43` and answered "committed proceed".

* **Step 1.** `go get github.com/maccavelli/acp-go-sdk2@v1.25.0`: "go: upgraded
  github.com/maccavelli/acp-go-sdk2 v1.20.0 => v1.25.0". `go mod tidy`, then `go mod tidy -diff`:
  no diff. `go.mod` 1 line, `go.sum` 2.
* **Step 2.** `make staticcheck` (`leftovers/c0_staticcheck_before.log`), compared by
  `leftovers/sa1019_compare.py` with the pin probe's log: "58 distinct SA1019 uses (23 outside
  tests, 35 in tests); other findings: 0", the same set as H-2's, on 37 distinct lines. No
  deviation.

### C0 Deviation 1, 2026-10-28 — a file-wide ignore hides every SA1019 in its file. The owner decided: "File-wide + gate (Recommended)"

* **What was found,** before step 3, on scratch copies of the working tree
  (`leftovers/c0_directive_probe.py`), each with a planted `strings.Title` in `plans.go`:
  * `//lint:file-ignore SA1019` at the top of `plans.go`: the planted use was **not** reported;
    staticcheck said only "func plantedTitle is unused (U1000)";
  * `//lint:ignore SA1019` above `plans.go:21` alone: that one line was silenced, and
    "strings.Title has been deprecated since Go 1.18 …" was still reported.

  So D45's file-wide form in `plans.go` and `plans_test.go`, and its rule that a planted use of
  another deprecated name there still fails, could not both hold. Step 4's first plant would have
  passed.
* **What makes a gate possible.** `staticcheck -show-ignored -f json` reports each ignored
  finding with `"severity": "ignored"` and its full message
  (`leftovers/c0_showignored_probe.py`): the planted `strings.Title` came back as
  `{"code": "SA1019", "severity": "ignored", … "strings.Title has been deprecated …"}`.
* **Offered:** file-wide plus a gate (recommended); line directives only, 37 of them, within C0's
  files; or both.
* **Decided:** file-wide plus a gate. `make staticcheck` runs, for each GOOS, a new
  `scripts/check_staticcheck_ignored.py` after staticcheck. It fails on:
  * any ignored finding other than SA1019 on an `acp-go-sdk2` declaration carrying the
    unstable-schema notice;
  * any `//lint:ignore` or `//lint:file-ignore` in a tracked Go file whose checks are not exactly
    `SA1019`, or whose reason does not name "0207 amendment H".
* **Files added to C0:** `Makefile` and `scripts/check_staticcheck_ignored.py`. CI's step runs
  `make staticcheck` on Ubuntu, where `python3` is present, so `ci.yml` is unchanged. Python's
  ignore rules are already in `.gitignore` (0174 P4).
* **Step 4 grows:** the gate is seen failing on the plants, and on a directive with a wrong check
  or reason. The MADR's D45 has a dated note.

### C0 Deviation 2, 2026-10-28 — the Linux race gate fails on a race older than C0. The owner decided: "Fix under 0208, first (Recommended)"

* **What was found.** Step 6's `go test -race ./...` on the Linux test host (a scratch copy of the
  C0 tree) failed: "WARNING: DATA RACE", "--- FAIL: TestSilentCommandIsMarked (0.08s)". The
  write is `closechan` in the fake session's `Close` (`internal/session/silent_test.go:95`, called
  from the test's cleanup through `Manager.CloseAll`). The read is the `chansend` of its `Prompt`
  goroutine (`:85`).
* **It is not C0's.**
  * The fake never touches `acp-go-sdk2`, and C0 changes nothing it uses.
  * On the Linux test host (`leftovers/silent_race_linux.py`, 300 `-race` runs of the one test):
    * at `f2e5aa43`, before C0: one failure, the same race with the same frames;
    * on the C0 tree: the same race once, plus one "silent_test.go:203: timed out waiting for
      /quiet turn", not seen at `f2e5aa43`.
* **Decided:** the race is fixed under
  [0208-PLAN-make-the-provider-layer-agent-neutral.md](0208-PLAN-make-the-provider-layer-agent-neutral.md),
  whose P4a wrote the fake, as its new P4c. It is staged and committed before C0, as 0174 P6 was
  before 0077's P10. P4c's repeated runs also show whether the timeout is a separate fault. C0's
  files are unchanged, and its Linux race gate runs again on top of P4c.

### C0, 2026-10-28: the pin and the unstable-name exception, steps 3 to 6

0208's P4c was committed as `7abe6956` ("committed proceed"), and C0's gates ran again on top of
it.

**Step 3, the directives,** each reason naming its feature MADR and "0207 amendment H":

| File | Form | Lines |
| :--- | :--- | :--- |
| `plans.go` | `//lint:file-ignore SA1019`, after the package clause | its 11 |
| `plans_test.go` | `//lint:file-ignore SA1019`, after the package clause | its 17 |
| `acpagent.go` | `//lint:ignore SA1019` | the `Plan` advertisement |
| `session.go` | `//lint:ignore SA1019`, one per line | the 4 plan dispatch lines; the 3 subagent declines, which C1 deletes |
| `advertise_test.go` | `//lint:ignore SA1019` | the `subagents` example |

* A line directive covers only the line after it, so each case line and each call line has its
  own.
* The gate, `scripts/check_staticcheck_ignored.py` (Deviation 1), is run by `make staticcheck`
  after each GOOS pass. It prints, for linux, darwin and windows: "check_staticcheck_ignored: 58
  ignored finding(s), each an acp-go-sdk2 unstable-schema name". That is H-2's 58, every one now
  ignored and none left reported.

**Step 4, seen to fail** (`leftovers/c0_plants.py`). Each plant was in a scratch copy of the
working tree, made a git tree, through `make staticcheck`:

| Plant | `make staticcheck` | The line |
| :--- | :--- | :--- |
| `strings.Title` inside the file-wide-ignored `plans.go` | exit 2 | "ignored finding outside MADR 0207 amendment H (D45): …plans.go:83: ignored SA1019: strings.Title has been deprecated …". staticcheck itself reported nothing; the gate alone caught it |
| `strings.Title` in `acpagent.go`, beside the line-ignored `Plan` | exit 2 | "acpagent.go:1100:6: strings.Title has been deprecated …", from staticcheck |
| `//lint:ignore SA1019 … 0207 amendment H` over `strings.Title` in `session.go` | exit 2 | "ignored finding outside … session.go:2602: ignored SA1019: strings.Title …" |
| `//lint:ignore U1000 … 0207 amendment H` | exit 2 | "lint directive outside MADR 0207 amendment H (D45): …plans.go:61: //lint:ignore U1000 …" |
| a directive whose reason drops "0207 amendment H" | exit 2 | "lint directive outside … session.go:2074: //lint:ignore SA1019 declines …" |
| none (the control) | exit 0 | "58 ignored finding(s), each an acp-go-sdk2 unstable-schema name" |

* **The first plant needed a second try.** My first version used `strings` in `plans.go` without
  importing it, and failed for that: "undefined: strings (compile)". With the import it failed as
  listed.

**Step 5.**
[0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md)
has its amendment of 2026-10-28, F15 and D5 amended, with a pointer from D5's own line. The MADR's
D45 has Deviation 1's note.

**Step 6, the gates,** on the tree that will be committed, with `7abe6956` beneath it:

| Gate | Result |
| :--- | :--- |
| `make pre-add-check FILES=…` (the five Go files) | "5 file(s) clean (gofmt, golint, govulncheck)" |
| `make staticcheck` | exit 0, the gate's line for each of the three GOOS |
| `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `go mod tidy -diff` | exit 0, no diff |
| the workflow pin tests | ok |
| `make test`, `make race` | exit 0; 44 packages ok |
| `go test -race -count=1 ./...` on the Linux test host, in a scratch copy removed afterwards | exit 0; 44 packages ok. The run before P4c failed (Deviation 2) |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| the guard's redact preview | "would edit 0 file(s)" |

**Files for this commit:** `go.mod`, `go.sum`, `Makefile`, `scripts/check_staticcheck_ignored.py`,
the five `internal/provider/acpagent` files above, the 0170 MADR, the 0207 MADR, this plan.

**C0 is done** once the owner commits it. Next by this plan: C1, then C1b and C2. C3's live run
waits for the owner's word.

### C1, 2026-10-28: conformance and the SDK's helpers (D26)

The owner committed C0 as `cb6781dc` and answered "committed proceed". Every site was re-read at
`cb6781dc` first. They had moved again with C0's directives: permission outcomes at
`session.go:2476-2490` and `:2616-2630`, config requests at `:1108-1114` and `:1165-1180`, the
subagent branches at `:2074-2082`, `ReadTextFile` at `:2655`.

**Step 1, the tests,** in two new files beside the package's own:

| Test | What it holds |
| :--- | :--- |
| `TestMcpServersKeepTheirBytes`, `TestPermissionOutcomesKeepTheirBytes`, `TestConfigRequestsKeepTheirBytes` (`protocolvalues_test.go`) | the JSON of the MCP entries, both permission outcomes (from auto-approval and from a person), and the three `session/set_config_option` requests as the agent reads them off the wire |
| `TestReadTextFileMissingIsResourceNotFound` (`conformance_test.go`) | a missing file answers `acp.IsResourceNotFound`, a `*acp.RequestError` naming the path |
| `TestReadTextFilePermissionKeepsItsMessage` | an unreadable file still says the daemon "lacks OS permission" |
| `TestCloseCaptureNamesDroppedBytes` | a capture that lost output warns with `dropped_bytes`; a whole one logs nothing |
| `TestInitializeNamesAProtocolVersionMismatch` | through a real client connection, an agent answering protocol version 2 is named, with both versions and "update", and the `*acp.ProtocolVersionError` stays in the chain |
| `TestSubagentUpdatesStopAtTheSDK` | on a wire session with Magic's own initialize, the three subagent variants never reach Magic's handler, and `Connection().RefusedMessages()` rises by 3 |

* **The golden bytes were captured from the code before the change.** The golden test was written
  first and run against `cb6781dc`'s code. My first guess at the bytes had the wrong key order (the
  SDK writes keys in alphabetical order). The constants were set to what that code wrote, and the
  test then passed before any code changed.
* **`ReadTextFile` had no test before.** The permission case is the first.

**Step 2, the code:**

* `ReadTextFile` returns `acp.NewResourceNotFound(path)` for `os.ErrNotExist`, after the
  permission check, which is unchanged.
* `NewMcpServerHttp`, `NewMcpServerSse`, `NewRequestPermissionOutcomeCancelled`, `…Selected`,
  `NewSetSessionConfigOptionRequestValueId` (two sites) and `…Boolean` replace the hand-built
  values.
* `closeCapture(log, wire, dropped)` closes the capture and warns when `proc.DroppedTapBytes()`
  reports lost output, at both places the capture closed: the exit watcher after `Wait`, and
  `killTree`.
* `initializeError` wraps a failed `initialize`. An answered `*acp.ProtocolVersionError` reads:
  "acp initialize: the agent answered ACP protocol version 2, and Magic speaks 1; update the agent
  or mcremote so they speak one version: …".
* **The subagent branches are gone,** with their three directives. `default` now declines by
  `u.Kind()`, which names every variant, unstable ones too, without reading a deprecated field.
  * This keeps 0184 A4's `TestEverySessionUpdateIsMappedOrDeclined` as it was. It hands each
    variant to Magic's handler directly, past the SDK, and requires a decline *by name*. Without
    `Kind`, the three would have been declined as "empty".
  * It is within C1's files. `Unknown` keeps its own branch and its "unknown:" name until C1b.

**Seen failing** (`leftovers/c1_plants.py`; a scratch copy of `HEAD` with the working tree's four
files, one break each):

| Plant | The failure |
| :--- | :--- |
| the `ErrNotExist` branch removed | "err = open …/missing.txt: no such file or directory, want resource-not-found (-32002)" |
| the SSE entry built with `NewMcpServerHttp` | "MCP servers:" with the differing bytes |
| `closeCapture` never warns | "records = 0, want 1" |
| `initializeError` never names the mismatch | "message \"acp initialize: acp: agent answered protocol version 2 to a request for 1\" does not say \"speaks 1\"", and "… \"update\"" |
| the client advertises `subagents` | "Magic's handler saw and declined [subagent_update session_message session_message_chunk]; the SDK should have refused them" |
| `default` declines "empty" | `sessionupdate_test.go:156: declined [empty], want [subagent_update] (once each)` |
| none (the control) | exit 0 |

The fourth plant shows what was there before: the SDK's own message already named both versions,
but not which side, or what to do.

**Step 3, the gates:**

| Gate | Result |
| :--- | :--- |
| `make pre-add-check FILES=…` (the four Go files) | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `make staticcheck` | exit 0; the gate: "55 ignored finding(s), each an acp-go-sdk2 unstable-schema name", 3 fewer than C0's 58 |
| `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok |
| `go test -race -count=1 ./...` on the Linux test host, in a scratch copy removed afterwards | exit 0; 44 packages ok |
| `make check-records`, `make markdownlint-docs`; the guard's redact preview | exit 0; "0 issues"; "would edit 0 file(s)" |

**Files for this commit:** `internal/provider/acpagent/session.go`, `acpagent.go`,
`conformance_test.go` (new), `protocolvalues_test.go` (new); this plan.

**C1 is done** once the owner commits it. Next: C1b, then C2.
