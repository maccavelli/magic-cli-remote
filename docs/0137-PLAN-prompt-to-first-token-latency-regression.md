---
status: in-progress
date: 2026-09-03
associated-madr: "0137-MADR-prompt-to-first-token-latency-regression.md"
---

# Implement: restore first-token latency — delivery, instrumentation, pins, and per-provider optimisation

Associated MADR: [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md)

## Goal

Make `hi` answer in about a second again, and make the next regression
detectable by the daemon rather than by the operator. In order: fix the turns
that never arrive, measure the turns that arrive slowly, then remove the
avoidable cost — across every provider, with pins that match reality.

## Scope

**In scope:**

* `internal/provider/httpagent/` — the SSE pump lifecycle and the
  agent-session → local-session binding (the kilo 7.5.6 delivery failure).
* `internal/provider/kilo/version.go`, `internal/provider/opencode/version.go`
  — pins moved to installed versions.
* New version gates for `grok`, `goose`, `codex`, following the existing
  `KnownGoodVersion` shape.
* `internal/provider/acpagent/acpagent.go` — prewarm re-arm scheduling (F5).
* `internal/provider/acpagent/session.go:1471` — `available_commands` dedupe
  (F2).
* `internal/provider/codex/store_reality.go` — probe cost (F1).
* `internal/ws/server.go` — per-turn latency + token instrumentation; the
  remaining inline handlers (F4).
* `internal/session/` and `internal/protocol/` only as far as carrying the new
  latency/version signals to the phone.
* Tests for each of the above, plus fixtures captured from the installed
  provider versions.

**Explicitly out of scope:**

* **Changing which model a provider defaults to, or pinning models anywhere.**
  Per the MADR's accepted constraint, a provider with an established default
  model runs on it, and a model is chosen only when the user asks for one. This
  plan measures the default's cost; it never substitutes it. `model: ""` stays
  the correct posture in `config.yaml`.
* Removing the operator's MCP servers or kilo plugins. Prompt weight is
  attributed in Phase 6 and acted on only after.
* The phone app. No Flutter change is required by any phase here.
* MADR 0135 (manifest versioning) and the outstanding 0136 host verification.
* Upgrading or downgrading any provider binary. Pins move to what is
  installed; the binaries are not touched.

## Implementation Steps

### Phase 0 — ~~restore event delivery for kilo 7.5.6~~ **withdrawn**

**Withdrawn 2026-09-03. There is no event-delivery bug.** The reproduction this
phase was built on was an artifact of a harness that bound itself to a stale
session id and prompted it, so a freshly created session was never prompted at
all. See the MADR's "Correction, 2026-09-03". Verified with the repository's own
`scripts/smoke-protocol`: a kilo 7.5.6 turn completes end to end through
mcremote, producing thought chunks, assistant text and `turn_complete`.

Two obligations survive from it, and both move into other phases:

1. **The harness bug is itself a finding.** The daemon replays
   `session.created` for pre-existing sessions to a newly connected client, and
   the replay carries the original request id, so a client correlating by
   request id can silently bind the wrong session. `scripts/smoke-protocol`
   correlates correctly, but any client that does not — including a future
   diagnostic — inherits this trap. Phase 2 adds the per-turn record that would
   have made the mistake obvious immediately, and Phase 4 gains a step to make
   replayed events distinguishable from live responses on the wire.
2. **The unreproduced hang stays open**, tracked in Phase 5 as an observation to
   watch for rather than a defect with a known cause.

Nothing from this phase is implemented.

### Phase 1 — capture wire fixtures from the installed versions

6. For each provider, record a real turn's event sequence to
   `testdata/wire/<provider>-<version>/`: kilo 7.5.6 (`/global/event`),
   grok 1.0.13 (ACP notifications), goose 1.48.0, opencode 1.18.26,
   codex 0.152.1. Note the version in the fixture, as MADR 0136's doctor
   fixtures do.
7. These are what make a pin move safe: a pin bump with no fixture is a claim
   that nothing changed, and kilo 7.5.6 is the counter-example.

Commit at the end of the phase.

### Phase 2 — per-turn latency and token instrumentation

8. Emit one structured record per turn: `prompt_accepted`,
   `first_output_event`, `turn_complete`, with provider, model, and the deltas
   between them. Info level, one line, no per-chunk logging.
9. Include prompt weight where the provider reports it — kilo returns `input`,
   `output`, `reasoning`, `cache.read`, `cache.write`. A 27k-token `hi` must be
   visible when it happens.
10. Surface first-token latency to the phone so the operator sees it without
    reading logs. Protocol addition only if a field is genuinely needed.
11. Verify the record reproduces the MADR's table from live turns rather than
    from `history.json` parsing.

Commit at the end of the phase.

### Phase 3 — pins to current, for every provider

12. `kilo` → **7.5.6**, `opencode` → **1.18.26**.
13. Add `KnownGoodVersion` gates for `grok` (**1.0.13**), `goose`
    (**1.48.0**), `codex` (**0.152.1**), reusing the existing shape.
14. Make the mismatch actionable: report it to the phone as a provider
    condition, not only as a log line. Do **not** refuse to start on mismatch —
    a routine upstream upgrade must not become an outage. Record that choice in
    the code comment.
15. Each pin bump cites the fixture from Phase 1 that proves the wire shape was
    re-checked at that version.

Commit at the end of the phase.

### Phase 4 — per-provider optimisations

16. **F5, prewarm re-arm (acpagent: grok, and any other acpagent provider).**
    Move `defer p.EnsureWarm()` off session creation. Re-arm when the session
    goes idle, so a ~3.8 s agent spawn no longer runs concurrently with the
    user's first turn. Assert ordering in a test rather than by timing.
17. **F2, `available_commands` dedupe.** Suppress an emission whose command
    list is identical to the last one sent for that session. grok's source
    states it bumps its generation counter even when the list is unchanged, so
    the dedupe belongs here. Assert a session that receives N identical updates
    emits one event.
18. **F1, Codex probe cost.** The MADR 0136 probe went 120 ms → 1400 ms and
    performs network reachability checks. Either narrow it to the
    `auth.credentials` check without the network probes, lengthen the cache
    window, or move it fully off any phone-triggered path — decide with a
    measurement, and state the residual cost.
19. **F4, inline ws handlers.** Move the remaining inline handlers
    (`session.list`, `session.set_mode`, `session.set_config`,
    `session.cancel`, `session.pending_asks`, `oauth.cancel`) to
    `dispatchAsync`, or document why each is safe to keep inline. One blocking
    handler currently stalls every later message on that connection.

Commit at the end of the phase.

### Phase 5 — verify on the reporting host

20. `make install`, then prompt `hi` on kilo and grok from the phone. Record
    prompt → first token from the new Phase 2 instrumentation.
21. Compare against the MADR's baseline (0.6-2.9 s historical, 5-18.6 s
    regressed) and against the direct-engine measurement (0.80-0.97 s for
    kilo). State plainly whether the gap closed, partially closed, or did not.
22. Confirm no turn is silent: every prompt produces either output or a
    surfaced error.

### Phase 6 — attribute the remaining prompt weight

Only after the above, because until delivery is fixed and turns are measured,
any prompt-weight change is unattributable.

23. **Start with `cache.write: 0`, which is now the highest-value question.**
    The corrected measurements show two populations, not a spread: a turn
    either pays a ~14,000-token uncached prefill or 99 tokens against a
    14,336-token cache read. Production showed `cache.write: 0` on every
    observed turn, so no turn there is ever warm. Establish why, because a cache
    that works removes most of the latency and the cost without removing any
    capability — and without touching the model, which the accepted constraint
    forbids.
24. Only then run the prompt-weight A/B: with and without the `magictools` MCP
    server, with and without the two kilo plugins. Record input tokens and
    first-token latency per arm, and separate cold from warm turns explicitly —
    conflating them is what produced two wrong conclusions in this record.
25. Report the attribution to the owner with numbers. **Do not remove an MCP
    server or plugin without approval** — that is their capability, not a
    performance knob this plan may turn unilaterally. **Do not pin or swap a
    model**, even if an arm shows the default is the dominant cost: the
    accepted constraint is that providers run on their own default model
    unless the user specifies otherwise. Report the number and stop.

## Verification

```bash
make pre-add-check
go test ./internal/... ./cmd/... -count=1
make vet
make lint
```

Host checks after Phase 5:

```bash
grep -a "turn latency\|prompt_accepted" ~/Library/Logs/mcremote/mcremote.err.log | tail -20
python3 - <<'PY'
import json,glob,os,datetime
# prompt -> first output, from live sessions, per the MADR's method
PY
```

### Acceptance criteria

* Every measurement in this plan separates **cold** from **warm** turns and
  says which it is. A number that does not is not evidence.
* Any new diagnostic harness is proven to distinguish success from failure
  before its output is trusted — the failure that produced the withdrawn
  Phase 0.
* No turn is silent: every accepted prompt ends in output or a surfaced error.
* The per-turn record reports first-token latency and, where available, token
  counts, for kilo and grok.
* All five providers have a `KnownGoodVersion` equal to the installed version,
  each backed by a Phase 1 fixture.
* A grok session receiving repeated identical command lists emits one
  `available_commands` event, verified against the 301-event session shape.
* The prewarm re-arm is proven by test to happen after the first turn, not at
  session create.
* The Codex probe's cost on the `AuthStatus` path is measured and stated.
* Phase 6 reports prompt-weight attribution with numbers, and recommends
  rather than removes.
* `git diff --stat` touches only files named in Scope.

## Rollout and Rollback

**Rollout.** Daemon-side only; effective on the next restart. No phone update
is required by any phase. Phases 0-4 are independently committable and each is
independently revertable.

**Rollback.** Revert the phase commits. The pins are constants and the
instrumentation is additive, so a revert restores prior behaviour exactly.

**Sequencing note.** Phase 0 is withdrawn. Phase 6 must not run before Phase 2,
or its arms cannot be compared — and its arms must separate cold from warm
turns. Phases 1, 3 and 4 are independent and may land in any order.

**What this plan will not do.** It will not remove the operator's MCP server or
plugins, will not change or pin a provider's default model (an accepted
constraint of the MADR, not merely a scoping choice), and will not upgrade or
downgrade a provider binary. Where those turn out to be the dominant cost,
Phase 6 reports the number and the decision stays with the owner.

## Execution Record

Not started. `status: proposed` — awaiting approval to execute.
