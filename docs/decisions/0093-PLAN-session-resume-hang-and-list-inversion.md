---
status: complete
date: 2026-08-16
associated-madr: "0093-MADR-session-resume-hang-and-list-inversion.md"
owner: [Implementer]
target-milestone: [Phase 5 complete — operator owns daemon/APK install]
---

# Implement session resume hang and list inversion

Associated MADR: [0093-MADR-session-resume-hang-and-list-inversion.md](0093-MADR-session-resume-hang-and-list-inversion.md)

## Goal

Make Resume return or fail, keep End session a real delete, and make
`session.list` after a daemon restart show this device's soft-closed
rows (newest first) instead of a resurrected kilo and a missing goose.

## Scope

**In:**

* `internal/session/manager.go` — Create mode-restore order; `purged`;
  `ListSnapshot` sort + `UpdatedAt`
* `internal/session/store.go` — skip logs
* `internal/session/manager_lifecycle_test.go`
* `internal/session/manager_persist_race_test.go`
* `internal/session/manager_durable_test.go`
* `apps/mobile/lib/data/protocol/models.dart` — `updatedAt`
* `apps/mobile/lib/features/sessions/sessions_screen.dart` — sort

**Out:**

* 0089 D3 boot rehydrate of live work
* 0068 transport resume
* Re-pair ownership transfer
* Provider `Attach()` API
* Durable on-disk delete tombstones
* Changing End session to `session.close`

## Implementation Steps

### Phase 0 — forensics (done)

Recorded in the MADR More Information section. Do not re-litigate
unless a new log line contradicts it.

* [x] Read `~/Library/Logs/mcremote/mcremote.err.log` for 2026-08-15
      22:12 → 2026-08-16 08:33
* [x] Confirm kilo `1b297add` `purge=true` at 23:54:12 (End session)
* [x] Confirm grok `8b85a721` + goose `9c635acc` `purge=false` at 01:23
* [x] Confirm 08:31 grok `acp session loaded` with **no** `session created`
* [x] Confirm phone re-auth as `4f8ab207-…` (pairing survived the APK)
* [x] Walk `~/.local/share/mcremote/sessions` owners vs `visibleTo`
* [x] Note: no adb / phone logcat

### Phase 1 — D1 resume deadlock (done in tree)

1. In `Manager.Create`, delete the `SetMode` block that sat immediately
   after `p.Start`.
2. After `go m.pump(runCtx, sess)`, restore persisted `opts.ModeID`
   (same debug-on-error behaviour).
3. Comment must name `acpagent.attached` and `httpagent.Emit`.
4. Add `TestCreateRestoresModeAfterPumpStarts`: 1-slot `Events` chan
   filled in Start; `SetMode` does a blocking `TypeMode` send; Create
   with `ModeID=auto` must return in 2 s.

**Gate:** `go test ./internal/session/ -run TestCreateRestoresModeAfterPumpStarts`

### Phase 2 — D3 persist resurrection (done in tree)

1. Add `purged map[string]struct{}` on `Manager`, init in
   `NewManagerWithLimits`.
2. `markPurged(id)` before every `store.Delete` in `closeMatching`
   (live and not-live purge paths).
3. `clearPurged(id)` after a successful insert in `Create`.
4. `writePersist`: if `purged[id]`, return without Save. After a
   successful Save, if `purged[id]` now, `store.Delete` again.
5. Keep `TestDeleteCancelsPendingDebouncedPersist`.
6. Add `TestDeleteWinsInFlightFlushPersist`: snapshot live meta,
   Delete, `writePersist(snapshot)`, `store.Get` must fail.

**Gate:** `go test ./internal/session/ -run 'TestDeleteWinsInFlightFlushPersist|TestDeleteCancelsPendingDebouncedPersist'`

Do **not** undo a stale flush with `store.Delete` based only on "id
left the live map." `CloseAll` persistNow writes *after* the id is
removed; that Delete would erase goose (the incident inversion).

### Phase 3 — D4 list (done in tree)

1. Add `UpdatedAt` to `session.Meta` (`json:"updated_at,omitempty"`).
   Fill it from `Record.UpdatedAt` in `ListSnapshot`.
2. Sort `ListSnapshot` with `sort.SliceStable`: live first, then
   `UpdatedAt` (fallback `CreatedAt`) descending, then id.
3. `store.List`: log `session list skipped unreadable meta` /
   `session list skipped corrupt meta` with `session_id` + `err`.
4. Add `TestCloseAllKeepsSessionsListable` (two Create, CloseAll, new
   Manager, both ids present, not live, newest first).

**Gate:** `go test ./internal/session/ -run TestCloseAllKeepsSessionsListable`

### Phase 4 — D6 phone sort (done in tree)

1. `SessionMeta.updatedAt` + `fromJson` `updated_at` + `copyWith`
   passthrough.
2. `_compareSessionsRecency` next to `_humanTimestamp`.
3. `_refresh` sorts the snapshot after applying in-flight status.

**Gate:** `dart format` + `dart analyze` on the two Dart files.

### Phase 5 — land the tree (done)

Install, LaunchAgent restart, phone APK, and C6 hardware are **out of
scope**. The operator builds and installs the daemons.

1. `make pre-add-check FILES="internal/session/manager.go internal/session/store.go internal/session/manager_lifecycle_test.go internal/session/manager_persist_race_test.go internal/session/manager_durable_test.go"`
2. `go test ./internal/session/` and `go test -race ./internal/session/ -run 'TestCreateRestoresModeAfterPumpStarts|TestDeleteWinsInFlightFlushPersist|TestCloseAllKeepsSessionsListable'`
3. `dart format --output=none --set-exit-if-changed` and `dart analyze`
   on the two Dart files.
4. Commit without `-m` (prepare-commit-msg), including the 0093 MADR/PLAN.

After this commit the operator installs `mcremote` (C7) and optionally
the phone APK (D6), then runs C6.

## Verification

| ID | Command / action | Pass |
| --- | --- | --- |
| C1 | `go test ./internal/session/ -count=1 -run TestCreateRestoresModeAfterPumpStarts` | [x] tree |
| C2 | `go test ./internal/session/ -count=1 -run TestDeleteWinsInFlightFlushPersist` | [x] tree |
| C3 | `go test ./internal/session/ -count=1 -run TestDeleteCancelsPendingDebouncedPersist` | [x] tree |
| C4 | `go test ./internal/session/ -count=1 -run TestCloseAllKeepsSessionsListable` | [x] tree |
| C5 | Inspect `store.List` skip logs (code review + skipped++ still set) | [x] tree |
| — | `go test ./internal/session/` | [x] 2026-08-16 |
| — | `go test -race ./internal/session/ -run 'TestCreateRestoresModeAfterPumpStarts\|TestDeleteWinsInFlightFlushPersist\|TestCloseAllKeepsSessionsListable'` | [x] 2026-08-16 |
| — | `make pre-add-check FILES="internal/session/manager.go internal/session/store.go internal/session/manager_persist_race_test.go internal/session/manager_durable_test.go"` | [x] 2026-08-16 |
| — | `dart analyze` on `models.dart` + `sessions_screen.dart` | [x] 2026-08-16 |
| C6 | Hardware: Resume `auto` grok after a long load; End kilo stays gone; restart host, goose+grok both listed newest-first | [ ] operator after install |
| C7 | Installed `mcremote version` ≠ `3ea8f9f` | [ ] operator after install |

### Hardware acceptance (C6) — exact script

1. Create grok, switch to **auto**, send enough traffic that a later
   `session/load` fills the event buffer (a multi-turn chat is
   enough; the 08:31 incident was overnight).
2. Create kilo, send one prompt, **End session**. Confirm the row
   disappears. Refresh. It must stay gone. Host log must show
   `purge=true` and must **not** grow a new `sessions/<id>/meta.json`
   for that id.
3. Create goose, send one prompt. Do **not** End it. Restart
   `mcremote`. Phone refresh: goose and grok both appear as closed /
   Resume, goose above older closed rows of this device. No kilo from
   step 2.
4. Tap Resume on grok. Host log must show `acp session loaded` **and**
   `session created` within ~2 s. Chat must open. Sessions screen
   must not stay on the create spinner.
5. Sign-out must **not** delete those host rows.

## Rollout and Rollback

**Rollout.** Operator installs and restarts `mcremote` (LaunchAgent).
Phone APK is required only for D6 sort on old daemons; D1–D4 are
server-side. Protocol addition (`updated_at` on list rows) is
additive; old phones ignore it. This plan does not run install,
`launchctl`, or `build-apk`.

**Rollback.** Revert the manager/store commits and restart. Old
Create SetMode-before-pump behaviour returns (hang on `auto` resume
of a full load). Old persist race returns (ghost End-session rows).
List order reverts to `ReadDir`. No migration: `purged` is
in-memory; `updated_at` is already on `Record`.

**Risk.** Holding no attach API (MADR Option 3) means a *new*
post-Start control emit before the pump would deadlock again. The
Create comment is the guard; a future emit belongs after `go m.pump`
or behind an explicit Attach MADR.

## Implementation notes (2026-08-16)

* Phases 0–5 landed in git. Install/restart/APK/C6 are operator-owned.
* Do not add `store.Delete` on "id not in live map" after a flush
  Save: that is how a CloseAll goose row would disappear.
* `_creatingBusy` on the phone is why a second Resume tap (kilo)
  never hit the wire while grok's `session.create` was stuck. Not a
  separate host bug.
