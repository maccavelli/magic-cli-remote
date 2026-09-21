---
status: proposed
date: 2026-09-21
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0163 — The Codex provider's jank is stale pins and a dead drift gate

Implements [0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md](0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md)
decisions D1–D19, closing findings F1–F35.

## Goal

Observable states, not activities:

1. A `writeStdin` approval arriving from Codex is presented as terminal input,
   never as a command, and a test proves the two render differently (**F22**).
2. `go test ./internal/provider/codex/` fails if any notification the embedded
   manifest declares has no route — so the 75-vs-82 class of gap cannot recur
   silently (**F12**).
3. `make live-codex-contract` **passes** on the installed 0.155.1 binary, and
   fails on a removed method, a renamed method, a newly required field, a
   stable→experimental demotion, or a method our code references that no longer
   exists — while a purely additive upstream release produces a warning and a
   machine-readable diff, not a red build (**F1**).
4. Re-pinning to a new Codex release is a documented command with no source edit
   (**F2**), and the three version pins in the package agree with each other and
   with the binary in use (**F3**).
5. `thread/read` is no longer called with `includeTurns: true`; transcript history
   is paged through `thread/turns/list` / `thread/items/list`, and no
   `deprecationNotice` is earned on a normal session (**F19**).
6. The phone distinguishes "out of credit" from "rate limited", and renders a
   `misalignment` refusal with a one-tap continuation (**F23**).
7. On Windows, the engine's environment always carries an explicit `USERNAME`,
   nothing in the tree ever calls `windowsSandbox/setupStart`, and the operator
   documentation states the `icacls` repair Codex does not provide (**F28–F30**).
8. Every workaround upstream has *not* fixed carries evidence that it is still
   necessary at 0.155.1, so the next audit does not re-derive it (**F31, F32**).

## Scope

### In scope (the only files any phase may touch)

```text
P1  internal/provider/codex/callbacks.go            approval kind decode + terminal-input rendering
    internal/provider/codex/callbacks_p2_test.go    kind cases
    internal/provider/codex/permission_test.go      rendering assertions
P2  internal/provider/codex/routing.go              the 7 missing routes; table rename
    internal/provider/codex/session.go              handler for the routed families (authRecovery already exists)
    internal/provider/codex/routing_test.go  (new)  manifest-vs-table completeness test
    internal/provider/codex/authrecovery_test.go    drive through routeNotification, not handleNotification
P3  internal/provider/codex/items.go                functionCallOutput registration
    internal/provider/codex/item_test.go            enumeration refreshed to 0.155.1
P4  internal/provider/codex/contract_generate_test.go   literals become inputs; stability from source
    internal/provider/codex/contract.go             manifest loading unchanged shape, new fields if needed
    scripts/codex-contract.ps1               (new)  the documented re-pin command
    docs/ops-codex-contract.md               (new)  how to re-pin, and the schema-vs-source rule
P5  internal/provider/codex/live_contract_test.go   two-mode drift check
    internal/provider/codex/contract.go             breaking-vs-additive classification helper
P6  internal/provider/codex/testdata/0.155.1/**     regenerated manifest, fixtures, source-watch
    internal/provider/codex/contract.go             embed paths
    internal/provider/codex/version.go              KnownGoodVersion
    internal/provider/codex/testdata/0.149.1/**     removed after P6 verification
P7  internal/provider/codex/collaboration.go        exact experimental-rejection detection
    internal/provider/codex/capabilities.go         capability re-probe instead of a permanent latch
    internal/provider/codex/diff.go                 re-probe for gitDiffToRemote
    internal/provider/codex/fast_personality.go     re-probe for thread/settings/update
P8  internal/provider/codex/threads.go              paginated history as the primary path
    internal/provider/codex/session.go              resume/fork excludeTurns + backwards cursors
    internal/provider/codex/threads_p6_test.go      paging assertions
P9  internal/provider/codex/session.go              rateLimitExceeded vs usageLimitExceeded; misalignment
    internal/provider/codex/turn_complete_test.go   error-class assertions
    internal/event/*.go                             only if a new error class needs a field
P10 internal/provider/codex/provider.go             explicit USERNAME in the engine env; identity records the path
    internal/provider/codex/config.go               managed_daemon_proxy refusal reworded, still disabled
    internal/provider/codex/launch.go               same
    internal/procutil/spawnsites_test.go            static ban on windowsSandbox/setupStart
    docs/ops-windows-install.md                     sandbox ACL repair + the two deny sources
P11 internal/provider/codex/execution.go            thread/shellCommand timeoutMs
    internal/provider/codex/runtime.go              McpServerStatus.runtimeStatus / toolsError
P12 internal/provider/codex/device_auth.go          evidence note: still destructive at 0.155.1
    internal/provider/codex/provider.go             evidence note: job objects still required
    internal/provider/codex/settings.go             advisory-vs-enforced managed policy
P13 internal/provider/codex/capabilities.go         advertise the negotiated surface
    internal/ws/liveness.go                         source of the advertised list
    apps/mobile/**                                  only if the advertised shape changes
```

### Out of scope

Named so the boundary is not mistaken for an oversight: `thread/attachment/*`,
`thread/timeline/list`, `turn/settings/update`, `userVerification/*`, realtime
voice, the managed daemon on Windows as an *enabled* transport, and any answer to
the Codex-remote-control product question. All are in Deferred with reasons.

Also out of scope: any change to Codex's own installation, config or sandbox
state on the owner's host. This plan reads that state and documents repairs; it
does not perform them.

## Stability rule

Every phase ends with, in order:

```bash
make pre-add-check FILES="<the phase's Go files>"
go test ./internal/provider/codex/            # plus any other package the phase touched
go test -race ./internal/provider/codex/
make ci-windows
```

Phases that touch files compiling on Linux (P1–P9, P11–P13) additionally run the
WSL lane: `go build ./... && go test ./internal/provider/... ./internal/procutil/`
under Ubuntu-24.04. P10 touches Windows-only behaviour and needs
`make ci-windows` plus a `GOOS=linux` build.

Commit discipline: one phase, one commit, message written by the hook
(`git commit --no-edit`). `git push` and tags need an explicit instruction in the
same turn. `make live-codex` and `make live-codex-contract` are run where a phase
names them; `make live-codex-turn` and `live-codex-review` spend tokens and are
run only at the acceptance of the release that changes turn behaviour (P8, P9).

## Cross-cutting contracts

* **C1 — No test is weakened to pass.** Where a phase must change an existing
  assertion (P3's enumeration, P6's pinned data, P8's history expectations), the
  new assertion is at least as strong, and the test body states in a comment why
  the old one was wrong. Precedent: 0159 P19 replaced a security assertion and
  compensated it with a stronger integration test in the same commit.
* **C2 — No new hardcoded protocol inventory.** Anything that enumerates methods,
  notifications or item types derives it from the embedded manifest or the
  generated schema. This is the contract most at risk: P2's route test is easiest
  to write as a second hand-maintained list, which is exactly how F12 happened.
* **C3 — The phone's wire shapes do not change before P13.** P1, P2 and P9 may
  change string *values* the phone renders, never field names or the event schema,
  so releases 1 and 2 need no mobile change.
* **C4 — Evidence is updated, never deleted.** A comment saying "observed on
  codex-cli 0.146.0" becomes "observed on 0.146.0; still true at 0.155.1
  (<evidence>)". Removing the version citation is forbidden: those citations are
  what made this audit possible (**F17**).
* **C5 — `windowsSandbox/setupStart` is never called.** Enforced by a static test,
  because the damage it caused on 2026-09-19 is unrepairable by Codex itself
  (**F30**).
* **C6 — No production code reads Codex's on-disk state DB, rollout files or
  session files.** Their formats are undetermined (MADR open question 5); the
  protocol is the only supported reader.
* **C7 — Every phase leaves `make ci-windows` green.** A red Windows gate at any
  commit is a stop, not a follow-up.

## Dependency and delivery order

```text
release 1  P1 ──► P2 ──► P3                          consent + silent drops, tiny diffs
release 2  P4 ──► P5 ──► P6 ──► P7 ──► P10 ──► P11 ──► P12   machinery, hygiene, evidence
release 3  P8 ──► P9 ──► P13                          transcript, error UX, advertised surface
```

P6 depends on P4 (the generator must accept inputs) and P5 (the gate must tolerate
additions, or P6 cannot be verified green). P13 depends on P6, because the
negotiated surface is only meaningful against a current manifest. P8 and P9 are
independent of each other but share the turn path, so they ship in the same
release with one acceptance pass. Nothing in release 1 depends on anything else,
which is why it goes first.

## Implementation Steps

### P1 — A stdin-injection approval says so (D1; closes F22)

Ground truth, measured from the installed binary's stable schema bundle:
`CommandExecutionApprovalKind` is `enum ["command","writeStdin"]`, described
*"Distinguishes a command approval from input sent to an existing terminal."*;
`CommandExecutionRequestApprovalParams.kind` is `allOf $ref` that enum with
`"default": "command"` and the note *"Defaults to `command` for older servers."*
It is **stable**, so no `experimentalApi` dependency, and `kind` is **not** in
`required` (`['itemId','startedAtMs','threadId','turnId']`).

1. `callbacks.go`: add `Kind string \`json:"kind"\`` to the anonymous `common`
   params struct (the block ending at `:92`).
2. In the `case "item/commandExecution/requestApproval":` arm (`:97-103`), branch
   on `common.Kind`:
   * `"writeStdin"` → a new `callbackTerminalInput callbackKind = "terminal_input"`
     (added beside the six at `:19-24`), `cb.tool = "terminal-input"`, and a
     `cb.detail` that names it as input to an already-running terminal rather
     than a command to run. The decision vocabulary is **unchanged** — the same
     `CommandExecutionApprovalDecision` enum applies — so `cb.allowedDecisions`
     keeps its existing derivation from `availableDecisions` with the same
     fallback.
   * `""` or `"command"` → today's behaviour, unchanged. The empty case is the
     schema's documented default and must be treated as `command`, not as
     unknown.
   * any other value → treat as `command` **and** log at Warn with the literal,
     so a future third kind is visible without breaking an approval.
3. `session.go:1862`'s event keeps its shape; only `ToolName` and `Detail` differ
   (**C3**). Confirmed safe: the mobile app does not switch on `callbackKind` —
   a repo-wide grep of `apps/mobile/lib` for the existing kind strings
   (`granular_permission`, `mcp_elicitation`, `legacy_apply_patch`) returns
   nothing.
4. Auto-approval (`session.go:1807-1854`) is deliberately left alone: a
   `writeStdin` request under an auto-approve mode is still auto-answered, because
   the user's contract is "auto means you will not be asked" (MADR 0044 D6). What
   changes is only what a *prompted* approval says. Record this explicitly in the
   comment so it reads as a decision.

**Verification (P1).**

```bash
go test ./internal/provider/codex/ -run 'Callback|Approval|Permission' -v
```

New cases: a `kind: "writeStdin"` params decodes to `callbackTerminalInput` with a
tool name that differs from the command case; `kind` absent decodes to
`callbackCommand`; `kind: "somethingNew"` decodes to `callbackCommand` and logs.
Mutation that must fail: delete the `Kind` field from the struct and the
writeStdin case must report the command rendering.

### P2 — Every declared notification has a route, enforced (D5; closes F12)

1. `routing.go`: add the seven missing entries —
   `modelProvider/authRecoveryStarted` and `modelProvider/authRecoveryCompleted`
   → `notificationRouteSession` (handlers already exist at
   `session.go:1717-1756`, and both are **stable** at 0.155.1);
   `thread/attachment/updated`, `mcpServer/event/stream/notification` and the
   three `thread/realtime/item/*` → `notificationRouteProvider` with a handler
   that logs and drops **explicitly**, since we do not consume them yet. An
   explicit drop is not the same as an unrouted one: it is declared, tested, and
   greppable.
2. Rename `codex01491NotificationRoutes` (`:22`) to a version-neutral name. The
   name is one of the three stale 0.149.1 references (**F3**), and P6 must not
   have to rename it again.
3. `routing_test.go` (new): read the **embedded manifest** (`C2` — never a second
   hand-written list), and for every `server_notifications` entry in both
   surfaces assert the route table has a non-`notificationRouteUnknown` entry.
   Failure message names the missing methods. This is the test that makes F12
   structurally impossible to repeat.
4. `authrecovery_test.go:28-51`: drive the notification through
   `routeNotification` rather than calling `handleNotification` directly, so the
   test would have caught the missing route. This is a **strengthening**, and the
   comment says so (**C1**).

**Verification (P2).**

```bash
go test ./internal/provider/codex/ -run 'Routing|Notification|AuthRecovery' -v
```

Mutations: remove one route entry → `routing_test.go` names it; revert
`authrecovery_test.go` to call `handleNotification` → it passes with the route
removed, demonstrating why step 4 is required.

### P3 — `functionCallOutput` stops being dropped (D6; closes F13)

Ground truth: `ThreadItem` has 19 variants at 0.155.1; `items.go:15-33` names 18
across `itemsRenderedAsTools` (8) and `knownNonToolItems` (10). The missing
variant's schema requires `['id','name','output','type']` with an optional
`namespace`, and carries no description.

1. Decide the registry by what it is, and record the reasoning: it is the
   *output* half of a function call whose call side already renders a tool card,
   so it belongs in `knownNonToolItems` — acknowledged, no second card. If a live
   observation (step 3) shows it arriving **without** a preceding rendered call,
   it moves to `itemsRenderedAsTools` instead. The plan does not guess: step 3
   settles it before the commit.
2. `item_test.go:15-33`: refresh the enumeration from the installed version. The
   old list is the 0.145.0 probe set; the new assertion derives the expected set
   from the embedded manifest where possible (**C2**) and states in a comment that
   the previous list was version-frozen (**C1**).
3. Observation step, `make live-codex` with one real function-calling turn under
   `live_codex_turn` if needed: confirm whether a `functionCallOutput` arrives
   paired with a rendered call. If tokens are not to be spent, fall back to the
   Rust source's emission site and record that as the basis instead.

**Verification (P3).**

```bash
go test ./internal/provider/codex/ -run 'Item' -v
```

The unknown-item counter must not increment for `functionCallOutput`. Mutation:
remove the entry and the enumeration test fails naming it.

### P4 — Re-pinning becomes a command, and notification stability stops being a lie (D2, D4; closes F2, F4)

Today `contract_generate_test.go` needs four env vars **and** six hand-edited
literals: `CodexVersion` (`:30`), `BinarySHA256` (`:31`), `Commit` (`:45`) and
three output paths (`:53-55`); the `implemented` set is a switch (`:118-132`), and
the capability allowlists (`:138-171`), response-schema overrides (`:247-262`),
fallback map (`:311-328`) and security heuristic (`:330-345`) are all in the test
body.

1. Turn the six literals into inputs, read from the environment beside the
   existing four: `CODEX_CONTRACT_VERSION`, `CODEX_CONTRACT_BINARY_SHA256`,
   `CODEX_SOURCE_COMMIT`, `CODEX_CONTRACT_OUT_DIR`. Absent → `t.Fatal` naming the
   variable, never a silent default, because a silent default is how a manifest
   would be written under the wrong version number.
2. Derive `BinarySHA256` rather than trusting the operator to paste it: the
   generator already has `resolveBinaryIdentity` (`provider.go:1068-1111`)
   computing exactly that. Accept the env var as an override for a cross-host
   capture, and fail if both are present and disagree.
3. Derive the `implemented` set from the code instead of the switch: the union of
   the methods our non-test files send and the notifications the route table
   routes. That is the same data P2's test already reads, so there is one source
   (**C2**) and `F5`'s server-request misclassification fixes itself.
4. **Notification stability (D4).** The schema bundles cannot answer this —
   Codex's `filter_experimental_schema` prunes experimental client and server
   *methods* and *fields* but never notifications (**F4**), so both bundles list
   all 82. Add a fifth input, `CODEX_SOURCE_TREE`, pointing at a Codex checkout at
   the pinned commit, and derive the experimental notification set by scanning the
   `server_notification_definitions!` invocation (`app-server-protocol/src/
   protocol/common.rs`, invocation at `:1907`) for entries carrying
   `#[experimental`. Expected at 0.155.1: **62 stable / 22 experimental**. The
   generator asserts the counts it derived and fails if the scan finds zero
   experimental entries, because zero is the failure signature of a macro-syntax
   change rather than a legitimate result.
   *If the scan proves brittle in review, the fallback is to capture the set once
   per pin into the source-watch manifest and assert its contents there — drift
   then shows up as a manifest diff rather than silently.*
5. `scripts/codex-contract.ps1` (new): the documented one-command capture. It
   resolves `codex` on PATH, refuses to continue if the resolved path is not the
   one the daemon uses (**F: two installs on this host**), runs
   `codex app-server generate-json-schema --out` twice (stable and
   `--experimental`), locates `ServerRequest.json` beside each composite schema as
   the generator expects (`:78-84`), and invokes the generator with every input
   set. It prints the resolved version, path and SHA before doing anything.
6. `docs/ops-codex-contract.md` (new): the procedure, plus the two rules this
   audit paid for — **the schema blobs are committed, not introspected**, so a
   source tree at a different commit than the binary yields a stale contract
   silently; and method literals live at `oneOf[].properties.method.enum[0]`,
   `enum` not `const`.

**Verification (P4).**

```bash
go test ./internal/provider/codex/ -run Contract -v            # generator unit paths
CODEX_CONTRACT_GENERATE=1 ./scripts/codex-contract.ps1 -DryRun # prints inputs, writes nothing
```

The generator must refuse: a missing input; a SHA that disagrees with the resolved
binary; a source tree whose commit does not match `CODEX_SOURCE_COMMIT`; an
experimental-notification scan returning zero.

### P5 — The gate reports additions and fails on breakage (D3; closes F1)

1. `contract.go`: add a classification helper that, given the pinned manifest and
   a freshly generated surface, partitions the difference into **breaking** —
   a method removed, a method renamed (absent here, present there, and our code
   references the absent one), a field newly added to a `required` array, a
   stable→experimental demotion, or a method our code sends that the new surface
   does not declare — and **additive**: everything else.
2. `live_contract_test.go`: default mode fails only on breaking drift, and on
   additive drift logs a compact diff (counts plus the added method names) and
   passes. `CODEX_CONTRACT_EXACT=1` restores today's `reflect.DeepEqual` behaviour
   for use at release pinning. The `EvidenceMatched` assertion (`:62-64`) moves
   behind the exact mode, since a SHA mismatch is the normal state on any host
   that is not the pinning host.
3. Keep the four content-free catalog probes (`:69-87`) exactly as they are: they
   cost no tokens and they are the only live proof the engine answers.
4. The known-good version check stays a warning, per `version.go:14-16`'s
   standing instruction not to harden it into a gate without a decision record.
   This record does not change that.

**Verification (P5).**

```bash
make live-codex-contract                       # must PASS on the installed 0.155.1
CODEX_CONTRACT_EXACT=1 make live-codex-contract # must FAIL until P6 re-pins
```

Mutations, each against a synthetic surface: delete a method → breaking, fails;
add a method → additive, passes with a logged diff; add an entry to a `required`
array → breaking, fails; move a method from stable to experimental → breaking,
fails.

### P6 — Re-pin to 0.155.1 (D2; closes F3)

1. Run P4's command against the binary the daemon drives
   (`%APPDATA%\npm\codex.cmd` → `0.155.1`), writing
   `internal/provider/codex/testdata/0.155.1/{manifest,fixtures,source-watch-manifest}.json`.
2. `contract.go:121-128`: point the three `//go:embed` directives at the new
   directory. `version.go:17`: `KnownGoodVersion` → `0.155.1`. Any remaining
   `0.149.1` identifier — the notification table name was already handled in P2 —
   is renamed or removed.
3. Delete `testdata/0.149.1/`. Its only readers are the embeds and tests updated
   here; keeping it invites the next reader to diff against the wrong baseline.
   The 0.147.0 and 0.152.1 fixture sets stay: they pin *behaviour* observed at
   those versions (wire fixtures, doctor output), which is different from pinning
   the *surface*.
4. Expect these deltas in the regenerated data, and assert them in the commit
   message body as a checkable record: stable client requests 95 → 102,
   notifications 75 → 82 (62 stable / 22 experimental after D4),
   experimental client requests 150 → 164, server requests unchanged at 10 / 11,
   and `implemented` recomputed from code rather than the old switch.

**Verification (P6).**

```bash
make live-codex-contract                        # passes
CODEX_CONTRACT_EXACT=1 make live-codex-contract # now ALSO passes, on this host
make live-codex                                 # engine start, thread lifecycle, both sandbox shapes
go test ./internal/provider/codex/
```

The exact mode passing is the proof that the pin, the binary and the schema all
agree for the first time since 0.149.1.

### P7 — Rejection detection becomes exact, and a capability can come back (D9, D10; bounds F11, closes F15)

1. `collaboration.go:65-77`: key `isExperimentalInitRejection` on the structured
   `data.capability == "experimentalApi"` field. Keep the message match, but
   demote it to a logged fallback that records the message it matched, so a
   reworded upstream error is visible in logs rather than silently changing
   behaviour. Measured ground truth at 0.155.1: the message is
   `"<method> requires experimentalApi capability"`, produced at
   `message_processor.rs:970-974`; the `-32600` code is unchanged.
2. `capabilities.go:232-241`: give `Disable` a companion re-probe. A capability
   disabled by `-32601`/`-32602` records the generation and the reason; the next
   natural boundary — a new engine generation, or an explicit refresh — clears it
   so the feature is retried instead of staying off for the process lifetime. The
   three latch sites (`diff.go:57-63`, `fast_personality.go:145-159`,
   `collaboration.go:168-182`) use it.
3. `diff.go`: note in the comment that `gitDiffToRemote` is a **v1** method
   deliberately absent from every schema bundle
   (`export.rs:75-76`, stripped at `:1429-1430`) yet still dispatched
   (`message_processor.rs:1773`), so its absence from the contract inventory is
   expected and not a bug to "fix" by deleting the call (**F14**, **C4**).

**Verification (P7).**

```bash
go test ./internal/provider/codex/ -run 'Experimental|Capabilit|Diff|Personality' -v
```

Cases: a rejection carrying `data.capability` is detected without the regex; a
rejection with neither the field nor the word is **not** detected, and that is
asserted, because today's regex would have guessed; a disabled capability is
re-enabled at the next generation.

### P8 — Transcript history moves to the paginated contract (D7; closes F19, finishes MADR 0141)

Ground truth: `thread/items/list`, `thread/turns/list` and `thread/revert` are
**stable** at 0.155.1 (promoted by #40673); `thread/start` now defaults durable
threads to `historyMode: "paginated"` (#40677); `thread/read` with
`includeTurns: true` emits a `deprecationNotice` on every call (#40676), and so
does `thread/resume`/`thread/fork` requesting full history. The notice is
advisory — verified: it is sent *after* the full response is computed — so this is
a migration, not a repair.

1. `threads.go`: make `thread/turns/list` + `thread/items/list` the primary
   history path with their cursors, and stop passing `includeTurns: true` on
   `thread/read` (`:930`). Keep `thread/read` for metadata.
2. `session.go:463`: `thread/resume` passes `excludeTurns: true` and consumes
   `itemsBackwardsCursor` / `turnsBackwardsCursor` from the response; the same for
   `thread/fork` (`:960`). All five of those fields left `#[experimental]` at
   0.155.1, so no opt-in is required for them.
3. Retire the local-search fallback's dependence on full hydration where paging
   now serves it (`threads.go:290-306`), leaving the fallback for genuinely
   unsupported stores. Its `Truncated: true` honesty is kept.
4. Capability gating: the three methods' manifest entries lose their
   `experimental` classification and their `fallback: rpc:thread/read` in P6's
   regenerated data; this phase makes the code match.
5. Assert no `deprecationNotice` is received during a normal session in the live
   test — the notice is routed to the provider (`routing.go`), so the test can
   observe it rather than infer it. That assertion is the proof the migration is
   complete, and it is the one most likely to be dropped as "flaky" (see
   acceptance).

**Verification (P8).**

```bash
go test ./internal/provider/codex/ -run 'Thread|History|Paging|Resume|Fork' -v
make live-codex            # no deprecationNotice on a normal session
make live-codex-turn       # one billed turn: history survives a real turn + resume
```

### P9 — The phone tells quota apart from throttling, and offers the continuation (D8; closes F23)

Ground truth: `CodexErrorInfo` gained `rateLimitExceeded` distinct from
`usageLimitExceeded` (#44492 maps `insufficient_quota`,
`credit_balance_exhausted`, `organization_spend_limit_exceeded`,
`project_spend_limit_exceeded`, `organization_usage_limit_exceeded` to quota, while
`rate_limit_exceeded`/`slow_down` keep retry semantics); `TurnError` gained
`misalignment` → `{errorType, detailedExplanation, steer: {message}}`, where
`steer.message` is documented as *"Instruction to submit as the next turn's user
input if continuation is confirmed."*

1. `session.go`: map the two error infos to distinct classes so "you are out of
   credit" cannot render as "back off and retry". The existing limit plumbing
   (`noteProviderLimit`, `:2316-2320`) already exists for the throttling case; the
   quota case must not reuse it, because its advice is the opposite.
2. Decode `misalignment` and carry `detailedExplanation` plus `steer.message`.
   Per the schema's own words a substantive explanation is required before
   offering continuation, so the continuation is offered **only** when
   `detailedExplanation` is non-empty.
3. `codexStopReason` (`:2246-2256`) keeps passing unmapped enums through, but a
   `misalignment` turn end is now a mapped class rather than visible noise
   (`:2238-2241` documents today's behaviour).
4. If a new event field is needed for the continuation, it is additive and
   documented in `internal/event` — the phone ignores unknown fields, so release 3
   can ship server-first (**C3** is released from here deliberately, since this is
   release 3).

**Verification (P9).**

```bash
go test ./internal/provider/codex/ -run 'Error|Limit|Turn' -v
```

Fixtures for both error infos and for a `misalignment` with and without an
explanation. Mutation: collapse the two error classes into one and the test must
fail naming the advice difference.

### P10 — Windows hygiene: the environment, the ban, and the repair (D11, D12, D18; bounds F20, F28; closes the doc half of F29/F30)

1. `provider.go:550-553`: add an explicit `USERNAME` to the engine's environment.
   At 0.149.1 Codex derived the ACL grant principal from
   `std::env::var("USERNAME").unwrap_or_else(|_| "Administrators")`; 0.155.1 reads
   the OS token instead (`setup.rs:351`/`:1097`, comment *"Sandbox launchers can
   filter USERNAME"*). We pass it anyway, as defence in depth for a downgrade or
   an older engine, and the comment cites the incident of 2026-09-19.
2. `provider.go`'s binary identity records the **resolved path** alongside the
   version and SHA (**D18**), because two Codex installs at different versions
   coexist on the reference host: `%APPDATA%\npm` at 0.155.1 and
   `%LOCALAPPDATA%\OpenAI\Codex\bin\…` at 0.154.0-alpha.6.2.
3. `internal/procutil/spawnsites_test.go`: extend the static ban to assert no
   non-test file contains the literal `windowsSandbox/setupStart` (**C5**).
   Occurrences in `testdata/` and the contract inventory are allowed by path.
4. `config.go:116-118` and `launch.go:46-48`: the `managed_daemon_proxy` refusal
   stays, but its reason is corrected. It is no longer *"Codex's daemon is
   Unix-only"* — that was true at 0.149.1 and is false at 0.155.1 — it is *"we
   have no live coverage for it on Windows"*, with the three constraints recorded
   in the comment: a non-elevated launch whose host permits detached children, the
   **108-byte AF_UNIX path limit including the terminator** (so a short
   `CODEX_HOME`), and no per-client environment isolation. Enabling it is
   Deferred.
5. `docs/ops-windows-install.md`: a Codex sandbox section stating what Codex does
   not provide — there is no repair or uninstall subcommand
   (`clean_up_packaged_windows_sandbox` has one caller, the MSIX uninstall event),
   the only record of applied denies is
   `$CODEX_HOME\.sandbox\deny_read_acl_state.json`, an empty or deleted state file
   orphans every deny ACE it recorded, and the repair is `takeown` + `icacls` by
   hand. Name the two places a deny can come from — the permission profile's
   `[permissions.<name>.filesystem]` Deny globs and
   `%ProgramData%\OpenAI\Codex\requirements.toml` — and say that
   `C:\ProgramData` is world-writable-ish, so that directory's owner and ACL are
   worth checking, since Codex measures standard-user pre-creation but does not
   prevent it (#44284).

**Verification (P10).**

```bash
go test ./internal/procutil/ -run SpawnSites -v
go test ./internal/provider/codex/ -run 'Env|Identity|Config' -v
make ci-windows
markdownlint-cli2   # clean on the changed page
```

Mutation: add `windowsSandbox/setupStart` to a non-test file and the ban must name
the file and line.

### P11 — Lean on Codex where Codex now does the work (D14; closes F25)

1. `execution.go:376`: pass `timeoutMs` on `thread/shellCommand`. Codex's
   documented semantics, which the code comment must state: omitted or null means
   one hour, zero means an **immediate** timeout rather than unlimited, negative is
   refused (#41384). Our own timeout stays as the outer bound; the inner one stops
   a wedged command from occupying the engine.
2. `runtime.go:253-258`: surface `McpServerStatus.runtimeStatus` and `toolsError`
   so an unhealthy MCP server can be explained rather than merely listed.
3. Do **not** add retry logic for `Retry-After`: Codex now shares those deadlines
   across enrolment, refresh, pairing and handshakes and adds jitter without
   shortening the server's deadline (#44311). Record that as the reason no code is
   added here, so a future reader does not add a second backoff.

**Verification (P11).**

```bash
go test ./internal/provider/codex/ -run 'Execution|Shell|Runtime|Mcp' -v
```

### P12 — The workarounds that stay, with evidence (D15, D17, D19; closes the doc half of F31/F32/F34)

Per **C4**, each comment gains a "still true at 0.155.1" clause with its evidence.

1. `device_auth.go:21-33` and `auth.go:94-98`: still destructive —
   `cli/src/login.rs:335` still calls `clear_existing_auth_before_login()` →
   `logout_with_revoke()` before the device flow begins, the only `login.rs` diff
   in the range is Bedrock-related, and `device_code_auth.rs` does not appear in
   the tree diff at all. Also record the live defect this audit surfaced: the
   superseded destructive path is the one production uses, because
   `StartOwnedDeviceAuth` needs `NewCoordinated` and production builds with `New`
   (`provider.go:127-133`). Wiring it is Deferred, named.
2. `provider.go`'s job-object comments: still required —
   `JOB_OBJECT_LIMIT_BREAKAWAY_OK` remains the default so
   `CREATE_BREAKAWAY_FROM_JOB` grandchildren still escape, and a tree-wide grep at
   0.155.1 finds no `SetConsoleCtrlHandler`, `GenerateConsoleCtrlEvent`,
   `CTRL_BREAK_EVENT`, `CTRL_C_EVENT` or `CREATE_NEW_PROCESS_GROUP` anywhere.
   Codex's new SIGTERM handler and 45 s watchdog are `cfg(unix)`.
3. `provider.go:236-241`: `model/list` still rejects a missing `params` object at
   0.155.1 (requiredness unchanged), so the empty-object send stays. Note the two
   upstream fixes that reduce the blast radius: identity-scoped catalog caches
   (#43897) and identity-less legacy entries now treated as cache misses (#43906).
4. `settings.go`: state which managed requirements Codex **enforces** (approval
   policies, approvals reviewers, sandbox modes, permission profiles, web-search
   modes, `additional_developer_instructions`) and which are **advisory** —
   `allowBrowserAndComputerUse`, `browserUse`, `inAppBrowser` are destructured and
   discarded in `TryFrom<ConfigRequirementsWithSources>`
   (`config_requirements.rs` ~`:1678-1683`). If we ever surface those
   capabilities we enforce the denial ourselves (**D17**). Also record that
   `additional_developer_instructions` is injected as its own developer-role
   message that our client cannot suppress.
5. Add a note where the contract inputs are documented (P4's page) that four
   `ConfigRequirements` fields — `allowedApprovalsReviewers`, `hooks`, `network`,
   `application` — are `#[experimental]` and therefore absent from every generated
   type while still being sent, so codegen would silently lose admin-managed hooks
   and network policy including `network.header_injections` (**F35**).

**Verification (P12).** Comment-only and documentation-only; the gate is
`go build ./... && go vet ./...`, `markdownlint-cli2`, and a reviewer check that no
version citation was deleted (**C4**).

### P13 — The phone is told what this binary can do (D16; closes the user-visible half of F3)

1. `capabilities.go:250-269`: `SurfaceCapabilityIDs` derives the advertised list
   from the negotiated engine surface — the manifest as the schema of record,
   intersected with what `initialize` actually accepted (`eng.experimental`) and
   with capabilities not currently disabled.
2. `internal/ws/liveness.go:177-178`: send that, not the raw embedded list.
3. Compatibility: the shape does not change, only the contents, so an older phone
   keeps working. If the contents can shrink, the phone must already tolerate a
   missing capability — verify that in `apps/mobile` before shipping, and if it
   does not, the mobile change ships first.

**Verification (P13).**

```bash
go test ./internal/provider/codex/ ./internal/ws/ -run 'Capabilit|Liveness' -v
cd apps/mobile && flutter analyze && flutter test
```

Cases: with `experimental == false`, no experimental-only capability is
advertised; a disabled capability disappears and reappears after P7's re-probe.

## Verification (whole plan)

```bash
make pre-add-check                 # every tracked Go file
go test ./... && go test -race ./...
make ci-windows && make ci-windows-smoke
make live-codex && make live-codex-contract
CODEX_CONTRACT_EXACT=1 make live-codex-contract
make live-codex-turn && make live-codex-review    # release 3 acceptance only; spends tokens
cd apps/mobile && flutter analyze && flutter test
# WSL lane
go build ./... && go test ./internal/provider/... ./internal/procutil/
```

### Acceptance criteria (mapped to the MADR's Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | A `kind: "writeStdin"` approval renders with a different tool name and detail than a command approval; absent `kind` renders as a command | D1 |
| A2 | An unknown `kind` value still produces a usable approval, and logs the literal | D1 |
| A3 | Every notification the manifest declares has a route; the test names any that do not | D5 |
| A4 | `modelProvider/authRecovery*` reach the session through `routeNotification` | D5 |
| A5 | `functionCallOutput` no longer increments the unknown-item counter | D6 |
| A6 | The contract generator refuses a missing input, a mismatched SHA, or a mismatched source commit | D2 |
| A7 | The generator derives 62 stable / 22 experimental notifications, and fails if it derives zero experimental | D4 |
| A8 | `make live-codex-contract` passes on 0.155.1; `CODEX_CONTRACT_EXACT=1` also passes after P6 | D3, D2 |
| A9 | A synthetic removal, rename, new required field, or demotion each fail the default gate; an addition passes with a logged diff | D3 |
| A10 | No `0.149.1` identifier remains in the package | D2 |
| A11 | An experimental rejection is detected from `data.capability` alone | D9 |
| A12 | A capability disabled by `-32601` is retried at the next engine generation | D10 |
| A13 | No `thread/read` call passes `includeTurns: true`, and a normal live session receives no `deprecationNotice` | D7 |
| A14 | Resume passes `excludeTurns` and consumes both backwards cursors | D7 |
| A15 | Quota and throttling render as different classes; `misalignment` offers a continuation only with an explanation | D8 |
| A16 | The engine environment carries an explicit `USERNAME` | D11 |
| A17 | No non-test file references `windowsSandbox/setupStart`, asserted statically | D11 |
| A18 | The Windows page documents the `icacls` repair, the orphaned-ACE trap, and both deny sources | D11 |
| A19 | The `managed_daemon_proxy` refusal cites coverage, not a platform limit, and records the three constraints | D12 |
| A20 | Binary identity records the resolved path | D18 |
| A21 | `thread/shellCommand` carries `timeoutMs`, with the zero-means-immediate semantics in the comment | D14 |
| A22 | Every retained workaround carries a "still true at 0.155.1" citation; none lost its original version citation | D15 |
| A23 | The advertised capability list reflects the negotiated surface, and shrinks when `experimental` is false | D16 |

**A13 is the criterion most likely to be quietly dropped.** It asserts the
*absence* of a notification over a live session, which is the shape reviewers call
flaky; and it is the only check that proves the migration actually happened rather
than being merely written. **A7 is second**: deriving zero experimental
notifications looks like success and is the exact failure signature of an upstream
macro change.

## Rollout and Rollback

* **Release 1 — patch (P1–P3).** No protocol re-pin, no wire-shape change, no
  mobile change. Rollback is the binary rollback `update` already performs.
* **Release 2 — minor (P4–P7, P10–P12).** Re-pins the contract and changes engine
  negotiation. The risky half is P6: if the regenerated manifest is wrong, engine
  start fails closed (`provider.go:871-875` fails start on an invalid manifest),
  which is loud rather than silent. Rollback is the previous binary; the embedded
  manifest travels with it.
* **Release 3 — minor (P8, P9, P13).** Touches transcript, resume, error rendering
  and the advertised surface. Acceptance includes one billed turn
  (`make live-codex-turn`) and one billed review, plus a manual phone pass:
  resume a long thread, exhaust nothing, and confirm a `writeStdin` approval and
  an error class read correctly. Rollback: binary rollback restores the previous
  advertised surface, since it is computed at runtime.
* Tags and pushes need an explicit instruction in the same turn, per the repo
  rule. Each release cuts after a green CI run, with `make ci-windows-smoke`
  before the tag per MADR/PLAN 0145.

## Deferred (named, so they are not mistaken for oversights)

* **`thread/attachment/{add,list,remove}` + `thread/attachment/updated`** —
  stable, idempotent by `(attachmentType, identityKey)`, cursor-paginated, and
  usable on an unloaded thread. The highest-value new surface for this product: it
  is server-side per-thread storage we would otherwise build. Deferred because it
  is a feature with a mobile design, not a fix, and it deserves its own pair.
* **`turn/settings/update`** (experimental) — retarget a live turn's model,
  effort, summary, service tier and approvals reviewer without restarting it.
  Needs the dual failure handling the audit measured: a synchronous `-32600`
  pre-flight rejection *and* a later `EventMsg::Error` when the apply races a
  managed-requirements change.
* **`thread/timeline/list`** (experimental) — items, realtime items and turn
  boundaries in one canonically ordered paginated call, replacing the stitching
  P8 leaves in place. Worth doing *after* P8, not instead of it.
* **`userVerification/*`** (experimental, 5 methods) — device-bound P-256
  credentials making the phone a biometric approver, wired to
  `mcpServer/elicitation/request.userVerification`. Strategically the closest
  thing in the delta to this product's thesis, and a project in its own right.
* **The managed app-server daemon on Windows** — newly possible (**F20**), still
  off. Enabling it needs live coverage, the 108-byte socket-path constraint
  designed for, and the daemon settings written to disk before bootstrap
  (`{"updater":{"autoUpdateEnabled":false}}`, a chosen `shutdownGraceSeconds`, and
  a deliberate `thread_unload_delay_secs`), because auto-update otherwise restarts
  the server under a connected phone with no in-band notice (**F24**, **D13**).
* **Wiring `StartOwnedDeviceAuth`** — the isolated-home device flow that supersedes
  the destructive sidecar path exists but is unreachable in production, because it
  requires `NewCoordinated`. Named in P12; fixing it is a credentials change with
  its own risk.
* **The Codex remote-control question** — whether Codex's own
  `remote-control`/pairing/WS-auth stack is a competitor, a transport to adopt or
  an integration to expose. An owner-level product decision, MADR open question 1.
* **On-disk format verification** — `thread-store` moved ~9100 lines including a
  rollout-migration rewrite; formats are undetermined. **C6** keeps us off them
  until a pass says otherwise.
* **Realtime voice** (`thread/realtime/*`, 11 notifications) — only if voice ships.
* **Purge's missing local teardown (F16)** — a live 60 s daemon hang and a 70 s
  phone timeout on every codex session end. It is **not** re-planned here because
  it already has a record: MADR 0146, still `status: proposed`. This plan touches
  neither `Purge` nor `session/manager.go`'s call site, so 0146 can be approved and
  executed independently. It is arguably more urgent than several phases above,
  and the only reason it is not here is that duplicating a pending decision is how
  records rot.
* **The live-coverage holes (F18)** — no live test exercises the `unix_ws` or `ws`
  transports or either WS auth mode (we sign an HS256 JWT with `iss: mcremote`,
  `aud: codex-app-server` that nothing has confirmed Codex accepts), engine
  replacement, any real approval round trip, the 1195-line execution surface, or
  `config/batchWrite`, which writes the user's `config.toml`. Closing them is a
  test-infrastructure project, not a phase; P1 and P2 add unit coverage for the two
  defects that mattered, and the rest is named here so the gap is a decision.
* **An unshallowed Codex clone** — would close the 2026-09-04 → 09-06 attribution
  gap. Only worth it if we need PR history for that window.
