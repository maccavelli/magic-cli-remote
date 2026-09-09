---
status: proposed
date: 2026-09-08
---
<!-- markdownlint-configure-file {"MD004": {"style": "asterisk"}} -->

# Make mcremote message size configurable

## Context and Problem Statement

Operators can configure mcrelay's message limit but cannot configure the
mcremote application connection's limit. Raising the relay limit alone does
not let a phone send larger requests to mcremote. This record proposes a new
configuration surface, extending the framing work in MADR 0056 and capability
negotiation in MADR 0068.

### Findings

Read-only inspection at commit `199c5c4` on 2026-09-08 established:

* F1: `internal/ws/server.go:300` fixes inbound messages at `1 << 20` bytes;
  `server.go:559` passes that value to `SetReadLimit`.
* F2: `internal/ws/server.go:3485` fixes serialized outbound responses at the
  same size. `writeJSON` checks it at line 3504 and normally sends a typed
  error when the original response is too large. This is not a universal
  outbound limit: `Broadcast` calls `writeBytes` directly at line 375, and
  `writeBytes` at line 3534 has no size check. Replay also calls it directly
  at lines 991 and 1002. These files were untouched during inspection; this
  bypass predates the proposed work. It is established by code inspection,
  not a newly executed regression test.
* F3: `internal/ws/liveness.go:154` advertises the outbound constant as
  `caps.max_frame_bytes`, so changing enforcement alone would misreport it.
* F4: `apps/mobile/lib/data/protocol/frame_budget.dart:7` defines a fixed
  1 MiB request budget. The client checks it in
  `apps/mobile/lib/data/ws/mcremote_client.dart:3049`; the chat screen checks
  it for images, audio, and sending at lines 879, 967, and 1339. Error text
  also hard-codes the limit. `models.dart:106` already parses the server's
  advertised value, but these checks do not use it.
* F5: `internal/relay/config.go:16` sets a 16 MiB ceiling and line 55 sets a
  1 MiB default. `ResolvedLimits` defaults nonpositive values and clamps
  oversized ones. mcremote's separate `relay.max_frame_bytes` field lives in
  `internal/config/config.go:108`; its validation permits zero or
  4096–16777216 bytes. The bridge applies it at
  `internal/relayhost/client.go:307` to outer tunnel messages. The bridge
  copies TCP chunks into WebSocket messages; this is a different framing
  layer from the inner application messages governed by F1–F4.
* F6: `internal/config/config.go:126` already groups daemon limits;
  `LimitsConfig.Resolved` at line 924 resolves defaults. `load.go:367`
  registers limit defaults, and `internal/daemon/daemon.go:419` passes
  resolved settings into `ws.Options`.
* F7: `internal/cli/service/template_parity_test.go` requires configuration
  keys in the service seed and all three mcremote example files. Adding a
  struct field alone would leave configuration discovery incomplete.
* F8: `internal/provider/codex/transport.go:17` independently limits the
  daemon-to-provider transport to 1 MiB. Application frame configuration
  does not change provider, attachment, history, or operation-specific limits.

## Decision Drivers

* Expose the same operator-facing key as mcrelay.
* Preserve the default size for existing installations.
* Make enforcement and advertised capabilities agree in both directions.
* Let mobile request checks follow the active server's configuration.
* Retain a finite ceiling and explicit oversize failures.

## Considered Options

* One application message limit, enforced and advertised by mcremote.
* Separate configurable inbound and outbound application limits.
* Reuse `relay.max_frame_bytes` for application messages and tunnel chunks.

## Decision Outcome

Chosen option: "One application message limit, enforced and advertised by
mcremote", because one value matches the existing capability field and makes
the connection's request and response budget predictable.

This is a proposal awaiting owner approval of the associated implementation
plan. Its contracts are:

* D1: Add `limits.max_message_bytes` and
  `MCREMOTE_LIMITS_MAX_MESSAGE_BYTES`. Default to 1048576 bytes (1 MiB);
  zero also resolves to that default. Accept explicit values from 4096 to
  16777216 bytes inclusive. Reject negative, smaller nonzero, and larger
  values during config validation. The key, default, and ceiling match
  mcrelay; validation deliberately follows mcremote's existing bridge rule
  rather than silently clamping operator input. The floor also avoids
  impractically small authentication and error budgets.
* D2: Resolve the limit at startup, pass it through `ws.Options`, and use
  it for inbound reads, serialized outbound responses, and the common
  outbound enqueue path. Zero-valued `ws.Options` retains the 1 MiB default.
  Configuration changes require a daemon restart.
* D3: Advertise the resolved value in the existing `caps.max_frame_bytes`
  field for authentication and pairing. No protocol version change is needed.
  The unit is serialized UTF-8 envelope bytes, including JSON and base64
  expansion, excluding WebSocket framing and TLS overhead.
* D4: Preserve typed `bad_payload` errors for oversized direct responses.
  Enforce the same budget on broadcasts and replay; report a bounded typed
  error and log the rejected type/size instead of silently losing an event.
  An oversized error must terminate without recursive error generation;
  close the connection if no bounded error can be delivered. Do not truncate
  or split protocol envelopes. Preserve shared-buffer fan-out and queue limits.
* D5: The mobile client's current valid advertised limit drives the final
  send guard, image/audio preflight, prompt preflight, and displayed limit.
  Fall back to 1 MiB before negotiation, on v1 connections, or for missing,
  malformed, or out-of-range advertised values. Reconnect and host switching
  must use the new connection's capabilities rather than a cached old budget.
* D6: Keep `relay.max_frame_bytes` independent and explain both framing
  layers in configuration documentation. Operators matching larger relay
  outer messages still configure that bridge cap to match mcrelay. Increasing
  the inner application limit does not automatically change outer chunk size.
  Document provider and operation limits so a larger transport budget is not
  mistaken for permission to send arbitrarily large prompts or attachments.

### Consequences

* Good, because operators can raise or lower the application limit without
  rebuilding mcremote, and updated phones honor the selected value.
* Good, because broadcasts and replay can no longer bypass the configured
  outbound bound.
* Neutral, because old phones retain their own 1 MiB outbound preflight until
  upgraded; v1 has no capability negotiation for this setting.
* Bad, because larger allowed messages increase encoding, decoding, and queue
  memory costs. The existing queue bounds message count, not aggregate bytes;
  the 16 MiB ceiling is not a process memory budget.
* Bad, because lowering the limit can reject existing large responses or
  replay entries; the client must receive an explicit error for those cases.

### Confirmation

Verify YAML and environment loading, range validation, default compatibility,
real WebSocket boundaries, direct and shared outbound paths, and auth/pair
capabilities. Verify mobile UTF-8/base64 accounting, larger and smaller
negotiated budgets, legacy fallback, reconnect, and composer feedback.
Observe the new regression tests fail against verified broken copies before
relying on their passing results. Exact steps and gates are in the associated
[implementation plan](0154-PLAN-configurable-mcremote-message-size.md).

## Pros and Cons of the Options

### One application message limit, enforced and advertised by mcremote

* Good, because it fits the existing capability and the requested config name.
* Good, because one resolved value prevents directional drift.
* Bad, because operators cannot tune request and response sizes independently.

### Separate configurable inbound and outbound application limits

* Good, because workloads can tune each direction independently.
* Bad, because the current single capability field cannot describe both;
  this requires additional protocol and client design without a demonstrated need.

### Reuse `relay.max_frame_bytes` for application messages and tunnel chunks

* Good, because it avoids another configuration field.
* Bad, because a relay-specific key would control direct connections and
  conflate two different framing layers and existing configuration contracts.

## More Information

* [MADR 0056](0056-MADR-mcremote-android-protocol-stack-audit.md): exact framing.
* [MADR 0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md): capabilities.
* [MADR 0115](0115-MADR-mcrelay-go126-audit-and-hardening.md): bridge configuration.
* [MADR 0090](0090-MADR-config-template-completeness.md): template completeness.
