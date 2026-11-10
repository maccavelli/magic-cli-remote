---
status: complete
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

## Execution record

### P0 — 2026-11-10: the records

* This pair, written `proposed`, with 0179's A8 and its PLAN's P4 deviation entry. `check_records.py --write-index`
  (the index gained this pair's two rows only); `make check-records markdownlint-docs`: exit 0; the redact preview: 0
  files. Staged alone, and committed by the owner (`95653d7b`).
* The owner: "staged records committed. proceed". This plan is approved and `in-progress`. The MADR was not named, so
  it stays `proposed` until the owner accepts it (P3 step 2).

### P1 — 2026-11-10: the probe, the refusal and the flag

Scripts and logs are in the session scratchpad (`p1_0244_planted.py`, `0244-p1-*.log`).

* **Start state (C1).** HEAD `95653d7b`, level with what the owner committed; 0179 P4's files unstaged, as C1 names,
  and untouched by this phase.
* **Steps 1 and 2.** `provider.UpstreamSwitchCapable` and `provider.UpstreamSwitchWired` (`internal/provider/auth.go`);
  `SupportsUpstreamSwitch` on `acpagent.Provider` (the Spec's hook is set) and `httpagent.Provider` (the dialect is an
  `UpstreamSwitchDialect`).
* **Step 3.** `handleSetActiveUpstream` refuses with the existing "unsupported" / "provider cannot switch upstream"
  when `UpstreamSwitchWired` is false, before calling the provider.
* **Step 4.** `ProviderAuthPayload.UpstreamSwitchUnsupported` (`upstream_switch_unsupported`, `omitempty`).
  *Difference from the step's text:* `authStatePayload` gains a `switchOK bool` beside its `deviceOK bool`, not the
  provider; both callers compute it with `UpstreamSwitchWired`, as they compute `deviceOK` with `deviceAuthWired`.
  The behaviour is the step's.
* **Step 5.** `docs/guides/protocol-v1.md`, under "Remote provider auth": the auth block's shape, the field, the
  older-daemon rule and the refusal.
* **Step 6, the tests.** `TestProviderAuthInterfaceConformance` gains `wantSwitchWired` (kilo true; opencode, grok and
  codex false). `internal/ws/credential_write_test.go` gains `TestSetActiveUpstreamRefusedByTheProbe` (a provider with
  the method and a probe answering false is refused, and its method is not called) and
  `TestAuthBlockSaysWhenTheSwitchIsNotWired` (`providers.list` with kilo-like, opencode-like and codex-like doubles:
  `true` for the two that cannot switch, no key for the one that can). The existing tests pass unchanged, among them
  `TestSetActiveUpstreamOverWire`, `TestSetActiveUpstreamUnsupported` and the `providers.list` tests.
* **Seen failing (C2),** a scratch clone of HEAD with this phase's eight files. Unplanted, exit 0. Each break, exit 1:

  | Break | Message |
  | :--- | :--- |
  | `acpagent`'s probe answers true | `UpstreamSwitchWired: got true, want false (MADR 0244 D5)` |
  | `httpagent`'s probe answers false | `UpstreamSwitchWired: got false, want true (MADR 0244 D5)` |
  | the refusal removed | `want the unsupported error, got ok null` (the log: `provider active upstream switched … provider=opencode`) |
  | the flag not set | `codex cannot switch; upstream_switch_unsupported = "null" (present false), want true` |
  | the flag always set | `kilo can switch, and its auth block carries upstream_switch_unsupported=true` |

  The first run scored the refusal break as missed though its test failed: the script looked for the test's second
  message, and the first check had already stopped it. The expected text was corrected and the breaks rerun; all five
  caught. The clone was removed.
* **The gates (C3),** on that clone, each exit 0: `make test`, `go test -race -count=1 ./...` (50 `ok` lines, no FAIL,
  panic or race line), `make lint`, `make staticcheck`, `make vet-tags`, and `make pre-add-check` on the seven Go files
  ("7 file(s) clean").
* `make check-records markdownlint-docs` and the redact preview, then **staged** by path for the owner's commit: the
  seven Go files (`internal/provider/auth.go`, `auth_conformance_test.go`, `acpagent/acpagent.go`,
  `httpagent/httpagent.go`, `internal/ws/server.go`, `credential_write_test.go`, `internal/protocol/messages.go`),
  `docs/guides/protocol-v1.md`, and this plan.

The owner committed P1 (`33bffce8`) and accepted the MADR: "accept madr 0244. committed staged, proceed". The MADR is
`accepted`.

### P2 — 2026-11-10: the phone

Scripts and logs are in the session scratchpad (`p2_0244_planted.py`, `0244-p2-*.log`, `0244-p2b-*.log`).

* **Start state (C1).** HEAD `33bffce8`; 0179 P4's files unstaged, as C1 names, and untouched.
* **Step 1.** `ProviderAuthInfo.upstreamSwitchUnsupported` (`lib/data/protocol/models.dart`), read as
  `upstream_switch_unsupported == true`, so an absent field is false. Nothing else builds or copies the class.
* **Step 2.** `provider_detail_screen.dart`: the "Active upstream" control needs two or more configured upstreams and
  the flag unset.
* **Step 3, the tests** (`test/provider_detail_screen_test.dart`): two configured upstreams with the flag show no
  control; the parse reads the flag, and reads an absent one, as an older daemon sends, as false. The test helper
  `providerWith` (`test/provider_test_fakes.dart`) gains `switchUnsupported`, false by default, so the existing switch
  tests send no flag and pass unchanged.
* **Seen failing (C2),** a scratch clone of HEAD with the four files. Unplanted, exit 0. Each break, exit 1:

  | Break | Failing test |
  | :--- | :--- |
  | the screen ignores the flag | `two configured upstreams on an agent that cannot switch: no control (MADR 0244 D4) [E]` |
  | the field is not read | `the auth block reads upstream_switch_unsupported; absent, as from an older daemon, is false (MADR 0244 D4) [E]` |
  | absent reads as unsupported | the same test |

  The clone was removed.
* **The Dart gates (C3),** on that clone. The first run's `flutter analyze` found one issue, mine:
  `unnecessary_import` for the `models.dart` import added to the screen test, whose names `app_providers.dart` already
  provides. The import was removed, and the whole run repeated: breaks caught as above, `dart format
  --output=none --set-exit-if-changed .` exit 0, `flutter analyze` "No issues found!", `flutter test` "+1523 ~3: All
  tests passed!" (two more than before, three skipped as before).
* `make check-records markdownlint-docs` and the redact preview, then **staged** by path for the owner's commit: the
  four Dart files, the MADR (`accepted`), and this plan. No Go file changed in this phase.

The owner committed P2 (`15b8d9d5`): "committed, proceed".

### P3 — 2026-11-10: close-out

* **Start state (C1).** HEAD `15b8d9d5`, three commits ahead of `origin/master` (this plan's P0, P1 and P2); 0179 P4's
  files unstaged, as C1 names. The pre-push disclosure guard over the three commits: exit 0.
* **Step 1, the MADR's Confirmation:**

  | Confirmation | Evidence |
  | :--- | :--- |
  | 1 the conformance column | `TestProviderAuthInterfaceConformance`: kilo `UpstreamSwitchWired` true; opencode, grok and codex false. Failed with `acpagent`'s probe answering true, and with `httpagent`'s answering false (P1) |
  | 2 the refusal | `TestSetActiveUpstreamRefusedByTheProbe`: refused, the method not called; `TestSetActiveUpstreamOverWire` unchanged. Failed with the check removed: `want the unsupported error, got ok null` (P1) |
  | 3 the flag on the wire | `TestAuthBlockSaysWhenTheSwitchIsNotWired`: `true` for the opencode-like and codex-like doubles, no key for the kilo-like one. Failed with the flag never set and with it always set (P1) |
  | 4 the phone | `provider_detail_screen_test.dart`: the flag hides the control; the existing two-upstream tests, which send no flag, show it; the parse reads an absent field as false. Three breaks caught (P2) |
  | 5 `protocol-v1.md` | "Remote provider auth": the auth block's shape, the field, the older-daemon rule and the refusal (P1 step 5) |
  | 6 gates and CI | the Go gates in P1 and the Dart gates in P2, each on a scratch clone of the commit, exit 0; CI on the owner's push of `15b8d9d5`, run 42841924687: `success`, every job that ran passing (Go test; Go linux/arm64, windows/amd64, darwin/arm64; Flutter analyze and test; Flutter test on Windows; live supervisors), the release and APK jobs skipped as on any push; the Docs workflow, run 42841923236: `success` |

* **Step 2.** Confirmation 1 to 6 met: this plan is `complete`; the MADR is `accepted` (P2). The index lists no
  status, so `check_records.py --write-index` leaves it unchanged; `make check-records markdownlint-docs`: exit 0; the
  redact preview: 0 files.
* **Step 3.** A dated note on 0179's PLAN, after its P4 deviation entry: P4 may resume.
* **Staged** for the owner's commit: this plan and 0179's PLAN.
