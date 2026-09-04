---
status: accepted
date: 2026-09-04
decision-makers: maccavelli
consulted: —
informed: —
---

# The turn path loses content and the provider surfaces are a ragged edge — fix delivery correctness first, then close the surface gap per provider

## Context and Problem Statement

MADR 0137 closed with the turn instrumented, all five engines pinned to their
installed versions, and a wire fixture per provider. This record is a second,
wider pass over the same system with a different question: not *why is the
first token slow*, but **what is broken, unwired, or half-built** across the
daemon's surfaces — the phone protocol, the four provider transports, and the
event path that joins them.

The pass was read-only. It drove the installed binary (`mcremote 0.16.3`,
commit `32f8642`), stood up an isolated daemon on the `fake` provider and
exercised **44 protocol methods** against it, queried the live kilo engine's
own OpenAPI document, extracted grok's ACP extension dispatch table from
upstream source, and read the operator's 34 stored sessions and 6,455 lines of
daemon log. No provider quota was spent.

### The baseline is healthy, which is why the findings below are the ones that matter

`go build ./...`, `go vet ./...` and `go test ./...` are all clean over the
whole module. Every protocol method answered in **0 ms** against the isolated
daemon. Relay splice uses pooled buffers on both sides. Event fan-out marshals
each envelope **once** and reuses the buffer per client, and a client that
cannot keep up is disconnected rather than allowed to back-pressure the session
pump. `p.Start` runs **outside** the manager lock, so a 120-second session
create cannot stall another session's event pump.

So this is not a record about a daemon that is slow. It is a record about a
daemon that **drops the user's own words out of the transcript**, **truncates
command output**, and **consumes about one seventieth of what its largest
provider offers**.

### F1 — the transcript ring is content-blind, and provider telemetry evicts the conversation

The durable transcript is a fixed **800-event ring**, trimmed in batches to 600
(`internal/session/manager.go:39`, `:182`). It counts events. It does not know
which of them a human would call the conversation.

Measured across the operator's 34 stored sessions:

| session | provider | evicted | retained | telemetry share of retained | `user_message` retained |
| --- | --- | --- | --- | --- | --- |
| 20e9170b | grok | **3,618** | 799 | 22% | 1 |
| 32da1cc5 | grok | 2,412 | 773 | 31% | 3 |
| 5e360a4e | codex | 2,814 | 723 | **96%** | 1 |
| a787cbb9 | grok | 402 | 606 | 61% | **0** |
| c8ede651 | grok | 201 | 657 | 29% | **0** |
| dbc29fc3 | codex | 1,809 | 716 | 40% | 2 |

"Telemetry" here is `available_commands` + `tool_call_update` + `notice`.

Two sessions retain **zero `user_message` events**. Their history begins at
`seq` 403 and 202 respectively — the prompts were produced, ringed out, and are
gone. A phone scrolling back sees an agent answering nothing.

`5e360a4e` is the sharpest case: **695 of its 723 retained events are
`tool_call_update` from a single `exec` tool id**, each carrying one line of a
`flutter test` run's stdout. One long-running command evicted 2,814 events,
including the conversation that asked for it.

This is not the `available_commands` finding from 0137 F2 — that one is fixed,
and its deduper (`internal/event/dedupe.go`) covers acpagent, acphttp, kilo and
opencode. `tool_call_update` has no equivalent and is the larger source.

The write itself is bounded and cheap: measured on the operator's real files,
a full marshal-plus-atomic-write of the 1.6 MB history costs **13.2 ms**
(5.7 ms marshal, 7.5 ms write), forced at most every 5 s by `historyMaxLatency`.
The cost is the truncation, not the I/O.

### F2 — codex streams tool output as deltas into a coalescer that supersedes text

`chunkbuf`'s tool lane holds non-terminal `tool_call_update` events per tool id
and folds superseded states away. `mergeTool` carries `ToolName`, `ToolKind`
and `Status` forward, and for `Text` keeps the **newer** value unless it is
empty (`internal/chunkbuf/chunkbuf.go:284-298`). The package's own test pins
that contract: two updates carrying `"line 1"` then `"line 2"` drain as a
single event whose text is `"line 2"`
(`chunkbuf_test.go:432`, `TestToolLaneHoldsNonTerminalUpdates`).

That is correct for a provider whose update means *here is the current text*.
It is wrong for one whose update means *here is the next chunk*.

Codex emits the second shape, at two sites, and both set `Text` to a delta:

* `internal/provider/codex/notifications.go:65` — `item/fileChange/outputDelta`
* `internal/provider/codex/session.go:1583` — `item/commandExecution/outputDelta`

And codex enables the lane: `internal/provider/codex/session.go:2399` calls
`chunkbuf.New(..., chunkbuf.WithToolLane())`. So **any two output deltas from
one codex tool that arrive inside the 80 ms coalesce window collapse to the
later one, and the earlier line of command output is discarded** — never
delivered to the phone, never written to history.

The lane's own doc comment states the guard that was not applied: *"Opt-in
because the cost it removes was measured on OpenCode's HTTP transport;
providers whose tool emission has not been profiled keep the old
pass-through."* Codex was opted in without its emission shape being checked.

The other three lane users are safe, and were verified rather than assumed:

| provider | `Text` on `tool_call_update` | semantics | lane |
| --- | --- | --- | --- |
| kilo / opencode | snapshot, deduped by `noteToolEmit` (`kilo/session.go:760`) | replace | on — correct |
| goose | `summarizeTCContent(tu.Content, …)` (`acphttp/session.go:1247`) | replace | on — correct |
| codex | `p.Delta` | **append** | on — **wrong** |
| grok | `summarizeToolContent(tu.Content, …)` (`acpagent/session.go:1480`) | replace | **off** |

### F3 — grok is the one provider whose tool updates are safe to coalesce, and it is the one that does not

The mirror of F2. `acpagent` builds its buffer without the option
(`internal/provider/acpagent/session.go:1208`), so every grok
`tool_call_update` crosses the WebSocket as its own blocking control frame and
is appended to history. grok's updates carry full ACP content, so the lane
would be correct there — and F1's table shows grok sessions are exactly the
ones losing their prompts to event volume.

### F4 — `permission.respond` runs a 10-second blocking HTTP call on the WebSocket read loop

`internal/ws/server.go` handles nine message types **inline** on the read loop
rather than through `dispatchAsync`: `auth`, `pair.claim`,
`session.pending_asks`, `oauth.cancel`, `permission.respond`,
`permission.receipt`, `receipts.list`, `devices.list`, `question.respond`.
MADR 0137 F4 named this class and Phase 4 moved the worst of the list
(`session.list`, `session.set_mode`, `session.set_config_option`,
`session.cancel`) onto the async path. `permission.respond` was not on that
list, and it is the one that leaves the process.

`handlePermissionRespond` (`:1496`) calls `Manager.RespondPermission` (`:1940`)
which calls the provider session's `RespondPermission`. On kilo and opencode
that is a synchronous HTTP POST to the engine under a **10-second timeout**
(`internal/provider/httpagent/session.go:1232`).

So answering a permission prompt can block the connection's read loop for ten
seconds — and the moment a permission is pending is precisely when the engine
is most likely to be unresponsive. Every later message from that phone queues
behind it, including `session.cancel` and the next `session.prompt`. The
120-second read deadline means the connection survives; it just stops
responding.

### F5 — a stalled session pump can make the ACP SDK tear down the grok connection

Stated as a bounded structural risk, **not** as an observed defect: no instance
appears in the log, and the pump is in-memory on every path examined.

The chain is nonetheless real and reads end to end in source:

1. The ACP SDK queues notifications for **sequential** processing in a channel
   of **1024** (`acp-go-sdk@v0.13.5/connection.go:19`, `:108`).
2. On overflow it does not drop — it **closes the connection**
   (`:446`, `shutdownReceive(errNotificationQueueOverflow)`).
3. mcremote's handler runs on that single consumer, and for a control event it
   **blocks** on a 256-deep channel until the manager pump takes it or the
   session ends (`internal/provider/acpagent/session.go:1337-1342`).

grok emits **247 frames for the word `hi`** (0137, seventh amendment). The
budget between "the pump stops for a moment" and "the grok engine connection is
destroyed" is therefore about four trivial turns' worth of backlog, and much
less under a real one.

### F6 — `session.shell` can hold an eighth of a connection's async budget for 30 minutes

`asyncOpTimeout` gives `session.shell` a **30-minute** deadline
(`internal/ws/server.go:1038`, mirrored in
`internal/protocol/op_timeouts.json`), and `dispatchAsync` bounds every async
op at `maxAsyncPerClient = 8` (`:181`) from one shared pool. Eight concurrent
shells exhaust it, and every subsequent operation on that connection — prompts
and cancels included — is answered `rate_limited`. The exposure is small today
because `allow_remote_shell` defaults to false, but a 30-minute op and a
60-second prompt should not draw on the same eight slots.

### F7 — mcremote calls one of grok's seventy extension methods

grok's ACP `ext_method` dispatch table
(`~/gitrepos/grok-build/crates/codegen/xai-grok-shell/src/agent/mvp_agent/acp_agent.rs:2268`
onward) routes **70** `x.ai/*` request methods. mcremote references twelve
`x.ai/*` strings, and only **one** of them — `x.ai/session/fork` — is a request
it issues. The rest are notifications it receives.

MADR 0137 step 7.7 recorded a deliberate decline for seven grok
*notifications*. That analysis was drawn from one captured turn and therefore
never saw the request surface at all. What is unwired maps almost one-to-one
onto optional `provider.*Session` interfaces grok does not implement:

| mcremote capability grok lacks | grok method that would provide it |
| --- | --- |
| `CompactSession` | `x.ai/compact_conversation` |
| `RenameSession` | `x.ai/session/rename` |
| `PurgeSession` | `x.ai/session/delete`, `x.ai/session/close` |
| `AgentSessionLister` | `x.ai/sessions/list`, `x.ai/session/info` |
| `RevertSession` / `UndoSession` | `x.ai/rewind/points`, `x.ai/rewind/execute`, `x.ai/restore_code` |
| `SkillRefreshSession` | `x.ai/skills/list`, `x.ai/skills/refresh-baseline` |
| `CommandCatalog` | `x.ai/commands/list` |
| `WorkspaceSession` | `x.ai/git/worktree/*`, `x.ai/workspaces/list` |
| `ShareSession` | `x.ai/share_session` |
| `ModelCatalogSession` | `x.ai/models/list` |
| usage / quota reporting (F9) | `x.ai/session/usage`, `x.ai/billing` |

### F8 — the capability matrix is ragged, and the protocol has one vendor-shaped bulge

`internal/provider/provider.go` declares **40 optional session interfaces**.
Implementation across the five transports, computed mechanically from the
method sets:

| transport | interfaces implemented (of 40) |
| --- | --- |
| codex | 26 |
| httpagent (kilo, opencode) | 24 |
| acpagent (grok) | 12 |
| acphttp (goose) | 10 |

Codex is not merely ahead on interfaces. It has **15 dedicated protocol
methods** in the `codex.*` namespace and its own negotiated surface
(`codex_surface_version` in `AuthPayload`,
`CodexSurfaceCaps` in `Caps` — `internal/protocol/messages.go:201`, `:219`).
Nothing equivalent exists for the other four, so every future vendor feature
either bends into the generic session vocabulary or grows a second bespoke
namespace. Probing the isolated daemon confirms the gate is live and
per-connection: all four `codex.*` methods answered
`permission_denied: Codex surface version 1 was not negotiated`.

`ModelReporter` is the concrete cost of the raggedness. Of 15 live turn-latency
records in the operator's log, **all 7 grok turns and 3 of 4 codex turns carry
no `model=` field**, while grok's own `initialize` result hands over
`_meta.modelState.currentModelId` — present in the checked-in fixture
(`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`) and unread.

### F9 — quota state is recovered by scraping English prose, from engines that report it structurally

`internal/agenterr/agenterr.go` is **967 lines** of regular expressions and
substring matching over engine log lines — `reBackoffSecs`, `reRetryDelaySome`,
`reMonthDay`, `reClock`, `reCompound`, `reRustDebugString` — reconstructing
rate-limit and quota state from vendor prose. `acphttp/provider.go:694` tails
engine stderr and feeds it in; the operator's log carries **45 "engine provider
limit detected"** lines produced this way, matching text like
`GoUsageLimitError`, `Monthly usage limit reached. Resets in 4 days` and
`Credits exhausted`.

Two of those engines answer the question directly. Verified live against the
running kilo 7.5.6 engine, read-only:

```text
GET /kilocode/provider-usage  -> {"items":[],"generatedAt":"2026-09-04T02:58:47.320Z"}
   ("Get cache-aware, secret-free provider plan usage and personal billing status")
GET /experimental/capabilities -> {"backgroundSubagents":true}
```

grok exposes `x.ai/billing` and `x.ai/limit` on the same dispatch table as F7.
Every vendor wording change is currently a silent regression in a code path
whose failure mode is "the turn hangs instead of reporting a quota wall".

### F10 — kilo 7.5.6 publishes 264 endpoints and mcremote reads a handful

The engine serves its own OpenAPI document at `/doc`; it lists **264 paths**.
Beyond F9's two, four are directly useful and unread:

* `GET /session/{id}/model-usage` — token usage and direct cost by model for
  the whole session tree.
* `GET /api/session/{id}/context` — the active context messages since the last
  compaction.
* `POST /api/session/{id}/wait` — wait for the agent loop to become idle; the
  turn completion mcremote currently infers.
* `GET /experimental/capabilities` — feature discovery, in place of hardcoded
  assumptions.

**0137's `/sync` decision was re-checked and stands.** `/sync/history`'s
contract still reads *"Aggregates not listed in the input get their full
history"*, and `/sync/replay` takes a client-supplied event array rather than
offering a scoped fetch. Declining seq-based resume remains right; only gap
detection was ever available.

### F11 — the pair QR advertises an address the daemon does not listen on

Reproduced, not inferred. With `listen.host: 127.0.0.1` and
`pair.advertise_host` empty, `mcremote pair create` printed:

```text
Host:   100.64.0.3:7642
WS URL: ws://100.64.0.3:7642/v1/ws
```

while the process listened only on loopback:

```text
$ curl -m3 http://100.64.0.3:7642/healthz ; echo $?
7                                    # connection refused
$ curl -m3 http://127.0.0.1:7642/healthz
{"ok":true}
$ lsof -nP -iTCP:7642 -sTCP:LISTEN
mcremote  17885  saxsmith  6u  IPv4  TCP 127.0.0.1:7642 (LISTEN)
```

`detectAdvertiseHost` (`internal/cli/pair.go:499`) prefers the Tailscale IPv4
and never consults `cfg.Listen.Host`. A phone that scans that QR cannot
connect, and the failure surfaces as a connection timeout with no explanation.

### F12 — a per-session warning that has never been true on this host

`internal/provider/acpagent/acpagent.go:584` logs *"agent advertises auth
methods but none configured; session/new may fail"* whenever the agent
advertises auth methods and `auth_method_id` is empty. It has fired **90
times**. grok is authenticated from its own credential store and `session/new`
has never failed for this reason. This is the same class as 0137 F6: a warning
the operator is trained to ignore is how the next real warning gets missed —
and 0137 documented that happening twice.

### What the live instrumentation now shows, and what it does not

The 0137 Phase 2 record is working and produced 15 turns on this host:

| provider | turns | ttft p50 | ttft max | records missing `model` |
| --- | --- | --- | --- | --- |
| grok | 7 | **1.11 s** | 2.32 s | **7 of 7** |
| codex | 4 | 1.45 s | 1.69 s | 3 of 4 |
| kilo | 4 | **5.20 s** | **17.62 s** | 0 of 4 |

Three things follow, and the third is the uncomfortable one:

* **grok's reported 5–18 s regression is not reproducing.** Every grok turn on
  the current binary answered in about a second.
* **kilo is now the slow provider**, and its worst turn — 17.6 s to first token
  — reported `cache_read=2219` against an 18,629-token context, consistent with
  0137's finding that `kilo-auto/balanced` routes across backends with separate
  caches.
* **`cold` was `false` on all 14 records that carried it.** As defined
  (`CacheRead == 0`) the flag can now only be true on an engine's very first
  turn, so it no longer separates the population it was built to separate. One
  grok record carries `turn_ms=23972` and no `ttft_ms` and no token fields at
  all, and nothing distinguishes "this turn produced no output" from "this turn
  was not measured".

## Amendment, 2026-09-03: F1 becomes a RAM-backed retention decision, and token cost enters scope

The owner sharpened F1 and widened the record:

> "I want to leverage more RAM memory for F1. We have way too small a ring
> buffer is my thinking, and session streaming should flow into a RAM-backed
> cache and buffer to the chat session screen to prevent context loss.
> mcremote is a small daemon. It would not hurt to leverage RAM. Really dig
> deep into context loss, and also token savings. Both are bad."

The instinct is right and the headroom is real: **the daemon's RSS is 51.4 MB
on a 32 GB host, while the single kilo engine it manages is 1,450 MB.** The
daemon is 3.5% of one of its own children. Nothing about 800 events was ever a
memory decision.

What follows is the measurement pass that turns "use more RAM" into a
deterministic design. It found that raising the cap alone would make things
*worse*, and it found a second loss stage the original F1 did not name.

### F13 — context is lost in four stages, not one, and the last one is the phone

| # | stage | retains | mechanism |
| --- | --- | --- | --- |
| 1 | provider emits | everything | — |
| 2 | daemon ring | last **600–800 events** | `appendHistoryLocked`, count-based FIFO (`manager.go:222`) |
| 3 | durable file | the same 800 | `SaveHistory` writes the ring (`store.go:278`) |
| 4 | **phone transcript** | last **800 chat items** | `_enforceCap`, FIFO from the front (`transcript_reducer.dart:873`) |
| 5 | phone disk cache | last **150 items** | `kTranscriptCacheMaxItems` (`chat_models.dart:71`) |

Stage 4 is new to this record and it is load-bearing: **even a perfect host
ring is truncated again on the phone.** The two caps are deliberately coupled —
`historyBufferCap`'s own comment reads *"Aligned with the mobile client's
kMaxTranscriptItems (800) so cold reopen can rebuild a full phone-side
transcript"* (`manager.go:36-39`). They must therefore move together, and the
protocol already has the mechanism: `Caps.HistoryRing` advertises
`HistoryRingCap` to every v2 client (`manager.go:41-43`,
`protocol/messages.go:242`), so the phone can read the size instead of
hardcoding it.

### F14 — one `session.history` request costs 709 MB of allocation and 2.5 seconds today

`HistoryPage`'s byte-budget loop marshals `ring[start:end]`, and on overflow
decrements `end` by **one** and marshals the whole page again
(`manager.go:1484-1494`). It is O(limit²) in JSON bytes.

The phone requests `limit: 800` — `kHistoryFetchLimit`
(`chat_models.dart:69`, `mcremote_client.dart:3466`) — which is the worst case.
Measured by replaying the loop verbatim against the operator's real
`history.json` files:

| session | ring | limit | marshals | JSON produced | allocated | elapsed |
| --- | --- | --- | --- | --- | --- | --- |
| a787cbb9 | 606 | **800** | **505** | **635 MB** | **709 MB** | **2,516 ms** |
| a787cbb9 | 606 | 200 | 99 | 74.7 MB | 78.3 MB | 306 ms |
| 1efee1da | 629 | **800** | **379** | **280 MB** | 286 MB | **1,114 ms** |
| 1efee1da | 629 | 200 | 1 | 0.43 MB | 2.0 MB | 1.8 ms |

**This runs every time the phone opens one of those sessions**, to return 102
and 251 events respectively. It is the most expensive single operation in the
daemon and it exists to enforce a 512 KB frame cap that a running byte total
would enforce in one pass.

Measured honestly, and against my own first assumption: the cost is bounded by
`limit`, **not** by ring size — replaying the same loop against synthetic
2,424- and 7,272-event rings produced identical figures. Growing the ring does
not worsen F14. It must still be fixed, because a client may legitimately ask
for a larger page once the ring is larger, and because 709 MB of garbage per
transcript open is the jank.

### F15 — the replay dedupe is genuinely quadratic, and it *is* the blocker to a bigger ring

`appendHistoryLocked` scans the **entire** ring for every event carrying
`Replay` (`manager.go:201-207`), comparing `Type`, `Text`, `ToolID` and
`AgentSessionID`. ACP `session/load` replays a whole conversation as ordinary
updates into an empty ring, so no comparison short-circuits.

| events replayed | ring cap | comparisons | elapsed |
| --- | --- | --- | --- |
| 799 | 800 | 295,580 | 0.6 ms |
| 3,196 | 3,197 | 4,753,850 | 13.5 ms |
| 9,588 | 9,589 | 42,833,790 | 92.1 ms |
| 19,975 | 19,976 | **185,966,000** | **352 ms** |

At 50,000 events this is roughly 2.2 seconds of CPU, executed on the **session
pump goroutine** — the same goroutine whose stalling is the mechanism behind
F5. Raising the cap without replacing this scan with a hash index converts a
latent risk into a reproducible one.

*This table is the corrected one.* The first measurement I took built its
larger inputs by concatenating a session's events, so every event after the
first pass matched at index 0 and the scan short-circuited — it reported linear
growth. The instrument agreed with the comfortable answer, which this
repository's predecessor record says is exactly when to re-check it. Re-run
with distinct events, the curve is quadratic.

### F16 — 44% of transcript bytes are one duplicated event type, and only 12% is conversation

Byte cost by event type across all 7,004 events in the operator's 34 stored
sessions:

| type | count | total | mean | share of bytes |
| --- | --- | --- | --- | --- |
| `available_commands` | 486 | 2.49 MB | **5,120 B** | **44.1%** |
| `tool_call_update` | 1,894 | 1.63 MB | 861 B | 28.9% |
| `tool_call` | 1,117 | 0.43 MB | 383 B | 7.6% |
| `assistant_message_chunk` | 1,717 | 0.37 MB | 216 B | 6.6% |
| `thought_chunk` | 968 | 0.25 MB | 261 B | 4.5% |
| `remote_commands` | 53 | 0.14 MB | 2,733 B | 2.6% |
| `session_config` | 29 | 0.13 MB | 4,652 B | 2.4% |
| everything else | 740 | 0.22 MB | — | 3.3% |

Overall mean: **806 B/event**. The actual conversation —
`user_message` + `assistant_message_chunk` + `thought_chunk` + `turn_complete`
— is **11.7% of stored bytes**.

So RAM is not the whole answer. **Two event types account for 73% of the
budget**, one of them pure duplication (0137 F2, now deduped at emit for four
providers but not retro-fixed in retention) and the other streaming increments
(F2/F3 of this record). Spending the same bytes on content instead of telemetry
is worth roughly **4×** on top of whatever the cap becomes.

`event.Event` is also a **44-field struct** whose header alone is ~720 bytes
before any string content, and it is stored **by value** in the ring and copied
by value on every `History()`/`historyRing()` call. A per-event budget must
count the header, not just the payload.

### Sizing: what "leverage RAM" means numerically

Using the measured 806 B/event mean plus the 720 B struct header — call it
**1.5 KB/event typical, 3 KB/event for a tool-heavy session**:

| retention | per session | × `max_live_sessions: 16` |
| --- | --- | --- |
| 800 events (today) | 1.2 MB | 19 MB |
| 20,000 events | 30 MB | 480 MB |
| 100,000 events | 150 MB | **2.4 GB** |

A uniform event count is the wrong unit, because event sizes vary by more than
100× (216 B for an assistant chunk, 5,120 B for an `available_commands`). **The
budget must be bytes, enforced per session and globally**, or one chatty
provider's telemetry sets the retention for every session on the host.

### F17 — history paging is forward-only, and a chat screen opens at the bottom

`SessionHistoryPayload` offers only `SinceSeq` (exclusive lower bound) and
`Limit` (`protocol/messages.go:386-392`). There is no `before_seq` and no
newest-first mode. `LatestSeq` is returned, but only *after* a request that
already started from the oldest event.

The phone therefore walks the whole ring forward, `limit: 800` per page, up to
32 pages, and shows nothing until the walk finishes
(`mcremote_client.dart:3474-3524`, whose own comment reads *"Safety bound: ring
is ≤800"*). It then discards all but the newest 800 items at stage 4.

At 800 events that is one or two round trips. At 20,000 it is 25 round trips
and 20,000 events transferred to render the last 800. **Forward-only paging is
the protocol blocker to a larger ring**, and it must be fixed in the same work,
not after it.

### The token half: what mcremote actually controls

Two things were checked first, because they bound everything else.

**mcremote adds nothing to the prompt.** Read at every provider's `Prompt`:
kilo/opencode send `{"parts":[{"type":"text","text":…}]}` plus an optional
model and agent name (`kilo/session.go:218-240`); grok sends the user's content
blocks (`acpagent/session.go:258`). No preamble, no injected context. The
13,637–27,474 tokens 0137 measured for `hi` are the engine's own system prompt,
MCP tool definitions and plugins.

**The history ring costs zero tokens.** It is populated from events the engine
already emitted, is served only to the phone, and is never sent back to a
model. Growing it is a pure RAM-for-content trade with no token consequence
whatsoever. That is the property that makes the owner's proposal safe.

What mcremote *does* control is measured below, from the 15 live turn records
MADR 0137 Phase 2 produced on this host:

| provider | session | ctx used | input tok | cache read | uncached |
| --- | --- | --- | --- | --- | --- |
| grok | 1b3742ba | **1,526,598** | 1,512,266 | 1,235,456 | **276,810** |
| grok | 84b277cd | 275,939 | 272,937 | 216,704 | 56,233 |
| codex | 7972ea3c | 84,605 | 84,291 | 77,056 | 7,235 |
| kilo | ecbadf80 | 18,629 | 16,409 | 2,219 | 14,190 |
| kilo | 10fe2896 (turn 1) | 15,545 | 13,317 | 2,227 | 11,090 |
| kilo | 10fe2896 (turn 2) | 15,545 | 13,317 | 2,227 | **11,090** |

**Across all 15 turns: 2,157,605 input tokens, 1,709,293 cached, 448,312
uncached (21%).**

**T1 — context grows without bound and nothing says so.** Session 1b3742ba went
from 99,385 to **1,526,598** tokens of context in two turns. Session 84b277cd
went 23,089 → 275,939. `context_used` is computed
(`session/turnlatency.go:117`) and written to a log line. It is **not** compared
to a threshold, **not** pushed to the phone, and **not** used to suggest
compaction. `/compact` and `/context` exist as manual commands
(`command/specs.go:62`, `:77`) that the user must think to run. Nothing in the
daemon has ever told anyone their session grew 15×.

**T2 — kilo re-paid an identical uncached prefill on consecutive turns.**
Session 10fe2896's two turns report byte-identical accounting: 13,317 input
against 2,227 cached, twice. 0137 attributed the mechanism —
`kilo-auto/balanced` routes across backends with separate caches — but the
daemon has never *reported* it, so an operator has no way to see that a
provider setting is costing them a second full prefill per turn.

**T3 — a third of turns are first turns.** The operator's store holds **93
`user_message` events across 34 sessions — 2.7 turns per session**. Each
session's first turn pays a full uncached prefill, measured by 0137 at
13,637–27,474 tokens and $0.042–$0.085. Session reuse is a token lever mcremote
owns and does not currently surface, measure, or encourage.

None of T1–T3 is fixed by changing a model, which MADR 0137's third amendment
forecloses and this record leaves foreclosed. All three are fixed by
**reporting**: the daemon has the numbers and keeps them to itself.

### Decision: a byte-budgeted, content-classed RAM cache, with the quadratics fixed first

F1's remediation is restated. In order, because each step is a precondition for
the next:

1. **Replace the two quadratics** (F15's replay scan with a hash index, F14's
   shrink loop with a single running-byte pass). Nothing may grow before this.
2. **Re-unit retention from events to bytes**, with a per-session and a global
   budget, and a **content class** per event type so telemetry is evicted
   before conversation. `user_message` and turn boundaries are never evicted
   while any evictable event remains.
3. **Add newest-first paging** (`before_seq`, or an explicit newest-first
   mode), because a chat screen opens at the bottom and F17 makes that
   impossible today.
4. **Raise both caps together** — host budget and the phone's
   `kMaxTranscriptItems` — with the phone reading `Caps.HistoryRing` rather
   than hardcoding, and set `GOMEMLIMIT` so a budget bug degrades into GC
   pressure rather than an OOM on the operator's laptop.
5. **Report token cost** (T1–T3): a context-pressure event when a session
   crosses a fraction of its window, the uncached share per turn on the phone,
   and per-session cumulative totals. Report only — no automatic compaction and
   no model substitution without the owner's word.

* Good, because it spends RAM where the daemon has three orders of magnitude of
  headroom, on the one thing users lose today, at zero token cost.
* Good, because content classes multiply the benefit ~4× on top of the raised
  budget, so a smaller budget buys more conversation than a larger naive one.
* Good, because F14 and F15 are live defects that are worth fixing on their own
  merits and would otherwise be amplified by the change.
* Neutral, because it makes the protocol and the Flutter client part of the
  work. They were already coupled by a shared constant; this makes the coupling
  explicit and negotiated.
* Bad, because a byte budget with content classes is more machinery than a
  larger number, and the eviction policy becomes a thing that can itself be
  wrong. Mitigated by pinning the policy with tests that assert what survives,
  not only how much.
* Bad, because raising the phone's cap raises phone memory too, on hardware
  with far less headroom than the host. The phone's budget is therefore set
  independently and lower, not copied from the host's.

## Amendment, 2026-09-04: F5 is observed, and the first fix for it did not work

F5 above was recorded as "a bounded structural risk, **not** an observed
defect", because no instance appeared in the log and the pump was in-memory on
every path examined. Phase 7 responded with a 30-second bound on the control
send and left its fail-first check (G2) unrun.

G2 has since been written and run against a real `acp.ClientSideConnection`.
Two things follow, and both correct this record.

**The mechanism is now observed, not inferred.** With the client's notification
handler blocked, the SDK tears the connection down in **7.16 ms** — measured, by
feeding 1,224 `session/update` frames into a stalled session. F5's chain is
real and fast.

**The bound Phase 7 shipped did not protect against it.** 30,000 ms against a
7.16 ms fill is not a race that can be won; the guard read as protection and
provided none. Isolating the timeout confirms it is exactly that race: at 1 µs
the connection survives all 1,224 frames, at 50 ms and at 30 s it dies.

The replacement removes the race rather than narrowing it. The control path
never blocks: an event that cannot be handed over immediately is parked in a
bounded per-session queue and a dedicated drainer does the waiting, so the SDK's
consumer always returns in O(1). Ordering is preserved by keeping the in-flight
event at the head of that queue until its send completes. A full queue faults
the session loudly rather than dropping the event.

*Why this belongs in the record and not only in the plan:* F5's classification
was wrong in both directions. It was more real than "unobserved" implied, and
the fix for it was less effective than "bounded" implied — and the second error
followed from the first, because a risk believed hypothetical got a guard that
was never driven. The implementation detail is in
[the plan's amendment](0138-PLAN-overhaul-provider-surfaces-and-turn-path.md);
what belongs here is that **reading a dependency establishes its mechanism, not
the adequacy of your response to it**.

## Decision Drivers

* Losing the user's own messages from the transcript is a correctness failure,
  and it outranks every performance item in this record.
* Silently discarding command output is worse than delivering it slowly: the
  phone cannot tell that anything is missing.
* A coalescing optimisation is only valid for the emission shape it was
  measured against; applying it to a different shape is a data-loss bug, not a
  tuning error.
* Work that leaves the daemon process must not run on the connection's read
  loop, whatever else is true about it.
* The five providers do not share a transport, so surface work is stated and
  delivered per provider or it is not delivered at all.
* A capability the engine already reports must not be reconstructed by parsing
  its log prose.
* Evidence before change. This record's predecessor was corrected twice for
  reasoning from an unverified instrument; every claim above names the file,
  the command, or the measurement that produced it, and F5 is explicitly
  labelled unobserved.

## Considered Options

* **Fix the delivery-correctness defects first, then close the provider surface
  gap per provider** — F1–F4 and F11 as a correctness phase, F7–F10 as a
  per-provider compatibility phase, F5/F6/F12 as bounded hardening.
* **Close the provider surface gap first** — implement grok's missing session
  interfaces and the unread kilo endpoints, and treat the event-path defects as
  follow-up work.
* **Rewrite the event path around a content-aware transcript** — replace the
  800-event ring with a typed retention model and re-derive coalescing from it,
  before fixing anything at the edges.
* **Fix only F2 and F4 and stop** — take the two provable data-loss and
  head-of-line defects, and accept the rest as known debt.

## Decision Outcome

Chosen option: "Fix the delivery-correctness defects first, then close the
provider surface gap per provider", because F1 and F2 destroy user content that
nothing downstream can reconstruct, while every surface item in F7–F10 is
capability the daemon has never had and whose absence no one has lost work to.
An overhaul that added grok's rewind support while still dropping the operator's
prompts out of history would be the wrong order.

Concretely, in priority order:

1. **Stop evicting the conversation (F1).** Make retention content-aware —
   `user_message`, `assistant_message_chunk`, `turn_complete` and permission
   decisions must outlive `tool_call_update`, `available_commands` and
   `notice`. The 800-event count budget is the wrong unit for a transcript.
   **Superseded in detail by the amendment above**, which adds the RAM budget,
   the four-stage loss chain (F13), the two quadratics that must be fixed first
   (F14, F15), the byte-cost evidence (F16), the paging blocker (F17) and the
   token-reporting scope (T1–T3). The amendment's five-step ordering is
   authoritative for F1; this line records what was decided before it.
2. **Fix codex's tool-output loss (F2), and give grok the lane (F3).** Codex
   needs append semantics — concatenate deltas under the tool id rather than
   supersede — or the lane off until it does. grok's updates are replace-shaped
   and should be coalesced.
3. **Move `permission.respond` and `question.respond` off the read loop (F4),**
   and audit the remaining seven inline handlers for anything else that leaves
   the process.
4. **Fix the pair QR (F11)** so a non-tailnet bind advertises the address it
   actually listens on, or refuses to guess.
5. **Bound the two structural risks (F5, F6):** the ACP notification path must
   not be able to kill an engine connection through a blocking send, and a
   30-minute op must not draw on the prompt's eight-slot budget.
6. **Close the surface gap per provider (F7–F10),** grok first because it is
   the furthest behind, starting with the interfaces mcremote already defines
   and the phone already has UI for. `x.ai/session/usage`, `x.ai/billing` and
   kilo's `/kilocode/provider-usage` retire the prose scraping in F9;
   `_meta.modelState.currentModelId` closes `ModelReporter` for grok.
7. **Repair the two signals (F12, and `cold`),** because a warning nobody reads
   and a flag that cannot vary are both worse than absent.

Nothing in this record pins or substitutes a model. MADR 0137's third amendment
— providers run on their own default model unless the user asks otherwise —
binds this work unchanged.

### Consequences

* Good, because the two defects that destroy content (F1, F2) are fixed before
  any capability is added, and both are provable from the operator's own data
  rather than from a hypothesis.
* Good, because F3, F4, F11 and F12 are small, independent, and testable
  without provider quota.
* Good, because the surface work is stated per provider and per interface, so
  it can be delivered incrementally and stopped at any point without leaving
  the daemon inconsistent.
* Neutral, because F5 is carried as a bounded risk with its mechanism recorded
  rather than as a claimed defect. If it is never observed, the hardening is
  cheap; if it is, the analysis is already written.
* Bad, because it does not make kilo's 17.6-second turn faster. That number is
  the auto-router's cache locality, which 0137 already attributed upstream and
  which this record's constraint forbids working around by pinning a model. The
  honest deliverable there is reporting, not speed.
* Bad, because content-aware retention is a change to a durable on-disk format
  that existing session files were written under, and old truncated histories
  cannot be recovered by it.

### Confirmation

* F1: a session whose turn produces more than 800 events retains every
  `user_message` in `history.json`, verified against a rerun of the survey that
  produced the eviction table above (6 of 34 sessions truncated, 2 with zero
  prompts retained).
* F2: a codex tool emitting two output deltas inside one coalesce window
  delivers both lines. Pinned by a test that fails against the shipped
  `mergeTool`, and by re-reading a captured `item/commandExecution/outputDelta`
  pair from the codex fixture.
* F3: a grok session's `tool_call_update` count for one turn drops measurably
  against the 0137 full-turn fixture, with no update lost.
* F4: `permission.respond` appears in the async dispatch list and in
  `op_timeouts.json`, and a permission answered against a deliberately stalled
  engine no longer delays a concurrent `session.cancel` on the same connection.
* F5: the acpagent notification path cannot block indefinitely; demonstrated by
  a test that fills the session channel and asserts the ACP connection survives.
* F6: `session.shell` no longer consumes a slot from the same pool as
  `session.prompt`, shown by eight concurrent shells followed by a prompt that
  is accepted.
* F7–F10: each newly consumed surface is exercised against the installed engine
  version and pinned by a live-tagged test, per this repository's rule that an
  assumption about external CLI behaviour without a test is a future bug report.
* F11: `mcremote pair create` against a loopback-bound daemon advertises
  `127.0.0.1`, verified by the same `curl`/`lsof` pair that reproduced the bug.
* F12: a grok session start on this host emits no auth-method warning.

## Pros and Cons of the Options

### Fix the delivery-correctness defects first, then close the provider surface gap per provider

* Good, because it orders the work by what is being lost: content first,
  responsiveness second, capability third.
* Good, because the correctness phase is self-contained and verifiable without
  spending provider quota.
* Good, because it leaves the surface work free to be scoped per provider,
  which is the only shape that survives five different transports.
* Neutral, because it is two phases rather than one, and the operator sees no
  new capability until the second.
* Bad, because F1's fix changes a durable format and cannot repair histories
  already truncated.

### Close the provider surface gap first

* Good, because it is the most visible improvement: grok gains compact, rename,
  rewind, skills and worktree support the phone already has affordances for.
* Good, because it directly answers the brief's "compatibility with the
  providers".
* Bad, because it adds events and capability to an event path that is currently
  dropping the user's messages and truncating command output — more traffic
  through a lossy pipe.
* Bad, because every new surface would be built against the same 800-event
  budget that F1 shows is already exhausted by telemetry.

### Rewrite the event path around a content-aware transcript

* Good, because F1, F2 and F3 are all symptoms of one absence: the event path
  has no model of which events are content and which are telemetry.
* Good, because it would make future coalescing decisions derivable rather than
  per-provider judgement calls.
* Neutral, because much of the machinery already exists — `IsControl`,
  `IsChunk` and `IsInPlaceUpdate` are three partial answers to the same
  question.
* Bad, because it blocks four small provable fixes behind one large refactor of
  the hottest path in the daemon, and this repository has twice been burned by
  large conclusions drawn ahead of small measurements.

### Fix only F2 and F4 and stop

* Good, because those two are the most clearly provable defects and the fixes
  are small.
* Bad, because F1 is the finding with the operator's own data behind it — two
  sessions with no prompts retained — and leaving it is choosing to keep losing
  transcripts.
* Bad, because it leaves the surface gap uncharacterised, which is the part of
  the brief that asked for an overhaul rather than a patch.

## Amendment, 2026-09-04: F7's table conflates two interfaces that grok cannot both satisfy

F7's surface-gap table lists one row as

| `RevertSession` / `UndoSession` | `x.ai/rewind/points`, `x.ai/rewind/execute`, `x.ai/restore_code` |

The slash reads as "either would do". Phase 9 read the grok source and it does
not: **grok can satisfy `UndoSession` and cannot satisfy `RevertSession`.**

`Revert(messageID, partID)` needs a provider-native message id and grok emits
none — its rewind is indexed by prompt position — and `RevertSession` also
requires `Unrevert`, for which grok has no method at all. So the row should read
`UndoSession` only, and `/redo` stays unavailable on grok.

That correction has a consequence the record did not anticipate. Every
`UndoSession` in this repository was also a `RevertSession` — `httpagent`
(kilo, opencode) and `fake` — so `internal/session/commands.go:630` could end
every successful undo with *"/redo restores it."* and always be right. grok is
the first provider for which that sentence is false, and enabling undo on it
made the daemon offer a command it immediately refuses. The notice is now
conditional on the same `RevertSession` assertion `OpRedo` is derived from; the
deviation and its evidence are recorded in the plan.

The wider assumption worth writing down: **an optional-interface table is not a
menu.** Two interfaces grouped by what they do for the user can have entirely
different requirements of the engine, and grouping them hid the fact that
picking one of the two changes daemon behaviour elsewhere.

`x.ai/restore_code` remains unread and unwired; the row's third method is
therefore still a gap, not a decline.

## Amendment, 2026-09-04: F9 names a grok method that does not exist, and misses the one that does

F9 states:

> grok exposes `x.ai/billing` and `x.ai/limit` on the same dispatch table as F7.

Half of that is wrong, and the correction changes what the grok half of F9 is
built from.

### `x.ai/limit` is not a method

grok's ACP extension dispatch (`agent/mvp_agent/acp_agent.rs:2290-2562`) carries
**27** `x.ai/*` request methods. `x.ai/billing` is one of them (`:2544`).
`x.ai/limit` is not there, and does not appear as a method anywhere in the
source.

It appears as a **`_meta` key**. `session/unified_list/mod.rs:181` reads it out
of a request's `_meta` object as a page size, alongside two siblings:

```rust
let limit = meta
    .get("x.ai/limit")
    .and_then(serde_json::Value::as_u64)
    .map(|n| n as usize);
```

and grok's own test fixes the shape — `{"x.ai/facetFilters": …, "x.ai/query":
"antelope", "x.ai/limit": 5}`. It is pagination for session listing. Calling it
as a method returns `method_not_found`.

The mistake is a legible one: `x.ai/limit` reads like a rate-limit query and was
recorded from a name grep rather than from the dispatch table. It is corrected
here rather than quietly dropped, because the plan's scope table was written
from it.

### The method F9 missed

`x.ai/session/usage` (`acp_agent.rs:2329` → `extensions::usage::handle`). It is
the better half of the pair, and the two answer different questions:

| | `x.ai/billing` | `x.ai/session/usage` |
| --- | --- | --- |
| scope | the authenticated account | one session |
| source | HTTP to the CLI chat proxy → backend `GetGrokCreditsConfig` | the in-process `UsageLedger` |
| auth | **requires grok.com auth**; fails on an API-key install | none |
| cost | a network round trip, 15 s upstream timeout | in-memory |
| answers | credits, plan tier, on-demand cap, billing period | tokens, cost, turns, per-model split, folded subagent spend |
| lifetime | account-lifetime | resets when a session is resumed in a new agent process |

`x.ai/session/usage` is what `RuntimeUsage` should read: it is local, free, needs
no credentials, and it reports **folded subagent spend**, which the per-turn
`turn_completed` notification mcremote already consumes
(`internal/provider/acpagent/xaiusage.go`) does not aggregate.

`x.ai/billing` is what `RuntimeStatus` and F9's quota confirmation should read.
Its auth gate matters: `extensions/billing.rs` calls `require_xai_auth` and
answers *"Billing data requires auth with grok.com. Run `grok login` to
authenticate."* on failure. mcremote lists `xai.api_key` as a safe headless
credential (`internal/provider/grok/grok.go`, `SafeAuthMethodIDs`), so an
API-key install is a supported configuration in which this method never
succeeds. It must degrade to "unavailable", never to an error on `/status`.

### One response, two casing conventions, nested

`BillingConfigResponse` carries **no** `rename_all`, so its own fields are
snake_case — `config`, `on_demand_enabled`, `subscription_tier`. The
`BillingConfig` nested inside it carries `rename_all = "camelCase"` —
`creditUsagePercent`, `currentPeriod`, `monthlyLimit`, `onDemandCap`,
`prepaidBalance`, `isUnifiedBillingUser`, `billingPeriodStart`, `history`. The
`Cent` inside *that* has no rename_all again: `{"val": 20000}`.

Three nesting levels, alternating conventions. Phase 9 already established what
a wrong guess costs here — a struct of zero values and no error — and this
response can get it wrong twice in one decode.

Two further traps the source states outright, both of which produce a plausible
wrong number rather than a failure:

* **A `$0` `Cent` arrives as `{}`.** proto3 JSON omits zero-valued scalars.
* **`costUsdTicks` is 1e10 ticks per dollar**, and is *absent* — not zero — when
  the bill is partial, with `costIsPartial` set to say so. Treating absent as
  zero reports a free turn.

### What this does not change

F9's finding stands: quota state is still reconstructed from English in
`internal/agenterr`, and grok still answers structurally. Only the method names
are corrected. kilo's half shipped in Phase 6.4 and is wired
(`internal/provider/kilo/session.go:640`).

One consequence worth stating plainly, because it is larger than F9: of the five
transports, **only codex implements `RuntimeSession`**. `/status` and `/usage`
are unavailable on grok, kilo, opencode and goose alike. Phase 10 fixes grok
because grok is what F9 named; the other three remain a gap, now recorded rather
than implied.

## More Information

Everything below was produced during this pass on 2026-09-03 against
`mcremote 0.16.3 (32f8642)`, with engines kilo 7.5.6, opencode 1.18.26,
grok 1.0.13, goose 1.48.0 and codex 0.152.1.

* **Baseline:** `go build ./...`, `go vet ./...`, `go test ./...` — all clean.
* **Protocol surface:** an isolated daemon (`127.0.0.1:7642`, `fake` provider,
  own data dir, TLS off) driven over `mcremote.v1` across 44 methods. Every
  method answered in 0 ms. `provider.auth_status` and `providers.prewarm`
  answer `unknown_type` inbound and were verified to be **daemon→phone push
  types** (`internal/ws/server.go:2637`, `:2076`) — correct, not a gap.
* **Transcript evidence:** `~/.local/share/mcremote/sessions/*/history.json`
  and `meta.json`, 34 sessions. Truncation is identified by a first `seq` above
  1. Rewrite cost measured over the real files.
* **F1:** `internal/session/manager.go:39` (`historyBufferCap = 800`), `:182`
  (`historyTrimTo`), `:2845`; `internal/session/store.go:267`.
* **F2/F3:** `internal/chunkbuf/chunkbuf.go:89-122`, `:136-190`, `:284-298`;
  `internal/chunkbuf/chunkbuf_test.go:432`;
  `internal/provider/codex/notifications.go:65`;
  `internal/provider/codex/session.go:1583`, `:2399`;
  `internal/provider/acpagent/session.go:1208`, `:1480`;
  `internal/provider/acphttp/session.go:1247`, `:1523`;
  `internal/provider/httpagent/session.go:905`;
  `internal/provider/kilo/session.go:760`.
* **F4:** `internal/ws/server.go:687`, `:874-882`, `:902`, `:1496`;
  `internal/session/manager.go:1940`;
  `internal/provider/httpagent/session.go:1228-1240`.
* **F5:** `acp-go-sdk@v0.13.5/connection.go:19`, `:108`, `:426-448`, `:495`;
  `internal/provider/acpagent/session.go:1302-1342`. No occurrence of
  `errNotificationQueueOverflow` in 6,455 log lines.
* **F6:** `internal/ws/server.go:181`, `:1017-1046`;
  `internal/protocol/op_timeouts.json`.
* **F7:** `~/gitrepos/grok-build/crates/codegen/xai-grok-shell/src/agent/mvp_agent/acp_agent.rs:2268`
  onward — 70 `x.ai/*` request methods extracted from the `ext_method` match;
  mcremote's twelve `x.ai/*` references extracted from `internal/`.
* **F8:** `internal/provider/provider.go` — 40 optional interfaces, matrix
  computed from method sets per provider package;
  `internal/protocol/messages.go:160-174` (15 `codex.*` methods), `:201`,
  `:219`, `:269`.
* **F9:** `internal/agenterr/agenterr.go` (967 lines);
  `internal/provider/acphttp/provider.go:688-708`; live
  `GET /kilocode/provider-usage` and `GET /experimental/capabilities` on the
  running kilo engine.
* **F10:** kilo's own OpenAPI document at `GET /doc` — 264 paths;
  `/sync/history`, `/sync/replay`, `/sync/start`, `/sync/steal` schemas read
  directly, confirming MADR 0137 step 7.1's decision.
* **F11:** `internal/cli/pair.go:432-500`; reproduced with `curl` and `lsof` as
  quoted.
* **F12:** `internal/provider/acpagent/acpagent.go:583-586`; 90 occurrences in
  `~/Library/Logs/mcremote/mcremote.err.log`.
* **Turn-latency table:** the 15 `msg="turn latency"` records in that log,
  produced by MADR 0137 Phase 2.
* Predecessor:
  [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md)
  and its plan. This record does not supersede it: 0137's decisions stand, and
  its `/sync` and default-model conclusions were re-verified here rather than
  revisited.
* Implementation:
  [0138-PLAN-overhaul-provider-surfaces-and-turn-path.md](0138-PLAN-overhaul-provider-surfaces-and-turn-path.md),
  `status: proposed`. Per `AGENTS.md`, no mutating work begins until the owner
  approves it.
* Evidence added by the amendment, all 2026-09-03 on the same binary and
  engines: daemon RSS via `ps` (51.4 MB) against `hw.memsize` (32 GB) and the
  kilo engine (1,450 MB); the `HistoryPage` shrink loop and the replay dedupe
  replayed verbatim against the operator's real `history.json` files; per-type
  byte cost computed over all 7,004 stored events; the 15 `msg="turn latency"`
  records; `event.Event`'s 44 fields counted from the source; and the phone's
  caps read from `apps/mobile/lib/data/chat/chat_models.dart:61`, `:69`, `:71`,
  `transcript_reducer.dart:873` and `mcremote_client.dart:3466-3524`.
