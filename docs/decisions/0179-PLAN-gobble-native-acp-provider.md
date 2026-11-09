---
status: in-progress
date: 2026-11-09
associated-madr: "0179-MADR-gobble-native-acp-provider.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement registering gobble as a native ACP provider

Associated MADR: [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md)

The MADR's third body (2026-11-06, amended 2026-11-07) was accepted on 2026-11-07. This plan was rewritten the same
day to match it. R1, the 2026-10-11 assessment, is done, and its scope and record are kept below. P0 to P6 are new
with this rewrite. Each is proposed, and none starts until the owner approves it ("do P0", "do P1", …) and its gate
holds.

## Goal

mcremote registers gobble as the provider `"gobble"`, as the MADR's D1 to D16 set it out:

* a thin Spec in `internal/provider/gobble`, registered by descriptor, right after `fake` (D1, D11, Q5);
* no client files or terminals (D2);
* phone provider-auth through gobble's own CLI, never a file this repository writes (D3);
* a credential-aware handshake, through one agent-neutral `acpagent` hook, `Spec.AuthPreferenceFunc` (D4);
* `KnownGoodVersion` ~~`0.2.0`~~ the target release's version (MADR A1), with the floor revisited for it (D5);
* the standard model and thinking path, `ModelsFromOption` set, and a `ListModels` that starts no process (D7);
* `bypass` marked dangerous (D8);
* Rename and Fork bound to `_gobble/set_session_name` and `_gobble/clone`, and handlers for `_gobble/retry` and
  ~~`_gobble/compaction`~~ *A2:* gobble's `CompactionMeta` on the standard `compaction_update`, all through acpext's
  names and types (D9, D10, D13);
* *A2:* gobble `v0.4.0` stated as the supported minimum, not enforced (D5);
* the turkey icon, fetched by `tools/vendor-icons/sync.sh` from gobble-cli at a pinned `v*` tag (D11, Q4);
* the contract pinned by acpext's frames, by fixtures captured from ~~`v0.2.0`~~ the target release, and by
  `live-gobble` (D12).

**The target release** (MADR A1, 2026-11-08) is the newest gobble release that pins the same acp-go-sdk2 tag as this
repository when P2 starts. Wherever this plan names `v0.2.0` as the release to build, capture or test against, it now
means the target release. On 2026-11-08 there is none: this repository pins `v1.34.2`, gobble-cli `v0.3.1` pins
`v1.34.0`, and gobble-cli's `main` (`4970cca`) pins `v1.34.2` but is not released. *A2 (2026-11-09): gobble-cli
`v0.4.0` (`2e83cce`, released 2026-11-08) pins `v1.34.2` and `acpext v0.2.0`, so it is the target release today, and
the supported minimum.*

The plan is complete when the MADR's Confirmation 1 to 8 is met.

## Scope

**Base.** master at `938b0002`, acp-go-sdk2 `v1.34.0`, go-llmprovider-sdk `v1.27.0`. gobble-cli at `v0.2.0` and
`acpext/v0.1.0`. *A1 (2026-11-08): master is now at `5b3533fc`, with acp-go-sdk2 `v1.34.2` and go-llmprovider-sdk
`v1.28.0`; gobble-cli at the target release.* gobble-cli is read, never written by this plan; its icon is its own
`0044-PLAN-publish-gobbles-icon-and-its-generator.md`.

**In scope:**

* `internal/provider/acpagent/`: `acpagent.go` (the `AuthPreferenceFunc` field and its one use), and a test beside the
  existing selection tests (P1). `session.go` and `acpagent.go` again only if P1 step 4 finds the close bound loses an
  entry (MADR Q6); *A2:* `extensions.go` (`MetaFromCompaction`), `compaction.go` (`onCompactionUpdate`'s `_meta`) and
  a test beside the compaction tests (P1 step 4a);
* `internal/provider/gobble/` (new): the package, its tests, and `testdata/wire/<target version>/` (P2, P3);
* `internal/provider/provider.go` and `provider_test.go`: `IDGobble` (P2);
* `go.mod` and `go.sum`: `github.com/maccavelli/gobble-cli/acpext` ~~`v0.1.0`~~ `v0.2.0` (A2) alone (P2);
* registration (P4):
  * `internal/provider/registered/registered.go`;
  * `internal/config/prewarm_write.go` (`KnownProviderIDs`) and its test;
  * `configs/config.example.yaml`, `config.prod.example.yaml`, `config.mesh-grok.yaml`;
  * `docs/guides/config.md`;
  * `internal/command/resolve_order_test.go` and `internal/provider/auth_conformance_test.go`;
  * the `Makefile` (`live-gobble` and its `.PHONY` entry);
* the phone's icon (P5): `tools/vendor-icons/sync.sh`, `apps/mobile/assets/vendor_icons/gobble.svg`,
  `apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart`, `apps/mobile/test/vendor_icon_test.dart`;
* the records: this pair; a dated note on
  `0186-MADR-integrate-current-library-releases-and-prepare-gobble.md`'s Q4; `docs/README.md`.

**Out of scope** (the MADR's D15): gobble's native surface (steer, follow-up, the queue, state, retry and compaction
controls, personality), todos, gobble as the default provider (0208 P9 step 2), image and audio input, tool-call
`locations`. No Dart code branches on the provider id. No gobble-cli file.

## Conventions every phase follows

* **C1. Start state.** Each phase starts on a clean master, or names the files that are not clean and are not its own.
  Another session's uncommitted work is never staged, reverted or reformatted.
* **C2. Line numbers** here and in the MADR are `938b0002`'s. They are re-read at each phase's start; a fact that
  differs is a deviation.
* **C3. Scratch work** (planted breaks, captures before they are copied in, long logs) lives in the scratchpad, never
  in the tree. Logs take the exit status before any filter.
* **C4. Seen failing.** Every new test or gate is seen failing first on a planted break in a scratch copy of the tree,
  and the record quotes the failure.
* **C5. Go gates:** `make pre-add-check FILES=…` on every staged Go file, then `make test`, `go test -race ./...`,
  `make lint`, `make staticcheck` and `make vet-tags`. `make ci-windows` runs on the Windows host for P2, P4 and P6,
  which touch process launch and configuration.
* **C6. Dart gates** (P5): `dart format --output=none --set-exit-if-changed .` over `apps/mobile`, `flutter analyze`,
  `flutter test`.
* **C7. Spawns** go through `procutil.Command` (`TestNoBareSpawnOutsideProcutil`). Test stubs are
  `testexec.WriteShellStub` scripts, which skip where no POSIX shell runs.
* **C8. Commits and pushes.** The agent stages; the owner commits with `git commit --no-edit`. Nothing is pushed by
  this plan, and the disclosure guard runs before any push the owner asks for.
* **C9. Identifiers.** No hostname, account name or real-machine path in any committed file, the wire fixtures
  included. A captured frame that carries a path has it replaced by a placeholder. The disclosure guard's redact
  preview, from the repository root, is the gate; the record names what was scanned for, never the value.
* **C10. Quota.** A capture or live run spends a provider's quota, so it runs only on the owner's word, with the
  provider and model the owner names.
* **C11. Deviations** stop the phase. The record gets a dated entry, and the MADR an amendment where a decision or a
  fact changes.

## Implementation Steps

### R1 — the integration assessment (done 2026-10-11)

R1's scope, steps and verification as approved, kept verbatim. Its record is below.

**R1's scope.** Only these files may change in R1:

- `docs/reports/0179-REPORT-gobble-native-acp-integration-assessment.md` (new);
- `docs/README.md`, regenerated by `python3 scripts/check_records.py --write-index`, keeping every
  existing row unchanged;
- this PLAN, for the R1 execution record.

The MADR is not edited in R1. `gobble-cli` is read, never written.

**R1's steps.**

1. Record the baselines: this repository, `gobble-cli`, and `acp-go-sdk2`, each by commit, with the
   SDK version each side pins.
2. Inventory gobble's ACP surface from its source:
   - capabilities and `agentInfo`;
   - session methods;
   - config options and modes;
   - the session updates it emits;
   - the `_gobble/*` extensions;
   - how it uses the client's file and terminal capabilities.
3. Say, per area, what today's `acpagent` would do with that surface, citing file and line.
4. Evaluate the design's strengths and risks, with a recommendation for each risk.
5. Reconcile MADR 0179's premises with schema 1.25 and gobble's frozen extension table, and give the
   overlap with `0167-REPORT-acp-go-sdk2-integration-assessment.md`'s full-adoption phases.
6. Give a recommended order of work across the SDK, this repository, and gobble, and list the owner
   decisions. Label each as a recommendation.
7. Regenerate the index. Read the complete report, this PLAN, and the generated index before
   concluding.

**R1's verification.**

Run, recording the exit status before reading output:

```bash
python3 scripts/check_records.py --write-index
make check-records
make markdownlint-docs
```

Then verify:

- the R1 changes stay inside the three Scope paths;
- the `docs/README.md` diff adds only this PLAN's and the report's rows;
- the report has no hostname, account name, real-machine path, or scratch path;
- nothing claims a probe was run or a decision made.

R1 adds no executable assertion, so the fail-first rule creates no test requirement.

### P0 — the records

Files: this pair; `docs/decisions/0186-MADR-integrate-current-library-releases-and-prepare-gobble.md`;
`docs/README.md`.

1. **0186's Q4** gains a dated italic note: 0179 D3 and D4 implement Q4. gobble advertises one ACP method per key
   provider; the phone's provider-auth writes gobble's store through gobble's own CLI; the handshake authenticates an
   upstream that has a credential. Q4's original answer stays.
2. **The index.** `python3 scripts/check_records.py --write-index`, run only on a tree where `docs/README.md` has no
   other session's uncommitted change. Otherwise the phase stops and the owner says how to proceed. The diff must touch
   only the 0179 rows.
3. `make check-records` and `make markdownlint-docs` pass. The redact preview finds nothing.
4. Staged; the owner commits.

### P1 — the handshake hook, and the close bound (MADR D4, D6, Q6)

**Gate:** P0 committed. P1 touches `acpagent` only, and names no provider.

Files: `internal/provider/acpagent/acpagent.go`; `internal/provider/acpagent/acptest/auth_test.go`, beside
`TestAnAdvertisedKeyIsAuthenticated`; this plan. *A2:* `internal/provider/acpagent/extensions.go`, `compaction.go`,
and a test beside `compaction_test.go`'s (step 4a).

1. **The facts, re-read:** the Spec's auth fields (`acpagent.go:115-131`), the handshake's auth branch
   (`acpagent.go:746-787`), `selectACPAuthMethod` (`authselect.go:24-39`), and the close bound
   (`session.go:1365-1366`).
2. **The hook.** `Spec.AuthPreferenceFunc func(ctx context.Context) []string`, documented beside `AuthPreference`:
   when set, its answer replaces `AuthPreference` for this handshake; the pin and the key method are unchanged. At
   `acpagent.go:753-754`, the preference passed to `selectACPAuthMethod` is `p.spec.AuthPreferenceFunc(initCtx)` when
   the field is set, else `p.spec.AuthPreference`. Nothing else changes: an empty answer with no key method leaves
   `id` empty, so the existing branch sets `authRequired` and `Start` fails `ErrACPAuthRequired` before
   `session/new`.
3. **The test**, on `acpagent/acptest`, with vendor names kept out (MADR D12):
   * a Spec with `SafeAuthMethodIDs` `["vendor.a", "vendor.b"]`, `AuthPreference` `["vendor.a", "vendor.b"]`, and
     `AuthPreferenceFunc` answering `["vendor.b"]`, against an agent advertising both: the one `authenticate` names
     `vendor.b`;
   * the same Spec with the func answering nothing: `Start` fails `errors.Is(err, ErrACPAuthRequired)`, and the agent
     received no `authenticate` and no `session/new`;
   * a pinned `auth_method_id` `vendor.a` still wins over the func's answer;
   * the func receives a context that ends with the handshake (it is cancelled once `Start` returns).

   Seen failing (C4) with line 754 reverted to `p.spec.AuthPreference`: the first case authenticates `vendor.a`, and
   the second authenticates `vendor.a` instead of failing.
4. **The close bound (Q6).** Read gobble-cli ~~`v0.2.0`~~ the target release's `session/close` and cancel paths (if
   P1 runs before a target release exists, read `v0.2.0` and re-read at the target in P2 step 1) (`acpserver/agent.go`'s close
   handler and the session writer it waits for), and answer one question: can a process killed between a cancel and
   the end of its close lose the cancelled turn's last JSONL entry?
   * **No** (each entry is written and synced before the cancel is acknowledged, or the close waits for nothing that
     writes): the 2 s bound stands. The record quotes the lines.
   * **Yes:** the phase stops, and the owner is asked to approve Q6's fix, an agent-neutral `Spec.CloseTimeout`
     (zero keeps `closeTimeout`), with its own seen-failing test. The MADR's D6 and this plan are amended before it is
     built, and the gobble Spec sets it to 5 s in P2.

   4a. *A2 (2026-11-09):* **the compaction meta source** (MADR D10, A2).
   * Re-read `MetaSource`, `handleMeta` (`extensions.go:19-52`), `onCompactionUpdate` (`compaction.go:97-120`) and
     its caller, and `session_info_update`'s use of `handleMeta` (`session.go:2120-2121`).
   * `MetaFromCompaction MetaSource = "compaction_update"`, documented with the other two. `onCompactionUpdate` takes
     the notification's context, emits the compaction's event as today, then passes a non-empty `_meta` to
     `handleMeta(ctx, MetaFromCompaction, m)`, declining `compaction_update._meta` only when that reports a key with
     no handler. `MetaHandler`'s doc comment names the new source and where it runs: the SDK's notification loop,
     under `HandleExtensionMethod`'s rule.
   * The test, on `acpagent/acptest` with vendor names kept out: a Spec whose `MetaHandlers` has `vendor.a` receives,
     from a `compaction_update` with `_meta` `{"vendor.a": {...}, "vendor.b": 1}`, `vendor.a`'s value with
     `MetaFromCompaction`, after the compaction event; `vendor.b` is still declined; the compaction event's fields are
     the same as with no `_meta`. Seen failing (C4) with the `handleMeta` call removed (no value arrives), then with it
     placed before the emit (the order fails).
5. `TestNoProviderIsNamedInAcpagent` passes unchanged. Go gates (C5). **Commit.**

### P2 — the gobble package, unregistered (MADR D1 to D10, D13)

**Gate:** P1 committed. P2 registers nothing: no descriptor list, config file or phone file changes. *A1: and a target
release exists, installed on the capture host (MADR D14 gates 2 and 3).*

Files: `go.mod`, `go.sum`; `internal/provider/provider.go`, `provider_test.go`; `internal/provider/gobble/` (`doc.go`,
`gobble.go`, `section.go`, `auth.go`, `catalog.go`, `ops.go`, `notify.go`, and their `_test.go` files); this plan.

1. **The facts, re-read:**
   * the `Spec`, `Ops`, `Host` and `ExtensionNotificationHandler` signatures, and the default `ClientCapabilities`
     (`acpagent.go:1354-1366`);
   * `ModelsFromOption`'s input and output (`acpagent.go:173-179`, `modelcatalog.go:21-52`);
   * `ForkOptions` and `ForkResult`;
   * acpext's API at its tag: `go doc github.com/maccavelli/gobble-cli/acpext` lists every name the MADR's D13 uses,
     as written there;
   * whether the adapter can load a session while an earlier process for it still runs (D6). If it can, the phase
     stops: gobble's writer lock refuses that load.
   * *A1:* the MADR's F3 facts, read at `v0.2.0`, re-read at the target release: `initialize`, `session/new`,
     `session/list` and `authenticate` from a no-credential run, the `_gobble/*` methods D9 and D10 use, and the
     `acpext` version its `go.mod` requires. Whether its binary accepts `--version`, which decides D5's floor. A
     difference is a deviation (C11).
   * *A2 (2026-11-09):* for `v0.4.0` the `go.mod` (acp-go-sdk2 `v1.34.2`, `acpext v0.2.0`) and `--version` are
     already read (MADR Baselines, A2's re-read). This step re-reads the rest, and adds: with a credential, `/compact`
     sends a `compaction_update` pair carrying `_meta.gobble`, and no `_gobble/compaction`, to a client offering
     `session.compaction`. That run spends quota, so it waits for P3's word (C10) and is recorded there.
   1a. *A5 (2026-11-09):* **`Close` waits for the exit.** In `acpagent/session.go`, after the kill, `Close` waits for
   the agent's process to exit (`agentproc.Process.Done`), bounded at 2 s and by the caller's context. A test beside
   `lifecycle_test.go`'s: `Close` does not return before the process has exited, and returns within the bound when it
   never does. Seen failing (C4) with the wait removed.
2. **The module.** `go get github.com/maccavelli/gobble-cli/acpext@v0.1.0` (*A2: `@v0.2.0`, the version `v0.4.0`
   requires*). `go.mod` gains that one requirement, with
   no `replace`. `go mod tidy` is clean, and `go list -m all` gains only that module (Confirmation 7).
3. **The id.** `provider.IDGobble ID = "gobble"` in `provider.go`, and its row in `TestIDConstants`.
4. **The Spec** (D1, D2, D5, D7, D8), built per provider from its `Config` in `gobble.go`, so the auth hooks run the
   configured `bin`:
   * `ID`, `DefaultBin: "gobble"`, `DefaultArgs` returning `["acp"]`; no `ModelArgs`; the default `StartTimeout`;
   * `KnownGoodVersion:` ~~`"0.2.0"`~~ the target release's version (A1); no `MinVersion` unless step 1 finds the
     target accepts `--version`, which is then a deviation for the owner to decide;
   * `ClientCapabilities`: the default less `fs.readTextFile`, `fs.writeTextFile` and `terminal`, with
     `auth.terminal` false; *A2: `session.compaction` and `session.notices` kept (D2);*
   * `DefaultModeID: "default"`, `SynthesizeAutoMode: false`, `DangerousModeIDs: []string{"bypass"}`;
   * `Commands`: an empty table; `CommandCaveat`: gobble's skills run as `/skill:<name>` prompt text and are not
     listed;
   * ~~`NewSessionState`: the compaction id (D10);~~ *A2: no `NewSessionState`: D10 keeps no compaction id;*
   * *A2: `MetaHandlers`: `acpext.MetaKey` to the `CompactionMeta` handler (step 9);*
   * *A3 (2026-11-09): `CloseTimeout: 6 * time.Second`, gobble's 5 s `closeWait` plus 1 s (MADR D6, A3);*
   * no `CredentialEnv`, `SetModelFallback`, `SetThinkingFallback`, `HarvestSessionMeta`, `ConfigureSession`,
     `SessionMeta`, `InitializeMeta`, `StaticModels` or `ModelProviders`.
5. **The section** (D11), `section.go`, on grok's pattern (`grok/section.go`): a `Section` embedding
   `config.ACPProviderConfig`, its defaults (`Bin: "gobble"`, enabled, prewarm off, the shared timeouts),
   `Validate()` (the shared ACP checks, and D4's `auth_method_id` rule: empty, or one of `acpext.AuthProviders()`'
   ids, refused otherwise with a message naming the allowed ids), `providerConfig`, and `Descriptor()` with `ID`,
   `Name: "gobble"`, `Section` and `New`; no `Floor` and no `Credentials`. `Descriptor()` is not yet listed.
6. **Auth** (D3, D4), `auth.go`. Every process runs through `procutil.Command` with the daemon's environment:
   * `AuthCatalogList`: `acpext.AuthProviders()`, in order; `api_key` for each, with Claude's optional workspace
     input; `oauth_device` where acpext marks a login; grok's `oauth_browser` marked unavailable (`host_oauth`);
     source `static`;
   * `SetCredential`: `gobble auth login <id> --api-key -`, the key written to the child's stdin and never to argv;
     `--workspace` from the input. A failure's text never carries the key;
   * `ClearCredential`: `gobble auth logout <id>`;
   * `StartDeviceAuth`: `gobble auth login <id> --method device --json`; the first `device_code` event fills the
     phone's `DeviceFlow`, then the process is waited for and an `error` event's message surfaced. A provider without
     a login is `ErrAuthMethodUnsupported`;
   * ~~`AuthStatus`: `gobble auth check --provider <id> --json` for each catalogued id, run concurrently, exit 0 ready,
     1 absent, 2 an error. The answer is cached for 15 s and dropped after every write, clear and finished device
     flow. When gobble-cli#13 ships `auth status --json` in a release, the source changes under an amendment;~~
     *A4 (2026-11-09): `AuthStatus` runs one `gobble auth status --json`. Each row's `usable` is the upstream's
     status (configured or missing); its `api_key` method is `Configured` when `source` is `auth.json` with `authType`
     `api_key`, and its `oauth_device` method when `authType` is `oauth`. An `error` object (gobble's exit 2) makes the
     state `error`, with gobble's message, never a value. The answer is cached for 15 s and dropped after every write,
     clear and finished device flow;*
   * `SafeAuthMethodIDs`: the ids of `acpext.AuthProviders()`. `HasHeadlessCredential`: any upstream ready in the
     status. `KeyAuthMethodID` unset. `AuthPreferenceFunc`: the ready upstreams, in `AuthProviders()` order;
   * ~~a -32000's data is decoded as `acpext.ErrorData`, and the phone's message names `Command` when set.~~ *A6:
     nothing is decoded; see item 6.*
7. **Models** (D7), `catalog.go`: `ListModels` answers an empty catalog and starts nothing, as opencode's
   `acpNoHarvest` does. `ModelsFromOption` keeps every group gobble sends, in gobble's order: its `recent` group
   first, then one group per provider. `recent` stays, by the owner's word of 2026-11-07 ("i like having recents in
   the model list"), as the choice D7 left to this plan. Its entries repeat models from the provider groups, with
   the same ids, so either row selects the same model. On the phone, the selected model shows its check mark, and
   its thinking-level chips, under both rows.
8. **Operations** (D9), `ops.go`: `Ops.Rename` calls `acpext.MethodSetSessionName` with
   `acpext.SetSessionNameParams`; `Ops.Fork` calls `acpext.MethodClone` with `acpext.SessionParams`, decodes
   `acpext.CloneResult`, and returns `ForkResult{AgentSessionID, ForkedFromID}`, ignoring `LastTurnID`. No other op.
   The package holds no `"_gobble/…"` literal.
9. **Notifications** (D10), `notify.go`: `acpext.MethodRetry` decodes `acpext.RetryNotice` into `Host.Notice` at
   `start` and at an `end` with an `error`; ~~`acpext.MethodCompaction` decodes `acpext.CompactionNotice` into
   `Host.UpdateCompaction`, one id per start from the session state.~~ *A1:* a `cause` outside acpext's twelve
   constants produces the same notice, naming the cause as sent. Any friendlier wording for known causes falls back
   to the raw value, never to nothing.
   * *A2 (2026-11-09): no `MethodCompaction` handler. The `CompactionMeta` handler, registered under
     `acpext.MetaKey` with `MetaFromCompaction`, decodes the value as `acpext.CompactionMeta`. With both `Reason` and
     `TokensBefore` set, a live completed compaction, it sends one `Host.Notice` naming the reason as gobble sent it
     and the tokens before. Otherwise it sends nothing. A value that does not decode is declined through
     `Host.Decline`. The notice's wording is fixed in this step and quoted in the record.*
10. **The tests** (D12 items 1 to 6, 8, 10 and 11; *A2: and 12*), on `acpagent/acptest` and `Provider.StartOn`, with frames built
    from acpext's types and its `testdata/` shapes. Each is seen failing (C4) on its own planted break:
    * item 1, `initialize` advertises no `fs`, `terminal` or `auth.terminal`: `fs` put back; *A2: and advertises
      `session.compaction` and `session.notices`: `session.compaction` removed;*
    * item 2, a stub `gobble` reporting only `openai` ready makes the handshake authenticate `openai`; reporting none
      fails `ErrACPAuthRequired` with no `authenticate` and no `session/new`: `AuthPreferenceFunc` replaced by a fixed
      `AuthPreference` of acpext's ids (the first then authenticates `gemini`), then `HasHeadlessCredential` forced
      true and the func answering every id with none ready;
    * item 3, the session declares `CapRename` and `CapFork`, model and thinking, and no other Ops-backed capability:
      an `Ops.Compact` added;
    * item 4, Rename and Fork send exactly D9's frames and map the answers, and return gobble's -32600 during a
      prompt: the clone result read as `id`;
    * item 5, the two notifications produce D10's events: the retry handler unregistered. *A1:* plus a retry `start`
      with `cause` `"a-cause-gobble-adds-later"` produces its notice naming that cause: the cause mapped through a
      table of the twelve constants with no fallback, so the unknown cause yields no notice or an empty cause; *A2:
      one notification now, and a `_gobble/compaction` produces no event: a `MethodCompaction` handler added;*
    * item 6, a -32000 prompt is `KindAuth` ~~with `ErrorData.Command` in the text: the data replaced by a bare
      message~~; *A6: and a handshake whose `authenticate` meets -32000 fails `Start` with text holding the
      command. Seen failing: the agent's -32000 replaced by -32603 (the prompt is then not `KindAuth`), and the data's
      `command` dropped (the start's text then lacks it);*
    * item 8, `AuthCatalogList` equals `acpext.AuthProviders()`; the stub records its argv and stdin, and the key is
      on stdin and absent from argv; `StartDeviceAuth` for `claude` is `ErrAuthMethodUnsupported`: the key moved to
      argv, then `claude` given a login; *A4: and `AuthStatus` maps a stub's `auth status --json` report: `usable`
      to configured, only an `auth.json` `api_key` row's key method `Configured`, an `env` row ready but not
      `Configured`, an `error` report to `error`. Seen failing with `env` treated as `Configured`;*
    * item 10, only `internal/provider/gobble` imports acpext, and it imports no go-llmprovider-sdk: an acpext import
      added to a scratch copy of `acpagent`;
    * item 11, `ListModels` starts no process: `ListModels` unset, so the adapter's harvest runs the stub;
    * *A2:* item 12, the `CompactionMeta` handler, with metas built from acpext's three `compaction.meta.*` frames: a
      live `completed` one (`manual`, a `tokensBefore`) sends one notice naming both, after the compaction event; a
      reason outside acpext's constants is named as sent; a replayed `completed` one (no `reason`), the `start` one and
      the `paused` one send none; an undecodable value is declined with no notice. Seen failing on: the notice sent
      for any value with a `reason` (the `start` frame then notifies), then the `reason` check dropped (the replay
      then notifies), then the reasons mapped through a table with no fallback;
    * the catalog keeps gobble's groups and order, `recent` first, with a model in both `recent` and its provider's
      group listed under each: the `recent` group filtered out;
    * `bypass` is confirmed on the phone like grok's `auto` (Confirmation 4), as
      `TestDangerousModeIDsMarkTheAgentsModes` checks it: `DangerousModeIDs` emptied;
    * the `auth_method_id` rule: the check removed;
    * *A3:* the Spec's `CloseTimeout` is 6 s: set to zero.

    Auth tests run against a scratch `GOBBLE_HOME` and a stub `gobble`, never the host's store or binary.
11. **Measured:** one `AuthStatus` refresh against the installed `gobble` ~~`v0.2.0`~~ target release with a scratch `GOBBLE_HOME`,
    ~~nine concurrent `auth check`s,~~ *A4: one `auth status --json`,* timed five times. A median over 1 s is a deviation (D3's ~~fallback~~ source is too slow for
    every `providers.list`).
12. `TestSDKIsImportedOnlyByGrok` and `TestNoProviderIsNamedInAcpagent` pass unchanged. Go gates (C5), and
    `make ci-windows`. **Commit.**

### P3 — the release fixtures (MADR D12)

**Gate:** P2 committed, and the owner's word for the capture (C10), naming the gobble provider and model to use.

Files: `internal/provider/gobble/testdata/wire/<target version>/frames.jsonl` and `meta.json`; a fixture test file;
this plan.

1. **The capture,** with `MCREMOTE_WIRE_CAPTURE_DIR` (`internal/wirecap`) against the installed `gobble` ~~`v0.2.0`~~
   target release,
   driven by acpagent with the P2 Spec, in an isolated home with the owner's chosen credential:
   * `initialize`, `session/new`, `authenticate`;
   * a prompt with a tool call; `/model` and `/thinking`;
   * rename and clone, idle and mid-turn;
   * a provider retry and a compaction (`/compact`); *A2: the compaction as a `compaction_update` pair with
     `_meta.gobble`, and its replayed `completed` update after the load below, with no `_gobble/compaction` (MADR
     D12). Its absence is the P2 step 1 fact that waited for this capture;*
   * a close, then a load;
   * a prompt with no credential: a second scratch `GOBBLE_HOME` with no `auth.json` and the provider variables unset.
2. **The cold start** (D1): the time from spawn to `session/new`'s answer, five times. A median over 15 s, half the
   default `StartTimeout`, is a deviation.
3. **Redaction.** Paths and account names replaced by placeholders, as opencode's `meta.json` records; the redact
   preview finds nothing. `meta.json` names the provider, version, source, date, host hash, frame count, what was
   redacted and what the run did.
4. **The fixture tests,** each seen failing (C4):
   * item 7, the recorded session sends nothing outside D6, D7 and D9: no other `_gobble/*`, no `_x.ai/*`, no `fs/*`
     or `terminal/*` answer: a stray `_gobble/get_state` call added to Rename; *A2: and no `_gobble/compaction`: one
     inserted into a scratch copy of the frames;*
   * item 9, every captured `_gobble/*` frame, and `initialize`'s `_meta.gobble`, decodes into its acpext type with
     unknown members refused, and encodes back to the same JSON: a captured retry frame given an extra member, and a
     clone result's `sessionId` renamed `id`. *A2: each captured `compaction_update`'s `_meta.gobble` too, as
     `acpext.CompactionMeta`: a captured one given an extra member.*
5. Go gates (C5). **Commit.**

### P4 — registration (MADR D11)

**Gate:** P3 committed.

Files: `internal/provider/registered/registered.go`; `internal/config/prewarm_write.go` and its test; the three
shipped configs; `docs/guides/config.md`; `internal/command/resolve_order_test.go`;
`internal/provider/auth_conformance_test.go`; a new `internal/provider/gobble/live_gobble_test.go`; the `Makefile`;
this plan.

1. **The list.** `gobble.Descriptor()` right after `fake.Descriptor()` in `registered.Descriptors()` (Q5).
2. **`KnownProviderIDs`** gains `"gobble"`, so the phone's prewarm toggle works; its test is seen failing without it.
3. **The shipped configs** gain a commented `providers.gobble` block, as each provider has. The test
   `TestTheShippedConfigFilesLoadThroughTheSections` loads them, and is seen failing on a planted unknown key in the
   block.
4. **`docs/guides/config.md`:** gobble's rows in "Defaults" and "Environment variables"; `auth_method_id`'s allowed
   values; that the phone writes gobble's store through gobble's CLI, and `gobble auth login` on the host still works;
   that `bypass` needs gobble's own `permissions.allowBypass`. *A2: that gobble `v0.4.0` is the supported minimum,
   and an older gobble shows no compaction progress (MADR D5).*
5. **The test lists:** gobble in `providerTables` (`resolve_order_test.go:21-27`), and an `auth_conformance_test.go`
   case with status, writer, catalog and device auth true, and no switcher.
6. **`live-gobble`:** `go test -tags live_gobble ./internal/provider/gobble/ -count=1 -timeout 600s -v`, its `.PHONY`
   entry, and `live_gobble_test.go` holding D12's live list. `make vet-tags` compiles it. It is not run in this phase.
7. **Assert by search:** outside `internal/provider/gobble` and this phase's files, no Go file names `IDGobble` or
   `"gobble"` (Confirmation 3).
8. Go gates (C5), and `make ci-windows`. **Commit.**

After P4 the phone can choose gobble when `gobble` is on the service's PATH. Its icon is the monogram until P5.

### P5 — the phone's icon (MADR D11, Q4)

**Gate:** P4 committed, and a gobble-cli `v*` release whose tree holds `assets/gobble.svg` (gobble-cli
`0044-PLAN-publish-gobbles-icon-and-its-generator.md`). Until then, P5 waits and P6 may run first.

Files: `tools/vendor-icons/sync.sh`; `apps/mobile/assets/vendor_icons/gobble.svg`;
`apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart`; `apps/mobile/test/vendor_icon_test.dart`; this plan.

1. **The fetch.** `sync.sh` gains `GOBBLE_ICON_REF=<tag>` beside `LOBE_ICONS_VERSION`. After the Lobe copy, it runs
   `curl -fsSL -o "$ASSETS/gobble.svg"` from
   `https://raw.githubusercontent.com/maccavelli/gobble-cli/${GOBBLE_ICON_REF}/assets/gobble.svg`, and checks that
   the file starts with `<svg` and holds `fill="currentColor"`. The manifest step maps `gobble` to
   `assets/vendor_icons/gobble.svg`, and its header names both sources and their licences. The 400 KB budget check is
   unchanged.
2. **The run.** `tools/vendor-icons/sync.sh`. `git status` shows only `gobble.svg` added and the manifest changed;
   the 67 Lobe icons are byte-identical (Lobe's pin is unchanged). `gobble.svg`'s sha256 is gobble-cli 0044-MADR's.
3. **Seen failing** (C4), on a scratch copy of `tools/vendor-icons/` pointed at a scratch asset directory:
   `GOBBLE_ICON_REF` set to a tag that does not exist (curl's `-f` fails the run), and the downloaded file replaced by
   HTML (the `<svg` check fails it).
4. **The phone test.** `vendor_icon_test.dart` gains: `VendorIcon(id: 'gobble')` renders an `SvgPicture` for
   `assets/vendor_icons/gobble.svg`, not a monogram. Seen failing with `gobble` removed from a scratch manifest.
5. **No Dart branch:** `rg -n "'gobble'" apps/mobile/lib` finds only the generated manifest.
6. Dart gates (C6). **Commit.**

### P6 — acceptance and close-out (MADR Confirmation)

**Gate:** P4 committed; the owner's word for the live run (C10). P5 may land before or after.

Files: this plan, the MADR, `docs/README.md`.

1. **`make live-gobble`** against the installed ~~`v0.2.0`~~ target release (D12's live list). The record lists the recorded connection's
   methods.
2. **The phone key write (Confirmation 8).** With a scratch `GOBBLE_HOME` and a dummy key that is not a real
   credential, a phone `provider.set_credential` for gobble, through a scratch daemon, makes `gobble auth check` report the
   provider ready. mcremote's log holds no copy of the key; P2 item 8 already proves argv.
3. **`mcremote doctor`** lists gobble, runs no version check for it, and reports the installed binary
   (Confirmation 1).
4. **The other providers' suites:** grok, opencode, kilo and codex unit, conformance and wire suites pass unchanged.
   Their live suites run only on the owner's word.
5. **The wider gates:** `make race`, `go test -race ./...` on the Linux test host, and `make ci-windows`.
6. **The MADR's Confirmation 1 to 8,** each with its evidence.
7. **A comment for issue #33,** written into the record, for the owner to post.
8. **Status.** This plan becomes `complete`, and the index is regenerated under P0 step 2's rule.

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 `IDGobble`; `gobble acp` started; `doctor` lists it with no version check | P2, P4, P6 | P2 step 3; P4 step 1; P6 steps 1 and 3 |
| 2 D12's eleven tests and the hook's test, each seen failing; *A2: twelve, and the meta source's test* | P1, P2, P3 | P1 steps 3 and 4a; P2 step 10; P3 step 4 |
| 3 no gobble name outside its package and D11's places | P1, P4 | P1 step 5; P4 step 7 |
| 4 `bypass` confirmed on the phone | P2 | step 10, the `bypass` test |
| 5 `live-gobble` against the target release (A1) | P6 | step 1 |
| 6 the other providers unchanged; race and Windows | every phase | C5; P6 steps 4 and 5 |
| 7 `acpext` by tag, no `replace`; `go list -m all` gains only it | P2 | step 2 |
| 8 a phone key write; no key in argv or logs | P2, P6 | P2 step 10 item 8; P6 step 2 |

## Rollout and Rollback

* **One commit per phase.** P1's hook is unused until P2. P2 and P3 build a package nothing registers. Reverting P4
  unregisters gobble; P5's icon then maps an id no provider uses, which is harmless. A phase is reverted only after
  every later phase that depends on it.
* **User-visible:** after P4, gobble is a provider the phone can choose, shown when `gobble` is on the service's PATH.
  Nothing changes for the other providers. grok stays the default until 0208 P9 step 2.
* **Configuration:** a new `providers.gobble` section, all optional. An existing configuration file needs no change.
* **The wire:** gobble only. No frame to another provider changes.

## Execution record

### R1, 2026-10-11

Approved by the owner's instruction to write the assessment as a report in this repository's
reports path. Files changed by R1:

- `docs/reports/0179-REPORT-gobble-native-acp-integration-assessment.md` (new);
- `docs/README.md` (regenerated);
- this PLAN.

The MADR was not edited, and `gobble-cli` was not written.

Steps 1–6 were carried out as written. The report records:

- baselines: this repository `9778cbc17b39`; `gobble-cli` `ad6ea57c5d0d` with a clean tree;
  `acp-go-sdk2` `f2a6a3a`, with `v0.14.0` pinned by gobble;
- gobble's observed ACP surface;
- a per-area table of today's `acpagent` behavior, marked derived;
- risks R1–R8 with recommendations;
- a reconciliation of MADR 0179 with schema 1.25 and gobble's `_gobble/*` table;
- a recommended order of work, registration acceptance gates, and five owner decisions.

Verification, exit status recorded before reading output:

```text
python3 scripts/check_records.py --write-index   exit=0
make check-records                               exit=0
make markdownlint-docs                           exit=0   (the new report included)
```

- `docs/README.md` diff against the R1-start copy: two added rows, this PLAN and the report, and
  nothing else.
- An identifier scan of the report and this PLAN (user paths, temp and scratch paths, host and
  account names, home-relative paths) found nothing.
- The Markdown gate was shown to fail on a deliberately broken scratch copy earlier the same day
  (PLAN 0167 P15 and PLAN 0054 R1 execution records). It was not repeated for R1.

Not done, deliberately: no probe, build or test in either repository (R1 is read-only by scope).

R1 is complete. This PLAN stays open until the owner decides whether to amend or supersede the MADR,
and source phases are planned.

### Rename, 2026-10-13

- **The owner decided** to amend in place under a gobble name, not supersede
  ([0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md),
  owner answer Q2).
- **The pair was renamed with `git mv`,** from the `pigo` filenames to
  `0179-MADR-gobble-native-acp-provider.md` and this file's name.
- **Links were repointed** in:
  - the 0179 MADR and this PLAN;
  - `0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md`;
  - `0186-MADR-integrate-current-library-releases-and-prepare-gobble.md`;
  - `docs/reports/0167-REPORT-acp-go-sdk2-integration-assessment.md`;
  - `docs/reports/0179-REPORT-gobble-native-acp-integration-assessment.md`.
- **The index was regenerated.**
- **The old filename survives once,** as historical text in
  `0175-PLAN-conform-docs-tree-to-adopted-record-layout.md`'s execution record. That is a record of
  what the file was called then, not a link.
- **Still to do:** the MADR's content rewrite, and this PLAN's source phases, follow 0184's A2
  (0186 D9). They are planned by an amendment of this PLAN.

## Status note — 2026-10-18

Found by the record audit in [0200-REPORT-record-status-audit.md](../reports/0200-REPORT-record-status-audit.md).

Still `in-progress`. What the rename entry left to do is still to do:

* **The file rename is done** (`68e348a1`, 2026-10-13).
* **The MADR's content rewrite is not.** Its title still reads "Drive pigo as a first-class
  ACP provider". The old name is on 79 lines of the MADR, on 2 lines of this PLAN before
  this note, and on 4 lines of `docs/reports/0179-REPORT-gobble-native-acp-integration-assessment.md`.
* **The source phases are not planned,** and there is no gobble provider under
  `internal/provider/`. The MADR stays `proposed`.

## Rewrite, 2026-11-03

* **The MADR was rewritten in place** as gobble's contract on schema 1.25, from code read at this repository's
  `3489184b`, gobble-cli's `9b5535a` and acp-go-sdk2 `v1.31.2`. This was 0186's Q2 and the owner's instruction of
  2026-11-03: "determine what functionality is provided by the shared libraries and sdk, and ensure only what needs to
  be built is included", "omit fs/terminal and auth via acp authenticate". The authenticate omit was withdrawn later
  the same day (MADR amendment): D3 is phone 0074 plus handshake `authenticate`; fs/terminal stay omitted. The status
  note above is answered: the earlier name is left only in the MADR's dated History.
* **This plan's forward parts were replaced** by P0 to P4. R1's scope, steps, verification and record are kept
  verbatim.
* **Not done with the rewrite:** P0's 0186 note and index regeneration. When the rewrite was written, `docs/README.md`
  carried another session's uncommitted change, so the index was not regenerated (P0 step 2).

## Amendment, 2026-11-03 — D3 is phone provider-auth

The MADR amendment of this date restores 0186 Q4. This plan's Goal, P0 step 1, P1 (files, capture, Spec auth
fields, D11 item 2 and item 8), P2 step 2 and the config.md bullet are updated to match. P0–P4 remain proposed. No
source is in this amendment.

## Second amendment, 2026-11-03 — gobble's `acpext` module

The MADR's second amendment adds D13: gobble's `acpext` module, by the owner's "i want the acpext module". The changes
here are each marked *second amendment*:

* the Goal's last bullet;
* `go.mod` and `go.sum` in Scope;
* P1's gate (D12 gate 6), its files, step 1's acpext read and `go get`, steps 3 to 6, and items 9 and 10;
* Confirmation 7 in the Verification table.

The acpext tag comes from gobble-cli's `0042-PLAN-acpext-extension-contract-module.md`, which is gobble-cli's to
approve and run. This plan still writes nothing in gobble-cli. P0 to P4 remain proposed, and no source is in this
amendment.

## Rewrite, 2026-11-07

* **The MADR's third body was accepted** on 2026-11-07, on the owner's "Approved to update 0179 madr and plan", after
  its amendment of the same day (Q4: the icon fetched from gobble-cli at a pinned tag).
* **This plan's forward parts were replaced** by P0 to P6, for that body: the `acpagent` hook and the close-bound read
  (P1), the package unregistered (P2), the release fixtures (P3), registration by descriptor (P4), the phone's icon
  (P5), and acceptance (P6). The 2026-11-03 P0 to P4, written for the second body, are superseded; their text is in
  this file's history at `938b0002`.
* **R1's scope, steps, verification and record are kept verbatim,** as are the entries below them.
* **Two choices the MADR left to this plan,** each stated where it is made: `AuthStatus`'s cache of 15 s (P2 step 6,
  with P2 step 11's measurement), and keeping gobble's `recent` model group in the picker (P2 step 7). The first
  draft dropped `recent` as a duplicate; the owner kept it the same day ("i like having recents in the model list").
* **The status** is `proposed` until the owner approves a phase. R1 is complete, and nothing else has started.

## Amendment, 2026-11-08 — MADR A1: unknown retry causes, and the target release

On the owner's "amend 0179 for the two #36 gaps". The MADR's A1 states the findings and the decision. Here:

* **P2 step 9 and step 10 item 5:** the retry handler takes a cause it does not know. The test sends
  `"a-cause-gobble-adds-later"` and is seen failing on a cause table with no fallback.
* **The release to build against** is the target release: the newest gobble release on this repository's
  acp-go-sdk2 tag when P2 starts. It replaces `v0.2.0` in the Goal, the Scope's base and test-data path, P1 step 4,
  P2's gate and steps 1, 4 and 11, P3, P6 step 1, and the Verification table. P2 step 1 re-reads the MADR's F3 facts,
  and whether the binary accepts `--version`, at the target release.
* **Where it stands, 2026-11-08:** this repository pins acp-go-sdk2 `v1.34.2`. gobble-cli's newest release, `v0.3.1`,
  pins `v1.34.0`. gobble-cli's `main` (`4970cca`) pins `v1.34.2`, go-llmprovider-sdk `v1.28.0` and `acpext v0.1.0`,
  the same as this repository, but has no release yet. P0 and P1 do not depend on the target release. P2 waits for
  it.
* No phase has started, and no source is in this amendment. The status stays `proposed`.

## Amendment, 2026-11-09 — MADR A2: gobble sends compaction as standard updates (issue #37)

On the owner's "amend 0179 for #37", with the owner's answers: "No handler; v0.4.0 minimum (Recommended)", and
"Decode it in registration". The MADR's A2 states the findings and the decisions. Here:

* **The target release is gobble `v0.4.0`** today (`2e83cce`): it pins acp-go-sdk2 `v1.34.2`, this repository's tag,
  and `acpext v0.2.0`. It is also the supported minimum, stated in `docs/guides/config.md` (P4 step 4) and not
  enforced. A1's rule still picks a newer release if one is cut before P2 starts. P2's gate on a target release is
  met.
* **acpext `v0.2.0`** replaces `v0.1.0` in the Scope and P2 step 2. The proxy serves it, and it still requires no
  module.
* **P1 step 4a** is new: `MetaFromCompaction` and `onCompactionUpdate`'s `_meta` passed to `MetaHandlers`, with a
  vendor-neutral test seen failing twice. P1's files gain `extensions.go`, `compaction.go` and that test.
* **P2:** step 1 records what A2 already read and what waits for the P3 capture; step 4 keeps `session.compaction`
  and `session.notices`, drops `NewSessionState` and registers the `CompactionMeta` handler; step 9 strikes the
  `_gobble/compaction` handler and specifies the new one; step 10 extends items 1 and 5 and adds item 12.
* **P3:** the capture records the `compaction_update` pair, its replay and the absence of `_gobble/compaction`; items 7
  and 9 cover them.
* **The Goal and the Verification table** say the same.
* No phase has started, and no source is in this amendment. The status stays `proposed`.

The owner committed A2 as `654b5a39`, then asked: "update the issue, and post. then proceed." A comment summarizing
A2 was posted on issue #37 (`issuecomment-6747718973`).

## P0 — 2026-11-09: the records

* **Start state (C1).** Clean tree. `master` one commit ahead of `origin/master`: A2's `654b5a39`, not yet pushed.
* **Step 1.** `0186-MADR-integrate-current-library-releases-and-prepare-gobble.md`'s Q4 answer gains a dated italic
  note, as its last bullet: 0179 D3 and D4 implement it; gobble advertises one agent method per key provider; the
  phone writes gobble's store through gobble's own CLI, with the key on stdin; the handshake authenticates an
  upstream that has a credential, through an agent-neutral hook. Q4's answer is unchanged.
* **Step 2.** `docs/README.md` had no uncommitted change (`git diff --quiet`, exit 0). `check_records.py
  --write-index` then left it unchanged: the index already lists this pair and 0186 as they stand.
* **Step 3.** `make check-records markdownlint-docs`: exit 0. The redact preview: 0 files.
* **Staged** for the owner's commit: this plan and the 0186 MADR. The status is `in-progress`: P0 is done, and P1
  waits for the owner's word ("do P1").

The owner committed P0 and approved P1: "committed proceed".

## P1 deviation — 2026-11-09: the close bound loses entries (step 4's "Yes")

* **Found.** Step 4's read of gobble `v0.4.0` (`acpserver/agent.go`, fetched through the GitHub API into the
  scratchpad): a turn writes with `context.WithoutCancel` (line 662); a cancelled turn's `agent.End` writes the
  results of calls the cancel cut short (line 981) before the turn releases `idle` (line 805); `CloseSession` waits
  for `idle` up to `closeWait` = 5 s (lines 39-42, 1293). This repository kills 2 s after sending `session/close`.
  So a kill can lose a cancelled turn's last entries. The phase stopped, as step 4 says.
* **Decision.** The owner chose "CloseTimeout, gobble 6 s (Recommended)": an agent-neutral `Spec.CloseTimeout`,
  zero keeping 2 s, set to 6 s by the gobble Spec. Not chosen: 5 s; keeping 2 s. The MADR records it as A3 (D6, Q6,
  History), amended before the field is built.
* **Added to P1:** step 4's "Yes" branch. The field in `acpagent.go`, its use in `session.go`'s
  `closeAgentSession`, and a test seen failing with the field ignored. Files: `internal/provider/acpagent/acpagent.go`,
  `session.go`, `lifecycle_test.go`.
* **Added to P2:** step 4 sets `CloseTimeout: 6 * time.Second`, and step 10 checks it, seen failing at zero.

## P1 — 2026-11-09: the handshake hook, the compaction meta source, and the close bound

Scripts and logs are in the session scratchpad (`p1_0179_planted.py`, `0179-p1-planted.log`, `fetch_gobble.py`,
`gobble-v0.4.0/`, `run_gates.py`, `0179-p1-*.log`).

* **Start state (C1).** Clean tree. `master` two commits ahead of `origin/master`: A2's `654b5a39` and P0's commit,
  not yet pushed.
* **Step 1, the facts re-read.** As the plan states them. The Spec's auth fields are still at `acpagent.go:115-131`,
  `selectACPAuthMethod` at `authselect.go:24-39`, and the close bound at `session.go:1365-1366`. The handshake's auth
  branch is now `acpagent.go:748-768`. No fact differs.
* **Step 2, the hook.** `Spec.AuthPreferenceFunc func(ctx context.Context) []string`, documented beside
  `AuthPreference`. The handshake passes its answer, called with the handshake's `initCtx`, to `selectACPAuthMethod`
  when the field is set, else `AuthPreference`. Nothing else changed.
* **Step 3, the test** (`acptest/auth_test.go`): `TestAuthPreferenceFuncChoosesTheUpstream` (the func's `vendor.b`
  is authenticated, and the func's context has ended once `Start` returns), `TestAnEmptyAuthPreferenceFuncRequiresAuth`
  (`ErrACPAuthRequired`, the agent receives only `initialize`), `TestAPinnedMethodWinsOverAuthPreferenceFunc`.
* **Step 4, the close bound:** "Yes", recorded in the deviation entry above. Then the fix:
  `Spec.CloseTimeout time.Duration` beside `StartTimeout`; `Provider.closeTimeout()` (the Spec's value, else
  `closeTimeout`); `bindSpec` sets the session's bound; `closeAgentSession` waits that bound. Tests in
  `lifecycle_test.go`: `TestSpecCloseTimeoutBoundsClose` (a 200 ms bound against an agent that stopped reading)
  and `TestCloseTimeoutDefaults` (zero keeps 2 s; 6 s is kept).
* **Step 4a, the compaction meta source.** `MetaFromCompaction` in `extensions.go`, with `MetaHandler`'s doc naming
  where it runs. `onCompactionUpdate` takes the notification's context, emits the compaction's event, then passes a
  non-empty `_meta` to `handleMeta`, declining `compaction_update._meta` only for a key with no handler. Its caller
  in `session.go` passes `ctx`. The test, `TestCompactionMetaReachesItsHandler`, is in package `acpagent`'s
  `compaction_test.go`, as P1's Files line names it, not on `acpagent/acptest` as step 4a's text says. Package
  `acpagent` is where the meta-handler tests already are (`metaext_test.go`), and where the queue a handler sees can
  be read to check the order. The existing `TestCompactionDeclinesWhatItCannotShow` passes unchanged: with no
  handler, `_meta` is still declined.
* **Seen failing (C4),** `p1_0179_planted.py`, a scratch clone of HEAD with the eight changed Go files copied in.
  Unplanted, the three test sets exit 0. Each break, exit 1, failing the named test with its own message:

  | Break | Failed | Message |
  | :--- | :--- | :--- |
  | the func's answer ignored | `TestAuthPreferenceFuncChoosesTheUpstream` | `authenticate methodId = vendor.a, want vendor.b, the func's answer` |
  | the same | `TestAnEmptyAuthPreferenceFuncRequiresAuth` | `StartOn err = <nil>, want ErrACPAuthRequired` |
  | the func given `context.Background()` | `TestAuthPreferenceFuncChoosesTheUpstream` | `the func's context is still live after Start returned; it must end with the handshake` |
  | `handleMeta` not called | `TestCompactionMetaReachesItsHandler` | `the handler was called 0 time(s), want 1` |
  | `handleMeta` before the emit | `TestCompactionMetaReachesItsHandler` | `0 event(s) queued when the handler ran, want 1: the compaction's event comes first` |
  | `closeAgentSession` ignores the bound | `TestSpecCloseTimeoutBoundsClose` | `Close took 2.00136075s with CloseTimeout 200ms; want well under the package's 2s` |
  | `bindSpec` drops the bound | `TestCloseTimeoutDefaults` | `CloseTimeout 0s: the session's bound is 0s, want 2s` |

  The clone was removed and checked gone.
* **Step 5.** `TestNoProviderIsNamedInAcpagent`: PASS, unchanged. `make pre-add-check` on the eight files: "8 file(s)
  clean (gofmt, golint, govulncheck)". The Go gates (C5), each exit 0: `make test` and `go test -race -count=1 ./...`
  (49 `ok` lines, no FAIL, panic or race line in either), `make lint`, `make staticcheck` (linux, darwin and windows;
  56 ignored findings, each an acp-go-sdk2 unstable-schema name, as before), `make vet-tags`.
* `make check-records markdownlint-docs` and the redact preview, then **staged** for the owner's commit: the eight Go
  files, this plan and the MADR (A3). P2 waits for the owner's word; its gate, a target release, is met by `v0.4.0`.

The owner committed P1 and approved P2: "committed, proceed".

## P2 deviation — 2026-11-09: `auth status --json` is released (MADR A4)

* **Found.** P2 step 1's re-read at `v0.4.0`: `gobble auth status --help` lists `--json`, "every key provider's
  credential source, never a credential". gobble-cli#13 was closed on 2026-11-07. Run against an empty scratch
  `GOBBLE_HOME`, it prints `{"providers":[{"provider","source","usable"}…]}` for the nine key providers. With a dummy
  `OPENAI_API_KEY` in the environment, `openai` reads `source: env`, `variable: OPENAI_API_KEY`, `usable: true`. With a
  dummy key stored by `gobble auth login openai --api-key -` (then logged out), it reads `source: auth.json`,
  `authType: api_key`, `usable: true`. Its source (`internal/cli/auth.go:352-400`, `405-466`) adds `models.json` as a
  source, `oauth` as an `authType`, and an `error` object with exit 2 when a file cannot be read. Step 6 had been
  written for the `auth check` fallback.
* **Decision.** The owner chose "Use auth status --json (Recommended)". Not chosen: build the fallback as planned;
  both, with a fallback. The MADR records it as A4 (D3, Q1, Consequences, History).
* **Changed:** step 6's `AuthStatus` bullet, step 10 item 8's stub (it prints a status report), and step 11's
  measurement. No file is added.

## P2 deviation — 2026-11-09: a replace can load a session its old process still holds (MADR A5)

* **Found.** P2 step 1's D6 check: `Manager.create`'s close-and-replace (`internal/session/manager.go:1241-1246`)
  closes the old session and starts the new one, which `session/load`s the same agent session. `acpagent`'s `Close`
  kills the process from a goroutine (`killProc`, `session.go:1289-1294`) and returns without waiting for it to
  exit. When `session/close` did not complete, gobble's old process can still hold the writer lock when the new one
  loads, and gobble refuses it with -32600. Step 1 says the phase stops.
* **Decision.** The owner chose "Close waits for the exit (Recommended)". Not chosen: a Spec hook for gobble to retry
  the load; accept the race. The MADR records it as A5 (D6, History).
* **Added to P2, as step 1a:** in `acpagent`'s `Close`, after the kill, wait for the process to exit, bounded at 2 s
  and by the caller's context. A test, seen failing with the wait removed. Files: `internal/provider/acpagent/session.go`
  and a test beside `lifecycle_test.go`'s.

## P2 deviation — 2026-11-09: no gobble prompt error carries a command (MADR A6)

* **Found.** P2 step 1's re-read: gobble `v0.4.0` sends a prompt's -32000 with data `{error}` alone
  (`acpserver/prompt.go:274-275`); only `authenticate`'s -32000 carries `command` (the probe above). A failed start
  reaches the phone as `session_create_failed` with the raw error text (`internal/ws/server.go:2165-2170`), which
  already holds the command. Step 6's last bullet and step 10's item 6 assumed a command on a prompt's error.
* **Decision.** The owner chose "Correct the record, no code (Recommended)". Not chosen: a new `acpagent` hook to
  rewrite a handshake auth error as prose. The MADR records it as A6 (D4, D12 item 6, History).
* **Changed:** step 6's last bullet (nothing decoded) and step 10's item 6 (two facts, tested with no new code).

## P2 — 2026-11-09: the gobble package, unregistered

Scripts and logs are in the session scratchpad (`probe_gobble_acp.py`, `0179-p2-probe.json`, `gobble-v0.4.0/`,
`time_auth_status.py`, `p2_0179_planted.py`, `0179-p2-planted.log`, `run_gates.py`, `0179-p2-*.log`).

* **Start state (C1).** Clean tree. `master` three commits ahead of `origin/master` (A2, P0, P1), not yet pushed.
  The installed `gobble version`: `gobble 0.4.0`, commit `2e83cce`, the target release; P2's gate holds.
* **Step 1, the facts re-read.** The Spec's hooks, the default client capabilities (with `session.compaction` and
  `session.notices`), `ModelsFromOption`, `Ops.Rename`/`Fork`, `Host.CallExtension`, `ForkOptions`/`ForkResult`, as
  the MADR has them. acpext `v0.2.0`'s `go doc` lists every name D9, D10 and D13 (as A2 changed it) use. A
  no-credential run of the installed `gobble acp` (`probe_gobble_acp.py`: `HOME` and `GOBBLE_HOME` a scratch folder,
  no other environment, no prompt) matches F3: twenty `_gobble/*` requests in `_meta.gobble.extensions`;
  `agentInfo.version` `0.4.0`; nine auth methods; `authenticate` of `openai` -32000 with data
  `{command, provider, reason: "no credential"}`, of an unknown id -32602; `session/new` with `thought_level` alone and
  three modes; `available_commands_update`, `current_mode_update`, `usage_update` and `_gobble/personality` before
  the answer; exit 0 12 ms after stdin closed, stderr empty. Three differences from F3, none a decision's:
  `agentCapabilities.auth: {}` is new; `session/list` lists an open session before its first message; with no model,
  `thought_level` ends at `xhigh`. `v0.4.0` accepts `--version` (A2). Three findings stopped the phase, each recorded
  above as a deviation: `auth status --json` (A4), the replace overlap (A5), no command on a prompt's error (A6).
* **Step 1a (A5).** `acpagent`'s `Close` waits, after the kill, for the process to exit (`agentproc.Process.Done`),
  bounded at 2 s (`exitWait`) and by the caller's context, with a warning when the bound ends it. The session keeps
  the process's `Done` channel, set beside `s.proc`. Tests: `TestCloseWaitsForTheAgentToExit`,
  `TestCloseStopsWaitingForAStuckExit`.
* **Step 2, the module.** `go get github.com/maccavelli/gobble-cli/acpext@v0.2.0`, then `go mod tidy`: `go.mod` requires
  it directly, with no `replace`; `go.sum` gains two lines; `go list -m all` gains that module alone (Confirmation 7).
* **Step 3.** `provider.IDGobble ID = "gobble"`, and its row in `TestIDConstants`.
* **Steps 4 to 9,** `internal/provider/gobble/`: `doc.go`; `gobble.go` (the Spec: `gobble acp`, `KnownGoodVersion`
  `0.4.0`, `CloseTimeout` 6 s, the default capabilities less `fs`, `terminal` and `auth`, the auth hooks,
  `ListModels`, `ModelsFromOption`, modes with `bypass` dangerous, the caveat, the retry handler, the
  `CompactionMeta` handler under `acpext.MetaKey`, `Ops.Rename` and `Ops.Fork`); `section.go` (the section, its
  defaults, the `auth_method_id` rule, `Descriptor()` with no `Floor` and no `Credentials`, not listed); `auth.go`
  (A4's `auth status --json`, cached 15 s and dropped after each write; `auth login --api-key - --json` with the key
  on stdin; `auth logout`; `auth login --method device --json`, its `device_code` the phone's flow); `catalog.go`;
  `ops.go`; `notify.go`. No `"_gobble/…"` literal in the package; the tests name methods through acpext too.
* **Step 10, the tests** (`gobble_test.go`, `auth_test.go`, `helpers_test.go`: a stub `gobble` that records argv and
  stdin and answers `auth status --json` from a file, and a small agent for starts that must fail). Items 1 to 6, 8,
  10, 11 and 12, the catalog, `bypass`, the `auth_method_id` rule, A3's 6 s, A4's status mapping and its cache.
* **Seen failing (C4),** `p2_0179_planted.py`, a scratch clone of HEAD with P2's files copied in. Unplanted, both
  packages exit 0. Each of 23 breaks, exit 1, failing the named test with its own message:

  | Break | Failed test, message |
  | :--- | :--- |
  | 1a: `Close` does not wait | `TestCloseWaitsForTheAgentToExit`: `Close returned after 116.166µs, before the process exited at 300ms` |
  | item 1: `fs` put back | `fs = map[readTextFile:true writeTextFile:true], want neither read nor write` |
  | item 1 (A2): `session.compaction` dropped | `session.compaction not advertised; gobble would fall back to its own notification` |
  | item 2: a fixed `AuthPreference` of acpext's ids | `authenticate methodId = gemini, want openai, the one ready upstream` |
  | item 2: key forced, the func answering every id | `StartOn err = <nil>, want ErrACPAuthRequired` |
  | item 3: `Ops.Compact` added | `declared [compact fork rename set_model set_thinking_level], want [fork rename set_model set_thinking_level]` |
  | item 4: the clone result read as `id` | `Fork: gobble's clone named no session` |
  | item 5: causes through a table, no fallback | `notices = [… "gobble is retrying (attempt 3)." …]`, the unknown cause lost |
  | item 5 (A2): a `_gobble/compaction` handler | `a _gobble/compaction produced {Type:compaction …}` |
  | item 6: -32603 for -32000 | `error kind = "server" (…), want auth` |
  | item 6: the data's `command` dropped | `StartOn err = acp authenticate (openai): {…"data":{"provider":"openai","reason":"no credential"}}, want it to name gobble's command` |
  | item 8: the key in argv | `argv = ["auth login claude --api-key - --json sk-test-not-a-real-key --workspace ws-1"]` |
  | item 8: `claude` given a login | `err = <nil>, want ErrAuthMethodUnsupported` |
  | A4: an `env` key `Configured` | `an env key: openai = "configured", Configured true; want configured and not removable` |
  | item 10: an acpext import in `acpagent` | `…/acpagent/zz_acpext_planted.go imports acpext; only internal/provider/gobble may` |
  | item 11: `ListModels` unset | `spawning a short-lived agent to read the model catalog`, and the test fails |
  | item 12: any reason notifies | `notices = ["Context compacted (manual), from 0 tokens." …]` |
  | item 12: the reason check dropped | `notices = [… "Context compacted (), from 110101 tokens." …]` |
  | item 12: reasons through a table, no fallback | `notices = ["Context compacted (threshold), from 110101 tokens."]`, the future reason lost |
  | the `recent` group filtered | `models = [{ID:openai/gpt-x … Group:openai …} …], want gobble's` |
  | `bypass` not dangerous | `mode "bypass" dangerous = false, want bypass alone dangerous` |
  | the `auth_method_id` check removed | `auth_method_id "gobble": err = <nil>, want ok false` |
  | A3: `CloseTimeout` zero | `CloseTimeout = 0s, want 6s` |

  The first run caught 19. Four breaks did not compile, a fault in the break script (a missing import, an unused
  variable), not in the tests; they were fixed and rerun. The second run caught 22, and its one miss was a fault in
  a test: `waitNotices` drained every event and kept only the notices, so the `_gobble/compaction` break's event was
  discarded before the check read it. The test now keeps every event and sends that notification first, and the
  third run caught all 23. The clone was removed and checked gone.
* **Step 11, measured.** `gobble auth status --json` against the installed `v0.4.0` and a scratch `GOBBLE_HOME`, five
  runs: 135.4, 52.6, 53.8, 55.1 and 56.1 ms; median 55.1 ms, under the 1 s limit.
* **Step 12.** `make pre-add-check` on the fifteen Go files: clean (gofmt, golint, govulncheck). The Go gates (C5),
  each exit 0: `make test` and `go test -race -count=1 ./...` (50 `ok` lines, `internal/provider/gobble` among them,
  no FAIL, panic or race line in either; `TestSDKIsImportedOnlyByGrok` and `TestNoProviderIsNamedInAcpagent` pass
  unchanged), `make lint`, `make staticcheck` (linux, darwin and windows; 56 ignored findings, as before),
  `make vet-tags`.
* **Not done: `make ci-windows` on the Windows host** (C5 for P2). It runs on that host, which this session does not
  reach. The package's stub tests skip on Windows (`testexec.SkipIfNoPOSIXShell`), so there it compiles the package
  and runs its other tests. CI's Go (windows/amd64) job runs the same on the push. P2 stays open on this item until
  the owner runs it there, or accepts CI's job in its place. *2026-11-09: the owner accepted CI's job in its place
  ("accept CI for windows"). The agent had marked the item unreachable without trying the `available-dev-hosts`
  skill, which exists for such a host; `make ci-windows` itself exists (`make/ci-windows.mk`, MADR 0145) and, off
  Windows, skips with exit 0.*
* **CI on the push.** The owner committed P1 (`821a58a3`) and P2 (`f4248820`) and pushed them with A2's and P0's
  commits. Run `42701341383` on `f4248820`: **success**. Go (test), Go (linux/arm64), **Go (windows/amd64)**,
  Go (darwin/arm64), Flutter (analyze & test), Flutter (test on windows) and Live supervisors (runit, s6) passed;
  Android APK and the two self-update release jobs were skipped, as on every push that is not a tag. P2 is done.
* `make check-records markdownlint-docs` and the redact preview, then **staged** for the owner's commit: `go.mod`,
  `go.sum`, `internal/provider/provider.go` and its test, `acpagent`'s `acpagent.go`, `session.go` and
  `lifecycle_test.go`, the ten files of `internal/provider/gobble/`, this plan and the MADR (A4 to A6).
