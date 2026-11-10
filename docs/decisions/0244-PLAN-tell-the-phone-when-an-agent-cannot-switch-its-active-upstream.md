---
status: proposed
date: 2026-11-10
associated-madr: "0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement telling the phone when an agent cannot switch its active upstream

Associated MADR: [0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md](0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md)

## Goal

When this plan is done:

* `provider.UpstreamSwitchWired` answers whether an agent can switch, from a probe both shared transports implement
  (MADR D1);
* the daemon refuses a switch the agent cannot make before calling it (D2);
* the auth payload says so in `upstream_switch_unsupported` (D3), and the phone hides the "Active upstream" control
  (D4);
* 0179's PLAN P4 can resume (D6).

## Scope

* **Go:** `internal/provider/auth.go` (the interface and `UpstreamSwitchWired`);
  `internal/provider/acpagent/acpagent.go` and `internal/provider/httpagent/httpagent.go` (the probe);
  `internal/provider/auth_conformance_test.go` (the new column); `internal/ws/server.go` (`handleSetActiveUpstream`,
  `authStatePayload`) and `internal/ws/credential_write_test.go`; `internal/protocol/messages.go`
  (`ProviderAuthPayload`).
* **Dart:** `apps/mobile/lib/data/protocol/models.dart` (`ProviderAuthInfo`),
  `apps/mobile/lib/features/settings/provider_detail_screen.dart`, `apps/mobile/test/provider_detail_screen_test.dart`.
* **Docs:** `docs/guides/protocol-v1.md`.
* **Records:** this pair; `docs/README.md`; 0179's pair (its A8 and its PLAN's deviation entry, written with P0).
* **Not in scope:** making any agent able to switch (MADR More Information).

## Conventions

* **C1. Start state.** 0179's PLAN P4 is in the working tree, unstaged and paused: `Makefile`, the four shipped
  configs, `docs/guides/config.md`, `internal/command/resolve_order_test.go`, `internal/config/prewarm_write.go` and its
  test, `internal/provider/registered/registered.go`, and the new `internal/provider/gobble/live_gobble_test.go`. None
  of them is a file of this plan. They are never staged, reverted or reformatted by it. `auth_conformance_test.go`, the
  one file both touch, is at HEAD: P4's gobble case was taken out on 2026-11-10 and returns in P4 after this plan.
* **C2. Seen failing.** Each new test fails on a planted break in a scratch clone, and the record quotes the failure.
* **C3. Gates on the commit, not the tree.** Because of C1, each phase's gates run on a scratch clone of HEAD with
  this phase's files copied in, so they check what will be committed: `make pre-add-check FILES=…` on the staged Go
  files, `make test`, `go test -race -count=1 ./...`, `make lint`, `make staticcheck`, `make vet-tags`; for Dart,
  `dart format --output=none --set-exit-if-changed apps/mobile`, `flutter analyze`, `flutter test`.
* **C4. Commits.** The agent stages by path; the owner commits with `git commit --no-edit`. Nothing is pushed by this
  plan; CI runs on the owner's push.
* **C5. Deviations** stop the phase, with a dated entry here and an amendment in the MADR where a decision or a fact
  changes.

## Implementation Steps

### P0 — the records

1. This pair, `proposed`; 0179's MADR amendment A8 and its PLAN's dated deviation entry (P4 paused, why, and the seed
   config added to P4's scope).
2. `check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview. **Stage** the four
   record files and `docs/README.md` only.

### P1 — the probe, the refusal and the flag (MADR D1 to D3, D5)

1. `provider.UpstreamSwitchCapable` and `provider.UpstreamSwitchWired` in `auth.go`, with doc comments naming 0083 D4
   and this record.
2. `acpagent.Provider.SupportsUpstreamSwitch` (`spec.SetActiveUpstream != nil`) and
   `httpagent.Provider.SupportsUpstreamSwitch` (the dialect is an `UpstreamSwitchDialect`).
3. `handleSetActiveUpstream`: `UpstreamSwitchWired` first, refusing with today's error code and message.
4. `ProviderAuthPayload.UpstreamSwitchUnsupported` (`upstream_switch_unsupported`, `omitempty`), set by
   `authStatePayload` from `UpstreamSwitchWired`. `authStatePayload` gains the provider as an argument, as it takes
   `deviceOK` today; its callers pass it.
5. `docs/guides/protocol-v1.md`: the field beside `provider.auth_status`, with the older-daemon rule.
6. **The tests:**
   * `auth_conformance_test.go`: a `wantSwitchWired` column, kilo true, opencode, grok and codex false;
   * `credential_write_test.go`: a provider with `SetActiveUpstream` and a probe answering false is refused and its
     method never called; `TestSetActiveUpstreamOverWire` and `TestSetActiveUpstreamUnsupported` unchanged;
   * a payload test: a provider that cannot switch sends `"upstream_switch_unsupported":true`; one that can sends no
     such key.

   Seen failing (C2): `acpagent`'s probe answering true; the refusal removed; the flag not set.
7. Gates (C3). **Stage.**

### P2 — the phone (MADR D4)

1. `ProviderAuthInfo.upstreamSwitchUnsupported`, read from `upstream_switch_unsupported`, false when absent.
2. `provider_detail_screen.dart`: the control needs two or more configured upstreams and the flag unset.
3. `provider_detail_screen_test.dart`: two configured upstreams with the flag show no control; the two existing
   switch tests pass unchanged (they send no flag, as an older daemon does). Seen failing with the flag ignored.
4. Dart gates (C3). **Stage.**

### P3 — close-out

1. The MADR's Confirmation 1 to 6, each with its evidence; CI on the owner's push.
2. This plan `complete`; the MADR `accepted` when the owner accepts it; the index regenerated; `make check-records`.
3. A dated note on 0179's PLAN that P4 may resume. **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 the conformance column | P1 | step 6 |
| 2 the refusal | P1 | step 6 |
| 3 the flag on the wire | P1 | step 6 |
| 4 the phone | P2 | step 3 |
| 5 `protocol-v1.md` | P1 | step 5 |
| 6 gates and CI | every phase | C3; P3 step 1 |

## Rollout and Rollback

* **Rollout:** ships with the next daemon and app. An older phone ignores the new field and shows the control as
  today; an older daemon sends no field and a new phone shows the control as today.
* **Rollback:** revert P2, then P1. P1 alone is safe: the daemon refuses earlier, and the phone, unchanged, still shows
  the control.
* **Risk:** low. kilo, the one agent that switches, sends a payload identical to today's.
