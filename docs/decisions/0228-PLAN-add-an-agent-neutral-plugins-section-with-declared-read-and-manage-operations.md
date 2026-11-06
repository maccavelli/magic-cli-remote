---
status: proposed
date: 2026-11-03
associated-madr: "0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement an agent-neutral plugins section with declared read and manage operations

Associated MADR: [0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md](0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md)

## Goal

Every session whose engine has plugins can show the owner its plugins on the phone: their state,
why a failed one failed, and what each provides. Where the engine offers it and the operator
allows it, the owner can act on them. Nothing sensitive crosses (MADR D4), and nothing is shown
that the session did not declare (MADR D2).

## Scope

* **In:** MADR D1 to D7, for opencode v2, grok and codex as D3 lists:
  * the provider model and capabilities;
  * acpagent's hooks;
  * the protocol, the daemon and the config key;
  * the phone section;
  * each engine's decoder and actions;
  * the tests, probes and documentation.
* **Out:**
  * opencode v2 enable, disable, install and uninstall: config edits, which Magic does not make
    (MADR D3);
  * grok's CLI mutations;
  * opencode 1.x and kilo, which have no plugin route;
  * gobble, which has no plugins;
  * marketplace browsing beyond what codex's 0109 D18 already decides;
  * the plugin-setup finding of
    [0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md](0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md)
    D5.
* **Order with other records.**
  * The opencode v2 phases build on the provider 0214 is still building. No phone reaches v2
    before 0214's P7 registers it.
  * 0230 lands before that, so the control plane these phases read starts no MCP copies.
  * The codex phase is 0109 PLAN P13's plugin part, done through this record's model.

## Constraints

* **C1. Each phase is one commit, started on the owner's word.** The agent stages; the owner
  commits with `git commit --no-edit`.
* **C2. Quota and network.** Ask before spending provider quota. No step here sends a prompt.
  These touch the network and run only on the owner's word:
  * a probe or live test that installs a plugin from a registry or marketplace;
  * one that checks a registry for updates.
* **C3. Hosts.** Process, path, signal or pipe changes pass on macOS, the Linux test host and the
  Windows test host. That covers grok's CLI run and the opencode control-plane routes.
* **C4. Every new test is seen failing** on a scratch copy before it is trusted.
* **C5. Pins.** `opencode --version` is `v2.2.3`, `grok --version` `1.0.70` and `codex --version`
  `0.166.0` at each phase's start. A different version is a deviation.
* **C6. Records.** No hostname, account name or real path enters a record or a fixture. Fixtures
  use made-up paths and secrets.
* **C7. Older phones keep working.** Protocol fields are additive, and a phone that does not know
  `plugins` shows nothing new.

## Implementation Steps

### P0 — Before each phase (read only)

1. C5's versions, and `git status` clean apart from untracked drafts.
2. Re-read the files the phase names, and note any drift from this plan as a deviation before
   the first write.

### P1 — The model, the capabilities, and the `plugin.gui` refusal (MADR D1, D2, D4)

1. **`internal/provider/provider.go`:**
   * the model: `PluginInfo`, `PluginInventory`, `PluginKind`, `PluginState`, `PluginScope`,
     `PluginProvides`, `PluginUpdate`;
   * the action types: `PluginActionKind` (`check_updates`, `update`, `enable`, `disable`,
     `install`, `uninstall`), `PluginActionRequest` and `PluginActionResult`;
   * `PluginSession` (`ListPlugins`, `PluginAction`);
   * `CapPlugins`, plus one capability per action (`CapPluginCheckUpdates`, …).

   The model has no field for a path, URL, option, code or credential (MADR D1).
2. **Sanitizers, in `internal/provider`,** so every engine shares them:
   * `SanitizePluginError`: clip to 160; replace the whole text with "failed to load" when it
     matches a path, URL or secret pattern, the patterns codex's `sanitizeDoctor` uses;
   * `PluginName`: a basename with no separator, clipped;
   * the bounds: 64 plugins, then `Truncated`.
3. **`internal/provider/opencode/control/control.go`:** refuse `GET /api/plugin/gui`, and anything
   under it, before anything starts (`ErrPluginGUI`), beside `ErrProviderRoute`.
4. **`internal/provider/acpagent`:**
   * `Ops.ListPlugins`, and `Ops.PluginAction` with `Ops.PluginActions`, the actions the Spec's
     engine has;
   * `Config.PluginActions`, the operator's allowed list;
   * the session implements `PluginSession` (`surfaces.go`) and declares `CapPlugins` from
     `ListPlugins`, and each action only when the Spec offers it and the config allows it
     (`sessioncaps.go`);
   * an undeclared action answers "not offered" without a hook call.
5. **`internal/event/event.go`:** `Capabilities` gains `plugins` and `plugin_actions` (a list of
   action names), both `omitempty`. acpagent fills them in `emitCapabilities`.
6. **httpagent** declares neither, since 1.x and kilo have no route. **fake** implements both, with
   outcomes chosen by the session id, as it does for skill refresh, so the wire tests have a
   provider.
7. **Tests:**
   * the sanitizer and name rules;
   * the bound;
   * the `plugin.gui` refusal beside `TestNeverCallsProviderRoutes`;
   * acpagent's declaration matrix (hook × policy);
   * the conformance suite's "declared matches answered" (0208 D7) for the new capabilities.

### P2 — Protocol, daemon and config (MADR D2, D4, D5)

1. **`internal/protocol/messages.go`:**
   * `session.plugins` and `session.plugins_result` (`SessionPluginsPayload`,
     `SessionPluginsResultPayload{SessionID, Inventory}`);
   * `session.plugin_action` and `session.plugin_action_result` (`SessionPluginActionPayload{
     SessionID, Action, PluginID, IdempotencyKey}`, and a result with the outcome and a new
     inventory).
2. **`internal/protocol/errors.go`:** `session_plugins_failed`, `session_plugin_action_failed`,
   `session_plugin_action_not_offered` and `session_plugin_action_busy`, each added to the error
   list the phone decodes.
3. **`internal/protocol/op_timeouts.json`:** `session.plugins` 30 s, and `session.plugin_action`
   at the longest action's bound. That is `update`, set at P5 from its probe.
   `internal/ws/op_timeout_test.go` keeps the file and the code in step.
4. **`internal/session/manager.go`:** `ListPlugins` and `PluginAction`.
   * Each checks ownership (`Authorize(id, deviceID, true)`) and the declared capability.
   * An action runs one at a time per provider, with a manager-level lock keyed by provider id.
     A second one answers `busy`.
   * Every action logs one audit line: provider, action, plugin id, device id and outcome.
5. **`internal/ws/server.go`:** handlers through `dispatchAsync`. The action goes through
   `idempotencyLedger`, so a resent request returns the first answer.
6. **Config** (`internal/config/config.go`, `load.go`, validation):
   * `providers.<id>.plugins.manage`, a list of action names, empty by default; an unknown name
     fails validation, naming the key and this record;
   * opencode's value reaches `acpagent.Config.PluginActions` through `NewACPWithLogger`;
   * codex and grok read theirs in their own packages.
7. **Documentation:** `docs/guides/protocol-v2.md` (the two message pairs and the error codes) and
   `docs/guides/config.md` (the key, and why it is off by default).
8. **Tests:**
   * a wire round trip on fake for each message;
   * ownership refused for a non-owner;
   * an undeclared action refused without a provider call;
   * the per-provider lock;
   * the idempotent resend;
   * config validation;
   * the op-timeout table.

### P3 — opencode v2: the list (MADR D3, D4)

1. **`internal/provider/opencode/acp_plugins.go`** (new). `ListPlugins` on the control plane in
   the session's directory:
   * it first waits for activation, through `acpSurfaces.activate`;
   * then `GET /api/plugin`;
   * then the decoder: `source` → `Kind`, keeping a package's `target` and `version` but never a
     local `path`, which becomes its basename when there is no id; `features` → `Provides` (`gui`
     is reported, and its bundle is never fetched); `state` → `State` with `SanitizePluginError`;
     `outdated` and `updating` → `Update`.
2. Wired in `newACP` as `spec.Ops.ListPlugins`.
3. **Tests:**
   * fixtures from the P6d probe's shape, with a made-up local path, a failed plugin whose error
     carries a made-up path and a URL, and a package plugin;
   * no path, URL, option or code in the result;
   * the activation order;
   * the session's directory;
   * `plugin.gui` never sent.
4. **Live pin** (`live_opencode`, isolated home, no prompt, no network):
   * a local plugin directory that fails to load, and one that loads;
   * the list shows one `failed` with the fixed text and one `active`;
   * no path appears.

### P4 — The phone (MADR D6)

1. **`apps/mobile/lib/data/protocol/models.dart`:** `PluginInfo` and `PluginInventory`, and
   `SessionCapabilities` gaining `plugins` and `pluginActions`.
2. **`apps/mobile/lib/data/ws/mcremote_client.dart`:** `sessionPlugins` and `pluginAction`, the
   latter with an idempotency key it keeps across a resend.
3. **`apps/mobile/lib/features/chat/diagnostics_sheet.dart`:** a Plugins section built with
   `_section`, failed plugins first, each with its fixed error text. It is shown only when
   `plugins` is declared.
4. **`apps/mobile/lib/features/chat/plugin_detail_sheet.dart`** (new):
   * what the plugin provides, its scope and its update state;
   * one button per declared action, each confirmed with the consequence text the daemon sends;
   * the section re-read after an action.
5. **Tests:**
   * `diagnostics_sheet_test.dart`: the section, its order, its absence when not declared;
   * `plugin_detail_sheet_test.dart`: actions only when declared, the confirmation, the re-read,
     the `busy` and `not_offered` texts;
   * the existing provider-id guard stays green.
6. `dart format`, `flutter analyze` and `flutter test` (`make preflight`).

### P5 — opencode v2: check and update, behind the policy (MADR D3, D5)

1. **Probe first** (owner's word, network: C2), on an isolated home with one package plugin:
   * `POST /api/plugin/check` with and without `target`;
   * `POST /api/plugin/update` `{targets}`: its duration, its 204, and the list's `outdated` and
     `updating` before and after;
   * whether a running engine's list changes without a restart.

   The update's duration sets `session.plugin_action`'s timeout. Anything that contradicts the
   MADR is a deviation.
2. **`acp_plugins.go`:**
   * `check_updates` and `update` as `Ops.PluginActions`, through `Ops.PluginAction`, for package
     plugins only; another kind answers "not offered";
   * after a successful update, `RestartSessionsWhenIdle` with a notice;
   * both through the control plane in the session's directory.
3. **Tests:** each action's request; the restart after an update and none after a check; a local
   plugin refused; the policy gate end to end on fake.
4. **Live pin** (owner's word, network): check and update one package plugin on an isolated home.

### P6 — grok: the probe, then the list (MADR D3)

1. **Probe first** (owner's word, network: C2):
   * an isolated grok home with one marketplace plugin installed;
   * `grok plugin list --json`: its shape, a disabled plugin, and a plugin whose load fails;
   * `grok plugin details` for what `Provides` can say.

   No credentials are involved, and no turn.
2. **`internal/provider/grok/plugins.go`** (new). `ListPlugins` runs `grok plugin list --json`:
   * with the session's binary and environment and a fixed argv;
   * with a 15 s timeout and a 1 MiB output bound;
   * decoded as in P3, through the shared sanitizers;
   * wired as the grok Spec's `Ops.ListPlugins`.
3. **Tests:** fixtures from the probe; the timeout; the bound; a non-JSON answer as an error with
   no output in it.
4. **Live pin** (`live_grok`): the list on an isolated home with the probe's plugin.

### P7 — codex, through 0109 P13 (MADR D3)

1. **At the phase's start,** re-check 0109 D38's maturity gate against codex 0.166.0's official
   documentation and schema. Record the result. Mutation stays off unless the gate passes or the
   owner approves that conflict, as 0109 requires.
2. **`internal/provider/codex`:**
   * the plugin methods' capability entries (`capabilities.go`, none today);
   * `ListPlugins` from `plugin/installed` (and `plugin/list` when it is needed for enablement),
     decoded into D1's model;
   * codex-only detail (marketplace, install and auth policy) in the optional fields;
   * the actions 0109 D18 decides, each declared only when its capability, the gate and
     `providers.codex.plugins.manage` all allow it.
3. **Tests:** fixtures from the 0.166.0 schema; the gate; the declarations; destructive actions
   confirmed.
4. **Live pin** (`live_codex`, no turn): `plugin/installed` on an isolated `CODEX_HOME`.
5. **0109's PLAN** gets a dated note: P13 step 5's screens are replaced by this record's section,
   and the rest of P13 is unchanged.

### P8 — Acceptance and documentation

1. The MADR's Confirmation, item by item, with its evidence.
2. `go test -race -count=1 ./...`; `make preflight`; `make lint`; `make staticcheck`;
   `make vet-tags`; `make check-records`; the hosts (C3).
3. `docs/README.md`'s index regenerated (`check_records.py --write-index`), and an "I want to…" row
   for "see why a plugin failed".
4. This PLAN's status set to `complete` only when every criterion below holds.

## Verification

### Acceptance criteria (mapped to the MADR's Confirmation)

| Criterion | Evidence |
| --- | --- |
| No path, URL, option or code reaches `PluginInfo`, for each engine | P1, P3, P6 and P7 decoder tests, with planted values |
| A failed plugin's error is sanitized | P1 sanitizer tests; P3 live pin |
| `plugin.gui` is refused before anything starts | P1 control-plane test |
| An action is declared only when offered and allowed, and undeclared actions never reach the engine | P1 matrix; P2 wire tests; conformance suite |
| Ownership, confirmation, one action at a time, idempotency, audit | P2 tests; P4 widget tests |
| The phone shows only declared controls, and compares no provider id | P4 tests; the existing guard |
| opencode v2's update restarts sessions when idle | P5 tests |
| Each engine's shape is pinned live | P3, P5, P6 and P7 live pins |
| Every new test was seen failing | each phase's record |

## Rollout and Rollback

* **Rollout.**
  * P1 and P2 add nothing a phone can see until a provider declares `plugins`.
  * P3 and P5 are reachable only once 0214's P7 registers the v2 provider.
  * P6 and P7 reach phones at their commits, behind `plugins` for the list and the operator
    policy for actions.
* **Rollback.**
  * Revert the phase's commit.
  * A phone that knew the section shows nothing when `plugins` is no longer declared.
  * The config key, once removed, fails validation if left set. That is named in P2's
    documentation, so an operator knows to drop it.
