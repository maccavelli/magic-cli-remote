---
status: completed
date: 2026-08-14
associated-madr: "0087-MADR-kilo-session-chrome-and-permission-decode.md"
owner: Implementer
target-milestone: 2026-08-14
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Filter Kilo session chrome and decode durable permission frames

## Executive Summary & Goal

* **Associated Decision Record**:
  [0087-MADR-kilo-session-chrome-and-permission-decode.md](./0087-MADR-kilo-session-chrome-and-permission-decode.md)
* **Goal**: Make a prompted Kilo session on 7.4.20–7.4.22 show only
  conversation text, stop dumping snapshot diffs into chat, and let auto
  mode answer bash/grep asks that arrive on either SSE envelope.
* **Success Criteria**:
  * [x] Live-shaped transient parts (dotted `kilocode.lifecycle`,
        `synthetic`, “Initializing session/snapshot”) never become
        `TypeAssistantChunk`
  * [x] `session.diff` with a `patch` emits no transcript event
  * [x] Durable `{payload:{type,data}}` `permission.asked` decodes with
        a non-empty session id and a normalizable ask
  * [x] Resume replay drops the same chrome
  * [x] `go test -race ./internal/provider/kilo/` green;
        `make pre-add-check FILES=` clean on touched Go files

This plan is the execution record of MADR 0087 D1–D6. The code landed
in the working tree on 2026-08-14 (not yet committed at plan-write
time).

## Prerequisites & Dependencies

* **Infrastructure / Credentials**: none for the unit work. A live
  `live_kilo` tool/permission turn needs a model that is not
  `PAID_MODEL_AUTH_REQUIRED` (the 2026-08-14 probe’s `kilo-auto/free`
  was not).
* **Dependencies / Upgrades**: none. Engine stays whatever is on PATH.
  D6 leaves `KnownGoodVersion` at 7.4.20.
* **Pre-Flight Checks**:
  * [x] Re-read 0075 §2.4, 0076 session-loop claim, kilo
        `session.go` / `dialect.go` / `permission.go` / `command.go`
  * [x] Confirm the 7.4.20 spike dotted key in
        `docs/kilo-spike-7.4.20/sse-or.raw`
  * [x] Boot kilo 7.4.22, dump `/doc` permission + `SnapshotFileDiff`
        + `Part` schemas, list `code` agent bash/grep rules
  * [x] Confirm httpagent drops `sid == ""` (`httpagent/provider.go`
        SSE pump)

## Architecture & Technical Design Summary

```
kilo serve  --/global/event-->  httpagent SSE pump
                                    | DecodeFrame (kilo dialect)
                                    |   properties OR data -> props
                                    |   sessionIDOf(props)
                                    |   sid=="" => DROP
                                    v
                              httpSession.HandleEvent
                                    |
                    +---------------+----------------+
                    |               |                |
           message.part.updated   session.diff   permission.asked
                    |               |                |
           isKiloChromePart?      no-op          emitPermissionAsk
             yes: mark transient                 (0044 auto path)
             no: text / tool
```

No new types, endpoints, or config keys. Helpers live next to the
call sites:

| Helper | File | Role |
| --- | --- | --- |
| `firstRaw` | `dialect.go` | first non-empty, non-null JSON blob |
| `isKiloChromePart` | `session.go` | D1 predicate |
| `kiloLifecycleIsTransient` | `session.go` | dotted + nested metadata |
| `looksLikeKiloLifecycleText` | `session.go` | spinner heuristic |
| `handleSessionDiff` | `command.go` | D2 no-op |

`Replay` uses the same `isKiloChromePart` predicate so a resume cannot
re-inject chrome the live path just dropped.

## Phased Implementation Plan

### Phase 1: Foundation & Setup

* **Objective**: Pin the live shapes with tests that fail on the
  pre-fix decoder/filter.
* **Tasks**:
  - [x] **Task 1.1**: Add `TestDecodeFrameDurableDataEnvelope` against
        a wrapped `payload.data` `permission.asked`.
  - [x] **Task 1.2**: Extend `TestTransientLifecyclePartsFiltered`
        with the spike dotted-key + `synthetic` frame and a
        metadata-less “Initializing session” frame (the original case
        stays for the nested object).
  - [x] **Task 1.3**: Add `TestInitializingSpinnerDoesNotRepeat`,
        `TestSessionDiffDoesNotEmitNotice`,
        `TestReplaySkipsInitializingChrome`.

### Phase 2: Core Implementation

* **Objective**: D1–D4 in the dialect.
* **Tasks**:
  - [x] **Task 2.1**: `DecodeFrame` reads `properties` and `data` on
        both the wrapper and the bare frame (D3).
  - [x] **Task 2.2**: Replace the nested-only metadata struct with
        `synthetic` / `ignored` / raw `metadata`; route chrome
        through `isKiloChromePart` and mark `partType=transient`
        so later deltas are dropped (D1).
  - [x] **Task 2.3**: Apply the same predicate in `Replay` (D1).
  - [x] **Task 2.4**: `handleSessionDiff` becomes a no-op (D2).
        Keep `httpSession.Diff` untouched.
  - [x] **Task 2.5**: Add `session.next.synthetic`, `file.edited`,
        and the other D4 events to the ignore list;
        extend `TestIgnoredKiloEventsEmitNothing`.
  - [x] **Task 2.6**: Map tool name `shell` to kind `execute`
        (display only; no new permission path).

### Phase 3: Integration, Telemetry & Fallbacks

* **Objective**: Document the 7.4.22 findings that we are **not**
  taking (D5, D6) so a later pass does not rediscover them as gaps.
* **Tasks**:
  - [x] **Task 3.1**: Do **not** call
        `POST /permission/allow-everything` from `SetMode(auto)`.
        Record the 401-without-Basic probe in the MADR (D5).
  - [x] **Task 3.2**: Do **not** handle `session.next.text.delta` or
        `session.next.tool.*` (dual-emit risk). Record in D4.
  - [x] **Task 3.3**: Do **not** bump `KnownGoodVersion` (D6). The
        existing `OnHealthy` skew warning remains the signal.

### Phase 4: Migration, Cutover & Verification

* **Objective**: Prove the unit net and leave the tree committable.
* **Tasks**:
  - [x] **Task 4.1**: `gofmt` + `make pre-add-check FILES=` on
        `session.go`, `dialect.go`, `command.go`, and the two test
        files.
  - [x] **Task 4.2**: `go test -race ./internal/provider/kilo/`
        (2026-08-14: ok).
  - [ ] **Task 4.3**: `go test -tags live_kilo ./internal/provider/kilo/
        -run 'LivePermission|LivePrompt' -count=1` when a non-paid
        model is available. **Not run** on 2026-08-14
        (`PAID_MODEL_AUTH_REQUIRED` on `kilo-auto/free`).
  - [x] **Task 4.4**: Author this PLAN + MADR 0087.

## Verification & Testing Strategy

| Test Level | Scope & Target | Execution Method | Passing Requirement |
| --- | --- | --- | --- |
| **Unit** | D1 chrome filter, D2 silent diff, D3 data envelope, D4 ignore list, Replay | `go test ./internal/provider/kilo/` | 100% of new cases pass |
| **Race** | same package | `go test -race ./internal/provider/kilo/` | clean |
| **Pre-add** | touched Go files | `make pre-add-check FILES="…"` | gofmt + golint clean; govulncheck warn-only if offline |
| **Live** (deferred) | real bash ask + auto-approve + no spinner in chunks | `go test -tags live_kilo` | permission round-trip and PONG turn contain no “Initializing” chunk |

Existing auto-approve tests (`autoapprove_test.go`, `automode_test.go`)
are unchanged and still require that a decoded `permission.asked`
reaches `emitPermissionAsk`. D3 is what makes that true for the
durable envelope.

## Rollback & Mitigation Procedures

* **Trigger**: a Kilo session loses real assistant text, drops a
  legitimate permission sheet, or stops showing an explicit Diff
  the user requested from the session-actions menu.
* **Rollback steps**:
  1. Revert the kilo dialect commits (`session.go`, `dialect.go`,
     `command.go`, the two `*_test.go` files). No config or protocol
     revert is required.
  2. If only D2 is wrong (user wanted live diff notices), restore
     the previous `handleSessionDiff` body; leave D1/D3 in place.
  3. If only the text heuristic is too aggressive, delete
     `looksLikeKiloLifecycleText` from `isKiloChromePart` and keep
     the metadata/`synthetic` matches.
* **Mitigation without revert**: operator can switch off kilo or
  pick a different provider. Auto mode can be left off; permissions
  then surface as sheets **once D3 is in** (without D3 the sheet
  never appears either).

## Task Progress Checklist

- [x] **Phase 1: Foundation & Setup**
  - [x] Task 1.1: Durable-envelope decode test
  - [x] Task 1.2: Live-shaped transient filter tests
  - [x] Task 1.3: Spinner / session.diff / Replay tests
- [x] **Phase 2: Core Implementation**
  - [x] Task 2.1: `DecodeFrame` `data` fallback (D3)
  - [x] Task 2.2: `isKiloChromePart` on `message.part.updated` (D1)
  - [x] Task 2.3: Replay uses the same predicate (D1)
  - [x] Task 2.4: `handleSessionDiff` no-op (D2)
  - [x] Task 2.5: D4 ignore list
  - [x] Task 2.6: `shell` → execute kind
- [x] **Phase 3: Integration, Telemetry & Fallbacks**
  - [x] Task 3.1: leave `allow-everything` unused (D5)
  - [x] Task 3.2: leave `session.next.text.delta` unmapped (D4)
  - [x] Task 3.3: leave `KnownGoodVersion` at 7.4.20 (D6)
- [x] **Phase 4: Migration, Cutover & Verification**
  - [x] Task 4.1: pre-add-check
  - [x] Task 4.2: race suite
  - [ ] Task 4.3: live_kilo permission/prompt (blocked: paid-model auth)
  - [x] Task 4.4: MADR + PLAN
