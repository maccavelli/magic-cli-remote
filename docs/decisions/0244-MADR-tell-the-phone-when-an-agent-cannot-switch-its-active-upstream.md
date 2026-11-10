---
status: accepted
date: 2026-11-10
decision-makers: Project Owner
consulted: "this repository at the commit after 0243 P5 (master, level with origin/master), read from code: internal/provider/auth.go, acpagent/acpagent.go, httpagent/httpagent.go, kilo/upstream.go, auth_conformance_test.go, internal/ws/server.go and credential_write_test.go, internal/protocol/messages.go, docs/guides/protocol-v1.md, apps/mobile/lib/features/settings/provider_detail_screen.dart, apps/mobile/lib/data/protocol/models.dart and apps/mobile/test/provider_detail_screen_test.dart; 0074, 0083 and 0214's MADRs; 0179's PLAN P4"
informed: 0179's PLAN (its P4 waits for this record)
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Tell the phone when an agent cannot switch its active upstream

## Context and Problem Statement

[0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md) D14 lets the phone move an
agent to another configured upstream without re-authenticating. The daemon offers it through
`provider.UpstreamSwitcher` (`internal/provider/auth.go:243-248`), and the phone shows an "Active upstream" control
whenever two or more upstreams are configured (`provider_detail_screen.dart:256-271`). Nothing else gates it.

Whether an agent can switch is not what its Go type says:

* **F1. Both shared transports declare the method for every agent.** `acpagent.Provider.SetActiveUpstream`
  (`acpagent.go:402-408`) returns `provider.ErrAuthUnsupported` when the Spec sets no `SetActiveUpstream` hook, and
  `httpagent.Provider.SetActiveUpstream` (`httpagent.go:324-334`) does the same when the dialect has no
  `UpstreamSwitchDialect`. So every ACP and HTTP agent passes the `UpstreamSwitcher` type assertion.
* **F2. Only kilo can switch.** `git grep SetActiveUpstream` finds the Spec field and its nil check, kilo's dialect
  (`internal/provider/kilo/upstream.go:22`), and nothing that sets the ACP hook. grok, opencode and gobble cannot
  switch. opencode lost the switch when it moved to ACP: 0214's MADR lists the active upstream as "not exposed" over ACP
  (its surface table, the `SetActiveUpstream` row).
* **F3. The daemon trusts the type.** `handleSetActiveUpstream` (`internal/ws/server.go:2584-2610`) asserts
  `UpstreamSwitcher` and calls it; for grok, opencode or gobble the call answers `ErrAuthUnsupported`, which reaches the
  phone as "unsupported for this provider" (`writeAuthErr`, `server.go:2792-2793`).
* **F4. The phone cannot tell.** `ProviderAuthPayload` (`internal/protocol/messages.go:798-802`) carries `status`,
  `active_upstream` and `upstreams`, and no switch flag. An opencode host with two configured upstreams shows the
  control, and a tap answers "Could not switch upstream: unsupported for this provider". This is read from the code;
  it has not been reproduced on a phone.
* **F5. The test checks the type side only.** `TestProviderAuthInterfaceConformance`'s comment says so, and expects a
  switcher for opencode and grok. gobble's registration (0179's PLAN P4 step 5) expected "no switcher", and the test
  answered true, which is how F1 to F4 were found.

Device auth had the same shape, and [0083-MADR-provider-auth-activation-and-layout-gaps.md](0083-MADR-provider-auth-activation-and-layout-gaps.md)
D4 fixed it with `provider.DeviceAuthCapable` (`auth.go:113-119`), which both transports implement and
`deviceAuthWired` (`server.go:2351-2358`) reads before promising a device flow.

## Decision Drivers

* The phone offers only what works, for every agent, with no Dart branch on a provider id.
* One answer for whether an agent can switch, read by the daemon, its tests and the phone.
* The wire stays additive: an older daemon, or an older phone, behaves as today.
* The same pattern as `DeviceAuthCapable`, so the two auth capabilities read alike.

## Considered Options

* **A.** A capability probe on the provider, read by the daemon, and an additive flag that hides the phone's control.
* **B.** The probe and an early daemon refusal only; the phone unchanged.
* **C.** The phone hides the control except for agents it names.
* **D.** Split the shared transports' types, so only a switching agent's provider has the method.

## Decision Outcome

Chosen option: "A", because it is the only option that stops the phone offering a switch that fails, for every agent,
without naming one, and it repeats the probe pattern 0083 D4 already uses.

### D1. The probe

`provider.UpstreamSwitchCapable { SupportsUpstreamSwitch() bool }` in `internal/provider/auth.go`, beside
`DeviceAuthCapable`, documented the same way. `acpagent.Provider` answers `spec.SetActiveUpstream != nil`;
`httpagent.Provider` answers whether its dialect is an `UpstreamSwitchDialect`.

`provider.UpstreamSwitchWired(p any) bool` is the one reading: false when p is not an `UpstreamSwitcher`; the probe's
answer when p has one; true otherwise, for a provider that implements the method without a probe (as a test double
does). The daemon and the conformance test both call it.

### D2. The daemon refuses before calling

`handleSetActiveUpstream` asks `UpstreamSwitchWired` first, and refuses with today's "unsupported" / "provider cannot
switch upstream" without calling the provider. A provider that can switch is handled as today.

### D3. The wire flag

`ProviderAuthPayload` gains `upstream_switch_unsupported`, a bool with `omitempty`. `authStatePayload` sets it true
when `UpstreamSwitchWired` is false. An agent that can switch sends nothing new, so its payload is byte-identical to
today's, and an older daemon's absent field reads as "may switch", which is today's behaviour. `protocol-v1.md`
documents it beside `provider.auth_status`.

### D4. The phone

`ProviderAuthInfo` reads `upstream_switch_unsupported` (absent is false). `provider_detail_screen.dart` shows the
"Active upstream" control only when two or more upstreams are configured and the flag is not set. No provider id is
named.

### D5. What this changes for each agent

| Agent | Can switch | Before | After |
| :--- | :--- | :--- | :--- |
| kilo | yes | control shown with two upstreams; works | unchanged |
| opencode | no (F2) | control shown with two upstreams; fails | hidden; the flag is sent |
| grok | no | shown only with two upstreams; would fail | hidden; the flag is sent |
| codex | no (no method) | shown only with two upstreams; refused | hidden; the flag is sent |
| gobble | no | not registered yet (0179 P4) | registered with the control hidden |

The conformance test gains a column for `UpstreamSwitchWired`: kilo true; opencode, grok, codex and gobble false.

### D6. 0179

0179's PLAN P4 waits for this record. Its gobble conformance case then states the type side (true, as for opencode
and grok) and `UpstreamSwitchWired` false, which is what P4 step 5's "no switcher" meant. 0179's MADR records this as
amendment A8.

### Consequences

* Good, because the phone never offers a switch the agent cannot make, for any agent, and says nothing new for kilo.
* Good, because the daemon, the test and the phone read one probe, as for device auth.
* Good, because the wire change is one omitted-when-false field; older phones and daemons behave as today.
* Neutral, because opencode and grok users with two configured upstreams lose a control that never worked over ACP.
* Bad, because a third optional interface is needed beside the method it qualifies; a provider that adds
  `SetActiveUpstream` on a shared transport must also answer the probe, or `UpstreamSwitchWired` keeps saying false.

### Confirmation

1. The conformance test's new column: kilo true; opencode, grok and codex false. Seen failing with `acpagent`'s probe
   answering true.
2. `set_active_upstream` to a provider whose probe answers false is refused "unsupported" and never reaches the
   provider; one that can switch is handled as today (`TestSetActiveUpstreamOverWire` unchanged). Seen failing with
   the check removed.
3. The auth payload carries `upstream_switch_unsupported: true` for a provider that cannot switch, and omits it for one
   that can. Seen failing with the flag not set.
4. The phone: two configured upstreams and the flag give no control; two without it give the control (the existing
   test); a payload from an older daemon, with no field, gives the control. Seen failing with the flag ignored.
5. `protocol-v1.md` documents the field.
6. The Go and Dart gates pass, and CI passes on the push.

## Pros and Cons of the Options

### A. A probe, an additive flag, and the phone hides the control

* Good, because it fixes the phone for every agent that cannot switch, with no id named.
* Good, because it follows 0083 D4.
* Neutral, because it touches the provider, both transports, the daemon, the protocol and the phone, in two phases.

### B. The probe and an early refusal only

* Good, because it is Go-only and small.
* Bad, because the phone still shows the control, and a tap still fails, only earlier.

### C. The phone names the agents that can switch

* Good, because it is Dart-only.
* Bad, because it branches on a provider id, which this repository does not do in Dart, and goes stale when an agent
  gains or loses the switch.

### D. Split the transports' types

* Good, because the type assertion would then be true only for an agent that can switch.
* Bad, because Go methods are fixed per type: every Spec-driven agent would need a second provider type, or a
  wrapper, for one method. That is a large change across both transports for what a probe answers.

## More Information

* The PLAN: [0244-PLAN-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md](0244-PLAN-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md).
* Found by [0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P4, the deviation of
  2026-11-10; the owner chose a probe, carried to the phone, in a record of its own, with 0179's P4 waiting for it.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md):
  where opencode's switch became "not exposed".
* Not in scope: making grok, opencode or gobble able to switch. An agent that gains a switch sets its Spec hook, and
  the probe then answers true with no other change.
