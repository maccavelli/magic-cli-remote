# MADR 0046: Post-Remediation Mobile Debug Pass — Findings and Fix Decisions

<!-- markdownlint-disable MD013 MD060 -->

- **Date**: 2026-07-28
- **Status**: Verified against `5322e63`; fix decisions proposed — NOT yet implemented
- **Scope**: `apps/mobile` (Dart sources + tests) at HEAD `5322e63`, i.e. *after*
  the MADR 0045 remediation batch (`c05b805..5322e63`), plus daemon↔app parity
  against `internal/event`, `internal/ws`, `internal/session`,
  `internal/provider`, and `docs/protocol-v1.md`
- **Related**: [MADR 0045](./0045-MADR-mobile-app-hardening-audit.md) /
  [plan](./0045-plan-mobile-app-hardening-audit.md),
  [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md),
  [MADR 0044](./0044-MADR-auto-approve-modes.md)
- **Standards applied**: `/home/mac/standards/mobile` v3.12.2-v3 (2026-07-28) —
  `networking.md`, `architecture.md`, `dart.md`, `flutter.md`, `android.md`
- **Companion plan**: [0046-plan-mobile-debug-pass.md](./0046-plan-mobile-debug-pass.md)

---

## 0. Method and verification discipline

This pass audited the tree the 0045 remediation produced. Five parallel
subsystem audits (WS transport; chat state/data pipeline; chat/sessions UI;
storage/notifications/lifecycle; Go↔Dart protocol parity) each traced full
code paths before reporting; candidates that turned out to be guarded
elsewhere were dropped, and each audit recorded the areas it verified clean
(§6). The four high-severity findings were then **independently re-verified**:

- **H-A was reproduced by execution** against the locked
  `web_socket_channel 3.0.3`: a dial to a refused port fails `ready` in 44 ms
  while `sink.close()` is still pending after 10 s.
- **H-B**'s deleted guard was recovered from the `6fe2e0d` diff
  (`persistedMatchesAuthority`), together with the tailnet-churn motivation
  for its removal (`settings_store_test.dart` "survives tailnet IP churn…"),
  so the decision below preserves churn while closing the contamination.
- **H-C** was confirmed from the key constants:
  `_indexKey = 'tx_cache_v1_index'` begins with `_entryPrefix =
  'tx_cache_v1_'`, so the prefix sweep necessarily visits the index itself.
- **H-D**'s throw was confirmed in `flutter_riverpod 3.3.2` source:
  `_assertNotDisposed` is a real `throw StateError(...)` (not an `assert`), so
  it fires in release builds too (`consumer.dart:469-477`).

**Baseline**: at `5322e63`, `flutter analyze` reports no issues and all 408
mobile tests pass. Every finding below is therefore invisible to the current
suite; each fix phase in the companion plan names the regression test that
makes it visible.

**Headline**: all four highs are **regressions introduced by the 0045
remediation commits** (`09ac07f`, `6fe2e0d`, `e101d54`) — none is a
pre-existing bug that pass missed. The cross-cutting lesson is recorded in §5.

**Severity**: **High** = user-visible breakage, data loss, security, or a
stuck flow on a mainline path. **Medium** = real defect on an edge path or a
robustness gap that violates a standard. **Low** = polish, performance,
hygiene. **Info** = contract/doc hygiene with no current user-visible effect.

---

## 1. Findings index

IDs are deliberately disjoint from MADR 0045's (`H-A…` not `H1…`) so review
threads can cite either document unambiguously.

| ID | Sev | Area | File | One-liner |
|---|---|---|---|---|
| H-A | High | Transport | `mcremote_client.dart:488,550` | Failed dial awaits a `sink.close()` that never completes → connect wedged, auto-reconnect dead, typed `cert_mismatch` never surfaced |
| H-B | High | Storage | `settings_store.dart:322-329,363-372` | Cert-pin identity fallback lost its authority guard → pairing a 2nd daemon overwrites the 1st daemon's pin; reads vouch cross-daemon |
| H-C | High | Pipeline | `transcript_cache.dart:169-183` | `_retainOnly` prefix-sweeps its own index key → LRU eviction and `clear()` permanently defeated; unbounded SharedPreferences growth |
| H-D | High | Chat UI | `chat_screen.dart:909-913` | `_sendText` failure path calls `ref.read` unmounted → `StateError` (all build modes) + staged-image FIFO leak → thumbnails mis-attach to the next image echo |
| M-1 | Med | Transport | `mcremote_client.dart:708` | `healthz()` never calls `_noteHost` → "Test connection" probes host B pinned under host A's identity/TLS mode; can persist B's fp over A's record |
| M-2 | Med | Pairing | `connect_screen.dart:74-81` | Host edit clears the pending QR fingerprint but not the pending relay tuple → B's token dialled toward A's relay; A's relay persisted under B's authority |
| M-3 | Med | Storage | `settings_store.dart:548-559` | `_clearSecret` inside the 2 s secure-storage cooldown silently skips keystore deletion → "cleared" credentials still live in the keystore |
| M-4 | Med | Notifications | `notification_coordinator.dart:311-325` | Actionable Allow/Deny notifications outlive their session (no removal on delete/close/sign-out); tap lands in a dead session |
| M-5 | Med | Pipeline | `transcripts_notifier.dart:398-419` vs `:152` | Fold-time seq noting skips neutral `usage_update` events → false `_seqGapSuspected` → spurious full resync truncates local-only items and staged thumbnails |
| M-6 | Med | Pipeline | `transcript_reducer.dart:221-228` | Permission-timeout notice deduped by literal text across the whole transcript → every timeout after the first is silently invisible |
| M-7 | Med | Chat UI | `sessions_screen.dart:222,261` | Rename dialog disposes its `TextEditingController` while the route is animating out → IME `clearComposing()` on a disposed notifier (the race this file documents at `:308-312`) |
| M-8 | Med | Chat UI | `chat_screen.dart:1654-1656` | Same disposed-controller race for the question sheet's "Other" text fields |
| M-9 | Med | Chat UI | `chat_screen.dart:2155-2160` | `_scrollToEnd(force:)` added but never passed → "Jump to latest" during a fling does nothing yet wipes the unread count and hides the FAB |
| L-1 | Low | Transport | `mcremote_client.dart:1141-1185` | Missed-ping/`probeLiveness` epoch guards evaluate at failure-delivery, not ping-send → stale handler flaps state and supersedes a user connect |
| L-2 | Low | Transport | `mcremote_client.dart:1352-1385` | Stale unawaited teardown's `_failAllPending` runs after awaits on the shared `_pending` map → can kill a newer attempt's handshake completer |
| L-3 | Low | Transport | `mc_exception.dart:43-70` | Every `auth_error`/`pair_error` mapped `permanent: true`; daemon `rate_limited`/generic `auth_failed` are transient → one hiccup parks background reconnect; `error`-typed replies lose the daemon's message |
| L-4 | Low | Transport | `relay_transport.dart:172-178,229-240` | Loopback peer write errors surface on a never-listened `Socket.done`; peer `onDone` no-op; dead `prev` in `_replacePeer` |
| L-5 | Low | Notifications | `notification_service.dart:34-77` | One transient `init()` failure permanently disables notifications for the process (no retry, throws swallowed, no error state) |
| L-6 | Low | Pipeline | `transcripts_notifier.dart:261-281` | `hydrateFromCache` live-state merge omits `usage` → a racing `usage_update` is clobbered, blanking the context indicator |
| L-7 | Low | Pipeline | `transcripts_notifier.dart:818-821` | Absent-id `sessionTranscriptProvider` materialises a fresh no-`==` `SessionTranscript` per commit → unrelated screens rebuild at streaming cadence |
| L-8 | Low | Chat UI | `chat_screen.dart:2591` | `_ModeSelector.onSelected` reads `ref` after the confirm await with no mounted check — H-D's class, narrower window |
| L-9 | Low | Chat UI | `chat_screen.dart:1109-1164` | Chat-screen `_endSession` has no busy guard (sessions screen added `_endingIdBusy` for this) → double-fire races cancel/delete and shows a spurious failure toast |
| L-10 | Low | Sessions UI | `sessions_screen.dart:155-177` | `_refresh` overwrites rows with a pre-await snapshot → status chip regresses to "Working…" after a `turn_complete` landed mid-RPC |
| L-11 | Low | Theme | `top_notification.dart:174-177` | `Dismissible.onDismissed` replays a 250 ms exit animation on the collapsed child → debug "still part of the tree" error window; queued toasts delayed ~550 ms |
| L-12 | Low | Notifications | `notification_coordinator.dart:57-68` | `_watchStack` de-dupes ids on claim → doubled `ChatScreen` push for one session (unguarded row double-tap, `sessions_screen.dart:1415`) lets the first dispose release the only entry; notifications fire for the visible chat |
| L-13 | Low | Protocol/Go | `internal/provider/provider.go:312` | `UpdatedAt time.Time` with `omitempty` (never applies to structs) leaks `0001-01-01T00:00:00Z` → picker shows "Updated ~2000 years ago" |
| I-1 | Info | Protocol/Go | `internal/event/event.go:387-389` | Plan-clear comment promises "empty, non-nil list" but `entries,omitempty` omits the key; app clears only via its absent→`[]` default; `session_mode` uses the opposite absent→merge rule; protocol-v1.md silent on both |
| I-2 | Info | Go | `internal/ws/server.go:588` | `handleSessionPendingAsks` reads `c.deviceID` without `s.mu`, breaking the file's own locking discipline (benign today, same-goroutine) |

---

## 2. High findings — detail and decisions

### H-A — Failed WebSocket dial hangs connect and kills auto-reconnect

`mcremote_client.dart:486-492` (`_openSocketDirect`) and `:547-556`
(`_openSocketViaRelay`), introduced by `09ac07f`.

Both failure paths run `await channel?.sink.close();` **before**
`httpClient.close(force: true)`. In `web_socket_channel 3.0.3` (locked), when
`channel.ready` completes with an error the adapter's internal controller
stream never gets a listener — that wiring happens only in the
connect-success branch — so the foreign sink's `close()` future **never
resolves**. Reproduced against the locked package: `ready` fails at 44 ms on
a refused port; `sink.close()` still pending at 10 s.

Consequences, all on mainline paths: `_openSocketDirect` never returns, so
`pinner.translate` never runs and a **cert-pin mismatch is never surfaced as
the typed `cert_mismatch` error**; the connect-screen `connect()` future
never resolves (spinner and `_busy` latched); on the reconnect path
`_reconnectInFlight` stays `true`, so `_scheduleReconnect` no-ops forever —
one failed background attempt kills automatic reconnection (and with it
background notifications) until an app-resume/connectivity `reconnect()`
resets the flag. Every hung attempt leaks its `HttpClient` and the pending
dial.

Note the repo already knows the safe shape: `_closeOpenedSocket`
(`mcremote_client.dart:559-570`) bounds the same close with
`.timeout(const Duration(seconds: 2))`.

**Decision**: in both catch blocks, close the `HttpClient` **first**
(`close(force: true)` aborts the in-flight connect and resolves the adapter's
futures), then fire-and-forget the sink close bounded by a short timeout
(`unawaited(… .timeout(2s).catchError(…))`). Add the missing test class: a
dial against a real refused port and a real accepting-then-resetting socket
must complete with a typed `McException` within a deadline, and
`_reconnectInFlight` must be observed false afterwards. Standards:
networking.md "Bound connection, handshake … operations with timeouts";
"Treat network, certificate, and protocol errors as typed user-visible
states".

### H-B — Cert-pin identity fallback cross-contaminates daemons

`settings_store.dart` `getPinnedCert` (`:322-329`) and `setFingerprint`
(`:363-372`), introduced by `6fe2e0d`.

Both methods previously guarded the persisted-id fallback:

```dart
final savedHost = await getHost();
final persistedMatchesAuthority =
    savedHost != null && _authorityOf(savedHost) == authority;
final effectiveId =
    explicitId ?? (persistedMatchesAuthority ? persistedId : null);
```

`6fe2e0d` replaced this with `final effectiveId = explicitId ?? persistedId;`
to make pins survive tailnet IP churn (the guard demanded a rescan whenever
the daemon re-registered on a new 100.x address — see the churn test added
with the change). But the unconditional fallback breaks the invariant the
client relies on: `_noteHost` (`mcremote_client.dart:1089-1098`) nulls the
in-memory `deviceId` precisely when the dialled authority changes, *so that*
a pin for the next host is never keyed under the previous host's identity —
its doc comment says exactly this. With the store re-adopting the persisted
id behind the client's back, that nulling is defeated:

- **Write side (destructive)**: paired to daemon A (persisted `device_id`
  dev-A, pin `id:dev-A` → fpA). Scan daemon B's QR: `_noteHost` resets the
  in-memory id; `_resolvePin` → `setFingerprint(hostB, fpB, deviceId: null)`
  falls back to dev-A and **overwrites `id:dev-A` with B's fingerprint** —
  before any claim succeeds. If the claim fails (expired code), the user is
  still paired to A, and A's next reconnect reads fpB → hard `cert_mismatch`
  with the "host may be spoofed" warning; unrecoverable without a rescan
  (self-healing only if A's stored host string still carries `#fp=`).
- **Read side**: `getPinnedCert(hostX)` with no explicit id returns whatever
  pin sits under the persisted id **regardless of which daemon `hostInput`
  addresses** — the method's own doc ("a pin recorded for a *different*
  identity is never returned") forbids this.

Test gap: the churn test covers same-daemon-new-address only; the
"different identity never inherits a pin" test uses explicit ids on both
sides; no test pairs a second daemon through the implicit-fallback path.

**Decision** — keep churn, close contamination, by splitting the two
meanings of "no explicit id":

1. **Writes never adopt the persisted identity.** `setFingerprint` with a
   null id records an **authority-keyed pending pin** (`host:authority`,
   `device_id: null`) — the pre-`6fe2e0d` shape — which the existing
   claim-migration already promotes to `id:` once pairing succeeds (test
   "a pin taken before pairing is claimed by the identity that follows").
   An unclaimed pairing attempt can no longer touch another daemon's record.
2. **Reads fall back to the persisted identity only on explicit opt-in**: new
   `fallbackToPersistedIdentity` parameter (default `false`). The client opts
   in only where identity continuity is actually warranted — token-bearing
   connect/reconnect paths, where the daemon accepting the stored token *is*
   the continuity proof. Pair-claim and healthz paths pass `false`. Churn
   still works: reconnect to the churned address with the stored token opts
   in, finds `id:dev-A`, connects.
3. The churn test is rewritten to mirror the real flow (pin written with an
   explicit id at pairing; read back via the opt-in), and two regression
   tests are added for the second-daemon write and read arms.

### H-C — `TranscriptCache._retainOnly` deletes its own index

`transcript_cache.dart:169-183`, introduced with `retainOnly` in `e101d54`.

`_indexKey` (`'tx_cache_v1_index'`, `:19`) starts with `_entryPrefix`
(`'tx_cache_v1_'`, `:20`). The prefix sweep therefore visits the index key,
computes `id = 'index'`, which is never in `liveIds`, and **removes the
index**; the follow-up `p.getStringList(_indexKey)` reads null and writes
back an empty list even when every cached session is live.

`syncFromMeta` calls `retainOnly` on **every sessions-screen refresh and
every reconnect** (`transcripts_notifier.dart:790`), so in practice the index
is always empty. Every previously saved entry blob becomes an orphan:
invisible to `_writeEntry`'s 12-session LRU eviction **and to `clear()`** —
the exact invariant `transcript_cache_test.dart:250` asserts ("no blob
without an index entry — invisible to eviction and clear"), but no test
exercises `retainOnly`'s effect on the index. SharedPreferences grows by up
to ~400 KB per session, bounded only by how many sessions the host lists.

**Decision**: the sweep must never treat the index as an entry
(`key != _indexKey` alongside the prefix test; do **not** rename the key —
that is a persisted-data migration for no benefit). Because released builds
have already orphaned blobs, `_retainOnly` additionally **reconciles** the
index: kept = stored index ∩ `liveIds`, then append any surviving entry keys
missing from it (recovered orphans), so eviction and `clear()` regain
authority over historical damage. Regression tests: `retainOnly` with all
sessions live preserves the index verbatim; orphaned blobs are re-indexed;
eviction still triggers after a `retainOnly`.

### H-D — Unmounted `ref.read` on the send-failure path; staged-image leak

`chat_screen.dart:906-913`.

`_sendText`'s catch block calls
`ref.read(transcriptsProvider.notifier).unstageSentImages(...)` before the
`mounted` check at `:914` (which guards only the UI recovery below).
`flutter_riverpod 3.3.2` throws `StateError` from `_assertNotDisposed`
whenever `context.mounted` is false — an unconditional `throw`, active in
release builds (`consumer.dart:469-477`).

Scenario: attach an image → send → back out of the chat while
`session.prompt` is in flight → the RPC fails (socket drop/timeout — exactly
the reconnect window). Result: an unhandled async `StateError`, **and** the
staged batch is never unstaged from `TranscriptsNotifier._sentImages`, so the
stale FIFO head mis-attaches its thumbnails to the next image-bearing
`user_message` echo when the session is reopened — the precise corruption the
call exists to prevent. The queued-flush path (`_maybeFlushQueue` →
`_sendText`, `:967`) reaches the same catch.

**Decision**: capture the notifier **before** the first await
(`final transcripts = ref.read(transcriptsProvider.notifier);`) — the
standards' prescribed pattern for post-await/`dispose`-adjacent access
(flutter.md § Async & Lifecycle Safety) — and unstage unconditionally in the
catch; `mounted` continues to guard only composer/queue/notification
recovery. Queued prompts remain `State`-scoped: leaving the chat already
discards the queue by design, so no cross-screen persistence is added; the
fix removes the crash and the corruption, not that (pre-existing,
documented) scoping. L-8 (`_ModeSelector.onSelected`) is fixed with the same
capture-before-await pattern in its phase.

---

## 3. Medium findings — detail and decisions

### M-1 — `healthz` resolves and persists pins under a stale identity

`mcremote_client.dart:697-728`. `healthz()` is the only dial entry point that
never calls `_noteHost`, so `_pinIdentity` still answers with the *currently
paired* daemon's `deviceId`, and `_resolvePin` both reads and (with a scanned
QR pending) **persists** a pin under that identity while probing a different
host — the probe also runs under the previous `_tlsMode`. "Test connection"
against host B can therefore raise the impersonation-warning `cert_mismatch`
copy for a host that never claimed the pin, and (pre-H-B-fix) overwrite A's
record.

**Decision**: probes must be side-effect-free on connection pin state. Give
`healthz` a strictly host-scoped resolve: explicit/`#fp=` fingerprint from
the input if present, else `getPinnedCert(hostInput, deviceId: null,
fallbackToPersistedIdentity: false)`; do not mutate
`_pinnedFingerprint`/`_pinnedFor`/`_tlsMode`; build the probe's `CertPinner`
from the *resolved* mode, not the client field. Persisting a scanned pending
fingerprint is permitted only as an authority-keyed pending pin (H-B rule 1
makes that safe).

### M-2 — Pending QR relay tuple survives a host edit

`connect_screen.dart`. `_onHostEdited` (`:74-81`) clears
`_pendingFingerprint/_pendingTlsMode/_pendingFor` but not
`_attemptRelayUrl/_attemptRelayHostId/_attemptRelaySpecified` (set in
`_applyPair`, `:435-436`, consumed at `:520-530`); the comment at `:244-245`
("Relay hints belong to this pairing attempt") states the contract the code
doesn't keep. A failed QR claim for daemon A followed by a manual connect to
daemon B passes A's relay tuple: `_shouldUseRelay`
(`mcremote_client.dart:603-609`) dials A's relay whenever B isn't directly
reachable — **sending B's token toward A's relay** — and on success persists
`setRelayRoute(url: A, hostId: A, authority: B)`, poisoning every future
off-mesh reconnect for B.

**Decision**: one `_clearPendingPairHints()` that resets fingerprint, TLS
mode, *and* the relay tuple, called from `_onHostEdited` and from every
pair-claim failure path. Regression test: QR-for-A + failed claim + host
edit + connect-to-B never passes A's relay parameters (extend
`connect_screen_test.dart`).

### M-3 — Sign-out silently skips keystore deletion during the cooldown

`settings_store.dart:548-559` with the `_shouldTrySecure` cooldown
(`:605-617`). `_clearSecret` honours the 2 s failure cooldown, so
`clearToken`/`clearFingerprint`/`clearClientIdentity` called inside it —
e.g. the connect screen's "Clear host & all data" right after a keystore
hiccup surfaced the error (`connect_screen.dart:794-805` → `clearAll`) —
delete only the (absent on Android) prefs fallback and report success. The
live `mcr_` token, client private key, and pins remain in the keystore while
the UI says "Saved credentials cleared"; a later `getToken()` reads the
token back. Write-side analogue of android.md's fail-closed secret handling.

**Decision**: deletes are not throttled — `_clearSecret` always attempts the
keystore delete regardless of cooldown, and a failed delete **throws**
`SecureStorageUnavailable` instead of returning success, so `clearAll`
propagates and the UI reports "couldn't clear saved credentials — try
again" rather than lying. The cooldown remains for reads/writes (its
purpose: not hammering a flaky keystore on the hot path); a one-shot
sign-out is neither.

### M-4 — Actionable ask notifications outlive their session

`notification_coordinator.dart:311-325`. Asks leave the shade only via
`permission_resolved`/`question_resolved` events or the reconnect reconcile.
The daemon deliberately emits no resolution on session close — entries
"disappear naturally on close" from the *snapshot* only
(`internal/session/manager.go:583-585`) — and the app's own delete paths
(`sessions_screen.dart:1059`, `chat_screen.dart:1150`) and sign-out never
inform the coordinator. On a stable connection there is no reconcile, so an
Allow/Deny notification for a deleted session stays actionable
indefinitely; tapping Allow fails `respondPermission` and the fallback
(`:182-184`) opens the dead session's chat. After sign-out it persists until
re-pair and can't be serviced at all.

**Decision**: app-side lifecycle, no protocol change (consistent with 0045's
"no new event types" stance). Coordinator gains `dropSessionAsks(sessionId)`
— cancels the session's ask notifications and forgets its pending state —
called from both delete paths after a successful `session.delete`/`close`,
and `dropAllAsks()` called from sign-out/`clearAll`. The reconnect reconcile
remains the authority for daemon-side closes that happen while disconnected.

### M-5 — Fold ordering fabricates seq gaps; spurious resync truncates

`transcripts_notifier.dart` — `_foldChunks` notes superseded heads' seqs at
fold time (`:398`, `:412`) while neutral events (`usage_update`) ride `out`
to be noted later (`:416-419`); `_noteSeq` (`:147-156`) flags
`_seqGapSuspected` on any `seq > last + 1`. A batch of
`[chunk s2, usage s3, chunk s4, …]` — the standard OpenCode cadence,
`transcript_ingest_test.dart:92` — notes 2 then 4 and flags a gap that does
not exist. The flag makes the next `resyncHistory` rebuild unconditionally
(`:750-752`), defeating the "resync is a no-op when nothing was missed"
contract (`history_replay_test.dart:376` passes only because it ingests
gapless per-event). A spurious rebuild from the daemon ring (trims 800→600;
phone cap 800) truncates locally held older items and drops staged-image
thumbnails (`_applyChunked` clears `_sentImages`). Reachable outside
reconnect via the cancel-resync path (`chat_screen.dart:990-993`).

**Decision**: separate the two jobs `_noteSeq` conflates. **Gap detection
runs once per batch over the raw, arrival-ordered events** before folding;
fold-time and apply-time noting becomes bounds-only (`_noteSeqBounds`: update
`_firstSeq`/`_lastSeq`, never the gap flag). Regression tests: the
chunk/usage interleave must not set `_seqGapSuspected` (assert via
`debugLastSeq` plus a new `debugGapSuspected` hook), and the
`history_replay_test` no-op contract must hold under chunked ingestion.

### M-6 — Permission-timeout notice suppressed after the first timeout

`transcript_reducer.dart:214-228` (added in `e101d54`). The replay-dedup
scans **all** items for the literal text `'Permission request timed out'`, so
it dedups forever: permission B's timeout an hour after permission A's shows
nothing — the pending card vanishes with no explanation. No test covers the
notice.

**Decision**: scope the dedup to the resolution, not the transcript. The
notice item carries a stable dedupe identity derived from the permission id
(`perm-timeout:<permissionId>`; `ChatItem.system` gains an optional
`dedupeKey`), and the scan matches only that key. Live and replayed
observations of the *same* resolution still collapse to one notice; distinct
timeouts each get theirs. Regression test: two sequential timeouts produce
two notices; a replayed resolution produces no duplicate.

### M-7 / M-8 — Disposed `TextEditingController` vs. exit-animation IME

`sessions_screen.dart:222,261` (rename dialog) and
`chat_screen.dart:1610-1617,1654-1656` (question sheet "Other" fields). Both
dispose owner-held controllers immediately after `await
showDialog`/`showModalBottomSheet` resolves, while the route is still
animating out with the field attached; focus loss during teardown calls
`EditableText`'s `widget.controller.clearComposing()` → `notifyListeners()`
on a disposed notifier → `FlutterError` in debug on every rename/submit with
an active IME composition (gesture/CJK/autocorrect keyboards). The repo
documents this exact race and its cure at `sessions_screen.dart:308-312`
(`_createSessionFlow`).

**Decision**: apply the documented pattern — controller ownership moves into
the route's own stateful widget (a small `_PromptTextDialog` for rename; the
question sheet's stateful body owns its per-question controllers), so
disposal happens in that widget's `dispose()` after the route is torn down.
No deferred-dispose timers.

### M-9 — "Jump to latest" FAB: `force` never wired

`chat_screen.dart:449-468` (`_scrollToEnd({bool force = false})`, guard at
`:467`) and the FAB handler (`:2155-2160`), which calls `_scrollToEnd()`
unforced **after** setting `_userNearBottom.value = true` and zeroing
`_unreadWhileScrolledUp`. Tapping the FAB during a ballistic fling is
swallowed by `_listScrolling`, but the unread state is already destroyed and
the FAB hides — the tap does nothing except lose information. The parameter
was evidently created for this call site.

**Decision**: the FAB passes `force: true` (a user tap is an explicit
gesture; yanking the list is the point), and the unread/near-bottom mutations
move after the jump is queued. Widget regression test: tap during an active
scroll activity ends pinned at offset 0 with the unread count cleared.

---

## 4. Low findings — decisions in brief

Full failure scenarios live in the pass record; the plan's Phase C carries
the per-item steps.

| ID | Decision |
|---|---|
| L-1 | Capture the epoch **at ping send** (and at `probeLiveness` entry) and compare at failure delivery; a mismatch means a newer attempt owns the connection — do nothing. Stop counting `connection_lost` from intentional teardown toward `_missedPings`. |
| L-2 | Snapshot-and-clear `_pending` in `_teardownSocket`'s synchronous detach section; fail the snapshot after the awaits. A stale teardown then cannot touch a newer attempt's completers. |
| L-3 | Transient codes map `permanent: false`: `pair_error rate_limited` (protocol-v1.md:739 marks only `client_key_*` permanent) and `auth_error auth_failed`; `invalid_token`/`bad_version`/`unauthorized`/`client_key_*` stay permanent. `error`-typed handshake replies surface the daemon's `message` and map transient (the `_handshakeFailures` cap still parks repeat offenders). |
| L-4 | Listen to `_peer.done`/socket errors and tear down the tunnel on peer close; delete dead `prev`. |
| L-5 | `NotificationService.init()` retries on next `show()` after a failure (latch the *attempt*, not the outcome); surface a Settings row when initialisation is failing. |
| L-6 | Add `usage` to `hydrateFromCache`'s keep-live merge, beside `modes`/`currentModeId` (the test precedent is `history_replay_test.dart:262`). |
| L-7 | Cache a canonical empty `SessionTranscript` per absent id (or give `SessionTranscript` value equality); either stops cross-session rebuild storms. |
| L-8 | Same capture-before-await pattern as H-D, applied in the H-D phase. |
| L-9 | Mirror sessions_screen's `_endingIdBusy` guard in chat-screen `_endSession`. |
| L-10 | `_refresh` merges the snapshot with statuses observed after the RPC began (event wins over snapshot for rows both touched), or re-applies buffered events post-`setState`. |
| L-11 | `onDismissed` removes the overlay entry immediately (skip the reverse animation on an already-collapsed child). |
| L-12 | `_watchStack` becomes a counted stack (allow duplicate ids); `releaseSession` removes one occurrence. Optionally debounce the sessions-row double-push at the source. |
| L-13 | `json:"updated_at,omitzero"` (Go 1.26; precedent `internal/event/event.go:373` `RetryAt`), plus a sweep for other `time.Time`+`omitempty` wire fields. Dart side needs no change. |

## 5. Informational and cross-cutting

- **I-1**: fix the `event.go:387-389` comment and document in
  `docs/protocol-v1.md` that a `plan` event **omits** `entries` to clear
  (replace semantics; absence ≠ merge), explicitly contrasting with
  `session_mode`'s absent→keep-current rule. Wire shape unchanged — this app
  already handles it, and changing emission would be churn without benefit.
- **I-2**: take `s.mu` for the `c.deviceID` read in
  `handleSessionPendingAsks`, matching `handlePermissionRespond`
  (`server.go:922`) and the `dispatchAsync` snapshot discipline
  (`:643-658`).
- **Cross-cutting lesson**: every high here was introduced *by* the 0045
  remediation, in exactly the code that batch rewrote, and none was visible
  to analyzer or the 408-test suite. Two systemic gaps recur: (1) failure
  paths of new code are tested with fakes that cannot fail the way
  production does (no failed real dial; no second-daemon pairing; no
  `retainOnly` index assertion; no unmounted send failure); (2) helper
  contracts (key namespaces, identity fallbacks, `force` parameters) changed
  without updating every caller/test that encoded the old contract. The plan
  therefore pairs **every** phase with the regression test that would have
  caught its finding, and Phase V refuses completion while any new test is
  red.

## 6. Verified clean (scope covered without findings)

Envelope/frame-budget parity both sides (incl. the 1 MiB limit mirror);
`session.pending_asks` request/reply and reconcile; `SessionMode.dangerous`
end-to-end (0044); model-selection wire schema (0043); all 21 event types
dispatched (no unknown-type void); reducer immutability/ownership and fold
correctness for content; hydrate/replay/resync generation+seq discipline
(minus M-5); cache serialization and orphan sweeps (minus H-C); history
paging vs `Manager.HistoryPage`; permission/question sheet single-flight,
external dismissal typing, and no-loop contract; `_maybeFlushQueue`
re-checks; transcript list keys/pagination/scroll pinning (minus M-9);
disposal coverage across screens (minus M-7/M-8); pair-URI validation and
trust rules; `CertPinner` mode/context handling; secure-storage fail-closed
reads/writes and migrations (minus M-3/H-B); FGS/Android 14 manifest and
runtime-permission compliance; QR scanner lifecycle (verified against
`mobile_scanner 7.4.0` source); `_ensureIdentity` failure reset; relay
single-peer bridge (T4 fix) and buffer bounds; epoch discipline in
`connect`/`claimPairCode`/`_connectInternal` (minus L-1/L-2).
