---
status: proposed
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
under `GOMAXPROCS=1`; once in the ledger over the observed period on real CI. A
retry that passes records it as a flake, which is why it survived from 2026-09-19
until it was traced.

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

**None yet — this record exists to stop the finding being lost.** MADR 0165 traced
it while fixing a different flake, and withdrew its own P4 rather than paper over
it. A decision needs the SDK questions below answered first, and a plan follows the
decision.

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
