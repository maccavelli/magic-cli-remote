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
amendment naming the exception); this plan.

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
