---
status: proposed
date: 2026-08-16
decision-makers: [Project Owner]
consulted: [Implementer]
informed: [Operators of macos-laptop, Android phone clients]
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Close the residual end-session defects 0094 left reachable, and fix the request-timeout and idempotency gaps behind them

## Context and Problem Statement

MADR 0094 ("Ending the only session from inside chat must return to a
populated sessions list") was accepted on 2026-08-16 (`f047456`). This
record is the assessment pass over that decision and the surrounding
tree: does the implementation match the record, does the record's own
positive requirement actually hold on every path, and what else does a
deep read of the session-lifecycle code surface.

Baseline measured at `b0e7261` (2026-08-16):

| Gate | Result |
| --- | --- |
| `go vet ./...` | clean |
| `go test ./...` | one failure — `TestSetupWritesDefaultMcrelayConfig` (F8) |
| `flutter analyze` | no issues |
| `flutter test` | 992 passed, 3 skipped |
| Marker sweep (`TODO`/`FIXME`/`XXX`/`not implemented`) | no live markers in `internal`, `cmd`, `apps/mobile/lib` |
| Go packages with no test file | `scripts/smoke-protocol`, `internal/debugserve`, `cmd/mcremote`, `cmd/mcrelay` |
| Statement coverage | 63.5% module-wide; `internal/ws` 75.3%, `internal/session` 75.8%; lowest meaningful `internal/procutil` 41.5% |

**0094 D1–D8 are all present in the tree and match the record.** The
guarded pop, `sessionsRevisionProvider`, the sessions-screen listener,
the `_completeEndSessionFlow` extraction, the D7 confirming read and the
D8 `_cleared` tombstone are each where the MADR says they are. The
record's diagnosis of the black frame against the go_router 17.5.0
source is correct, and the D6 probe that disproved the first mechanism
attempted there is exactly the evidentiary standard `AGENTS.md` asks
for.

Three things the assessment nevertheless found, in the record itself:

1. 0094's Consequences claim the positive requirement "holds on every
   completion path" and that "the black screen is structurally
   impossible from this flow". Both are true of the delete's own pop.
   Neither is true of the screen: the same rogue-pop primitive is still
   reachable from the chat's own error toast (F2), and the D6 branch
   table enumerates three modal kinds while at least five other routes
   can sit above the chat when the delete lands (F3).
2. D7 states its conservative fallback as "row present **or** read
   failed". A third case exists and is unhandled: the read *succeeded*
   and is incomplete. Classifying a purge from a partial snapshot
   violates the invariant `syncFromMeta` documents and MADR 0056 H-6
   locked (F1).
3. 0094-PLAN Steps 5 and 12 institutionalise a broken local gate —
   `make preflight` is declared unusable on macOS and replaced by the
   mobile trio — for what is a one-line defect in an unrelated test
   (F8).

The architectural question: **0094 is accepted and its rationale must
not be rewritten in place; how are the residual defects of the class it
closed, plus the request-lifecycle gaps sitting behind D7, recorded and
sequenced?**

Scope: the phone's end-session and request paths (`chat_screen.dart`,
`sessions_screen.dart`, `transcripts_notifier.dart`,
`mcremote_client.dart`), the daemon's async dispatch and idempotency
ledger (`internal/ws`), session purge (`internal/session`,
`internal/provider/acphttp`), and the `internal/cli/service` test gate.
No protocol change is proposed; no change to the meaning of
`session.delete` (0093 D2 stays).

## Decision Drivers

* 0094's own first driver — *every* completion path of the delete lands
  the user on the sessions screen with no further action — is the bar
  the residual findings are measured against, not a new requirement.
* Local state must never be pruned from a snapshot the host did not
  vouch for as complete (MADR 0056 H-6, restated verbatim in
  `transcripts_notifier.dart:877-882`).
* A client that retries must never receive silence. A response frame
  the daemon declines to write is indistinguishable on the phone from a
  dead link, which is the failure shape MADR 0093 was written about.
* A green local gate is worth more than a documented workaround for a
  red one: `make preflight` should be runnable on the machine the work
  happens on.
* Accepted records are superseded, not edited. 0094 keeps its history;
  corrections land here.
* Findings must be pinned by tests that fail without the fix, and
  external-CLI behaviour by live-tagged tests (`AGENTS.md`).

## Considered Options

* Option 1: Record the findings here as a severity-ranked backlog with a
  companion plan, remediating S1 first
* Option 2: Amend MADR 0094 in place with new decisions D9–D12
* Option 3: Fix only the two S1 navigation defects and drop the rest
* Option 4: Treat 0094 as closed; carry the findings as informal debt

## Decision Outcome

Chosen option: **"Record the findings here as a severity-ranked backlog
with a companion plan, remediating S1 first"**, because 0094 is accepted
and its rationale is the historical record of a decision that was
correct for the mechanism it addressed — the residual defects are a
distinct class (routes 0094 did not enumerate, and the request
lifecycle behind D7), and folding them into that record would rewrite
its reasoning rather than extend it.

Severity scale (as 0071):

* **S0** — security / data loss / common-path break
* **S1** — user-visible wrong behaviour or silent incomplete product path
* **S2** — incomplete wiring / product gap with code half-present
* **S3** — hardening, performance, maintainability

| ID | Finding | Severity |
| --- | --- | --- |
| **F1** | The D7 confirming read treats an incomplete `session.list` as proof of purge | S1 |
| **F2** | The "Send failed → Sessions" toast action reproduces the 0094 rogue-pop class from a root overlay | S1 |
| **F3** | D6's branch table enumerates three modal kinds; five other routes can sit above the chat at delete completion | S1 |
| **F4** | The sessions-list End action has no D7/D8 parity | S2 |
| **F5** | `dispatchAsync` can return without writing any frame on all three idempotency paths | S2 |
| **F6** | `idempotencyLedger.purgeLocked` does not actually enforce `maxEntries` | S3 |
| **F7** | Two tombstone sets grow without bound — phone `_cleared`, daemon `purged` | S3 |
| **F8** | `TestSetupWritesDefaultMcrelayConfig` asserts systemd directives against the host OS, so it fails on macOS and drives real `launchctl` | S2 |
| **F9** | The request-timeout ladder is uncoordinated between phone and daemon, in both directions | S2 |
| **F10** | goose sessions survive `session.delete` provider-side and stay listed by `agent_sessions.list` | S2 |
| **F11** | `resumeStore.validate` compares a bearer token with `==` | S3 |

Locked decisions:

| ID | Decision |
| --- | --- |
| **D1** | **F1, F2 and F3 are remediated first, as one round.** All three are instances of the class 0094 set out to close, and F2/F3 falsify two sentences of its Consequences. Their fixes are independent of each other and of the daemon. |
| **D2** | **The D7 absent-classification requires `snap.complete`.** An incomplete or degraded snapshot takes the existing conservative branch (error toast, stay in chat), because a partial list is not evidence of removal. This restates MADR 0056 H-6 at the one call site that reads a snapshot without honouring it; it does not change what a complete snapshot means. |
| **D3** | **Navigation from a root-overlay callback is router-aware, never `Navigator.pop`.** A notification inserted with `rootOverlay: true` outlives the route that created it by design (`top_notification.dart:90-91`); `mounted` on the creating State is therefore not a proxy for "my route is on top". The `Sessions` action uses `context.go('/sessions')`. |
| **D4** | **The end-session completion branch is decided by route state, not by a flag list.** Enumerating modal kinds is unbounded by construction — every new sheet in `chat_screen.dart` is a new way to miss the landing. The discriminator is the chat route's own state: still current ⇒ guarded pop; still in the navigator with something above it ⇒ revision bump + `context.go('/sessions')`; no longer in the navigator ⇒ bump only (the user popped us and is already there). **This mechanism is not yet proven and must be probed against the installed Flutter SDK before implementation**, exactly as D6's first mechanism was disproven: `Route.isActive` and `Route.isCurrent` are both computed from `_RouteEntry` lifecycle predicates whose settling time 0094's probe already showed is not one frame. If the probe disproves it, the fallback is D4b. |
| **D4b** | **Fallback if D4's probe fails: gate the chat's other route-opening entry points on `_endingSession`.** Fork, the diff and diagnostics dialogs, the config-option sheet and the message-action sheet become unavailable while a delete is in flight, the way a second End tap already is (`chat_screen.dart:1213`). Narrower and provable, but it leaves the permission/question sheets — which arrive unbidden over the socket — on the `hadModal` path. |
| **D5** | **The sessions-list End action gets D7/D8 parity, not the D6 navigation.** The confirming read and the `clearSession` / `dropSessionAsks` cleanup apply there too; the navigation branches do not, because the user is already on the landing screen. 0094 scoped this path out on navigation grounds, which is correct for navigation and does not cover classification. |
| **D6** | **The daemon never declines to write a frame for a request it accepted.** Every `dispatchAsync` early return that has no frame to send writes a typed error envelope instead. Silence is reserved for a connection that is gone. |
| **D7** | **Client request timeouts strictly exceed the daemon's per-method allowance.** The daemon's `asyncOpTimeout` is authoritative for how long an operation may take; the phone's timeout is a backstop for a dead link, not a competing deadline. Where the two are equal today the daemon's own error frame is pre-empted by a client timeout, and where the client is shorter a successful late reply is discarded. `session.delete` additionally gets an explicit daemon allowance sized to what a purge costs rather than the 30 s default. |
| **D8** | **Both tombstone sets are bounded.** Neither `_cleared` nor `Manager.purged` needs to outlive the window it protects (seconds, not the process lifetime), and every other map in either codebase is pruned. |
| **D9** | **`TestSetupWritesDefaultMcrelayConfig` pins `installOS` and stubs the service manager**, restoring `make preflight` as the local gate and retiring the 0094-PLAN Step 5/12 workaround. |
| **D10** | **goose gets a capability-gated `Purge`.** The ACP surface already defines `session/delete` and advertises support through `SessionCapabilities.Delete`; acphttp already uses the identical gate for `session/list`. Where the agent does not advertise it, `Close` remains the behaviour and the limitation is documented rather than silently accepted. |

### Consequences

* Good, because 0094's positive requirement becomes true of the screen
  and not only of the delete: after D3 and D4 there is no reachable path
  from the chat that empties the navigator or strands the user above a
  purged session.
* Good, because D2 removes the one call site that prunes local state
  from a snapshot the host did not vouch for, restoring a single
  consistent reading of MADR 0056 H-6.
* Good, because D6 and D7 attack the entry condition for D7-of-0094
  rather than only its consequence: a delete whose `ok` was lost is
  rarer when the client stops racing the daemon's own deadline and the
  daemon stops returning silence to a retry.
* Good, because D9 makes the full preflight runnable locally, so the
  next plan does not have to document a substitute gate.
* Neutral, because D4 replaces state (three booleans) with a route
  query; if its probe fails, D4b is a strictly smaller change that
  trades coverage for provability.
* Bad, because D4 is the one decision here whose mechanism is not yet
  established. It is recorded as a decision about *what* discriminates
  the branches; the plan must not begin implementation before the probe.
* Bad, because D7 changes timeouts on nine call sites across two
  languages with no shared source of truth, so the invariant can drift
  again. The proposed table test (C-h) is the only thing that would
  hold it.
* Bad, because D10 changes what ending a goose session destroys. A user
  who relied on ending a session in the app while keeping the
  goose-side session importable loses that; the alternative is the
  current silent divergence between what the confirm dialog promises
  and what a provider-native session list shows.

### Confirmation

Each finding is pinned by a test that fails without its fix. Numbering
continues 0094's C-series.

* **C9 (F1).** `chat_end_session_navigation_test.dart`: a delete that
  throws while the fake's snapshot omits the row *and* reports
  `complete: false` must keep the error toast and the chat. Fails today
  — the row's absence alone drives the success path.
* **C10 (F2).** With a "Send failed" toast on screen, press back and tap
  `Sessions` inside the exit transition. Without D3 the navigator is
  emptied (debug assertion / black frame); with it the user lands on the
  sessions screen. A second case pins the post-transition state: the
  action must still navigate, not silently do nothing.
* **C11 (F3).** With the delete gated, open the session-diff dialog (an
  untracked modal) and, separately, push a forked chat via
  `_forkSession`; completing the delete must land on the sessions screen
  in both. Fails today on the bump-only branch.
* **C12 (F4).** `sessions_screen_test.dart`, mirroring 0094 C7: a delete
  whose `ok` was lost must clear the transcript and drop the session's
  asks and must not show "End failed"; the contrast case (row survives)
  keeps the error.
* **C13 (F5).** `internal/ws`: a parked `idemWait` waiter whose in-flight
  peer calls `fail` must receive a frame, not nil; and `dispatchAsync`
  must write an error envelope rather than returning silently on the
  empty-replay and nil-wait paths.
* **C14 (F6).** `purgeLocked` with one in-flight entry and `maxEntries`
  finished entries must drop to the cap regardless of map iteration
  order. Deterministic by construction (the current code's outcome is
  order-dependent, so the test must assert the invariant, not one run).
* **C15 (F7).** Bound tests on both sets: N deletes leave a bounded
  `_cleared` / `purged`, and the tombstone still suppresses within its
  window.
* **C16 (F8).** `go test ./internal/cli/service/` passes on macOS with no
  `launchctl` invocation, and `make preflight` reaches the mobile
  stages. **Probe (2026-08-16, this machine):** adding
  `defer service.OverrideInstallOS("linux")()` plus an
  `OverrideRunSystemctl` stub — the pair `TestSetupIdempotentRerun`
  already carries — turns the failure into a pass; the edit was reverted
  after measuring.
* **C17 (F9).** A table test asserting, per method, that the phone's
  request timeout exceeds `asyncOpTimeout`. Fails today for
  `models.list`, `agents.list`, `agent_sessions.list` (client shorter)
  and for `session.delete`, `session.close`, `session.fork`,
  `session.history`, `provider.auth_catalog` (equal).
* **C18 (F10).** Live-tagged goose test (`-tags live_goose`, per
  `AGENTS.md`) that a session ended through `session.delete` no longer
  appears in `agent_sessions.list`, plus a unit test that the purge is
  skipped without the capability.
* **C19.** No regressions: `go test ./...`, the mobile trio, and
  `make preflight` all green — the last of these for the first time on
  this machine.

## Pros and Cons of the Options

### Option 1: Record the findings here with a companion plan, S1 first

* Good, because 0094 keeps its rationale intact and this record states
  the relationship explicitly, which is the repository's own convention
  for correcting an accepted decision.
* Good, because severity ranking separates the three defects that
  falsify 0094's stated requirement from the seven that are debt found
  while reading around it.
* Good, because the daemon-side findings (F5–F7, F9, F10) have no
  phone-side dependency and can land in any order after the S1 round.
* Bad, because it is a twelfth open document in a chain where 0093 and
  0094 already cover adjacent behaviour, and a reader must follow three
  records to understand one screen.

### Option 2: Amend MADR 0094 in place with D9–D12

* Good, because one record would then describe the whole end-session
  flow.
* Bad, because two sentences of 0094's Consequences would have to be
  rewritten to accommodate F2 and F3, destroying the record of what was
  believed at acceptance and why — the failure mode the supersede
  convention exists to prevent.
* Bad, because F5–F11 are not about ending a session at all; they would
  arrive in 0094 with no context.

### Option 3: Fix only the two S1 navigation defects and drop the rest

* Good, because F2 and F3 are the user-visible ones and the fixes are
  small.
* Bad, because F1 is also S1 and is a silent data defect rather than a
  visible one: a wrongly cleared transcript and a wrong success toast
  leave no trace for the operator to report.
* Bad, because F8 leaves the local gate red, so the next plan documents
  the same workaround again.

### Option 4: Treat 0094 as closed; carry the findings as informal debt

* Good, because zero documentation cost.
* Bad, because the black-screen class was reported by an operator, not
  found by a test, and F2/F3 leave two paths to the same screen state.
  An unrecorded known defect of a class already escalated once is the
  worst of both.

## More Information

### Finding detail

**F1 — the D7 confirming read treats an incomplete list as proof of purge (S1).**
`chat_screen.dart:1279-1287` sets `rowSurvives` from
`snap.sessions.any(...)` alone. `ListSnapshot` reports
`Complete: false, Degraded: true, Skipped: n` when the durable store
enumeration skipped corrupt rows (`internal/session/manager.go:1402-1408`),
and `handleSessionList` forwards those fields on a **successful**
`session.list_result` (`internal/ws/server.go:1568-1575`) — the phone
parses them (`models.dart:162-182`) and every other consumer honours
them (`sessions_screen.dart:205`, `session_synchronizer.dart:113`).
`syncFromMeta` states the rule the D7 branch breaks:
"Incomplete/degraded snapshots must never prune local state"
(`transcripts_notifier.dart:877-882`, MADR 0056 H-6). Consequence: a
non-live session whose record was among the skipped rows is classified
as purged — success toast, transcript cleared, id tombstoned — while
the host still has it; the row returns on the next complete snapshot.
The store-*error* case is already safe: the daemon replies with an error
frame, so `listSessionSnapshot()` throws into the existing conservative
`catch`.

**F2 — the "Send failed" toast action reproduces the rogue-pop class (S1).**
`chat_screen.dart:997-1005` attaches `actionLabel: 'Sessions'` with
`onAction: () { if (mounted) Navigator.of(context).pop(); }`.
`showTopNotification` inserts into the **root** overlay — its own
comment: "rootOverlay, so this survives route changes for the app's
lifetime" (`top_notification.dart:90-91`) — and an action notification
stays up for 6 s (`_kActionDuration`, `:13`). The button is therefore
live after the user leaves the chat. During the route's exit transition
the State is still mounted, which is the exact fact 0094 D1 rests on, so
`mounted` is true and `pop` targets the navigator's top route — the
`/sessions` page — emptying the configuration the way `delegate.dart`'s
own assert describes. After the transition the same button is a silent
no-op. Both halves argue for the same fix (D3): a callback that outlives
its route must navigate by location, not by popping.

Reproduction note (execution, 2026-08-16): the defect only reproduces
when the back press is delivered through the system route
(`tester.binding.handlePopRoute()`). The notification card is
`Positioned` across the top of the screen and covers the app bar, so
`tester.pageBack()`'s tap lands on the toast and the route never pops —
the first draft of both regression tests passed for that reason.
Driven correctly, the mid-transition tap empties the navigator (measured:
sessions AppBar and chat both absent, two navigator exceptions). That the
toast physically covers the back button for its 6 s lifetime is a
separate UX observation, not a finding here.

**F3 — the completion branches enumerate three modals; five other routes qualify (S1).**
`_completeEndSessionFlow` captures
`hadModal = _permissionSheetOpen || _questionSheetOpen || _openAlwaysConfirmRoute != null`
(`chat_screen.dart:1311-1314`). `_endingSession` (`:1213`) guards only a
second End tap — nothing else in the screen reads it — so while the
delete is in flight the user can still open the long-press message-action
sheet (`:542`), the session config-option sheet (`:680`), the
diagnostics dialog (`:1088`), the session-diff dialog (`:1131`), and,
from the same overflow menu, `_forkSession`, which opens the forked chat
with `context.push('/sessions/<id>')` (`:1196`). In each case the
completion evaluates `route.isCurrent == false` with `hadModal == false`
and takes the bump-only branch: the user is left above a chat whose
transcript has just been cleared and whose session is purged, and
dismissing the sheet or backing out returns them into that dead chat
rather than the sessions screen. The daemon's own comment on how long a
lifecycle op takes (`internal/ws/server.go:689-698`) is what makes the
window realistic. 0094-PLAN's P5 row also mis-states the mechanism for
this case: `mounted == false` holds for a `go` exit (notification tap),
not for `context.push`, which keeps the chat State alive underneath.

**F4 — the sessions-list End action has no D7/D8 parity (S2).**
`sessions_screen.dart:1222-1256`. The success path does `clearSession`
and `dropSessionAsks`; the failure path refreshes and shows "End failed"
with no classification. A lost `ok` therefore produces an error toast
for a delete that happened, with the local transcript retained and the
session's asks still pending. The list row does disappear on the
refresh, which is why this is S2 and not S1. 0094 scoped this path out
because "the user is already on the main screen there" — true of
navigation, and orthogonal to classification and local-state cleanup.

**F5 — `dispatchAsync` can return without writing a frame (S2).**
`internal/ws/server.go:858-874`: the `idemReplay` branch writes only
`if len(frame) > 0`, and the `idemWait` branch only `if len(f) > 0`;
both then `return`. `idempotencyLedger.fail` closes `done` **and**
deletes the entry (`idempotency.go:171-176`), so a waiter parked in the
closure at `:77-88` re-reads the map, misses, and returns nil — the nil
case above. The retrying client then has no response for its request id
and waits out its own timeout; for `session.delete` that is another 30 s
and then the D7 path. `complete(deviceID, env.ID, nil)` at `:893` can
likewise finish an entry with no frame, so a later retry replays
nothing.

**F6 — `purgeLocked` does not enforce `maxEntries` (S3).**
`idempotency.go:186-196`: the eviction loop returns from the function as
soon as map iteration reaches an unfinished entry, before scanning the
rest. Go randomises map iteration order, so with any request in flight
the cap is enforced probabilistically and the ledger can stay above 256
indefinitely. The 10-minute TTL is the only real bound.

**F7 — two tombstone sets grow without bound (S3).**
Phone: `TranscriptsNotifier._cleared` (`transcripts_notifier.dart:129`).
On a complete snapshot `syncFromMeta` prunes every other side table with
`removeWhere((id, _) => !liveIds.contains(id))` (`:898-905`), but
`_cleared` gets only `removeAll(liveIds)` (`:887`) — the opposite
direction, which by construction never matches a purged id. Daemon:
`Manager.purged` (`manager.go:229`) is cleared only by `clearPurged` on
Create of the same id (`:657`); ids are UUIDs, so an entry is permanent
for the process. Per-entry cost is one id, and both are the sole
unbounded maps in code that prunes carefully everywhere else — the
resume store expires entries (`ws/resume.go:87-94`) and the relay runs a
dedicated rate-map prune (`relay/server.go:286-294`). Relevant because
MADR 0089 set the bar for a daemon expected to run for weeks.

**F8 — the service setup test asserts systemd against the host OS (S2).**
`internal/cli/service/setup_test.go:136-176` calls `service.Setup`
without pinning `installOS`, so on macOS `Setup` correctly writes a
launchd plist and the `KillMode=mixed` / `PrivateDevices=true`
assertions read a plist. It is the only failure in `go test ./...` here,
and the reason 0094-PLAN Steps 5 and 12 declare `make preflight`
unusable locally. The sibling `TestSetupIdempotentRerun` (`:363-371`)
already carries the fix *and* a comment describing this exact failure
mode. The test also drives real `launchctl` on the developer's machine —
the run emitted `Boot-out failed: 3: No such process` — so
`OverrideRunLaunchctl` belongs there too. Probe recorded under C16.

**F9 — the timeout ladder is uncoordinated in both directions (S2).**
Daemon `asyncOpTimeout` (`internal/ws/server.go:899-912`): 120 s create,
60 s prompt / `models.list` / `agents.list` / `agent_sessions.list`,
30 s default. Phone `request()` defaults to 30 s
(`mcremote_client.dart:2791`), overridden only for create (120 s) and
prompt (60 s).

| Method | Client | Daemon | Effect |
| --- | --- | --- | --- |
| `models.list`, `agents.list`, `agent_sessions.list` | 30 s | 60 s | the completer is dropped at 30 s; a reply the daemon was still entitled to send is discarded, so a slow-but-successful catalog fetch always surfaces as a failure |
| `session.delete`, `session.close`, `session.fork`, `session.history`, `provider.auth_catalog` | 30 s | 30 s | both expire at the same instant, converting the daemon's own `deadline_exceeded` frame into a client timeout plus an idempotent retry — the entry condition for 0094 D7 |
| `session.create` | 120 s | 120 s | same simultaneous expiry, on the longest op |
| `session.prompt` | 60 s | 60 s | same |

`session.delete` inheriting the 30 s default is its own gap: the
opencode/kilo purge budgets 15 s for the engine-side delete *after* a
local teardown (`internal/provider/httpagent/session.go:759-763`), and
the daemon's own comment says these ops "can take many seconds".

**F10 — goose sessions survive `session.delete` provider-side (S2).**
`closeMatching` purges provider-side durable state only for sessions
implementing `provider.PurgeSession` (`manager.go:2044-2051`);
`httpagent` (opencode, kilo) and `codex` implement it, `acphttp` (goose)
does not. acphttp is also the only provider that exposes native durable
sessions to the phone (`ListAgentSessions`, `acphttp/provider.go:189`,
surfaced as `agent_sessions.list`). Ending a goose session therefore
removes the daemon's record while the goose-side session stays listed
and re-importable via `session/load`, against a confirm dialog that says
the agent is stopped and removed. The surface exists:
acp-go-sdk v0.13.5 defines `AgentMethodSessionDelete = "session/delete"`
(`constants_gen.go:29`) and the `SessionCapabilities.Delete` advertisement
(`types_gen.go:4439-4448`), and acphttp already uses the identical
capability gate for `session/list` (`provider.go:193-195`).

**F11 — `resumeStore.validate` compares a bearer token with `==` (S3).**
`internal/ws/resume.go:83`. Every other secret comparison in the tree
uses `subtle.ConstantTimeCompare` (`auth/store.go:532`,
`relay/hub.go:100`, `:251`). Not a vulnerability as reached today:
resume validation runs only after `s.authenticate` accepted the 256-bit
device token (`server.go:950-976`), so only an already-authenticated
device can probe its own token, and a resume miss costs a full reconcile
rather than access. Recorded as the one secret comparison outside the
house rule.

### D4 probe evidence (2026-08-16)

D4 was recorded as a decision about *what* discriminates the completion
branches, with its mechanism explicitly unproven. The probe (0095-PLAN
Step 6) instrumented `_completeEndSessionFlow` to record
`(isActive, isCurrent, hadModal)` at guard time and ran five scenarios
against the installed Flutter SDK, each with a `Completer`-gated
`deleteSession`. Measured:

| Scenario | `isActive` | `isCurrent` | `hadModal` |
| --- | --- | --- | --- |
| S1 — chat current, nothing above | true | true | false |
| S2 — user backed out mid-delete (0094 P2) | **false** | false | false |
| S3 — untracked dialog (`Session diagnostics`) on top | **true** | false | false |
| S4 — pushed forked chat on top (`context.push`) | **true** | false | false |
| S5 — permission sheet retired by `clearSession` (0094 D6) | **true** | false | true |

Two conclusions:

* **F3 is confirmed empirically.** S3 and S4 reach the completion with
  `isCurrent == false` **and** `hadModal == false`, so the committed code
  takes the bump-only branch and strands the user above a purged chat —
  the defect the MADR asserted from a code read.
* **The M1 mechanism holds.** S2 is cleanly separable from S3/S4/S5 by
  `isActive` alone, with no frame-timing dependence: a route the user
  popped leaves the navigator's history, a route with something above it
  does not. D4's wording therefore stands as recorded, and the D4b
  fallback is not needed.

Implemented as: `isCurrent` ⇒ guarded pop; `isActive && !isCurrent` ⇒
revision bump + `context.go('/sessions')`; `!isActive` ⇒ bump only. The
`hadModal` capture and its three-flag read are gone; the flags themselves
remain, since sheet retirement still reads them.

### Implementation state (2026-08-16)

0095-PLAN executed Steps 1–21. `make preflight` is green end to end — the
first time on this machine — with 1003 mobile tests and no Go failures.

| Finding | Outcome | Where |
| --- | --- | --- |
| **F1** | Fixed | `chat_screen.dart` `_endSessionFlow` requires `snap.complete`; pinned by C9 |
| **F2** | Fixed | `_sendPrompt` captures `GoRouter.of(context)` and the action `go`s; pinned by two C10 tests |
| **F3** | Fixed | `_completeEndSessionFlow` branches on `isCurrent`/`isActive` (M1); pinned by two C11 tests |
| **F4** | Fixed | `sessions_screen.dart` `_completeEndSession` + confirming read; pinned by three C12 tests |
| **F5** | Fixed | `dispatchAsync` writes `retry_no_result` instead of returning silently; pinned by C13 |
| **F6** | Fixed — **severity re-measured**, see below | `purgeLocked` scans for a finished victim; pinned by C14 |
| **F7** | Fixed | `_cleared` capped at `kMaxClearedSessions`; `Manager.purged` capped at `maxPurgedIDs`; pinned by C15 |
| **F8** | Fixed | `TestSetupWritesDefaultMcrelayConfig` pins `installOS` and stubs systemctl |
| **F9** | Fixed | `internal/protocol/op_timeouts.json` + `opTimeoutFor`; pinned by C17 in both languages |
| **F10** | **Not fixed — documented limitation**, see below | `acphttp` `Purge` implemented and capability-gated; inert on this goose version |
| **F11** | Fixed | `resumeStore.validate` uses `subtle.ConstantTimeCompare` |
| **F12** | Fixed — **new, found during execution** | `TestCloseAllKeepsSessionsListable`; see below |

Outstanding: **C5 device confirmation (P1–P3, P6–P8) has not been run** —
it needs the `s22+` handset. Every finding's automated confirmation is
green; the MADR therefore stays `proposed` until the operator completes
C5, per 0095-PLAN Step 23.

#### F6 — severity re-measured

The original entry claimed the cap was "a coin flip per call". Measured
during execution, that overstates the single-in-flight case and
understates the busy one. `purgeLocked` sampled **one** random entry per
eviction pass and returned from the whole function if it was unfinished,
so the bail probability scales with the in-flight fraction:

| in-flight entries | cap | worst seen | settles at |
| --- | --- | --- | --- |
| 1 | 256 | 257 | 256 |
| 64 | 256 | 261 | 256 |
| 200 | 256 | 368 | **328** |
| 255 | 256 | 524 | **518** |

With one in-flight entry the overshoot is 1 and self-corrects on the next
call — which is why the first regression test written for this passed.
Under load the ledger settles at roughly **2× `maxEntries`, permanently**.
S3 was the right severity; the mechanism in the original text was not.

#### F10 — goose does not advertise `session/delete`

Probe (2026-08-16, `make live-goose`, installed goose):
`AdvertisesSessionDelete() == false`. `session/delete` is marked UNSTABLE
in acp-go-sdk v0.13.5 and this agent does not offer it, so the
capability-gated `Purge` added for D10 is **correct and inert**: ending a
goose session still removes only the daemon's record, and the
agent-native session remains listed by `agent_sessions.list`.

F10 therefore stands as a **documented limitation**, not a fixed defect.
The code is retained because it costs nothing when the capability is
absent and takes effect the moment goose advertises it; the live test
pins both the capability answer and the behaviour, so an upgrade that
adds `session/delete` will show up as a changed probe rather than a
silent behaviour change. The confirm dialog's copy remains accurate about
the sessions list and inaccurate about the agent's own store on this
version.

#### F12 — `TestCloseAllKeepsSessionsListable` was order-flaky (S2, found during execution)

`CloseAll` writes both rows back-to-back, so their `UpdatedAt` can be
identical; `ListSnapshot` then falls back to its documented `ID >`
tie-break, under which `sess-grok` legitimately precedes `sess-goose`.
The test asserted "newest first" unconditionally, making it a coin flip.
Measured on the untouched baseline `b0e7261`: **5 failures in 30 runs**
(~17%) — so `go test ./...`, `make preflight` and CI were intermittently
red for reasons unrelated to any change under review. Not caused by this
plan; found because Step 16 required a clean full-suite run. The test now
asserts the rule the sort actually implements, including the tie-break;
green 60/60.

### Checked and rejected (not findings)

| Candidate | Why rejected |
| --- | --- |
| D8's tombstone suppressing a re-created session | Ids are UUIDs and `syncFromMeta` lifts the tombstone the moment the host lists the id; pinned by `b0e7261` |
| D8 vs. staged-image release | Already resolved by `d2ba813`; the test asserts the D8 semantics directly |
| D7's confirming read when the store enumeration *errors* | The daemon replies with an error frame, so the read throws into the conservative branch — safe (contrast F1, which is the successful-but-partial case) |
| Relay rate-limiter map growth | Pruned by the R39 sweeper (`relay/server.go:286-294`) |
| Resume-token map growth | `purgeExpiredLocked` on every issue/validate (`ws/resume.go:87-94`) |
| `c.negotiated` read unlocked at `server.go:667` | Written only on the read loop; every cross-goroutine read takes `s.mu` — no race |
| `_endingSession` left true when unmounted (`chat_screen.dart:1218`) | The State is disposed; the flag dies with it |
| Packages without test files (`cmd/*`, `internal/debugserve`) | Thin `main` wiring and an opt-in debug surface; same judgement as 0071 |
| `make preflight`'s Go stages preceding the mobile trio | Ordering is correct; the failure is F8, not the ordering |

### Assessment of MADR 0094 and its plan

Sound, and worth keeping as precedent:

* The failure mechanism is traced to named lines of the resolved
  go_router version rather than asserted, and the pinning test fails
  *hard* (navigator assertion) rather than cosmetically — 0094 C1.
* The D6 probe disproved the first mechanism the record itself proposed,
  and the record says so and explains why, with the SDK line numbers.
  That is the standard `AGENTS.md` asks for when behaviour is inferred.
* The plan names the exact expected failing assertion for every RED step
  and instructs the executor to stop if a different failure appears.

Corrections this record makes:

* "The black screen is structurally impossible from this flow" and "the
  positive requirement holds on every completion path" (Consequences)
  are true of the delete's own pop and not of the screen — F2, F3.
* D7's conservative fallback is stated as two cases (row present, read
  failed); the third — read succeeded, snapshot incomplete — is
  unhandled and takes the success branch — F1.
* 0094-PLAN's P5 row attributes non-interference to `mounted == false`.
  That holds for a `go` exit, not for `context.push` — F3.
* 0094-PLAN Steps 5 and 12 substitute a local gate rather than fixing a
  one-line test defect — F8.

### Test-coverage expansion beyond the pinning tests

* `internal/procutil` (41.5%) is the lowest meaningful package and owns
  process ownership and kill paths that `session.delete` depends on;
  three of the six non-live-tagged `t.Skip`s in the tree are there and
  in `internal/provider` (platform/environment guards). Worth an
  explicit inventory of which skips are environmental and which are
  unwritten.
* `internal/ws` has no test for the `fail`-with-parked-waiter or
  cap-enforcement paths (C13, C14) — the two places the ledger's
  invariants are only asserted by comments.
* The end-session widget suite covers P1–P4b but no untracked-route
  case (C11); the sessions-screen suite has no lost-`ok` case (C12).
* No test anywhere relates a client timeout to the daemon's allowance
  (C17); it is the only invariant here that spans both languages.

### Evidence

* Baseline `b0e7261`, 2026-08-16. `go vet` clean; `go test ./...` one
  failure (F8); `flutter analyze` clean; `flutter test` 992 passed,
  3 skipped.
* F8 probe: `OverrideInstallOS("linux")` + `OverrideRunSystemctl` stub
  turns `TestSetupWritesDefaultMcrelayConfig` from FAIL to PASS on
  darwin; edit reverted.
* acp-go-sdk v0.13.5 (`~/go/pkg/mod/github.com/coder/acp-go-sdk@v0.13.5`)
  for the `session/delete` surface behind F10.

### Explicit non-decisions

* No change to `session.delete` semantics or to the absence of a delete
  broadcast (0093 D2, 0094 D4).
* No re-introduction of awaiting the chat push future (MADR 0046 L-12).
* No protocol version change: F9 adjusts timeouts, not the wire.
* No change to the 0094 D1/D2/D3 mechanism itself — the guarded pop and
  the revision bump stay; D4 here changes only which branch is selected
  when the chat is not current.
* This record does not supersede 0094. It corrects two claims in its
  Consequences and extends its coverage; 0094's decisions remain in
  force.

## Amendment, 2026-09-01 — D4's S2 row is unreachable under go_router 18.0.0

Recorded while executing [0127-PLAN](0127-PLAN-adopt-current-flutter-toolchain.md)
P6a, which bumps `go_router` 17.5.0 → 18.0.0.

The D4 probe above measured S2 — *"user backed out mid-delete"* — as
`(isActive false, isCurrent false)`. Reading those flags at all presumes the
chat `State` is **still mounted** when the delete resolves, which was true on
the SDK the probe ran against and which
`chat_end_session_navigation_test.dart` states in a comment: *"its State stays
mounted until the transition finishes."*

**Under go_router 18.0.0 it is not.** The State is disposed before the in-flight
`session.delete` completes, so `_completeEndSessionFlow`'s opening
`if (!mounted) return;` fired and the guard never reached the route flags.
Measured, with a `Completer`-gated delete:

```text
PROBE endedToast=0  betaRows=1  emptyState=0  listTiles=2  chatScreens=0
PROBE Beta ancestors: DefaultTextStyle < AnimatedDefaultTextStyle < _ListTile < …
```

`chatScreens=0` (no lingering route), `endedToast=0` (the completion never
ran), and the deleted session still rendered as a sessions-list `ListTile`.
The host had deleted it — `deleteCalls == ['sess-b']` passed. So all three
completion effects were skipped: `clearSession`, the toast, and
`sessionsRevisionProvider.bump()` — and the bump is the only thing that
refreshes the list on that path (`chat_screen.dart` → `sessions_screen.dart`'s
`ref.listen`).

### What changed, and why it is not just a probe refresh

Re-measuring S2 against go_router 18 would produce a row that says "State
disposed" — but that is not a discriminator the code can branch on, because by
then `ref` is dead too. The decision underneath D4 is amended rather than its
table:

**The completion's *state* effects no longer run behind the `mounted` guard.**
`clearSession` and `bump()` are not UI. The host has deleted the session, so
the local transcript is stale and the sessions list is wrong regardless of
whether the screen still exists. They now run first and unconditionally, using
notifiers captured in `initState` (the pattern `app_lifecycle.dart:50-58`
already uses for exactly this reason). Only the toast and the navigation — the
two things that genuinely need a live `BuildContext` — remain behind `mounted`,
and the route-flag branch below it is unchanged.

This makes the S1/S3/S4/S5 rows of the D4 table still correct and still
load-bearing, and removes S2's dependence on a timing measurement that a router
major is entitled to change. D4's *mechanism* stands; its *precondition* — that
the State outlives the RPC — was the part that was never guaranteed.

Verified: all 10 tests in `chat_end_session_navigation_test.dart` pass, full
suite `+1358 ~3` on Flutter 3.47.2 / go_router 18.0.0.

### Follow-up not taken here

The confirmed-purge path (0094 D7) in the same method has the same shape: its
`catch` block returns early on `if (!mounted) return;` **twice**, so a delete
whose `ok` was lost while the user backs out still leaves stale local state.
Fixing it means deciding whether a disposed screen should keep issuing
`session.list` round trips, which is a larger question than this amendment.
Named so it is not mistaken for an oversight.
