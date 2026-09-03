---
status: accepted
date: 2026-09-03
decision-makers: maccavelli
consulted: —
informed: —
---

# The first-token latency regression is prompt weight, not transport — instrument the turn and control what enters the prompt

## Context and Problem Statement

Starting a new agent session and prompting `hi` used to answer almost
immediately. It now takes 5-18 seconds and is reported to hang indefinitely.
Both **kilo** (HTTP+SSE) and **grok** (ACP over stdio) are affected. Those are
different vendors, different wire protocols, and different processes, so the
common factor is either mcremote, the transport, the host, or something shared
in how turns are constructed.

This record is the result of a measurement pass across the phone transport, the
daemon, and the provider engines. It names what is established by measurement,
what is inferred, and what remains unproven.

### The regression is real, and it is measurable from the operator's own data

Every session is persisted with per-event timestamps under
`~/.local/share/mcremote/sessions/<id>/history.json`. Computing
*user_message → first output event* (`assistant_message_chunk`,
`thought_chunk`, or `tool_call`) across all 35 stored sessions:

| date | provider | prompt → first output | model in `meta.json` |
| --- | --- | --- | --- |
| 2026-08-02 | grok | 0.90s, 1.05s, 1.20s, 1.25s | — |
| 2026-08-11 | opencode | 0.63s, 1.78s | `opencode-go/minimax-m3` |
| 2026-08-11 | kilo | 1.10s, 1.50s, 1.69s, 1.74s, 2.85s | `opencode-go/minimax-m3` |
| 2026-08-11 | codex | 1.08s, 1.21s | `gpt-5.6-sol` |
| **2026-09-02** | **kilo** | **6.64s, 9.33s** | *(absent)* |
| **2026-09-03** | **kilo** | **5.11s** | *(absent)* |
| **2026-09-03** | **grok** | **18.55s** | *(absent)* |

Sub-3-second answers became 5-18 second answers. The operator's report is
confirmed, not anecdotal.

### It is not the transport, and it is not the daemon

Measured directly, not inferred:

* **The ws transport and daemon core cost ~20-90 ms.** An isolated daemon
  (separate port, separate data dir, `fake` provider, zero quota) driven by a
  purpose-built timing harness over the real `mcremote.v1` protocol:
  ws connect 1.4 ms, `session.create` **20 ms**, `session.prompt` accepted
  **0 ms**, event stream flowing at 21 ms intervals.
* **Production accepts prompts in tens of milliseconds.** The daemon logs its
  own enqueue cost (`internal/provider/kilo/session.go:238-240`): the
  2026-09-03 kilo turn recorded `enqueue_ms=24ms`, with `session.create`
  at `ms=40` and the full create round trip 284 ms.
* **The host is not saturated.** Load average 1.84 with five engine processes;
  no leaked agents, no CPU pressure.
* **Streaming is genuinely streaming.** kilo uses SSE
  (`internal/provider/httpagent`), grok uses ACP notifications. Neither polls.
  Text coalescing (`internal/chunkbuf`) passes the leading edge through
  verbatim, so it cannot delay a first token.
* **The relay is not the culprit.** Relay RTT is 27 ms and the bridge
  (`internal/relayhost/client.go:284`) is a straight byte copy. Reconnect
  counts today are unremarkable, and no `slow_client` or `async slots
  exhausted` event appears anywhere in the log.

Every millisecond of the 5-18 seconds falls **between the daemon accepting the
prompt and the provider emitting its first token**.

### What is actually in the prompt

Querying the live kilo engine's own API (read-only, no model invocation) for
the three most recent `hi` turns:

```json
{"input": 13667, "output": 23, "reasoning": 35, "cache": {"read": 768,  "write": 0}}  cost $0.0421
{"input": 27474, "output": 35, "reasoning": 123, "cache": {"read": 1536, "write": 0}} cost $0.0853
{"input": 15046, "output": 248, "reasoning": 3,  "cache": {"read": 43520,"write": 0}} cost $0.0620
```

A one-word `hi` costs **13,667 to 27,474 input tokens** and **4 to 8.5 cents**.
`cache.write` is **0 on every turn**, so essentially the whole prompt is
prefilled from scratch each time. Time-to-first-token scales with prefill, and
a 13-27k-token uncached prefill is a straightforward explanation for a
multi-second wait before the first token appears.

The engine's `/config` shows what is riding along: an `mcp` entry
(`magictools`, a remote MCP server on `localhost:48080`) and two plugins
(`@kilocode/kilo-indexing`, `@kilocode/plugin-atomic-chat`). The MCP server is
healthy and answers in under a millisecond, so it is not hanging — it is
contributing tool definitions to every prompt.

### The gap that made this hard to see

**The daemon measures the part that is fast and never measures the part that is
slow.** It logs `enqueue_ms` — the time to hand a prompt to an engine — and
then logs nothing until the turn ends. There is no `prompt → first token`
metric anywhere in the daemon, so a 20x regression in the number the user
actually feels produced no signal at all. It was only recoverable because
session history happens to carry per-event timestamps, which is a fortunate
side effect rather than a designed measurement.

### Independent defects found during the pass

* **F1 — the Codex reality probe is 12x more expensive than the one it
  replaced.** MADR 0136 changed `ObserveCredentialStore`
  (`internal/provider/codex/store_reality.go:65`) from `codex login status` to
  `codex doctor --json`. Measured on this host: **120 ms → 1,400 ms**, and
  `doctor` performs network reachability checks the old probe did not. It is
  reachable from `AuthStatus` → `backupProjection` and therefore from
  `providers.list`. A 30-second cache
  (`ObserveCredentialStoreCached`, `:183`) bounds it, and it is not on the
  prompt path — so this is not the regression above — but it is a real cost
  regression introduced by that record, on a path the phone touches.
* **F2 — `available_commands` is forwarded and persisted with no dedupe.**
  `internal/provider/acpagent/session.go:1471` emits an event for every ACP
  `available_commands_update`, unconditionally. grok re-sends them constantly:
  **301 of 606 events (50%)** in one session, and 8, 13 and 7 in three recent
  short ones. No other provider does this (goose 1, codex 0). Each one is
  serialised, pushed to the phone, and written to history.
* **F3 — version pinning covers two providers out of five, and warns without
  acting.** Only `kilo` (`version.go:15`, pin `7.4.23`) and `opencode`
  (`version.go:24`, pin `1.18.21`) have a known-good gate. `grok`, `goose` and
  `codex` have none. Every installed provider has drifted: kilo **7.5.6**,
  opencode **1.18.26**, grok 1.0.13, goose 1.48.0, codex 0.152.1. The daemon
  logs `kilo engine version differs from known-good pin` and proceeds.
* **F4 — every message from one phone is decoded in the read loop.**
  `internal/ws/server.go:687` calls `handleMessage` synchronously. Most
  handlers immediately hand off via `dispatchAsync` (`:874`), so this is
  currently benign, but several remain inline (`session.list`,
  `session.set_mode`, `session.set_config`, `session.cancel`,
  `session.pending_asks`, `oauth.cancel`). An inline handler that blocks stalls
  every later message on that connection, including `session.prompt`. The
  async path is additionally bounded at `maxAsyncPerClient = 8` (`:181`).

### What is not established

* **The prompt weight has not been attributed.** That 13-27k tokens enter each
  turn is measured; *which* contributor dominates — MCP tool definitions, the
  two kilo plugins, the agent system prompt, or mcremote's own additions — is
  not. No isolation experiment was run.
* **Why `cache.write` is 0 is unknown.** It may be a route that does not
  support prompt caching, a kilo configuration, or an upstream behaviour. This
  matters, because working prompt caching would likely absorb most of the
  prefill cost without removing anything from the prompt.
* **The model's contribution is unquantified.** The fast August sessions pinned
  `opencode-go/minimax-m3`; the slow September sessions pin nothing and take
  the provider default, which the daemon log shows resolving to
  `kilo/kilo-auto/balanced` — an auto-router. Both old and new turns report a
  1,000,000-token context, so the model family may not have changed much. The
  A/B that would settle it (same prompt, pinned model vs default) spends
  provider quota and was not run.
* **The indefinite hang was not reproduced.** No stalled turn appears in the
  captured data, and no evidence distinguishes a provider stall from a phone-side
  one. `permission_timeout_seconds: 300` and `turn_stall_notice_seconds: 300`
  are both five minutes, which would present as an indefinite hang to a user.

## Amendment, 2026-09-03: the A/B ran, and it found a worse failure than prompt weight

The isolation A/B was approved and executed. It **reproduced a second, more
severe failure that this record's original Decision Outcome did not account
for**, and it narrows the cause to a specific area of mcremote rather than to
the provider.

### What was run

A dedicated kilo engine was started per arm (`kilo serve` on its own port,
separate from the production engine), prompted with `hi` over kilo's own HTTP
API, with SSE captured frame by frame. Then the same prompt was driven
**through an isolated mcremote daemon** (its own port, own data dir, own
device token, kilo enabled, production daemon untouched) so the two paths could
be compared in the same time window against the same engine.

### Result 1 — kilo alone is fast

| arm | agent | TTFT |
| --- | --- | --- |
| direct, no `agent` | — | **0.97s** |
| direct, `agent=code` (what mcremote sends) | code | **0.80s** |
| direct, `agent=code` + `model={providerID:kilo, modelID:kilo-auto/balanced}` (exactly mcremote's body) | code | turn ran |

Same engine, same model (`kilo-auto/balanced`), same agent, same cwd. Sub-second.

### Result 2 — through mcremote, the answer never arrives

Driving the *same engine* through mcremote: `session.create` completed in
241 ms, `prompt_async` was accepted (`ok=true`, `enqueue_ms=9.7ms`), and then
**zero events reached the client for 30-45 seconds**, across repeated runs.

Querying the engine afterwards for that exact session proves the model was not
the problem:

```text
ses_f97fab36effeQsVtlU8k  title='latprobe'  in=99 out=22
  roles=['user','assistant']  reply='Hi! How can I help?'
```

**kilo answered. mcremote delivered nothing.** This is the reported
"hangs indefinitely", reproduced deterministically off the phone entirely.

### Where the fault is not

Each of these was checked and cleared, so the next person does not re-check
them:

* **The frames are on the wire mcremote subscribes to.** A concurrent capture
  of `/global/event` during a turn recorded **86 `message.part.delta`** plus
  `message.part.updated`, `message.updated`, `session.turn.open/close`,
  `session.idle`, `session.status`, `session.diff` and `sync`. The
  per-directory `/event?directory=` stream carries an identical set, so the
  choice of stream is not the fault.
* **Frame decoding is correct.** `DecodeFrame`
  (`internal/provider/kilo/dialect.go:158-177`) already handles both the
  `{payload:{type,…}}` wrapper 7.5.6 uses and the bare form.
* **Session-id extraction is correct.** Replaying `sessionIDOf`'s logic
  (`:192-216`) against live 7.5.6 frames resolves a session id for every
  message and session frame; only `sync`, `server.heartbeat` and
  `server.connected` yield none, and those carry no session.
* **Type coverage is complete.** Every type 7.5.6 emits, including
  `message.part.delta` and `sync`, already appears in the kilo dialect.
* **The pump is connected.** Both the test and production daemons hold an
  established TCP connection to their kilo engine.
* **It is not the model reference.** Sending mcremote's exact
  `{providerID, modelID}` body directly still ran the turn.

That leaves the SSE pump's lifecycle or the agent-session → local-session
binding inside `internal/provider/httpagent` as the remaining candidate. It was
not isolated further, and is deliberately **not** guessed at here.

### One unexplained difference, recorded rather than resolved

The production turn that *did* deliver events logged `agent=code`; the isolated
runs that delivered nothing logged `agent=""` (the ws harness sets no mode, the
phone does). Whether the empty agent is causal or incidental is **not
established**, and it is the first thing to test next.

### How this changes the record

Both failures are real and they are different:

* **Slow-but-working**, seen in production: events arrive, TTFT 5-18s, and the
  prompt carries 13,667-27,474 input tokens with `cache.write: 0`. The prompt
  weight finding stands.
* **Working-but-silent**, reproduced here: the model answers in under a second
  and the answer is never delivered.

The original Decision Outcome — instrument first, then attribute prompt weight
— remains right for the first, and is **insufficient for the second**. The
delivery failure is a correctness bug, not a performance one, and it outranks
the instrumentation work: no amount of latency measurement helps a turn whose
events never arrive.

The revised priority is therefore:

0. **Fix event delivery for kilo 7.5.6 first**, isolating the pump/binding
   area named above, with the `agent=""` question answered on the way.
1-5. The original steps then follow, unchanged.

This also promotes **F3** (version pinning) from a side finding to a direct
contributor: kilo is running **7.5.6** against a pin of **7.4.23**, the daemon
logs `wire shapes were live-probed on the pinned version` on every start, and
it proceeds anyway. That warning described this failure before it happened.

## Amendment, 2026-09-03 (second): scope widened to every provider, and pins move to current

The owner widened the scope: pin kilo to the version actually installed, treat
grok's degradation as in scope, and cover all providers rather than the two
that happen to have a pin today.

### Every provider has drifted, and three cannot detect it

| provider | installed | pin in code | gate |
| --- | --- | --- | --- |
| kilo | **7.5.6** | 7.4.23 (`internal/provider/kilo/version.go:15`) | warns, proceeds |
| opencode | **1.18.26** | 1.18.21 (`internal/provider/opencode/version.go:24`) | warns, proceeds |
| grok | **1.0.13** | *none* | none |
| goose | **1.48.0** | *none* | none |
| codex | **0.152.1** | *none* | none |

Upstream source for all five is checked out locally and current
(`~/gitrepos/{kilocode,opencode,grok-build,goose,codex}`), so wire shapes can be
read rather than guessed at when a pin is moved.

### F5 — the prewarm re-arm races the user's first turn

`Start` re-arms the spare with `defer p.EnsureWarm()`
(`internal/provider/acpagent/acpagent.go:775`), so a **replacement agent process
is spawned the instant a session is created** — which is immediately before the
user's first prompt. The same file measures a full grok start at **~3.8 s**
(`:287`). Prewarm exists to move that cost off the critical path, and re-arming
at create time puts a fresh copy of it back on, concurrently with the turn the
user is waiting for. Two grok processes were observed live on this host.

This is a plausible contributor to grok's first-turn latency. It is **not**
proven to be the dominant term — that requires the instrumentation in step 1 —
but the ordering is wrong on its face and the fix is to re-arm when the session
goes idle rather than when it is created.

### F2 is confirmed upstream, and must be fixed on our side

grok's own source states the behaviour is deliberate
(`~/gitrepos/grok-build/crates/codegen/xai-grok-pager/src/app/agent.rs:715`):

```rust
/// Generation counter for `available_commands`. Bumped on every update
/// (even if the list is identical).
```

grok will keep re-sending an identical command list. Since the provider will not
stop, mcremote must dedupe before it forwards and persists — which is what
turned 301 of 606 events in one session into pure overhead.

### Pins move to the installed versions, and gain teeth

Pinning is extended to all five providers and each pin is set to the version
installed today. A pin that only warns did not prevent the kilo delivery
failure, so the gate must also be *actionable*: the mismatch is reported to the
phone, not only written to a log line nobody reads. Whether a mismatch should
ever refuse to start is left to the plan, because refusing to run on a routine
upstream upgrade would be its own outage.

## Correction, 2026-09-03: the second amendment's central claim was wrong

**Retracted:** "kilo answers, mcremote drops it." That claim was an artifact of
a broken measurement instrument, and it should not have been recorded as a
finding. What follows replaces it.

### What went wrong with the instrument

The purpose-built ws harness reused the request id `c1` for every run. The
daemon implements **idempotent replay** for mutating async operations, keyed on
`(deviceID, request id)` (`internal/ws/server.go:925-928`, MADR 0095 D6/F5):
a repeated id replays the original response frame instead of executing again.
So the harness received a replayed `session.created` for an *earlier* session,
bound to it, and prompted that one. The freshly created session received no
prompt at all, which is why it looked like "prompt accepted, nothing
delivered".

**The daemon was correct.** Idempotency is designed to collapse exactly this,
and a client that reuses a request id across logically distinct requests is the
party in the wrong. This is not a daemon defect and needs no daemon change; the
lesson belongs to whoever writes the next diagnostic.

The proof is in the test daemon's own session store: the session created by the
silent control run recorded **no `user_message` event whatsoever**, while a
neighbouring session from the same period recorded a complete turn
(`thought_chunk` ×2, `assistant_message_chunk` ×3, `turn_complete`). A prompt
that was never delivered to a session cannot be evidence that the session's
replies were dropped.

Re-run with the repository's own `scripts/smoke-protocol`, which correlates
correctly, the turn completes end to end through mcremote on kilo 7.5.6:

```text
event type=thought_chunk            text="A simple greeting warrants a brief response."
event type=assistant_message_chunk  text="Hi. What would you like to work on?"
event type=turn_complete            status=end_turn
✓ smoke-protocol PASSED
```

**There is no event-delivery bug.** F3's version-drift warning did not predict
this failure, because this failure does not exist. Every mechanism the second
amendment "cleared" was in fact fine, and so is the one it accused.

I trusted a harness I had written without first proving it could distinguish
success from failure — the exact failure mode this repository's own rule about
unverified instruments describes.

### What the corrected measurements actually show

Through mcremote, with the known-good driver, prompt → first output:
**4.25s, 4.43s, 9.10s**. Directly against the same engine: **0.79-0.97s**.

But those two sets are not comparable as stated, and the reason is the real
finding. Token accounting for every turn on that one engine:

| input | cache read | cost | turn |
| --- | --- | --- | --- |
| 13,637 | 2,176 | $0.0061 | cold |
| 14,435 | 0 | $0.0445 | cold |
| 99 | 14,336 | $0.0053 | warm |
| 99 | 14,336 | $0.0059 | warm |
| 99 | 14,336 | $0.0074 | warm |

There are two populations, not a spread. A turn either pays a **~14,000-token
uncached prefill** or it pays **99 tokens against a 14,336-token cache read**.
The direct probes measured a warm cache; the mcremote runs were mostly cold.

So the operative variable is **whether the ~14k-token system prompt is cached
when the turn starts**, and the user's complaint names exactly the case that
cannot be warm: *"starting a new agent session."*

This also re-reads the production evidence in this record's first section.
Those turns showed `cache.write: 0` on **every** observed turn — a cache that
is never written is never warm, so every production turn pays the full prefill.
That, not a delivery bug, is the strongest available explanation for 5-18s
first tokens on a host whose engine can answer in under a second when warm.

### What is still not established

* **Why `cache.write` is 0 in production** while the isolated engine shows
  `cache_read: 14,336`. This is now the single highest-value open question and
  Phase 6 is re-pointed at it.
* **What the ~14k tokens are.** Attribution to MCP tool definitions, kilo
  plugins, or the agent system prompt is still unmeasured.
* **Whether mcremote adds prompt weight of its own.** No injection was found in
  the kilo path, but the two populations above were not separated by client.
* **The indefinite hang remains unreproduced.** The reproduction I believed I
  had was the instrument bug. It should be treated as an open report, not a
  diagnosed defect.

The decision outcome is unchanged and is, if anything, reinforced: instrument
the turn first. Two rounds of confident conclusions have now been produced from
unmeasured systems, and the second was wrong.

## Amendment, 2026-09-03 (third): providers run on their own default model

**Accepted constraint.** A provider that has an established default model runs
on it. mcremote does not pin a model, substitute one, or silently prefer a
"faster" one; a model is chosen only when the user asks for one.

This binds the remediation in two places:

* **Nothing in this work may pin a model to buy latency.** "Pin a fast model
  per provider and treat the default as untrusted" was weighed as an option
  below and is now foreclosed by this constraint, independently of the
  correlational evidence that already made it weak.
* **Phase 6 reports, it does not re-model.** Where prompt weight or model
  routing is the dominant cost, the finding is reported with numbers and the
  decision stays with the user. Attribution is not licence to change the model.

The reasoning is that the default is the provider's own answer to "what should
this agent be", and it moves as the vendor improves it. Freezing it inside
mcremote would substitute our judgement for theirs, would go stale silently,
and would have to be maintained per provider forever. `model: ""` in
`config.yaml` — "use the provider's own default" — stays the correct default
posture, and remains overridable by the user per provider or per session.

What this constraint does **not** excuse: the daemon must still make the cost
of the default visible. Reporting that a `hi` turn cost 27,474 input tokens is
honest observability; quietly swapping the model to make that number smaller is
not.

## Amendment, 2026-09-03 (fourth): all five providers, and the Codex deprecation notice

The owner scoped this record to **all five providers** rather than the two the
evidence happened to come from, and added a sixth finding.

### The five providers do not share one transport

Optimisations do not generalise across them, so the remediation is stated per
provider rather than "for providers":

| provider | transport | package |
| --- | --- | --- |
| grok | ACP over stdio | `internal/provider/acpagent` |
| kilo | HTTP + SSE | `internal/provider/httpagent` |
| opencode | HTTP + SSE | `internal/provider/httpagent` |
| goose | ACP over HTTP | `internal/provider/acphttp` |
| codex | app-server JSON-RPC | `internal/provider/codex` |

Measured consequences of that split:

* **Prewarm exists only in `acpagent`.** `EnsureWarm` appears nowhere in
  `httpagent`, `acphttp` or `codex`; those keep a shared engine instead. So
  **F5 is a grok-only defect**, not a general one, and the other four need no
  change for it.
* **`available_commands` is emitted by four of the five** — `acpagent`
  (grok), `acphttp` (goose), `kilo` and `opencode`. Only grok was observed
  spamming it, and grok's source says it re-sends even when the list is
  unchanged, but the dedupe belongs where all four can inherit it rather than
  in grok's branch alone.
* **`notice` is emitted from all five**, and one codex session recorded **77
  of them** — a second instance of the same "emit unconditionally, persist
  unconditionally" pattern as F2.

### F6 — Codex emits a deprecation notice on every session start

Reproduced from source rather than from the symptom. Codex emits
`DeprecationNotice` once per session for each entry in
`config.features.legacy_feature_usages()`
(`~/gitrepos/codex/codex-rs/core/src/session/session.rs:1052-1059`). A legacy
usage is recorded when the config names a feature by an **alias** instead of its
canonical key.

This host's `~/.codex/config.toml` contains:

```toml
[features]
codex_hooks = true
```

and `codex_hooks` is exactly such an alias
(`codex-rs/features/src/legacy.rs:48-51`):

```rust
Alias { legacy_key: "codex_hooks", feature: Feature::CodexHooks },
```

whose canonical spec is `key: "hooks"`, `stage: Stable`,
**`default_enabled: true`** (`codex-rs/features/src/lib.rs:1146-1151`).

Two consequences, and they belong to different owners:

1. **The operator's fix is to delete the block.** Because `hooks` is stable and
   enabled by default, `[features] codex_hooks = true` is not merely
   deprecated — it is redundant. Removing it silences the notice and changes
   no behaviour. Renaming it to `hooks = true` would also work and is equally
   redundant. This is a change to the user's own file and mcremote must not
   make it unasked.
2. **mcremote's fix is not to repeat it.** A notice that fires once per session
   start is not itself a defect, but nothing dedupes notices, and the 77-notice
   codex session shows the same unconditional-emit pattern as F2. The daemon
   should not turn a once-per-session upstream notice into per-session noise
   the operator learns to ignore.

F6 is a *correctness-of-signal* finding, not a latency one. It is in scope
because a warning the operator has been trained to ignore is how the next real
warning gets missed — which is precisely how the kilo version-pin warning was
treated.

## Amendment, 2026-09-03 (fifth): surface and protocol analysis of all five providers

The owner asked three questions this record could not answer: why opencode
pins a floor, whether the binaries were driven to evaluate their surfaces, and
whether the protocols were analysed. The honest answers were "it does not",
"no" and "no". This amendment is that work.

### Correction: opencode does not pin a floor, and it did warn

Two earlier statements in this record's Phase 1 notes were wrong.

`opencode.KnownGoodVersion` is an **exact** pin — `VersionIsKnownGood` is
`CompareVersions(v, KnownGoodVersion) == 0`, the same policy as kilo. What
opencode has *in addition* is a separate `MinVersion = 1.18.0` hard floor that
gates session-tree mode and can refuse `Start` via `CheckMinVersion`. Two
constants, two deliberate policies, both documented in
`internal/provider/opencode/version.go:11-24`.

And opencode **did** warn at 1.18.26: `opencode engine differs from the
known-good release … hint="supported, but this release has not been assessed
against mcremote"`. I missed it because I grepped for kilo's wording
(`differs from known-good`) against opencode's (`differs from the known-good
release`). The gate works; my instrument did not. That is the third measurement
error in this record, and the pattern — trusting a grep or a harness without
first proving it can distinguish the two answers — is now the most reliable
finding in it.

### Method

Each engine was driven and its live surface compared against what mcremote
consumes: SSE frames captured from kilo and opencode (Phase 1 fixtures), an ACP
`initialize` handshake against grok and goose (no model tokens spent), and
codex's `ServerNotification` set read from its own source and diffed against
`internal/provider/codex/routing.go`.

### F7 — kilo 7.5.6 ships an event-sourced `sync` stream that mcremote ignores

`sync` is the **most common frame in the fixture — 18 of 56** — and is
unhandled. It wraps the same session and message events in a durable envelope:

```json
{"type":"sync","syncEvent":{"id":"evt_…","type":"session.created.1",
 "seq":0,"aggregateID":"ses_…","data":{…}}}
```

Versioned types (`.1`), a monotonic **`seq`** per **`aggregateID`**. That is
replay, and mcremote currently has none: `streamOnce` comments that "frames
emitted while the stream was down are gone — the engine does not replay", and
compensates with `resyncSessions`, which polls engine state after every
reconnect. On 7.5.6 that statement is no longer true. Consuming `sync` would
let a reconnect resume from the last seen `seq` instead of polling and hoping —
closing an acknowledged reliability gap with an upstream capability that is
already on the wire.

### F8 — opencode 1.18.26 emits four unhandled types, one of them load-bearing

| type | count | consequence |
| --- | --- | --- |
| `plugin.added` | **45** | 53% of the fixture; a fourth instance of the unconditional-emit pattern behind F2 and F6a |
| `catalog.updated` | 2 | **the model catalog changed and mcremote does not know** — it caches catalogs in `p.catalogs`, so it will keep serving a stale model list |
| `reference.updated` | 1 | unassessed |
| `integration.updated` | 1 | unassessed |

`catalog.updated` is the one that matters: mcremote harvests the model catalog
once and caches it provider-wide, and the engine is now telling it when that
cache is invalid.

### F9 — codex 0.152.1 declares 83 notifications; mcremote routes 75, with none stale

The router is in good shape — `routing.go` claims to be "exhaustive for the
installed stable schema", and it is exhaustive for the schema it was written
against. Eight have appeared since:

* **`modelProvider/authRecoveryStarted` and `modelProvider/authRecoveryCompleted`**
  — codex now *announces* credential recovery. MADRs 0133, 0134 and 0136 were
  all spent inferring that state from files and probes; the provider has since
  started reporting it directly.
* `mcpServer/event/stream/notification`
* `rawResponse/completed`, `rawResponseItem/completed`
* `thread/realtime/item/started|completed|transcript/delta`

A first pass at this diff produced a **false** finding — that
`account/login/completed` had been renamed and mcremote was listening for a dead
method. It had not: the variant carries an explicit
`#[serde(rename = "account/login/completed")]` that my extraction missed
because it only handled the macro's inline `=> "wire"` form. Corrected, there
are **zero** stale routes. Recorded because the near-miss is the point: a
protocol diff is only as good as its parser.

### F10 — grok and goose advertise capabilities mcremote never reads

Driven live. Neither provider is asked for anything it offers beyond the basics.

| capability | grok 1.0.13 | goose 1.48.0 | referenced in mcremote |
| --- | --- | --- | --- |
| `sessionCapabilities` | `list`, `resume`, `close` | `list`, `delete`, `close` | **0** |
| `promptCapabilities.embeddedContext` | true | true | **0** |
| `promptCapabilities.image` | false | **true** | logged only (grok); **never read** for goose |
| `mcpCapabilities` | http, sse | http | read |
| `_meta` extensions | `x.ai/fs_notify`, `x.ai/hooks` (blocking `pre_tool_use`/`stop`/`subagent_stop`, decisions `deny`/`block`), `toolOverrides` | `recipeParameterScopes`, `localInference` | **0** |
| `agentInfo.version` | — | `1.48.0` | **0** |

Three of these are directly actionable:

* **goose accepts images and mcremote cannot send them.** `acphttp` never reads
  `promptCapabilities` at all, so goose's `image: true` is unobserved. `acpagent`
  reads it but only to log it.
* **`agentInfo.version` is a free version source.** grok and goose have no pin
  today partly because their version was awkward to obtain; the ACP handshake
  hands it over.
* **`x.ai/hooks` is a blocking permission mechanism** mcremote does not use,
  having built its own auto-approve. Whether to adopt it is a design question,
  not a defect — but it should be a decision rather than an oversight.

### What this changes

Pinning alone was never going to be enough. Four of the five providers have
grown surface since mcremote was last assessed against them, and in two cases
(`sync`, `catalog.updated`) the new surface exists precisely to fix a
correctness gap mcremote works around today. "Optimise for the current
versions" therefore means consuming what the current versions offer, not only
recording their numbers — and the plan is extended accordingly.

None of F7-F10 is a latency fix. They are compatibility and correctness debt
that accumulated silently because the only drift signal was a log line that,
as this record has now demonstrated twice, nobody reads.

## Amendment, 2026-09-03 (sixth): the capture gap is resolved in the daemon

Phase 1 could only capture kilo and opencode, because the five providers do not
share a transport: grok speaks ACP over stdio through a third-party SDK that
owns its read loop, goose speaks ACP over a websocket, and codex speaks JSON-RPC
over an app-server proxy. Capturing those from outside would mean
reimplementing each client handshake in a script.

**Decision: capture inside the daemon, gated by an environment variable.**
`internal/wirecap` records raw frames at the point they arrive, with one hook
per transport:

| transport | providers | hook |
| --- | --- | --- |
| `httpagent` | kilo, opencode | the SSE line in `streamOnce` |
| `codex` | codex | `conn.readPump`, after `transport.Read` |
| `acphttp` | goose | `readPump`, after `ws.Read` |
| `acpagent` | grok | a tee on the `stdout` reader handed to the ACP SDK |

`wirecap.For` returns **nil** unless `MCREMOTE_WIRE_CAPTURE_DIR` is set, and
every method is nil-safe, so a shipped daemon pays one nil check per frame and
a reader can see at each call site that nothing happens in production. The grok
hook is a tee because the SDK owns the loop and the reader is the only seam;
a nil capture's `TeeReader` returns the reader unchanged.

Redaction happens at capture time rather than afterwards, in both the absolute
and separator-stripped forms of the home path — the second form is the one that
survived a fixture I had already called redacted.

This is a change to production code paths, which the plan did not previously
authorise; it is recorded here as the decision rather than taken quietly.

**Verified by capturing the three providers the external tool could not reach:**
codex 0.152.1 (38 frames), goose 1.48.0 (15), grok 1.0.13 (19). All five
providers now have a version-stamped fixture.

### F11 — grok's `_x.ai/*` extension namespace is largely unhandled

**Superseded by the live capture below; the counts here were taken from a
lifecycle-only capture and a loose grep, and were wrong in both directions.**
grok emits **ten** `_x.ai/*` methods during a real turn, and mcremote handles
**three** — `_x.ai/models/update`, `_x.ai/session_notification` and
`_x.ai/mcp_initialized`. Verified by exact literal match against the source
rather than substring:

| notification | frames in one `hi` turn | handled |
| --- | --- | --- |
| `_x.ai/announcements/update` | 11 | **no** |
| `_x.ai/settings/update` | 10 | **no** |
| `_x.ai/sessions/changed` | 9 | **no** |
| `_x.ai/queue/changed` | 9 | **no** |
| `_x.ai/mcp/init_progress` | 6 | **no** |
| `_x.ai/mcp/servers_updated` | 4 | **no** |
| `_x.ai/session/prompt_complete` | 3 | **no** |

Seven unhandled methods, **52 of 247 frames (21%)** of a single turn. Three are
interesting rather than merely unused: `_x.ai/mcp/init_progress` and
`_x.ai/mcp/servers_updated` report MCP startup progress and membership — the
"why is my first turn slow" signal this record has spent its length pursuing
indirectly — and `_x.ai/session/prompt_complete` is an explicit turn-completion
signal mcremote currently infers.

(`_x.ai/exit_plan_mode` and `_x.ai/ask_user_question` are also implemented, as
client-side requests rather than notifications; neither occurred in this turn.)

### What the codex fixture confirms

Every method codex emitted during a real turn — twelve of them — is routed. The
eight unrouted notifications from the fifth amendment are for features a basic
turn does not exercise (`rawResponse*`, `thread/realtime/*`,
`modelProvider/authRecovery*`), not broken basics. The router is sound; it is
simply behind on surface.

## Amendment, 2026-09-03 (seventh): grok tested live, end to end

The owner authorised spending grok quota. Three live `hi` turns were run through
mcremote against grok 1.0.13, with in-daemon capture on.

**grok's quota is not exhausted.** A note carried since 2026-09-01 said it was,
and that note gated every grok test in this record. It is stale: all three turns
completed normally and returned real replies.

### Latency — highly variable, and consistent with the two-population finding

| run | prompt → first output |
| --- | --- |
| 1 | **5.26s** |
| 2 | **2.57s** |
| 3 | **14.36s** |

Each was a fresh session, so all three are cold. Against this record's other
grok figures — 0.90-1.25s historically (2026-08-02) and 18.55s earlier today —
the spread is the finding: a 5.6x range across three identical prompts minutes
apart, on the same binary, same model and same host. That is upstream variance,
not something mcremote's transport is adding, and it is why Phase 2's
instrumentation must record every turn rather than sampling.

### What a full turn actually contains

247 frames for the word `hi`:

| frame | count |
| --- | --- |
| `session/update` → `agent_thought_chunk` | **93** |
| `session/update` → `agent_message_chunk` | 30 |
| `session/update` → `available_commands_update` | **22** |
| other `session/update` | 5 |
| `_x.ai/*` extensions | 76 |
| JSON-RPC results | 17 |

Two things stand out. The model emitted **93 reasoning chunks to say "Hi. What
would you like to work on?"**, which is where the seconds go and which no change
in mcremote can shorten. And `available_commands_update` fired **22 times in a
single turn** — F2 was inferred from a 301-event session; this is the same
defect measured inside one turn, and it is pure overhead on the wire, on the
phone and in the history file.

## Amendment, 2026-09-03 (eighth): the latency record needs a model the daemon does not have

Phase 2's per-turn record was built and verified live. It exposed a gap this
record had not anticipated, and the gap follows directly from the third
amendment's own decision.

**`model` is empty on every default-model session.** The session manager's only
model is `Meta.Model` (`internal/session/manager.go:773`), which is
`StartOptions.Model` — what the *client asked for*. The third amendment made
"providers run on their own default model" the norm, so in the normal case the
client asks for nothing and the field is omitted. The daemon does resolve a
real model (`kilo/kilo-auto/balanced`, logged at engine-ready), but that value
lives inside the kilo dialect (`internal/provider/kilo/dialect.go:136-143`) and
reaches nothing else. No event carries it.

Surveyed across all five: **only kilo and opencode track a resolved default
model at all** (`defaultModelID`), and only those two log one. grok, goose and
codex track none.

This matters because a latency record without a model cannot distinguish a
regression in the transport from a change in what the engine is running — and
separating those is the whole purpose of the instrumentation.

### Decision: an optional provider interface, not an event field

A new optional interface in `internal/provider`:

```go
// ModelReporter is optionally implemented by sessions that know the model
// they are actually running on, including one the provider chose by default.
type ModelReporter interface {
    Session
    CurrentModel() string
}
```

The manager reads it at turn end. kilo and opencode implement it now from the
`defaultModelID` they already hold; grok, goose and codex report nothing until
they track one, and the field stays absent rather than guessed.

* Good, because it adds no protocol surface. `event.Event` is serialized to the
  phone, so putting the model there would be a protocol change — which Phase
  2's step 2.4 rules out of this phase deliberately, to keep instrumentation
  separable from a client-visible feature.
* Good, because it follows the optional-capability pattern the codebase already
  uses for nineteen other session facets (`ModelSession`, `CWDSession`,
  `RuntimeSession`, …), so a provider that cannot answer simply does not
  implement it.
* Neutral, because it reports the session's model rather than the turn's. A
  mid-session `/model` switch is picked up on the next turn, which is the right
  granularity for this record and is not the same as the per-message model
  `kilo/session.go:331` decodes.
* Bad, because it covers two of five providers on delivery. The other three are
  not blocked by the interface — they are blocked by not knowing their own
  default, which is a provider-side gap this amendment names rather than fixes.

## Amendment, 2026-09-03 (ninth): the five engines report their version five ways

The fifth amendment recorded `agentInfo.version` as an unconsumed ACP surface
and the plan built step 7.5 on it, as the single version source for the grok
and goose pins. Driving the engines shows that is true for one of them.

| provider | installed | version on the wire | shape |
| --- | --- | --- | --- |
| kilo | 7.5.6 | `version` in `GET /global/health` | already consumed |
| opencode | 1.18.26 | `version` in `GET /global/health` | already consumed |
| goose | 1.48.0 | `agentInfo.version` in the `initialize` result | standard ACP |
| grok | 1.0.13 | `_meta.agentVersion` in the `initialize` result | vendor extension |
| codex | 0.152.1 | `userAgent` prefix at `initialize`; `cliVersion` on `thread/started` | app-server, not ACP |

**grok sends no `agentInfo`.** Zero occurrences across all 247 frames of the
full-turn fixture, which includes the complete `initialize` result. It is
within spec to omit it: the ACP SDK types the field as optional
(`acp-go-sdk@v0.13.5/types_gen.go:2336`, "in future versions of the protocol,
this will be required"). What grok does send is
`result._meta.agentVersion: "1.0.13"`, alongside a substantial vendor `_meta`
block — `modelState.currentModelId` (`grok-4.6`), `availableModels` with
per-model reasoning-effort rungs, `availableCommands`, and `mcpServers`.

**goose does send it**, confirmed by driving the engine directly:
`{"name":"goose","version":"1.48.0"}`.

**codex is not ACP and was never covered by 7.5.** Its version arrives twice:
embedded in the `initialize` result's `userAgent`, which mcremote already
parses and stores in `initializeMetadata.UserAgent` but never reads for a
version, and explicitly as `cliVersion` on `thread/started`. The userAgent
shape is `<originator>/<CARGO_PKG_VERSION> (…)`, established from the upstream
source (`codex-rs/login/src/auth/default_client.rs:164-170`) rather than
inferred from the string.

### Decision: read each engine's own source, rather than one source for all

Each pin compares against a version the *running engine* reported. The
alternative considered was executing `<bin> --version` at engine ready — one
uniform mechanism, no per-engine parsing.

* Good, because a pin exists to say "the wire shapes were checked against
  *this* engine", and only the engine can say which engine it is. Codex makes
  the difference concrete: its app-server may be a managed daemon of a
  different build from the `codex` on `PATH`, so the subprocess answer can be
  confidently wrong.
* Good, because it costs no process spawn on any engine start.
* Neutral, because it is three readers rather than one. All three sit in files
  Phase 7 already touches.
* Bad, because two of the three parse vendor-specific shapes that upstream may
  change without notice. Mitigated by every reader failing to empty rather than
  to a wrong value, and by an unreported version producing no warning at all.

### A second finding: the goose fixture has no handshake

`acphttp` performs `initialize` over `POST /acp` (`conn.go:109`), while the
MADR 0137 wire capture hooks the websocket. The goose fixture's 15 frames
therefore begin at the `session/new` result, and the question above had to be
settled by driving the engine live rather than by reading the fixture the sixth
amendment added for exactly this purpose.

A capture hook goes into `postJSON`, under the same environment gate and the
same redaction, and the goose fixture is re-recorded with the handshake
included. A fixture that silently omits a transport's control plane is the same
class of defect as the empty `frames.jsonl` Phase 1 found in its own tool.

## Decision Drivers

* The number the user feels — prompt to first token — must be measured by the
  daemon, or the next regression is found by a person complaining again.
* Diagnosis must not require spending provider quota or reading raw JSON out of
  the session store by hand.
* Prompt weight is a first-class performance input and is currently invisible:
  nothing reports the token cost of a turn until the money is already spent.
* A fix must not be a guess. The dominant contributor to prefill is not yet
  isolated, so the first move must be measurement rather than deletion.
* Provider version drift is a known, recurring source of behaviour change and
  is currently detected for two providers and acted on for none.
* Cost and latency are the same problem here: 4-8.5 cents to say `hi` is the
  same finding as 5-18 seconds to say `hi`.

## Considered Options

* Instrument the turn end to end, then attribute prompt weight before changing
  it
* Cut prompt weight immediately — disable MCP servers and plugins for the
  affected providers
* Pin a fast model per provider and treat the default as untrusted
* Accept it as upstream provider behaviour and change nothing in mcremote

## Decision Outcome

Chosen option: "Instrument the turn end to end, then attribute prompt weight
before changing it", because the measurement pass proved the daemon is not the
bottleneck but could not attribute the bottleneck it found, and every remaining
option is a guess until that attribution exists. The instrumentation is also
the durable fix for the deeper gap: this regression was invisible to the daemon
that caused the user to notice it.

Concretely, in priority order:

1. **Emit a per-turn latency record.** At minimum
   `prompt_accepted → first_output_event → turn_complete`, per provider and
   model, logged at info and exposed to the phone. This is the metric the
   regression is defined in, and nothing measures it today.
2. **Record prompt weight per turn** where the provider reports it — kilo
   already returns `input`, `output`, `reasoning`, `cache.read` and
   `cache.write`. Surface it alongside the latency record, so a 27k-token `hi`
   is visible at the moment it happens rather than after a bill.
3. **Attribute prefill before cutting it.** Run the isolation A/B — same
   prompt, with and without the MCP server, with and without plugins, pinned
   model vs default — and record the result. Only then decide what leaves the
   prompt.
4. **Investigate `cache.write: 0`.** If prompt caching can be made to work,
   it addresses the cost and the latency together and removes nothing the
   agent can use.
5. **Fix the independent defects (F1-F4) on their own merits**, none of which
   waits on the attribution above.

### Consequences

* Good, because the next latency regression is caught by the daemon rather than
  by the operator, and is attributable when it is caught.
* Good, because prompt weight and cost become observable at the moment they are
  incurred, which is the only point at which a user can act on them.
* Good, because F1-F4 are real defects that can be fixed immediately and
  independently, without waiting for the attribution work.
* Bad, because it does not make `hi` fast today. The instrumentation step
  deliberately produces knowledge rather than speed, and the operator's turns
  stay slow until step 3 identifies what to remove.
* Bad, because per-turn latency and token logging is a new event on a hot path;
  it must be cheap and must not itself become a source of latency or log noise.
* Neutral, because nothing here changes provider behaviour. If the dominant
  cost turns out to be an upstream routing or caching decision, mcremote's
  remaining lever is to report it honestly and let the operator pin around it.

### Confirmation

* The per-turn record shows `prompt → first token` for a `hi` turn on kilo and
  grok, and the value matches what the session history independently reports
  for the same turn.
* Replaying the analysis in this record against the new instrumentation
  reproduces the table above without reading `history.json` by hand.
* The isolation A/B is recorded with token counts per arm, so the dominant
  prefill contributor is named with a number rather than asserted.
* F1: the Codex probe's cost on the `AuthStatus` path is measured again and is
  either back near its previous ~120 ms or explicitly accepted with its cost
  stated.
* F2: a session with grok records one `available_commands` event per distinct
  command list, verified against a session that previously recorded 301.
* F3: a provider whose version differs from its pin is reported to the phone,
  not only to a log line nobody reads.

## Pros and Cons of the Options

### Instrument the turn end to end, then attribute prompt weight before changing it

* Good, because it fixes the reason this took a measurement pass to find at
  all.
* Good, because it makes the cost finding (4-8.5 cents per `hi`) visible, which
  is independently valuable.
* Good, because it does not remove capability from the agent on a hunch.
* Neutral, because it is two steps rather than one: measure, then act.
* Bad, because the user waits longer for relief than an immediate cut would
  give them.

### Cut prompt weight immediately — disable MCP servers and plugins

* Good, because it would very likely produce an immediate, large improvement:
  13-27k tokens is the measured input and tool definitions are a known large
  contributor.
* Bad, because the contributor is not isolated. Disabling `magictools` removes
  real capability the operator uses, and might not be the dominant term.
* Bad, because it treats a symptom in the provider's configuration as though it
  were an mcremote decision, and leaves nothing behind that would catch the
  next occurrence.

### Pin a fast model per provider and treat the default as untrusted

* Good, because the fast historical sessions did pin a model and the slow ones
  do not, which is a real observed difference.
* Good, because `model: ""` currently means "whatever the vendor changed its
  default to this week", which is an uncontrolled input to a latency-sensitive
  path.
* Bad, because the evidence is correlational: both eras report a 1M context, and
  the model's contribution was never isolated from the prompt weight.
* Bad, because a pin freezes a choice the vendor will keep evolving, and it
  would have to be maintained per provider.
* **Foreclosed** by the accepted constraint above: providers run on their own
  default model unless the user specifies one.

### Accept it as upstream provider behaviour and change nothing

* Good, because the measurements do show the daemon is fast and the delay is
  the provider's time to first token.
* Bad, because "the provider is slow" is not established — prompt weight is an
  input mcremote and its operator control, and it was never measured before
  this pass.
* Bad, because it leaves the observability gap in place, which guarantees the
  next regression is found the same way.

## More Information

* Latency evidence: `~/.local/share/mcremote/sessions/*/history.json`;
  the 2026-09-03 kilo turn is session `575db868` (prompt 15:27:15.601, first
  `thought_chunk` 15:27:20.707) and the grok turn is `76a15409` (prompt
  15:26:59.561, first `thought_chunk` 15:27:18.113).
* Daemon-side timings: `internal/provider/kilo/session.go:238-240`
  (`enqueue_ms`), and the daemon log for 2026-09-03T10:27.
* Transport measurement: isolated daemon on `127.0.0.1:7599` with the `fake`
  provider, driven over `/v1/ws`.
* Token and cost evidence: the live kilo engine's `/session` endpoint.
* F1: `internal/provider/codex/store_reality.go:65`, `:183`;
  [0136-MADR-classify-codex-auth-from-the-doctor-report.md](0136-MADR-classify-codex-auth-from-the-doctor-report.md).
* F2: `internal/provider/acpagent/session.go:1471`.
* F3: `internal/provider/kilo/version.go:15`,
  `internal/provider/opencode/version.go:24`.
* F4: `internal/ws/server.go:687`, `:874`, `:181`.
* Implementation:
  [0137-PLAN-prompt-to-first-token-latency-regression.md](0137-PLAN-prompt-to-first-token-latency-regression.md).
* Provider sources read during this pass, all current as of 2026-09-03:
  `~/gitrepos/kilocode`, `~/gitrepos/opencode`, `~/gitrepos/grok-build`,
  `~/gitrepos/goose`, `~/gitrepos/codex`.
* F5: `internal/provider/acpagent/acpagent.go:775` (`defer p.EnsureWarm()`),
  `:287` (the ~3.8 s grok start measurement).
