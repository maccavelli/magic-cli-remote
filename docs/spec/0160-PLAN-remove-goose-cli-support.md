---
status: proposed
date: 2026-09-18
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0160 — Remove Goose CLI support from the product, including its ACP-over-HTTP transport

Implements [0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md)
decisions D1–D14, closing findings F1–F23.

**Revision 2026-09-18.** Rewritten against the revised MADR. The 09-17 draft
could not execute as written: P1 edited the service template without
`configs/config.example.yaml`, which fails the parity test (MADR F18); its
leftover-config capture missed two shapes and would itself have become a
required template key (F17, F18); its fail-closed rule would have stopped every
provisioned host (F16); its scope missed 22 files (F22); its P5 wrote a status
PLANs cannot carry and marked the mixed MADR 0073 as superseded (F20); and its
verification used `rg` and `flutter`, neither of which existed on the execution
host at the time (F21; both were installed later on 2026-09-18).

## Goal

The finish line, as observable states:

1. `internal/provider/goose/` and `internal/provider/acphttp/` do not exist, and
   no `.go` file imports either path.
2. `git grep -il goose -- . ':!docs/spec'` prints **exactly** the seven paths in
   A2, in that order.
3. `go build ./...`, `go vet ./...` pass; `go test ./...` and
   `go test -race ./...` report no failure except, on a Windows host whose live
   `%APPDATA%\mcremote\config.yaml` sets `display_name`, the pre-existing
   `TestLoadDisplayNameUnset` (MADR F21). `go mod tidy` changes nothing.
4. `mcremote paths --json --config <file>` exits 0 on a config containing
   `providers.goose` (including a copy of this host's real product-seeded
   config) and on an env with `MCREMOTE_PROVIDERS_GOOSE_*`, and its
   `diagnostics` array contains code `retired_provider_goose`; with neither it
   contains no such code.
5. `make -n live-goose` has no rule.
6. On a Flutter host: `dart format --set-exit-if-changed`, `flutter analyze`,
   and `flutter test` pass in `apps/mobile`; `goose.svg` is gone; the phone has
   `unknown_provider` copy.
7. MADRs 0110 and 0122 carry `status: superseded by 0160-MADR-remove-goose-cli-support.md`;
   MADRs 0025, 0026, 0030 and PLANs 0025, 0030, 0110, 0122 carry the D9 banner
   and no other change; 0073 is byte-identical.
8. `make ci-windows` passes on this host under the same baseline rule as 3.

## Scope

### In scope (the only files any phase may touch)

**P1 — Go removal, leftover-config warning, templates and examples (one commit):**

Delete (whole trees / files):

* `internal/provider/goose/` — 17 files
* `internal/provider/acphttp/` — 20 files
* `internal/daemon/goose_keyring.go`
* `internal/daemon/goose_keyring_test.go`
* `internal/provider/credstore/goose_keyring_test.go`
* `internal/provider/credstore/goose_keyring_parity_test.go`

Create:

* `internal/config/retired_goose.go`
* `internal/config/retired_goose_test.go`

Modify:

* `internal/daemon/daemon.go`
* `internal/provider/provider.go`
* `internal/provider/provider_test.go`
* `internal/provider/auth_conformance_test.go`
* `internal/config/config.go`
* `internal/config/load.go`
* `internal/config/prewarm_write.go`
* `internal/config/config_test.go`
* `internal/config/acp_config_test.go`
* `internal/config/secret_keys_test.go`
* `internal/provider/credstore/credstore.go`
* `internal/provider/credstore/credstore_test.go`
* `internal/provider/credstore/write.go`
* `internal/provider/credstore/write_test.go`
* `internal/command/conformance_test.go`
* `internal/chunkbuf/provider_mode_test.go`
* `internal/ws/server.go`
* `internal/ws/auth_err_code_test.go`
* `internal/cli/doctor.go`
* `internal/cli/engines.go`
* `internal/cli/service/defaults_mcremote.yaml`
* `internal/cli/service/template_parity_test.go`
* `configs/config.example.yaml`
* `configs/config.mesh-grok.yaml`
* `configs/config.prod.example.yaml`

**P2 — every remaining Go comment and fixture (no behaviour change):**

* `internal/agenterr/agenterr.go`
* `internal/agenterr/agenterr_test.go`
* `internal/chunkbuf/chunkbuf.go`
* `internal/chunkbuf/toollane_mode_test.go`
* `internal/command/command_test.go`
* `internal/command/specs.go`
* `internal/daemon/main_test.go`
* `internal/event/event.go`
* `internal/picker/order.go`
* `internal/picker/order_test.go`
* `internal/picker/picker.go`
* `internal/procutil/reap.go`
* `internal/protocol/sessionmode_compat_test.go`
* `internal/provider/auth.go`
* `internal/provider/cwd.go`
* `internal/provider/stderr_tail_test.go`
* `internal/provider/acpagent/acpagent.go`
* `internal/provider/acpagent/automode_test.go`
* `internal/provider/acpagent/session.go`
* `internal/provider/acpagent/sessioncaps.go`
* `internal/provider/acpagent/subagents.go`
* `internal/provider/acpagent/subagents_test.go`
* `internal/provider/acpagent/version.go`
* `internal/provider/acpagent/version_test.go`
* `internal/provider/codex/provider.go`
* `internal/provider/codex/session.go`
* `internal/provider/codex/tool_lane_baseline_test.go`
* `internal/provider/httpagent/currentmodel_test.go`
* `internal/provider/httpagent/supervise_wiring_test.go`
* `internal/provider/kilo/lifecycle.go`
* `internal/provider/kilo/lifecycle_test.go`
* `internal/provider/opencode/lifecycle.go`
* `internal/provider/opencode/upstream.go`
* `internal/session/defaultmode_test.go`
* `internal/session/manager_durable_test.go`
* `internal/session/turnlatency.go`
* `internal/session/turnlatency_test.go`
* `internal/wirecap/wirecap.go`
* `internal/ws/credential_write_test.go`
* `internal/ws/server_test.go`

**P3 — build, living docs, governance:**

* `Makefile`
* `AGENTS.md` — line 132 only
* `README.md`
* `docs/config.md`
* `docs/protocol-v1.md`
* `docs/ops-macos-tcc.md`
* `docs/ops-android-emulator.md`
* `apps/mobile/README.md`

**P4 — mobile (on a Flutter host):**

* `apps/mobile/assets/vendor_icons/goose.svg` — deleted
* `apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart`
* `apps/mobile/lib/features/widgets/vendor_icon.dart`
* `apps/mobile/lib/data/ws/mc_exception.dart`
* `apps/mobile/lib/data/ws/mcremote_client.dart`
* `apps/mobile/lib/data/protocol/models.dart`
* `apps/mobile/lib/data/protocol/picker.dart`
* `apps/mobile/lib/features/chat/chat_screen.dart`
* `apps/mobile/lib/features/settings/upstream_catalog_sheet.dart`
* `apps/mobile/lib/state/transcripts_notifier.dart`
* `apps/mobile/test/friendly_op_error_test.dart`
* `apps/mobile/test/mode_selector_dangerous_test.dart`
* `apps/mobile/test/model_picker_sheet_test.dart`
* `apps/mobile/test/model_picker_test.dart`
* `apps/mobile/test/provider_detail_screen_test.dart`
* `apps/mobile/test/resolve_displayed_mode_test.dart`
* `apps/mobile/test/session_meta_test.dart`
* `apps/mobile/test/session_mode_dangerous_test.dart`
* `apps/mobile/test/sessions_screen_test.dart`
* `apps/mobile/test/upstream_catalog_sheet_test.dart`
* `tools/vendor-icons/ids.txt`
* `tools/vendor-icons/sync.sh` — comment lines 5-8 only

**P5 — historical records (additive marks only):**

* `docs/spec/0110-MADR-goose-keyring-prompts-block-headless-launch.md` — frontmatter `status`, `date`
* `docs/spec/0122-MADR-deterministic-goose-file-log-tail-attach.md` — frontmatter `status`, `date`
* `docs/spec/0025-MADR-goose-provider.md` — banner
* `docs/spec/0026-MADR-mobile-goose-support.md` — banner
* `docs/spec/0030-MADR-goose-remote-parity.md` — banner
* `docs/spec/0025-PLAN-goose-provider.md` — banner
* `docs/spec/0030-PLAN-goose-remote-parity.md` — banner
* `docs/spec/0110-PLAN-goose-keyring-prompts-block-headless-launch.md` — banner
* `docs/spec/0122-PLAN-deterministic-goose-file-log-tail-attach.md` — banner

**Every phase may also append to this file's `## Execution record`.**

### Out of scope

* **MADR 0073** and every other mixed record (0023, 0028, 0029, 0043, 0044,
  0069, 0074, 0083, 0086, 0089, 0095, …). D9/F20.
* **`docs/agent_cli_slash_commands_matrix.md`.** Dated survey.
* **`AGENTS.md:67` and `:148`.** Developer Goose (D11).
* **`internal/provider/codex/testdata/wire/0.152.1/frames.jsonl`.** A wire
  capture; its "goose" is a directory path.
* **Rewriting operator config files or unsetting env vars.** D3 (L3 rejected).
* **Purging durable sessions or `~/.config/goose/`.** D4, D5.
* **Removing `keyring_managed` from the protocol.** D6.
* **Running `tools/vendor-icons/sync.sh`.** D10: network-dependent, rewrites
  unrelated icons.
* **Fixing `TestLoadDisplayNameUnset` / the ten live-config config tests.**
  Pre-existing Windows isolation defect (F21), independent of Goose. It
  deserves its own record; this plan only refuses to add an eleventh.
* **CI workflow edits.** None reference Goose (F13).
* **`git push` and tags.** Not authorised by this plan.

## Stability rule

Baseline, measured 2026-09-18 at `b3d3355` on the Windows host: `go test ./...`
→ 42 packages `ok` (of 48; 5 have no test files), one `FAIL`: `internal/config` `TestLoadDisplayNameUnset`
(F21). "Green" below means **no failure other than that one test, and that one
only on a host where it failed at baseline.** Record the baseline again before
P1 if `HEAD` has moved:

```bash
go test ./... 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' | sort | uniq -c | sort -rn | head
```

Every Go phase (P1, P2) ends with, from the repository root in Git Bash:

```bash
git diff --cached --name-only --diff-filter=AM -z -- '*.go' | xargs -0 -r gofmt -l   # → no output
#   (xargs -r: a bare `gofmt -l` with no files reads stdin and hangs)
go build ./... && go vet ./...                                         # → exit 0
go test ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'                         # → baseline rule
go test -race ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'                   # → baseline rule
make pre-add-check FILES="$(git diff --cached --name-only --diff-filter=AM -- '*.go' | tr '\n' ' ')"
```

P3 and P5 (docs-only) end with `go test ./internal/protocol/ ./internal/cli/service/`
(the doc-coverage and template tests) green, plus their own phase checks.

P4 ends with, on the Flutter host:

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test
```

If P4 is attempted on a host where `flutter --version` fails, stop; do not
commit P4 there and do not claim A7.

**Commits:** stage with `git add` of exactly the phase's in-scope paths
(`git rm -r` for deletions), then `git commit --no-edit` — the global
`prepare-commit-msg` hook writes the message; never `-m`, `--amend`, or an
edited message. Before the first commit, confirm
`git rev-parse --path-format=absolute --git-path hooks` resolves to
`~/.global-git-hooks` or a wrapper that chains to it. One commit per phase.
**`git push` and tags are not authorised by this plan.**

## Cross-cutting contracts

**C1 — Every commit compiles and is green under the baseline rule.** P1 carries
every compile-breaking edit, and the template together with all three example
configs (F18).

**C2 — No remaining-provider test is deleted because it said "goose".**
Retarget the id; keep every assertion. The only deletions allowed are the
tests named in P1 that test Goose-only code (keyring reconcile, Goose config
parse, `with_builtins`, Goose credstore functions, the Goose conformance rows).

**C3 — A leftover Goose config never stops the daemon.** No code path added by
this plan returns an error because `providers.goose` or
`MCREMOTE_PROVIDERS_GOOSE_*` is present. No struct field, `SetDefault`, or
`BindEnv` names `providers.goose`.

**C4 — Nothing reads or writes `~/.config/goose/` after P1**, and nothing
rewrites an operator's mcremote config file.

**C5 — Historical rationale is not edited.** P5 changes exactly the lines
D9 names. `0073` is untouched.

**C6 — Developer Goose stays.** `AGENTS.md:67` and `:148` are not edited.

**C7 — `keyring_managed` stays registered and documented:**
`protocol.ErrKeyringManaged`, `protocol.AuthReasonKeyringManaged`, its
`ErrorCodes()` entry, and its `docs/protocol-v1.md` entry
(`TestErrorCodesAreDocumented` enforces the last).

**C8 — No new test reads the host's live config.** Every test added or edited
by this plan that calls `config.Load` passes an explicit
`LoadOptions{ConfigFile: …}` under `t.TempDir()` and sets any
`MCREMOTE_PROVIDERS_GOOSE_*` it depends on with `t.Setenv` (F21). Tests that
inspect diagnostics find `retired_provider_goose` **by code**, never by
`len(cfg.Diagnostics)` — on Windows, temp-dir configs also draw
`config_not_owner_only` (measured: `mcremote paths --json` on a scratchpad
config).

**C3 is the contract most likely to be broken under pressure** — not by
omission this time but by reflex: the house precedent (MADR 0019) is to refuse,
and a reviewer who remembers 0019 but not F16 will ask for it. The answer is
F16: this host's own config would stop the daemon. **C8 is second**: the
quickest way to write the new tests is to copy an existing
`Load(LoadOptions{})` test, which is exactly the defective pattern.

## Dependency and delivery order

```text
P1 ──► P2 ──► P3 ──► P5          (Windows host, this repository)
  └──────────► P4                (Flutter host; any time after P1)
```

P2 needs P1 (it rewrites comments that name deleted packages). P3 needs P1 so
the docs describe the code. P5 is last so the superseding record describes
executed work. P4 depends only on P1 and runs on a Flutter host; the whole-plan
verification waits for it.

**Flutter host** means any host on Flutter 3.47.2, the CI `FLUTTER_VERSION`
(`.github/workflows/ci.yml:27`), because the `pubspec.lock` gate and analyzer
results depend on the exact version. As of 2026-09-18 that is this Windows
host (natively and in WSL `Ubuntu-24.04`), wonder, or the Mac. Confirm with
`flutter --version` before P4; a host on another version is not a Flutter host
for this plan. On this host P4 needs no pull. On another host, pull P1 first. Do not start P2 until P1's A4–A6 pass.

## Implementation Steps

### P1 — Delete Goose and `acphttp`; warn on leftovers; templates and examples (D1, D2, D3, D4, D6, D8, D12; closes F1, F2, F3, F4, F5, F13, F14, F15, F16, F17, F18, F19)

1. **Delete** the six paths listed under P1 "Delete" (`git rm -r`).

2. **`internal/daemon/daemon.go`.** Remove the `acphttp` and `goose` imports
   (`:27`, `:30`); the whole `if cfg.Providers.Goose.Enabled { … }` block
   (`:247-262`, including the `reconcileGooseKeyring` call); the
   `cfg.Providers.Goose.Enabled ||` term in `anyEnabled` (`:384`); the Goose
   arm of `prewarmPlan` (`:764-766`). Delete `:721-755`: that range is
   `acpAgentConfig`'s doc comment (`:721-723`, stranded above the wrong
   function) followed by `acpHTTPConfig`'s comment and body. Re-add the
   `acpAgentConfig` comment directly above `func acpAgentConfig` (today
   `:788`, which has none), reworded "Every ACP CLI agent (grok) is
   constructed through this one converter …". Deleting only `:724-755` would
   leave that comment as `prewarmPlan`'s godoc. Reword the reaper comment at
   `:173-175` to "opencode, kilo, and codex".

3. **`internal/provider/provider.go`.** Delete `IDGoose` and its comment
   (`:69-70`). **`provider_test.go`:** delete its row.
   **`auth_conformance_test.go`:** delete the Goose case (`:47-52`) and the
   `goose` import.

4. **`internal/config/config.go`.** Delete the `Goose` field of
   `ProvidersConfig` (`:451`), `GooseProviderConfig` (`:544-570`), the Goose
   default block (`:834-850` including its comment), and every Goose check in
   validation (`:1153-1180`). Edit comments: `:442`
   `providers.{opencode,goose,codex,grok}` → `providers.{opencode,codex,grok}`;
   `:473` → "grok"; `:821` and `:876` drop "goose"; `:1086` example list drops
   "goose".
   **`load.go`:** delete the Goose cwd resolve (`:232-233`) and the eleven
   `providers.goose.*` `SetDefault` lines (`:323-333`). Add **no** default,
   bind, or field for `providers.goose`.
   **`prewarm_write.go`:** `KnownProviderIDs` → `{"grok", "opencode", "codex", "kilo"}`;
   delete both `case "goose":` arms (`:47-48`, `:66-67`); `ErrUnknownProvider`'s
   comment "one of the five agent providers" → "one of the agent providers in
   KnownProviderIDs".

5. **`internal/config/retired_goose.go` (new).** One unexported function,
   called from `Load` immediately after `cfg.ConfigFile = usedConfigFile`
   (`load.go:129`), as `noteRetiredGoose(v, os.Environ(), &cfg)`; it reads the
   file path from `cfg.ConfigFile`:

   ```go
   // retiredGooseCode is the Diagnostic code for a providers.goose setting left
   // over from before MADR 0160 removed the Goose provider.
   const retiredGooseCode = "retired_provider_goose"

   // noteRetiredGoose reports, without failing the load, a providers.goose
   // block or MCREMOTE_PROVIDERS_GOOSE_* variable (MADR 0160 D3). Refusing
   // would stop every host setup-service provisioned before 0160, because the
   // seed config carried the block (F16).
   func noteRetiredGoose(v *viper.Viper, environ []string, cfg *Config)
   ```

   Behaviour, exactly:
   * `sources` = `"providers.goose in "+cfg.ConfigFile` if
     `v.InConfig("providers.goose")`, plus every `environ` entry name (the text
     before the first `=`) that has prefix `MCREMOTE_PROVIDERS_GOOSE_`, sorted.
   * If `sources` is empty, return.
   * Otherwise build one message: `"<sources joined by ", "> ignored: the Goose
     provider was removed (MADR 0160); delete the setting"`; call
     `slog.Default().Warn("retired goose settings ignored", slog.String("sources", …))`;
     append `appdirs.Diagnostic{Code: retiredGooseCode, Message: msg}`.
   * Never return an error. `Load` passes `os.Environ()`.

6. **`internal/config/retired_goose_test.go` (new).** Every case writes its
   config under `t.TempDir()` and calls
   `config.Load(config.LoadOptions{ConfigFile: path})` (C8), and asserts by
   code via a helper `hasDiag(cfg, "retired_provider_goose")`:

   | Test | Input | Expect |
   | --- | --- | --- |
   | `TestRetiredGoosePopulatedBlockWarns` | `providers:\n  goose:\n    enabled: true\n    bin: goose\n` | load ok; diag present; message contains `0160` |
   | `TestRetiredGooseDisabledBlockWarns` | `providers:\n  goose:\n    enabled: false\n` | load ok; diag present |
   | `TestRetiredGooseEmptyMapWarns` | `providers:\n  goose: {}\n` | load ok; diag present |
   | `TestRetiredGooseNullKeyIsSilent` | `providers:\n  goose:\n` | load ok; diag absent (F17: inert, undetectable) |
   | `TestRetiredGooseEnvWarns` | no Goose in file; `t.Setenv("MCREMOTE_PROVIDERS_GOOSE_ENABLED","true")` | load ok; diag present; message names the variable |
   | `TestRetiredGooseAbsentIsSilent` | `providers:\n  grok:\n    enabled: true\n` | load ok; diag absent |
   | `TestRetiredGooseSeededTemplateLoads` | the **pre-P1** `defaults_mcremote.yaml` `providers.goose` block, pasted as a literal | load ok; diag present; `cfg.Providers.Grok.Enabled` still true |

   The last case is the regression test for F16; paste the block rather than
   read a file, because P1 deletes it from the template.

7. **Config tests.** `acp_config_test.go`: delete
   `TestGooseWithBuiltinsParseAndValidate` and
   `TestGooseWithBuiltinsRejectsEmptyAndDuplicate` (`:100-129`); change the
   comment at `:23` to "reused by any provider that embeds
   ACPProviderConfig". `config_test.go`: delete the `{"goose", …}` prewarm row
   (`:625`) and the four keyring tests (`:1106-1170`,
   `TestDefaultsGooseKeyringDisabled`, `TestGooseKeyringDisabledExplicitFalse`,
   `TestGooseKeyringDisabledAbsentKeepsDefault`,
   `TestGooseKeyringDisabledEnvOverride`). `secret_keys_test.go:68`: delete
   the `keyring_disabled` exemption (no field carries that tag after P1).

8. **`credstore`.** In `credstore.go` delete `GooseConfigPath`,
   `GooseSecretsPath`, `GooseKeyringDisabled`, `gooseKeyringDisabledValue`,
   `isFalsey`, `GooseConfig`, `ReadGooseConfig`, `splitYAMLScalar` and their
   comments. In `write.go` delete `SetGooseActiveProvider`,
   `ErrGooseKeyringManaged`, `ReadGooseSecretNames`, `readGooseSecrets`,
   `writeGooseSecrets`, `SetGooseSecret`, `DeleteGooseSecret`,
   `GooseKeyringMarker`, `ErrGooseKeyringOperatorOwned`, `gooseKeyringLine`,
   `SetGooseKeyringDisabled`, and the `yaml "go.yaml.in/yaml/v3"` import. Then
   `go build ./internal/provider/credstore/` and `go vet` it; if the compiler
   reports another now-unused import, remove it; if `go vet` or `gopls` reports
   another now-unused unexported function, stop and record it rather than
   widening the deletion silently. In `credstore_test.go` delete
   `TestReadGooseConfig`, `TestReadGooseConfigActiveProviderAlwaysListed`,
   `TestReadGooseConfigMissingFileIsNotAnError` (`:79-157`); in `write_test.go`
   delete `TestSetGooseActiveProviderIsSurgical` and
   `TestSetGooseActiveProviderAddsKeyWhenAbsent` (`:207-256`).

9. **WS.** `server.go`: delete the `case errors.Is(err, credstore.ErrGooseKeyringManaged):`
   arm and its return (`:2699-2700`); reword the comment at `:2274` "(goose's
   keyring)" → "(for example, a keyring the host must manage)".
   `auth_err_code_test.go`: delete the two keyring rows (`:27-28`). C7 stays
   enforced by `protocol` `TestErrorCodesAreDocumented` and the constants.

10. **CLI.** `doctor.go`: delete the Goose probe (`:79-88`). `engines.go:18`:
    the `Long` text lists "opencode and kilo `serve` engines, codex's
    `app-server`".

11. **Templates and examples (F18 — same commit).** Delete the whole
    `goose:` block from `internal/cli/service/defaults_mcremote.yaml`
    (`:75-88`), `configs/config.example.yaml`, `configs/config.mesh-grok.yaml`,
    `configs/config.prod.example.yaml`, plus any comment in those four files
    that names Goose as a current agent. Grok `mcp_servers` stays. In
    `template_parity_test.go` replace `omittedConfigKeys` with just
    `"providers.opencode.transport": {}`. Verify:
    `git grep -il goose -- configs internal/cli/service` → no output.

12. **Conformance and chunkbuf.** `command/conformance_test.go`: remove the
    `goose` import (`:14`) and the Goose `Tabler` (`:55`) from both provider
    lists. `chunkbuf/provider_mode_test.go`: delete the
    `goose (acphttp)` case (`:41-44`).

**Verification (P1):**

```bash
test ! -e internal/provider/goose && test ! -e internal/provider/acphttp && echo A1-OK
git grep -n -e 'internal/provider/goose"' -e 'internal/provider/acphttp"' -- '*.go'   # → none
git grep -n -E 'IDGoose|GooseProviderConfig|Providers\.Goose|credstore\.(Goose|ReadGoose|SetGoose|DeleteGoose|ErrGoose)|reconcileGooseKeyring' -- '*.go'   # → none
git grep -n 'providers.goose' -- '*.go' ':!internal/config/retired_goose*.go'          # → none
go test ./internal/config/ -run 'RetiredGoose' -v 2>&1 | grep -E '^(--- |ok|FAIL)'   # → 7 PASS
go test ./internal/cli/service/ -run 'Template' -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'  # → ok
go mod tidy && git diff --exit-code go.mod go.sum                                    # → exit 0
# plus the Stability rule, plus the binary drive in A4 (MADR Confirmation §4)
```

### P2 — De-goose every remaining Go comment and fixture (D7, D8, D12, D14; closes F6, F7, F22)

Rule for this phase (D14): after it, `git grep -il goose -- '*.go'` prints
exactly `internal/config/retired_goose.go` and
`internal/config/retired_goose_test.go`. Provenance is cited by MADR number.
No assertion changes; no test is deleted. Edits, by file:

* **`agenterr.go`** `:8,120,121,315,323,328,348,352,371,381,413,774`: say
  "structured engine logs", "Rust Debug payloads", "provider retry sleep",
  and cite "MADR 0073" where the shape's origin matters.
  **`agenterr_test.go`:** rename `TestExtractTextGooseJSON` →
  `TestExtractTextStructuredJSON`; `:234`, `:266`, `:308` comments/messages
  say "structured log line" / "Rust Debug shape (MADR 0073)". Fixture strings
  unchanged.
* **`chunkbuf.go:111`**, **`toollane_mode_test.go:32`**: "OpenCode and Kilo".
* **`command_test.go:87-126`**: rename `rGoose` → `rNone` and `gooseTbl` →
  `noneTbl` (they test `KindNone` precedence); failure messages say "KindNone
  loop/review/fork must stay unavailable". **`specs.go:57`**: "opencode never
  claims it".
* **`daemon/main_test.go:17`**: drop `~/.config/goose/config.yaml` from the
  list of paths the isolation protects.
* **`event.go:463-464`**: "a provider may ship a dangerous mode as its default
  (MADR 0069 D3)".
* **`picker/order.go:50`**, **`picker.go:54`**, **`order_test.go`**:
  "a provider that reports no dates (grok, codex)".
* **`procutil/reap.go:11`**: "(opencode, kilo, codex — …".
* **`sessionmode_compat_test.go:55`**, **`session/defaultmode_test.go:48-50`**:
  a pre-0069 daemon's unflagged `auto` list keeps its values; subtest
  `goose_unflagged_auto_is_still_eligible` → `unflagged_default_auto_is_still_eligible`;
  comments say "a provider whose default is an unflagged auto".
* **`picker/order_test.go:49`**, **`session/turnlatency_test.go:41,344,375`**:
  drop Goose from the provider lists; `:375` "a context-total-only shape".
* **`provider/auth.go:88`**: "e.g. a method whose keys live in a host keyring".
* **`provider/cwd.go:46`**: "(codex previously fell …" — drop `acphttp`.
* **`stderr_tail_test.go`**: log label `"goose"` → `"agent"`.
* **`acpagent/acpagent.go:124`**, **`automode_test.go:142`**,
  **`session.go:35,153,1554,2023`**, **`sessioncaps.go:18,24-25`**,
  **`subagents.go:83`**, **`subagents_test.go:232`**, **`version.go:14`**:
  drop Goose/acphttp; keep MADR citations (e.g. "shared quota path — MADR 0073
  F1"); `sessioncaps.go:24-25` becomes "This package is grok only."
* **`acpagent/version_test.go:13,26,28`**: case name `"agent: standard
  agentInfo"`, `Name: "agent"`; comment drops Goose. `Version` unchanged.
* **`codex/provider.go:571`**, **`codex/session.go:2317`**: "(MADR 0073 F1)"
  without "goose/acphttp". **`tool_lane_baseline_test.go:17`**: quote reduced
  to the Codex half, or cite "MADR 0073 M-2" without the agent name.
* **`httpagent/currentmodel_test.go:58`**: "covers grok and codex".
  **`supervise_wiring_test.go:64`**: "Bounded long enough that …" (drop the
  `acphttp` reference).
* **`kilo/lifecycle.go:122`**, **`kilo/lifecycle_test.go:61`**,
  **`opencode/lifecycle.go:122`**: "(parity with codex/grok — MADR 0073)".
  **`opencode/upstream.go:22-23`**: "the same weekly-quota product MADR 0073
  records wedging an agent".
* **`session/manager_durable_test.go`**: `sess-goose` → `sess-second`,
  `goose-chat` → `second-chat`; still Fake.
* **`session/turnlatency.go:109`**, **`turnlatency_test.go`**: "a provider
  that reports no cache counters".
* **`wirecap.go:8`**: list grok stdio, opencode/kilo SSE, codex JSON-RPC; no
  websocket-ACP sentence.
* **`ws/credential_write_test.go:115,122,306,309,314`**: provider id
  `"goose"` → `"opencode"` (no WS handler branches on it; MADR measurement);
  comment `:306` "the phone moves an agent off a quota-blocked upstream".
* **`ws/server_test.go:970,1003`**: `"goose-1"` → `"native-1"`.

**Verification (P2):**

```bash
git grep -il goose -- '*.go'
#   → exactly: internal/config/retired_goose.go
#              internal/config/retired_goose_test.go
git diff --stat HEAD~1 -- '*.go' | tail -1        # only the P2 files changed
# plus the Stability rule; test count per package must equal P1's
go test ./... -json 2>/dev/null | grep -c '"Action":"pass","Package":[^,]*,"Test"'
#   → equal to the same count taken after P1 (C2: nothing deleted, only renamed)
```

### P3 — Makefile, AGENTS live tag, living docs (D1, D3, D9, D11, D14; closes F1, F10)

* **`Makefile`:** drop `live-goose` from `.PHONY` (`:159`) and delete
  `:351-355` (comment + target).
* **`AGENTS.md:132`:** delete `` `-tags live_goose ./...` `` and fix the list
  punctuation. Lines 67 and 148 untouched (C6).
* **`README.md`:** remove Goose from the product-surface line (`:181`), the
  diagram (`:205`, `:214`), PATH binaries (`:235`), engines row (`:578`),
  provider table (`:752`), mesh-config row (`:787`), config table (`:808`),
  `stream_coalesce_ms` (`:818`) and `mcp_servers` (`:853`) sentences, the whole
  `## Provider: Goose` section (`:966-991`), `:1145`, `:1152`, `:1385`, the
  live-test line (`:1492`), the tree comment (`:1512`). Replace the design-table
  row at `:1564` with one row: `| [docs/spec/0160-MADR-remove-goose-cli-support.md](docs/spec/0160-MADR-remove-goose-cli-support.md) | Goose provider removed (supersedes 0025) |`.
  That row is README's only remaining Goose hit.
* **`docs/config.md`:** delete the `providers.goose.*` key table and the
  `MCREMOTE_PROVIDERS_GOOSE_*` env rows; add one row or sentence, the file's
  only remaining Goose hit: "`providers.goose` / `MCREMOTE_PROVIDERS_GOOSE_*` —
  retired (MADR 0160). Ignored; the daemon logs a warning and `mcremote paths`
  reports `retired_provider_goose`. Delete the setting."
* **`docs/protocol-v1.md`:** provider enum without `goose`; Goose examples
  rewritten with `"provider":"grok"`; `:1297` "only codex did"; the
  `keyring_managed` entry stays (C7) with text that names no agent.
* **`docs/ops-macos-tcc.md`**, **`docs/ops-android-emulator.md`**,
  **`apps/mobile/README.md`:** remove the one Goose mention each.

**Verification (P3):**

```bash
git grep -il goose -- README.md docs/config.md docs/protocol-v1.md docs/ops-*.md apps/mobile/README.md Makefile AGENTS.md
#   → exactly: AGENTS.md  README.md  docs/config.md
git grep -n -i goose -- AGENTS.md | cut -d: -f2          # → 67 and 148
git grep -c -i goose -- README.md docs/config.md          # → README.md:1  docs/config.md:1 (each a single line)
make -n live-goose 2>&1 | grep -c 'No rule'              # → 1
go test ./internal/protocol/                             # → ok (keyring_managed still documented)
```

### P4 — Mobile icon, copy, comments, fixtures (D5, D6, D8, D10, D13; closes F5, F8, F9, F12, F23)

Runs on a Flutter host (defined under "Dependency and delivery order") after P1.

* **Icon (D10, by hand):** `git rm apps/mobile/assets/vendor_icons/goose.svg`;
  delete `vendor_icon_manifest.g.dart:46`; delete `tools/vendor-icons/ids.txt:83`;
  rewrite `sync.sh:5-8` to "the union of a live opencode/kilo catalog dump and
  the agent ids". Do not run `sync.sh`.
* **Copy (D6, D13) in `mc_exception.dart`:** `keyring_managed` →
  `'This agent keeps its keys in the host\'s OS keyring — add the key on the host.'`
  (still contains `keyring` and `host`). Add
  `case 'unknown_provider': return 'The agent for this session is no longer available on the host.';`
  **`friendly_op_error_test.dart`:** add a test that `unknown_provider` maps to
  a string containing `no longer available`.
* **Comments:** `vendor_icon.dart:6`, `mcremote_client.dart:3761`,
  `models.dart:2308-2309`, `picker.dart:134`, `chat_screen.dart:1199`,
  `upstream_catalog_sheet.dart:150`, `transcripts_notifier.dart:107` (cite the
  daemon behaviour or a MADR number; no Goose, no `acphttp`).
* **Tests (D8; C2):** retarget ids to `grok`, `opencode`, `kilo`, or a
  synthetic id, keeping every `expect`:
  `mode_selector_dangerous_test.dart` (17 hits: `_gooseModes` →
  `_legacyUnflaggedModes`, `_gooseModes0069` → `_flaggedModes`; test names say
  "a legacy unflagged auto" / "a flagged dangerous auto"),
  `session_mode_dangerous_test.dart`, `resolve_displayed_mode_test.dart`,
  `model_picker_test.dart`, `model_picker_sheet_test.dart`,
  `provider_detail_screen_test.dart`, `session_meta_test.dart`,
  `sessions_screen_test.dart`, `upstream_catalog_sheet_test.dart`.

**Verification (P4):**

```bash
test ! -e apps/mobile/assets/vendor_icons/goose.svg && echo OK
git grep -il goose -- apps/mobile tools/vendor-icons       # → no output
cd apps/mobile
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test                                             # test count ≥ pre-P4 count + 1
```

### P5 — Mark Goose-only historical records (D9; closes F11, F20)

* **YAML MADRs (0110, 0122):** in the frontmatter only, set
  `status: superseded by 0160-MADR-remove-goose-cli-support.md` and
  `date:` to the execution date.
* **Banner files (MADRs 0025, 0026, 0030; PLANs 0025, 0030, 0110, 0122):**
  insert, as the first line after the H1 and one blank line, exactly:

  ```markdown
  > **Superseded by [MADR 0160](0160-MADR-remove-goose-cli-support.md) (YYYY-MM-DD):** the Goose provider was removed from the product. This record is kept as history.
  ```

  with the execution date. Change nothing else — not the existing status line,
  not headings, not body text.
* **0073:** no change (F20).

**Verification (P5):**

```bash
git grep -n '^status:' -- docs/spec/0110-MADR-*.md docs/spec/0122-MADR-*.md
#   → both: status: superseded by 0160-MADR-remove-goose-cli-support.md
git grep -l 'Superseded by \[MADR 0160\]' -- docs/spec ':!docs/spec/0160-*' | wc -l   # → 7
#   (the exclusion matters: this PLAN quotes the banner text itself)
git diff --numstat HEAD~1 -- docs/spec ':!docs/spec/0160-*' | awk '{print $1, $2, $3}'
#   → 9 files; banner files "2 0" (banner + blank); YAML files "2 2"
git diff --quiet HEAD~1 -- docs/spec/0073-MADR-goose-prompt-hang-and-debug-pass.md && echo 0073-untouched
```

## Verification (whole plan)

Run after all five phases, from the repository root on the Windows host (the
P4 lines on the Flutter host):

```bash
# A1
test ! -e internal/provider/goose && test ! -e internal/provider/acphttp && echo OK
# A2
git grep -il goose -- . ':!docs/spec'
# A3
go build ./... && go vet ./...
go test ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'
go test -race ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'
go mod tidy && git diff --exit-code go.mod go.sum
# A4–A6: MADR Confirmation §4 (binary drive)
# A7: P4 verification block
# A8–A9: P5 verification block
# A10
make ci-windows
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `internal/provider/goose/` and `internal/provider/acphttp/` do not exist; no `.go` imports them | D1, D2 (Confirmation §1) |
| A2 | `git grep -il goose -- . ':!docs/spec'` prints exactly: `AGENTS.md`, `README.md`, `docs/agent_cli_slash_commands_matrix.md`, `docs/config.md`, `internal/config/retired_goose.go`, `internal/config/retired_goose_test.go`, `internal/provider/codex/testdata/wire/0.152.1/frames.jsonl` | D14 (§2) |
| A3 | build, vet, `go test`, `go test -race` green under the baseline rule; `go mod tidy` no diff | D1, D8 (§3) |
| A4 | `mcremote paths --json` on a leftover-block config and on a copy of this host's live config exits 0 with `retired_provider_goose` | D3 (§4) |
| A5 | `MCREMOTE_PROVIDERS_GOOSE_ENABLED=true` → exit 0, diagnostic names the variable | D3 (§4) |
| A6 | A config with no Goose key and no Goose env → no `retired_provider_goose` | D3 (§4) |
| A7 | On a Flutter host: format/analyze/test pass; no `goose.svg`; `keyring_managed` copy has no agent name; `unknown_provider` has copy and a test | D6, D10, D13 (§7) |
| A8 | 0110/0122 MADRs superseded by 0160; seven banner files carry the banner and nothing else changed | D9 (§8) |
| A9 | 0073 unchanged | D9 (§8) |
| A10 | `make ci-windows` passes under the baseline rule | (§9) |
| A11 | `protocol.ErrorCodes()` and `docs/protocol-v1.md` still contain `keyring_managed` | D6 (§6) |
| A12 | `AGENTS.md:67` and `:148` still name Goose as a coding agent | D11 (§2) |
| A13 | `agenterr` still classifies the former Goose fixture strings (tests renamed, not removed) | D7 |
| A14 | P2 leaves the Go test count unchanged from P1 | D8 (C2) |

**A4 is the criterion most likely to be quietly dropped**: it needs a built
binary and a copy of a real config rather than a unit test, and the unit tests
in P1 step 6 look like they cover it. They cover the loader; A4 covers the
product-seeded file that motivated D3 (F16), driven through the real command.

## Rollout and Rollback

**What a user observes after upgrading the daemon.** Goose disappears from the
provider picker. Existing Goose sessions stay in the list; opening one shows
"The agent for this session is no longer available on the host" (after P4
reaches the phone; before that, the raw "unknown provider"). A host whose
config still has a `goose:` block — every `setup-service`-provisioned host —
starts normally and logs `retired goose settings ignored` once per start;
`mcremote paths --json` lists `retired_provider_goose`. `mcremote update` is
unaffected by the block. `~/.config/goose/` is untouched.

**Operator action.** Delete the `providers.goose` block (and any
`MCREMOTE_PROVIDERS_GOOSE_*` variable). Nothing else; there is nothing to
migrate.

**Per-phase revert.** Each phase is one commit; `git revert` restores that
slice. Revert in reverse order (P5 → P1). Reverting P1 alone restores Goose
while P3's docs say it is gone.

## Deferred (named, so they are not mistaken for oversights)

* **Removing `keyring_managed` from the protocol.** D6 keeps it; a later record
  can drop the producer-less string once mixed-version phones are irrelevant.
* **Stripping `providers.goose` from operator configs automatically (L3).**
  Rejected for now; if the warning proves too noisy, a later record can add an
  explicit `mcremote config prune`-style command rather than a silent rewrite.
* **Hiding or purging durable Goose sessions.** D5; a product question.
* **Fixing the Windows config-test isolation defect** (`TestLoadDisplayNameUnset`
  and the ten `Load(LoadOptions{})` tests, F21). Independent of Goose; needs
  its own record, likely an `appdirs` roots override for tests.
* **Rewriting mixed historical MADRs**, including 0073. History (D9).
* **Rebuilding `acphttp` as a generic transport.** Only if a future agent
  speaks ACP over HTTP; start from MADR 0025 in git.
* **Touching developer-Goose hook registration** (`~/.global-agent-hooks`,
  `AGENTS.md:67`). Different Goose (D11).

## Execution record

Not yet executed.
