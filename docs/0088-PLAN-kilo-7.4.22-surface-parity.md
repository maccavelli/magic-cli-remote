---
status: draft
date: 2026-08-14
associated-madr: "0088-MADR-kilo-7.4.22-surface-parity.md"
owner: Implementer
target-milestone: after 0088 acceptance
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Pin Kilo known-good to 7.4.22 and close session-loop gaps

## Executive Summary & Goal

* **Associated Decision Record**:
  [0088-MADR-kilo-7.4.22-surface-parity.md](./0088-MADR-kilo-7.4.22-surface-parity.md)
* **Goal**: Make 7.4.22 the dialect’s known-good release and land only
  the session-loop deltas D1–D5 / D4 command-table flips. Worktrees,
  PTY, Cloud, Agent Manager, and `allow-everything` stay out (D7–D11).
* **Success Criteria**:
  * [ ] `KnownGoodVersion == "7.4.22"`; this host’s health check logs
        info, not warn
  * [ ] `/review`, `/resume-claude`, `/resume-codex` are advertised and
        routed through `POST /session/{id}/command`
  * [ ] 0087 chrome + durable-envelope tests remain green
  * [ ] `live_kilo` prompt/permission pass on a host with a usable model
  * [ ] 7.4.22 path/agent/command probe notes exist under
        `docs/kilo-spike-7.4.22/` (thin; do not copy the multi-MB
        catalog)

## Prerequisites & Dependencies

* **Infrastructure**: `kilo` 7.4.22 on PATH (this host already).
* **Credentials**: a non-paid-blocked model for Task 4.3 (`kilo-auto/free`
  returned `PAID_MODEL_AUTH_REQUIRED` on 2026-08-14).
* **Pre-Flight Checks**:
  * [x] Live `/doc` path diff vs 7.4.20 spike (+13 / −0)
  * [x] Live `GET /command` and `kilo debug agent code`
  * [x] Release notes 7.4.21 / 7.4.22 and official CLI / Skills docs
  * [x] 0087 D6 marked superseded

## Architecture & Technical Design Summary

No new packages. Touch list for the pin:

| File | Change |
| --- | --- |
| `internal/provider/kilo/version.go` | `KnownGoodVersion = "7.4.22"` + comment → 0088 |
| `internal/provider/kilo/commandtable.go` | `review` → `KindOp`/`Op` via command submit; add resume-import rows if they are canonical or live-only advertised |
| `internal/provider/kilo/command.go` | live catalog already advertises whatever `GET /command` returns — confirm resume-* flow through `submitCommand` |
| `docs/0075-MADR-kilo-cli-provider.md` | one-line erratum: known-good now 7.4.22 per 0088 |
| `docs/kilo-spike-7.4.22/` | README + path list + agents/commands JSON (not `/provider`) |

0087’s `session.go` / `dialect.go` / `command.go` chrome+decode work is
a prerequisite, not this plan’s code.

```
GET /command  ──advertise──►  phone slash list
POST /session/{id}/command    review | resume-claude | resume-codex | init
KnownGoodVersion              7.4.22  ◄── GET /global/health
```

## Phased Implementation Plan

### Phase 1: Setup & Groundwork

* **Objective**: Pin the probe so the version bump is evidence-backed.
* **Tasks**:
  - [x] **Task 1.1**: Write `docs/kilo-spike-7.4.22/README.md` with
        health, path counts, added paths, live command list, `code`
        tool map, sandbox/auth notes (copy from 0088 §More Information).
  - [x] **Task 1.2**: Save `openapi-paths.txt` (names only) from the
        7.4.22 `/doc` dump. Do not commit `/provider`.
  - [x] **Task 1.3**: Unit test: `OnHealthy` of a 7.4.22 body records
        the version and does not fail boot (`TestOnHealthyRecords7422`).
        The `KnownGoodVersion == "7.4.22"` assert lands with the P2 bump.

### Phase 2: Core Implementation

* **Objective**: D1 + D4.
* **Tasks**:
  - [x] **Task 2.1**: Bump `KnownGoodVersion` and comments (D1).
  - [x] **Task 2.2**: `review` in `CommandTable` is no longer
        `KindNone`. Route like `init` (engine `POST …/command`) unless
        a dedicated op already exists.
  - [x] **Task 2.3**: Ensure `resume-claude` / `resume-codex` survive
        `advertiseCommands` (they will if live `GET /command` lists
        them). Add table entries only if the canonical vocabulary
        needs them; otherwise live advertise is enough.
  - [x] **Task 2.4**: 0075 known-good line erratum pointing at 0088.

### Phase 3: Integration, Telemetry & Fallbacks

* **Objective**: Do not accidentally take D7–D11.
* **Tasks**:
  - [ ] **Task 3.1**: No `allow-everything`, no worktree/sandbox/PTY
        client, no `session.next.text.delta` handler in this plan.
  - [ ] **Task 3.2**: Keep `session.diff` silent (0087 D2).

### Phase 4: Verification, Migration & Cutover

* **Objective**: Prove the pin.
* **Tasks**:
  - [ ] **Task 4.1**: `make pre-add-check FILES=` on touched Go files.
  - [ ] **Task 4.2**: `go test -race ./internal/provider/kilo/`.
  - [ ] **Task 4.3**: `go test -tags live_kilo` permission + prompt
        when a usable model exists.
  - [ ] **Task 4.4**: One commit after 0088 is accepted. Do not push
        until asked.

## Verification & Testing Strategy

| Test Level | Scope | Method | Pass |
| --- | --- | --- | --- |
| Unit | version pin, command table | `go test ./internal/provider/kilo/` | review not KindNone; version 7.4.22 |
| Race | kilo package | `go test -race` | clean |
| Live | real engine | `-tags live_kilo` | prompt streams; bash ask auto-approves |
| Probe | `/doc` vs spike | path-count 256, +13/−0 | still holds on this host |

## Rollback & Mitigation Procedures

* **Trigger**: 7.4.22-only decode breaks a 7.4.20 leftover, or `/review`
  400s on `POST …/command`.
* **Rollback**:
  1. Revert the version constant (skew warning returns — noisy but safe).
  2. Revert command-table `review` to `KindNone` if the engine route
     rejects it.
  3. Leave 0087 chrome/decode in place; those frames exist on 7.4.20
     too (`sse-or.raw`).

## Task Progress Checklist

- [x] **Phase 1: Setup & Groundwork**
  - [x] Task 1.1: 7.4.22 spike README
  - [x] Task 1.2: path list artifact
  - [x] Task 1.3: version unit test (`TestOnHealthyRecords7422`; pin assert in P2)
- [x] **Phase 2: Core Implementation**
  - [x] Task 2.1: KnownGoodVersion
  - [x] Task 2.2: review command
  - [x] Task 2.3: resume-import advertise
  - [x] Task 2.4: 0075 erratum
- [ ] **Phase 3: Integration & Fallbacks**
  - [ ] Task 3.1: no D7–D11 scope creep
  - [ ] Task 3.2: session.diff stays silent
- [ ] **Phase 4: Verification**
  - [ ] Task 4.1: pre-add-check
  - [ ] Task 4.2: race
  - [ ] Task 4.3: live_kilo
  - [ ] Task 4.4: commit when accepted
