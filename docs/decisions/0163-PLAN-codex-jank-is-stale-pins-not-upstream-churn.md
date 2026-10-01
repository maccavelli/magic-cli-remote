---
status: in-progress
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
4. ~~Capability gating: the three methods' manifest entries lose their
   `experimental` classification and their `fallback: rpc:thread/read` in P6's
   regenerated data; this phase makes the code match.~~ **Wrong as written — see
   the 2026-09-21 deviation below.** P6 regenerated the data but not the
   hand-maintained allowlist that assigns stability, so the entries are still
   `experimental` with a fallback. This phase must first make
   `generatedCapabilities` derive stability from the schema bundles, then
   regenerate, and only then make the code match.
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
| A24 | `mcpServerStatus/list` surfaces `runtimeStatus` and `toolsError`; a healthy server acquires no error, and an engine predating both fields still decodes | D14 |
| A25 | Replay unwraps `ThreadItemEntry` so a paged transcript is not silently empty, and a page over the bound is refused | D7 |
| A26 | Replay requests `sortDirection: asc` explicitly, and sends no `cursor` on the first page | D7 |

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

## Amendment — 2026-09-21: P14, the live suite cannot go green on Windows

Discovered while verifying P6, which runs `make live-codex`. Out of scope for
P4–P6, so it is named here rather than smuggled into them; the owner approved
fixing it directly ("Fix the cleanup").

**What happens.** `make live-codex` fails on this host with

```text
TempDir RemoveAll cleanup: unlinkat …\TestLiveThreadStartSandboxShapestring_accepted…\001:
  The process cannot access the file because it is being used by another process.
```

No assertion fails: `object_rejected` and `unknown_variant_rejected` both pass, so
the sandbox param-shape contract (MADR 0044 Finding 5, 0163 F17) is intact. It is
the harness, and it predates this plan — it reproduces with every commit of
releases 1 and 2 stashed.

**Why.** A lifetime inversion. `liveEngine(t)` starts the engine for the *parent*
test and hands back `p.Shutdown`, which the parent runs from `defer`. The cwd for
each `thread/start` comes from `t.TempDir()` called on the *subtest*, so the
directory is removed when the subtest ends — while the engine is still running and
codex still has that directory as a live thread's working directory. On Windows an
open handle blocks removal; on Unix it does not, which is why CI never saw it.

Four sites share the pattern: `live_sandbox_test.go` (3) and
`live_turn_test.go:183` (1).

### P14 — a thread cwd outlives the engine that is using it

1. `live_helpers_test.go`: add `liveThreadCwd(t)`, which creates the directory
   with `os.MkdirTemp` and registers its own cleanup, plus a bounded retry around
   `os.RemoveAll`. Owning the cleanup rather than borrowing `t.TempDir()`'s makes
   the fix independent of whether the caller is a parent or a subtest, which is
   the actual defect — the ordering is easy to reintroduce.
2. A cleanup that still fails after the retry window **logs and does not fail the
   test**: a leftover temp directory is harness residue, not a product defect, and
   failing on it is what makes a live suite unrunnable. The log names the path and
   the error so a genuine process leak is still visible.
3. Replace the four `t.TempDir()` thread-cwd call sites with it. Other
   `t.TempDir()` uses — isolated `CODEX_HOME`s, schema output directories — are
   left alone: nothing holds them open.

**Verification (P14).**

```bash
make live-codex          # green, including string_accepted
go vet -tags live_codex ./internal/provider/codex/
go vet -tags live_codex_turn ./internal/provider/codex/
```

`live_turn_test.go` is under the billed `live_codex_turn` tag; its call site is
corrected and vetted, and exercising it waits for the release-3 acceptance pass
that already spends a turn.

**Acceptance (addition).**

| # | Criterion | MADR |
| --- | --- | --- |
| A24 | `make live-codex` passes on Windows; a thread cwd is removed after the engine stops, and an unremovable one logs rather than failing | — (harness) |

## Amendment — 2026-09-21: P9 and P11 corrected after re-verification

Checked before execution rather than during it: 27 plan premises against the
repo, the installed binary's schema and the Rust tree at `rust-v0.155.1`. 25
held. Three needed attention, and only one was a real plan error.

### P9, rewritten — the engine's error class is a string we currently ignore

The step as written assumed two new tagged variants. Measured, `codexErrorInfo`
is a **plain string** from a thirteen-value enum (MADR 0163's correction of the
same date), and nothing in our non-test Go reads it at all. So the work is not
"tell two variants apart" but "start decoding a field we have always dropped".

1. `session.go`: decode `TurnError.codexErrorInfo` as a string and map it. Treat
   the enum as **open** — the schema's own last value is `other`, and a value this
   build does not know must fall back to today's generic handling with a Debug
   line, never a dropped turn end.
2. Map at least these four to distinct user-visible advice, because each needs a
   different action and all four are free once the field is decoded:

   | value | what the phone should say |
   | --- | --- |
   | `usageLimitExceeded` | out of quota or credit — retrying will not help |
   | `rateLimitExceeded` | throttled — back off and retry |
   | `contextWindowExceeded` | the thread is too long — compact or start a new one |
   | `sandboxError` | the workspace sandbox failed, which on Windows is the ACL path in D11 |

   `sessionBudgetExceeded` and `unauthorized` are the next two worth a distinct
   message; the remaining values may share the generic error path until someone
   has a reason to split them.
3. `misalignment` is unchanged from the original step: decode
   `detailedExplanation` and offer `steer.message` as a one-tap continuation, only
   when the explanation is non-empty.
4. The existing limit plumbing (`noteProviderLimit`, `session.go:2316-2320`) is a
   **stderr scrape** for a silent 429 (MADR 0073 F1). It stays: it fires when no
   structured error arrives at all. But a `codexErrorInfo` that says
   `usageLimitExceeded` must not be routed through it, because its advice is the
   opposite of backing off.

**Verification (P9), revised.** Fixtures for each of the four mapped values plus
an unknown one, asserting the unknown falls back rather than disappearing; and one
`misalignment` case with and without an explanation. The mutation that must fail:
collapse `usageLimitExceeded` and `rateLimitExceeded` onto one class and the test
must name the advice difference.

### P11, precision — one call site, not the package

The premise "execution.go does not yet send `timeoutMs`" is false as stated:
`ExecSandboxed` (`execution.go:286`, `command/exec`) and `SpawnProcess` (`:656`,
`process/spawn`) already send it. The one that does not is `RunThreadShell`
(`:376`, `thread/shellCommand`), which is the call the step meant. Scope is
unchanged; the step now names the function so it cannot be read as package-wide.

`ThreadShellCommandParams.timeoutMs` is confirmed present, and its own
description carries the semantics the comment must state: *"Defaults to one hour
when omitted or null. Must be non-negative; zero requests an immediate timeout,
not unlimited execution."*

### P12, evidence — the sweep is now exhaustive

The console-signal claim is measured across 4,025 `.rs` files: zero production
occurrences, one test-only (`CREATE_NEW_PROCESS_GROUP` in
`codex-rs/utils/pty/src/windows_tests.rs`). The annotation P12 writes into
`provider.go` must say "none in production code; one in a pty test" rather than
"none anywhere", so the next reader who greps and finds it does not conclude the
note is stale.

### Unchanged, and confirmed by the same pass

P8's six premises all hold: `threads.go` still sends `includeTurns`, the three
promoted methods are stable, `excludeTurns` exists on both resume and fork, both
backwards cursors exist on the resume response, `Thread.historyMode` exists, and
`deprecationNotice` is routed so the migration's own assertion is observable.
P10's five and P13's three hold as written.

## Execution record (2026-09-20)

**Ran: P10, P11, P12** — commits `595831e`, `2ddbaa9`, `3b252f2`. Release 2 is now
complete (P4–P7 earlier, P10–P12 here). Release 3 (P8, P9, P13) is not started.

Gates per phase: `make pre-add-check FILES=...` (clean), `gofmt -l` (empty),
`go build ./...`, `go vet`, the affected package tests, and for P12
`markdownlint-cli2` compared against its own baseline.

### What the plan predicted incorrectly

Four premises written into the phase text were wrong or imprecise. All four were
caught by re-reading the source at execution time rather than by a test, which is
the argument for grounding each phase again at the moment it runs.

1. **P11 — `RuntimeMCPServer.Error` "had no writer".** It has one:
   `runtime.go:229`, from `mcpServer/startupStatus/updated`. The real defect was
   different and worse, so the comment now states it: `applyMCPStatusList`
   *replaces* `p.runtime.mcp` wholesale, so every catalog refresh discarded the
   error a notification had recorded and left a failed server looking merely
   unauthenticated. Reading `toolsError` narrows that loss; it does not close it,
   because the list stays authoritative.

2. **P11 — `timeoutMs` was to track the caller's `ctx` deadline.** Upstream's own
   doc comment forbids it: the timeout *"does not affect the immediate RPC
   acknowledgement"*, and `RunThreadShell` returns `Started: true` without waiting.
   The caller's context therefore bounds the acknowledgement, not the command, and
   binding them would have killed a legitimately long command at the ack timeout.
   Shipped as a named constant (`threadShellTimeout`, 10 minutes) instead.

3. **P12 — the managed-policy ENFORCED/ADVISORY table.** It listed
   `allowed_permission_profiles` as enforced, but
   `TryFrom<ConfigRequirementsWithSources>` discards it
   (`config_requirements.rs:1674`). Measured: **seven** fields are discarded, not
   three, and the function's own comment explains two of them as config-load
   values that are still honoured elsewhere. Calling `browser_use` /
   `in_app_browser` simply "advisory" was also unproven — there is a separate
   `InAppBrowserRequirementsToml`, and `mcp_tool_call.rs:1317` consults
   `confirmation_policies.browser_use`. The comment now states only the verified
   discard list and its consequence.

4. **P12 — `additional_developer_instructions` "capped at 10,000 tokens".** The
   limit is 10,000 (`MAX_MANAGED_DEVELOPER_INSTRUCTIONS_TOKENS`), but oversized
   policy is **rejected** with an `io` error, not capped — deliberately, per
   `managed_developer_instructions.rs:53`: *"Reject oversized policy rather than
   silently dropping part of its instructions."* A comment describing a cap would
   have had an operator looking for truncated instructions instead of a hard error.

### What the plan got right, and cheaply

P10's five premises and P12's console-signal sweep held exactly as written. The
sweep is worth restating because it came out stronger than the plan claimed: across
the Codex tree, the five console-signal APIs return **one** hit in total, inside a
Python string literal in `utils/pty/src/windows_tests.rs:142`. `login.rs:335` was
confirmed to sit in `run_login_with_device_code`, and the four
`configRequirements/read` experimental gates matched the documented list exactly.

### Acceptance criteria

A16, A17, A18, A19 (P10), A21 and the new **A24** (P11), A22 (P12) are met. A24 was
added during execution: P11's MCP half changed observable wire output and had no
criterion, which is the gap that lets a change ship unasserted.

Two citation errors were corrected while closing out: `p11_test.go` initially cited
A18/A19, which were already assigned to P10's Windows-page and daemon-refusal
criteria. Renumbering to A21/A24 is trivial; the reason it is recorded is that a
wrong criterion reference in a test is invisible — nothing checks that the number
a test names is the number the plan means.

### Verification of the new guards

The three P11 behaviours were mutation-tested rather than assumed: dropping
`timeoutMs`, sending it as `0` (the plausible wrong answer, since zero means an
*immediate* timeout upstream), and ceasing to read `runtimeStatus`/`toolsError`
each fail the new tests — 3/3 caught, tree restored. P10's static ban was proven
the same way earlier, by planting a non-test file that references
`windowsSandbox/setupStart` and observing the ban name it.

`TestExecAndShellUseDistinctLabelsPoliciesAndAudit` skips on Windows (POSIX
fixture paths, MADR 0116 P11), so its `thread/shellCommand` assertion was covered
in the WSL Linux lane rather than here.

## Deviation — 2026-09-21 (P8): the paging capabilities are still marked experimental

**Found** while grounding P8, before its first write, and **resolved by owner
decision the same turn.** Recorded in the MADR as well, because it contradicts an
assumption D7 rests on rather than merely changing a step.

**Evidence.** `thread/items/list` and `thread/turns/list` are both declared by the
**stable** schema bundle (102 stable client requests) and carry no
`#[experimental]` upstream (`common.rs:801-811`), yet
`testdata/0.155.1/manifest.json` records each as
`"stability": "experimental"` with `"fallback": "rpc:thread/read"`.
`thread/revert` has no capability entry at all. The cause is
`contract_generate_test.go:169-204`: capability stability is read from two
hand-maintained allowlists, not from the bundles, and both methods sit in
`experimentalAllowed` (`:194`).

**Consequence had it shipped.** P8 would have migrated the replay path onto
capabilities gated on the `experimental` negotiation, whose declared fallback is
the deprecated `thread/read` call P8 exists to remove — so an `experimental:
false` engine, or any runtime denial, would silently resume earning the
`deprecationNotice` that A13 asserts against, in a configuration A13 does not
exercise. P13/D16 would also have refused to advertise a stable capability.

**Decision: derive stability from the bundles** (option A of three offered; the
hand-move was rejected for leaving the mechanism that mislabels the *next*
promotion, which is the stale-enumeration pattern this plan already found twice
in F12 and F13).

**Files added to P8's scope** by this deviation:

```text
internal/provider/codex/contract_generate_test.go   stability derived from the bundles, not the allowlist
internal/provider/codex/testdata/0.155.1/manifest.json   regenerated consequence
```

**Added verification for P8**, beyond the phase's own:

```bash
# the two paging capabilities are stable and carry no fallback
go test ./internal/provider/codex/ -run 'Contract|Capabilit' -count=1
make live-codex-contract
CODEX_CONTRACT_EXACT=1 make live-codex-contract
```

Deferred, named so it is not mistaken for an oversight: `thread/revert` stays
without a capability entry. It is stable and promoted in the same upstream change,
but no step in this plan calls it, and adding a capability for an uncalled method
is surface without a caller.

## Execution record — P8 (2026-09-21)

**Ran: the P8 deviation fix and P8 itself** — commits `68a3d75` (capability
stability derived from the bundles, with its regenerated manifest) and `069400a`
(paginated replay, `excludeTurns` on resume, backwards cursors captured).

Gates: `pre-add-check` clean, `gofmt -l` empty, `go build ./...`, `go vet`,
`go test` and `go test -race` on the package green, `make live-codex-contract`
green in both default and `CODEX_CONTRACT_EXACT=1` modes against the installed
0.155.1 binary.

### Three premises corrected, two of which reduced the work

The 2026-09-21 deviation above covers step 4. Two more did not survive contact
with the source, and both were found before writing code rather than after:

1. **Step 2 is wrong about `thread/fork`.** It asks for `excludeTurns` and the two
   backwards cursors on fork as well as resume. Measured at 0.155.1: `excludeTurns`
   is a field of `ThreadResumeParams` only (`v2/thread.rs:404`, and not
   experimental), and `turnsBackwardsCursor` / `itemsBackwardsCursor` are fields of
   `ThreadResumeResponse` (`:455`, `:461`, neither experimental) and of
   `ThreadRevertResponse` (`thread_processor.rs:2304-2308`). `ThreadForkParams` and
   `ThreadForkResponse` (`:518-`, `:606-637`) carry none of them. So the fork half
   of step 2 asks for fields that do not exist and was not implemented. The plan's
   "all five of those fields" counted fork's non-existent pair.

2. **Step 3 has nothing to retire.** It asks that the local-search fallback stop
   depending on full hydration. It never did: the fallback (`threads.go:284-308`)
   pages `thread/list` and matches on `Title` + `Preview` metadata only. It calls
   neither `thread/read` nor any history RPC, and its `Truncated: true` honesty was
   already in place. Step 3 is a no-op, not a deletion.

The upstream deprecation text is worth quoting, because it is the specification for
what step 1 and step 2 had to do and it names both halves: *"Full-history hydration
is deprecated for paginated threads; use `excludeTurns: true`, then page with
`thread/turns/list` and `thread/items/list`."* (`thread_processor.rs:33`).

### Two traps that would have shipped a silently empty transcript

Both are recorded because neither produces an error:

* `thread/items/list` returns `ThreadItemEntry` — `{turnId, item}` — not bare items
  (`v2/thread.rs:1754-1758`). Decoding an entry as an item succeeds, leaves every
  field zero, matches no case in the render switch, and yields an empty transcript.
* The two paging RPCs have **opposite** default sort directions: `thread/items/list`
  defaults to ascending, `thread/turns/list` to descending (`:1746`, `:1709`).
  Replay must emit oldest-first, so the direction is now sent explicitly and
  asserted, rather than inherited from a default that differs between neighbours.

### The instrument had to be fixed before it could be trusted

The first mutation run did not fail — it **hung**, for the full 600s harness
timeout. The new test read `<-s.events` unguarded, so a regression that stops
emitting blocks forever instead of failing. Replaced with the package's existing
non-blocking `select`/`default` idiom, which fails immediately and names what was
missing. Recorded because a guard that deadlocks rather than fails is worse than
one that is merely slow: in CI it reports a timeout panic, not the assertion.

After that fix, **5 of 5 mutations were caught**: dropping the entry unwrap,
treating a null `nextCursor` as a value, dropping the page bound, asking for
descending order, and sending an empty cursor on the first page. The ordering and
first-page-cursor cases were caught only after `replayPageParams` was extracted as
a seam — before that, two mutations passed unnoticed because no test could see the
request.

### A13 could not be verified on this host — blocked, not dropped

**A13 is unproven, and the reason is a host defect rather than the migration.**
`live_p8_test.go` is written and vetted, and it fails at the first step:

```text
thread/list -> JSON-RPC error -32603: failed to list threads:
thread-store internal error: failed to list threads: Access is denied. (os error 5)
```

Measured cause: `~/.codex/sessions/2026/09/19` is the only path in the sessions
tree whose security descriptor cannot be read **by its owner**, while every sibling
day directory carries the normal `OWNER RIGHTS` + `SYSTEM` pattern and reads fine.
It is residue of the 2026-09-19 ACL incident (MADR 0163 F28-F30), and
`.sandbox/deny_read_acl_state.json` is `{"principals": {}}` — empty — so Codex's
own cleanup has no record to undo and there is no repair subcommand.

One unreadable day directory aborts the whole thread-store walk, so on this host
`thread/list` and every resume fail, including for threads created today. That is a
user-facing Codex defect independent of this plan, and it is out of scope here by
the plan's own Scope section: this plan documents host sandbox repairs and does not
perform them. Owner elected to run the elevated repair (2026-09-21).

A13 is therefore **deferred to the repaired host**, not weakened. The test was
deliberately left failing rather than converted to a skip: a skip on a thread-store
error would hide exactly the defect that matters.

Also corrected in the test itself: the first version created a thread and resumed
it, which can never work — a thread that has taken no turns has no rollout, so
there is nothing to locate. It now resumes an existing thread, which uses real
history and spends no tokens.

## Execution record — P9 and P13 (2026-09-21)

**Ran: P9 and P13** — commits `1d94f8d` and `e27ce65`. With P8 earlier the same
day, release 3 is implemented and every phase P1-P13 has now run.

Gates per phase: `pre-add-check` clean, `gofmt -l` empty, `go build ./...`,
`go vet`, the affected packages' tests and `-race` green, `make ci-windows`
exit 0 with no "skipping", and the WSL Linux lane green over
`./internal/provider/... ./internal/procutil/ ./internal/event/ ./internal/ws/`.

### P9 — the gap was larger than "two classes read alike"

The MADR's 2026-09-21 correction said we decoded none of `codexErrorInfo`, and the
mutation testing showed what that cost: with the engine's classification ignored,
the prose classifier labels `rateLimitExceeded`, `serverOverloaded` **and**
`unauthorized` all as `quota`. Three wrong answers, not one, each with advice that
does not apply — "wait for your limit to reset" for a rejected credential.

Mapped conservatively, and the restraint is deliberate: `usageLimitExceeded` ->
quota, `rateLimitExceeded` -> rate_limit, `serverOverloaded` /
`internalServerError` -> server, `unauthorized` -> auth, `sandboxError` ->
permission. `contextWindowExceeded` and `sessionBudgetExceeded` are **left
unmapped on purpose** and a test asserts they stay that way: their remedy is a new
or compacted session, so filing them as quota would tell the operator to wait for
a reset that will never come. `agenterr` has no class for "start a new session",
and inventing one is deferred rather than faked.

Two things were picked up that the phase did not ask for and that cost nothing:
`additionalDetails` (previously dropped, though it exists precisely because
`message` is often too terse to act on) and a log line naming the misalignment
category, which the schema warns is open-ended and must never be switched on.

`steer_message` is additive and server-first, documented in `internal/event` and
in `docs/protocol-v1.md`, and counted in `retention.go`'s size accounting — the
last of which is easy to forget and silently under-counts retention.

**Mutations: 4/4 caught**, including the one this plan named. Collapsing the two
classes fails with exactly the advice difference: `codexErrorInfo
"rateLimitExceeded" produced ErrorKind "quota", want "rate_limit"`.

### P13 — the compatibility question had a better answer than expected

Step 3 asked whether the phone tolerates a capability list that can now shrink,
and said the mobile change ships first if it does not. Measured: `apps/mobile`
parses `operations` and `experimental` into `CodexSurfaceCaps` and **never reads
either field** — a repository-wide search for `.operations` / `.experimental`
finds only the constructor parameters. Only `codexSurface != null` is consulted,
and the parser already falls back to `const []` for a missing or non-list value.
So a shrinking list cannot break an older phone and no mobile change is needed.
`flutter analyze` / `flutter test` were **not** run, because no Dart file changed;
the compatibility claim rests on that read of the code, not on the mobile suite.

One design decision worth recording: `NegotiatedSurfaceCapabilityIDs` reads the
capability *snapshot* rather than calling `Supports`. `Supports` has a side effect
— it lets an expired denial through so the next call re-probes (D10) — and an
advertisement should observe state, not mutate it. The cost is that a capability
whose denial has just expired stays unadvertised until real use re-probes it,
which under-advertises rather than over-promises. The test covers both ends of
that: the capability disappears when disabled and reappears after the window.

**Mutations: 2/2 caught** — advertising the manifest regardless of negotiation,
and claiming a negotiated surface with no engine running.

### A pre-existing flakiness finding, confirmed against an unmodified tree

`go test ./internal/...` with default parallelism fails intermittently on this
host, and **not** because of this plan. Three consecutive full-suite runs failed
with three *different* sets of tests, every failure a localhost dial or readiness
deadline rather than an assertion:

```text
run 1  TestProxyHealthReadinessAndOriginRejection    Get ".../readyz": context deadline exceeded
run 2  TestProxyPreInitializeAuthenticationFailure   WebSocket dial: context deadline exceeded
       TestProviderInitializesBeforeTimingOut
run 3  TestStartServerBailsWhenEngineExitsImmediately   (run on a worktree at HEAD,
       TestFirstEnvelopeDeadlineReapsSilentUpgrade      WITHOUT the P13 changes)
       TestEnvelopeVersionRejected
```

Run 3 is the decisive one: a detached worktree at the pre-P13 commit fails the same
way, so the cause is load, not the change. `TestProxyHealthReadinessAndOriginRejection`
passes 5/5 in isolation, and `make ci-windows` — the repo's actual gate, which is
what CI runs — passes. Recorded rather than fixed because tightening those deadlines
is not in this plan's scope; it belongs to whichever record owns the proxy
readiness tests.

### Status: implemented, one criterion unverified

Every phase has run. **A13 remains unverified** and is the only outstanding item:
it needs the host's `~/.codex/sessions/2026/09/19` ACL repaired before a live
resume can be observed (see the P8 record). The plan therefore stays
`in-progress` rather than `completed`, because marking it complete with an
unverified acceptance criterion is exactly how an unproven migration comes to look
finished.

Also still outstanding, and deliberately not run: `make live-codex-turn` and
`make live-codex-review`, the billed acceptance for release 3. They spend real
tokens and need an explicit instruction.

## Deviation — 2026-09-21 (A13): the notice was routed, then discarded

**Found while verifying A13, after P8 was already committed**, and resolved by
owner decision the same turn. Recorded in the MADR too, because it contradicts an
assumption A13 rests on.

**Evidence.** `live_p8_test.go` passed. It also passed with `excludeTurns`
removed, and with replay forced back onto `thread/read` + `includeTurns:true` —
**0 of 2 mutations caught**, so the test could not distinguish the migration from
its absence. Wire capture shows the engine did send the notice; our fan-out
dropped it, because `handleProviderNotification` iterates `p.sessionsSnapshot()`
and the session is not registered until `Start` returns.

**Consequence had it shipped.** Two harms, and the smaller one is the test. A13
would have been reported as proof of a migration it never checked. The larger one
is that every provider-level warning raised before a session registers is silently
lost, including `configWarning` and `windows/worldWritableWarning` — warnings about
host misconfiguration, during the window where host misconfiguration surfaces.

**Decision: buffer and replay to the next session** (option A of three; recording
on the Provider alone was rejected for leaving the user-facing half open, and
making the test resume inside an already-registered session was rejected outright
as a workaround — it would hide the dropped warning rather than fix it).

**Files added to scope** by this deviation:

```text
internal/provider/codex/routing.go        record when no session took the warning
internal/provider/codex/provider.go       bounded buffer + drain at registration
internal/provider/codex/live_p8_test.go   A13 asserts on something observable
internal/provider/codex/pending_warning_test.go   (new) unit + mutation target
```

**Added acceptance criterion:**

| # | Criterion | MADR |
| --- | --- | --- |
| A27 | A provider warning raised with no session registered reaches the next session; a thread-scoped warning for an unknown thread does not; the buffer is bounded | D8 |

**A13 is re-armed, not re-asserted.** It is only met once the two mutations above
are caught. Until then it stays unverified, and this plan does not claim otherwise.

## Execution record — A13 verified (2026-09-21)

**A13 is met.** Commit `c9e0ef8` closes the 2026-09-21 deviation; the host ACL was
repaired by the owner beforehand, which is what made a live resume possible at all.

### The host repair

`~/.codex/sessions/2026/09/19` now carries only **inherited** ACEs —
`OWNER RIGHTS` + `SYSTEM`, matching every sibling day directory — the tree
enumerates 29 files where it previously threw, and no path in it denies. Codex's
`thread/list` and resume work again on this host. Recorded because the cause
(F28-F30's orphaned deny, with an empty `deny_read_acl_state.json`) will recur if
Codex provisions its sandbox again, and the symptom was indistinguishable from our
own bug: `thread-store internal error: Access is denied. (os error 5)`.

### What made A13 real

Before the fix, the assertion passed in all three worlds — migrated, resume
reverted, and replay reverted — **0 of 2 mutations caught**. After it, **2 of 2**,
and the two failures carry *different* upstream messages, which is stronger
evidence than the criterion asked for:

```text
resume reverted  -> "...use `excludeTurns: true`, then page with
                     `thread/turns/list` and `thread/items/list`."   (thread_processor.rs:33)
replay reverted  -> "...omit `includeTurns` or set it to `false`, then page with
                     `thread/turns/list` and `thread/items/list`."   (thread_processor.rs:34)
```

Each half of the migration is therefore proven independently, by the engine's own
wording, rather than by one assertion that both halves happen to satisfy. The
baseline run sees no notice at all.

The diagnosis itself is worth keeping: the engine's frame was recovered with
`MCREMOTE_WIRE_CAPTURE_DIR`, which showed the notice on the wire while no event
reached the session. That is the instrument to reach for when a live assertion
about an absence passes suspiciously easily — it separates "the engine did not send
it" from "we threw it away", and those have opposite fixes.

### Deviations from the deviation

* The new test file is `early_warning_test.go`, not `pending_warning_test.go` as
  the deviation entry named it, matching its source file `early_warning.go`.
* `emitCodexWarning` was extracted so a replayed warning is byte-identical on the
  wire to a live one. That was not in the deviation's file list but is in
  `routing.go`, which was; it replaces the inline emit rather than adding a second
  one, keeping the single-emitter property the warning path already had.

### Gates

`pre-add-check` clean over all four files, `go test -race` green,
`make ci-windows` exit 0 with no "skipping", the WSL Linux lane green over
`./internal/provider/codex/ ./internal/ws/ ./internal/event/`, and the live
`make live-codex`-class assertion green against the installed 0.155.1 binary.

### Status

Every phase P1-P13 has run and **every acceptance criterion A1-A27 is met**.

The plan stays `in-progress` for one reason only: release 3's billed acceptance —
`make live-codex-turn` and `make live-codex-review`, plus the manual phone pass
described in Rollout — has not been run. Those spend real tokens and need an
explicit instruction. Marking the plan `completed` before them would claim a
release acceptance that has not happened.

## Execution record — release 3 acceptance (2026-09-21)

Run at the owner's explicit instruction, including the two token-bearing targets.

| target | result |
| --- | --- |
| `make live-codex` (unbilled) | exit 0 — 337 pass, 41 skip, 0 fail |
| `make live-codex-turn` (**billed**) | exit 0 — 3 pass, 1 skip of the four tagged tests |
| `make live-codex-review` (**billed**) | exit 0 — `TestLiveInlineReview` pass (7.78s) |
| `make live-codex-contract`, both modes | exit 0 (run earlier, against the same binary) |
| A13 live assertion | pass, with 2/2 mutations caught |

The unbilled suite was run first, deliberately, so that anything broken surfaced
before spending tokens on it.

### The one skip, and why it is not a failure

`TestLiveTurnPlanUpdatedNotSkipped` skipped after 3.68s of real work:

```text
live_turn_test.go:110: model did not emit a plan; wire shape and translation are
pinned by unit tests
```

That is the test's own designed behaviour, not a gate being dodged: whether a model
emits a plan for a given prompt is not deterministic, and the shape it would emit is
pinned by unit tests that do not need a live model. Recorded because "1 of 4 skipped"
in a billed acceptance is exactly the line a later reader would otherwise have to
re-establish from scratch.

## Observed — P14's cleanup conclusion is too strong (2026-09-21)

The P14 record and the comment in `live_helpers_test.go` state that the probe
showed a thread cwd can be removed once the engine has shut down. Measured during
this acceptance run, that is true of the unbilled suite and **not** reliably true
of the turn suite:

```text
live_helpers_test.go:76: could not remove thread cwd C:\...\codex-live-cwd-4190111268
after 3s: The process cannot access the file because it is being used by another
process.
```

Counted across the three runs: the warning fired **2 times in `live-codex-turn`
and 0 times in `live-codex` and `live-codex-review`**, leaving exactly two
directories behind, **both empty**. So the failure mode is narrower and more benign
than "cleanup is broken", and it is specific to the suite where a real turn is in
flight — which is the plausible mechanism: a turn's child processes can hold the
cwd open past the engine's own shutdown, which the P14 probe (no turn running) had
no way to observe.

Nothing fails and nothing is lost: the retry already treats this as non-fatal and
leaves the directory to the OS, which is the right call — a test must not block on
Windows releasing a handle. What is wrong is the strength of the claim in the
record and in the comment, which tell the next reader that seeing this warning means
something is broken. It does not; it means a turn was running.

The retry itself is deliberately **not** lengthened: that would trade test
wall-clock for tidiness in a case costing two empty directories, and the honest
repair is to correct the claim rather than chase the handle.

**Done, 2026-09-21 (owner instruction).** `liveThreadCwd`'s comment and its log
line now say an expiry is *expected while a turn is in flight* and is worth
investigating only when it happens with no turn running, and they carry the
measurement — 2 expiries in `live-codex-turn`, 0 in `live-codex` and
`live-codex-review` — so the next reader inherits the evidence rather than the
conclusion. The P14 phase text above needed no change: it never claimed removal
always succeeds; only the comment and this record did.

### Status

All phases P1-P13 run, all criteria A1-A27 met, and release 3's billed acceptance
green. The only outstanding item is the **manual phone pass** described in Rollout
(resume a long thread, confirm a `writeStdin` approval and an error class read
correctly), which needs a person and a device. The plan stays `in-progress` until
that is done.

## Deviation — 2026-09-21 (post-acceptance): the CI flake was not a timeout

**Found** by the flake ledger after release 3 was pushed, **fixed** at the owner's
instruction in commit `2bd43d3`. Amended here rather than opened as a new number
because the test guards `resolveBinaryIdentity`, which P10 changed (**A20**), and it
was this plan's own pushes that surfaced it.

### The first diagnosis was wrong, and the record should say so

`TestResolveBinaryIdentityHelperDoesNotCountAsLaunch` failed the whole build once
(run `35552774193`) and was recorded fail-then-pass hours later on a different job
(run `35606940444`). It was reported — by me, in this plan's own status summary — as
a timing flake: the test spawns the test binary as a helper under a 5-second
deadline, which is the classic shape that expires on a loaded runner. A fix
lengthening that deadline was written and then **reverted unused**.

The full CI log says otherwise, and the truncated read is what hid it:

```text
--- FAIL: TestResolveBinaryIdentityHelperDoesNotCountAsLaunch (0.01s)
    collaboration_test.go:199: identity probe wrote launch log (1 lines);
    --version inherited helper env
```

**0.01 seconds.** Not a deadline — the third assertion, that the launch log does not
exist. Widening the timeout would have changed nothing, and its explanatory comment
would have left a confidently wrong cause in the tree. The first read had grepped
only the `--- FAIL` line and inferred the rest, which is precisely the failure the
house rule about never truncating evidence exists to prevent.

### The real cause

`runAppServerHelper` (`provider_test.go:29`) reads `CODEX_HELPER_LAUNCH_LOG` from
its **inherited** environment, and `t.Setenv` makes that variable process-wide. So
every helper child alive during the test appends a row to *that test's* file —
including a straggler spawned by an earlier test whose teardown has not finished
killing it, which the surrounding CI log shows happening ("engine exited … signal:
killed"). The test asserted the file did not exist, and any stranger falsified it.

It bites on CI and not locally because slower process teardown widens the window.
Nothing about the code under test was ever wrong.

### The fix, and why it is stronger rather than looser

Relaxing a flaky assertion usually weakens it. This one is now **stricter**, because
it states the guarantee instead of a proxy for it. The helper records what it was
launched with, and the test fails on a row naming `--version` — an exec of the
binary by the identity probe, which is the actual defect (MADR 0119 P6) — while
rows belonging to anyone else are no longer this test's business.

Verified both directions, since a flake fix that cannot fail is just a deleted test:

| mutation | required | observed |
| --- | --- | --- |
| the probe execs the binary, version still correct | fail | fail — `identity probe exec'd the binary: "launch --version"` |
| a stranger's row seeded in the log | pass | pass |

The first mutation keeps the reported version correct on purpose, so only the new
assertion can catch it; the old file-existence check would have been satisfied by
either.

Gates: `pre-add-check` clean, `-race` green, `make ci-windows` exit 0, and on the
Linux cgo-free lane that produced the original failure the package is green and the
formerly flaky test passes 5 consecutive runs.

### Scope deliberately not widened

`collaboration_test.go` holds nine helper-spawn deadlines and
`reconnect_p3_test.go` two more, all the same 5-second shape. **None was changed**:
they are generous budgets that no test asserts on, they were never the cause here,
and changing them would have been the wrong fix applied broadly. Named so a later
reader does not mistake the restraint for an oversight — if a genuine timing flake
ever appears in this file, that is the moment to revisit them, with evidence.

`ci-flakes.tsv` keeps both historical rows. They are the evidence that the flake was
real and recurring, and deleting them to make the ledger look clean would destroy
the only record that this was ever a problem.
