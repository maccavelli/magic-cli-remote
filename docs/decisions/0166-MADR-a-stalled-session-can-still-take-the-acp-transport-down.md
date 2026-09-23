---
status: accepted
date: 2026-09-21
decision-makers: Project Owner
consulted: none
informed: none
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# One stalled session can still take the ACP transport down

## Context and Problem Statement

MADR 0138 F5 established the hazard: the ACP SDK queues inbound notifications in a
channel of 1024 whose overflow does **not** drop the event — it closes the whole
connection (`errNotificationQueueOverflow` → `shutdownReceive`,
`acp-go-sdk@v0.13.5 connection.go:108`, `:446`). So a client handler that cannot
keep up does not merely stall its own session; it takes the engine's transport, and
every other session on it, down.

The mitigation was a parked overflow: `deliver` never blocks, parking what it
cannot hand over and faulting the session past a cap
(`internal/provider/acpagent/session.go:1414-1455`). That is sound as far as it
goes, and `TestACPConnectionSurvivesAStalledPump` was written to prove it.

**It does not go far enough.** That test appears in `ci-flakes.tsv` as a
fail-then-pass on `windows/amd64` (run `35461886119`, 2026-09-19), and tracing it
under MADR 0165 found the failure is real rather than a test artefact.

### What was measured, not assumed

**The flake reproduces on demand** with `GOMAXPROCS=1`, which is the cheapest
available model of a CPU-starved CI runner. Instrumented, three consecutive runs:

| run | session faulted | `conn.Done()` closed | writer |
| --- | --- | --- | --- |
| 1 | yes | no | finished all 1224 frames — passes |
| 2 | yes | no | finished — passes |
| 3 | yes | **yes** | still blocked at 15s — the CI failure |

Run 3 is the one that matters: `conn.Done()` was closed, so the transport was torn
down. MADR 0138 F5's property failed.

**The numbers that bound the problem**, all from source:

| quantity | value | source |
| --- | --- | --- |
| SDK inbound notification queue | 1024 | `acp-go-sdk` `defaultMaxQueuedNotifications`; quoted at `stalledpump_test.go:16-24` |
| our per-session event buffer | 256 | `acpagent.go:454` |
| `controlOverflowCap` | 512 | `session.go` |
| absorbed before the session faults (production) | 768 | 256 + 512 |
| absorbed before the session faults (the test fixture) | 513 | `cap(events)`=1 + 512 |

**Two mechanisms were ruled out by reading the code, not by assuming it.**
`deliver` returns in O(1) on every path — the channel takes the event, the overflow
parks it, or the session is faulted (`session.go:1414-1455`). And `drainOverflow`
releases `overflowMu` **before** blocking on its send and selects on `s.done`
(`:1488`, `:1490-1501`), so it cannot wedge a `deliver` caller. Neither is the cause.

### Findings

**F1 — The guard is downstream of the queue that overflows.** `controlOverflowCap`
can only act when `deliver` is called, which happens on the SDK's *consumer*
goroutine. Nothing it does slows the SDK's *reader*. When the reader outpaces the
consumer — which is what CPU starvation produces — the 1024-deep queue between them
fills and the SDK closes the connection before our cap is ever consulted.

**F2 — The failure is intermittent by nature, so CI's retry hides it.** 1 run in 3
under `GOMAXPROCS=1`; ~~once~~ **twice** in the ledger over the observed period on
real CI — runs `34139293426` (2026-09-07) and `35461886119` (2026-09-19), both
`windows/amd64`, twelve days apart. *(Corrected 2026-09-22; the second occurrence
was found when checking A11. See PLAN 0166's addendum.)* A retry that passes records
it as a flake, which is why it survived until it was traced.

**F3 — The blast radius is larger than one session.** The connection is per engine,
not per session. A single stalled consumer therefore ends every session on that
engine, which is precisely what 0138 F5 called out and what the parked overflow was
built to prevent.

**F4 — The test is currently correct and should stay failing.** It asserts the
connection survives. Under load it does not. Bounding the writer or relaxing the
assertion would remove the flake and the detection together — considered and
rejected under MADR 0165, whose P4 was withdrawn for this reason.

**F5 — `io.Pipe` is the transport in the test, so it cannot be closed to tidy up.**
The obvious way to stop the writer hanging is to close the pipe when the session
faults. That gives the SDK EOF and *causes* the teardown the test forbids — it was
tried, and it turned the assertion green for the wrong reason. **[measured]**

## Decision Drivers

* A retried flake that is really a transport failure is the worst of both worlds:
  it looks like noise and it hides a defect with a multi-session blast radius.
* The queue that overflows belongs to the SDK. Any fix either changes how fast we
  drain it, how deep it is, or how the SDK behaves when it fills — and only the
  first is entirely ours.
* 0138's parked overflow is good and should not be reverted; the question is what
  to add.
* Whatever is chosen must be verifiable under `GOMAXPROCS=1`, since that is what
  reproduces it.

## Considered Options

* **A — Drain the SDK's queue into our own buffer immediately**, so the consumer
  goroutine never lags the reader, and apply the existing cap to our buffer.
* **B — Raise or configure the SDK's queue depth**, if `acp-go-sdk` exposes it.
* **C — Backpressure the reader** rather than buffering, so the agent is slowed
  instead of the transport being dropped.
* **D — Accept and contain**: treat transport teardown as survivable by
  reconnecting the engine, and document the limit.

No option is chosen yet: each rests on a property of `acp-go-sdk` that this record
has not measured. That is what the open questions are for.

## Decision Outcome

**Decided 2026-09-21 — see the amendment "decision taken: contain it, and stop
promising what cannot be delivered" at the end of this record.** The paragraph
below is what was true before the SDK was measured, and is kept so the record shows
why the decision waited.

~~**None yet — this record exists to stop the finding being lost.** MADR 0165 traced
it while fixing a different flake, and withdrew its own P4 rather than paper over
it. A decision needs the SDK questions below answered first, and a plan follows the
decision.~~

### Consequences

* Until this is decided, `TestACPConnectionSurvivesAStalledPump` will keep flaking
  on loaded runners, and the ledger will keep recording it. That is the intended
  state: it is the only thing currently detecting the defect.
* Anyone tempted to "fix the flake" should read MADR 0165's amendment first.

### Confirmation

```bash
# reproduce before changing anything
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -run StalledPump -count=20

# whatever lands must make this pass, and must still assert conn.Done() is open
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -count=20
```

## Pros and Cons of the Options

### A — Drain the SDK's queue into our own buffer

* Good, because it attacks F1 directly: if our consumer never lags, the SDK's queue
  never fills, and the existing cap still bounds memory.
* Good, because the cap and the stall detector keep working unchanged.
* Bad, because "never lags" is a strong claim to make of a goroutine under CPU
  starvation; it may only move the threshold rather than remove it.

### B — Raise the SDK's queue depth

* Good, because it is the smallest change if the SDK exposes the knob.
* Bad, because it buys headroom rather than a guarantee, and the failure returns on
  a slower runner.
* Bad, because it may not be configurable at all — unmeasured.

### C — Backpressure the reader

* Good, because it is the only option that cannot lose events or drop the
  transport: the agent is simply made to wait.
* Bad, because the SDK may offer no way to stop reading without closing, and
  blocking its reader may itself be what triggers a teardown.

### D — Accept and contain

* Good, because engine reconnection already exists and is exercised.
* Bad, because it accepts that one bad session ends every session on the engine,
  which is the outcome 0138 F5 rejected.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| The SDK closes the connection on queue overflow | `acp-go-sdk@v0.13.5 connection.go:108,:446`, quoted at `stalledpump_test.go:16-24` |
| Recorded as a fail-then-pass flake | `ci-flakes.tsv`, run `35461886119`, `windows/amd64`, 2026-09-19 |
| Reproduces 1 in 3 under `GOMAXPROCS=1`, with `conn.Done()` closed | instrumented run, 2026-09-21 |
| `deliver` is O(1) on every path | `internal/provider/acpagent/session.go:1414-1455` |
| `drainOverflow` does not hold the mutex while blocking | `session.go:1488`, `:1490-1501` |
| Production event buffer is 256 | `internal/provider/acpagent/acpagent.go:454` |
| `controlOverflowCap` is 512 | `internal/provider/acpagent/session.go` |
| Closing the pipe on fault causes the teardown | measured while executing MADR 0165 P4 |

### Related records

* **MADR 0138 F5** — established the hazard and added the parked overflow.
* **MADR 0165** — traced this flake while fixing a sibling one, corrected its own
  F7/F8, and withdrew its P4 rather than hide the defect. Read its 2026-09-21
  amendment before touching the test.
* **MADR 0143** — the flake ledger that surfaced it.

### Open questions for the plan

1. Does `acp-go-sdk` expose the notification queue depth, or a hook to drain it?
   Option B depends entirely on this and it is unmeasured.
2. Can the SDK's reader be paused without closing the connection (option C)?
3. Under `GOMAXPROCS=1`, how far does the reader actually run ahead of the
   consumer? That number decides whether option A removes the failure or only moves
   its threshold.
4. Does the production configuration (256-deep buffer, not the fixture's 1) change
   the reproduction rate? Every measurement here used the test fixture.

## Amendment — 2026-09-21: all four questions answered; three of four options are dead

Measured, read-only, before proposing anything. The result narrows this record from
four options to one decision, and it is not the comfortable one.

### The decisive experiment

Under `GOMAXPROCS=1`, 1224 frames at a consumer that never drains, 5 trials per
variant. The variable is our own handler: `real` is `session.SessionUpdate`, `null`
is a type embedding the session that discards every `session/update` without
touching `deliver` — the floor of what the SDK costs with nothing of ours added.

| variant | transport torn down | session faulted | min frames written |
| --- | --- | --- | --- |
| real handler, 1-deep buffer (fixture) | **3/5** | 4/5 | 1025 |
| **null handler**, 1-deep buffer | **3/5** | 0/5 | 1025 |
| real handler, 256-deep buffer (production) | **3/5** | 4/5 | 1025 |
| **null handler**, 256-deep buffer | **3/5** | 0/5 | 1025 |

### Findings

**F6 — Our handler's cost is irrelevant (closes question 3).** A handler that does
*nothing* tears the transport down at exactly the same rate, 3 in 5. The reader
outpaces the SDK's single `processNotifications` goroutine regardless of what we do
on it, because we are downstream of the queue that overflows. **Option A is dead:
there is nothing left for us to make faster.**

The null-handler row is the sharpest evidence in this record: `session_faulted=0/5`
— our stall detector never fires at all, since nothing calls `deliver` — and yet the
transport still dies 3/5. The teardown is entirely independent of our code.

**F7 — The production buffer depth changes nothing (closes question 4).** 256-deep
behaves exactly as 1-deep, 3/5 either way. Of course it does: the overflow happens
in the SDK's queue, upstream of our buffer, so the size of ours cannot matter.

**F8 — `min frames written = 1025` in every variant confirms the mechanism
exactly.** 1024 queued plus one in flight, then the writer blocks. That is
`defaultMaxQueuedNotifications` to the frame, so the failure is the SDK queue
filling and nothing else.

**F9 — The queue depth is not configurable (closes question 1).**
`defaultMaxQueuedNotifications = 1024` is an unexported constant used directly in
`make(chan queuedNotification, defaultMaxQueuedNotifications)`
(`acp-go-sdk@v0.13.5 connection.go:19`, `:108`). No option field, no setter.
**Option B is dead** without forking.

**F10 — The reader cannot be paused (closes question 2).** `receive()` does a
*non-blocking* send into the queue (`connection.go:432`); the `default` branch goes
straight to `shutdownReceive(errNotificationQueueOverflow)` (`:446-447`). The SDK
has decided that a full queue means closing the connection, and it never applies
backpressure to its reader. **Option C is dead.**

**F11 — Containment already exists, and it is clean.** A dead ACP connection is
already turned into an orderly per-session teardown: `watchConnClose`
(`internal/provider/acpagent/session.go:1079`) funnels into `signalDisconnected`
(`:1057`), which emits the terminal error and disconnected status so the session
manager reaps the session. It is wired per session at `acpagent.go:655`. So nothing
zombies — but every session on that engine is torn down, which is **F3**'s blast
radius, unchanged.

**F12 — The property MADR 0138 F5 promised is unachievable at SDK v0.13.5.** Taken
together, F6, F9 and F10 mean no client-side change can keep the transport alive
when the reader outpaces the consumer. A test asserting "the connection survives"
is therefore asserting something no version of our code can satisfy.

That reframes MADR 0165's F4, which said the test should stay failing because it was
detecting something real. It *is* detecting something real. But it is not detecting
a regression we introduced or can fix — so leaving it red indefinitely trains
readers to ignore a red test, which is its own harm.

### What still needs deciding

The options in the body are superseded. What remains is a genuine choice about what
to promise, and it belongs to the owner:

1. **Accept the blast radius and re-aim the test at containment.** Assert what is
   achievable and already true — the connection dying produces a clean teardown via
   F11, not a zombie or a hang — and amend 0138 F5 to say the transport cannot be
   guaranteed under CPU starvation at this SDK version. Honest and cheap. It gives
   up a guarantee that was never deliverable.
2. **Fork or vendor `acp-go-sdk`** so the reader blocks instead of closing, or so
   the depth is configurable. Actually removes the failure. Costs a fork of a
   dependency on the engine transport path, forever or until upstream lands.
3. **Upstream a patch to `acp-go-sdk`** and pin to it when released. Correct
   long-term and benefits every client, but leaves the flake in place meanwhile and
   depends on a third party's schedule.

These are not mutually exclusive: 1 is the only one that can land this week, and 3
is the only one that fixes it for good.

## Amendment — 2026-09-21: decision taken — contain it, and stop promising what cannot be delivered

Owner decision, option 1 of the three the previous amendment left. Options 2 (fork
`acp-go-sdk`) and 3 (upstream a patch) are **deferred, not rejected** — 3 remains
the only permanent fix and is named in the plan's Deferred section.

### The decisions

* **D1 — Stop asserting a property no code can satisfy.** Retire
  "the ACP connection survives a stalled pump" as an assertion.
  **F6**, **F9** and **F10** together show it is unachievable at `acp-go-sdk`
  v0.13.5: a handler doing nothing at all still loses the transport 3 times in 5.
  A red test that no change can turn green teaches readers to ignore red tests.
* **D2 — Assert containment instead, because containment is real and is ours.**
  Under the same storm, prove what **F11** already provides: the session is torn
  down in an orderly way — the stall detector faults it, or a dead transport is
  turned into a terminal error and a `disconnected` status — rather than hanging,
  zombieing, or panicking.
* **D3 — Keep asserting the part that is genuinely our guarantee.** `deliver` must
  absorb `cap(events) + controlOverflowCap` events without blocking. That is the
  parked overflow doing its job, it is unaffected by the SDK's queue, and it is
  measurable. Closes the useful half of MADR 0165's F7.
* **D4 — Change no production behaviour.** The parked overflow stays exactly as it
  is: it bounds memory and ends a stalled session deliberately. What was wrong was
  never the mechanism, only the claim made for it.
* **D5 — Correct the claims in the code.** Three comments state or imply that the
  parked overflow keeps the transport alive
  (`session.go:110-125`, `:1407-1413`, `stalledpump_test.go:16-24`). They are the
  reason the wrong assertion looked correct for so long, and they must say what is
  actually true.
* **D6 — Amend MADR 0138 F5 rather than silently drop it.** Its hazard analysis was
  right; its mitigation is weaker than it claimed. An additive amendment pointing at
  this record keeps that history legible.
* **D7 — Verify under `GOMAXPROCS=1`.** Default parallelism hides the failure — that
  is why this survived from 2026-09-19. Any test claiming to cover it runs in the
  configuration that reproduces it.

### Consequences of deciding this way

* Good: CI stops carrying a permanently-red test, and starts carrying one that fails
  only if containment actually regresses.
* Good: no production change, so nothing to roll back and no risk to the transport
  path.
* Bad: the product accepts that one stalled consumer ends every session on that
  engine (**F3**). That is a real reduction in what we promise, written down rather
  than discovered later.
* Bad: if `acp-go-sdk` later makes the reader block or the depth configurable, this
  test will under-assert — it will pass where the stronger property has become
  available. The plan's Deferred section names that as the trigger to revisit.

## Amendment — 2026-09-22: the deferred items are decided by MADR 0167

This record deferred three items by name. MADR 0167 (accepted 2026-09-22) decides two of them:

* **Upstreaming a patch — decided.** The contribution is an overflow-*policy* option layered on
  the open PR #40's `ConnectionOption` surface, defaulting to today's fail-fast behaviour, with
  `OverflowDropNewest` available to consumers that prefer losing a notification to losing the
  connection. Capacity alone does not fix this record's defect: F6's null-handler measurement
  loses the transport 3/5 at any depth.
* **Forking/vendoring the SDK — rejected as a goal**, retained as a contingency. The fork exists
  only as the development vehicle for the PR; this project's `go.mod` does not change.
* **Per-engine blast radius (F3)** — still deferred, unaffected.

**What does not change here.** D1–D7 stand: this project remains on upstream `v0.13.5`, the
retired "transport survives" assertion stays retired, and containment is the guarantee. That only
changes if the PR merges **and** this project later adopts the drop policy, which is its own
decision (MADR 0167 D13).
