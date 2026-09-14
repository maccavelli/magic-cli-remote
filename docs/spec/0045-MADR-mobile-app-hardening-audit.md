# MADR 0045: Mobile App Hardening Audit — Findings and Remediation Decisions

<!-- markdownlint-disable MD013 MD060 -->

- **Date**: 2026-07-28
- **Status**: Revalidated against `ff4b2c1`; remediation decisions revised
- **Scope**: `apps/mobile` (41 production Dart sources, 33 Dart tests,
  `android/` project) plus
  daemon↔app protocol parity against `internal/event`, `internal/ws`,
  `internal/protocol`, and `docs/protocol-v1.md`
- **Related**: [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md),
  [MADR 0015](./0015-MADR-mcrelay-transport-security.md),
  [MADR 0018](./0018-MADR-mobile-chat-performance-action-plan.md),
  [Report 0032](./0032-MADR-codex-ui-ux-polish-report.md),
  [Report 0033](./0033-MADR-opencode-ui-ux-polish-report.md),
  [MADR 0042](./0042-MADR-android-app-remediation.md),
  [MADR 0044](./0044-MADR-auto-approve-modes.md)
- **Standards applied**: `/home/mac/standards/mobile` v3.12.2-v3
  (2026-07-28) — `networking.md`, `architecture.md`, `dart.md`, `flutter.md`,
  `android.md`

---

## 0. Method and verification discipline

The original audit used six subsystem passes (transport, state/transcript
pipeline, chat UI,
screens/pairing/storage, notifications/Android platform, protocol wiring
parity) produced ~55 candidate findings. Every candidate then went through a
second, adversarial verification pass whose brief was to *refute* the claim by
finding the guard or caller constraint the first pass missed; the nine
high-severity findings were additionally confirmed by direct source reads.
One finding was reproduced by executing the app's own parser (P4).

A final implementation-readiness pass against `ff4b2c1` found several
important corrections to the first record:

- the Android network-security policy deliberately rejects cleartext, and
  `PairPayload` does not recognize the daemon's real `mode=off` value; H3 was
  therefore an inconsistent contract, not merely a dropped scheme;
- a history-wide `_zipStagedImages` call would attach local bytes to the first
  old image in the replay, while current echo descriptors have no unique
  correlation field; P1 therefore clears transient bytes at replay boundaries;
- parsing `timed_out` alone cannot tell an already-open sheet why it was
  dismissed; W1 needs resolution state retained by the reducer;
- audio attachment UI and disabled-command discovery are feature enhancements,
  not correctness failures in the current documented client contract; and
- the WebSocket library rejects an oversize frame before application dispatch,
  so H4 cannot honestly promise a daemon `payload_too_large` reply while the
  1 MiB read limit remains in place.

Those corrections are incorporated below and in the companion plan. Finding
IDs remain stable even where severity changed, so existing review comments can
still refer to them.

Per the Report-0033 precedent, refuted and weakened claims are recorded in §6
rather than silently dropped. **Three claims were refuted outright** (a relay
frame-reorder that is unreachable on a single event loop, a "notification
labels never populated" claim that was a grep artifact, and a supposed need to
latch `agent_session_id` again in the app — see below); several were adjusted,
three of them downgraded from medium to low. Everything in
§§2–3 survived adversarial verification with file:line evidence on both sides
where a claim spans daemon and app.

One refutation is worth stating up front because it changed a fix from cosmetic
to load-bearing: `sessions_screen.dart:403` contains a literal NUL byte inside a
string literal (`'$p\x00$modelProvider'`), which makes `git grep`/`grep` treat
the whole 1,477-line file as binary and silently exclude it. The first-pass
protocol audit briefly concluded `session.create` had no callers because of it,
and the notifications audit's "sessionLabels is never written" finding (N5) was
a false positive for the same reason — the writes are in that file. The NUL is
itself a defect (L-tool below); fixing it is prerequisite to trusting any
text-search over this app.

**Severity**: **High** = user-visible breakage, data loss, security, or a
stuck flow on a mainline path. **Medium** = real defect on an edge path or a
robustness gap that violates a standard. **Low** = polish, performance,
hygiene.

---

## 1. Findings index

| ID | Sev | Area | File | One-liner |
|---|---|---|---|---|
| H1 | High | Transport | `mcremote_client.dart:654` | Host switch resolves/persists the TLS pin under the previous daemon's identity → false impersonation alarm, clobbered pin |
| H2 | High | Pipeline | `transcripts_notifier.dart:703` | Resync gate checks only window extremes → events missed during a disconnect become a permanent transcript hole |
| H3 | Med | Pairing | `pair_uri.dart` / `connect_screen.dart:257` | Daemon `mode=off` QRs are rejected; hand-built `ws://` QRs then lose their scheme; Android policy intentionally forbids cleartext |
| H4 | High | Protocol | `chat_screen.dart:538` | 4 MB image cap vs daemon's 1 MiB WS read limit → sending a normal photo kills the socket and loses the prompt |
| H5 | High | Protocol | `models.dart` (`SessionEvent.fromJson`) | `session_title` events dropped entirely; agent-set titles never reach any UI |
| H6 | High | Notifications | `notification_coordinator.dart:104` | `question_request` passes the notify policy but has no dispatch case → backgrounded user never pinged |
| H7 | High | Notifications | `notification_coordinator.dart:168` | error→reconnecting stop/start race can kill the FGS in background, where API 31+ forbids restart |
| H8 | High | Notifications | `notification_coordinator.dart:64` | Asks that fire during a socket outage are never reconciled into notifications after reconnect |
| H9 | High | Chat UI | `chat_screen.dart:1572` | Question sheet reintroduces the inescapable-modal-loop bug the permission path fixed (commit `00d15b7`) |
| H10 | High | Chat UI | `app.dart:33` | `ChatScreen` built without a per-session key → `/sessions/A → /sessions/B` reuses State; queued prompts can deliver cross-session |
| T1 | Med | Transport | `mcremote_client.dart:723` | `claimPairCode` unguarded by epoch after the await → stale attempt silently disables the newer connection's auto-reconnect |
| T2 | Med | Transport | `mcremote_client.dart:465` | `_relayTransport` assigned before epoch check → interleaved attempts clobber/leak a live RelayTransport |
| T3 | Med | Transport | `mcremote_client.dart:681` | Failed re-pair leaves host B + host A's token in memory → next reconnect sends A's bearer token to B |
| T4 | Med | Transport | `relay_transport.dart:136` | Loopback bridge accepts unlimited peers, replacing the live one → any co-resident app can evict/splice the tunnel |
| P1 | Med | Pipeline | `transcripts_notifier.dart:664` | Sent-image FIFO only consumed on `_applyLive` → deferred/chunked echo leaves stale head; *wrong images on the next bubble* |
| P2 | Med | Pipeline | `chat_bubble.dart:843` | Steady-state streaming parses raw text, skipping `bufferStreamingMarkdown` → unclosed `**`/backticks flash literally |
| P3 | Med | Pipeline | `settings_store.dart:542` | One transient keystore error latches `_secureDisabled` for the process → app presents as unpaired |
| P4 | Med | Pipeline | `markdown_parser.dart:197` | Nested lists flatten during streaming: `- item1\n  - nested` renders as `item1nested` (reproduced by execution) |
| C1 | Med | Chat UI | `chat_screen.dart:1118` | External resolution while the "Allow always?" dialog is open pops the wrong route with the wrong type → TypeError, sheet stuck |
| C2 | Med | Chat UI | `chat_screen.dart:633` | Config-sheet optimistic revert calls `setSheet` after the sheet may be dismissed → setState-after-dispose |
| C3 | Med | Chat UI | `chat_screen.dart:716` | `/model` intercept has no reentrancy guard → double-tap stacks two pickers, can send two `/model` prompts |
| C4 | Med | Chat UI | `chat_screen.dart:780` | Busy-queueing drops staged images (they ride the *next* direct send) and can queue an empty prompt via the IME action |
| S1 | Med | Screens | `app.dart:60` | Notification tap on cold start races auth → target session id discarded, user lands on the list |
| S2 | Med | Screens | `connect_screen.dart:243` | `_applyPair` overwrites/clears the persisted relay route before claim succeeds → failed re-pair destroys the working relay path |
| S3 | Med | Screens | `connect_screen.dart:554` | Connect-screen overflow "Clear saved credentials" wipes pins + client identity with no confirmation (Settings guards the same action) |
| N1 | Med | Notifications | `notification_coordinator.dart:175` | Reconnect parks in error after 6 handshake failures with no timer → background alerts silently dead until the user returns |
| W1 | Med | Protocol | `models.dart` / `chat_screen.dart:1296` | `timed_out` never parsed → timeouts reported as "Request was resolved elsewhere" |
| W2 | Follow-up | Product parity | `chat_screen.dart:527` | ACP-agent audio capability has no mobile attachment UI; this is a new feature, not a hardening defect |
| W3 | Follow-up | Product UX | `chat_screen.dart:346` | Unavailable commands are omitted, which is permitted by the protocol; showing disabled rows is an optional discoverability enhancement |

Low findings are indexed in §5.

---

## 2. High findings — detail and decisions

### H1 — Certificate pin resolved under the previous daemon's identity

`connect()` runs `_setLastHostInput(hostInput); _lastToken = token; await
_resolvePin(...)` (`mcremote_client.dart:654-656`) *before* `_connectInternal`
calls `_noteHost` (line 807). `_noteHost`'s guard — whose own doc comment
describes exactly this hazard — compares `prev` to `next`, but `prev` was
already overwritten, so the stale `deviceId` survives. Warm-process switch
from daemon A to daemon B: `_resolvePin(B)` matches the `id:devA` record,
keeps A's pin, dials B with it → guaranteed `cert_mismatch`, `permanent:
true`, auto-reconnect disabled, and the user is told the host may be
impersonated. With a fresh QR fingerprint, B's pin is *written over* A's
`id:devA` record (`settings_store.dart:330-336`), destroying A's pairing. The
storage fallback (`_idOrNull(deviceId) ?? await getDeviceId()`,
`settings_store.dart:285/321`) reproduces the cross-host lookup on cold
processes because the by-id hit never checks the stored `authority`.

**Decision**: in `connect()`, call `_noteHost(hostInput)` (not bare
`_setLastHostInput`) before `_resolvePin`. In `SettingsStore`, a persisted
device id may select an id-keyed pin before authentication only when the
dialled authority matches the authority associated with the saved host/pin.
If the authority changes, fail closed unless the connection supplies a fresh
QR fingerprint; store that pre-auth fingerprint under `host:<authority>`, then
migrate it to the new daemon's id only after auth returns that id.

This intentionally retracts the current test's unsafe promise that an
unauthenticated host move keeps the old pin automatically: before TLS/auth the
client cannot distinguish "same daemon at a new address" from "different
daemon." A moved self-signed daemon therefore needs a fresh QR (or another
authenticated address-binding protocol). Pin with A→B rejection/preservation,
A-address-change-without-QR rejection, and A-address-change-with-fresh-QR
success tests. `cert_pinning_test.dart:324` currently misses the persisted-id
case, while the "pin survives host moving" test encodes the unsafe behavior and
must be replaced.
Standard: `networking.md` §Server identity ("a pin must never vouch for the
wrong daemon").

### H2 — Reconnect resync can leave a permanent mid-transcript hole

`resyncHistory` gates on `missedNewer = maxSeq > last` and `missedOlder =
minSeq < first` (`transcripts_notifier.dart:700-705`). The notifier applies
live events globally, so after a disconnect the first post-reconnect live
event advances `_lastSeq` past the fetched window's max before the
`session.history` response is processed; both gates then read false and the
rebuild is skipped. Applied 1–10, missed 11–20 during the outage, live 21
lands first → 11–20 (assistant text, tool cards, another device's
user_message) are invisible forever; every later fetch hits the same no-op.
Since the daemon handles slow consumers by dropping the whole connection
(`internal/ws/server.go:1556`), *this is the common outcome of any disconnect
during an active streaming turn*. `history_replay_test.dart:353/373` pin only
the non-racing variants.

**Decision**: detect holes instead of inferring completeness from bounds. In
`_noteSeq`, when `ev.seq > last + 1`, set a per-session `_seqGapSuspected`
flag (and set it for all sessions on a reconnect transition); `resyncHistory`
rebuilds when the flag is set regardless of the bounds test, clearing it on
commit. Add the interior-gap case to `history_replay_test.dart`.
Standard: `architecture.md` (state transitions independently testable — the
gate is the seam).

### H3 — Plaintext pairing behavior contradicts both the daemon and Android policy

This finding has two code defects and one policy constraint:

1. The daemon emits `mode=off` for `tls.mode=off`
   (`internal/pairuri/pairuri.go:30-31`), but the app's `TlsMode` enum contains
   only `selfsigned` and `letsencrypt`. `PairPayload.tryParse` therefore rejects
   a real daemon-generated plaintext QR before `_applyPair` runs.
2. A hand-built legacy QR that carries an explicit `ws://` but omits `mode`
   does parse; `_applyPair` then replaces `payload.host` with schemeless
   `payload.hostAuthority`, and the secure-by-default endpoint parser turns it
   into `wss://`.
3. Even if both bugs are fixed, the Android product target's
   `network_security_config.xml` sets
   `base-config cleartextTrafficPermitted="false"`, and the applicable Android
   standard says TLS is mandatory. `apps/mobile/README.md:95` incorrectly says
   debug builds allow cleartext; there is no debug network-security override.

This is not a high-severity mainline outage: TLS is the default and mandatory
Android transport. It is a medium-severity contract/diagnostic defect because a
valid daemon QR is reported as invalid, while a legacy QR is silently changed
to a different transport.

**Decision**: add `TlsMode.off` so the parser can represent the daemon contract
and preserve `payload.host` (minus its fingerprint fragment) through the screen.
Match the daemon's backward-compatibility rule when `mode` is absent: a present
fingerprint means `selfsigned`; an absent fingerprint means `off`. For that
legacy `off` case, materialize the insecure scheme instead of allowing the
app's modern secure-by-default host parser to reinterpret a bare authority.
Reject contradictory explicit mode/scheme/fingerprint combinations.
On Android, reject `mode=off` / `ws://` before any connection attempt with an
explicit "This Android app requires TLS" message; do not enable cleartext
globally. Linux development builds may retain explicit `ws://` support. Correct
the mobile README and clarify in `protocol-v1.md` that clients with a
TLS-mandatory platform policy may reject `mode=off`, but must never silently
reinterpret it as TLS. Add parser, screen, Android-policy, and TLS-regression
tests.

### H4 — Oversized image attachments kill the socket and lose the prompt

The composer accepts images up to 4 MB raw (`chat_screen.dart:538`) then sends
`base64Encode(bytes)` (~+33 %) as one `session.prompt` frame via a single
`ch.sink.add` (`mcremote_client.dart:1614-1627`). The daemon sets
`conn.SetReadLimit(1 << 20 /* 1 MiB */)` (`internal/ws/server.go:183,424`), and
`coder/websocket` closes with `StatusMessageTooBig` on breach. A 2 MB photo —
well under the app's own stated cap — produces a ~2.7 MB frame, so the daemon
closes the socket, the request times out, the client enters reconnect, and the
prompt plus image are gone. `docs/protocol-v1.md` documents no attachment budget
at all.

**Decision**: enforce the limit twice in the client. `McremoteClient.request`
must serialize once, measure the exact UTF-8 frame bytes, and throw a typed
local `McException(code: 'payload_too_large')` before `sink.add` when the frame
exceeds the daemon's 1 MiB limit. The composer also preflights the complete
candidate `session.prompt` (text plus all attachments) before it clears staged
content, so the user gets an immediate, specific message and nothing is lost.
Document the 1 MiB frame budget in `protocol-v1.md`.

Do **not** add an unreachable daemon error code: `coder/websocket` rejects a
frame over `SetReadLimit` before `handleMessage` can recover its request id or
write a typed response. A nonconforming/older client will still lose that
socket by design; the authoritative fix for this client is the pre-send gate.

### H5 — Agent-generated session titles never reach any UI

The daemon emits `TypeSessionTitle` with a `Title` field
(`internal/event/event.go:360`; goose at `acphttp/session.go:1149`) and
`protocol-v1.md` tells clients to update their session-list label on it. The
app never parses `title` in `SessionEvent.fromJson`, `applySessionEvent` hits
`default: break` (`transcript_reducer.dart:162`), and `_onSessionEvent` reacts
only to `session_status`/`turn_complete` — a repo-wide search for
`session_title` returns zero hits. The daemon does *not* fold the title into
`Meta.Name` either (`internal/session/manager.go:463-494` applies only status,
commands, and mode), so `session.list` never carries it: the event is the sole
channel and the app drops it. This also silently defeats N5's intended
notification labels — a titled session would otherwise read better than
"Session 1a2b3c4d".

**Decision**: parse `title`, handle `session_title` in `_onSessionEvent`
(update `SessionMeta.name` via `copyWith`) and in the chat app-bar, and update
`NotificationCoordinator.sessionLabels` from the event so notifications use
the title even while the sessions screen is absent. Belt and suspenders: have
the daemon also apply the title to `Meta.Name` and persist it, so
`session.list` is self-healing for every client and future one.

### H6 — Questions never raise a notification

`shouldNotify` whitelists `question_request` (`agent_notifications.dart:88`) and
the daemon really emits it (grok's `ask_user_question`), but `_onEvent`'s switch
has only `permission_request` and `turn_complete` cases
(`notification_coordinator.dart:105-118`) — a `question_request` clears the
policy gate and then falls through, doing nothing. The in-app path exists
(`_maybeShowQuestion`), so only the phone-in-pocket user is stranded: the agent
is blocked on a question and nothing pings. There is no `question_resolved`
cancel path either.

**Decision**: add a `question_request` case that posts an open-only
notification (no Allow/Deny — questions need the form UI) keyed by
`ev.questionId`, and cancel it on `question_resolved`, mirroring the permission
path.
Standard: `android.md` §Foreground work ("notification content aligned with
actual user-visible tasks" — the raison d'être of the keep-alive service).

### H7 — Foreground service can die in the background and cannot restart

A failed background reconnect emits `error` then, synchronously,
`reconnecting` (`mcremote_client.dart:838-846`, `_scheduleReconnect` sets
`reconnecting` at line 1088). `_onConn` maps `error → unawaited(_service.stop())`
and `reconnecting → unawaited(_service.start())`. If both `isRunningService`
probes observe "running", `start()` early-returns while `stop()` proceeds — net
result: keep-alive service down while the client is mid-backoff in the
background. On Android 12+ a later `startService` from a cached (background,
no-FGS) process throws `ForegroundServiceStartNotAllowedException`, which
`start()` swallows (`foreground_service.dart:72`), so the service stays dead,
the process becomes killable, the backoff timer dies with it, and permission
asks after the daemon recovers are missed — exactly what the service exists to
prevent.

**Decision**: treat `error` with auto-reconnect still armed as "keep the
service running" — stop only on `disconnected` (manual/logout) or when the
master switch flips off. Additionally serialize start/stop through a single
in-order async queue in `ForegroundServiceController` so a stop can never
overtake a start.
Standard: `android.md` §Foreground work; `networking.md` (deliberate reconnect
lifecycle).

### H8 — Asks that fire during an outage are never recovered as notifications

The coordinator listens only to the live event stream, fed solely by live
`event` envelopes (`mcremote_client.dart:1254`). The daemon's `BroadcastEvent`
reaches only currently-attached clients and nothing re-emits an unresolved
`permission_request`/`question_request` on re-auth; recovery is only via the
`session.history` request/response, whose results go to the transcript reducer,
never through `_onEvent`. A routine 20 s cellular gap while backgrounded, an ask
raised during the gap, socket reconnects → transcript state knows about the ask,
the notification layer never does, and the user waits indefinitely.

**Decision**: history scraping is only a mitigation, not a complete fix: the
800-event ring can trim the request that is still pending. Add an authoritative
daemon RPC, `session.pending_asks`, backed by pending permission/question maps
maintained by `Manager.pump`. It returns unresolved request events for the
authenticated device's live sessions. After every transition to `connected`,
the notification coordinator calls it, posts missing notifications, and
cancels locally tracked notifications absent from the snapshot. The RPC is
idempotent and does not rebroadcast events into transcripts. This closes the
outage window without depending on which screens are mounted or on bounded
history retention. Keep a separate known-pending map even when display is
suppressed because the user is watching that chat; foreground/session-watch
changes re-evaluate it, so backgrounding with an already-open ask still
produces a notification.
Standard: `networking.md` (reconnect policy changes require a tested recovery
path).

### H9 — The question sheet reintroduces the inescapable-modal-loop bug

The permission path deliberately keeps a failed id in `_presentedPermissionIds`
and routes retry through the "Waiting for permission" banner's Review button,
with a comment (`chat_screen.dart:1321-1328`) explaining that removing the id
re-presents the sheet in an "approve, fail, re-present" loop when the failure is
permanent; `permission_loop_test.dart` pins it. The question sheet's catch does
the opposite — `_presentedQuestionIds.remove(questionId)`
(`chat_screen.dart:1572`) — and the id is still in `pendingQuestions` (cleared
only on success), so the post-frame `finally` re-runs `_maybeShowQuestion` and
the sheet reopens instantly and forever. Both Submit and "Cancel / skip" call
the failing RPC, so every visible button loops; only the system back escapes.

**Decision**: mirror the permission path exactly — keep the id in
`_presentedQuestionIds` on failure, rely on the banner's Review button for
deliberate retry, and use `friendlyOpError(e)` for the message. Add the
question-sheet variant to `permission_loop_test.dart`.
Standard: commit `00d15b7` ("fix(chat): resolve inescapable permission modal
loop") — the fix that this path never received.

### H10 — Chat state leaks across sessions on in-app navigation

`ChatScreen` is built without a key (`app.dart:33`). go_router 17.3.0 keys its
pages by matched path *pattern*, not parameters, so a notification tap's
`router.go('/sessions/$id')` from one open chat to another reuses the existing
`ChatScreen` element with a new `widget.sessionId`, and `_ChatScreenState` has
no `didUpdateWidget` (confirmed absent). Consequences: `_notifCoord` stays
claimed for the old session and the new one is never claimed; `_queuedPrompts`
from A flush into `client.prompt(widget.sessionId /* now B */)` — a
**cross-session message delivery**; `_pendingImages`,
`_presentedPermissionIds/_presentedQuestionIds`, `_cwd`/`_provider` (app-bar
label and the opencode-only menu gating), composer draft, and scroll position
all carry over; `_maybeReplayHistory`/`_loadSessionCwd` never run for B.

**Decision**: give the route builder a per-session key —
`ChatScreen(key: ValueKey(id), sessionId: id, …)`. One line, and it forces a
fresh State with correct `initState` wiring. (A `didUpdateWidget` that
release/re-claims and resets every per-session field is the heavier
alternative and easier to get subtly wrong.)
Standard: `architecture.md` §Riverpod and routing; `flutter.md` widget
lifecycle.

---

## 3. Medium findings — evidence and decisions

### Transport

- **T1 — pair stale-attempt disables the newer connection's auto-reconnect**
  (`mcremote_client.dart:723`). `claimPairCode` has no `_staleAttempt(epoch)`
  checks after `request('pair.claim')`, unlike `_connectInternal`
  (guards at 875/905/918/927). Verification narrowed the mechanism: the stale
  attempt can no longer tear down the newer *socket* (its completer is already
  failed while `_channel` is null), but generic pair failure maps to
  `permanent:true` → `_failHandshake` sets `_autoReconnect=false` *after* the
  newer `connect()` set it true, silently disabling auto-reconnect; a late
  `pair_ok` in the pre-cancel window can also write `_lastToken`/`_paired`/
  `connected` over the newer attempt. **Fix**: thread the epoch into
  `claimPairCode` and make its state mutations (and `_failHandshake`) no-ops on
  a stale epoch.

- **T2 — interleaved connects leak/clobber a live RelayTransport**
  (`mcremote_client.dart:465`). `_relayTransport = transport` is assigned inside
  the attempt, before the caller's epoch check, and the stale-cleanup paths
  close only channel + httpClient. Two interleaved relay attempts can leave one
  transport's outer WSS + loopback `ServerSocket` orphaned for the process
  lifetime, or close the wrong (live) one. The catch path's
  `_closeRelayTransport()` can also close a newer attempt's transport.
  **Fix**: return an attempt-owned socket bundle
  `(channel, httpClient, relayTransport?)`; never read or write the shared
  `_relayTransport` from `_openSocket*`; let the epoch owner adopt it only after
  `_staleAttempt(epoch)` passes, and close the bundle it owns on every stale or
  error path.
  Standard: `networking.md` (close sockets/subscriptions on every path).

- **T3 — failed re-pair sends the old host's token to the new host**
  (`mcremote_client.dart:681`). `_noteHost(hostInput)` runs before the handshake
  and never clears `_lastToken`; a non-`invalid_token` pair failure leaves
  `_lastHostInput=B` + `_lastToken=A` with `_paired` still true, so
  `reconnectFromStore` → `reconnect()` auths host B with host A's bearer token (a
  cross-host credential disclosure) before failing and wiping A's session in
  memory. **Fix**: when pairing changes authority, clear `_lastToken` and
  `_paired`, plus the persisted token, before opening B and keep them empty
  until B issues a token; leave the stored A host/relay tuple untouched until
  success. A same-authority re-pair may retain the old token only through an
  explicit, tested fallback path; a B failure or process restart can never
  leave A credentials associated with B.

- **T4 — loopback relay bridge is hijackable by any co-resident app**
  (`relay_transport.dart:136`). The bridge binds `127.0.0.1:0` and
  `_replacePeer`s on *every* accept with no peer limit and no same-process
  check; on Android any local process that dials the port evicts the live
  WebSocket leg (repeatable = connection DoS) and splices a raw byte pipe onto
  the tunnel. Inner TLS + client-cert still protect authentication, but the
  bridge itself is evictable. **Fix**: accept exactly one connection per
  transport (`server.close()` after the first accept), or reject new sockets
  while a peer is healthy.
  Standard: `networking.md` (relay bridging changes need rejection tests).

### Pipeline

- **P1 — staged images mis-attach to the wrong bubble after a resync**
  (`transcripts_notifier.dart:664`). `_zipStagedImages` runs only on
  `_applyLive`; a `user_message` echo drained via `_drainDeferred`/`_applyChunked`
  (during a resync rebuild) leaves the `_sentImages` FIFO head unconsumed, so the
  *next* live echo pops the previous prompt's images onto the new bubble. Resync
  rebuilds also downgrade already-zipped thumbnails to placeholders. A naive
  call to `_zipStagedImages` for every history event is worse: it attaches the
  FIFO head to the first *old* image message in the ring. The echo exposes only
  kind and MIME type—no origin device, request id, payload digest, or other
  unique correlation—so matching text/descriptors would still be heuristic.
  **Fix**: clear that session's transient staged-image FIFO whenever a
  history/hydration generation starts, before any event can be deferred or
  rebuilt. Do not zip bytes in deferred/history paths. This may replace a
  just-sent local thumbnail with the correct descriptor-only placeholder during
  a reconnect, but it cannot display the wrong image. Lossless restoration
  requires a future protocol `client_prompt_id`.

- **P2 — raw markdown markers flash during steady-state streaming**
  (`chat_bubble.dart:843`). `_updateStreamingRender` parses raw text without
  `bufferStreamingMarkdown`, and `_render` prefers that cached parse, so unclosed
  `**`/backticks render literally on every frame after the first parse — only the
  pre-first-parse fallback buffers, contradicting the module's own "raw syntax
  markers never flash" contract. Bounded to replies < 4000 chars (the markdown
  branch cap). **Fix**: parse `bufferStreamingMarkdown(text)` while keeping
  `_parsedText` keyed on the raw text for the staleness compare.

- **P3 — one transient keystore error latches the app "unpaired" for the
  session** (`settings_store.dart:542`). `_disableSecure` sets a process-lifetime
  `_secureDisabled` from a blanket catch in every secret read/write; on Android
  (no plaintext fallback) all subsequent reads return null, so the app presents
  as unpaired after a single surfaced Keystore hiccup. The pinned
  `flutter_secure_storage` 10.3.1 Android implementation already applies its
  required `resetOnError:true` delete-and-retry behavior; the app must not
  second-guess or disable that standard-mandated recovery. Failing closed is
  correct; latching a surfaced failure forever is the gap. **Fix**: replace the
  permanent latch with a bounded cooldown and retry, catch `Exception` rather
  than `Error`, and keep `resetOnError:true`.
  Standard: `dart.md` (catch the narrowest handleable exception).

- **P4 — nested lists garble while streaming** (`markdown_parser.dart:197`).
  Reproduced by executing `parseMarkdownOffMain('- item1\n  - nested')`: the
  inner `ul` hits `_extractSpans`'s default case, which splices child text
  inline, and `_mergeAdjacent` fuses the same-styled spans into `"item1nested"`.
  The finalized `MarkdownBody` path nests correctly, so the text visibly reflows
  at turn end. Nested lists are routine in agent replies. **Fix**: recurse into
  nested `ul`/`ol` as indented blocks (carry a depth into `ParsedBlock.level`)
  and insert a break span when descending into a block-level child. Update the
  streaming block renderer too: it currently uses `level` for headings, not
  list indentation.

### Chat UI

- **C1 — external resolution during "Allow always?" pops the wrong route**
  (`chat_screen.dart:1118`). `_dismissSheet` does
  `Navigator.pop(ctx, '__external__')`, which pops the navigator's *top* route;
  if the `_confirmAlways` `DialogRoute<bool>` is open when `ref.listen` fires
  external dismissal, popping a `Route<bool>` with a `String` throws a TypeError,
  the pop aborts, and both dialog and sheet stay up. **Fix**: capture the
  sheet's route and whether the confirmation dialog is open; on external
  resolution, dismiss the dialog with `false` and then remove/pop the exact
  sheet route. Removing only the sheet would leave the confirmation dialog
  orphaned.

- **C2 — config-sheet revert is a setState-after-dispose**
  (`chat_screen.dart:633`). The sheet is drag-dismissible; swiping it away while
  `setConfigOption` is in flight makes the failure path's
  `setSheet(() => opts[i] = prev)` run on a disposed `StatefulBuilder`. The
  `sheetCtx.mounted` guard wraps only the notification, not the revert. **Fix**:
  guard the revert with the same `sheetCtx.mounted` check.

- **C3 — `/model` intercept has no reentrancy guard** (`chat_screen.dart:716`).
  `_send` awaits `_maybeInterceptModelCommand` before clearing the composer or
  setting `_sending`; during the `listModels` await the send button stays
  enabled, so a second tap stacks two model pickers and can issue two `/model`
  prompts (two agent restarts). **Fix**: use a dedicated `_interceptingModel`
  flag around catalog/picker work. Do not reuse `_sending`: the chosen
  `/model <id>` re-enters `_send`, and `_sending=true` would incorrectly queue
  it as a busy prompt.

- **C4 — busy-queueing drops staged images and can queue an empty prompt**
  (`chat_screen.dart:780`). The "attach disabled while busy" invariant only
  holds for same-device turns: images staged while idle survive an externally
  started turn into the busy branch, which queues text only and lets the
  thumbnails ride the *next* direct send; and empty text + staged images passes
  the send guard so the IME action queues a `_QueuedPrompt('')`. **Fix**: in the
  busy branch, move text, attachment descriptors, and local bytes into one
  queued-prompt object. Treat attachment-only input as valid; reject only when
  both text and attachments are empty.

### Screens & notifications

- **S1 — notification tap on cold start loses the target session**
  (`app.dart:60`). The replayed `router.go('/sessions/$id')` races the TLS+auth
  handshake; while unpaired the redirect bounces it to `/`, and ConnectScreen's
  success path then does `context.go('/sessions')` with no id, so the tapped
  session's chat never opens. **Fix**: stash the intended location when the
  redirect blocks an unpaired `/sessions/...` nav and have ConnectScreen's
  success paths honor the stash; clear it on sign-out. (Composes with H10's key
  fix.)

- **S2 — failed re-pair destroys the working relay route**
  (`connect_screen.dart:243`). `_applyPair` writes `setRelayRoute` +
  `store.setRelayUrl/HostId` *unconditionally before* claim/connect succeeds, and
  `setRelayUrl(null)` removes the stored key; host/token are written only on
  success. A stale QR or one without `relay=` therefore wipes the still-valid
  pairing's relay path, and the next off-mesh reconnect dials direct-only.
  **Fix**: pass relay hints as attempt-local connection arguments, then adopt
  and persist them only with the winning authenticated host/token. A failed or
  superseded attempt must neither mutate nor “restore” global relay state.

- **S3 — one-tap credential wipe with no confirmation**
  (`connect_screen.dart:554`). The connect-screen overflow "Clear saved
  credentials" runs `clearAll()` (token + pins + client identity) with no dialog,
  while the identical Settings action is guarded by an `AlertDialog`. A mis-tap
  discards the pin and enrolled client key, forcing physical host access for a
  new QR. **Fix**: reuse the Settings confirmation dialog on the connect screen.

- **N1 — background alerts die after a handshake-failure park**
  (`notification_coordinator.dart:175`). After 6 handshake failures the client
  `_setState(error)` and `return`s with no timer; backgrounded, `_onConn` stops
  the FGS and only a resume or a connectivity *interface change* revives it —
  neither of which a daemon-side wedge (cert renewal/restart) produces. Roughly a
  minute of daemon downtime permanently kills background notifications with no
  "alerts paused" signal. **Fix**: keep a slow retry while backgrounded with
  alerts on. Let the notification coordinator own the maintenance timer
  because it also owns foreground state and service policy; cancel on
  foreground, success, manual disconnect, credential clear, or alerts-off,
  and use bounded backoff rather than the fast 30-second loop. A notice alone
  does not close the missed-alert gap.

### Protocol parity and product follow-ups

- **W1 — timeouts are mislabeled "resolved elsewhere"**
  (`models.dart` / `chat_screen.dart:1296`). The daemon sets `timed_out:true` on
  timeout auto-cancel (`acphttp/session.go:1235`, `codex/session.go:1389`) and
  the doc says to distinguish it, but the app never parses it and shows the
  generic "Request was resolved elsewhere" for an expiry the user caused by not
  answering. Parsing alone is insufficient because the sheet receives the
  original request event, not the later resolution event. **Fix**: parse
  `timed_out`, retain permission-resolution metadata in transcript state long
  enough for the sheet dismissal to read it, branch the copy ("Request timed
  out — the agent moved on"), and append one transcript system line. The daemon
  defines `timed_out` only for permissions, not questions.

- **W2 — audio attachment UI is a product-parity follow-up, not a defect.**
  `Capabilities.Audio` and `PromptAttachment{kind:audio}` exist, but only the
  ACP subprocess path (`acpagent`) currently forwards audio blocks; `acphttp`
  and codex currently handle images only. The app already uses its microphone
  affordance for speech-to-text and has no recording/file-picker dependency.
  Adding audio requires an explicit product choice, dependency/security review,
  MIME/duration UX, and provider-parity work. Track it outside this hardening
  plan.

- **W3 — disabled command rows are an optional discoverability enhancement.**
  `chat_screen.dart:346` filters unavailable canonical commands. The protocol
  says clients should offer available entries and may use `reason` *if they
  surface the rest* (`protocol-v1.md:1140-1142`); current behavior conforms.
  A future command-browser UX may render disabled rows with the reason, but it
  is not remediation.

---

## 4. Cross-cutting themes

Four patterns recur across otherwise unrelated findings and are worth fixing as
classes, not just instances:

1. **Reconnect completeness is under-modeled.** H2, H8, P1, and S2 all fail
   because a disconnect/reconnect crosses a boundary that assumes steady state:
   the seq gate assumes the fetched window bounds the truth (H2), the notifier
   assumes asks arrive live (H8), the image FIFO assumes a live echo (P1), the
   relay writes assume pairing succeeds (S2). A single "on reconnect, reconcile
   from authority" discipline covers most of them.

2. **Epoch/generation guards are applied unevenly.** `_connectInternal` is
   rigorously epoch-checked; `claimPairCode` (T1), the relay-transport
   assignment (T2), and the missed-ping teardown (L-t2) are not. The
   pattern exists — extend it to every path that mutates shared connection
   state after an await.

3. **Per-session widget state without a per-session identity.** H10 and S1 are
   the same root cause: `ChatScreen` is reused across session ids. Keying the
   route fixes the class.

4. **Blanket `catch` erasing type.** P3, and the low-severity `sessionHistory`
   and `_failAllPending` items, all swallow or flatten error types the standard
   says to preserve (`dart.md` §Error Handling). None should catch `Error`.

The **NUL-byte-in-source** hazard (§0) sits underneath all of this: a source
file that text tools treat as binary undermines audits, code review, and
grep-based refactors. It is `L-tool` below and should be fixed first.

---

## 5. Low findings

Verified, low-severity. Grouped by area; each is a one-line fix.

| ID | File | Finding | Fix |
|---|---|---|---|
| L-tool | `sessions_screen.dart:403` | Literal NUL in a string literal makes the file read as binary to grep/git-grep (caused a false positive this audit) | Make `catalogFor` generic, key `modelCatalogs` by the record `(provider, modelProvider)`, and remove delimiter encoding entirely |
| L-t1 | `mcremote_client.dart:405` | `_ensureIdentity()` caches a failed Future; residual exotic-transient exposure after P3 is fixed | Null the field on error before rethrow |
| L-t2 | `mcremote_client.dart:985` | Missed-ping teardown lacks the state/epoch guard `probeLiveness` has → transient state flap during a manual reconnect | Capture epoch at ping-send, bail unless still `connected` and current |
| L-t3 | `mcremote_client.dart:442,492` | `channel.ready` timeout closes only the HttpClient; an upgrade completing just after leaks the socket | `unawaited(channel.sink.close().catchError((_){}))` in both catches |
| L-t4 | `mcremote_client.dart:1389` | `sessionHistory`'s `catch (_)` swallows `Error` and conflates fetch-failure with empty history | Let typed request errors propagate; callers already catch failures separately from an authoritative empty result |
| L-t5 | `mcremote_client.dart:1233` | `_failAllPending` completes with untyped `Exception` → screens can't classify connection loss | `McException(reason, code:'connection_lost')` |
| L-t6 | `mcremote_client.dart:510` | Relay hints are global, not host-keyed → switching to an unreachable host reuses the prior host's relay, yielding a misleading permanent error | Store normalized authority with adopted hints and refuse a route whose authority differs |
| L-t7 | `relay_transport.dart:165` | `List<int>.from(msg)` copies every binary frame (WS already delivers a fresh `Uint8List`) | Pass through when `msg is Uint8List` |
| L-p1 | `transcripts_notifier.dart:738` | `syncFromMeta` eviction misses cached sessions absent from in-memory state → deleted session's transcript persists until LRU | Add `TranscriptCache.retainOnly(liveIds)` sweeping the index |
| L-p2 | `transcripts_notifier.dart:408` | Replay-event guard tested against evolving `t` → old daemons that broadcast replays leave a torn one-item transcript | Snapshot `t.items.isNotEmpty` once per batch |
| L-p3 | `transcripts_notifier.dart:207` | `unawaited(_cache.save(...))` drops errors the cache deliberately raises; `jsonEncode` of ≤150 items runs on the UI isolate every 400 ms | Route saves through a catch+`debugPrint` helper and encode the plain payload map with `compute` |
| L-p4 | `app_providers.dart:37` | `ThemeModeController._load` unconditionally overwrites state and is unguarded | Track whether `set` ran; after guarded load, assign only if the user has not changed it |
| L-p5 | `transcripts_notifier.dart:769` | `sessionTranscriptProvider` is non-autoDispose → one retained element per session id ever viewed | `Provider.autoDispose.family` |
| L-c1 | `chat_bubble.dart:784,793` | Streaming `RichText` has no `textScaler` → streamed text renders at 1.0 and jumps on finalize under system font scaling | `textScaler: MediaQuery.textScalerOf(context)` |
| L-c2 | `chat_screen.dart:2461` | Dangerous-mode selector uses `ref` after `await` with no mounted check; the bare catch turns it into a silently dropped mode change (no crash) | `if (!context.mounted) return;` before `setMode` |
| L-c3 | `chat_screen.dart:2038` | Jump-to-latest FAB during an active fling is a dropped no-op | Give `_scrollToEnd` a `force` param that skips the `_listScrolling` guard |
| L-c4 | `scroll_activity.dart:43` | Sensor reacts to nested scrollables (code-block scroller) → pauses streaming + auto-follow | `if (n.depth != 0) return false;` |
| L-c5 | `top_notification.dart:120` | No tap/swipe dismiss; action-bearing errors share the default 3 s window | Add `Dismissible`; extend duration when `actionLabel != null` |
| L-s1 | `sessions_screen.dart:1221` | Full-screen spinner replaces the visible list on every reconnect refresh | Show the spinner only when `_sessions.isEmpty` |
| L-s2 | `sessions_screen.dart:1042` | End-session error restores a pre-await snapshot, stomping status updates that arrived during the delete | Run authoritative `_refresh()` on failure; never restore the snapshot |
| L-s3 | `settings_screen.dart:41` | `_load`'s two prefs reads (lines 41-42) are unguarded → a throwing read is an uncaught async error | Wrap in try/catch like `ConnectScreen._load` |
| L-s4 | `sessions_screen.dart:1081` + | Raw `e.toString()` surfaced in several banners/toasts while siblings use `friendlyOpError` | Route through `friendlyOpError`; keep raw detail in `debugPrint` |
| L-s5 | `sessions_screen.dart:240` | Rename Save with an empty trimmed name is a silent no-op with no way to clear a name | Disable Save while the trimmed value is empty |
| L-s6 | `settings_store.dart:530` | `clearAll` leaves global last/recent-cwd and per-provider preferred-model keys → a re-paired phone offers the old host's paths/models | Remove those pairing-context keys in `clearAll` |
| L-n1 | `notification_coordinator.dart:62` | Coordinator subscribes to streams *after* `await _notifs.init()` → a narrow warm-start window drops transitions | Subscribe first, then initialize notifications |
| L-n2 | `notification_coordinator.dart:94` | Stale Allow/Deny notification not swept after reconnect (folds into H8's reconciliation) | Cancel notifications whose id is no longer pending on reconnect |
| L-n3 | `foreground_service.dart:63` | Service notification says "Connected to host" even while `reconnecting` | `updateService` with state-appropriate text, `onlyAlertOnce` |
| L-n4 | `foreground_service.dart:47` | Wakelock + wifi-lock held all night; 30 s-capped retry loop drains battery on an unreachable host | Disable both locks, verify screen-off reconnect, and use N1's slow maintenance retry after the fast budget |
| L-n5 | `agent_notifications.dart:95` | Permission notification ids omit `sessionId`, and use `String.hashCode`, which is not a cross-process persistence contract; collisions/restarts can update or fail to cancel the intended request | Stable-hash a versioned `kind + sessionId + requestId` target key and use the same helper for show/cancel |
| L-w1 | `chat_screen.dart:2386` | Usage chip hidden entirely when `size <= 0`, but codex legally emits `size:0` (render the count alone) | Render count without a percentage when `size <= 0 && used > 0` |
| L-w2 | `docs/protocol-v1.md` | `session_config` documented as "full replacement" but both daemon and app merge by id; removal is impossible | Fix the current contract to specify merge-by-id semantics |
| L-w4 | `docs/protocol-v1.md` | tool_kind vocabulary omits `switch_mode` (the app maps it deliberately, so no runtime bug) | Add `switch_mode` to the doc's enum list |
| L-w5 | `apps/mobile/README.md:104` | README says transcripts are memory-only and awaits a history/cache API, but both daemon history replay and the bounded phone cache exist | Document daemon ring authority plus the best-effort last-N phone cache |

---

## 6. Refuted and adjusted claims

Kept per the Report-0033 discipline: a claim that does not survive verification
is as useful to record as one that does.

**Refuted:**

- **Relay frame reorder on peer replacement** (`relay_transport.dart:184`). The
  reorder window needs `_outerBuf` non-empty while a previous peer exists, which
  is unreachable: buffering happens only while `_peer == null`; the first accept
  runs synchronously with no await between publish and flush, and replacement
  accepts necessarily have an empty buffer (frames were taking the fast path to
  the prior peer). No bug.
- **"sessionLabels is never written" (N5).** False positive from the NUL-byte
  grep artifact — `_refresh` repopulates the map from the session list and
  `_renameSession` updates it (`sessions_screen.dart:153-160, 250-251`). The
  notification body *does* get a real label. (The deeper reason labels can still
  read poorly is H5, not this.)
- **`agent_session_id` must be latched by the app (former L-w3).** The daemon
  deliberately latches this event field into persisted `SessionMeta`, and every
  mobile workflow that needs it (`session.list` and resume) reads `SessionMeta`.
  The unused `SessionEvent.agentSessionId` field is harmless protocol parity,
  not a stale-state bug.

**Adjusted (severity or mechanism corrected):**

- **`_ensureIdentity` cached failure** — downgraded medium → low: after P3 the
  only residual throw is an exotic platform-channel failure, since the mobile
  secure path already fails closed identically on retry. (L-t1.)
- **Missed-ping teardown** — downgraded medium → low: the pending ping is failed
  only inside the very teardown that already nulled the socket, so it cannot
  reach a freshly reconnected one; the residue is a transient state flap.
  (L-t2.)
- **Dangerous-mode selector mounted check** — downgraded medium → low: the bare
  `catch` absorbs the disposed-`ref` `StateError`, so it degrades to a silently
  dropped mode change, not a crash. (L-c2.)
- **Capability-flag uptake (protocol)** — the sessions screen genuinely cannot
  gate on `list_sessions` because capabilities are per-session and no session
  exists yet at that call site; the daemon returns a clean `unsupported` error
  the app already surfaces. W2 (audio) is a real, buildable product gap, but
  not a hardening defect or a closure criterion here.
- **`switch_mode` handling** — a documentation omission, not an app bug: the app
  maps it explicitly to `ToolClass.other`. (L-w4.)
- **Settings `_load` unguarded reads (S7)** — real but narrower than reported:
  `osBlocked()` and the model reads are already guarded, so only the two prefs
  reads at lines 41-42 are exposed. (L-s3.)
- **Plaintext pairing (H3)** — downgraded high → medium and mechanism expanded:
  real daemon QRs fail first on missing `TlsMode.off`; Android intentionally
  forbids cleartext; legacy scheme loss is still real and must never silently
  change transport.
- **Audio and unavailable commands (W2/W3)** — reclassified as product
  follow-ups. Audio is not end-to-end across all providers and needs new mobile
  UX/dependencies; hiding unavailable commands conforms to the current
  protocol's optional wording.

Also confirmed clean by the audits (no finding): the Android manifest and Gradle
setup fully conform to `android.md` (remoteMessaging FGS type, both FGS
permissions, `allowBackup=false`, data-extraction rules, predictive back,
cleartext disabled, Kotlin DSL, AGP 9.0.1 / Kotlin 2.3.20 / Java 17,
`desugar_jdk_libs:2.1.4`, centralized plugin versions); `AndroidOptions(resetOnError)`
is present as required; the SharedPreferences fallback is correctly Android-gated
and fails closed; the deep-link intent filter was deliberately removed; MADR 0044
mode/dangerous-confirm parity is complete; and reconnect backoff math, pending-
request correlation across reconnects, and controller/subscription disposal are
all sound.

---

## 7. Remediation sequencing

Proposed order. Each item is independently shippable with its own regression
test; nothing here is a rewrite.

**Phase A — data-loss and security (the nine highs + H3/T3/T4/S1).**
H4 (exact outbound-size gate) and H3 (honest plaintext policy) are the smallest
and stop active data loss / misleading onboarding. H2 (seq-gap detection) and
H8 (authoritative pending-ask snapshot) share the "reconcile on reconnect"
theme — do them together.
H1 (pin identity) and T3/T4 (token/bridge) are the security cluster. H6/H7/H9/H10
are self-contained correctness fixes. H5 needs a small daemon change too.

**Phase B — remaining mediums.** P1–P4, C1–C4, S1–S3, N1, and W1. Group the
two streaming-markdown items (P2, P4) and the two reconnect-state items
(P1, S2). W2/W3 are not closure criteria.

**Phase C — lows.** Land **L-tool first** (it unblocks reliable grep for
everything after), then the `dart.md` error-typing cluster (L-t4, L-t5, P3's
classification), then polish.

Every fix above cites a standard or a module contract; none conflicts with an
existing decision record. The `session_config` and `tool_kind` items (L-w2,
L-w4) are documentation corrections to `protocol-v1.md`, not code.

---

## 8. Appendix: verification method

- **Six subsystem audit passes**, one per subsystem, each briefed to verify
  candidates against surrounding guards and existing tests before reporting.
- **Adversarial second pass**: every medium/low was challenged from source; the
  high findings were confirmed by direct reads. The implementation-readiness
  pass added the H3/P1/W1 corrections and reclassified W2/W3/L-w3.
- **Executed, not just read**: P4 was reproduced by running
  `parseMarkdownOffMain` against nested-list input and observing the fused
  `"item1nested"` span.
- **Both sides cited**: every protocol finding (H4, H5, W1–W3, L-w1, L-w2,
  L-w4, and L-w5)
  carries the daemon emit site and the app consume site.
- **Standards**: `/home/mac/standards/mobile` v3.12.2-v3 (2026-07-28), applied
  per the development-standards workflow — `networking.md`, `architecture.md`,
  `dart.md`, `flutter.md`, `android.md`.

---

## 9. References

- [MADR 0014 — SSE reconnect resync](./0014-MADR-sse-reconnect-resync-decision.md) — the resync gates H2/H8 build on
- [MADR 0015 — mcrelay transport security](./0015-MADR-mcrelay-transport-security.md) — the relay bridge T4 concerns
- [MADR 0018 — mobile chat performance](./0018-MADR-mobile-chat-performance-action-plan.md) — streaming-render budget (P2)
- [Report 0032 — Codex UI/UX](./0032-MADR-codex-ui-ux-polish-report.md) / [Report 0033 — OpenCode UI/UX](./0033-MADR-opencode-ui-ux-polish-report.md) — sister report-style audits; verification discipline followed here
- [MADR 0044 — auto-approve modes](./0044-MADR-auto-approve-modes.md) — the dangerous-mode UI whose parity §6 confirms complete
- `docs/protocol-v1.md` and `apps/mobile/README.md` — the contracts
  H4/H5/W1–W3 and L-w1/L-w2/L-w4/L-w5 measure against
