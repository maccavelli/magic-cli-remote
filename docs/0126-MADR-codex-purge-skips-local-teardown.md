---
status: proposed
date: 2026-08-31
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Purge must tear down locally and detach from the caller's deadline

## Context and Problem Statement

Ending a session hangs. Reported from live use: sessions "are either hanging and
not being ended cleanly, or they are not ending at all and they timeout,
finally returning to the session screen if you terminate via the menu inside
the chat session."

Three providers implement the purge path. Two follow the same shape. Codex
follows none of it, and every reported symptom falls out of the difference.

### What was measured, not assumed

**`session.delete` calls `Purge` *instead of* `Close`, never both.**
`session/manager.go:2395-2404`:

```go
if ps, ok := e.sess.(provider.PurgeSession); ok {
    closeErr = ps.Purge(ctx)
} else {
    closeErr = e.sess.Close(ctx)
}
```

So a provider implementing `PurgeSession` owns its own local teardown. Two of
the three do exactly that; the third does not.

| | local teardown first | detached from caller ctx | own budget |
| --- | --- | --- | --- |
| `httpagent.Purge` (`:1260`) | `_ = s.Close(ctx)` | `context.WithoutCancel` | 15s |
| `acphttp.Purge` (`:731`) | `_ = s.Close(ctx)` | `context.WithoutCancel` | 15s |
| **`codex.Purge` (`:921`)** | **none** | **none** | **none** |

Codex's, in full:

```go
func (s *session) Purge(ctx context.Context) error {
	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	_, err := fr.sendRequest(ctx, "thread/delete", map[string]any{
		"threadId": s.agentID,
	})
	return err
}
```

**`sendRequest` imposes no deadline of its own.** `codex/conn.go:166-175` waits
on exactly three things: the reply, `ctx.Done()`, or the connection dropping.
With the caller's context passed straight through, the bound is whatever the
caller allowed.

**The caller allows 60 seconds.** `ws/server.go:995-1003` gives
`session.delete` a 60s budget, and the phone waits 70s
(`opTimeoutFor`, 60s + `kOpTimeoutMargin`). So an engine that does not answer
`thread/delete` produces a one-minute hang, then a client timeout — and the
phone's own confirm-against-`session.list` fallback is what eventually returns
the user to the sessions screen. That is the reported sequence, in order.

**Codex's `Close` is where all the local teardown lives, and delete never runs
it.** `codex/session.go:884-918` sets `s.closed`, drains chunks, cancels
pending permissions and questions, sends `thread/unsubscribe`, removes the
session from `p.sessions`, and closes `s.done`. None of that happens on
`session.delete`.

**Four goroutines are gated on `s.done`** (`grep 'case <-s.done'`), plus a
blocking `<-s.done` at `session.go:1844`. A purged codex session never closes
that channel, so those goroutines outlive the session the manager has already
dropped from `m.sessions`.

**The error is logged, not returned** (`manager.go:2406-2412`), so once the
call does return, a failed purge still reports success to the phone as long as
the store delete succeeds. The hang is visible; the failure is not.

### Findings

**F1 — Codex's `Purge` performs no local teardown.** `Close` holds every
release step and `session.delete` never calls it, because the manager treats
`Purge` as the whole operation. The other two providers compensate inside
their own `Purge`; codex does not.

**F2 — Codex's `Purge` inherits the caller's 60s deadline.** Both peers
deliberately detach with `context.WithoutCancel` and impose 15s. Codex passes
`ctx` straight to `sendRequest`, which has no timeout of its own, so one
unanswered engine call consumes the whole RPC budget.

**F3 — The reported symptoms are F1 and F2, in order.** A busy or unresponsive
codex engine leaves `thread/delete` unanswered → 60s daemon hang → 70s phone
timeout → the phone confirms the purge against `session.list` and finally
navigates. Nothing else needs to be wrong to produce exactly what was
described.

**F4 — Purged codex sessions leak goroutines.** `s.done` is never closed, so
four `case <-s.done` selects and one blocking receive never release. The
manager has already removed the entry, so nothing will ever close it.

**F5 — Pending permission and question asks are never cancelled on delete.**
`cancelPendingPermissions` / `cancelPendingQuestions` live only in `Close`.
MADR 0046 M-4 made the *phone* drop its asks after a delete; the daemon side
was relying on `Close` running, and on this path it does not.

**F6 — A failed purge is invisible.** `closeErr` is logged and discarded; the
RPC reports success if the store delete succeeded. The user sees a hang, then
an apparent success.

**F7 — The interface documents an obligation it does not enforce.**
`provider.PurgeSession` (`provider.go:309-315`) says purge "owns durable
provider-side state" and that soft close must not call it — but nothing states
that `Purge` must also do everything `Close` does, and nothing checks. Two
implementations inferred it; one did not.

## Decision Drivers

* Ending a session is a foreground, user-initiated action. It must complete
  promptly or report why, never hang for a minute.
* A provider-side call must never be able to consume a whole RPC budget.
* One obligation, stated once, checkable. Three implementations silently
  disagreeing is the actual defect here.
* A daemon that has dropped a session from its map must not still be running
  that session's goroutines.

## Considered Options

* **A — Fix codex's `Purge` to match its peers.**
* **B — Make the manager call `Close` then `Purge`, so no implementation can
  forget.**
* **C — Wrap the provider call in the manager with its own bounded context.**
* **D — Raise the RPC budget, or lower the phone's timeout.**

## Decision Outcome

**Chosen: B and C together, with A as the concrete change they make
unnecessary to repeat.**

A alone fixes the reported bug and leaves the trap. The next `PurgeSession`
implementation has the same two chances to get it wrong, and F7 says the
obligation is nowhere stated.

B removes the first chance: if the manager always closes locally before
purging, no implementation can skip teardown, and the two peers' internal
`_ = s.Close(ctx)` becomes belt-and-braces rather than load-bearing. `Close` is
already idempotent in all three (`if s.closed { return nil }`), so calling it
twice is safe — that is checked, not assumed.

C removes the second: a bounded, detached context applied by the *caller*
means no provider can consume the RPC budget by omission. The peers' own 15s
stays; C is a ceiling, not a replacement.

D is rejected outright. The budget is not the problem — a minute of hanging is
already far past what a delete should take, and raising it makes the symptom
worse while lowering the phone's timeout only hides the daemon's own error
frame, which is precisely the failure MADR 0095 F9 corrected.

### The decisions

**D1 — The manager closes locally, then purges.** `closeMatching` calls
`e.sess.Close(ctx)` for every session, and additionally `ps.Purge(...)` when
the session implements `PurgeSession`. Ordering is close-then-purge: local
release must not depend on an engine call that may never answer.

**D2 — The manager bounds and detaches the purge call.** The provider-side
purge runs under `context.WithoutCancel` with its own ceiling, so a
non-answering engine cannot consume the `session.delete` budget. The ceiling is
the plan's to choose; it must be well under the 60s RPC allowance so the daemon
answers the phone rather than racing it.

**D3 — Codex's `Purge` drops its local-teardown gap and its ctx passthrough.**
With D1 and D2 in place codex's `Purge` becomes what its peers' already are:
an engine call. It keeps its own detached bound so it is correct when called
directly, not only via the manager.

**D4 — Purge failure is reported, not just logged.** F6. A purge that failed
must reach the caller. The session is gone locally either way — that is not in
question — but "the engine still holds this thread" is information the user
needs, and today it is written to a log nobody reads.

**D5 — State the obligation on the interface (F7).** `PurgeSession` documents
that the manager performs local close and that `Purge` is the engine-side
remnant only. An obligation two implementers inferred and one missed should not
stay implicit.

**D6 — Do not change either timeout (option D rejected).** The 60s daemon
budget and the phone's 60s + 10s margin stay exactly as they are. They are a
deliberate ladder (MADR 0095 F9) and they are not what is broken.

### Consequences

* Good: ending a codex session stops hanging, because the only unbounded call
  on the path is now bounded and no longer required for local release.
* Good: the goroutine and pending-ask leaks (F4, F5) close, because `Close`
  runs on every delete.
* Good: the next `PurgeSession` implementation cannot repeat this — the manager
  owns teardown and the bound.
* Neutral: `httpagent` and `acphttp` keep their internal `Close` and 15s
  budgets. Both become redundant rather than wrong, and removing them is
  deliberately not part of this record.
* Bad: a purge that legitimately needs longer than the new ceiling will now be
  cut short. Accepted: the engine-side delete is a remnant cleanup, and leaving
  a thread behind is a smaller harm than a minute of hanging UI.

### Confirmation

```bash
go build ./... && go vet ./...
go test ./internal/session/ ./internal/provider/...
```

```text
delete a codex session, engine responsive    -> returns promptly, session gone
delete a codex session, engine not answering -> returns within the ceiling, reports
                                                the engine-side failure, session gone locally
after either                                 -> no goroutine still selecting on s.done
pending permission asks at delete            -> cancelled, not orphaned
```

The two device rows are owner-run; nothing here claims them from a Windows
host.

## Pros and Cons of the Options

### A — Fix codex's Purge only

* Good: smallest change; fixes the report.
* Bad: leaves the trap for the next implementation, and F7 says the obligation
  is written down nowhere.

### B — Manager closes, then purges (chosen)

* Good: no implementation can skip local teardown.
* Good: `Close` is already idempotent in all three, so the peers keep working
  unchanged.
* Bad: two of three providers now close twice. Harmless, and cheaper than the
  class of bug it removes.

### C — Manager bounds and detaches the purge (chosen)

* Good: no provider can consume the RPC budget by omission.
* Good: the daemon answers the phone with its own error rather than being
  pre-empted by a client timeout — the thing MADR 0095 F9 fixed.
* Bad: a legitimately slow engine delete is cut short.

### D — Change the timeouts

* Good: nothing.
* Bad: a longer budget lengthens the hang; a shorter phone timeout re-creates
  the exact race 0095 F9 removed.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Purge replaces Close, never both | `internal/session/manager.go:2395-2404` |
| httpagent purges after a local close, detached, 15s | `internal/provider/httpagent/session.go:1260-1265` |
| acphttp does the same | `internal/provider/acphttp/session.go:731-751` |
| codex does neither | `internal/provider/codex/session.go:921-932` |
| `sendRequest` has no timeout of its own | `internal/provider/codex/conn.go:166-175` |
| `session.delete` budget is 60s | `internal/ws/server.go:995-1003` |
| Phone waits 60s + 10s margin | `apps/mobile/lib/data/ws/mcremote_client.dart:409-437` |
| Codex teardown lives only in Close | `internal/provider/codex/session.go:884-918` |
| Goroutines gated on `s.done` | `codex/session.go:276, 1844, 2320, 2386, 2401` |
| Purge error is logged, not returned | `internal/session/manager.go:2406-2412` |
| Interface states no teardown obligation | `internal/provider/provider.go:309-315` |
| Phone falls back to confirming via session.list | `chat_screen.dart:1684-1717` |

### Related records

* [0095](0095-MADR-post-0094-assessment-and-debug-pass.md) — introduced `PurgeSession`
  and the timeout ladder; F9 is why D6 refuses to touch either timeout.
* [0046](0046-MADR-mobile-debug-pass.md) — M-4 made the phone drop
  pending asks after a delete; F5 is the daemon half that was assumed.
* [0094](0094-MADR-end-session-return-black-screen.md) — D7's confirm-against-the-list
  fallback, which is what currently rescues the user after the timeout.

### Open questions for the plan

1. What ceiling for D2? It must be comfortably under 60s so the daemon reports
   rather than races, and at least the 15s the two peers already budget.
2. Should `httpagent` and `acphttp` drop their now-redundant internal
   `_ = s.Close(ctx)`? Keeping them is harmless and makes each `Purge` correct
   when called directly; removing them concentrates the obligation in one
   place. Not obvious either way.
3. Does any *other* provider-side call on a user-facing path pass the caller's
   context straight to `sendRequest`? The same shape would produce the same
   hang elsewhere; this record found it in purge, and did not sweep.
4. Is `thread/delete` even answerable while a codex turn is running? If the
   engine defers it until the turn ends, the ceiling is doing the real work and
   the sequence should probably cancel first — which the phone already does,
   but the daemon does not.
