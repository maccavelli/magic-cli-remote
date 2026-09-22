---
status: in-progress
date: 2026-09-22
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0166 — Assert the containment we have, not the transport we cannot keep

Implements [0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md](0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md)
decisions D1–D7, closing findings F4, F6–F12.

## Goal

Observable states, not activities:

1. No test asserts that the ACP connection survives a stalled pump, because no
   client-side code can deliver that at `acp-go-sdk` v0.13.5 (**D1**, **F12**).
2. A test asserts **containment** under the same storm — the session is faulted or
   cleanly disconnected, never hung, zombied or panicking — and it passes 20
   consecutive runs **under `GOMAXPROCS=1`**, the configuration that reproduces the
   failure (**D2**, **D7**).
3. A test still asserts that `deliver` absorbs `cap(events) + controlOverflowCap`
   events without blocking, derived from the constants (**D3**).
4. `git diff` shows **no behaviour change** in `internal/provider/acpagent`
   outside test files — comments only (**D4**).
5. The three comments that imply the parked overflow protects the transport say what
   is true instead (**D5**).
6. MADR 0138 carries an additive amendment recording that its F5 mitigation is
   weaker than claimed, pointing here (**D6**).
7. `ci-flakes.tsv` gains no further row naming `TestACPConnectionSurvivesAStalledPump`.

## Scope

### In scope (the only files any phase may touch)

```text
P1  internal/provider/acpagent/stalledpump_test.go   retire the transport assertion; assert containment
P2  internal/provider/acpagent/session.go            comments only: :110-125, :1407-1413
    docs/spec/0138-MADR-overhaul-provider-surfaces-and-turn-path.md   additive amendment to F5
P3  docs/decisions/0166-PLAN-*.md                    execution record only
```

### Out of scope

Named so the boundary is not mistaken for an oversight: any change to `deliver`,
`drainOverflow`, `controlOverflowCap` or the event buffer (**D4** — the mechanism is
correct, only the claim was wrong); forking or vendoring `acp-go-sdk`; upstreaming a
patch; `TestStalledPumpFaultsTheSessionRatherThanDroppingTheEvent`, which asserts a
choice this record does not revisit; and MADR 0165, which is closed.

## Stability rule

Every phase ends with, in order:

```bash
make pre-add-check FILES="<the phase's Go files>"
go test ./internal/provider/acpagent/ -count=1
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -count=20
go test -race ./internal/provider/acpagent/ -count=1
make ci-windows
```

The `GOMAXPROCS=1` run is not optional decoration: at default parallelism this
failure appears roughly never, which is exactly how it survived in the ledger from
2026-09-19. P2 touches comments and a document, so it needs only `go vet`,
`markdownlint-cli2` and `make ci-windows`.

Commit discipline: one phase, one commit, message from the hook
(`git commit --no-edit`). `git push` and tags need an explicit instruction in the
same turn.

## Cross-cutting contracts

* **C1 — No production behaviour changes.** `session.go` edits are comments only.
  Verify mechanically, not by eye: the phase's diff of non-test Go files must contain
  no line that is not a comment.
* **C2 — The new test must be seen to fail.** A containment test that cannot fail is
  worse than the red test it replaces, because it looks like coverage. P1 does not
  land until a deliberate break makes it fail. **This is the contract most at risk**:
  the assertions are about absence of hanging, and "it did not hang" is the default
  outcome of almost any mistake.
* **C3 — The retired assertion is retired explicitly.** The test says, in prose, that
  the transport is no longer asserted and why, citing 0166 F12. A silently deleted
  assertion reads as an oversight to the next person.
* **C4 — 0138's rationale is not rewritten.** Additive amendment only; its original
  F5 text stays as it was.
* **C5 — No new flakiness.** The replacement passes 20 consecutive runs under
  `GOMAXPROCS=1` *and* at default parallelism.

## Dependency and delivery order

P1 then P2; they share no file and could be reversed, but P1 is the substantive
change and should land first so the comments in P2 describe a tree that already
matches them. P3 records both.

## Implementation Steps

### P1 — Assert containment (D1, D2, D3, D7; closes F4, F12)

1. Rename `TestACPConnectionSurvivesAStalledPump` to
   `TestAStalledPumpIsContainedNotHung`. The old name is the retired claim; leaving
   it would keep asserting it in the one place everyone reads first.
2. Wire the containment path in `stalledSession`: it currently never starts
   `go s.watchConnClose(conn)`, so a dead transport produced no teardown in the test
   at all. Without this the containment being asserted is not the containment
   production has (**F11**).
3. Replace the writer's `writeErr` channel with an `atomic.Int64` counter and a
   deadline. The writer is **allowed** to end up blocked: with an unbuffered
   `io.Pipe` and a reader that has stopped, that is the expected consequence of a
   dead transport (**F5** of 0166). Register a `t.Cleanup` that closes the pipe so
   the goroutine cannot outlive the test.
   * Do **not** close the pipe when the session faults. Closing it gives the SDK EOF
     and *causes* a teardown, which is how the first attempt at this turned the
     assertion green for the wrong reason (0166 F5, measured).
4. Assert, with the storm run under whatever parallelism the suite has:
   * **absorb**: frames accepted ≥ `cap(s.events) + controlOverflowCap`, derived from
     the constants rather than written as a number (**D3**).
   * **containment**: within a bounded wait, either `s.done` is closed (the stall
     detector ended the session) or — after draining `s.events` — a terminal error
     and a `disconnected` status are present (`signalDisconnected` ran). At least one
     must hold; both are correct outcomes.
   * **not hung**: the bounded wait completes; the test never relies on the writer
     finishing.
5. State in the test, in prose, that `conn.Done()` is deliberately **not** asserted,
   with the measurement: a handler that does nothing loses the transport 3 times in 5
   under `GOMAXPROCS=1`, so no client-side change can keep it (**C3**).
6. Keep `TestStalledPumpFaultsTheSessionRatherThanDroppingTheEvent` untouched.

**Verification (P1).**

```bash
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -run StalledPump -count=20 -v
go test ./internal/provider/acpagent/ -count=1
go test -race ./internal/provider/acpagent/ -count=1
```

**Fail-first, required by C2.** Two breaks, each must fail the new test:

* make `deliver` block instead of parking (park into a full unbuffered channel) —
  the **absorb** assertion must fail;
* stop `markClosedAndKill` firing at the cap **and** leave `watchConnClose`
  unwired — the **containment** assertion must fail.

Record both outcomes. If either break leaves the test green, the assertion is
decoration and must be sharpened before the phase lands.

### P2 — Correct the claims that made the wrong assertion look right (D5, D6; closes F12's paper trail)

1. `session.go:110-125` and `:1407-1413`: these describe the parked overflow as the
   thing that stops a stalled consumer taking the connection down. Reword to what is
   true — it bounds memory and ends a stalled session deliberately, and the SDK's
   1024-deep queue can still overflow upstream of it under CPU starvation, which no
   client-side change prevents at v0.13.5. Cite 0166 F6/F9/F10.
2. `stalledpump_test.go:16-24`'s `sdkNotificationQueueDepth` comment: keep the
   mechanism, drop the implication that our guard prevents the teardown.
3. Amend `docs/spec/0138-MADR-overhaul-provider-surfaces-and-turn-path.md` with an
   `## Amendment — 2026-09-21` section: F5's hazard analysis was right, its
   mitigation is weaker than claimed, here is the measurement, see MADR 0166. Do not
   edit F5's original text (**C4**).

**Verification (P2).**

```bash
go vet ./internal/provider/acpagent/
# C1, mechanically: no non-comment change to production Go
git diff --unified=0 -- internal/provider/acpagent/session.go | grep -E '^[+-][^+-]' | grep -v -E '^[+-]\s*(//|$)'
npx markdownlint-cli2 "docs/spec/0138-MADR-*.md"
```

The `grep` must print nothing. If it prints a line, a behaviour change crept into a
comment-only phase.

### P3 — Record what execution taught (no decisions)

Append an execution record: which phases ran, both fail-first results from P1, the
`GOMAXPROCS=1` pass count, and anything this plan predicted incorrectly.

## Verification (whole plan)

```bash
make pre-add-check
go test ./... -count=1
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -count=20
go test -race ./internal/provider/acpagent/ -count=1
make ci-windows
# WSL lane
go build ./... && go test ./internal/provider/acpagent/ -count=1
# and after the next push, the ledger:
git show origin/master:ci-flakes.tsv | grep StalledPump
```

### Acceptance criteria (mapped to the MADR's Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | No test asserts `conn.Done()` stays open under a stalled pump | D1, F12 |
| A2 | The containment test passes 20 consecutive runs under `GOMAXPROCS=1` | D2, D7, C5 |
| A3 | The containment test also passes at default parallelism | C5 |
| A4 | Blocking `deliver` fails the absorb assertion | D3, C2 |
| A5 | Unwiring the stall detector and `watchConnClose` fails the containment assertion | D2, C2 |
| A6 | The absorb bound is derived from `cap(events)` and `controlOverflowCap`, not written as a literal | D3 |
| A7 | The diff of non-test Go files contains only comment lines | D4, C1 |
| A8 | The test states in prose why the transport is no longer asserted | C3 |
| A9 | MADR 0138 carries an additive amendment; its F5 text is unchanged | D6, C4 |
| A10 | `stalledSession` starts `watchConnClose`, so the asserted containment is production's | F11 |
| A11 | No new ledger row names this test | — |

**A5 is the criterion most likely to be quietly dropped.** It requires breaking two
things at once to prove the containment assertion bites, and the obvious single
break — unwiring only the stall detector — still leaves `watchConnClose` producing a
clean disconnect, so the test would pass and the criterion would look satisfied.
**A7 is second**: it is the only mechanical guard that a comment-only phase stayed
comment-only, and it is tempting to eyeball instead of run.

## Rollout and Rollback

* **One release, patch.** Test and comment changes only; no wire shape, no
  behaviour, no mobile change. Nothing to roll back operationally.
* Rollback is per commit, and reverting P1 restores a test that fails under load —
  which is the state this plan exists to leave behind, so a revert should be paired
  with re-opening 0166.
* `git push` and tags need an explicit instruction in the same turn.

## Deferred (named, so they are not mistaken for oversights)

* **Upstreaming a patch to `acp-go-sdk`** — option 3, and the only permanent fix.
  Either make the notification queue depth configurable or let the reader apply
  backpressure instead of closing the connection (`connection.go:19`, `:108`,
  `:432`, `:446-447`). Deferred because it depends on a third party's schedule and
  cannot gate this repository's CI. **When it lands, this plan's test becomes an
  under-assertion and A1 should be revisited** — that is the trigger to come back.
* **Forking or vendoring the SDK** — option 2. Removes the failure now at the cost
  of maintaining a fork on the engine transport path. Held in reserve if the
  teardown proves disruptive in practice rather than only in a starved test.
* **Reducing the blast radius to one session** — the transport is per engine, so
  every session on it dies together (**F3**). Isolating sessions onto separate
  connections would contain it, and is a much larger architectural change than this
  record's subject.
* **The other ledger entries.** `ci-flakes.tsv` is the place to look for the next
  one; the method that worked twice is in MADR 0163 and 0165 — read the full CI log,
  never the `--- FAIL` line alone, and reproduce before diagnosing.
