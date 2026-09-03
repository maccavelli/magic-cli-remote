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

### Phase 0 — restore event delivery for kilo 7.5.6

The correctness bug. Everything else is latency; this is a turn that never
arrives.

1. Reproduce off-phone first, so the fix has a failing case: isolated daemon
   (own port, own data dir, own token) + kilo enabled, prompt `hi`, assert the
   client receives no event within 30 s while the engine's
   `/session/<id>/message` shows an assistant reply. This is the harness the
   MADR amendment used; rebuild it in `scripts/` or as a Go test, not as a
   scratch file.
2. Instrument the httpagent SSE pump enough to answer one question: does it
   receive `message.part.delta` for the session it created, and if so where is
   the frame discarded? Add debug logging at the pump boundary — there is none
   today, which is why this took a packet capture to narrow.
3. Answer the recorded open question first: the delivering production turn
   logged `agent=code`, the silent isolated runs logged `agent=""`. Test an
   empty agent explicitly. If it is causal, that is the bug; if not, the
   remaining candidate is the agent-session → local-session binding.
4. Fix the cause found, not the symptom. Do not add a timeout that fabricates a
   turn end.
5. Regression test: a stub engine that emits kilo 7.5.6's exact frame sequence
   (captured in Phase 1) must produce assistant text on the session, and must
   fail against the pre-fix code.

Commit at the end of the phase.

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

23. Run the isolation A/B on prompt weight: with and without the `magictools`
    MCP server, with and without the two kilo plugins, pinned model vs default.
    Record input tokens and first-token latency per arm.
24. Investigate `cache.write: 0`. Working prompt caching would cut cost and
    latency together without removing any capability, and is preferable to
    deleting tools if it is achievable.
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

* **Phase 0 is the gate.** A `hi` on kilo 7.5.6 produces assistant text on the
  phone. The pre-fix reproduction fails and the post-fix one passes, with both
  outputs recorded.
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
instrumentation is additive, so a revert restores prior behaviour exactly —
including, for Phase 0, the silent-turn bug.

**Sequencing note.** Phase 0 must land before Phase 5 is meaningful, and
Phase 6 must not run before Phase 2, or its arms cannot be compared. Phases 3
and 4 are independent of both and may land in any order.

**What this plan will not do.** It will not remove the operator's MCP server or
plugins, will not change or pin a provider's default model (an accepted
constraint of the MADR, not merely a scoping choice), and will not upgrade or
downgrade a provider binary. Where those turn out to be the dominant cost,
Phase 6 reports the number and the decision stays with the owner.

## Execution Record

Not started. `status: proposed` — awaiting approval to execute.
