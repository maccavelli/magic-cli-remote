---
status: proposed
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
* No implementation plan exists yet. Per the repository workflow, one must be
  written and approved before any code change.
