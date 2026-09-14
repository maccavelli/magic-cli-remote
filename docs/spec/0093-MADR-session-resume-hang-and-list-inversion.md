---
status: accepted
date: 2026-08-16
decision-makers: [Project Owner]
consulted: [Implementer]
informed: [Operators of macos-laptop, Android phone clients]
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Resume a closed session without hanging, and list the sessions that actually survived

## Context and Problem Statement

On 2026-08-16 the operator updated the Android app on `s22+` and tried to
resume two closed chats. Both attempts hung until they signed out. The
host daemon (`mcremote` `0.10.11.2.g3ea8f9f` on `macos-laptop`) stayed
up. After the hang they reported a second, contradictory symptom: a
**kilo** row still offered Resume, while last night's **goose** session
— which the host had soft-closed on daemon restart — did **not** appear.

Those two symptoms look like one "sessions no longer resume across
updates" bug. They are not. The investigation separated four facts that
the product currently conflates in operator language:

1. **Chat resume** is `session.create` with the prior `id` +
   `agent_session_id`. It is not protocol-v2 socket resume (0068).
2. **End session** is `session.delete` (`purge=true`). Leaving chat, an
   app update, and a daemon restart are `purge=false`.
3. A **full session/load replay buffer** plus **mode restore before the
   event pump starts** deadlocks `session.create`. The phone waits on a
   120 s RPC and shows a spinner (`_creatingBusy`), so a second tap
   never leaves the device.
4. A **debounced persist flush can recreate a just-deleted row**, so
   End session on kilo can still produce a Resume-able ghost. The
   sessions list is unsorted and silently drops unreadable metas, so
   the row the operator expects (goose) can be absent or buried.

The architectural question is: **after a phone update or a host
restart, which closed sessions must appear, which must stay gone, and
what may the host do between `provider.Start` and `session.created`
without blocking the client forever?**

Scope: `internal/session` create / close / list / persist,
`internal/provider/acpagent` and `httpagent` control-event delivery,
and the phone sessions list (`apps/mobile/lib/features/sessions`,
`SessionMeta`). Does **not** reopen 0068's transport resume window,
0066's secure-storage upgrade path, 0078's handoff visibility, or
0089 D3's boot-time rehydrate of *live* work. Does **not** change the
meaning of End session.

Companion plan:
[0093-PLAN-session-resume-hang-and-list-inversion.md](0093-PLAN-session-resume-hang-and-list-inversion.md).

Related: [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md)
(socket resume), [0066](0066-MADR-secure-storage-upgrade-resilience.md)
(app update vs pairing), [0078](0078-MADR-session-handoff-and-receipt-surfacing.md)
(`visibleTo` / owner list), [0089](0089-MADR-long-running-session-stability.md)
(CloseAll + durable rows), [0053](0053-MADR-grok-auto-mode-silent-arm.md)
(synthetic `auto` + `emitArmedMode`), [0051](0051-MADR-auto-approve-chat-noise.md)
(control-event delivery), [0056](0056-MADR-mcremote-android-protocol-stack-audit.md)
(H-6 complete snapshots).

## Decision Drivers

* A tap on Resume must either open the chat or fail with an error. It
  must not hang the sessions screen.
* Daemon restart and phone app update must leave **soft-closed**
  sessions (`purge=false`) on `session.list` as Resume rows.
* End session must **remove** the row and the provider-side conversation.
  A later list must not resurrect it.
* The list a device sees must be **that device's** durable + live
  sessions, newest first, with corrupt rows logged rather than dropped
  silently.
* Provider transports already promise "Start has returned ⇒ the
  manager pump is attached" (`acpagent.attached`, `httpagent.Emit`).
  The manager must honour that promise, not weaken every transport.
* Phone logcat was not available (no adb). Host `mcremote.err.log` and
  `~/.local/share/mcremote/sessions` are the evidence of record.
* Fixes must be pinned by tests that hang or resurrect without them.

## Considered Options

* Option 1: Honour Start's "pump is attached" contract; ban persist
  resurrection; sort and log the list
* Option 2: Soft-close on End session so everything stays resumable
* Option 3: Delay `attached=true` / make control emits non-blocking
  until the manager calls Attach
* Option 4: Auto-rehydrate every durable row on daemon boot (0089 D3)
  and skip manual Resume
* Option 5: Treat this as a phone-only bug (stale list / secure
  storage after APK update)

## Decision Outcome

Chosen option: **"Honour Start's pump-attached contract; ban persist
resurrection; sort and log the list"**, because the hang, the ghost
kilo, and the missing goose are three manager/list defects on top of a
correct End-session-vs-soft-close contract. Changing that contract, or
pushing attach semantics down into every provider, would paper over
the manager lying about when the pump exists.

* Companion Implementation Plan:
  [0093-PLAN-session-resume-hang-and-list-inversion.md](0093-PLAN-session-resume-hang-and-list-inversion.md)

Locked decisions:

| ID | Decision |
| --- | --- |
| **D1** | **Mode restore runs after `go m.pump`.** `Manager.Create` must not call `SetMode` between `provider.Start` returning and the pump reading `Events()`. `TypeMode` is a control event (`event.IsControl`). acpagent `deliver` blocks once `attached=true` (set at the end of Start). httpagent `Emit` always blocks on control events. A session/load that filled the buffer deadlocks SetMode("auto") → `armAutoMode` → `emitArmedMode`. Persist `mode_id=auto` after the first arm is what makes *resume* hit this path while *new* chats (empty buffer) succeed. |
| **D2** | **End session stays `session.delete` / `purge=true`.** Back, app update, sign-out, and `CloseAll` stay `purge=false`. The 23:54 kilo close is a confirmed End session (dialog copy promises removal), not a restart side-effect. Do not invent a third verb. The bug is resurrection after delete, not delete itself. |
| **D3** | **A purged id must not be written back.** `writePersist` consults an in-process `purged` set marked before `store.Delete`. A Save that races the mark is undone with a second Delete. `Create` of the same id clears the mark. `TestDeleteCancelsPendingDebouncedPersist` (timer after delete) is not enough; `TestDeleteWinsInFlightFlushPersist` pins the copied-meta-then-Delete-then-Save race. |
| **D4** | **`session.list` is owner-scoped, live-first, newest-touched first.** `ListSnapshot` keeps `visibleTo` (0078). Disk rows carry `updated_at` on the wire. Sort: live before closed, then `updated_at` (else `created_at`) descending. `store.List` logs each skipped unreadable/corrupt `meta.json` (`session list skipped …`) instead of incrementing `skipped` in silence (0056 H-6). |
| **D5** | **This incident is not 0068 transport resume and not 0066 storage wipe.** The phone re-authenticated as the same `device_id` `4f8ab207-…` after the APK update. Pairing survived. The hung RPC is `session.create` / `session.created`, 8 s after `device authenticated`. |
| **D6** | **The phone sorts the snapshot the same way** (`_compareSessionsRecency`) so an old daemon that omits `updated_at` still puts recent closed rows at the top. Do not merge local transcript cache into the list; the host snapshot remains authoritative. |
| **D7** | **The operator builds and installs the daemons.** This change set does not run `make install`, LaunchAgent restart, or a phone APK build. The incident host remains `0.10.11.2.g3ea8f9f` until that install. Tree tests (C1–C5) confirm the code; C6–C7 confirm the installed product. |

### Consequences

* Good, because Resume of a long grok/kilo/opencode session that was
  last in `auto` no longer deadlocks `session.create`.
* Good, because End session stays a real delete; the ghost-kilo race
  is closed.
* Good, because a soft-closed goose/grok pair after `CloseAll` is
  required to appear on the next manager's list
  (`TestCloseAllKeepsSessionsListable`).
* Good, because a corrupt goose `meta.json` leaves a host log line
  instead of a mysteriously empty row.
* Bad, because D1 does not timeout a hung `session/set_mode` RPC
  itself — only the emit-into-full-buffer deadlock. A grok that never
  answers set_mode still holds the 120 s phone timeout.
* Bad, because `purged` is in-process only. A flush from a *previous*
  daemon process cannot be banned; that would need a durable
  tombstone, which this MADR rejects as too much machinery for a
  same-process race.
* Neutral, because 0089 D3 (boot rehydrate of live work) is unchanged
  and still the right answer for "I should not have to tap Resume
  after a host restart of a session that was running." This MADR is
  about the tap that *is* required for a session that was already
  closed.
* Neutral, because re-pair at 18:59 (`RevokeByClientKeyFP`, keep
  `4f8ab207-…`) still hides rows owned by revoked device ids. That is
  0078 / 0072 D4, not this list inversion. Transfer-on-re-pair is out
  of scope.

### Confirmation

* **C1.** `TestCreateRestoresModeAfterPumpStarts` fills a 1-slot event
  buffer in Start and has SetMode block on `TypeMode`. Create with
  `ModeID=auto` returns within 2 s. Without D1 this deadlocks.
* **C2.** `TestDeleteWinsInFlightFlushPersist`: copy live meta, Delete,
  `writePersist` that snapshot, `store.Get` must fail.
* **C3.** `TestDeleteCancelsPendingDebouncedPersist` still passes
  (timer-after-delete).
* **C4.** `TestCloseAllKeepsSessionsListable`: two Create, CloseAll,
  new Manager, `ListSnapshot` contains both, neither live, newest
  first.
* **C5.** `store.List` on a dir with unreadable `meta.json` increments
  `skipped` and emits `session list skipped unreadable meta` /
  `corrupt meta`.
* **C6.** After install: Resume of a grok session that has been in
  `auto` overnight returns `session created` and opens chat. End
  session on kilo, `session.list` does not contain that id. Soft-close
  (daemon restart) of goose + grok shows both, goose (newer) above
  older closed rows of this device.
* **C7.** `make pre-add-check` on touched Go files; `go test ./internal/session/`;
  `dart analyze` on the two Dart files. Nothing is "done" on the
  phone until C6.

## Pros and Cons of the Options

### Option 1: Honour Start's pump-attached contract; ban persist resurrection; sort and log the list

Keep End vs soft-close. Move mode restore after the pump. Ban persist
of purged ids. Sort and log the list.

* Good, because it matches the logs: `acp session loaded` then no
  `session created`; `purge=true` at 23:54 then a Resume-able kilo;
  `purge=false` on goose at 01:23 with no later delete log.
* Good, because it does not change provider attach semantics that
  every transport already documents.
* Good, because the failing tests hang or resurrect without the fix.
* Bad, because it does not auto-reopen live work after a host restart
  (0089 D3).
* Neutral, because the operator still taps Resume for a session they
  had already left.

### Option 2: Soft-close on End session so everything stays resumable

Make End session call `session.close` (`purge=false`).

* Good, because last night's kilo would have survived and the
  operator's "it was still resumable" expectation would match.
* Bad, because the dialog and comments already promise removal
  (`sessions_screen.dart`, `chat_screen.dart`). Soft-close is how
  closed rows *reappeared* after End, which is why delete was wired.
* Bad, because kilo engine `DELETE /session/…` would stop; engine
  sessions would accumulate.
* Bad, because it does not fix the grok resume deadlock.

### Option 3: Delay `attached=true` / add manager `Attach`

Providers stay in drop-oldest until the manager says the pump is up.

* Good, because any future post-Start emit (not just SetMode) would
  be safe.
* Bad, because it adds an Attach API to acpagent, acphttp, and
  httpagent, and it *contradicts* the existing comments that Start's
  return *is* the attach point.
* Bad, because httpagent has no `attached` flag at all — every
  control emit already assumes a pump. The manager must not call
  SetMode first regardless.
* Neutral, because D1 is the one-line honouring of the existing
  contract; Attach is a later hardening if another post-Start emit
  appears.

### Option 4: Auto-rehydrate every durable row on daemon boot (0089 D3)

On `serve`, resume every non-purged row so the phone never taps
Resume after a host restart.

* Good, because 0089 already locked this for *live* work that died
  with the daemon.
* Bad, because it does not explain this morning: the sessions were
  already closed; the tap hung; the list was wrong. Rehydrate would
  have started grok+goose at 01:30, which is a different product
  (and a prewarm/RSS cost 0089 D5 just turned off).
* Neutral, because 0089 D3 remains accepted and is not superseded.

### Option 5: Treat this as a phone-only bug

Blame APK update, secure storage, or a stale `_sessions` list.

* Good, because the operator reproduced it immediately after an
  in-place app update.
* Bad, because pairing survived (`device authenticated` same
  `4f8ab207-…`). `_sessions` is memory-only; a new process starts
  empty and replaces from `session.list`. The hang is a missing
  `session created` on the host. The ghost kilo is a host disk
  write.
* Bad, because phone logcat was unavailable; the host log is
  sufficient and points at the manager.

## More Information

### Incident timeline (host `mcremote.err.log`, CDT)

Device `4f8ab207-da01-4f85-8c8f-076af9863dc9` (`s22+`), paired
2026-08-15 18:59 via short code (revoked one prior device for the
same client key; 0072 D4).

| Time | Fact |
| --- | --- |
| 22:12:46 | grok `8b85a721` created; `acp auto-approve armed` (`mode_id` persists as `auto`) |
| 23:40:14 | kilo `1b297add` / `ses_ff721326…` created; auto armed |
| 23:53:51 | last kilo `prompt_async` |
| 23:54:12 | kilo `session closed purge=true` — End session, not restart |
| 23:54:21 | goose `9c635acc` / `20260816_2` created |
| 23:54:27 | goose `acphttp prompt` |
| 01:23:19 | daemon SIGTERM; grok + goose `purge=false`; kilo already gone |
| 01:29:59 | `mcremote` `0.10.11.2.g3ea8f9f` starts; device auth 01:30:07 |
| 08:31:17 | phone WS `peer_closed` (APK update) |
| 08:31:24 | same device authenticated |
| 08:31:32–33 | grok ACP init; `dropping event; slow consumer` `available_commands`; **`acp session loaded`**; **no `session created`** |
| 08:32:24 | operator signed out (`peer_closed`) |
| 08:33:55 | new grok `8b80eba3` created successfully (empty load buffer) |

Historical same hang: 2026-08-12 17:50 grok `f359e960` — drop +
`acp session loaded` + no `session created`. Successful loads the
same day (16:08) *did* emit `session created` 16 ms later; those
records did not yet have `mode_id=auto` on disk, or the buffer still
had a slot.

### What the disk said (after the incident)

`~/.local/share/mcremote/sessions`:

* `1b297add` — absent (purge).
* `9c635acc` — absent by the time of the later disk walk (no
  `purge=true` log between 01:23 and 08:31; later in the morning the
  operator created and then ended a *new* goose `c2fcac1e`).
* `8b85a721` — absent after the hung resume / sign-out / subsequent
  End; it *was* on disk at 08:31 because LoadSession ran.
* `9f066db8` — leftover kilo from 2026-08-11, owner `94bbf75d-…`
  (previous device). `visibleTo` hides it from `4f8ab207-…`.
* Only rows visible to this phone after 08:56: live grok `8b80eba3`
  and live kilo `d39b777d` (new chats).

Phone adb/logcat: **not retrieved** (no device on `adb devices`).
Sign-out does not delete host sessions (`_signOut` / `disconnect(manual:
true)`).

### Failure mechanisms (code)

**Hang (D1).** Phone `resumeSession` → `session.create` with prior
id, 120 s timeout, `_creatingBusy=true`. Manager `Create` used to
`SetMode(opts.ModeID)` immediately after `Start`. For grok, persisted
`auto` calls `armAutoMode` → `emitArmedMode` (`TypeMode`, control).
`acpagent.Start` has already set `attached=true`. `deliver` blocks
on a full `events` chan. Pump is not started yet. `session created`
is logged only after persist, so it never appears. The same SetMode
is used for kilo/opencode (`httpagent.Emit` blocks on control with
no attached flag; comment assumes the pump exists the moment Start
returns).

**Ghost kilo (D3).** `FlushPersist` copies dirty ids, drops
`persistMu`, then `Save`. `close(…, purge=true)` `store.Delete`s in
that window. A later Save recreates `meta.json`.
`TestDeleteCancelsPendingDebouncedPersist` only fires FlushPersist
*after* Delete (live map already empty). The in-flight copy is the
hole.

**Missing goose (D4).** `CloseAll` is `purge=false` and *does* call
`persistNow` (log line exists). `ListSnapshot` merges live + disk
with `visibleTo`. Two remaining ways a persisted goose disappears
from the UI: (1) `store.List` skips a bad `meta.json` and used not
to log; (2) unsorted `ReadDir` order buries a new row. A third
hypothesis — inverted prune of host rows from the phone cache — is
rejected: `syncFromMeta` only evicts *local* transcripts, and
`_sessions` is assigned from the host snapshot only.

**Why new chat worked.** `session/new` does not fill the event
buffer. SetMode-before-pump still ran, but the control send had a
slot. That is why 08:33:55 succeeded and 08:31:33 did not.

### Software written in the working tree (not the installed daemon)

| Change | Where |
| --- | --- |
| SetMode after `go m.pump` | `internal/session/manager.go` `Create` |
| 1-slot buffer + blocking SetMode | `TestCreateRestoresModeAfterPumpStarts` |
| `purged` set; Save skipped / undone | `markPurged`, `writePersist` |
| In-flight flush vs Delete | `TestDeleteWinsInFlightFlushPersist` |
| CloseAll leaves both rows, newest first | `TestCloseAllKeepsSessionsListable` |
| Skip logs | `internal/session/store.go` `List` |
| `Meta.UpdatedAt` + sort | `ListSnapshot` |
| Phone `updatedAt` + sort | `models.dart`, `sessions_screen.dart` `_compareSessionsRecency` |

Installed `mcremote` at incident time: `0.10.11.2.g3ea8f9f` (commit
`3ea8f9f`, before this tree).

### Explicit non-decisions

* No durable delete tombstone on disk (D3 is same-process only).
* No manager `Attach()` API (Option 3 deferred).
* No change to 0089 D3 boot rehydrate.
* No transfer of session ownership from a revoked device id to the
  new id on re-pair.
* No phone-side merge of transcript cache into the sessions list.
* `deliver` still does not select on the request `ctx`; a hung
  set_mode RPC still waits until `s.done` or the phone's 120 s
  timeout.
