---
status: accepted
date: 2026-08-16
decision-makers: [Project Owner]
consulted: [Implementer]
informed: [Operators of macos-laptop, Android phone clients]
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Ending the only session from inside chat must return to a populated sessions list, not a black screen

## Context and Problem Statement

On 2026-08-16 the operator ended the only active session from inside the
session window on `s22+` (Android app, host `mcremote` on
`macos-laptop`). After pressing the back arrow they were returned to a
blank, black screen that never refreshed or populated the session
landing screen data. The app only recovered by creating a new session
later.

Host `mcremote.err.log` pins the timeline:

| Time | Fact |
| --- | --- |
| 11:05:03.143 | `session closed session_id=1955822d-… purge=true` — the End session (`session.delete`) |
| 11:05:09.320 | `ws client disconnected reason=peer_closed` |
| 11:05:13.069 | `device authenticated` — same `device_id`, app-level reconnect |
| 11:05:35–36 | new kilo session created — the user's recovery |

The chat screen pops itself after a successful delete
(`apps/mobile/lib/features/chat/chat_screen.dart` `_endSessionFlow`:
`Navigator.of(context).pop(true)`), and the sessions landing screen
refreshes on return via a shared `RouteObserver` (`didPopNext`,
`sessions_screen.dart`). Both worked in the happy path — a widget test
reproducing "end session → return to landing" passed before any fix.
The black screen needs a second actor: the user pressing back while the
delete is still in flight.

Root cause, grounded in the go_router 17.5.0 source
(`~/.pub-cache/hosted/pub.dev/go_router-17.5.0/lib/src/delegate.dart`):

1. `_endSessionFlow` runs `cancel` + `deleteSession` over the socket.
   The daemon itself documents that lifecycle ops "can take many
   seconds" (`internal/ws/server.go` `dispatchAsync` comment).
2. If the user presses the app-bar/system back during that window, the
   chat route pops **and the State stays mounted through the ~300 ms
   exit transition** — so the `if (!mounted) return;` guard at
   `chat_screen.dart:1271` does not fire.
3. The completing delete then calls `Navigator.of(context).pop(true)`
   unconditionally. `pop` pops the navigator's *top* route — now the
   `/sessions` page, the last page on the stack.
4. go_router's `onPopPage` (`_handlePopPageWithRouteMatch`) →
   `_completeRouteMatch` (`delegate.dart:184`) removes the match from
   `currentConfiguration` and calls `notifyListeners()` **without
   checking whether it was the last match**. The Router rebuilds with
   zero pages → a black frame. The delegate's own debug assert names
   the hazard: "You have popped the last page off of the stack, there
   are no pages left to show" (`delegate.dart:176`).
5. Nothing re-establishes the stack afterwards: `refreshListenable`
   re-runs the redirect against an already-empty configuration, and the
   user sees black until a real navigation event (notification tap
   `router.go`, app restart).

A second, smaller defect sits next to it: the landing screen's
`didPopNext` refresh fires at user-pop time, which *races* the
in-flight delete — its `session.list` snapshot can still contain the
session, leaving a stale row after the host confirmed removal. In the
incident the socket dropped 6 s later and the reconnect refresh cleared
it, but that drop is incidental (the daemon broadcasts no delete event;
`handleSessionDelete` replies `ok` only), not contractual.

The architectural question: **when a long host-side operation outlives
the route that started it, how may the completing code navigate —
and how does the landing screen learn the result?**

Scope: the phone's end-session flow (`chat_screen.dart`,
`sessions_screen.dart`, `app_providers.dart`) and its tests. Does not
change the daemon, the protocol, or the meaning of End session
(`session.delete` / `purge=true` stays; 0093 D2).

## Decision Drivers

* **Ending a session from within the session window must return the user
  to the main session screen with no further action** — no back press,
  no retry, no app restart. The landing screen must show the
  post-delete list. Every completion path of the delete must land
  there, including when the user backs out mid-delete or a permission
  modal is on top when the delete completes.
* A navigation made by completing async work must not pop a route the
  user has already left; it must be conditional on the route still
  being the current one.
* The landing screen must show the post-delete list, not a row that
  the host has already removed, even when the user backed out
  mid-delete.
* Keep the existing refresh-on-return (`routeObserverProvider`,
  `didPopNext`): the pushed route may exit via `go` (notification tap),
  and awaiting the push future is the documented trap (MADR 0046 L-12).
* Fixes must be pinned by tests that black-screen without them.
* Follow existing app idioms (Riverpod `Notifier` providers, `ModalRoute`
  guards) rather than introducing new navigation machinery.

## Considered Options

* Option 1: Guard the auto-pop with `ModalRoute.isCurrent`; bump a
  sessions-revision notifier when the user has already left so the
  landing screen re-reads the list
* Option 2: Guard the auto-pop with `ModalRoute.isCurrent` only
* Option 3: Replace the pop with `context.go('/sessions')` on success
* Option 4: Await the chat push future on the landing screen again and
  refresh from its result
* Option 5: Rely on the incidental socket drop / next natural refresh

## Decision Outcome

Chosen option: **"Guard the auto-pop with `ModalRoute.isCurrent`; bump
a sessions-revision notifier when the user has already left"**, because
it makes the completing delete incapable of popping the navigator's last
page, and it gives the landing screen an explicit post-delete refresh
that does not depend on the daemon dropping the socket.

Locked decisions:

| ID | Decision |
| --- | --- |
| **D1** | **The end-session auto-pop is conditional.** `_endSessionFlow` pops only when `ModalRoute.of(context)?.isCurrent` is true. `isCurrent` goes false the moment the user's own pop starts, so the completing delete can never pop the route above the chat. The `!mounted` guard stays as the outer check; it alone is insufficient because the State survives the exit transition. |
| **D2** | **When the chat is no longer current, the delete completion bumps `sessionsRevisionProvider`** (new `Notifier<int>` in `state/app_providers.dart`). The sessions screen `ref.listen`s it and refreshes. This covers the race where the user's `didPopNext` refresh ran before the host confirmed the delete, leaving a stale row. |
| **D3** | **When the chat is still current, nothing extra happens**: the pop fires, `didPopNext` refreshes the landing screen after the delete completed — the pre-existing happy path, unchanged. |
| **D4** | **The daemon is not changed.** `session.delete` keeps replying `ok` without broadcasting a delete event; the socket drop in the incident log is evidence of the stale state, not a recovery mechanism to rely on. |
| **D5** | **`Navigator.pop` stays, not `context.pop()`.** Both pop the navigator's top route; only the `isCurrent` guard prevents the rogue pop, and the plain `Navigator.pop(true)` result is unused (the landing screen does not await it). |
| **D6** | **The modal path lands via a router-aware exit, not the guarded pop.** A permission/question sheet can appear while the delete RPCs are outstanding (asks arrive over the socket until the session dies; MADR 0046 M-4). `clearSession` retires it through the existing external-resolution listener (`chat_screen.dart:1974-1981` → `_dismissPermissionSheetExternally` → `removeRoute`). A probe against the installed Flutter SDK disproved the first mechanism attempted here: `removeRoute` does **not** synchronously restore `isCurrent` to the chat route. `Route.isCurrent` counts entries present up to and including `_RouteLifecycle.remove` (`navigator.dart:584`, `:3494`), and one flush pass left the retired sheet still "present": the probe measured `isCurrent=false` at guard time, `true` only after the next frame — the success toast shown, no pop fired, the user stranded on the dead chat. Therefore `_endSessionFlow` captures whether a modal of ours was open *before* `clearSession`; on completion, if the chat is not current **and** a modal was retired, it bumps `sessionsRevisionProvider` and takes the router-aware exit `context.go('/sessions')` — deterministic, no frame-timing dependence. If the chat is not current and no modal was open, the user already left: bump only. The completion branches are **ordered** (isCurrent first), not mutually exclusive: correctness holds because each branch's guard is checked in sequence, not because at most one can be true. The `hadModal` capture carries one microtask-ordering dependence that errs toward safety: the sheet's `finally` clears the flag as a microtask *after* the synchronous block, so the capture is a superset of the true set — a stale-true flag produces an unnecessary-but-correct `go`, never a missed exit. Pinned by C6. |
| **D7** | **The delete-failure path confirms against the host list before showing the error.** `deleteSession` sends `session.delete` with `idempotentRetry: true` (`mcremote_client.dart:3672`), while the daemon errors on a double delete (`TestWSSessionDelete` "delete already deleted") and lifecycle ops can take many seconds (`internal/ws/server.go` `dispatchAsync`). A delete the host already completed whose `ok` response was lost therefore surfaces to the phone as an error — the session is gone but the catch branch would keep the user in a dead chat. In the `_endSessionFlow` catch branch, take one confirming `listSessionSnapshot()`; if the session id is **absent**, treat the delete as ended and run the D6 completion branches (success toast included); if the row **survives** or the list read itself fails, keep the error toast and stay in chat (conservative). The residual in-flight-purge window (row still present because the daemon's background delete is mid-kill when the confirming read runs) is accepted: the user saw an explicit error, and the landing screen self-heals on the next connection edge — the incident log shows the daemon closing the connection ~6 s after the last session's purge (`peer_closed` 11:05:09). Pinned by C7. |
| **D8** | **Cleared sessions are tombstoned in `TranscriptsNotifier`.** A `permission_request` frame delivered *after* the delete `ok` can re-create the cleared session's transcript — `forSession` falls back to an empty transcript (`chat_models.dart:535-542`) and `_onEvent` applies the event to it — and while the chat State is still alive (the ~300 ms exit transition), its listener could schedule an `isDismissible:false` sheet over the landing screen. `clearSession` adds the id to a tombstone set; `_onEvent` drops events for tombstoned ids. The tombstone mirrors the daemon's `purged` set exactly (MADR 0093 D3): removed when the id reappears in a host snapshot (`syncFromMeta`) and on `clearAll`. The residual sheet arm is bounded — it needs the daemon to emit an ask after the delete `ok` (structurally rare: Purge tears down the SSE pump before the server-side delete, `internal/provider/httpagent/session.go:749-764`) and the Android sheet remains back-dismissible because it sets no `PopScope` (base `popDisposition` is `pop`, `navigator.dart:382-390`) — but the tombstone removes the class entirely. Pinned by C8. |

### Consequences

* Good, because the positive requirement holds on every completion
  path: chat still current → the guarded pop returns the user to the
  sessions screen; user already left → they are already there and the
  list re-reads via the revision bump; a modal on top at completion →
  it is retired and the router-aware exit lands the user on the
  sessions screen with a refreshed list (D6). Ending a session never
  requires a back press or any other action to land on the main
  session screen.
* Good, because a back press during a slow delete can no longer empty
  the navigator: the black screen is structurally impossible from this
  flow.
* Good, because the landing screen is refreshed exactly once more when
  the user left mid-delete, showing the post-delete list instead of a
  stale row.
* Good, because the fix is pinned by a regression test that corrupts
  the navigator (assertion failure) without D1.
* Bad, because when the user leaves mid-delete and the delete *fails*,
  the error notification is shown while the user is already on the
  landing screen, whose list still shows the session — matching the
  pre-existing "End session failed" behavior, now from the landing
  screen instead of the chat.
* Neutral, because the sessions-revision notifier is a monotonic
  counter bumped only on this path; it does not re-fire on unrelated
  state changes.
* Bad, because the D7 confirming list read adds one `session.list`
  RPC to the delete-failure path, and the conservative fallback (row
  present or read failed) keeps the pre-existing error-toast behavior
  inside the residual in-flight-purge window — visible, self-healing,
  but not a full landing.
* Bad, because D8 changes `TranscriptsNotifier` semantics: any
  consumer that could legitimately see a cleared id reappear (a
  re-created session with the same id) depends on the tombstone being
  removed by `syncFromMeta`/`clearAll` — mirrored from the daemon's
  `purged` set, so the pattern is proven, but it is new phone-side
  state.

### Confirmation

* **C1.** `test/chat_end_session_navigation_test.dart` "back press
  while the delete is in flight leaves the sessions list intact": the
  fake client's `deleteSession` is gated by a `Completer`; the test
  presses back 100 ms into the exit transition, then completes the
  delete. Without D1 the test fails with the navigator dispose
  assertion (the popped-last-page corruption); with D1+D2 it passes and
  the landing screen shows the empty state with the ended row gone.
* **C2.** The original repro ("ending the only session in chat returns
  to a populated sessions list") still passes — the happy path is
  unchanged.
* **C3.** Related suites pass: `sessions_screen_test.dart`,
  `chat_render_test.dart`, `replaced_close_test.dart` (68 tests).
* **C4.** `dart format --output=none --set-exit-if-changed` and
  `flutter analyze` clean on the four touched Dart files.
* **C5.** Manual device check: end the only session and press back
  immediately; the landing screen must appear populated (empty state
  for a single session) and the ended row must disappear without
  restarting the app.
* **C6.** `test/chat_end_session_navigation_test.dart` "a permission
  sheet open at delete completion still lands on the sessions screen":
  with the delete gated, a `permission_request` event injected via
  `TranscriptsNotifier.debugOnEvent` opens the sheet; completing the
  delete must retire the sheet and land the user on the sessions
  screen with the post-delete list via the router-aware exit — no
  user action (pins D6).
* **C7.** `test/chat_end_session_navigation_test.dart` "a delete whose
  ok was lost still lands on the sessions screen": the fake client's
  `deleteSession` throws (timeout) while its list snapshot omits the
  session; the catch branch's confirming read classifies the delete as
  ended, shows the success toast, and runs the D6 exit — no "End
  session failed" toast, no dead chat (pins D7). Contrast case: the
  snapshot still contains the session → error toast, user stays in
  chat.
* **C8.** `test/chat_end_session_navigation_test.dart` (or the
  transcripts suite) "events for a cleared session are dropped": after
  `clearSession(sid)`, a `permission_request` for `sid` injected via
  `debugOnEvent` must not re-create the transcript (no
  `pendingPermissions`, no sheet) (pins D8).

## Pros and Cons of the Options

### Option 1: Guard the auto-pop with `ModalRoute.isCurrent`; bump a sessions-revision notifier

* Good, because it closes both defects: the black screen and the stale
  row after a mid-delete back press.
* Good, because the guard is the standard Flutter idiom for "am I still
  the top route", and the revision notifier mirrors the existing
  `SessionSynchronizer`/`ThemeModeController` patterns in this app.
* Good, because the failing test fails hard without the fix (navigator
  assertion), not with a cosmetic difference.
* Bad, because it adds one small provider (`sessionsRevisionProvider`)
  to app state.

### Option 2: Guard the auto-pop with `ModalRoute.isCurrent` only

* Good, because the black screen is the reported bug and the guard is a
  two-line change with no new state.
* Bad, because the landing screen's refresh raced the in-flight delete:
  the ended session would keep showing as a stale row until the next
  natural refresh (pull-to-refresh, connection event, or the incidental
  socket drop). The incident log shows exactly that drop, but it is not
  contractual — the row may persist for minutes.

### Option 3: Replace the pop with `context.go('/sessions')` on success

* Good, because the exit goes through go_router, so the router state
  cannot be emptied the same way.
* Neutral, because go_router preserves matched page identity — the
  `/sessions` page key is stable, so the landing screen State would
  survive the `go`.
* Bad, because `go` replaces the whole stack, discarding the pop exit
  animation and the predictive-back gesture, and it does not fire
  `didPopNext` (a `go` exit notifies `didRemove`, not `didPopNext`),
  so the refresh-on-return design that replaced awaiting push futures
  (MADR 0046 L-12) would have to be re-wired separately.
* Bad, because it does not fix the stale-row race either — the
  `didPopNext` refresh still runs before the delete completes.

### Option 4: Await the chat push future on the landing screen again

* Good, because the refresh would happen exactly at pop time with the
  pop result available.
* Bad, because it reintroduces the documented trap: a chat exited via a
  location change (`go`, notification tap) never resolves its push
  future, which wedged row taps until process death (the reason
  `sessions_screen.dart:983` moved to `didPopNext`).

### Option 5: Rely on the incidental socket drop / next natural refresh

* Good, because zero code changes on the happy path.
* Bad, because the daemon's connection behavior after the last session
  ends is unguaranteed (and absent for non-last sessions), and the
  black screen itself has no natural refresh at all — the user must
  restart the app or trigger a notification navigation.

## More Information

### Failure mechanism (code)

`chat_screen.dart` `_endSessionFlow` (line ~1222): confirm dialog →
`client.cancel` (best-effort) → `client.deleteSession` →
`dropSessionAsks` → `clearSession` → success toast →
`Navigator.of(context).pop(true)`. The State survives a user-initiated
pop through the exit transition (~300 ms), so `!mounted` (line 1271)
does not fire when the delete completes in that window. The
unconditional pop then targets the navigator's top route — the
`/sessions` page. go_router's `onPopPage` →
`_completeRouteMatch` (`delegate.dart:184–200`) removes the match and
`notifyListeners()`s without checking `isNotEmpty`; the Router rebuilds
with zero pages. The debug assert at `delegate.dart:176` ("You have
popped the last page off of the stack") fires in debug; release shows
the black frame the operator reported.

Why the repro test initially passed: it only exercised the happy path
(no user back press during the delete). The daemon's own comment
(`internal/ws/server.go` `dispatchAsync`, lines 689–698) confirms the
delete can take seconds — "a grok ACP subprocess, or an opencode engine
cold boot" — which is the realistic window for the back press.

### Modal-path probe evidence (D6)

Probe (2026-08-16): same harness as C1/C2 with a gated fake
`deleteSession`; a `permission_request` event injected via
`TranscriptsNotifier.debugOnEvent` while the delete is pending; the
sheet (`isDismissible: false`) is up when the gate completes. Measured
after `pumpAndSettle`:

* `deleteSession` called; success toast shown; sheet gone.
* Chat screen **still mounted**; `ModalRoute.isCurrent == true`,
  `isActive == true`, `Navigator.canPop() == true`.
* Sessions screen **not** on screen — no pop ever fired.

So the guarded pop evaluated `isCurrent == false` at guard time even
though `clearSession` ran first and retired the sheet synchronously.
The installed Flutter SDK explains it: `Route.isCurrent` is computed
from the last history entry satisfying `_RouteEntry.isPresentPredicate`
(`navigator.dart:584–595`); `isPresent` is true for every lifecycle
state up to and **including** `_RouteLifecycle.remove`
(`navigator.dart:3494–3497`). `NavigatorState.removeRoute`
(`navigator.dart:5693–5711`) runs one `_flushHistoryUpdates` pass with
`rearrangeOverlay: false`; the retired sheet's entry did not advance
past the present states in that pass, so the chat route was not the
last present entry at guard time. One frame later the entry had
advanced and `isCurrent` flipped true — with nothing left to pop. The
first D6 mechanism (rely on `clearSession` restoring `isCurrent`
synchronously) is therefore disproven; the router-aware exit (D6)
removes the frame-timing dependence entirely.

### Incident evidence

* Host log `~/Library/Logs/mcremote/mcremote.err.log`: `session closed
  purge=true` at 11:05:03; `peer_closed` 11:05:09; re-auth 11:05:13;
  next session created 11:05:35.
* go_router 17.5.0 `delegate.dart` (pub cache) — pop-of-last-page
  mechanics above. go_router 17.3.0 is also cached; 17.5.0 is what
  `flutter pub deps` resolves for this app.
* `session.delete` handler: replies `ok`; no broadcast to connected
  clients (`internal/ws/server.go` `handleSessionDelete`). The
  provider-internal `session.deleted` SSE events
  (`internal/provider/kilo/lifecycle.go:83`) are for child agent
  sessions on the host, not the phone.

### Implementation state

Committed and pushed baseline — `81c4fb0` "fix(chat): Guard end-session
pop and add sessions refresh notification":

| Change | Where |
| --- | --- |
| `ModalRoute.isCurrent` guard on the end-session pop | `chat_screen.dart` `_endSessionFlow` |
| `sessionsRevisionProvider` bump when the chat is no longer current | `chat_screen.dart`; provider in `state/app_providers.dart` |
| `ref.listen(sessionsRevisionProvider, …)` → `_refresh()` | `sessions_screen.dart` `build` |
| Happy-path repro test | `test/chat_end_session_navigation_test.dart` |
| Race regression test (gated `deleteSession`, back press mid-transition) | `test/chat_end_session_navigation_test.dart` |

Planned (companion plan 0094-PLAN): the D6 modal-path exit
(`hadModal` capture + revision bump + `context.go('/sessions')`) and
its C6 regression test — committed in the working tree's per-phase
commits (`72adc47`, `de7ea5d`, `0e11ae1`, CI green). Post-assessment
amendments from the socratic dialectic: D7 (confirming list read in
the delete-failure catch, C7), D8 (cleared-session tombstone in
`TranscriptsNotifier`, C8), and the D6 wording corrections (ordered
branches; documented `hadModal` microtask direction). The modal path
was a proven residual defect in `81c4fb0` (probe above).

### Explicit non-decisions

* No change to `session.delete` semantics (0093 D2 stays).
* No daemon broadcast on delete; the phone refreshes from `session.list`
  via the revision notifier.
* No re-introduction of awaiting the chat push future (MADR 0046 L-12).
* The normal exit stays `Navigator.pop` with the `isCurrent` guard;
  `context.go('/sessions')` is used only on the modal-race branch,
  where the pop provably cannot fire synchronously (D6).
* No frame-timing-dependent pop (post-frame re-evaluation): the probe
  showed the lag exists but did not bound it to one frame.
* The `!mounted` guard stays as the outer check; it is necessary but
  not sufficient.
