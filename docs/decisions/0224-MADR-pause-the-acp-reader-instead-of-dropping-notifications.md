---
status: accepted
date: 2026-10-31
decision-makers: Project Owner
consulted: "GitHub issue #32; acp-go-sdk2 v1.28.0 and v1.29.0 from the Go module proxy (connection.go, errors.go, doc.go diffed; docs/decisions/0044-MADR-notification-queue-back-pressure.md; docs/reports/0044-REPORT-release-notes-v1-29-0.md); this repository at 7ed38787 (internal/provider/acpagent, internal/provider/grok, internal/config); 0166-MADR, 0167-MADR (D16 to D20), 0184-MADR (D12), 0207-MADR (D12, D13, amendments H and K); gobble-cli's go.mod and acpclient/conn.go, read locally; the daemon's log on this Mac; scratch probes leftovers/0224_sdk_probe.py, 0224_probe_test.go.txt, 0224_baseline.py and 0224_mu_scan.py; for amendment A, acp-go-sdk2 v1.30.0 from the Go module proxy diffed against v1.29.0 (connection.go, errors.go, extensions.go, types_gen.go; docs/decisions/0045-MADR-notification-queue-byte-budget.md, 0046-MADR-sending-and-receiving-middleware.md; docs/reports/0044-REPORT-consumer-check-of-the-v1-29-0-default.md, 0045-REPORT-release-notes-v1-30-0.md), and scratch probes leftovers/0224_sdk_probe_v130.py and 0224_v130_sum.py"
informed: anyone who runs an ACP agent through Magic (grok today; gobble when it is registered)
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Pause the ACP reader instead of dropping notifications: adopt acp-go-sdk2 v1.29.0's OverflowBlock

## Context and Problem Statement

Magic talks to ACP agents through `acp-go-sdk2`. The SDK reads an agent's output on one goroutine and
hands notifications, chiefly `session/update`, to a single consumer through a bounded queue, 1,024
deep by default. When that queue is full, the SDK must do something with the next notification.

Until now it had two choices, and Magic picked the lesser harm:

* **Close the connection** (the SDK's old default). [0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md](0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md)
  measured that even a handler doing nothing lost the transport 3 times in 5 under `GOMAXPROCS=1`,
  because the reader outran the consumer under CPU starvation.
* **Drop the notification** (`OverflowDropNewest`). [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
  chose it in its amendment of 2026-09-22: D17 opts the client in, D18 tells the user per turn that
  output *may* be incomplete, and D19 reinstates the transport-survival test. 0167's P4 measured the
  trade: 20/20 survival, 0 to 199 updates dropped per run.
  [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)
  D12 added a drop handler that records the method, so D18's notice can name it.

`acp-go-sdk2 v1.29.0` adds a third choice and makes it the default: **pause the reader** until a
handler returns, so the agent's writes wait and nothing is lost. GitHub issue #32 proposes adopting
it. The question this record answers: should Magic's ACP client pause instead of drop, what goes with
the drop machinery, and what does the issue get right and wrong about the consequences?

### What v1.29.0 changes [observed: both module versions, `diff -r`; the SDK's 0044 records at the tag]

* **F1 — One API addition, one change of default, three production files.**
  * `diff -rq` of v1.28.0 against v1.29.0: the Go files outside tests that differ are `connection.go`,
    `errors.go` and `doc.go`. The rest are tests, docs and `tools/`.
  * The SDK's release report (`docs/reports/0044-REPORT-release-notes-v1-29-0.md`): `make api`
    against v1.28.0, "0 incompatible, 1 compatible", `OverflowBlock` added. The ACP schema is still
    `1.25.0`, and "Nothing sent or accepted on the wire changes."
  * `connection.go`, v1.29.0: `newConnection` sets `notificationOverflow: OverflowBlock` before the
    options apply, and maps any undefined value to it. Under `OverflowBlock` the queue's channel holds
    `2n`, and `queueLimit` holds `n`.
  * Fetched from the proxy: v1.29.0, origin `f2c53b9c08f9…`, `h1:FVSj/mafo+…`. Its `go.mod` hash,
    `h1:Iz5f3g3L3m…`, is v1.28.0's, so the SDK's own requirements did not change.
* **F2 — What the pause is** (`waitForRoom`, `beginPeerWait` and `inRunningNotification`, added in
  `connection.go`; the godoc of `OverflowBlock`).
  * The reader waits, before giving a notification its sequence number, while more than `n`
    notifications are received and not completed, no wait of the notification loop on the reader is
    counted, and the connection lives.
  * **The reading-on guard.** Two waits are counted: a request sent under the context of the
    notification whose handler is running, and every `sendRequestCommit`, which is `Initialize`. While
    either is counted the reader reads on, to `2n`. Past `2n` it closes with
    `ErrNotificationQueueOverflow`, as `OverflowCloseConnection` does.
  * **The log.** A pause that lasts `pauseWarnAfter`, 5 s, logs once at `Warn`:
    `notification queue full; reading paused`, with `capacity` and `paused`.
  * **The rule the SDK cannot check** (godoc of `MethodHandler` and `OverflowBlock`; SDK 0044 D7):
    under `OverflowBlock`, a notification handler must not wait for a request or a
    `$/cancel_request` the peer sends later, nor for code that waits for one.
  * **What is unchanged** (SDK 0044 D6): `OverflowDropNewest` and `OverflowCloseConnection` behave as
    before when named; `DroppedNotifications` stays zero unless `OverflowDropNewest` is chosen.
* **F3 — How far a pause reaches depends on the transport** (SDK 0044 F6). Over a child's stdio, the
  OS pipe fills and the agent's writes wait. Magic runs every ACP agent as a child over stdio through
  `agentproc` (`acpagent.go:531-536`), and uses neither `ServeAgent`, `InProcess` nor package
  `remote` (no import of `acp-go-sdk2/remote` in non-test code). So under a pause, the agent is slowed
  at its `session/update` writes, and `session/cancel` still ends its turn (SDK 0044 F5).
* **F4 — Why the SDK's maintainers think the pause adds no wait a caller does not already have.**
  Every response already waits for the notifications received before it (the response barrier, SDK
  0044 F3). A response read late behind a paused reader therefore returns at the same moment it
  would have: when the last notification before it is handled. The two exceptions, a handler's own
  request and `Initialize`, are the two waits F2's guard counts. The SDK's 0044 F9 probes and nine
  `synctest` tests (`connection_overflow_block_test.go`) cover them.

### What Magic does today [observed at `7ed38787`]

* **F5 — The drop machinery.**
  * `clientConnOptions` (`internal/provider/acpagent/acpagent.go:1133-1141`) names
    `acp.WithNotificationOverflowPolicy(acp.OverflowDropNewest)` and
    `acp.WithNotificationDropHandler(s.recordDropped)`. It is the one definition production uses
    (`acpagent.go:534`) and the stalled-pump and wire tests share.
  * `session.go`: the `testDropped` and `lastDropped` fields (`:240-245`); `droppedNotifications`
    (`:623-633`); `recordDropped`, which stores the method (`:635-643`); `noticeDroppedUpdates`, the
    per-turn notice "The agent sent updates faster than they could be processed and N were dropped,
    so this turn's output may be incomplete." (`:658-688`), snapshotted at `:442` and emitted at
    `:500`.
  * Tests: `droppednotice_test.go` (three tests, two driving `testDropped` and `recordDropped`
    directly), and `TestDropHandlerRecordsTheDroppedMethod` (`stalledpump_test.go:315-354`).
  * No guide, README or architecture document describes the policy. It lives only in code and records.
* **F6 — One agent process, and one ACP connection, per Magic session. This contradicts 0166 F3.**
  * `Provider.Start` (`acpagent.go:860`) takes its `*session` from `claimWarm` (`:809`), a spare
    spawned by `EnsureWarm` (`:753`), or from `spawnAgent` (`:469`). `spawnAgent` starts one agent
    process with one connection whose client is that session (`:531-542`).
  * `Start` then opens exactly one ACP session on it: `conn.LoadSession` *or* `conn.NewSession`, an
    if/else (`:975`, `:991`). No other non-test code opens a session on a connection.
  * `git log -S "spawnAgent("` dates this shape to `91757927` (2026-07-22), before 0166 (2026-09-21).
  * So **0166 F3, "The connection is per engine, not per session", is not true of this code**, and
    neither are the comments that repeat it: `acpagent.go:1113-1115` ("which would end every session
    on this engine"), `session.go:667-670` and `stalledpump_test.go:251-252`. Issue #32's central
    trade-off, "a pause holds all of them, permission requests included", inherits the error. A
    pause holds **one** session: the one whose agent outran its own consumer.
* **F7 — Magic's notification handlers never wait on the agent: the rule F2 states already holds.**
  * `SessionUpdate` (`session.go:1814`) sends no request. Its header says so, and why
    (`:1809-1813`): acp-go-sdk2 v0.17.1 deadlocked on a request sent from a notification handler, and
    "the rule stays because nothing here needs to send one".
  * Every path into the consumer ends in `emit` (`:1464`) and `deliver` (`:1628`). `deliver` returns
    in O(1) on every path, by design and comment (`:1680-1682`): the channel takes the event, the
    overflow parks it, or the session is faulted at `controlOverflowCap`, 512 (`:1699-1708`, `:1731`).
  * The extension notifications, which run on the same loop (`extensions.go:82-97`), are grok's
    three: `HandleModelsUpdate`, `HandleMCPStatus` and `HandleMCPInit` (`acpagent.go:1370-1436`). Each
    parses and stores, under `catalogMu` or `mcpMu`, and sends nothing.
  * The locks they take are never held across a wait on the agent. Every `catalogMu`, `mcpMu` and
    `overflowMu` section stores or copies and releases. `leftovers/0224_mu_scan.py` lists the 17
    connection calls in non-test `acpagent` code, and `s.mu` is free at each. That scan is line-based,
    not a proof. `RequestPermission` reads `s.mu` and releases it (`session.go:2418-2420`), and
    `awaitDecision` releases it before it waits (`:2494-2504`, wait from `:2550`).
  * Agent *requests* (`RequestPermission`, file and terminal calls) run on their own goroutines, not
    on the notification loop (SDK 0044 F1).
* **F8 — Only grok uses this path in production today.** The only non-test constructions of an
  `acpagent` provider are `grok/grok.go:146` and `:151`. gobble is named in comments
  (`stderrtail.go:5`) but not registered as a provider.
* **F9 — How often drops happen in practice is unknown.** The SDK logs a drop only at `Debug`
  (`connection.go:1021` at v1.28.0). The daemon's log on this Mac (`~/Library/Logs/mcremote`,
  2026-08-01 to 2026-10-31) has 0 `Debug` lines, so it cannot show one. It has 0 lines of the SDK's
  close-on-overflow and 0 of Magic's stall detector firing. Its 13 "dropping event; slow consumer"
  lines are all grok `available_commands` events, dropped on Magic's *own* best-effort path
  (`session.go:1629-1638`), a different queue that this decision does not touch.
* **F10 — What a long silence already shows the user.** `watchStall` (`session.go:849-875`) emits
  "Still waiting — no output from the agent for …" after `TurnStallNotice`, 120 s for grok by default
  (`internal/config/config.go:803`). It reads `lastActivity`, which every `SessionUpdate` refreshes
  (`:1817`).

### What was measured [2026-10-31, scratch copies of `7ed38787` made with `git archive`; the tree was not written]

`leftovers/0224_sdk_probe.py`, `leftovers/0224_baseline.py`; logs `leftovers/0224-*.log`.

* **P1 — The bump alone changes nothing Magic relies on.** v1.28.0 → v1.29.0 with
  `OverflowDropNewest` still named:
  * `go.mod` changes one line and `go.sum` two: the module's two hashes;
  * `go build ./...` and `go vet ./...`, exit 0;
  * `go test -race ./internal/provider/acpagent/`, 410 PASS;
  * the two stalled-pump tests, the drop-handler test and the drop-notice tests, `-race -count=5`:
    75 PASS;
  * `go test ./...`: 44 packages ok, one failure, `TestTrackedFilesUseOnlyPlaceholderHomes`, "no git
    index". The same test fails the same way in a copy of `HEAD` without the bump: a `git archive`
    copy has no `.git`.
* **P2 — The switch.** v1.29.0 with `clientConnOptions` naming `OverflowBlock` and no drop handler:
  * `go build ./...`, exit 0;
  * `TestDropHandlerRecordsTheDroppedMethod` **hangs**: at a 60 s timeout it is blocked in
    `io.(*PipeWriter).Write` at `stalledpump_test.go:338`, the test's own synchronous write, because
    the reader has paused. It asserts that drops happen, so it cannot pass under this policy;
  * every other `acpagent` test, `-race`, with that one skipped: 409 PASS;
  * `TestACPConnectionSurvivesAStalledPump` and `TestAStalledPumpIsContainedNotHung`, under
    `GOMAXPROCS=1`, `-race`, 20 runs each: 40 PASS, the condition 0166 D7 sets.
* **P3 — The property to pin instead, seen failing first.** A scratch test,
  `TestProbeBlockPausesAndLosesNothing` (`leftovers/0224_probe_test.go.txt`), builds a connection
  with `clientConnOptions`, blocks its `session/update` handler, writes 1,224 `tool_call`
  notifications from a goroutine, waits for the writer to stop, then releases the handler:
  * with `OverflowDropNewest` (today's options on v1.29.0): **fails**, "the writer was never paused:
    1224 frames written";
  * with `OverflowBlock`: **passes**. The writer paused after **1,026 of 1,224** frames: the queue's
    1,024, the one whose handler runs, and the one in the reader's hand. The connection stayed open
    while paused, with 0 dropped. After the release all 1,224 were handled, in order, with 0 dropped
    and the connection open.

### The issue's other claims, checked

* **Correct:** v1.29.0 is tagged and Magic pins v1.28.0 (`go.mod:11`); the line references to
  `clientConnOptions`, `session.go:1308-1310` and `stalledpump_test.go:132-135`; the drop-handler
  test hangs and the other tests pass (P2); the stale comment at `stalledpump_test.go:18-33`, which
  cites `acp-go-sdk@v0.13.5`.
* **Wrong, and material:** "One engine connection carries every session on that engine, so a pause
  holds all of them, permission requests included" (F6).
* **Incomplete:** the issue does not mention 0184 D12 (F11), and does not say that the drop notice's
  absence from logs proves nothing (F9).

### The pin rule with gobble

* **F11 — 0184 D12 has Magic and gobble pin the same `acp-go-sdk2` tag** and bump together, "each bump
  checked by both repositories' live ACP suites"
  ([0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)).
  * gobble-cli's `go.mod:16` pins `v1.28.0`, as Magic does, so the rule holds today.
  * gobble also names `OverflowDropNewest`, in its own ACP client (`acpclient/conn.go:127`). That is
    gobble's decision, in gobble's repository.
  * Precedent: for a bump under 0207's amendment H the owner chose "Magic first (Recommended)", with
    gobble following in its own repository and D12 not holding meanwhile. 0207's amendment K, the
    v1.28.0 bump, ran no live suite.

## Decision Drivers

* **D-1 — Lose nothing.** A dropped `session/update` is a missing piece of a transcript, which the
  user is told about only as "may be incomplete".
* **D-2 — Keep the transport.** 0166 and 0167 exist because closing it was worse.
* **D-3 — No new deadlock.** Adopt the pause only if Magic keeps the rule it needs (F2, F7).
* **D-4 — Records and comments that say what the code does.** F6 shows a recorded fact that is false.
* **D-5 — The smallest change that does it,** in the pin series 0207 already runs.

## Considered Options

* **A — Pin v1.29.0, name `OverflowBlock` in `clientConnOptions`, and retire the drop notice and its
  recorder.**
* **B — As A, but keep the drop notice and recorder as a defence.**
* **C — Pin v1.29.0 and keep `OverflowDropNewest` named.**
* **D — Pin v1.29.0 and name no policy, taking the SDK's default.**
* **E — Stay on v1.28.0.**

## Decision Outcome

Chosen option: "A", because it is the only option that meets D-1 and D-2 together: nothing is
dropped and the connection stays open (P3). Magic already keeps the rule the pause needs (F7), and the
issue's main cost does not exist, since a pause holds one session, not all of them (F6). C keeps the
loss. D leaves the policy implicit, which breaks 0167 D19's point that the survival test pins what
production builds. E forgoes the fix. B keeps code that cannot run, since `DroppedNotifications` stays
zero under `OverflowBlock` (F2).

### The decisions

* **D1 — Pin `acp-go-sdk2` ~~`v1.29.0`~~ `v1.30.0`.** One `go.mod` line, two `go.sum` lines (P1;
  P4), in 0207's pin series. *Amended 2026-10-31 by amendment A: v1.30.0 was tagged before this
  record was executed, and is pinned instead.*
* **D2 — `clientConnOptions` names `acp.OverflowBlock` and drops the drop handler.**
  * The policy is named although it is the SDK's default, so the shared options, and the survival
    test built from them, state what production builds (0167 D19's reason).
  * `WithNotificationDropHandler(s.recordDropped)` goes. Under `OverflowBlock` it never fires.
  * This amends 0167 D17 and 0207 D12.
* **D3 — The per-turn drop notice and its recorder are retired.**
  * `noticeDroppedUpdates`, `droppedNotifications`, `recordDropped`, the `lastDropped` and
    `testDropped` fields, the snapshot at `session.go:442` and the call at `:500`, and
    `droppednotice_test.go`. The counter they read cannot rise under `OverflowBlock` (F2).
  * This amends 0167 D18. The inbound-refusal notice of 0207 D13 (`noteInbound`,
    `noticeRefusedUpdates`) is a different mechanism and stays.
  * Whether to keep them instead is Q1 below; A retires them.
* **D4 — The tests pin the new property.**
  * `TestDropHandlerRecordsTheDroppedMethod` is replaced by a test of P3's property: with the
    handler blocked, a flood past the queue's depth pauses the writer instead of dropping, the
    connection stays open, and on release every notification arrives, in order, with
    `DroppedNotifications()` at 0. The flood runs on its own goroutine with deadlines, since the writer
    is now expected to wait. It is seen failing first with `OverflowDropNewest` named, as P3 shows it
    does.
  * `TestACPConnectionSurvivesAStalledPump` and `TestAStalledPumpIsContainedNotHung` stay, with their
    comments brought up to date. Both pass under `OverflowBlock` (P2).
* **D5 — The rule of F2 is written where a future handler will meet it.** The comment on
  `SessionUpdate` (`session.go:1809-1813`) and on `HandleExtensionMethod` (`extensions.go:78-81`) says
  that under `OverflowBlock` a notification handler must not wait for a request or a
  `$/cancel_request` the agent sends later, nor for code that waits for one, nor take a lock held
  across such a wait. No automated check is decided here: the scan of F7 is a heuristic, not a gate.
* **D6 — Correct what is false.**
  * 0166 F3 gets an additive amendment: the connection is per session, and has been since
    2026-07-22 (F6).
  * The comments at `acpagent.go:1110-1132`, `session.go:1306-1310`, `deliver`'s
    `session.go:1664-1678` (which still describes v0.13.5's close), and `stalledpump_test.go:18-33`
    and `:248-261` are rewritten to say what the code does under `OverflowBlock`.
  * 0166 F3's error is not repeated in this record's consequences.
  * *Extended 2026-10-31 (PLAN deviation 1, on the owner's word):* four more places describe the old
    SDK's behaviour and are rewritten too: the overflow fields' comment (`session.go:107-127`),
    `TestAStalledPumpIsContainedNotHung`'s comment (`stalledpump_test.go:117-142`), whose "revisit"
    condition v1.29.0 met, a comment in the survival test (`:278-279`), and that test's failure
    message, which printed `DroppedNotifications()` (`:293-294`). No assertion changes.
* **D7 — No phone notice for a pause.** A pause loses nothing, so there is nothing to warn about per
  turn. The SDK's `Warn` after 5 s (F2) reaches the daemon's log through `WithLogger`, which
  `clientConnOptions` already passes (`acpagent.go:1138`). A pause long enough for the user to notice
  is already covered by `watchStall`'s notice (F10).
* **D8 — Magic moves first; gobble follows in its own repository** (F11, the amendment-H
  precedent). 0184 D12 does not hold until gobble pins ~~v1.29.0~~ v1.30.0 (amendment A). Whether gobble also moves its own
  client off `OverflowDropNewest` is gobble's decision. Q2 below.
* **D9 — No live run is required for this change.** Q3 below.

### Consequences

* Good, because a slow consumer no longer costs the user any of the agent's output (P3), and the
  transport survives as it does today (P2, 40/40 under `GOMAXPROCS=1`).
* Good, because code and tests that exist only to explain a loss go (D3): `session.go:623-688`,
  two fields, two call sites, and the three tests of `droppednotice_test.go`.
* Good, because the records and comments stop asserting a per-engine connection (D6).
* Neutral, because only grok reaches this path today (F8), and drops have never been observed in its
  logs, though the logs could not have shown them (F9).
* Bad, because a session whose consumer is behind now slows its agent instead of losing updates. Its
  own permission requests and cancel replies wait behind the queue until the consumer catches up
  (F2). With `deliver` non-blocking (F7) that lasts only while the consumer is behind, and a consumer
  that stops entirely is still faulted at `controlOverflowCap`, which ends the session as it does
  today.
* Bad, because Magic now depends on a rule the SDK cannot check (F2). A future handler that waits on
  the agent would deadlock its session under a full queue rather than drop. D5 states the rule where
  such a handler would be written.
* Bad, because 0184 D12 does not hold until gobble moves (D8).

### Confirmation

* `go list -m github.com/maccavelli/acp-go-sdk2` gives ~~`v1.29.0`~~ `v1.30.0` (amendment A).
* `clientConnOptions` names `acp.OverflowBlock`; no non-test code names `OverflowDropNewest`,
  `WithNotificationDropHandler`, `DroppedNotifications` or `recordDropped`.
* The test of D4 passes, and was seen failing with `OverflowDropNewest` named.
* `TestACPConnectionSurvivesAStalledPump` and `TestAStalledPumpIsContainedNotHung` pass 20 of 20
  under `GOMAXPROCS=1` and `-race`.
* `make race`, `make staticcheck`, `make vulncheck`, `make check-libs` on the committed `HEAD`, and
  the Linux and Windows test hosts pass.

## Pros and Cons of the Options

### A — Pause, named; retire the drop notice

* Good, because nothing is lost and the connection stays open (P3).
* Good, because the policy stays visible at the one place production builds its options.
* Good, because the code shrinks by the part that only explained a loss.
* Bad, because the drop notice cannot come back without code, if the policy is ever changed back.

### B — Pause, named; keep the drop notice as a defence

* Good, because a later change back to `OverflowDropNewest` would be explained to the user at once.
* Bad, because the code cannot run while the policy is `OverflowBlock` (F2), and its tests would
  drive it through `testDropped` only, testing a path production cannot take.

### C — Bump, keep `OverflowDropNewest`

* Good, because it changes no behaviour, as amendment K's bump did not.
* Bad, because updates are still dropped when the consumer falls behind (0167's 0 to 199 per run).

### D — Bump, name no policy

* Good, because the SDK's default is the chosen behaviour.
* Bad, because the shared options would no longer say what production builds, and a future SDK
  default would change Magic silently.

### E — Stay on v1.28.0

* Good, because nothing changes.
* Bad, because the loss stays, and Magic falls behind the pin series 0207 runs.

## More Information

### Questions for the owner

* **Q1 — The drop notice (D3).** Retire it with its recorder and tests (recommended), or keep them as
  a defence, as B?
* **Q2 — gobble (D8).** Magic first, gobble after in its own repository (recommended, as amendment
  H), or wait for gobble so the two move together?
* **Q3 — Quota (D9).** No live run (recommended: the change acts only when a queue fills, which a
  live turn does not provoke, and amendment K ran none for its bump), or one `make live-grok`, which
  spends grok quota?

*Answered by the owner, 2026-10-31:* Q1 "drop it": the notice, its recorder and their tests are
retired (D3). Q2 "move first": Magic pins v1.29.0 now (v1.30.0, by amendment A), and gobble follows in its own repository
(D8). Q3 "skip it": no live run (D9). Each answer is the option this record recommends, so no
decision above changes.

### Evidence index

| Claim | Source |
| --- | --- |
| Three production files changed; one compatible addition | `diff -rq` of the two module directories; SDK `docs/reports/0044-REPORT-release-notes-v1-29-0.md` |
| `OverflowBlock` is the default; queue `2n`; pause and guard | v1.29.0 `connection.go`: `newConnection`, `waitForRoom`, `beginPeerWait`, `inRunningNotification`, `sendRequestCommit` |
| The rule a handler must keep | v1.29.0 godoc of `MethodHandler` and `OverflowBlock`; SDK 0044 D7 |
| Same requirements | `go.mod` hash `h1:Iz5f3g3L3m…` at both versions (`go mod download -json`; `leftovers/0224_baseline.py`) |
| Drop machinery | `acpagent.go:1133-1141`; `session.go:240-245`, `:442`, `:500`, `:623-688` |
| One connection per session | `acpagent.go:469`, `:531-542`, `:753`, `:809`, `:860`, `:975`, `:991`; `git log -S "spawnAgent("` → `91757927` |
| Handlers never wait on the agent | `session.go:1464`, `:1628-1731`, `:1809-1813`, `:2418-2420`, `:2494-2504`; `extensions.go:82-97`; `acpagent.go:1370-1436`; `leftovers/0224_mu_scan.py` |
| Only grok in production | `grok/grok.go:146`, `:151` |
| Drops cannot be seen in the log | v1.28.0 `connection.go:1021` (`Debug`); 0 `level=DEBUG` lines in the daemon's log |
| P1 to P3 | `leftovers/0224-{bump,block,probe-drop,probe-block}-*.log`; `leftovers/0224_baseline.py` output |
| gobble pins v1.28.0 and names `OverflowDropNewest` | gobble-cli `go.mod:16`, `acpclient/conn.go:127` |

### Related records

* [0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md](0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md):
  F3 corrected by D6.
* [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md):
  D17 and D18 amended by D2 and D3; D19 kept.
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md):
  D12, the pin rule with gobble.
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md):
  D12 amended by D2 and D3; D13 kept; the pin series D1 joins.
* GitHub issue #32, which proposed this change.

## Amendment — 2026-10-31 (A): `acp-go-sdk2 v1.30.0`

### Context

`acp-go-sdk2 v1.30.0` was tagged on 2026-10-31, after this record and its plan were written and
before either was approved or executed. The owner asked that the pin be updated and the release's new
functionality assessed. This amendment does both. It changes D1's version and D8's, adds D10, and
leaves D2 to D7 and D9 as they are.

### What v1.30.0 changes [observed: both module versions, `diff -rq` and `diff -u`; the SDK's records at the tag]

* **F12 — Two API additions, four production files, the same requirements.**
  * The tag `v1.30.0` peels to `d65819a94986…` (`git ls-remote`). From the proxy: `h1:tKVtczDjyi1e…`.
    Its `go.mod` hash, `h1:Iz5f3g3L3m…`, is v1.28.0's and v1.29.0's, so the SDK's requirements did
    not change.
  * `diff -rq` of the two module directories: the Go files outside tests that differ are
    `connection.go`, `errors.go`, `extensions.go` and `types_gen.go`. `errors.go` and `extensions.go`
    change doc comments only.
  * The SDK's release report (`docs/reports/0045-REPORT-release-notes-v1-30-0.md`): `make api`
    against v1.29.0, "0 incompatible, 2 compatible", `WithMaxQueuedBytes` and
    `(*Connection).QueuedBytes` added. The ACP schema is still `1.25.0`.
* **F13 — A byte budget beside the count** (SDK 0045 MADR, `accepted`; v1.30.0 `connection.go`).
  * The connection counts the line length of every message it has received and not finished:
    notifications queued or running, and requests whose handlers run, each until its handler returns.
  * The budget is 64 MiB by default (`defaultMaxQueuedBytes`, `connection.go:26`), set by
    `WithMaxQueuedBytes` (`:545`), and raised to `WithMaxMessageSize` when below it (`:814`). Magic's
    message bound is 10 MiB (`acpagent.go:1144`), so Magic's budget would be the default 64 MiB.
  * **Notifications.** A notification that would take the count past the budget, while another
    notification is counted, meets the policy as at the count (`overByteBudget`, `:1990`). Under
    `OverflowBlock` the reader pauses (`waitForRoom`, `:1965`), and reads on to twice the budget
    under F2's guard. Requests' bytes alone never pause the reader.
  * **Requests.** A request that would take the count past the budget, while anything is counted, is
    answered at once with `-32603`, data `{"error":"too many queued bytes","limit":n}`, and its
    handler does not run (`admitRequestBytes`, `:2002`; the answer at `:1650`). It is logged only at
    `Debug`.
  * **The log.** F2's pause `Warn` gains `bytes`, `size` and `byte_limit` (`:1974`).
* **F14 — What the budget means for Magic.**
  * **Memory.** At v1.29.0 under `OverflowBlock`, the queue could hold `2n` notifications of up to
    10 MiB each while reading on: about 20 GiB in principle. At v1.30.0 the budget caps it at twice
    64 MiB. The 0224 PLAN's "Not done" item, bounding notification memory by bytes (SDK 0044 F8), is
    what the SDK now does.
  * **A new refusal.** Magic answers these agent requests: `RequestPermission`, `CreateElicitation`,
    `ReadTextFile`, `WriteTextFile`, the five terminal calls (`session.go:2417-2775`,
    `elicitation.go:31`), and grok's extension requests (`extensions.go:82`). Of these, only
    `fs/write_text_file` carries a body that can approach the 10 MiB bound. It is refused only if the
    bytes already held plus its own pass 64 MiB, which needs about 54 MiB of unfinished messages.
    Magic's notification handlers return in O(1) (F7), and the requests that wait on the user hold
    only their own short lines. No such backlog has been observed, but, as F9 says of drops, the log
    could not show one: the refusal is logged at `Debug`, and grok would see it as a failed tool
    call.
  * **The count still binds first** for small notifications: at 1,024, the budget binds only when
    the average notification held passes 64 KiB (SDK release report). The size of grok's
    `session/update` lines was not measured.
* **F15 — `RequestId` refuses a JSON object** (`types_gen.go`, the union's `Null` trial removed; the
  error `RequestId: no variant matches`, `:11753`).
  * No non-test Magic code decodes a `RequestId`. It reaches Magic only inside
    `ElicitationRequestScope`, and Magic declines every request-scoped form
    (`elicitation.request_scope`, MADR 0191 D1).
  * The one test that builds one, `TestElicitationFormsMagicCannotShow`
    (`elicitation_test.go:333`), passes on v1.30.0 (P4).
* **F16 — What the release does not carry.**
  * The SDK's 0046 MADR, sending and receiving middleware, is `proposed`. No API for it is in
    v1.30.0 (F12), so there is nothing to adopt.
  * The SDK's consumer report on v1.29.0 (`0044-REPORT-consumer-check-of-the-v1-29-0-default.md`,
    line 95) repeats issue #32's trade-off, "one engine connection carries every session on that
    engine". F6 shows that is not true of Magic.

### What was measured [2026-10-31, scratch copies of `dd62a24d` made with `git archive`; the tree was not written]

`leftovers/0224_sdk_probe_v130.py`, the probes of P1 to P3 with the version changed, and
`leftovers/0224_v130_sum.py`; logs `leftovers/0224v130-*.log`.

* **P4 — v1.30.0 gives P1 to P3's results unchanged.**
  * **The bump**, `OverflowDropNewest` still named:
    * `go.mod` changes one line and `go.sum` two, the module's two hashes: `h1:tKVtczDjyi1e…` and
      the unchanged `go.mod` hash;
    * `go build ./...` and `go vet ./...`, exit 0;
    * `go test -race ./internal/provider/acpagent/`, 410 PASS;
    * the stalled-pump, drop-handler and drop-notice tests, `-race -count=5`: 75 PASS;
    * `go test ./...`: 44 packages ok; one failure, `TestTrackedFilesUseOnlyPlaceholderHomes`, "no
      git index", the copy's missing `.git`, as in P1.
  * **The switch**, `OverflowBlock` named and no drop handler: build exit 0;
    `TestDropHandlerRecordsTheDroppedMethod` hangs, blocked at `stalledpump_test.go:338`; the other
    `acpagent` tests, 409 PASS; the two stalled-pump tests under `GOMAXPROCS=1`, 20 runs each, 40
    PASS.
  * **The property.** With `OverflowDropNewest`: fails, "the writer was never paused: 1224 frames
    written". With `OverflowBlock`: passes, "writer paused after 1026 of 1224 frames". The byte
    budget does not change where the pause falls: 1,224 `tool_call` frames are far below 64 MiB.

### Decisions

* **D1, amended — Pin `acp-go-sdk2 v1.30.0`**, not v1.29.0. It carries everything this record
  adopts from v1.29.0 (P4), bounds memory where v1.29.0 did not (F14), and pinning v1.29.0 first would
  be a second bump of the same line for no gain.
* **D8, amended — gobble follows to v1.30.0.** 0184 D12 holds again when gobble pins v1.30.0.
* **D10 — Take the byte budget's default.** Magic names neither `WithMaxQueuedBytes` nor
  `QueuedBytes`, as it names no `WithMaxQueuedNotifications` and takes the count's default.
  * The comment on `clientConnOptions` says that a pause comes at the count or at 64 MiB, and that a
    request past the budget is refused by the SDK.
  * A metric of `QueuedBytes` is not added: Magic has no metrics surface for the connection.
  * Whether to name the budget instead is Q4 below.

### Consequences

* Good, because the pause's memory is bounded at twice 64 MiB rather than by the count alone (F14).
* Good, because the bump is one change of the pin, not two.
* Neutral, because the `RequestId` fix reaches only request-scoped forms, which Magic already
  declines (F15).
* Bad, because a new failure is possible: an agent request refused at the budget, seen by grok as a
  failed tool call and by Magic only at `Debug` (F14). It needs a backlog of about 54 MiB, which
  Magic's O(1) handlers are designed to prevent.
* Bad, because the budget's default is the SDK's to change. Under D10 a later SDK default would change
  Magic's limit without a change here, which is the cost option D carries for the policy.

### Confirmation, amended

* `go list -m github.com/maccavelli/acp-go-sdk2` gives `v1.30.0`, and `go.sum` holds
  `h1:tKVtczDjyi1e…` for it.
* The rest of the Confirmation above holds unchanged.

### Questions for the owner

* **Q4 — The byte budget (D10).** Take the SDK's default, 64 MiB, as Magic takes the count's
  (recommended), or name `WithMaxQueuedBytes(64 << 20)` in `clientConnOptions`, so a later SDK default
  cannot change it unseen?

*Answered by the owner, 2026-10-31:* Q4 "use the sdk default": D10 stands. The owner accepted this
record, with amendment A, the same day.

### Evidence index, amended

| Claim | Source |
| --- | --- |
| Four production files changed; two compatible additions; same requirements | `diff -rq` of the two module directories; SDK `docs/reports/0045-REPORT-release-notes-v1-30-0.md`; `go mod download -json` |
| The budget, the pause and the refusal | v1.30.0 `connection.go:26`, `:545`, `:814`, `:1650`, `:1965`, `:1974`, `:1990`, `:2002` |
| Magic's agent-request handlers | `session.go:2417`, `:2616`, `:2650`, `:2754-2775`; `elicitation.go:31`; `extensions.go:82` |
| `RequestId` | v1.30.0 `types_gen.go:11753`; `elicitation_test.go:333` |
| Middleware not shipped | SDK `docs/decisions/0046-MADR-sending-and-receiving-middleware.md`, `status: proposed` |
| P4 | `leftovers/0224v130-*.log`; `leftovers/0224_v130_sum.py` output |
