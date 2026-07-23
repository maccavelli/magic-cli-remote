# MADR 0014: SSE reconnect resync (H4) — engine-state reconciliation for the HTTP transport

- **Status**: Accepted
- **Date**: 2026-07-23
- **Deciders**: Project Owner
- **Resolves**: H4 from the 2026-07-22 audit ([MADR 0012](0012-mcremote-daemon-assessment-action-plan.md)), deferred in [MADR 0013](0013-audit-remediation-decisions.md)

## Context

The httpagent transport reads every session's events from one engine SSE
stream (`pumpEvents` → `streamOnce`), reconnecting with backoff after a drop.
The engine does **not** replay: every frame emitted while the stream was down
is gone. If the gap swallows a turn-end frame (`session.idle`, or the final
text/error of a turn that finished during the gap), the local `turnActive`
flag stays set forever:

- the client shows **"running" indefinitely**;
- new prompts are refused (`prompt already in progress`);
- **Stop cannot recover** — aborting an already-idle engine session emits no
  `session.error`, so nothing ever clears the turn;
- only `/reset` (close-and-replace) unsticks the session.

The stall watchdog made it worse in a way: it nagged "still waiting — the
agent may be working on something long" about a turn that was already over.

## Decision

Add a **resync protocol**: the transport asks the dialect to reconcile a
turn-active session against authoritative engine state (REST) whenever SSE
frames may have been missed. New `DialectSession.Resync(ctx, turnStartedAt)`.

### Triggers (transport, `httpagent`)

1. **Every successful SSE (re)connect** (`streamOnce`, once the stream is
   confirmed 200): all registered sessions of the current generation are
   resynced (async, per-session goroutines). The first connect of a
   generation is a free no-op — no session can hold a turn before the engine
   is up.
2. **The stall watchdog**, before emitting a notice: a missed turn-end and a
   genuinely long-running turn look identical from the daemon. If resync ends
   the turn, the watcher exits silently instead of nagging about a ghost
   turn; if the turn is genuinely live, the notice still fires. This trigger
   also covers gap causes that involve no reconnect at all (e.g. a dropped
   oversized SSE line that happened to carry `session.idle`).

### Gates (why resync can't do harm)

- **Turn-active only.** An idle session has no in-flight state; resync is
  skipped (cheap for the reconnect-triggered sweep).
- **Never while the prompt submit is in flight.** Between claiming the turn
  and `prompt_async` returning, the engine does not know the turn exists —
  its message log would read as a finished *previous* turn and falsely end
  the new one. `promptInFlight` excludes the window.
- **Stale-evidence guard.** `turnStartedAt` is recorded when the turn is
  claimed; engine evidence of a turn finished *before* it is ignored (the
  fetch can still race a just-accepted prompt). Engine and daemon share the
  host clock (the engine is a local child process), so direct comparison is
  sound.
- **Finished turns only.** If the engine reports the turn still streaming,
  resync does nothing: in-stream `part.updated` snapshots already heal text
  gaps for live turns, and acting on a moving turn risks duplicate chunks.

### OpenCode dialect implementation

`GET /session/{id}/message`; look at the **last** message only (earlier
messages were fully streamed — or belong to a resumed history, which must
never be re-emitted as live chunks):

| Engine state | Action |
|---|---|
| last message is the user's | Reply not started; do nothing |
| assistant, `time.completed == 0`, no error | Turn live; do nothing |
| assistant, finished before `turnStartedAt` | Stale; do nothing |
| assistant, completed | Heal text (below), `EndTurn`, `turn_complete(end_turn)` + `idle` |
| assistant, `error == MessageAbortedError` | Heal text, `EndTurn`, `turn_complete(cancelled)` + `idle` (the lost frame was a cancel) |
| assistant, other `error` | Heal text, `EndTurn`, classified `error` event + status `error` |

**Text healing** reuses the existing snapshot catch-up (M8): the message log
carries the full final text per part; the accumulated-text comparison emits
only the missing tail, so text the stream already delivered is never
duplicated, and the tail lands *before* the turn-end events — the same order
the stream would have produced. The catch-up now holds the dialect mutex
across the (non-blocking) chunk emit so a concurrent pump snapshot and a
resync cannot interleave their tails out of order.

`EndTurn` remains the single idempotent arbiter: if the live stream delivers
the real turn-end mid-resync, whichever side wins emits the completion events
exactly once.

## Considered alternatives

- **SSE `Last-Event-ID` replay** — not supported by the engine; would need an
  upstream OpenCode feature.
- **Periodic polling of engine state** — resync on reconnect + stall covers
  the same failures with no steady-state cost; a poller would be a second
  source of truth running against a healthy stream for no benefit.
- **Ending the turn on any reconnect** — destroys genuinely long-running
  turns; reconciliation against real engine state is barely more code.

## Scope limits (accepted)

- **Tool-call updates lost in the gap are not replayed** (a tool that started
  and finished entirely inside the gap shows no card). Recovering them
  reliably would need replaying tool parts with dedupe against `seenTools`;
  the payoff is cosmetic next to the stuck-turn fix. Revisit only if it
  bothers in practice.
- **Missed `permission.asked` frames are not re-fetched** (no known engine
  endpoint to enumerate pending permissions). The existing permission-expiry
  fail-safe already bounds the damage: the request is rejected server-side
  after `PermissionTimeout` and the user is told to re-prompt.
- **Residual duplicate-text window**: if the turn completes in the sub-second
  gap between the new stream connecting and the resync fetch returning, a
  late-delivered delta could double-append after the catch-up. Self-limited
  to that window; the next snapshot re-aligns.
- ACP transport (`acpagent`) is unaffected — stdio has no reconnect-with-loss
  mode; connection death is already handled by the `conn.Done()` watcher
  (MADR 0013 D1).

## Verification

- Transport: reconnect triggers resync of turn-active sessions
  (fail-on-buggy verified: fails with the `streamOnce` hook removed); gating
  on idle/closed/prompt-in-flight; stall watchdog ends ghost turns silently
  (fail-on-buggy verified: fails with the watchdog hook removed) and still
  notices live turns.
- Dialect: recovered turn-end (text tail + `turn_complete` + `idle`, in
  order), live-turn no-op, stale-evidence no-op, pending-user no-op, aborted
  → cancelled, errored → classified error, no duplication of already-streamed
  text.
- Full tree green under `go test ./... -race`.
