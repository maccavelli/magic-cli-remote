---
status: accepted
date: 2026-11-08
decision-makers: Project Owner
consulted: "the CI log of run 42442989254, job Go (linux/arm64); internal/ws/server.go, internal/session/manager.go and commands.go; the phone client (apps/mobile/lib/data/ws/mcremote_client.dart, apps/mobile/lib/state/session_synchronizer.dart); scripts/smoke-protocol; scratch-clone experiments flake_widen.py and sweep_auth_order.py"
informed: anyone writing a client of the WebSocket protocol, or a test in internal/ws
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Tolerate server pushes that arrive before the auth reply

## Context and Problem Statement

`TestResumeReturnsOwnedSessionBounds` (`internal/ws/resume_test.go:123`) failed once in CI, on the
v0.25.0 tag's run `42442989254`, job Go (linux/arm64). The retry passed, and
[0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md)
recorded the row in `ci-flakes.tsv`. It is the test's first row in the ledger, and the first row for
`internal/ws`.

The failure:

```text
--- FAIL: TestResumeReturnsOwnedSessionBounds (0.00s)
    resume_test.go:153: want auth_ok got event {"event":{"type":"remote_commands","session_id":"…","seq":3,…}}
```

The question this record answers: is the bug in the server or in the test, and what should change?

### Findings [observed 2026-11-08]

* **F1 — The server can queue a push ahead of the auth reply.** `handleAuth`
  (`internal/ws/server.go:1165`) calls `authenticate`, which calls `setAuthed`
  (`server.go:1436`, `1487-1496`). From then on the connection is a broadcast target, because
  `BroadcastEvent` filters on `c.authed` (`server.go:359`). `handleAuth` then validates the resume
  token, builds the resume bounds (`resumedFor`), issues a fresh token, starts v2 liveness, and only
  then queues `auth_ok` (`server.go:1235-1236`). An event broadcast in between goes into the
  connection's queue first. The same gap exists on the two other paths that authenticate a socket:
  * the Bearer-on-upgrade path (`server.go:661-677`), whose `auth_ok` carries no `id`;
  * `pair.claim` (`setAuthed` at `server.go:1357`, `pair_ok` queued at `server.go:1385-1386`).

  Four fan-outs gate on `c.authed` and can reach the connection in that gap: `BroadcastEvent`
  (`server.go:359`), `sendToDevice` (`server.go:1777`), `broadcastPrewarm` (`server.go:2296`), and
  the provider auth status push (`server.go:2846`).
* **F2 — What the test hit.** The test creates a session on connection 1, reads `session.created`,
  and closes. The session's event pump is still running. It re-advertises the slash commands when
  the provider reports more about the session (`internal/session/manager.go:1761-1765`,
  `commands.go:130-166`), which is the seq 3 `remote_commands` event. Connection 2 then authenticates
  with a resume token. `authV2` (`resume_test.go:63-81`) reads exactly one frame and requires it to be
  `auth_ok`. When the pump's emit lands in F1's gap, that frame is the event.
* **F3 — Reproduced on demand.** All runs were in a scratch clone of `master`, which was removed
  afterwards:

  | Run | Result |
  | :--- | :--- |
  | the test unchanged, `-race -count=3000` on the Mac | 0 failures: here the pump always finishes before `session.created` reaches the client |
  | a 20 ms delay before `auth_ok` is queued, `-count=200` | 0 failures: the delay also slows connection 1's auth, so the pump finishes before connection 2 dials |
  | a 20 ms delay inside `handleAuth`'s resume branch only, `-count=200` | 0 failures, for the same reason as the unchanged run |
  | a 30 ms delay inside the resume branch, and a 10 ms delay before the pump's re-advertise, `-count=50` | **50 of 50 failed**, each with `resume_test.go:153: want auth_ok got event {"event":{"type":"remote_commands"…` |
  | the same two delays, with `authV2` skipping `event` frames until the reply, `-count=50` | 50 of 50 passed; the bounds assertion at `resume_test.go:164` still held |

* **F4 — The real clients already tolerate it.**
  * The phone routes `event` and other pushed frames on their own path, and completes a request
    only when a frame's `id` matches it (`mcremote_client.dart:2946-2993`). An event that arrives
    before `auth_ok` is applied, not lost.
  * The phone's resume fast path compares each session's cached seq with the bounds in `auth_ok`
    (`session_synchronizer.dart:80-92`). If an early event moved the cached seq past those bounds,
    the comparison fails and the ordinary resync runs. That costs one extra round of work, and loses
    nothing.
  * `scripts/smoke-protocol` reads in the background and matches replies by `id`
    (`scripts/smoke-protocol/main.go:120-135`).
* **F5 — No security consequence.** A push reaches a socket only after `store.Validate` and
  `verifyClientKey` have passed (`server.go:1429-1436`). The ownership filter in `BroadcastEvent` is
  unchanged. Nothing reaches an unauthenticated peer.
* **F6 — The ordering is undocumented.** Neither `docs/guides/protocol-v1.md` nor
  `docs/guides/protocol-v2.md` says whether `auth_ok` / `pair_ok` is the first frame after the
  request. A client written from the guides could reasonably assume it is.
* **F7 — No other test makes the same assumption.** `sweep_auth_order.py` listed every test in
  `internal/ws` that creates a session and afterwards reads an `auth_ok` or `pair_ok`, directly or
  through one of the eight helpers that read one (`authAs`, `authConn`, `authV2`, `pairDevice`,
  `pairDeviceSigning`, `setupWSSessionWithProvider`, `startAuthServer`, `startPrewarmServer`). It
  found one: this test. The sweep is a text heuristic; its limits are stated in the PLAN.

## Decision Drivers

* The test must stop failing on timing, without hiding a real failure.
* No change to delivery semantics that could lose an event. The phone's resume fast path trusts
  `auth_ok`'s bounds plus every event it receives afterwards.
* The protocol guides must describe what the daemon does, so the next client does not repeat the
  test's assumption.
* Smallest change that is correct. The daemon's fan-out is concurrency-sensitive code on the hot
  path.

## Considered Options

* Correlate the reply in the test, and document that pushes may come first
* Make the daemon hold pushes until the auth reply is queued
* Make the daemon drop pushes until the auth reply is queued
* Leave it to the CI retry

## Decision Outcome

Chosen option: "Correlate the reply in the test, and document that pushes may come first", because
the daemon's behaviour is safe (F4, F5) and every real client already handles it (F4). The defect is
a test assuming an ordering the daemon never promised (F2), and guides that do not say so (F6).

* **D1 — `authV2` reads until the reply.** It uses the package's existing `readUntil`
  (`internal/ws/prewarm_test.go:263`), which skips `event` and `providers.prewarm` frames and fails
  on any other unexpected type, so an `auth_error` still fails the test with its payload.
* **D2 — The guide states the order.** `docs/guides/protocol-v1.md`, section Authentication, gains a
  paragraph: a server push can arrive before the `auth_ok` or `pair_ok` that answers the request.
  Clients match the reply by `id`, and treat a push that comes first as they would one that comes
  after. The Bearer-on-upgrade `auth_ok` / `auth_error` has no `id`, so it is matched by `type`.
  `docs/guides/protocol-v2.md` is not changed: its `auth_ok` additions sit on top of v1's section.
* **D3 — The daemon is not changed.** The gap in F1 stays. If a future client needs `auth_ok` to be
  the first frame, the option "hold pushes" below is the way to provide it, and needs its own
  record.

### Consequences

* Good, because the flake's cause is removed rather than retried, and F3's experiment shows the new
  helper passing under the exact interleaving that fails the old one.
* Good, because the guide now tells client authors what the phone and `smoke-protocol` already do.
* Good, because no daemon code changes, so delivery and resume semantics are untouched.
* Neutral, because the daemon still sends pushes before the auth reply. That is now the documented
  contract rather than an accident.
* Bad, because no in-tree test pins F1's interleaving. Forcing it needs delays in daemon code
  (F3), and adding a test seam to the auth path for one test is more code on the hot path than the
  fix itself. The experiment is recorded instead, and repeated on the change before it is staged.

### Confirmation

1. `internal/ws/resume_test.go`'s `authV2` reads the reply with `readUntil(…, protocol.TypeAuthOK)`.
2. In a scratch clone carrying the change and F3's two injected delays, the test passes 50 of 50.
   With the old `authV2` and the same delays it fails 50 of 50, with F2's message.
3. `go test -race -count=1 ./internal/ws` and `make race` pass. `make pre-add-check` is clean for
   the changed Go file.
4. `docs/guides/protocol-v1.md` carries D2's paragraph in its Authentication section.
5. `make check-records` passes.
6. CI passes on the push.

## Pros and Cons of the Options

### Correlate the reply in the test, and document that pushes may come first

* Good, because it is a one-line change to a test helper plus one paragraph of documentation.
* Good, because it matches how the phone and `smoke-protocol` already read replies (F4).
* Good, because it changes no delivery semantics.
* Bad, because a client that wants "first frame is the reply" cannot have it.

### Make the daemon hold pushes until the auth reply is queued

Each connection keeps a short buffer. From `setAuthed` until the reply is queued, fan-out frames for
that connection go into the buffer instead of its queue. After the reply is queued, the buffer is
flushed in order under `s.mu`.

* Good, because `auth_ok` / `pair_ok` would always be the first frame, a stronger contract.
* Good, because no push is lost: held frames are delivered, in order, after the reply.
* Bad, because it touches the four fan-outs (`server.go:359`, `1777`, `2296`, `2846`) and the three
  auth paths, all concurrency-sensitive. The buffer needs a bound and an overflow rule that matches
  the queue's slow-client drop.
* Bad, because no current client needs the stronger contract (F4).

### Make the daemon drop pushes until the auth reply is queued

* Good, because it is a small change: fan-outs filter on a "reply sent" flag instead of `c.authed`.
* Bad, because it loses events. An event emitted after `resumedFor` takes its bounds, but before the
  flag is set, is neither in the bounds nor delivered live. The phone's resume fast path
  (`session_synchronizer.dart:80-92`) would then see its cached seq equal to the bounds and skip the
  history fetch, so the event is missing until a later gap is noticed.

### Leave it to the CI retry

* Good, because it costs nothing now.
* Bad, because the retry hides the flake rather than fixing it, and a run where both attempts lose
  the race fails CI.
* Bad, because the guides would still not say what order to expect (F6).

## More Information

* The resume fast path this test covers: [0068-MADR-protocol-v2-reconnect-resilient-transport.md](0068-MADR-protocol-v2-reconnect-resilient-transport.md), D4.
* The retry and the ledger that surfaced it: [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md).
* Implementation: [0238-PLAN-tolerate-pushes-before-the-auth-reply.md](0238-PLAN-tolerate-pushes-before-the-auth-reply.md).
* Revisit if a client that cannot correlate by `id` is planned, or if the ledger records this class
  of failure in another test.
