---
status: proposed
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

* Ending a session from inside chat must land on the sessions screen —
  never a blank frame with no route left.
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

### Consequences

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
* Bad, because `go` replaces the whole stack and the landing screen
  State may be recreated, discarding the refresh-on-return design that
  `didPopNext` (MADR 0046 L-12) deliberately replaced; a `go` exit also
  loses the back-swipe/predictive-back animation of a pop.
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

### Software written in the working tree

| Change | Where |
| --- | --- |
| `ModalRoute.isCurrent` guard on the end-session pop | `chat_screen.dart` `_endSessionFlow` |
| `sessionsRevisionProvider` bump when the chat is no longer current | `chat_screen.dart`; provider in `state/app_providers.dart` |
| `ref.listen(sessionsRevisionProvider, …)` → `_refresh()` | `sessions_screen.dart` `build` |
| Race regression test (gated `deleteSession`, back press mid-transition) | `test/chat_end_session_navigation_test.dart` |
| Happy-path repro test | `test/chat_end_session_navigation_test.dart` |

### Explicit non-decisions

* No change to `session.delete` semantics (0093 D2 stays).
* No daemon broadcast on delete; the phone refreshes from `session.list`
  via the revision notifier.
* No re-introduction of awaiting the chat push future (MADR 0046 L-12).
* No `context.go('/sessions')` replacement of the pop.
* The `!mounted` guard stays as the outer check; it is necessary but
  not sufficient.
