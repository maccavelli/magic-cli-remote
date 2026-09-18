---
status: proposed
date: 2026-09-17
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0160 — Remove Goose CLI support from the product, including its ACP-over-HTTP transport

Implements [0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md)
decisions D1–D12, closing findings F1–F15.

## Goal

Finish line:

* `internal/provider/goose/` and `internal/provider/acphttp/` do not exist;
* `go test ./...` and `go test -race ./...` pass with no Goose provider row
  in command or auth conformance;
* a config that still contains `providers.goose` or
  `MCREMOTE_PROVIDERS_GOOSE_*` refuses to load, naming MADR 0160;
* living product docs, example YAML, `make live-goose`, and the product
  `live_goose` mention in `AGENTS.md` are gone;
* the phone no longer ships a Goose agent icon or `goose configure` copy;
* Goose-topic MADRs listed in P6 are stamped superseded; mixed historical
  records are untouched;
* developer-Goose lines in `AGENTS.md` (hooks, commit-message agents) still
  exist.

## Scope

### In scope (the only files any phase may touch)

P1 (delete + compile-breaking call sites + fail-loud config):

* `internal/provider/goose/` — entire tree, deleted
* `internal/provider/acphttp/` — entire tree, deleted
* `internal/daemon/daemon.go`
* `internal/daemon/goose_keyring.go` — deleted
* `internal/daemon/goose_keyring_test.go` — deleted
* `internal/daemon/main_test.go` — comment only
* `internal/provider/provider.go`
* `internal/provider/provider_test.go`
* `internal/provider/auth.go` — comment
* `internal/provider/auth_conformance_test.go`
* `internal/provider/stderr_tail_test.go` — fixture string
* `internal/config/config.go`
* `internal/config/load.go`
* `internal/config/config_test.go`
* `internal/config/acp_config_test.go`
* `internal/config/prewarm_write.go`
* `internal/config/secret_keys_test.go`
* `internal/provider/credstore/credstore.go`
* `internal/provider/credstore/credstore_test.go`
* `internal/provider/credstore/write.go`
* `internal/provider/credstore/write_test.go`
* `internal/provider/credstore/goose_keyring_test.go` — deleted
* `internal/provider/credstore/goose_keyring_parity_test.go` — deleted
* `internal/command/conformance_test.go`
* `internal/command/command_test.go`
* `internal/command/specs.go` — comment
* `internal/chunkbuf/provider_mode_test.go`
* `internal/ws/server.go`
* `internal/ws/auth_err_code_test.go`
* `internal/ws/credential_write_test.go`
* `internal/cli/doctor.go`
* `internal/cli/engines.go`
* `internal/cli/service/defaults_mcremote.yaml`
* `internal/cli/service/template_parity_test.go`

P2 (living Go comments / remaining fixtures that still compile after P1):

* `internal/picker/order.go`
* `internal/picker/picker.go`
* `internal/picker/order_test.go`
* `internal/event/event.go`
* `internal/session/defaultmode_test.go`
* `internal/session/manager_durable_test.go`
* `internal/session/turnlatency.go`
* `internal/session/turnlatency_test.go`
* `internal/protocol/sessionmode_compat_test.go`
* `internal/agenterr/agenterr.go`
* `internal/agenterr/agenterr_test.go`
* `internal/wirecap/wirecap.go`
* `internal/provider/acpagent/version_test.go`
* `internal/ws/server_test.go`

P3 (build, examples, living docs):

* `Makefile`
* `AGENTS.md` — the `live_goose` sentence only
* `README.md`
* `docs/config.md`
* `docs/protocol-v1.md`
* `docs/ops-macos-tcc.md`
* `docs/ops-android-emulator.md`
* `apps/mobile/README.md`
* `configs/config.example.yaml`
* `configs/config.mesh-grok.yaml`
* `configs/config.prod.example.yaml`

P4 (mobile):

* `apps/mobile/assets/vendor_icons/goose.svg` — deleted
* `apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart`
* `apps/mobile/lib/features/widgets/vendor_icon.dart`
* `apps/mobile/lib/data/ws/mc_exception.dart`
* `apps/mobile/lib/data/ws/mcremote_client.dart`
* `apps/mobile/lib/data/protocol/models.dart`
* `apps/mobile/lib/data/protocol/picker.dart`
* `apps/mobile/lib/features/chat/chat_screen.dart`
* `apps/mobile/lib/features/settings/upstream_catalog_sheet.dart`
* `apps/mobile/test/model_picker_test.dart`
* `apps/mobile/test/model_picker_sheet_test.dart`
* `apps/mobile/test/mode_selector_dangerous_test.dart`
* `apps/mobile/test/provider_detail_screen_test.dart`
* `apps/mobile/test/resolve_displayed_mode_test.dart`
* `apps/mobile/test/sessions_screen_test.dart`
* `apps/mobile/test/session_mode_dangerous_test.dart`
* `apps/mobile/test/session_meta_test.dart`
* `apps/mobile/test/upstream_catalog_sheet_test.dart`
* `apps/mobile/test/auth_method_availability_test.dart`
* `apps/mobile/test/friendly_op_error_test.dart`
* `tools/vendor-icons/ids.txt`
* `tools/vendor-icons/sync.sh`

P5 (historical stamps only):

* `docs/spec/0025-MADR-goose-provider.md`
* `docs/spec/0025-PLAN-goose-provider.md`
* `docs/spec/0026-MADR-mobile-goose-support.md`
* `docs/spec/0030-MADR-goose-remote-parity.md`
* `docs/spec/0030-PLAN-goose-remote-parity.md`
* `docs/spec/0073-MADR-goose-prompt-hang-and-debug-pass.md`
* `docs/spec/0110-MADR-goose-keyring-prompts-block-headless-launch.md`
* `docs/spec/0110-PLAN-goose-keyring-prompts-block-headless-launch.md`
* `docs/spec/0122-MADR-deterministic-goose-file-log-tail-attach.md`
* `docs/spec/0122-PLAN-deterministic-goose-file-log-tail-attach.md`

If P5 finds a `0073-PLAN-*` that the MADR measurement missed, stamp it too
and record that in the execution record; do not invent a PLAN file.

### Out of scope

* **Deleting or rewriting mixed historical MADRs** (0023, 0028, 0029, 0043,
  0044, 0069, 0074, 0083, 0086, 0089, 0095, …). D9. Their Goose sentences are
  evidence of what was true when they were written.
* **`docs/agent_cli_slash_commands_matrix.md`.** Dated 2026-07-25 survey.
* **Developer-Goose lines in `AGENTS.md`** (hooks list, commit-message
  agents). D11.
* **Purging durable sessions or `~/.config/goose/`.** D4, D5.
* **Removing `keyring_managed` from `protocol.ErrorCodes()`.** D6.
* **Deleting `agenterr` classifiers or their wire-sample strings.** D7.
* **Deleting vendor SVGs other than `goose.svg`.** D10.
* **`ACPProviderConfig`, `acpagent`, Grok `mcp_servers`, Fake.** D12.
* **CI workflow edits.** F13: there is no Goose job. No workflow edits
  without Mac permission (AGENTS.md / 0145).
* **`git push` and tags.** Explicit ask required in the same turn.

## Stability rule

Every Go phase ends with:

```bash
go test ./...
go test -race ./...
make pre-add-check FILES="<the Go files that phase staged>"
```

The mobile phase additionally ends with:

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed <touched dart files>
cd apps/mobile && flutter analyze
cd apps/mobile && flutter test
```

If `flutter` / `dart` is not on this host, say so in the execution record
and do not claim A8.

One commit per phase (`git commit --no-edit`; never `-m`). **`git push` and
tags are not authorised by this plan.**

The contract most at risk under time pressure is C3 (fail-loud leftover
config): it is tempting to delete `GooseProviderConfig` and ship, because
the tree compiles either way. That is the 0019 foot-gun. P1 is not done
until the leftover-YAML test exists and fails closed.

## Cross-cutting contracts

**C1 — the tree compiles after every phase.** No "delete the package now,
fix imports later" commit. P1 includes every compile-breaking call site.

**C2 — no remaining-provider test is deleted because it said "goose".**
Retarget the fixture (D8). A deleted mode-danger test is a regression.

**C3 — leftover Goose config refuses to load.** Empty `goose:`, populated
`goose:`, and `MCREMOTE_PROVIDERS_GOOSE_*` are all refused. A config with
no Goose mention still loads. No Viper default on the capture field.

**C4 — do not write `~/.config/goose/`.** Tests that previously isolated
Goose's config dir by setting `HOME`/`XDG_CONFIG_HOME` go away with the
code. Nothing new creates that path.

**C5 — do not rewrite historical rationale.** P5 is a `status:` frontmatter
stamp and nothing else.

**C6 — developer-Goose stays.** A grep that is used as a completion check
must exclude `AGENTS.md` hook/commit sentences, or it will false-fail C6.

**C7 — `keyring_managed` remains registered** in `protocol.ErrorCodes()` and
`docs/protocol-v1.md`. Phone copy may drop the `goose configure` example
(D6) but must still handle the code.

C3 is the contract most likely to be quietly dropped under pressure: see
Stability rule.

## Dependency and delivery order

P1 is the load-bearing phase; P2–P5 are independent of each other once P1
has landed, but P3's protocol-v1 edit should follow P1 so the living spec
matches the code, and P4's phone copy should follow D6 (P1 already removed
the producer). Run in order P1 → P2 → P3 → P4 → P5 so each commit is a
reviewable slice rather than one giant diff.

Do not start P2 while P1's leftover-config test is missing.

## Implementation Steps

### P1 — Delete Goose and `acphttp`; make the Go tree compile; fail-loud leftover config (D1, D2, D3, D4, D12; closes F1, F2, F3, F4, F13, F14, F15)

Delete the two package trees and `internal/daemon/goose_keyring*.go`.

Then, in the same commit, every compile-breaking site:

1. **Daemon.** Drop the `goose` and `acphttp` imports, the
   `Providers.Goose.Enabled` block, `acpHTTPConfig`, `IDGoose` in
   `prewarmPlan`, Goose from the `anyEnabled` check, and the "goose,
   opencode, and codex" comment (F14: reaping stays; the help-text name
   does not).
2. **Provider ID.** Remove `IDGoose` and its `provider_test.go` row.
3. **Config.** Remove `GooseProviderConfig` as a live type. Replace the
   `ProvidersConfig.Goose` field with a capture that mapstructure still
   binds to `"goose"` (MADR open question 1: prefer
   `RetiredGoose map[string]any \`mapstructure:"goose"\`` so presence of
   the key is visible even when empty). `Validate` returns a fixed error
   if the map is non-nil **or** any process env name has prefix
   `MCREMOTE_PROVIDERS_GOOSE_`. Do not `v.SetDefault("providers.goose…")`.
   Drop Goose from `KnownProviderIDs` / `SetProviderPrewarm` /
   `ProviderPrewarm`. Drop Goose cwd resolve in `load.go`. Drop Goose
   validation of timeouts / `with_builtins`. Drop Goose default block.
   Drop `keyring_disabled` from `secret_keys_test.go`'s exemption map
   (the field will be gone). Drop `TestGooseWithBuiltins*` and
   `TestDefaultsGooseKeyringDisabled*`. Add tests: leftover YAML fails;
   leftover env fails; a config with no Goose key loads.
4. **credstore.** Remove every Goose-named symbol listed in the MADR F4
   measurement. Delete the two Goose keyring test files. Remove Goose
   cases from `credstore_test.go` / `write_test.go`.
5. **WS.** Remove the `ErrGooseKeyringManaged` branch. Retarget
   `auth_err_code_test.go` so it still proves `keyring_managed` *would*
   map if that error existed, **or** drop those two table rows and keep
   C7 via `TestWSErrorCodesAreRegistered` / protocol docs. Prefer keeping
   the protocol test that the code is registered, and dropping the
   credstore-error rows (the error type is gone). Retarget
   `credential_write_test.go` fixtures from `id: "goose"` to `opencode`
   or `kilo` (D8).
6. **Doctor / engines / service template.** Remove the Goose credential
   probe. Engines `Long` text lists `opencode`/`kilo` `serve` and `codex
   app-server`, not `goose`. Delete the `goose:` block from
   `defaults_mcremote.yaml`. Remove `providers.goose.args` /
   `providers.goose.fs_roots` from `omittedConfigKeys` (the squash type
   is gone; the whole key is now retired and must not appear in
   templates — the leftover-config test covers it).
7. **Conformance.** Drop Goose from both provider lists in
   `command/conformance_test.go` and from `auth_conformance_test.go`.
   In `command_test.go`, rename the `gooseTbl` locals to a generic
   `noneTbl` — they test KindNone precedence, not Goose.
8. **chunkbuf.** Delete the `goose (acphttp)` case. The file it reads
   will not exist.
9. **ACP comments.** `ACPProviderConfig` / `acpAgentConfig` comments
   that say "goose and codex next" become "Grok today" (D12).

**Verification:**

```bash
go test ./...                         # pass
go test -race ./...                   # pass
test -d internal/provider/goose && exit 1
test -d internal/provider/acphttp && exit 1
# leftover YAML (use the project's config.Load, not a hand-rolled decoder)
# → error containing "providers.goose" and "0160"
```

### P2 — De-goose remaining Go comments and fixture names (D7, D8; closes F6, F7)

No package deletion. Comments and test names that still talk about Goose
as a *current* agent:

* picker / event / session / protocol / turn-latency comments: speak of
  remaining agents, or of "a provider that reports no dates", not Goose.
* `manager_durable_test.go`: rename `sess-goose` / `goose-chat` to a
  second Fake session id. The test is about CloseAll, not Goose.
* `agenterr`: comments say "structured engine logs" / "Rust Debug", not
  "goose file logs". Test names `TestExtractTextGooseJSON` →
  `TestExtractTextStructuredJSON` (keep the fixture *string*).
* `acpagent/version_test.go`: the `"goose"` `agentInfo.Name` is a generic
  ACP parse fixture — retarget to `"agent"` so a later grep does not
  false-flag it.
* `wirecap.go`: "four transports" / list grok stdio, opencode/kilo SSE,
  codex JSON-RPC. No websocket-ACP sentence.
* `stderr_tail_test.go` / `server_test.go`: `"goose"` as a log label or
  native-session id can become `"agent"` / `"native-1"`.

Do not weaken assertions. Do not delete `LooksLikeLongBackoff` tests.

**Verification:**

```bash
go test ./internal/agenterr/... ./internal/picker/... ./internal/session/... ./internal/chunkbuf/... ./internal/protocol/...
go test ./...
```

### P3 — Living product docs, examples, Makefile, AGENTS live tag (D1, D9, D11; closes F1, F10)

* `Makefile`: drop `live-goose` from `.PHONY` and the target.
* `AGENTS.md`: delete `-tags live_goose ./...` from the live-tag sentence.
  Leave the hooks list and the commit-message agent list (D11).
* `README.md`: product surface, architecture diagram, PATH binaries,
  engines blurb, provider table, config key table, `## Provider: Goose`,
  live-test command, tree comment, MADR 0025 row in the design table.
* `docs/config.md`: both the `providers.goose.*` key table and the
  `MCREMOTE_PROVIDERS_GOOSE_*` env table.
* `docs/protocol-v1.md`: provider enum and Goose-specific examples (MADR
  open question 4: rewrite examples onto Grok or another remaining agent;
  do not leave `provider: "goose"` as a current example). Keep
  `keyring_managed` in the error-code list (C7).
* `docs/ops-macos-tcc.md`, `docs/ops-android-emulator.md`,
  `apps/mobile/README.md`.
* The three `configs/*.yaml` example files: delete the `goose:` blocks
  and any comment that presents Goose as a current agent. Grok
  `mcp_servers` stays.

**Verification:**

```bash
rg -n 'live-goose|live_goose|providers\.goose|## Provider: Goose' README.md docs/config.md docs/protocol-v1.md Makefile AGENTS.md configs
# → no product hits. AGENTS.md still contains developer-Goose sentences.
make -n live-goose                    # → no rule
```

### P4 — Mobile icon, copy, and fixture retarget (D6, D8, D10, D12; closes F5, F9, F12)

* Delete `goose.svg`. Remove `goose` from `ids.txt`. Drop the `'goose'`
  manifest entry. Update `sync.sh` comment (OpenCode/Kilo dumps + remaining
  agent ids, not Goose's pinned table).
* `mc_exception.dart`: keep the `keyring_managed` case; drop
  `goose configure` (D6).
* Comments in `vendor_icon.dart`, `mcremote_client.dart`, `models.dart`,
  `picker.dart`, `chat_screen.dart`, `upstream_catalog_sheet.dart`: stop
  citing Goose as a current catalog/mode source.
* Tests: retarget provider ids to `grok` / `opencode` / `kilo` / a
  synthetic id. Keep the *behaviour* of:
  * unflagged default `auto` is not alarmed
  * flagged dangerous `auto` is alarmed and gated
  * large catalogs
  * `keyring_managed` wall copy
  * session create with a named provider
* `mode_selector_dangerous_test.dart` / `session_mode_dangerous_test.dart`
  / `resolve_displayed_mode_test.dart` may keep an `auto`/`approve`/
  `smart_approve`/`chat` list — that is a mode vocabulary, not a provider
  import — but names and comments must not claim a current Goose daemon.

**Verification:**

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed lib test
cd apps/mobile && flutter analyze
cd apps/mobile && flutter test
test -f apps/mobile/assets/vendor_icons/goose.svg && exit 1
```

### P5 — Stamp Goose-topic historical records superseded (D9; closes F11)

Frontmatter only. Set

```yaml
status: superseded by 0160-MADR-remove-goose-cli-support.md
```

on each in-scope file listed under P5. Do not edit headings, findings, or
plans' historical phases. `0026` is already superseded by 0030; this stamp
replaces that with 0160 (Goose support itself is what ended).

**Verification:**

```bash
rg -n '^status:' docs/spec/0025-MADR-goose-provider.md docs/spec/0030-MADR-goose-remote-parity.md docs/spec/0110-MADR-goose-keyring-prompts-block-headless-launch.md docs/spec/0122-MADR-deterministic-goose-file-log-tail-attach.md
# → each is superseded by 0160-MADR-remove-goose-cli-support.md
# git diff of those files is frontmatter-only
```

## Verification (whole plan)

```bash
go test ./...
go test -race ./...
rg -n 'internal/provider/goose|internal/provider/acphttp' --glob '*.go'   # none
make -n live-goose                                                      # no rule
# leftover config refuses (P1 test)
# AGENTS.md still lists goose among coding agents
```

On this Windows host, before calling the work done:

```bash
make ci-windows
```

**Not verifiable on this host unless the tools exist:** `flutter analyze` /
`flutter test` / `dart format` (P4). `make live-grok` etc. are not required
— this plan does not change remaining live suites.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `internal/provider/goose/` and `internal/provider/acphttp/` are gone | D1, D2 |
| A2 | `go test ./...` and `go test -race ./...` pass | D1, D8 |
| A3 | Leftover `providers.goose` YAML fails load, naming 0160 | D3 |
| A4 | Leftover `MCREMOTE_PROVIDERS_GOOSE_*` fails load | D3 |
| A5 | A config with no Goose key still loads | D3 |
| A6 | `credstore` has no Goose-named API; Grok/Codex/OpenCode/Kilo auth tests still pass | D4 |
| A7 | `make live-goose` is not a target; `AGENTS.md` live-tag sentence has no `live_goose` | D1, D11 |
| A8 | Phone has no Goose icon; `keyring_managed` copy has no `goose configure`; widget tests that encoded generic behaviour still exist | D6, D8, D10 |
| A9 | Living README/config/protocol/ops/examples do not present Goose as a current agent | D9 |
| A10 | Goose-topic MADRs in P5 are stamped superseded; mixed MADRs are untouched | D9 |
| A11 | `protocol.ErrorCodes()` still contains `keyring_managed` | D6 |
| A12 | `AGENTS.md` still names Goose as a coding agent / hook registrant | D11 |
| A13 | `agenterr` still classifies the former Goose JSON/Debug fixture strings | D7 |
| A14 | Durable-session tests still prove CloseAll keeps rows; they do not purge by provider id | D5 |

A3 is the criterion most likely to be quietly dropped under pressure. It
is C3. A phase that deletes `GooseProviderConfig` without the capture
field and the two leftover tests is not P1, even if `go test ./...` is
green.

## Rollout and Rollback

**What a user observes.** After a daemon that includes this plan: Goose
disappears from the provider picker. Existing Goose sessions remain in the
list and fail to resume with `unknown_provider`. A host whose YAML still
has a `goose:` block will not start, with an error that says to remove it.
`~/.config/goose/` is untouched; Goose's own CLI still works if installed.

**Per-phase revert.** Each phase is one commit. `git revert` of that
commit restores that slice. Reverting P1 without reverting P3/P4 leaves
docs claiming Goose is gone while the code has it again — revert in
reverse order (P5 → P1) if rolling back the whole change.

**No migration tool.** Operators delete the YAML block. There is nothing
to convert: Goose settings have no remaining destination.

## Deferred (named, so they are not mistaken for oversights)

* **Removing `keyring_managed` from the protocol.** D6 keeps it. A later
  record can drop a producer-less code once mixed-upgrade is irrelevant.
* **Auto-hiding or purging durable Goose sessions.** D5. If the list looks
  noisy, that is a product question for another pair.
* **Rewriting mixed historical MADRs** so they no longer mention Goose.
  History. D9.
* **Rebuilding `acphttp` as a generic transport.** Only if a future agent
  actually speaks ACP over HTTP. Start from MADR 0025 in git, not from a
  kept corpse.
* **Touching `~/.global-agent-hooks` Goose registration.** Different
  Goose. D11.
