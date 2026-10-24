---
status: proposed
date: 2026-10-23
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

* The pin stays `acp-go-sdk2 v1.20.0` (D24); nothing here moves it.
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
