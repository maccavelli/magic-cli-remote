---
status: in-progress
date: 2026-11-12
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

*2026-11-11: P4 lands now, against gobble `0.4.0`, before `0074-PLAN-remote-provider-auth-from-phone.md` P23 step
5, with MADR D14's gate 3 knowingly unmet (MADR A10). The order, its reason and what P4 still owes are in the entry
"P4 — 2026-11-11" at the end of this plan.*

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

### P6 — acceptance and close-out (MADR Confirmation) — moved to 0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md P7 (MADR A9.5, 2026-11-12)

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

The owner committed P2's CI record and approved P3: "committed, proceed". Asked for P3's provider and model (C10),
the owner chose "opencode-zen, a free model (Recommended)".

## P3 deviation — 2026-11-09: the first capture ran on a paid model

* **Found.** The first capture run chose `opencode-zen/deepseek-v4-flash-free` from `gobble models list`, a list the
  session's `model` option does not match. That option, gobble's live catalog, offers ten free opencode-zen models,
  and not that one. gobble refused the switch (-32602, `unknown model`). The capture driver recorded the refusal and
  went on, so the session stayed on gobble's default, `opencode-zen/claude-fable-5-2`, a paid model. Two model turns
  ran on it, the tool-call prompt and the mid-turn prompt: 23,648 input tokens in all (15,702 of them cache reads,
  7,936 cache writes) and 201 output tokens, which the prompts' `usage` reports; gobble sent no cost. `/model`,
  `/thinking` and `/compact` ran in gobble without a model call, and `/compact` was refused, "Nothing to compact
  (session too small)", so no compaction was captured. No retry occurred. This was the agent's fault: the owner had
  approved a free model only, and the driver should have stopped at the refused switch.
* **Decision.** The owner chose "Discard, fix, rerun free (Recommended)". Not chosen: stop P3 for now. The first
  run's frames are discarded, not committed.
* **Changed for the rerun:** the model is `opencode-zen/deepseek-v4.1-flash-free`, taken from the session's own
  option; the driver stops before any prompt unless that switch succeeds; it sends longer prompts before `/compact`
  so gobble has a session to compact, and fails the run if no compaction update arrives.
* **The second run** switched to the free model, and every prompt then failed with -32603, data
  `{"error":"llm: api status 429: … opencode-zen/chat_completions HTTP 429 FreeUsageLimitError (retry-after
  1h33m21s): Free usage limit reached. …"}`. No prompt completed, so nothing was spent, and no compaction or tool call
  was captured. Asked again, the owner chose "Approve a named paid model", then `opencode-zen/deepseek-v4.1-flash`, the
  paid model of the same name, from the session's own option. The third run uses it, with the same guards.
* **A finding outside this plan,** from the second run: `agenterr` presented gobble's quota error as a server error
  ("The model provider had a server error…"). `ExtractText` takes the JSON-RPC `message`, "Internal error", and the
  cause in `data.error` ("quota exhausted", HTTP 429) is not read. It is recorded here for its own record, and not
  changed by this plan.

  *Note, 2026-11-09: that record is
  [0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md](0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md),
  whose PLAN fixed it: this error now reads as kind `quota`, scope `free_tier`, provider `opencode-zen`, with its reset
  and the provider's own words (0243 Confirmation 1). Its P4 also sets the gobble Spec's `ErrorLimit`, which reads
  acpext v0.3.0's structured `ErrorData` once a gobble release sends it.*
* **The third run,** on `opencode-zen/deepseek-v4.1-flash`, captured everything but a compaction: `/compact` was again
  "Nothing to compact (session too small)". gobble keeps the last `compaction.keepRecentTokens` (20,000 by default,
  gobble `compaction/compaction.go:47`, `:99-112`) out of any compaction, and the session was 7,838 tokens. Asked,
  the owner chose "Rerun the whole capture". The fourth run's scratch home has its own `settings.yaml`, which sets
  `defaultProvider: opencode-zen`, `defaultModel: deepseek-v4.1-flash` (so gobble's default never reaches a paid
  model unapproved) and `compaction.keepRecentTokens: 1000`. That is the throwaway gobble's own setting; nothing the
  daemon sends changes. The fourth run is the fixture.
* **Spend on the approved model:** the third run's five model turns, 36,462 input tokens (28,708 cache reads) and
  2,093 output; the fourth run's, 36,513 input (28,686 cache reads) and 2,217 output, plus one compaction summary,
  whose usage gobble does not report in a prompt result.

## P3 — 2026-11-09: the release fixtures

Scripts and logs are in the session scratchpad (`p3-capture/`: `zz_capture_test.go`, `run_capture.py`,
`out4/steps.jsonl`, `out4/go-test-*.log`, `inspect.py`, `scan_ids.py`; `p3_0179_planted.py`, `0179-p3-*.log`).

* **Start state (C1).** Clean tree, level with `origin/master` after P2's push.
* **Step 1, the capture** (`run_capture.py`, the fourth run): a scratch clone of HEAD with the driver
  (`zz_capture_test.go`, build tag `capture_gobble`, never committed) added; `HOME` and `GOBBLE_HOME` a scratch home;
  `OPENCODE_API_KEY` alone, passed through and never printed; `MCREMOTE_WIRE_CAPTURE_DIR` set. The driver, through
  `gobble.New` and `provider.Session`, as the daemon would: set the model by `session/set_config_option` (and stops
  before any prompt if refused), thought level `low`; `/model`, `/thinking`; a turn reading `notes.txt` (a `tool_call`
  and its update, no permission asked); Rename and Fork idle (the title updated; the clone named
  `01a1c272-bda4-…`), then each refused mid-turn with -32600 "a prompt is already running"; a counting turn; three
  prose turns; `/compact` (two compaction events and a notice, from A2's handler); a close; `session/load` (the
  compaction replayed). A second phase, with no key and an empty `GOBBLE_HOME`: `Start` fails `ErrACPAuthRequired`
  after `initialize` alone. No retry occurred; gobble cannot be made to retry on demand, as opencode's fixture notes
  for its own. The clone was removed.
* **Step 2, the cold start** (D1): spawn to `session/new`'s answer, five times: 445, 510, 489, 424 and 556 ms; median
  489 ms, far under the 15 s limit.
* **Step 3, redaction.** `wirecap` rewrote the scratch home to `/home/user`. `scan_ids.py` over the frames: no local
  account name, no host name, no path under `/Users/`, `/private/`, `/var/folders/` or `/tmp/`; the one home path is
  `/home/user/work/notes.txt`. The redact preview, with the fixture in the tree: 0 files. `meta.json` names the
  provider, version, source, date, frame count (312), what was redacted and what the run did. `capture_host` is
  empty: with a scratch `HOME`, `wirecap` had no host key and wrote no tag, so `TestCommittedFixturesCarryNoIdentifiers`
  checks the running host's name in it, the strictest choice (MADR 0151 D12). *Corrected 2026-11-10 by the P3
  deviation of that date:* that choice fails on the Linux test host, whose name is an ordinary word in a public model
  id. `capture_host` is now this Mac's tag.
* **Step 4, the fixture tests** (`fixture_test.go`): item 7, `TestFixtureSendsOnlyKnownMethods`; item 9 with A2,
  `TestFixtureShapesAreAcpexts` (initialize's `_meta.gobble`, `_gobble/personality`, the clone result, every error's
  data and each `compaction_update`'s `_meta.gobble`, each decoded with unknown members refused and encoded back
  unchanged, and each kind required to be present); `TestFixtureCompactionIsTheStandardUpdate` (A2's pair and
  replay); `TestFixtureIsTheKnownGoodRelease` (`meta.json` agrees with the file and `KnownGoodVersion`).
* **Seen failing (C4),** `p3_0179_planted.py`, a scratch clone's copy of the frames, `meta.json`'s count kept in step
  so the break, not the count, fails the test. Unplanted, exit 0. Each, exit 1:

  | Break | Message |
  | :--- | :--- |
  | a `_gobble/compaction` notification inserted | `gobble sent [_gobble/compaction]; none is allowed (MADR 0179 D12 item 7)` |
  | an `fs/read_text_file` request inserted | `gobble sent [fs/read_text_file]; none is allowed` |
  | a personality frame given a member | `… does not decode as *acpext.PersonalityNotice: json: unknown field "planted"` |
  | the clone result's `sessionId` renamed `id` | `the fixture holds no clone result; the check would pass without checking it` |
  | a compaction meta given a member | `… does not decode as *acpext.CompactionMeta: json: unknown field "planted"` |
  | the replayed update dropped | `2 compaction updates, want the live pair and the replay` |
  | `meta.json`'s count off by one | `TestFixtureIsTheKnownGoodRelease` fails |

  The clone was removed and checked gone.
* **A gap in step 4 as written.** Its item 7 break, "a stray `_gobble/get_state` call added to Rename", cannot fail a
  test on this fixture: `wirecap` taps only the agent's output (`internal/wirecap/wirecap.go:1-3`), so a call this
  repository makes is not in the frames. Item 7 here checks what gobble sends; what this repository sends for
  Rename and Fork is pinned by P2's item 4, which compares the exact params. The two breaks used instead are in the
  table. This is for the owner to accept or redirect.
* **Deviation — 2026-11-09: item 7 in both directions (MADR A7).** Asked to assess the gap with three options, the
  owner chose "Add a recorded-session test (Recommended)". Not chosen: capture both directions with agentproc's
  `InputTap` (shared capture code, a recapture, and still a frozen record); accept as staged. The fixture checks
  above stay as gobble's side. **Added:** `TestRecordedSessionSendsOnlyTheContract` in `fixture_test.go`, on
  `acptest`. It drives a session through the gobble Spec: the model and thinking switches, a prompt with a
  `compaction_update`, Rename, Fork, an `fs/read_text_file` request from the agent (for a probe file of the test's
  own), a close, then a load on a second agent. Every request this repository sent must be in the contract
  (`initialize`, `authenticate`, `session/new|load|close|set_config_option|prompt|cancel`,
  `_gobble/set_session_name`, `_gobble/clone`), each of `session/prompt`, the two `_gobble/*` methods,
  `session/close` and `session/load` must have been sent, and the `fs/*` request must be refused -32601. Seen
  failing (`p3b_0179_planted.py`, a scratch clone; unplanted, exit 0):

  | Break | Message |
  | :--- | :--- |
  | a stray `_gobble/get_state` call in `rename`, the plan's original break | `this repository sent gobble _gobble/get_state, outside the contract (MADR 0179 D12 item 7)` |
  | client files offered (`c.Fs` left as the default) | `gobble's fs/read_text_file was answered {"content":"probe\n"} / <nil>; want -32601, since the Spec offers no files` |

  `make pre-add-check` on `fixture_test.go`: clean.
* `make pre-add-check` on `fixture_test.go`: clean.
* **Step 5, the Go gates (C5),** each exit 0: `make test` and `go test -race -count=1 ./...` (50 `ok` lines, no FAIL,
  panic or race line), `make lint`, `make staticcheck`, `make vet-tags`. `TestCommittedFixturesCarryNoIdentifiers`
  ran on `internal/provider/gobble/testdata/wire/0.4.0/frames.jsonl` and passed. Run again after A7's test was
  added: each gate exit 0, 50 `ok` lines, no FAIL, panic or race line.
* `make check-records markdownlint-docs` and the redact preview, then **staged** for the owner's commit: the fixture
  (`frames.jsonl`, `meta.json`), `fixture_test.go` and this plan.


* `make check-records markdownlint-docs` and the redact preview, then **staged** for the owner's commit: `go.mod`,
  `go.sum`, `internal/provider/provider.go` and its test, `acpagent`'s `acpagent.go`, `session.go` and
  `lifecycle_test.go`, the ten files of `internal/provider/gobble/`, this plan and the MADR (A4 to A6).

## P4 deviation — 2026-11-10: "no switcher", and a fourth shipped config (MADR A8)

On "committed and pushed, proceed" after 0243's close-out, P4 started on a clean tree level with `origin/master`
(C1), its gate met (P3 committed). Steps 1 to 4, 6 and 7 were written; two facts differed from the plan (C2, C11).

* **Step 5: "no switcher" is not what the test checks.** With gobble's case added to
  `TestProviderAuthInterfaceConformance`, it failed: `auth_conformance_test.go:78: UpstreamSwitcher: got true, want
  false`. The test asserts Go types, and `acpagent.Provider.SetActiveUpstream` (`acpagent.go:402-408`) exists for every
  ACP agent, answering `ErrAuthUnsupported` when the Spec sets no hook, as gobble's does. Read on: no ACP Spec sets the
  hook (`git grep SetActiveUpstream`), so opencode and grok cannot switch either, and the phone shows its "Active
  upstream" control to any provider with two configured upstreams (`provider_detail_screen.dart:256-271`), with no
  wire flag to say otherwise (`protocol/messages.go:798-802`). Asked three times, the owner chose a capability probe
  in `acpagent`, carried to the phone, in a record of its own, with this phase waiting: MADR A8, and
  [0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md](0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md)
  with its PLAN. Step 5's gobble case is taken out of `auth_conformance_test.go`, which is back at HEAD, so 0244 can
  change that file alone; it returns after 0244 with 0244's `UpstreamSwitchWired` column (false for gobble).
* **Step 3: a fourth shipped config.** The service seed `internal/cli/service/defaults_mcremote.yaml`, which
  `setup-service` writes and `TestTheShippedConfigFilesLoadThroughTheSections` loads, has a block for every other
  provider. The owner chose "Add it to the seed (Recommended)": it gets the same gobble block, and joins this phase's
  files.
* **The state P4 waits in.** Unstaged in the working tree: `Makefile`, the four configs, `docs/guides/config.md`,
  `internal/command/resolve_order_test.go`, `internal/config/prewarm_write.go` and its test,
  `internal/provider/registered/registered.go`, and the new `internal/provider/gobble/live_gobble_test.go`. Four planted
  breaks were caught (`p4_0179_planted.py`; gobble unregistered, prewarm refusing gobble, an unknown key in gobble's
  block, `config.md` documenting a key gobble lacks), and `go vet -tags live_gobble` is clean; the live suite skips
  without the owner's model and key, so nothing was spent. The gates (step 8) and `make ci-windows` have not run.
  The record of these steps is written when P4 resumes.

*Note, 2026-11-10:*
[0244-PLAN-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md](0244-PLAN-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md)
*is `complete` (CI run 42841924687 passed on `15b8d9d5`). `provider.UpstreamSwitchWired` and the conformance test's
`wantSwitchWired` column are on master, so P4 may resume: step 5's gobble case goes back in with the type side true
and `wantSwitchWired` false (MADR A8).*

## P3 deviation — 2026-11-10: the fixture names no capture host, and fails on the Linux test host

**Found** at
[0217-PLAN-update-every-pin-to-its-current-release.md](0217-PLAN-update-every-pin-to-its-current-release.md)'s P3h,
step 5, in its Linux host run. `TestCommittedFixturesCarryNoIdentifiers` fails on the Linux test host. The fixture is
`internal/provider/gobble/testdata/wire/0.4.0/frames.jsonl`, at lines 7, 8, 9, 12 and 310: "this host's name,
unredacted (MADR 0151 F9)".

* **It predates 0217's move.** The same test fails on that host at plain `37aebdcb`, with none of P3h's files applied.
  P3h changes neither the fixture nor the test.
* **Nothing identifying is in the fixture.** Each hit is one of opencode-zen's public model ids, listed in gobble's
  catalog. It holds the Linux test host's short name as an ordinary word. This is the case
  [0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md](0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md)
  F11 found in opencode's fixture.
* **The cause is P3 step 3's choice.** The capture ran on this Mac under a scratch `HOME`, so `os.UserConfigDir`
  held no wirecap host key and `wirecap.For` wrote no tag. P3 then recorded `"capture_host": ""`, "the strictest
  choice". By 0151 D12, an empty `capture_host` applies the host-name rule on every host, so a host whose name is an
  ordinary word in public data fails.
* **Why CI did not see it.** No CI runner's name occurs in the fixture. The Windows test host's does not either.
* **It would recur.** P4's `live_gobble_test.go` sets a scratch `HOME` (`newLiveEnv`) before it captures through
  `wirecap` (`TestLiveGobbleSession`). So P6's live run would write no tag again.

**Decision (owner, 2026-11-10): "follow recommendations and proceed",** on the first of three resolutions. The other
two were staging 0217's P3h with the Linux gate failing and fixing afterwards, and folding the fix into P3h. 0217's P3h
is pins only, and the fixture is this plan's.

1. **The fixture, in its own commit, before 0217's P3h.** `meta.json`'s `capture_host` becomes this Mac's wirecap tag,
   `06e8a1e8…`, the full 64-hex value. It was computed with `wirecap.HostTag(false)` in a scratch clone. It equals
   the `capture_host` of opencode 2.2.3's fixture, which 0151 F11 says was captured on this Mac. No frame changes.
2. **The live test, in P4.** Before it swaps `HOME`, `newLiveEnv` points `wirecap.EnvHostKey` at this host's real
   key. It then checks that the path gives the same tag as wirecap's own lookup did. After the capture,
   `TestLiveGobbleSession` requires the capture's `capture_host` file to hold that tag, when the host has a key. So
   P6's recapture records its host, and a key path that drifts fails loudly. Both helpers live in a new, untagged
   `internal/provider/gobble/wirecap_host_test.go`, with a unit test that runs without quota. That test is seen
   failing on planted breaks in a scratch clone (C4). **P4's files gain** that file.

**Option 1 tested before the decision** (`fixcheck_local.py`, `fixcheck_hosts.py` in the session scratchpad; each
a scratch clone of `37aebdcb` with only `meta.json` changed, removed afterwards; read per subtest from `go test
-json`):

| Host | Unfixed | Fixed | `go test` of `internal/provider/gobble` and `internal/wirecap`, fixed |
| :--- | :--- | :--- | :--- |
| This Mac, the capture host | pass, host-name rule applied | pass, rule **still applied** | exit 0 |
| The Linux test host | **fail**, the five lines above | pass, rule skipped: "captured on another host" | exit 0 |
| The Windows test host | pass, rule applied | pass, rule skipped: "captured on another host" | exit 0 |

* **Seen failing on the capture host:** with the fix, this Mac's own name planted in frame 15 of a scratch copy fails
  the gobble subtest on this Mac, with "line 15: this host's name, unredacted". So the guard still holds where the
  frames were taken.
* The email-address and Windows-path checks apply on every host either way.

**Not done here:** the grok, codex and kilo fixtures record no `capture_host` either. They pass on every host today,
but a later public model id could collide in the same way. Each is its own provider's record.

## P7 — proposed 2026-11-10, approved 2026-11-11: MADR A9, acpext `v0.4.0`, the device table, the bounded code read, gobble's `reason`

**Status: approved 2026-11-11; 7a staged 2026-11-12 (the entry "P7a — 2026-11-12" at the end of this plan), 7b and
7c not started.** The owner accepted MADR A9 and approved this phase on 2026-11-11
("accept and approve 0179 a9"). It starts at its place in the order (the entry "P4 — 2026-11-11" below): after
`0074-PLAN-remote-provider-auth-from-phone.md` P23 steps 6 to 9, on the owner's word for each of 7a, 7b and 7c.
~~Nothing here starts before the owner accepts MADR A9 and approves this phase.~~ The owner answered
A9.2 on 2026-11-10 with (a), the two id tables and their partition test; step 2 builds it. The same day the owner
answered the timing (DF): this phase lands now, ahead of
`0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`.

**Gate: met.** The pin commit of `0217-PLAN-update-every-pin-to-its-current-release.md` (`go.mod`, `go.sum`,
`.github/workflows/ci.yml`, and that MADR and PLAN) landed at `912b9aa1`. P4 need not be committed: P7's files are disjoint
from P4's (MADR A9, "How it fits P4"). C1 to C11 apply.

*2026-11-11: the owner's order of that day (the entry "P4 — 2026-11-11" below) places this phase after P4's commit
and after `0074-PLAN-remote-provider-auth-from-phone.md` P23 steps 5 to 9, whose step 5 edits
`internal/provider/gobble/auth.go` first. So the lines this phase cites in that file are read again at its start
(C2). `0217-PLAN-update-every-pin-to-its-current-release.md` P3j, which moves the acp-go-sdk2 line of `go.mod` and
`go.sum`, is proposed to land before 7a, so that step 1's "`go.mod` changes that one line" is checked against one
pin move at a time.*

**Three commits** (MADR A9.1), so the bump can be reverted alone: **7a**, the pin with A9.2's tables and partition
test (steps 1 and 2, with step 0's records); **7b**, A9.3, the bounded code read (step 3); **7c**, A9.4, gobble's
`reason` (step 4). Step 5's tests go with the commit of the code they test.

Files: `go.mod`, `go.sum`, `internal/provider/gobble/auth.go`, `internal/provider/gobble/auth_test.go`, this plan, the
MADR.

0. **The records, on acceptance.** MADR A9 marked accepted with the owner's A9.2 answer, (a) of 2026-11-10, and DF; in-place annotations
   of D3's catalog and `StartDeviceAuth` bullets, D5's acpext rule, D12 item 8 (`StartDeviceAuth` for a provider
   without a login, now without a `deviceSignIn` entry) and D13's recapture rule, each a dated italic note;
   a History line; P6's heading annotated "moved to `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7 (MADR A9.5)". `make check-records
   markdownlint-docs`; the redact preview. **Stage** the records only.
1. **The pin.** In a scratch clone first: `go get github.com/maccavelli/gobble-cli/acpext@v0.4.0`, `go mod tidy`.
   `go.mod` changes that one line; `go.sum` that module's lines only. Any other change is a deviation (C11).
2. **The device tables (A9.2, answered (a)).** `deviceSignIn = {"grok", "kilo"}` and `noPhoneSignIn = {"openai"}`
   beside `methodAPI`, each with a comment naming MADR A9.2 and its removal at 0246's cut-over; `methods` lists
   `oauth_device` for `deviceSignIn` alone; `startDeviceAuth` refuses any other id with
   `provider.ErrAuthMethodUnsupported`, as it refuses a method today (`auth.go:329-331`). The tables and their test are
   removed by
   [0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)
   P4 step 5.
3. **The bounded read (A9.3).** `firstDeviceCode(events, timeout)` scans in a goroutine and returns at the first
   `device_code`, the first `error`, the end of output, or `deviceCodeScanTimeout` (30 s); on the timeout,
   `startDeviceAuth` calls `cancel()` and `end()` before it returns.
4. **gobble's `reason` (A9.4).** `statusRow.Reason` (`json:"reason"`); one helper, `row.ready()`, `Usable &&
   Reason == ""`, read by `status`, `methods` and `readyUpstreams`.
5. **Tests, each seen failing on a planted break in a scratch clone (C4):**

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestCatalogIsAcpextsProviders` (rewritten, `auth_test.go:21`) | upstreams in acpext's order; `oauth_device` exactly for `deviceSignIn`; openai has none at `acpext v0.4.0`; grok's browser row as today | `Login` read again |
   | `TestDeviceTablesPartitionAcpextsLogins` | every acpext `Login` id is in exactly one table; every table id is one acpext lists | `kilo` dropped from `deviceSignIn` |
   | `TestDeviceAuthRefusesOpenAI` | `startDeviceAuth` for `openai:device` runs no child and returns `ErrAuthMethodUnsupported` | the old `!p.Login` guard |
   | `TestDeviceCodeReadIsBounded` | a stub that prints nothing and sleeps returns an error within 30 s, with a test seam shortening the bound to 100 ms, and the child is killed | the timeout case removed |
   | `TestReasonMeansNotConfigured` | a row `{usable: true, reason: "oauth_unused"}` is `missing`, its methods not `Configured`, and its id not in `readyUpstreams` | `reason` ignored in `readyUpstreams` |
   | `TestFixtureShapesAreAcpexts` and the other fixture tests (existing) | pass unchanged at `acpext v0.4.0` | — |

   `TestDeviceAuthNeedsALogin` (`auth_test.go:88`) is renamed `TestDeviceAuthNeedsADeviceSignIn` with the same
   assertions, and named here.
6. **Gates (C5),** on the scratch clone with steps 1 to 5's files: `make pre-add-check FILES="internal/provider/gobble/auth.go
   internal/provider/gobble/auth_test.go"`, `make test`, `go test -race -count=1 ./...`, `make lint`, `make
   staticcheck`, `make vet-tags`; the Linux test host's `go test -race -count=1 ./...` and the Windows test host's
   `make ci-windows` on scratch copies; `python3 ~/.global-git-hooks/github-disclosure.py redact`, from the
   repository root, before each staging. **Stage** by commit: 7a, `go.mod`, `go.sum`, `auth.go`'s tables and
   `methods`/`startDeviceAuth` change, their tests (`TestCatalogIsAcpextsProviders`, `TestDeviceTablesPartitionAcpextsLogins`,
   `TestDeviceAuthRefusesOpenAI`, the renamed `TestDeviceAuthNeedsADeviceSignIn`), and this plan's entry; 7b,
   `firstDeviceCode`'s bound and `TestDeviceCodeReadIsBounded`; 7c, `statusRow.Reason`, `row.ready()` and
   `TestReasonMeansNotConfigured`. Each is committed by the owner before the next is staged. Before any push the owner
   asks for, the guard over the outgoing commits (`AGENTS.md`, "Host identifiers never appear in records"):

   ```bash
   echo "refs/heads/master $(git rev-parse HEAD) refs/heads/master $(git rev-parse origin/master)" | python3 ~/.global-git-hooks/github-disclosure.py pre-push origin "$(git remote get-url origin)"
   ```

Acceptance: the tests pass and each plant failed; `go list -m github.com/maccavelli/gobble-cli/acpext` reports
`v0.4.0`; no other module moved.

**Rollback,** newest first, each alone: reverting 7c reads `usable` alone again; reverting 7b removes the 30 s bound;
reverting 7a returns the pin to `acpext v0.3.0` and removes the tables and their test together, which needs 7b and 7c
reverted first only if they no longer apply cleanly.

## P4 — 2026-11-11: P4 lands now, against gobble `0.4.0`; the order, and what P4 still owes

**The owner's ruling, 2026-11-11,** relayed by the coordinator of the four-repository auth-contract work; none of the
owner's wording was relayed, so none is quoted. P4 is finished and committed now, against gobble `0.4.0`
(`KnownGoodVersion`), before
[0074-PLAN-remote-provider-auth-from-phone.md](0074-PLAN-remote-provider-auth-from-phone.md) P23 step 5. MADR A10,
accepted the same day, records that D14's gate 3 is knowingly unmet while it does.

**This entry is an order and a to-do list. It is not an execution record.** Nothing in P4 was run or changed for it.
The only commands behind it are the read-only checks it names, run on 2026-11-11 at `14200b6e` with P4's unstaged
files in the tree.

**The order in this repository, as approved:**

1. `0074-PLAN-remote-provider-auth-from-phone.md` P23 steps 3 and 4 (another session; step 3 is next).
2. **This plan's P4,** finished and committed.
3. `0074-PLAN-remote-provider-auth-from-phone.md` P23 steps 5 to 9.
4. This plan's P7a, 7b and 7c, once MADR A9 is accepted.
5. A release carrying those.
6. [0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)
   P1, once go-llmprovider-sdk's `authcontract/v0.1.0` resolves (its gate G1).
7. That plan's P2 onward, under its own rule that no release is tagged from P2 until P6 is committed (its C5).
8. `0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`.

The acp-go-sdk2 move to `v1.36.0` is not one of the eight. It is
`0217-PLAN-update-every-pin-to-its-current-release.md` P3j, a commit of its own that changes `go.mod`, `go.sum` and
new test files, none of them a file of P4 or of 0074's P23. That phase proposes its place: after step 2's commit and
before step 4's first.

**Why P4 goes before 0074's P23 step 5: three files have more than one editor.** Each row was read from the three
plans on 2026-11-11.

| File | Edited by |
| :--- | :--- |
| `internal/provider/auth_conformance_test.go` | this plan's P4 step 5 (gobble's case); 0074's P23 step 5 (codex's row changes, and a `wantOwnedDeviceAuth` column is added); 0246's P3 step 6 (a `SignInStarter` column and a `wantSignInWired` column) |
| `internal/provider/gobble/auth.go` | 0074's P23 step 5 (it is in that step's file list, for the names its MADR's D35 removes and the one `host_oauth` constant); this plan's P7a, 7b and 7c; 0246's P4 step 5 |
| `Makefile` | this plan's P4 step 6 (`live-gobble` and its `.PHONY` entry, unstaged in the tree now); 0246's P1 step 4 (`check-authcontract`) |

P4's hunks are the ones already in the working tree, so they are committed first and each later editor starts from a
file that holds them. One consequence falls on a record this entry cannot write, because the other session owns it:
0074's P23 step 5 adds a column to the conformance test's table, and with P4 committed first that step also has to
give gobble's row its value in the new column. Its text names codex's and grok's rows only.

**What P4 still owes before it can close.** Items 1 to 4 are the audit's, each checked again in the tree on
2026-11-11. Item 5 is from this plan's own text.

1. **Step 5's gobble case is not in `internal/provider/auth_conformance_test.go`.** A case-insensitive search of the
   file for "gobble" finds nothing, and the file equals `HEAD`. The case was taken out on 2026-11-10 for 0244 (the P4
   deviation above). It goes back in with status, writer, catalog and device auth true, the type side of the switcher
   true and `wantSwitchWired` false (the note of 2026-11-10; MADR A8), and is seen failing first (C4). Step 5's other
   half is in the tree: gobble is in `providerTables` (`internal/command/resolve_order_test.go:24`).
2. **Step 8 has not run.** No entry records C5's Go gates for P4 (`make pre-add-check FILES=…`, `make test`,
   `go test -race ./...`, `make lint`, `make staticcheck`, `make vet-tags`) or `make ci-windows` on the Windows test
   host. The P4 deviation of 2026-11-10 says they had not run, and nothing since records a run.
3. **The planted-break evidence for `internal/provider/gobble/wirecap_host_test.go` is not recorded.** The P3
   deviation of 2026-11-10 (its decision, item 2) requires that file's unit test to be "seen failing on planted breaks
   in a scratch clone (C4)". The file is in the tree, untracked, with `TestWirecapHostKeySurvivesAHomeSwap` and
   `TestCaptureHostMismatch`. No entry names a plant for either or quotes a failure. The four plants that are recorded
   (`p4_0179_planted.py`) are for steps 1 to 4.
4. **`internal/provider/gobble/live_gobble_test.go` vets clean and has never run.** `go vet -tags live_gobble
   ./internal/provider/gobble/` exits 0 with no output (go1.27.4, 2026-11-11, on the tree as it stands). No entry
   records a run of `make live-gobble`. Step 6 keeps it out of this phase, C10 needs the owner's word for it, and by
   the owner's Q19 answer the run is
   `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L4 (MADR A9.5, proposed).
   So P4 commits a live suite that compiles and is unproven, the capture-host check of the P3 deviation included, and
   P4's record says so when it closes.
5. **P4's execution record is unwritten.** The P4 deviation of 2026-11-10 ends "The record of these steps is written
   when P4 resumes." It owes steps 1 to 4, 6 and 7 with the four plants' quoted failures, step 5's, the fourth config
   (`internal/cli/service/defaults_mcremote.yaml`), the live test's change from the P3 deviation, step 7's search run
   again on the final tree, and the list of staged files.

**Gate 3 while P4 lands.** P4 is committed with this repository on acp-go-sdk2 `v1.35.0`, or `v1.36.0` if 0217's P3j
has landed, and gobble `0.4.0` built on `v1.34.2`. MADR A10 states that risk and where the gate is checked again.
P4's close records the two tags as they are at its commit.

## P4 — 2026-11-11: registration, executed

On the owner's word in this session ("confirmed proceed", after P23 step 3 landed as `f6e99522`), P4 was finished
against gobble `0.4.0`, in the order the entry above sets. It closes what that entry listed as owed. Nothing here
spent quota or ran a live suite.

**What P4 changes, by step.**

1. `internal/provider/registered/registered.go`: `gobble.Descriptor()` right after `fake.Descriptor()`.
2. `internal/config/prewarm_write.go`: `KnownProviderIDs` gains `"gobble"`; `TestSetProviderPrewarmFileGobble`.
3. A commented `providers.gobble` block in `configs/config.example.yaml`, `configs/config.mesh-grok.yaml`,
   `configs/config.prod.example.yaml` and, by the P4 deviation of 2026-11-10, `internal/cli/service/defaults_mcremote.yaml`.
4. `docs/guides/config.md`: gobble's rows in "Defaults", its section (credentials are gobble's, `bypass` needs gobble's
   own `permissions.allowBypass`, `0.4.0` is the supported minimum and an older gobble shows no compaction progress),
   and its `MCREMOTE_PROVIDERS_GOBBLE_*` rows in "Environment variables".
5. gobble in `providerTables` (`internal/command/resolve_order_test.go`), and gobble's case in
   `TestProviderAuthInterfaceConformance` (`internal/provider/auth_conformance_test.go`): status, writer, catalog,
   switcher (the type side) and device auth true, `wantSwitchWired` false, as the note of 2026-11-10 and MADR A8 say.
6. `live-gobble` in the `Makefile` with its `.PHONY` entry, and `internal/provider/gobble/live_gobble_test.go`.
7. Step 7's search, run on the final tree:
   `git grep -n 'IDGobble\|"gobble"' -- '*.go' ':!internal/provider/gobble/'` finds the `KnownProviderIDs` line and its
   test, the conformance case, and `provider.go` and `provider_test.go`, whose `IDGobble` is D11's place from P2
   (`f4248820`); no other Go file names it (Confirmation 3).

**Planted breaks, each seen failing in a scratch clone** (C4). Steps 1 to 4, from the script of 2026-11-10, run again
on 2026-11-11 against the tree, `ALL BREAKS CAUGHT`:

```text
gobble not registered                    configfiles_test.go:75: configs/config.example.yaml does not load: providers.gobble is not a provider
prewarm refuses gobble                   prewarm_write_test.go:133: SetProviderPrewarmFile(gobble): unknown provider: gobble
an unknown key in gobble's block         configfiles_test.go:75: configs/config.example.yaml does not load: providers.gobble has no key planted_key
config.md documents a key gobble lacks   configfiles_test.go:107: docs/guides/config.md documents MCREMOTE_PROVIDERS_GOBBLE_PLANTED, which does not load: MCREMOTE_PROVIDERS_GOBBLE_PLANTED names no providers.gobble key
```

Step 5's case, and the two helpers of `wirecap_host_test.go` whose planted-break evidence the P3 deviation of
2026-11-10 required (item 3 of the entry above), each failed on a planted break and passed clean:

```text
conformance, the gobble case wants a wired switcher   auth_conformance_test.go:88: UpstreamSwitchWired: got false, want true (MADR 0244 D5)
conformance, the gobble case expects no catalog       auth_conformance_test.go:79: AuthCataloger: got true, want false (MADR 0074 D16/D19)
wirecap, the host key is not kept                     wirecap_host_test.go:120: after a HOME swap with the key kept: the capture recorded no capture_host, want <tag>…
wirecap, the key path drifts from wirecap's           wirecap_host_test.go:114: MCREMOTE_WIRECAP_HOST_KEY gives tag "" where wirecap's own lookup gives "<tag>…": the key path no longer matches wirecap's
wirecap, captureHostMismatch never reports           wirecap_host_test.go:171: captureHostMismatch = <nil>, want ok false   (three subtests)
```

**Gates (step 8)**, on a scratch clone of `f6e99522` with P4's thirteen files copied in: `make pre-add-check
FILES=…` "7 file(s) clean (gofmt, golint, govulncheck)"; `make test` and `go test -race -count=1 ./...` 50 packages ok
each, no failure; `make lint` and `make staticcheck` clean; `make -n live-gobble` prints the step 6 command;
`make ci-windows` on the Windows test host, "ALL SELECTED CHECKS PASSED". `make vet-tags` first listed eight tags and
no `live_gobble`: the script reads tags from tracked files only (`scripts/vet_build_tags.py`), and in the clone the
two new test files were untracked. With them tracked it lists nine, `live_gobble: ok`, and
`go vet -tags live_gobble ./internal/provider/gobble/` exits 0; in the repository the owner's commit makes them
tracked, so `make vet-tags` covers the tag from then on.

**What P4 does not prove.** `live_gobble_test.go` compiles and is unproven: nothing ran it, the capture-host check of
the P3 deviation included. Its run is `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`
P7's L4 (MADR A9.5, proposed), by the owner's Q19 answer and under C10.

**Gate 3 at this commit** (MADR A10). `go.mod` pins acp-go-sdk2 `v1.35.0` and `acpext v0.3.0`; 0217's P3j has not
landed. gobble `0.4.0`, the supported release, pins acp-go-sdk2 `v1.34.2`. The two sides are on different tags, which
D14's gate 3 forbids; A10 records it as knowingly unmet until gobble `v0.6.0`.

**Staged for the owner's commit:** `Makefile`, the four configs, `docs/guides/config.md`,
`internal/command/resolve_order_test.go`, `internal/config/prewarm_write.go` and its test,
`internal/provider/registered/registered.go`, `internal/provider/auth_conformance_test.go`,
`internal/provider/gobble/live_gobble_test.go`, `internal/provider/gobble/wirecap_host_test.go`, and this plan.

## P7a — 2026-11-12: acpext `v0.4.0`, the two device id tables, their tests

Run by the coordinator after the owner handed this repository over; 0074 P23 closed at `dde47a11`.

On the owner's word of 2026-11-11 ("accept and approve 0179 a9"), who approved 7a, 7b and 7c to run back to back, this
entry records 7a only: steps 0, 1 and 2 of P7, MADR A9.1 and A9.2, with step 5's tests for them. 7b (A9.3) and 7c
(A9.4) are not started. Nothing here spent quota or ran a live suite. Every command wrote a log in the session
scratchpad with its exit status taken before any filter. Go was `go1.27.4` on this Mac and on both test hosts.

**Start state, verified.** Branch `master`, HEAD `dde47a11`, three commits ahead of `origin/master`, working tree
clean, index empty. `go.mod` required `acpext v0.3.0`; `KnownGoodVersion` was `0.4.0`. Per C2 the lines the MADR cites
were read again: `0074-PLAN-remote-provider-auth-from-phone.md` P23 step 5 left `internal/provider/gobble/auth.go`
as the 2026-11-09 commit had it (`git log` of the file ends at `6c8f2623`), so `if p.Login` in `methods` and the
`!p.Login` guard in `startDeviceAuth` were where A9 says.

**The tag, read-only** in gobble-cli: `acpext/v0.4.0` peels to `d6c10a5e15f2`. Two commits since `acpext/v0.3.0`
touch `acpext/`: `d6c10a5` (openai: `{Command: "gobble auth login openai", Login: true}`) and `6cee0af`
(`ErrorData.WindowMinutes`, `omitzero`). `AuthProvider` is `{ID, Name, Command string; Login bool}` at both tags. At
the tag the `Login` providers are exactly openai, grok and kilo.

**Step 0, the records.** MADR A9 was already marked accepted. Dated italic notes (2026-11-12) were added to D3's
catalog and `StartDeviceAuth` bullets, D5's acpext rule, D12 item 8 and D13's recapture rule, and a History line for A9;
this plan's P7 status line and P6 heading were annotated ("moved to
`0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7 (MADR A9.5)"); both records' `date` is
2026-11-12.

**Step 1, the pin,** in a scratch clone of `dde47a11` first: `go get github.com/maccavelli/gobble-cli/acpext@v0.4.0`
("upgraded ... v0.3.0 => v0.4.0"), `go mod tidy` exit 0. `go.mod` changed one line; `go.sum` changed the module's two
lines (the `h1:` of the zip, `9gYm1n6J…` to `99tSH9FL…`; the `/go.mod` line is byte-identical, the module requires
nothing) and no other. No other module moved, `go 1.27.4` stays, there is no `toolchain` line, and no file under
`testdata` changed. `go list -m github.com/maccavelli/gobble-cli/acpext` reports `v0.4.0`.

**Step 2, the tables.** In `internal/provider/gobble/auth.go`, beside `methodAPI`: `deviceSignIn = {"grok", "kilo"}`
and `noPhoneSignIn = {"openai"}`, each commented with MADR A9.2 and its removal at 0246's cut-over. `methods` lists
`oauth_device` for `deviceSignIn` alone. `startDeviceAuth` still refuses an unknown id as before, and refuses any id
not in `deviceSignIn` with `provider.ErrAuthMethodUnsupported` before it builds a command. `p.Login` is no longer read
in non-test code. `noPhoneSignIn` is read by the partition test alone.

**Tests,** in `internal/provider/gobble/auth_test.go`:

* `TestCatalogIsAcpextsProviders` (rewritten): upstreams in acpext's order; `oauth_device` exactly for `deviceSignIn`;
  openai has none; grok's browser row as before.
* `TestDeviceTablesPartitionAcpextsLogins` (new): every acpext `Login` id is in exactly one table; every table id is one
  acpext lists.
* `TestDeviceAuthNeedsALogin` renamed `TestDeviceAuthNeedsADeviceSignIn`, assertions unchanged.
* `TestDeviceAuthRefusesOpenAI` (new): `startDeviceAuth` for `openai:device` returns `ErrAuthMethodUnsupported` and the
  stub records no run.
* The fixture tests (`TestFixtureSendsOnlyKnownMethods`, `TestFixtureShapesAreAcpexts`,
  `TestFixtureCompactionIsTheStandardUpdate`, `TestFixtureIsTheKnownGoodRelease`) and the others of the package pass
  unchanged at `acpext v0.4.0`; no fixture changed.

**Planted breaks, each seen failing on a scratch copy of the scratch clone** (`plants.py`; the clean copy then passed,
`ALL PLANTS CAUGHT`):

```text
catalog, Login read again in methods          auth_test.go:49: openai methods [api_key oauth_device]; a device method only for deviceSignIn [grok kilo]
                                              auth_test.go:52: openai methods [api_key oauth_device]; gobble refuses --method device for it
catalog, openai added to deviceSignIn         auth_test.go:52: openai methods [api_key oauth_device]; gobble refuses --method device for it
partition, kilo dropped from deviceSignIn     auth_test.go:74: kilo is marked Login by acpext and is in 0 of deviceSignIn [grok], noPhoneSignIn [openai]; want exactly one
partition, openai in both tables              auth_test.go:74: openai is marked Login by acpext and is in 2 of deviceSignIn [grok kilo openai], noPhoneSignIn [openai]; want exactly one
partition, a table id acpext does not list    auth_test.go:81: noPhoneSignIn names "planted", which acpext does not list
openai refusal, the old !p.Login guard        auth_test.go:139: err = <nil>, want ErrAuthMethodUnsupported
renamed test, the device guard removed        auth_test.go:126: err = <nil>, want ErrAuthMethodUnsupported
```

The first, third and sixth are the plan's three plants; the rest are the extra branches of the same tests (the
openai line of the catalog test, the "exactly one" and "lists" halves of the partition test, and the renamed test).

**Gates, on the scratch clone** with the four Go-side files:

| Gate | Result |
| :--- | :--- |
| `make pre-add-check FILES="internal/provider/gobble/auth.go internal/provider/gobble/auth_test.go"` | exit 0, "2 file(s) clean (gofmt, golint, govulncheck)"; the same in the tree after the copy |
| `make test` | exit 0; 50 `ok` lines, none FAIL |
| `go test -race -count=1 ./...` | exit 0; 50 `ok` lines, no FAIL, panic or data race |
| `make lint` | exit 0 |
| `make staticcheck` | exit 0; "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name" on each GOOS |
| ignored findings against `HEAD` | per GOOS, `staticcheck -show-ignored -f json ./...` in a clone of `HEAD` and in this clone: 56 each; the sets of (check, file, message) equal after the clone root is removed from the path, with line numbers equal; 16 distinct names on both sides. No name is new |
| `make vet-tags` | exit 0; nine tags, each ok |
| `make vulncheck` | exit 0; "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `make check-libs`, on a scratch commit of the clone, with `GO_SHARED` pointing at the go-shared clone | exit 0; "toolchain: go1.27.4"; acp-go-sdk2 "pinned v1.36.0, checking v1.36.0", go-selfupdate-lib "pinned v1.26.2, checking v1.26.2", go-llmprovider-sdk "pinned v1.29.0, checking v1.30.0", "0 failing checks; scratch removed" |

check-libs names go-llmprovider-sdk `v1.30.0` as the newest tag. This phase does not move it; it is another record's.

**Host runs,** on `dde47a11` plus the four files, carried as a pack of the refreshed scratch clone:

* the Linux test host: `go1.27.4`, `go test -race -count=1 ./...` exit 0, 50 `ok`
  lines, no FAIL, panic or race line;
* the Windows test host: `go1.27.4`, `make ci-windows` exit 0, "ci-windows-local: ALL SELECTED CHECKS PASSED", 50 `ok`
  lines, no FAIL or panic line.

Each host's remote copy was removed by the runner.

**Records.** `make check-records` and `python3 -B scripts/check_records.py --check-all` exit 0; `make
markdownlint-docs` exit 0, "0 issues in 0 files"; the redact preview (`github-disclosure.py redact`, without `--apply`)
exit 0: "would edit 0 file(s), 0 path(s) need a rename by hand".

**Not done here:** 7b and 7c; the commit, which is the coordinator's after the gates; any push. D14's gate 3 stays
unmet by MADR A10. The pin is ahead of every gobble release (A9.1), `KnownGoodVersion` stays `0.4.0`, and the
`0.4.0` fixture is unchanged.

**Staged for the commit:** `go.mod`, `go.sum`, `internal/provider/gobble/auth.go`,
`internal/provider/gobble/auth_test.go`, this plan and `0179-MADR-gobble-native-acp-provider.md`.
