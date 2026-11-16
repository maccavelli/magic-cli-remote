# Documentation defect list, 2026-11

<!-- markdownlint-disable MD013 -->

Made on 2026-11-15 at `master` `4e085b86`, by Phase A of
[0248-PLAN-bring-the-docs-current-and-keep-them-so.md](../decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md),
for [0248-MADR-bring-the-docs-current-and-keep-them-so.md](../decisions/0248-MADR-bring-the-docs-current-and-keep-them-so.md).
It decides nothing and fixes nothing. It gives every lead a verdict, names every doc read with its result, lists the
surface the docs must describe, and lists every defect found with its kind, its release class and the phase that fixes
it. Later phases mark the rows they close.

## Method

- **Start state (PLAN C1).** `HEAD` was `4e085b86`, not the `0fbed31b` the PLAN names. `git log 0fbed31b..HEAD` is one
  commit, the owner's commit of this pair's first draft. It changed `docs/README.md` (six index rows) and the pair, and
  no file under `internal/`, `cmd/`, `deploy/`, `README.md` or the `Makefile`. So it touches one lead, the survey's C1,
  which it closes. The tree held the pair's staged amendments and nothing else.
- **Release class.** `git log v0.26.0..HEAD` is three commits (`a7c993c0`, `0fbed31b`, `4e085b86`). Each changes only
  `docs/decisions/` and `docs/README.md`. No defect below was caused by a commit after `v0.26.0`, so every row is
  **older** for the next tag. The release-caused count is 0.
- **One-off tools**, in the executing session's scratchpad (`<scratch>`), standard library only, never committed. Each
  was first seen failing on a planted bad input in `<scratch>`:
  - a reference checker, after 0239's: it resolves eight kinds of reference (repository path, `make` target,
    `mcremote`/`mcrelay` subcommand, `--flag`, dotted settings key, `MCREMOTE_*`/`MCRELAY_*` variable, Go or Dart
    identifier, `x.y.z` version) against the tree at `HEAD`. On a planted doc with one bad and one good reference of each
    kind it reported exactly the eight bad ones;
  - a Go `go/parser` walker that lists every settings key from the `mapstructure` tags (squash, `-` and lists handled)
    and every module struct type an exported field reaches. On a planted package it listed exactly the planted keys and
    types, and it exited 3 on a missing root type;
  - the Phase D measurement program (PLAN step D1), described under "The doc-comment baseline";
  - a surface inventory script (step 4). On a scratch copy of the docs with `--trusted-proxy` removed it reported that
    flag as described nowhere; on the real docs it does not;
  - smaller checks: example-config keys against the code keys (a planted `listen.hostx` was reported), records without
    front matter (a planted record raised the count by one), guides against the README's documentation table (a
    planted README without `config.md` reported it), and package doc comments (a planted undocumented package was not
    listed).
- **Reading.** Every doc in the PLAN's read list was read in full against the code at `HEAD`. Four read-only helper
  passes, run at the same time, each read a share in full: `protocol-v1.md`; the mcrelay, Headscale, Route 53, receipts,
  `protocol-v2.md` and chat guides; the install and operations guides with the codex fixture README; and the mobile tree
  with the Go standards. The executor read the rule files, `AGENTS.md`, `docs/architecture.md`, the example configs, the
  package doc comments and every lead's cited lines. The "Basis" column says who read each defect's evidence: **own**
  (the executor), **checked** (a helper's finding whose cited lines the executor re-read before entering it) or
  **helper** (a helper's finding, cited by file and line, not re-read by the executor).
- No build, test or suite run was made, and no provider quota was spent. go-selfupdate-lib v1.26.2, libdns/route53
  v1.6.2 and viper were read in the module cache where a doc depends on them.

## The leads

| Lead | Verdict | Evidence |
| --- | --- | --- |
| Survey A1 | verified | `README.md:773` gives `_MAX_RETRIES` as a short `MCREMOTE_TLS_ROUTE53_*` alias. `internal/config/load.go:68-70` binds only the zone, region and profile; `load.go:304-306` says only the full `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` works, as `docs/guides/config.md:468` says. R01 |
| Survey A2 | verified, and wider | closed provider lists without gobble at `README.md:240-241`, `:264`, `:273`, `:1466`, `:1612`; `docs/architecture.md:37`, `:46`, `:81-84`; `apps/mobile/README.md:13`; also `apps/mobile/docs/architecture.md:34-35`, `protocol-v1.md:375-377`, `ops-macos-tcc.md:25`, `chat-performance.md:39`, `standards/go/testing.md:28-31`. `grep -c -i gobble` is 0 for `README.md`, `docs/architecture.md` and `apps/mobile/README.md`. `internal/config/prewarm_write.go:28`; `internal/provider/registered/registered.go:21` |
| Survey B1 | verified | no gobble row in the provider matrix (`README.md:833-841`), no "Provider: gobble" section (`README.md:1005-1213`), no `providers.gobble` row in the YAML surface (`README.md:876-899`). R07-R09 |
| Survey B2 | verified; not an orphan | `deploy/systemd/mcrelay.service` is cited by no unnumbered doc by path (the README's Deploy table names the other five files). Its own header gives the manual install (`deploy/systemd/mcrelay.service:5-7`) and `make verify-units` checks it (`Makefile:498`). So it is a documented-nowhere unit, not an orphan. R13, R35 |
| Survey B3 | verified, list corrected | of the 44 `.PHONY` targets (`Makefile:158-161`, `make/ci-windows.mk:15`), 11 are named by no unnumbered doc or `AGENTS.md` as `make <target>`: `check-cgo-off`, `check-host-target`, `clean`, `live-gobble`, `live-grok-discrimination`, `live-grok-rest`, `live-replacefirst`, `manifest-surface`, `markdownlint-docs`, `run`, `vet-tags`. The MADR's ten omitted `run`, which a loose word match finds everywhere. Three more targets are defined but not `.PHONY` and named nowhere: `codesign-maybe`, `live-codex-turn`, `live-codex-review` (`Makefile:362`, `:366`). 26 targets are absent from the README's Development block. R12 |
| Survey B4 | verified | `auth-recovery` is in the usage block (`README.md:639`) and has no row in the command table (`README.md:646-660`); `internal/cli/root.go:86`. R11 |
| Survey B5 | refuted as a defect | `doctor`, `version` and `completion` have rows (`README.md:657`, `:659`, `:660`); the owner's definition asks for one description |
| Survey B6 | verified, and wider | 17 of the 100 variable names in non-test Go are named by no doc (inventory below), including `XAI_API_KEY` (`internal/provider/grok/auth.go:81`) and the three `OPENCODE_*` names the daemon sets on its OpenCode child (`internal/provider/opencode/control/control.go:77`, `:487-488`). R23, R24 |
| Survey B7 | verified, prefix settled | the release spec sets `"env_prefix": "MCREMOTE"` (`internal/updateclient/selfupdate-release.json`), which go-selfupdate-lib v1.26.2 renders into both installers. `--no-hooks`, `--dir`, `--dry-run`, `MCREMOTE_NO_HOOKS`, `MCREMOTE_INSTALL_DIR`, `MCREMOTE_VERSION` and (Windows only) `MCREMOTE_NO_PATH_UPDATE` are supported; `MCREMOTE_NO_SERVICE` and `MCREMOTE_INSTALL_NO_PATH_UPDATE` are old names the guides present as history. Basis: helper, with the library's `install.sh` and `install.ps1` line numbers |
| Survey B8 | verified | no changelog is tracked (`git ls-files`); `gh release list` gives 17 releases, `v0.17.1` to `v0.26.0`; the `v0.26.0` body is one "Full Changelog" link (other bodies not opened); 123 `vX.Y.Z` tags |
| Survey C1 | verified, closed | `4e085b86` added the four `0246`/`0247` rows with `--write-index` |
| Survey C2 | verified, not a defect | the index has no status or date column (`scripts/check_records.py:189`) |
| Survey C3 | verified OK | the three "I want to…" record links resolve (`make check-records` exit 0) |
| Survey C4 | verified OK | `README.md:1683` matches `0074-MADR-remote-provider-auth-from-phone.md:3` |
| Survey C5 | verified OK | `README.md:1664`, `:1667`, `:1679` match 0025's and 0065's status lines |
| Survey C6 | verified OK | `README.md:1189` and `docs/guides/config.md:185` give kilo 7.8.18; `internal/provider/kilo/version.go:25`. The example configs still say 7.4.22 (R140) |
| Survey C7 | verified | three commits since `v0.26.0`, all records only |
| Survey C8 | verified | 168 of 469 tracked records have no front matter: 82 MADRs, 67 PLANs, 19 REPORTs (scratch count). OQ2: left as history |
| MADR new: record path | verified | `.opencode/rules.md:36` and `.kilo/agent/madr-and-plan-skill.md:41` say `docs/NNNN-MADR-*`; `AGENTS.md:75`, `:110`, `.claude/rules/madr-and-plan-skill.md:41`, `.codex/rules/madr-plan-before-mutating-work.md:27`, `.grok/rules/madr-plan-before-mutating-work.md:42` say `docs/decisions/`. R129 |
| MADR new: live targets | verified, count corrected | `AGENTS.md:51` names 4 live targets. Of the 10 `.PHONY` `live-*` targets, 6 are unnamed there, not 5: `live-codex-contract`, `live-gobble`, `live-grok-discrimination`, `live-grok-rest`, `live-replacefirst`, `live-supervisors`; plus the non-`.PHONY` `live-codex-turn` and `live-codex-review`. R128 |
| MADR new: README tables | verified, count corrected | the README's documentation table omits 8 guides, not 4. Four are linked nowhere in the README (`ops-codex-contract.md`, `ops-hardware-validation.md`, `ops-android-emulator.md`, `ops-ios-signing.md`); four are linked elsewhere in it but not in the table (`ops-credential-recovery.md`, `ops-linux-install.md`, `ops-macos-tcc.md`, `ops-windows-install.md`). "Architecture & key decisions" lacks 0179 and also 0214. The "I want to…" table gives no route to the MADR's seven guides plus `mobile-profiling.md` and `ops-android-emulator.md`. R14, R15, R19 |
| MADR new: gobble behaviour | verified | `af8c42fb`, `bac7db11`, `4614263c` (all in `v0.26.0`) changed `internal/provider/gobble/auth.go` and records only. `docs/guides/config.md:128`, `:236-237` still say the handshake takes the first upstream "with a stored credential"; the code takes the first with a usable credential and no reason (`internal/provider/gobble/auth.go:259-261`). No guide says device sign-in is offered only for the grok and kilo upstreams (`internal/provider/gobble/auth.go:61`) or that the device code is awaited 30 s (`internal/provider/gobble/auth.go:41`). R21, R22 |
| MADR new: doc comments | verified, reproduced exactly | see "The doc-comment baseline" |
| 0239 F6 "≤60 s" | refuted | no 60 s bound exists: a v2 connection is reaped at the read deadline, `limits.ws_read_deadline_seconds`, default 120 (`internal/ws/liveness.go:50-56`, `:121-128`; `internal/config/config.go:568`). 60 s was the old deadline, raised in `13de0ffb`. R97 |
| 0239 "164 methods" | refuted as current | 164 was the 0.155.1 pin's experimental client-request count; at the 0.167.0 pin both sides declare identical sets, 202 stable and 277 experimental methods (`internal/provider/codex/testdata/0.167.0/manifest.json`). Basis: helper, numbers computed from the manifests. R99 |
| 0239 "Host dedup" | refuted except for kilo | only kilo suppresses unchanged `usage_update` (`internal/provider/kilo/usage.go:74-90`); acpagent and codex forward every report (`internal/provider/acpagent/session.go:2121-2144`, `internal/provider/codex/session.go:1695-1714`); `running` is sent once per turn by each. `usage_update` is batchable on the phone, so it does not bypass the 32 ms window. R48 |
| 0239 `AsyncNotifier` | verified, still true | no `AsyncNotifier`, `AsyncValue` or `AsyncValue.guard` in `apps/mobile/lib` or `apps/mobile/test`; state is synchronous `Notifier`. R122 |

## Every doc read

"Checker" is the reference checker's unresolved count; each unresolved reference was read in context and is a wire
path, an external tool's flag or name, an example value, or marked history, except where a defect row cites it.

| Doc | Lines | Read by | Checker | Result |
| --- | ---: | --- | ---: | --- |
| `docs/guides/protocol-v1.md` | 2,677 | helper | 30 | defects R52-R87 |
| `docs/guides/config-mcrelay.md` | 458 | helper | 7 | defects R26-R36 |
| `docs/guides/ops-mcrelay.md` | 390 | helper | 13 | defect R37 |
| `docs/guides/headscale.md` | 296 | helper | 15 | defects R38-R40 |
| `docs/guides/iam-route53-acme.md` | 376 | helper | 40 | defect R41 |
| `docs/guides/receipts.md` | 218 | helper | 5 | defect R42 |
| `docs/guides/protocol-v2.md` | 251 | helper | 6 | defects R44-R46 |
| `docs/guides/chat-performance.md` | 101 | helper | 1 | defects R47-R51 |
| `docs/guides/ops-linux-install.md` | 466 | helper | 18 | defect R88 |
| `docs/guides/ops-windows-install.md` | 496 | helper | 41 | defects R89-R91 |
| `docs/guides/ops-macos-tcc.md` | 126 | helper | 2 | defects R92-R95 |
| `docs/guides/ops-credential-recovery.md` | 110 | helper | 2 | defect R96 |
| `docs/guides/ops-hardware-validation.md` | 423 | helper | 23 | defects R97-R98 |
| `docs/guides/ops-codex-contract.md` | 233 | helper | 15 | defects R99-R100 |
| `internal/provider/codex/testdata/0.167.0/README.md` | 60 | helper | 5 | defect R101 |
| `docs/guides/standards/go/` (9 files) | 400 | helper | 18 | defects R123-R127 |
| `apps/mobile/README.md` | 192 | helper | 6 | defects R102-R109 |
| `apps/mobile/docs/README.md` | 24 | helper | 0 | accurate |
| `apps/mobile/docs/architecture.md` | 59 | helper | 2 | defects R110-R113 |
| `apps/mobile/docs/guides/` (4 guides) | 532 | helper | 20 | defects R114-R117; `mobile-profiling.md` and `ops-ios-signing.md` accurate |
| `apps/mobile/docs/guides/standards/mobile/` (7 files) | 422 | helper | 14 | defects R118-R122 |
| `AGENTS.md` | 110 | own | 3 | defects R128, R130 |
| `.claude/rules/`, `.codex/rules/`, `.grok/rules/` (one file each) | 120 | own | 1 | accurate |
| `.opencode/rules.md`, `.kilo/agent/madr-and-plan-skill.md` | 83 | own | 3 | defect R129 |
| `docs/architecture.md` | 128 | own | 3 | defects R17-R18 |
| package doc comments (52 packages carry one, 54 blocks) | 433 | own | — | defects R131-R138 |
| `configs/*.yaml` against `config.md` | 1,348 | own | — | defects R139-R142 |

The read list is 8,623 lines of Markdown (44 files); with `docs/guides/config.md` (931), against which the example
configs were read, it is 9,554, the PLAN's "about 9,500". `README.md`, `docs/guides/config.md` and `docs/README.md`,
which the survey read in full, were run through the checker (55, 39 and 19 unresolved) and read where a lead or the
inventory pointed.

## The surface inventory (step 4)

Each list is drawn from the code at `HEAD`. "Named nowhere" means no unnumbered doc or `AGENTS.md` names the item. OQ4's
reading, (a), puts every list in scope.

| List | Source | Items | Named nowhere | Gaps |
| --- | --- | ---: | ---: | --- |
| providers | `KnownProviderIDs` and `registered.Descriptors` | 6 | 0 | gobble is described only in `config.md` and `protocol-v1.md`: no README matrix row or section (R07, R08). `fake` has a matrix row |
| `mcremote` commands | cobra `Use:` under `internal/cli/`, plus `update` and cobra's `completion` | 21 | 0 | `auth-recovery` has no README table row (R11) |
| `mcrelay` commands | `internal/relay/cli.go:80-84`, plus `completion` | 6 | 0 | `mcrelay update` has no `config-mcrelay.md` entry (R26) |
| flags | every flag registration under `internal/cli/` and in `internal/relay/cli.go` | 82 (44 + 38) | 0 | `mcrelay paths --data-dir` and the hidden `--detach-console` are absent from `config-mcrelay.md` (R26, R36) |
| environment variables, read, bound or set | literals in non-test Go | 100 | 17 | internal markers set on children: `MCREMOTE_ENGINE_ID`, `MCREMOTE_ENGINE_OWNER`, `OPENCODE_PASSWORD`, `OPENCODE_CONFIG`, `OPENCODE_CONFIG_CONTENT` (R23); user-set: `XAI_API_KEY` (R24); platform: `NOTIFY_SOCKET`, `COMSPEC`, `PATHEXT` (R24); test or developer only: `MCREMOTE_TOKEN`, `MCREMOTE_STUB_WARM`, `MCREMOTE_WIRE_CAPTURE_DIR`, `MCREMOTE_WIRECAP_HOST_KEY`, the three `MCREMOTE_UPDATECLIENT_FIXTURE_*`, `GO_WANT_CODEX_APP_SERVER_HELPER` (R24) |
| environment variables, key-derived | viper's prefix and key replacer over every settable key | 99 | 36 | none: each is covered by the stated rule (`docs/guides/config.md:442`, `:543`; `config-mcrelay.md:138`). Keys under a list of structs (`mcp_servers`, `environments`, `hosts`) have no variable and are left out |
| settings keys | `mapstructure` tags from `config.Config`, each `providers.<id>` section, and `relay.FileConfig` | 164 (128 + 36) | 0 | 18 are named by their leaf, not their path: the `mcp_servers` entry fields (the README's example, `README.md:945-954`), codex `environments` fields (`docs/guides/config.md:181`), mcrelay `hosts` fields (`config-mcrelay.md:220-222`) |
| make targets | `.PHONY` in the `Makefile` and `make/*.mk`, plus 3 defined-only | 47 | 14 | see Survey B3; every target needs a README Development row (OQ4), 26 lack one (R12) |
| deploy files | `git ls-files deploy/` | 6 | 1 | `deploy/systemd/mcrelay.service` (R13, R35) |
| Go packages | `go list ./...` | 56 | — | 4 lack a package doc comment (R138); the five packages outside `internal/` are all `main` |

## The contract types (step 5a)

`go list -f '{{.ImportPath}} {{.Name}}' ./...` lists 56 packages. The five outside `internal/` (`cmd/mcremote`,
`cmd/mcrelay`, `scripts/capture-wire`, `scripts/smoke-protocol`, `test/supervisors/probeagent`) are all `main`, so
"public packages" adds no type. The contract types are:

- the wire: the 90 exported struct types in `internal/protocol`, the 6 join-plane structs in
  `internal/relay/protocol.go`, and every module struct their exported fields reach (the scratch walker's closure): 150
  types;
- the config: every struct the `mapstructure` walk reaches from `config.Config`, the six `providers.<id>` sections and
  `relay.FileConfig`: 32 types.

182 types in all. None is an interface type, so the interface-method count is 0. A field counts as documented when it
has its own doc or trailing comment, or when its type's doc comment names it as a word.

| Package | Types | Exported fields | Lacking a comment |
| --- | ---: | ---: | ---: |
| `internal/protocol` | 90 | 351 | 248 |
| `internal/event` | 20 | 179 | 92 |
| `internal/provider` | 26 | 131 | 111 |
| `internal/config` | 17 | 74 | 22 |
| `internal/relay` (6 wire, 8 config) | 14 | 63 | 42 |
| `internal/session` | 2 | 31 | 18 |
| `internal/picker` | 3 | 20 | 5 |
| `internal/provider/codex` | 2 | 16 | 4 |
| `internal/receipt` | 3 | 9 | 6 |
| `internal/provider/grok` | 1 | 6 | 0 |
| `internal/provider/kilo` | 1 | 3 | 0 |
| `internal/provider/fake` | 1 | 1 | 0 |
| `internal/provider/opencode` | 1 | 1 | 0 |
| `internal/provider/gobble` | 1 | 0 | 0 |
| **all** | **182** | **885** | **548** |

The per-type list is the walker's output, regenerated by Phase D's step D1 from the same roots: every exported struct
type in `internal/protocol/messages.go`, `internal/protocol/errors.go` and `internal/relay/protocol.go`, followed through
exported fields; and the `mapstructure` roots above. These 548 fields are Phase D's contract-field scope (R138).

## The doc-comment baseline (step 5)

The PLAN's D1 program, rebuilt in `<scratch>` with `go/parser`, `go/ast` and `go/token`, over the 359 files of
`git ls-files '*.go'` less `_test.go` (none generated). On the PLAN's planted two-package tree it reported exactly the
five planted gaps; with a contract list it reported an undocumented field on a listed type and not a listed type whose
comment names its one field.

At `4e085b86`, whose Go files are byte-identical to `0fbed31b`'s (`git diff --name-only 0fbed31b HEAD -- '*.go'` is
empty), it reproduces the PLAN's baseline exactly:

| Category | Total | Missing |
| :--- | ---: | ---: |
| package doc comment | 56 | 4 |
| func | 363 | 0 |
| type | 515 | 0 |
| method, exported receiver | 443 | 5 |
| method, unexported receiver | 384 | 176 |
| struct field | 1,822 | 1,177 |
| interface method | 216 | 107 |
| const, own or group comment | 711 | 0 |
| const, own comment | 711 | 356 |
| var, own or group comment | 81 | 0 |
| var, own comment | 81 | 65 |
| contract-type field | 885 | 548 |
| contract-type interface method | 0 | 0 |

The struct-field and interface-method rows count the direct fields and methods of exported top-level types. Counting
every struct and interface literal in the tree gives 3,051 / 2,348 and 272 / 159 instead, which is not the baseline;
D1 must keep the narrower reading. The missing items are those the PLAN names: package comments in `cmd/mcremote`,
`cmd/mcrelay`, `internal/provider/codex`, `internal/provider/opencode`; methods `internal/procutil/reap.go:68`,
`internal/provider/cwd.go:36`, `:40`, `internal/providerauth/classify.go:32`, `internal/updateclient/client.go:328`.

## The defect table

Kinds: **wrong** (false today), **missing** (a surface or fact the doc should carry), **stale** (true once, now
outdated). Every row's release class is **older**: nothing below was caused by a commit after `v0.26.0`. Destinations:
**B**, **C**, **D** or **F**, the phase that fixes it; **owner** where the fix might be the code (D8), or where the file
is outside the PLAN's scope. Where a doc is wrong and the code's behaviour may be the intended one, B rewrites the doc to
the code only after the owner's word; such rows say "owner".

### Entry points and the configuration guide

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R01 | `README.md:773` | wrong | B | `_MAX_RETRIES` given as a short `MCREMOTE_TLS_ROUTE53_*` alias | give `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` its own row; it has no short alias, as `docs/guides/config.md:468` says | `internal/config/load.go:68-70`, `:304-306` | own |
| R02 | `README.md:240-241` | wrong | B | product surface lists Grok, OpenCode, Codex, Kilo and Fake | add gobble (ACP, `gobble acp`) | `internal/provider/registered/registered.go:21` | own |
| R03 | `README.md:264` | wrong | B | diagram: "providers: grok, opencode, codex, kilo, fake" | add gobble | as R02 | own |
| R04 | `README.md:273` | wrong | B | engine list omits `gobble acp` | add `gobble acp` | `internal/provider/gobble/doc.go:1-2` | own |
| R05 | `README.md:1466` | wrong | B | picker: "grok / opencode / codex / kilo / fake" | the picker shows what the daemon's `providers.list` returns: gobble, grok, opencode, codex, kilo, and fake when enabled | `apps/mobile/lib/data/ws/mcremote_client.dart:3625-3640` | helper |
| R06 | `README.md:1612` | wrong | B | layout: `provider/ # grok, opencode, codex, kilo, fake` | add gobble | as R02 | own |
| R07 | `README.md:833-841` | missing | C | provider matrix has no gobble row | `gobble`, `enabled: true`, ACP over stdio (`gobble acp`), one process per session | `internal/provider/gobble/section.go:22-29` | own |
| R08 | `README.md:1005-1213` | missing | C | no "Provider: gobble" section | binary and transport, known-good 0.4.0, `providers.gobble.*` (link `config.md`), sign-in as built (R22), readiness (a row with a reason is not ready), `make live-gobble` | `internal/provider/gobble/gobble.go:18`; `internal/provider/gobble/auth.go:41`, `:61`, `:259-261`; `Makefile:402` | own |
| R09 | `README.md:876-899` | missing | C | YAML surface has no `providers.gobble` row | add the row with gobble's keys (the ACP keys, no `fs_roots` effect) | `internal/config/config.go:514-532` | own |
| R10 | `README.md:941-956` | missing | C | "The `grok` provider supports an `mcp_servers` list" | gobble and opencode take the same list (`README.md:893` already lists it for opencode) | `internal/config/config.go:532` | own |
| R11 | `README.md:646-660` | missing | C | no `auth-recovery` row | `auth-recovery status` / `choose`: inspect and choose a provider's credential backup; link `ops-credential-recovery.md` | `internal/cli/root.go:86`; `internal/cli/auth_recovery.go:54` | own |
| R12 | `README.md:1563-1599` | missing | C | Development block lacks 26 targets | one row each for `apk`, `build`, `build-relay`, `build-remote`, `check-cgo-off`, `check-host-target`, `check-records`, `ci-windows`, `ci-windows-smoke`, `clean`, `codesign-maybe`, `debug`, `install`, `install-relay`, `live-codex-contract`, `live-codex-review`, `live-codex-turn`, `live-gobble`, `live-grok-discrimination`, `live-grok-rest`, `live-replacefirst`, `live-supervisors`, `manifest-surface`, `markdownlint-docs`, `run`, `vet-tags` | `Makefile:158-161`, `:362`, `:366`; `make/ci-windows.mk:15` | own |
| R13 | `README.md:1413-1420` | missing | C | Deploy table has no row for `deploy/systemd/mcrelay.service` | a row: system-wide (root) mcrelay unit for a server edge, installed by hand | `deploy/systemd/mcrelay.service:1-7`; `Makefile:498` | own |
| R14 | `README.md:1641-1654` | stale | F | documentation table omits 8 guides | the rows in "Phase F rows" below | the guide map | own |
| R15 | `README.md:1656-1685` | stale | F | "Architecture & key decisions" lacks the gobble and OpenCode v2 provider MADRs | the rows in "Phase F rows" below | `docs/decisions/0179-MADR-gobble-native-acp-provider.md`; `README.md:838` links 0214 | own |
| R16 | `README.md:1548-1562` | missing | C | the smoke step uses `MCREMOTE_TOKEN` without naming it | name `MCREMOTE_TOKEN` as the token source `smoke-protocol` reads when `-token` is not given | `scripts/smoke-protocol/main.go:57` | own |
| R17 | `docs/architecture.md:37`, `:46`, `:81-84` | wrong | B | provider and engine lists omit gobble | add gobble: registered adapter, ACP over stdio per session (`gobble acp`) | as R02, R04 | own |
| R18 | `docs/architecture.md:111-115` | wrong | B | Linux services: "systemd `--user`" only | systemd `--user`, runit, s6 or OpenRC on Linux, as `README.md:653` says | `internal/cli/service/detect.go:19-31` | own |
| R19 | `docs/README.md:492-512` | stale | F | "I want to…" gives no route to 9 guides | the rows in "Phase F rows" below | the guide map | own |
| R21 | `docs/guides/config.md:128`, `:236-237` | wrong | B | the handshake authenticates "the first upstream with a stored credential" | the first upstream with a usable credential and no reason, in gobble's order, unless `auth_method_id` pins one | `internal/provider/gobble/auth.go:259-261` (`4614263c`) | own |
| R22 | `docs/guides/config.md:230-242` | missing | C | gobble's sign-in scope is not described | the phone's device sign-in is offered only for the grok and kilo upstreams; the device code is awaited up to 30 s | `internal/provider/gobble/auth.go:41`, `:61` (`af8c42fb`, `bac7db11`) | own |
| R23 | `docs/guides/config.md` (environment section) | missing | C | no "set by the daemon, do not set" table (OQ4) | `MCREMOTE_ENGINE_ID`, `MCREMOTE_ENGINE_OWNER` (engine markers), `OPENCODE_PASSWORD`, `OPENCODE_CONFIG`, `OPENCODE_CONFIG_CONTENT` (set on the OpenCode control child) | `internal/procutil/reap.go:22`, `:25`; `internal/provider/opencode/control/control.go:77`, `:487-488` | own |
| R24 | `docs/guides/config.md`; the test targets' docs | missing | C *(2026-11-15, the coordinator's application of OQ4's answer, not a new ruling: `NOTIFY_SOCKET`, `COMSPEC` and `PATHEXT` are read by non-test code, so Phase C gives the three one short note in `docs/guides/config.md`)* | 12 more variables are named nowhere | user-set `XAI_API_KEY` (grok counts as signed in when set); platform `NOTIFY_SOCKET`, `COMSPEC`, `PATHEXT`; test or developer only, beside their target or tool: `MCREMOTE_TOKEN` (R16), `MCREMOTE_STUB_WARM`, `MCREMOTE_WIRE_CAPTURE_DIR`, `MCREMOTE_WIRECAP_HOST_KEY`, `MCREMOTE_UPDATECLIENT_FIXTURE_VERSION`, `_EXIT`, `_FAIL_NAMED`, `GO_WANT_CODEX_APP_SERVER_HELPER` | `internal/provider/grok/auth.go:81`; `internal/sdnotify/sdnotify.go:29`; `internal/provider/launch/launch_windows.go:160`, `:201`; `internal/testexec/testexec.go:62`; `internal/wirecap/wirecap.go:47`, `:87`; `internal/updateclient/testdata/fixture/main.go:32-34`; `internal/provider/codex/provider.go:1160` | own |

### mcrelay, Headscale, Route 53, receipts, protocol v2 and chat guides

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R26 | `docs/guides/config-mcrelay.md:21`, `:316-441` | missing | C | no `mcrelay update`, and `paths` lacks `--data-dir` | a `paths` row (`--json`, `--data-dir`) and an `update` row linking `config.md`'s update section | `internal/relay/cli.go:84`, `:137-138` | helper |
| R27 | `config-mcrelay.md:42-47`, `:72` | missing | C | only `--allow` is said to merge into `hosts` | `MCRELAY_HOSTS` and `--allow` both merge into the YAML hosts, in that order; a later entry with the same id wins | `internal/relay/fileconfig.go:332-356` | helper |
| R28 | `config-mcrelay.md:138-178` | wrong | B | the variable table is called complete | key-derived names (`MCRELAY_TLS_LETSENCRYPT_*` and the rest) also work and are read before the bound ones | `internal/relay/fileconfig.go:228-230` | helper |
| R29 | `config-mcrelay.md:113-115` | wrong | B | a world-readable config is refused | any group or other permission bit is refused; a symlinked config path is refused too (inferred) | `internal/appdirs/owneronly_unix.go:18-22`; `fileconfig.go:292-298` | helper |
| R30 | `config-mcrelay.md:151` | wrong | B | `MCRELAY_TLS_DOMAINS` "(HTTP-01)" | the domains serve HTTP-01 and DNS-01 | `internal/relay/tls.go:86-87` | helper |
| R31 | `config-mcrelay.md:248` | wrong | B | `off`: "warns; tests / loopback only" | refused on a non-loopback bind unless `--allow-plaintext`; on loopback it serves plaintext with a warning | `internal/relay/server.go:202-211` | checked |
| R32 | `config-mcrelay.md:36-38` | missing | C | the seed config needs only `hosts`/TLS edits | the seed binds loopback and holds a placeholder host; set `listen.host` with `tls.mode` and replace the host before exposing it | `internal/cli/service/defaults_mcrelay.yaml:55-56` | checked |
| R33 | `config-mcrelay.md:389` | missing | C | `--binary` default omits the fallback | else this executable | `internal/relay/cli.go:355` | helper |
| R34 | `config-mcrelay.md:364` | wrong | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: a deploy file; carried to the queued deploy-files record, not Phase B)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: this is queued record 3 of four, and the four records wait for the gobble branch work)* | the example plist's PATH and environment differ from what `setup-service` renders | make `deploy/launchd/com.magiccliremote.mcrelay.plist` match the rendered closed PATH and `XDG_STATE_HOME` (a deploy file, outside the PLAN's scope) | `internal/cli/service/setup.go:1150-1157`; `plist_render.go:88-97` | helper |
| R35 | `config-mcrelay.md` (whole) | missing | C, owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: Phase C keeps the documentation part; the question whether the unit gains a writable state directory goes to the queued deploy-files record)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: its unit part is in queued record 3 of four, and the four records wait for the gobble branch work)* | the system unit is not documented; it sets `ProtectSystem=strict` with no writable data path, so the data and ACME directories may be unwritable (inferred, not run) | C: document the system unit; owner: whether the unit gains `StateDirectory=` (a deploy change) | `deploy/systemd/mcrelay.service`; `internal/relay/cli.go:241` | helper |
| R36 | `config-mcrelay.md:320-353` | missing | C | hidden `--detach-console`; root accepts the setup-service flags | note both | `internal/relay/cli.go:78`, `:320-321` | helper |
| R37 | `docs/guides/ops-mcrelay.md:387` | wrong | B | "`hub.phoneGone`" | `pendingJoin.phoneGone`, under the hub lock | `internal/relay/hub.go:53-55`, `:304` | helper |
| R38 | `docs/guides/headscale.md:162`, `:178-179` | wrong | B | says v0.29 takes a user ID, then passes a user name | use the numeric user ID in both places | the guide itself | helper |
| R39 | `headscale.md:285` | stale | B | off-mesh access is out of scope | off-mesh access is mcrelay (`ops-mcrelay.md`); Phase 1 did not include it | `internal/config/load.go:71-73`; `internal/relayhost/client.go:188` | helper |
| R40 | `headscale.md:22`, `:44`, `:48`, `:68`, `:124`, `:131-132`, `:150`, `:175`, `:178-179` | wrong | owner | names a private resolver address, a Headscale user name that looks real, and the live state of a named utility host (values not repeated here) | placeholders (`<user>`, `<resolver>`), and the host's state as a dated observation or removed | published already; the disclosure guard's deny list does not cover them | helper. *2026-11-15: closed by the PLAN's entry "Identifiers in two published guides — 2026-11-15"; the line numbers here are those of `e1922f35`.* |
| R41 | `docs/guides/iam-route53-acme.md:8-10` | stale | B | the worked example's `devbox.ts.example.com` "from headscale.md" | "a daemon name such as `devbox.ts.example.com`" | `headscale.md` has no `devbox` | helper |
| R42 | `docs/guides/receipts.md:74-76` | wrong | B, owner *(2026-11-15, the owner's ruling "Prove it first, then decide", relayed by the coordinator: Phase B corrects only the "walks backward" sentence; the truncation sentence is carried to a queued record, see the next note)* *(2026-11-15, later, the owner's ruling "Fix the code first; guide waits", relayed by the coordinator: the experiment ran against the real code on a scratch copy: tail truncation is not detected. A queued record makes it detectable. The guide's sentences that truncation is detected and that verify is safe for an audit script stay as written and are false until it lands. Phase B still corrects only "walks backward")* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: this is queued record 4 of four, and the four records wait for the gobble branch work)* | verify "walks backward from the last line"; truncation "is detected" | B: verify walks forward from line 1 and reports the first broken line. Owner: removing trailing whole lines leaves a valid chain, so tail truncation is not detectable (inferred); whether the code should detect it | `internal/receipt/store.go:286-356` | checked |
| R44 | `docs/guides/protocol-v2.md:51-109` | missing | C | `caps` lacks `receipts`, `provider_auth`, `provider_auth_transactions` | add the three booleans (omitted when false) | `internal/protocol/messages.go:255-276` | helper |
| R45 | `protocol-v2.md:104-106` | stale | B | capability ids come from the installed manifest | the sorted ids the running engine negotiated; the installed manifest before any engine has | `internal/ws/liveness.go:176-190`; `internal/provider/codex/capabilities.go:336-389` | helper |
| R46 | `protocol-v2.md:111-133` | missing | C | host-global operations omit `codex.threads.read` / `.write` | list them, or link `protocol-v1.md:346` | `internal/ws/codex_handlers.go:62-75` | helper |
| R47 | `docs/guides/chat-performance.md:39` | wrong | B | coalescing "on grok, codex and kilo; fixed for OpenCode" | configurable on grok, codex and kilo; fixed at 80 ms for OpenCode and gobble | `internal/provider/gobble/gobble.go:101-108`; `internal/provider/acpagent/config.go:116-122` | helper |
| R48 | `chat-performance.md:40` | wrong | owner *(2026-11-15, the owner's ruling "Docs follow the code, all three", relayed by the coordinator: now B; the doc is rewritten to what the code does, with no behaviour change)* | "Host dedup": every host suppresses unchanged `usage_update` | only kilo does; acpagent and codex forward every report. The dedup existed for OpenCode's HTTP dialect (`b0e26560`) and was lost with `f47b530d`; whether to restore it is the owner's call, then B | as the lead | checked |
| R49 | `chat-performance.md:41` | missing | C | the batched set omits `identified_plan` | add it | `apps/mobile/lib/state/transcripts_notifier.dart:56-70` | helper |
| R50 | `chat-performance.md:47` | stale | B | transcripts are kept in SharedPreferences | per-session files, with the LRU index in SharedPreferences (150 items, 12 sessions) | `apps/mobile/lib/data/chat/transcript_cache.dart:131-143` | helper |
| R51 | `chat-performance.md:84`, `:87` | wrong | B | the FCM non-goal appears twice | merge the rows | the guide | helper |

### `protocol-v1.md`

`internal/protocol/doc_coverage_test.go` holds two things against this guide: every `event.Types()` value appears in the
"Event `type` values" paragraph (`:1909-1913`; 38 of 38, both directions), and every `ErrorCodes()` entry appears
somewhere in the guide (a substring match, so generic codes pass on prose). Nothing holds message types, payload
fields, nested shapes, numbers, provider lists or close codes.

| ID | Where (`docs/guides/protocol-v1.md`) | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R52 | `:1915-1917` | wrong | B | a test enforces a section or field entry per event type | the test enforces that every emitted type appears in the list paragraph | `internal/protocol/doc_coverage_test.go:44-72` | checked |
| R53 | `:498-500`, `:507`, `:515` | wrong | owner *(2026-11-15, the owner's ruling "Docs follow the code, all three", relayed by the coordinator: now B; the doc is rewritten to what the code does, with no behaviour change)* | `agent_sessions.list` is served by the ACP providers | only codex serves it; ACP providers answer `unsupported` (the lister is on the ACP session, not the provider), kilo answers `agent_sessions_list_failed`. Whether ACP should serve it is the owner's call, then B | `internal/ws/server.go:3113-3116`; `internal/provider/acpagent/sessioncaps.go:58`; `internal/provider/codex/threads.go:868` | checked |
| R54 | `:1663-1668` | wrong | owner *(2026-11-15, the owner's ruling "Docs follow the code, all three", relayed by the coordinator: now B; the doc is rewritten to what the code does, with no behaviour change)* | kilo emits `diagnostics_changed` on `mcp.tools.changed` / `lsp.updated` | no dialect implements the hook, so nothing emits it; owner: implement or say so, then B | `internal/provider/httpagent/httpagent.go:542`; `internal/provider/httpagent/provider.go:833-837` | checked |
| R55 | `:774-775` | wrong | B | live `session.diff` events become a notice | they are deliberately not rendered | `internal/provider/kilo/session.go:542` | helper |
| R56 | `:763`, `:767`, `:772` | stale | B | fork/revert/unrevert/diff are OpenCode session-tree features | fork: grok, gobble, codex; revert, unrevert, diff: OpenCode's control plane; others `session_*_failed` | `internal/provider/grok/grok.go:176-178`; `internal/provider/gobble/gobble.go:93-96` | helper |
| R57 | `:375-377`, `:419` | wrong | B | `provider` is one of fake, grok, opencode, kilo, codex | add gobble | as R02 | checked |
| R58 | `:1066-1068` | wrong | B | `unknown_provider`: "outside the four agents" | outside the five agents: gobble, grok, opencode, codex, kilo | `internal/config/prewarm_write.go:28` | checked |
| R59 | `:979` | stale | B | the busy queue is on kilo, grok and opencode | every provider: kilo, the ACP agents (grok, opencode, gobble) and codex, max 4 | `internal/provider/acpagent/session.go:348-372`; `internal/provider/codex/session.go:27` | helper |
| R60 | `:386-391` | stale | B | `thinking_level` in `_meta` for grok and OpenCode | the ACP providers, gobble included | `internal/provider/gobble/gobble.go:101-108` | helper |
| R61 | `:701-715` | missing | C | 16 canonical commands absent from the vocabulary table | list all 30 in `internal/command/specs.go` | `internal/command/specs.go:13-180` | helper |
| R62 | `:722-733` | missing | C | per-provider mechanisms for grok and OpenCode only | add gobble, codex and kilo, or say the table is partial | each provider's `commandtable.go` | helper |
| R63 | `:753-759` | missing | C | mode rows for grok, opencode, fake only | add kilo, codex, gobble, and grok's synthesized `auto` | `internal/provider/grok/grok.go:65-70` | helper |
| R64 | `:459` | missing | C | catalog `source` lacks `saved` | `saved`: the last live catalog, served when the agent cannot answer | `internal/picker/picker.go:34-36` | helper |
| R65 | `:1015` | wrong | B | an oversize `name` is `bad_payload` | `session.create` truncates `name` to 256 bytes (rename rejects); `thinking_level`, `permission_profile_id`, `approvals_reviewer` are also capped | `internal/ws/server.go:1574-1591` | helper |
| R66 | `:316`, `:375` | missing | C | `provider` shown as required | optional: empty picks grok if ready, else fake if ready, else the first ready provider | `internal/ws/server.go:3555-3578` | helper |
| R67 | `:351`, `:767` | missing | C | `session.fork` lacks `last_turn_id` | the preferred alias of `message_id`; a conflicting pair is `bad_payload` | `internal/protocol/messages.go:1218-1224` | helper |
| R68 | `:770` | missing | C | `session.diff_result` lacks `base_sha`, `scope`, `truncated` | add them | `messages.go:1245-1252` | helper |
| R69 | `:340` | missing | C | `question.respond` row lacks `declined` | add it | `messages.go:1330` | helper |
| R70 | `:350` | missing | C | `commands.list` row lacks `session_id` | add it | `messages.go:1201-1207` | helper |
| R71 | `:1093` | missing | C | `provider.clear_credential` lacks `method_id` | optional; clears only that method | `messages.go:895-903` | helper |
| R72 | `:1101-1135` | missing | C | the auth block's upstream, method and input shapes | a schema sub-section for them | `messages.go:695-795` | helper |
| R73 | `:1267` | missing | C | `session.list_result` lacks `epoch`, `seqs` | add them (sent on v1 too) | `messages.go:613-617` | helper |
| R74 | `:1287-1352` | missing | C | Meta omits 11 fields | document `thinking_level`, `thinking_mutability`, `mode_id`, `collaboration_mode_id`, `permission_profile_id`, `approvals_reviewer`, `service_tier`, `personality`, `agent_session_aliases`, `handoff_nonce`, `updated_at` | `internal/session/manager.go:133-202` | helper |
| R75 | `:1929-1939` | missing | C | `session_capabilities` lacks `workspace_read`, `skill_refresh`, `shell` | add them | `internal/event/event.go:627-648` | helper |
| R76 | `:530-534` | missing | C | `agent_sessions` entries lack `preview`, `created_at`, `thinking_mutability` | add them | `internal/provider/provider.go:981-1031` | helper |
| R77 | `:541-552` | wrong | B | the `aggregate` example shows zero counts | zero counts are omitted; only `cost_usd` can be 0 | `internal/provider/provider.go:1377-1383` | helper |
| R78 | `:1537-1539`, `:1827-1833` | stale | B | `replace: true` arrives on snapshots | no provider sets it today (inferred by search); the contract stands | `internal/event/event.go:893-902` | helper |
| R79 | `:1642`, `:1649-1650` | stale | B | diagnostics carry `lsp` and `formatters` | no provider fills them today (inferred) | `internal/provider/opencode/acp_surfaces.go:33` | helper |
| R80 | `:136` | missing | C | oversize responses are "not enqueued" | the client gets `bad_payload` "outbound frame too large" | `internal/ws/server.go:3655-3666` | helper |
| R81 | `:203` | missing | C | `auth` failure codes | also `bad_version`, and `rate_limited` after 10 failures, then a close | `internal/ws/server.go:1170-1184` | helper |
| R82 | `:2650-2671` | missing | C | `/v1/hello` | returns `protocols`; needs the enrolled client key when enforced | `internal/ws/server.go:499-551` | helper |
| R83 | `:1570` | wrong | B | "8,000-character" tool-output limit | 8,000 bytes | `internal/provider/output/output.go:19-26` | checked |
| R84 | `:868-871`, `:898` | missing | C | history paging limits | page clamp 800; all sessions share a 384 MiB budget | `internal/session/manager.go:59-61`, `:82-85` | helper |
| R85 | `:2256-2275` | missing | C | the `codex` object lacks `from_model`, `to_model`, `reason` | add them | `internal/event/codex.go:18-27` | helper |
| R86 | `:2557-2558` | missing | C | receipts: only `allow_patterns` | `deny_patterns` also applies | `internal/config/config.go:73-75` | helper |
| R87 | `:312-358` | missing | C | the client-to-server table lacks `session.set_collaboration_mode`, `projects.list`, and the `codex.runtime`, `codex.doctor`, `codex.permissions` types | add rows (the last three link `protocol-v2.md:116-127`) | `internal/protocol/messages.go:162-167` | helper |

### Install, operations and fixture docs

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R88 | `docs/guides/ops-linux-install.md:87-88` | wrong | B | refused "before anything is downloaded" | refused with exit 1 before anything is installed | go-selfupdate-lib v1.26.2's `install.sh` template, lines 347-357 and 484-489 | helper |
| R89 | `docs/guides/ops-windows-install.md:10` | stale | B | "smoke-tested in CI on every push and tag" | built and unit-tested on every push (`go-native` windows/amd64); on a tag the release build runs each binary's `version` on Windows | `.github/workflows/ci.yml:187-240`; no smoke job (`ffd39d0b`) | checked |
| R90 | `ops-windows-install.md:447` | stale | B | drift row "smoke-native GH artifact download" | remove, or "release identity job (tag only)" | as R89 | helper |
| R91 | `ops-windows-install.md:86-90` | missing | C | `-Version` refusal cases | also exit 1, naming that release's own `install.ps1`, when its `SHA256SUMS` lacks the asset | library `install.ps1:572-575` | helper |
| R92 | `docs/guides/ops-macos-tcc.md:25` | wrong | B | one grant covers codex, grok, kilo, opencode | add gobble | as R02 | checked |
| R93 | `ops-macos-tcc.md:116-119` | missing | C | the ACP note names grok only | name the ACP providers (grok, gobble, opencode); whether each routes file and terminal work through the daemon is inferred | `internal/provider/acpagent/acpagent.go:1394-1395` | helper |
| R94 | `ops-macos-tcc.md:40-41` | wrong | B | `setup-service --from-installer` is the installer's last step | it is the step that prints the steps; the mcrelay hook runs after it | `internal/updateclient/selfupdate-release.json`; `internal/cli/service/advisories.go:33` | helper |
| R95 | `ops-macos-tcc.md:89-90` | wrong | B | `install-binary.sh` verification follows the `update` sentence | it is `make install`'s path; `update` installs through go-selfupdate-lib | `Makefile:317-323`; `scripts/install-binary.sh:243-244` | helper |
| R96 | `docs/guides/ops-credential-recovery.md:73-75` | wrong | owner *(2026-11-15, the owner's ruling "The code; fix it in its own record", relayed by the coordinator: leaves Phase B and is carried to the queued record that makes `main` map the errors to 2 and 3; the guide stays as written and is wrong until that record lands)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: this is queued record 1 of four, and the four records wait for the gobble branch work)* | exit codes 2 (usage, unknown provider or choice) and 3 (failure) | the process exits 1 on every error: `main` never calls `cli.ExitCode`, whose only caller is a test. A code fix (its own record) or a doc fix ("exit 1 on any failure") is the owner's call | `cmd/mcremote/main.go:30-42`; `internal/cli/auth_recovery.go:33` | checked |
| R97 | `docs/guides/ops-hardware-validation.md:347` | wrong | B | F6: a suspended socket lingers "≤60 s" | at most `limits.ws_read_deadline_seconds` (default 120 s); sooner if TCP keepalive (about 45 s) sees the peer stop | `internal/ws/liveness.go:50-56`, `:121-128`; `internal/config/config.go:568` | helper |
| R98 | `ops-hardware-validation.md:347` | wrong | B | "`resumed` in auth logs" | `resumed` is a field of the auth reply to the phone; check the client log | `internal/ws/server.go:1206-1216` | helper |
| R99 | `docs/guides/ops-codex-contract.md:151-153` | stale | B | "an identical 164 methods" | identical method sets (at 0.167.0, 202 stable and 277 experimental) | `internal/provider/codex/testdata/0.167.0/manifest.json` | helper |
| R100 | `ops-codex-contract.md:157-158` | stale | B | "all 22 experimental notifications" | every experimental notification (24 at 0.167.0, as `:81` says) | the same manifest | helper |
| R101 | `internal/provider/codex/testdata/0.167.0/README.md:59` | missing | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B, comment-only text)* | omits `../wire/0.152.1/` and `../wire/0.166.0/`, which tests replay | add them (a testdata file, outside the PLAN's docs scope) | `internal/provider/codex/runtime_passthrough_test.go:21` | helper |

### The mobile tree

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R102 | `apps/mobile/README.md:13` | wrong | B | picker lists grok, opencode, codex, kilo, fake | as R05; not-ready providers are listed disabled | `apps/mobile/lib/features/sessions/sessions_screen.dart:808-822` | helper |
| R103 | `apps/mobile/README.md:184` | wrong | B | link `../../docs/guides/mobile-profiling.md` is dead | `docs/guides/mobile-profiling.md` | the target is `apps/mobile/docs/guides/mobile-profiling.md` | checked |
| R104 | `apps/mobile/README.md:48` | stale | B | host and token "under Advanced" | **Paste URI / code / token**, or the Settings token dialog | `apps/mobile/lib/features/connect/connect_screen.dart:1488-1505` | helper |
| R105 | `apps/mobile/README.md:117` | wrong | B | "Logout (Disconnect)" | **Sign out**; the token stays until **Clear saved credentials** | `sessions_screen.dart:1186-1204` | helper |
| R106 | `apps/mobile/README.md:150-158` | wrong | B | `make apk` gives both outputs | `make apk` writes `app-release.apk`; `scripts/build-apk.sh` also copies it to `dist/` | `scripts/build-apk.sh:53-58`; `Makefile:549-567` | helper |
| R107 | `apps/mobile/README.md:28` | stale | B | "easiest on this host" | "Linux desktop (no Android emulator required)" | host wording | helper |
| R108 | `apps/mobile/README.md:1` | stale | B | title "(Android)" | Android and iOS (iOS verified on the Simulator) | `apps/mobile/README.md:130` | helper |
| R109 | `apps/mobile/README.md:136-143` | stale | B | `lib/` layout | `data/{chat,diagnostics,local,notifications,protocol,update,ws}`, `features/{chat,connect,sessions,settings,widgets}`, `app_lifecycle.dart` | `apps/mobile/lib/` | helper |
| R110 | `apps/mobile/docs/architecture.md:28` | wrong | B | control plane after `GET /v1/hello` | the app probes `GET /healthz` for path selection and opens `GET /v1/ws`; it never calls `/v1/hello` | `apps/mobile/lib/data/local/settings_store.dart:1271` (a comment only) | checked |
| R111 | `apps/mobile/docs/architecture.md:28-29` | wrong | B | "Debug builds may use cleartext `ws://`" | Android and iOS refuse `ws://` in every build mode; only Linux desktop may use it | `connect_screen.dart:1171-1190` | checked |
| R112 | `apps/mobile/docs/architecture.md:34-35` | wrong | B | provider list omits gobble | as R102 | as R05 | checked |
| R113 | `apps/mobile/docs/architecture.md:59` | stale | B | JDK 21 builds the APK (0168) | JDK 25 in CI and on dev hosts (0169); bytecode stays Java 17 | `.github/workflows/ci.yml:482-486` | helper |
| R114 | `apps/mobile/docs/guides/ops-android-emulator.md:22-28` | stale | B | direct Gradle needs JDK 21; Gradle 9.1 | JDK 25, Gradle 9.3.1 (whether 9.3.1 rejects JDK 26 is unverified) | `apps/mobile/android/gradle/wrapper/gradle-wrapper.properties` | helper |
| R115 | `ops-android-emulator.md:63` | wrong | B | `listen: tailscale` | `listen.host: tailscale` | `internal/config/config.go:190` | checked |
| R116 | `ops-android-emulator.md:59`, `:87` | wrong | owner | names a tailnet address as the daemon's observed address (value not repeated) | `<tailnet-ip>` | published already | helper. *2026-11-15: closed by the PLAN's entry "Identifiers in two published guides — 2026-11-15".* |
| R117 | `apps/mobile/docs/guides/ops-android-signing.md:105-107` | stale | B | an update "is expected to preserve pairing" | it does: hardware rows E1 and E2 passed on 2026-08-03 | `docs/guides/ops-hardware-validation.md:302-303` | helper |
| R118 | `apps/mobile/docs/guides/standards/mobile/android.md:25-26` | wrong | B | "the Makefile's Android targets" set `GRADLE_OPTS` | `make apk`, `make profile-apk`, `make manifest-surface` do; `make profile` does not | `Makefile:513` | helper |
| R119 | `standards/mobile/networking.md:34-36` | wrong | B | the accept-all probe is "diagnostic only" | it is the unauthenticated `/healthz` reachability probe that drives path selection | `apps/mobile/lib/data/ws/transport_probes.dart:69-90` | helper |
| R120 | `standards/mobile/architecture.md:16-24` | stale | B | `lib/` tree lacks `app_lifecycle.dart` | add it | `apps/mobile/lib/app_lifecycle.dart` | helper |
| R121 | `standards/mobile/*.md` front matter | stale | B | `last_updated` older than the last edit | the date of each file's last content change | `git log -1` per file | helper |
| R122 | `standards/mobile/architecture.md:36-45`; `dart.md:23-25` | wrong | owner *(2026-11-15, the owner's ruling "Fix R130 and R147 here; drop the unused rules", relayed by the coordinator: now B; the two rules no code follows are dropped from the two mobile standards)* | rules no code follows: `AsyncNotifier` with `AsyncValue.guard()`, and extension types | keep the rules as rules, or drop them (0239 left the first to the owner) | no use in `apps/mobile/lib` | checked |

### The Go standards and the process text

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R123 | `docs/guides/standards/go/{cli,concurrency,config,core,logging,network,session,testing}.md:3` | stale | B | `version: "1.26.5-vN"`; `last_updated` older than the last edit | `1.27.4-vN`, as the standards README; update `last_updated` | `go.mod:3`; `docs/guides/standards/go/README.md:3` | own |
| R124 | `standards/go/core.md:48` | stale | B | "Go 1.22 – 1.26+" | Go 1.22 – 1.27 | `go.mod:3` | helper |
| R125 | `standards/go/README.md:55` | wrong | B | `[Configuration](../../config.md)` opens the operator guide | `[Configuration](config.md)` | the link target | checked |
| R126 | `standards/go/README.md:22-23`; `core.md:32` | wrong | B | `netgo,osusergo` "on Linux only" | every target but Darwin and Windows | `Makefile:92-110` | helper |
| R127 | `standards/go/testing.md:28-31` | wrong | B | live tags list omits gobble | add `live_gobble` (`make live-gobble`) | `Makefile:402-403` | checked |
| R128 | `AGENTS.md:51` | wrong | B | four live targets | name every `live-*` target, or point to the README's Development table | `Makefile:158`, `:362`, `:366` | own |
| R129 | `.opencode/rules.md:36`; `.kilo/agent/madr-and-plan-skill.md:41` | wrong | B | records at `docs/NNNN-MADR-*` | `docs/decisions/NNNN-MADR-*` / `docs/decisions/NNNN-PLAN-*` | `AGENTS.md:75`, `:110` | own |
| R130 | `AGENTS.md:63`, `:83` | wrong | owner *(2026-11-15, the owner's ruling "Fix R130 and R147 here; drop the unused rules", relayed by the coordinator: now B; `AGENTS.md:63` and `:83` name all five rule files)* | the per-agent pointers and the bootstrap exception name three rule files; `.codex/rules/` and `.kilo/agent/` also carry the gate | name all five (OQ5 as applied allows only the three lines in the PLAN's Scope) | the five rule files | own |

### Package doc comments (Phase D)

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R131 | `internal/provider/gobble/doc.go:10-11` | stale | D | "built unregistered at MADR 0179 PLAN P2; P4 adds its Descriptor" | drop: gobble is registered | `internal/provider/registered/registered.go:21` | own |
| R132 | `internal/provider/httpagent/httpagent.go:14-16` | wrong | D | OpenCode is the first dialect; cites `docs/0011-MADR-…` | kilo is the one dialect; OpenCode runs over acpagent; records live in `docs/decisions/` | only `internal/provider/kilo` imports httpagent | own |
| R133 | `internal/provider/stderrtail/stderrtail.go:5-6` | wrong | D | "httpagent for kilo and opencode" | acpagent for grok, gobble and opencode; httpagent for kilo | as R132 | own |
| R134 | `internal/wirecap/wirecap.go:5-6` | wrong | D | kilo and opencode stream SSE | kilo streams SSE; grok, gobble and opencode speak ACP | as R132 | own |
| R135 | `internal/appdirs/product.go:1-3` | stale | D | "on Linux and Darwin" | and Windows Known Folders | `internal/appdirs/roots_windows.go` | own |
| R136 | `internal/cli/service/setup.go:1-2` | stale | D | systemd `--user` and launchd only | also runit, s6, OpenRC and the Windows task | `internal/cli/service/detect.go:19-31` | own |
| R137 | `internal/ws/server.go:1` | stale | D | "the mcremote.v1 WebSocket control plane" | protocol v1 and v2 | `internal/protocol/messages.go:1-2` | own |
| R138 | the baseline | missing | D | 4 package comments, 5 methods, 548 contract fields; also stale field comments `internal/protocol/messages.go:366-368` (`agent` is Kilo's, not OpenCode's) and `:1027` (`source` lacks `saved`), and `internal/cli/service/installerenv.go:15` (cites a removed `scripts/install.sh`) | comment each per D's rules | the tables above | own, helper |

### Outside the PLAN's file scope (owner)

*(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: Phase B may fix comment-only text, with no behaviour change, in `configs/*.yaml`, the codex fixture README, the lint config's header, and the comments in the `Makefile`, the CI workflow and the script a row names. The setup-service seed templates and the deploy files get their own queued record. Row by row: Phase B, comment-only: R101, R139 to R141 (the `configs/*.yaml` part), R144, R145 (the three comments), R148. Queued setup-service seed record: the seed copies of R139 to R141, and R143's comments. Queued deploy-files record: R34, and R35's question about a writable state directory. Queued record of its own for the placeholder host secret (ruling "Its own record"): R143's placeholder. Not placed, stopped for the owner: R142 (adds a key, not comment-only) and R146 (`.PHONY` is not comment text), and the two echoed messages at `.github/workflows/ci.yml:541` and `:546`, which R145 did not list. R147 is ruling 7's, in Phase B. The PLAN's execution record, "The owner's rulings after Phase A — 2026-11-15", has the options.)*

*(2026-11-15, later, the owner's ruling "Yes, all three, by amendment", relayed by the coordinator: R142, R146 and the two echoed paths of R145 are no longer open: Phase B makes them as three named exceptions to comment-only text.)*
*(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: the queued records are four (R96; the placeholder host secret; the seed templates and deploy files with the system unit's state-directory question; receipts tail truncation), and they wait for the gobble branch work.)*

| ID | Where | Kind | Dest | Defect | Replacement fact | Evidence | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R139 | `configs/config.example.yaml:46`; `config.prod.example.yaml:48`; `config.mesh-grok.yaml:50` | wrong | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B for the three `configs/*.yaml` comments; the seed copies go to the queued setup-service seed record)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: the seed copies are in queued record 3 of four, which waits for the gobble branch work)* | `max_retries: 0 # 0 => AWS SDK default` | `0` = the Route 53 provider's default, five attempts (as `docs/guides/config.md:108`) | libdns/route53 v1.6.2's `client.go`, lines 142-153 | own |
| R140 | `configs/config.example.yaml:337`; `config.prod.example.yaml:334`; `config.mesh-grok.yaml:342` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B for the three `configs/*.yaml` comments; the seed copies go to the queued setup-service seed record)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: the seed copies are in queued record 3 of four, which waits for the gobble branch work)* | kilo known-good 7.4.22 | 7.8.18 | `internal/provider/kilo/version.go:25` | own |
| R141 | `configs/config.example.yaml:397-398`; `config.prod.example.yaml:376-377`; `config.mesh-grok.yaml:384-385` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B for the three `configs/*.yaml` comments; the seed copies go to the queued setup-service seed record)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: the seed copies are in queued record 3 of four, which waits for the gobble branch work)* | empty `pair.advertise_host` = "Tailscale IPv4, else loopback" | follows the bind, as `docs/guides/config.md:210` says | `internal/cli/pair.go:615-633` | own |
| R142 | `configs/mcrelay.example.yaml:71-85` | missing | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: first stopped, not placed: adding a key is not comment-only text)* *(2026-11-15, later, the owner's ruling "Yes, all three, by amendment", relayed by the coordinator: now B, as a named exception to comment-only: Phase B adds the key at its default, read from the code)* | lacks `limits.max_conns`, the one mcrelay key it omits | add it with `MCRELAY_LIMITS_MAX_CONNS` | `internal/relay/fileconfig.go:162`, `:255` | own |
| R143 | `internal/cli/service/defaults_mcremote.yaml:301`, `:362`; `defaults_mcrelay.yaml:43` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: the seed comments go to the queued setup-service seed record)* *(2026-11-15, the owner's ruling "Its own record", relayed by the coordinator: the placeholder host secret goes to its own queued MADR; nothing in 0248 changes the seed's placeholder)* *(2026-11-15, later, the owner's ruling "After the gobble branch work", relayed by the coordinator: the seed comments are in queued record 3 of four and the placeholder host secret is queued record 2 of four; both wait for the gobble branch work)* | the seeds carry R139-R141's comments; the mcrelay seed's placeholder host has a 22-character secret that passes validation (it binds loopback) | the same corrections; the placeholder is a separate security question | `defaults_mcrelay.yaml:55-56` | own |
| R144 | `.markdownlint-cli2.jsonc:2` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B, comment-only text)* | "MagicTools MCP fleet READMEs" | this repository's docs (D4 keeps the file unchanged) | the file | own |
| R145 | `Makefile:153`, `:570`; `.github/workflows/ci.yml:510` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B for the comments at `Makefile:153`, `:570` and `.github/workflows/ci.yml:510`; two echoed messages at `ci.yml:541` and `:546` are not comments and were stopped on)* *(2026-11-15, later, the owner's ruling "Yes, all three, by amendment", relayed by the coordinator: the two echoed paths at `.github/workflows/ci.yml:541` and `:546` are now B, as named exceptions to comment-only)* | cite `docs/guides/mobile-profiling.md` and `docs/guides/ops-android-signing.md` | `apps/mobile/docs/guides/…` | the moved guides | own |
| R146 | `Makefile:362`, `:366` and `codesign-maybe` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: first stopped, not placed: `.PHONY` is not comment text)* *(2026-11-15, later, the owner's ruling "Yes, all three, by amendment", relayed by the coordinator: now B, as a named exception to comment-only: the three names join `.PHONY`)* | three targets defined but not `.PHONY` | add them to `.PHONY` | `Makefile:158-161` | own |
| R147 | `scripts/check_records.py:167-180` | missing | owner *(2026-11-15, the owner's ruling "Fix R130 and R147 here; drop the unused rules", relayed by the coordinator: now B, as an extension of an existing check, with a dated exception to MADR D4)* | `apps/mobile/README.md` links are not checked, so R103 went unnoticed | add it to the unnumbered set (a check change; D4) | the function | own |
| R148 | `scripts/codex-contract.ps1:147` | stale | owner *(2026-11-15, the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator: now B, comment-only text)* | "164 methods" | as R99 | the script | helper |

## Phase F rows

So that Phase F is mechanical, these are the exact rows it adds.

README "Operator / product" table (`README.md:1641-1654`), after the `chat-performance.md` row:

```markdown
| [docs/guides/ops-linux-install.md](docs/guides/ops-linux-install.md) | Linux and macOS install, service backends, upgrade, uninstall |
| [docs/guides/ops-windows-install.md](docs/guides/ops-windows-install.md) | Windows install, the logon task, upgrade, uninstall |
| [docs/guides/ops-macos-tcc.md](docs/guides/ops-macos-tcc.md) | macOS Full Disk Access and browser-downloaded binaries |
| [docs/guides/ops-credential-recovery.md](docs/guides/ops-credential-recovery.md) | Inspect and choose a provider's credential backup with `auth-recovery` |
| [docs/guides/ops-codex-contract.md](docs/guides/ops-codex-contract.md) | Move the Codex pin and its app-server contract |
| [docs/guides/ops-hardware-validation.md](docs/guides/ops-hardware-validation.md) | Device and network validation runs |
| [apps/mobile/docs/guides/ops-android-emulator.md](apps/mobile/docs/guides/ops-android-emulator.md) | Run the app on an Android emulator |
| [apps/mobile/docs/guides/ops-ios-signing.md](apps/mobile/docs/guides/ops-ios-signing.md) | iOS signing and the Simulator build |
```

README "Architecture & key decisions" table, after the 0075 row:

```markdown
| [docs/decisions/0179-MADR-gobble-native-acp-provider.md](docs/decisions/0179-MADR-gobble-native-acp-provider.md) | gobble provider over native ACP |
| [docs/decisions/0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](docs/decisions/0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md) | OpenCode v2 over ACP, with a lease-bound control plane |
```

`docs/README.md` "I want to…" table, before the "find why a decision was made" row:

```markdown
| look up a setting, flag or environment variable | [config.md](guides/config.md) |
| run mcrelay in production, rotate its secrets or read its logs | [ops-mcrelay.md](guides/ops-mcrelay.md) |
| give the daemon Route 53 rights for a Let's Encrypt certificate | [iam-route53-acme.md](guides/iam-route53-acme.md) |
| recover an agent's sign-in from a backup | [ops-credential-recovery.md](guides/ops-credential-recovery.md) |
| move the Codex pin to a new release | [ops-codex-contract.md](guides/ops-codex-contract.md) |
| validate a build on real devices and networks | [ops-hardware-validation.md](guides/ops-hardware-validation.md) |
| understand how the chat screen stays smooth on long streams | [chat-performance.md](guides/chat-performance.md) |
| run the app on an Android emulator | [ops-android-emulator.md](../apps/mobile/docs/guides/ops-android-emulator.md) |
| profile the app on Android | [mobile-profiling.md](../apps/mobile/docs/guides/mobile-profiling.md) |
```

Phase E adds its own `CHANGELOG.md` rows. The Go and mobile standards are reached through the standards links already in
the README and the mobile docs index, and need no row.

## Totals

145 rows. The numbers R20, R25 and R43 are unused.

| By kind | Rows |
| --- | ---: |
| wrong | 59 |
| missing | 48 |
| stale | 38 |

| By destination | Rows |
| --- | ---: |
| B | 70 |
| C | 44 |
| D | 8 |
| F | 3 |
| owner | 20 |

Two rows also carry a part for the owner beside their phase: R35 (C) and R42 (B). Release class: 145 older, 0
release-caused.

*(2026-11-15, after the owner's rulings of that day, relayed by the coordinator; the table above is kept as written. By destination now: B 83 (the 70 above, plus R48, R53, R54, R101, R122, R130, R139 to R141, R144, R145, R147 and R148); C 44; D 8; F 3; open for the owner 2 (R142 and R146); closed on 2026-11-15 by the PLAN's entry "Identifiers in two published guides — 2026-11-15" 2 (R40 and R116); carried to a queued record 3 (R34, R96 and R143). The total is still 145. R35 keeps its C part and sends its other part to the queued deploy-files record, and R42 keeps its B part and waits on the experiment for its other part. R145 also has two echoed messages that are open.)*

*(2026-11-15, later, after the owner's rulings "Fix the code first; guide waits", "Yes, all three, by amendment" and "After the gobble branch work", relayed by the coordinator; this note supersedes the one above for the counts. By destination: B 85 (adds R142 and R146); C 44; D 8; F 3; open for the owner 0; closed on 2026-11-15 by the PLAN's entry "Identifiers in two published guides — 2026-11-15" 2 (R40 and R116); carried whole to a queued record 3 (R34, R96 and R143). The total is still 145. Two rows are split: R35 keeps its C part and sends its system-unit part to queued record 3, and R42 keeps its B part ("walks backward") and sends the truncation part to queued record 4. The four queued records: 1, the `auth-recovery` exit codes (R96); 2, the seed's placeholder host secret (R143); 3, the stale text of the seed templates and deploy files, with the system unit's state-directory question (R34, R35's unit part, the seed copies of R139 to R141, R143's comments); 4, receipts tail-truncation detection (R42). The PLAN's execution record, "Further rulings of the owner — 2026-11-15", lists them and the eight address lines left open for the owner.)*

## Findings for the owner

Nothing below was changed. Each is a row above, with its evidence.

| Finding | Rows | Kind |
| --- | --- | --- |
| `mcremote auth-recovery` exits 1 on every error; the guide's exit codes 2 and 3 cannot happen, because `main` never maps them | R96 | doc-versus-code; a code fix needs its own record *2026-11-15: the owner's ruling "The code; fix it in its own record", relayed by the coordinator. Carried to a queued record; Phase B does not touch R96.* *Queued record 1, which waits for the gobble branch work (ruling "After the gobble branch work").* |
| `agent_sessions.list` is answered only by codex; the guide says the ACP providers serve it | R53 | doc-versus-code *2026-11-15: the owner's ruling "Docs follow the code, all three", relayed by the coordinator. Now Phase B.* |
| no provider emits `diagnostics_changed`; the guide says kilo does | R54 | doc-versus-code *2026-11-15: ruling "Docs follow the code, all three". Now Phase B.* |
| the host-side `usage_update` dedup was lost when OpenCode moved to ACP; only kilo dedups | R48 | doc-versus-code *2026-11-15: ruling "Docs follow the code, all three". Now Phase B.* |
| receipts: tail truncation is not detectable from the chain, and the guide says truncation is detected | R42 | doc-versus-code (inferred) *2026-11-15: the owner's ruling "Prove it first, then decide", relayed by the coordinator. Phase B corrects the "walks backward" sentence only; the truncation sentence waits for the experiment and the owner's next ruling.* *2026-11-15, later: the owner's ruling "Fix the code first; guide waits", relayed by the coordinator. The experiment ran and tail truncation is not detected; queued record 4 makes it detectable. The guide's two sentences stay as written and are false until it lands.* |
| `headscale.md` and `ops-android-emulator.md` name a resolver address, a Headscale user name, a tailnet address and a live host's state | R40, R116 | identifiers in published docs. *2026-11-15: the owner chose "Placeholders now, own commit"; closed by the PLAN's entry "Identifiers in two published guides — 2026-11-15". History is not rewritten, and the values are the owner's to add to the disclosure guard's deny list.* |
| the example configs, the setup-service seeds, a codex fixture README, the mcrelay system unit and plist, the lint config header, CI and Makefile comments, and a script carry stale text outside the PLAN's file scope | R34, R35, R101, R139-R146, R148 | a scope amendment, or separate records *2026-11-15: the owner's ruling "Comments into this plan; the rest get records", relayed by the coordinator. Placed row by row in the note above the totals; R142 and R146 are open.* *2026-11-15, later: ruling "Yes, all three, by amendment" makes R142, R146 and the two echoed paths of R145 Phase B's; ruling "After the gobble branch work" holds queued record 3 until the gobble branch work.* |
| the mcrelay seed's placeholder host secret passes validation | R143 | a security question *2026-11-15: the owner's ruling "Its own record", relayed by the coordinator. A queued MADR decides it.* *Queued record 2, which waits for the gobble branch work (ruling "After the gobble branch work").* |
| `check_records.py` does not check `apps/mobile/README.md`'s links | R147 | a check change, which D4 excludes *2026-11-15: the owner's ruling "Fix R130 and R147 here; drop the unused rules", relayed by the coordinator. Now Phase B, as an extension of an existing check.* |
| two mobile standards prescribe what no code does (`AsyncNotifier`, extension types) | R122 | a rule to keep or drop *2026-11-15: ruling "Fix R130 and R147 here; drop the unused rules". The two rules are dropped, in Phase B.* |
| `AGENTS.md:63` and `:83` name three of the five rule files | R130 | process text beyond OQ5's three lines *2026-11-15: ruling "Fix R130 and R147 here; drop the unused rules". Now Phase B.* |
