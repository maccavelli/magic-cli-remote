---
status: accepted
date: 2026-09-18
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Remove Goose CLI support from the product, including its ACP-over-HTTP transport

## Context and Problem Statement

The owner wants Goose CLI support removed from this product in its entirety:
the provider, its transport, its config, its tests, its live suite, and the
living documentation that still presents Goose as a selectable agent.

Goose is not a thin flag. It is a first-class provider (`internal/provider/goose`,
`provider.IDGoose = "goose"`), enabled by default, driven through a dedicated
shared-engine transport (`internal/provider/acphttp`) that no other agent
imports. Leaving that transport behind as "maybe useful later" would keep a
second ACP stack, a second engine supervisor, and several thousand lines of
tests compiling for a binary the product no longer ships.

This record is the inventory and the cut. It does not implement.

**Revision 2026-09-18 (still `proposed`).** A second, measured pass over the
2026-09-17 draft found that its leftover-config decision (old D3: refuse to
load) would stop the daemon on every host `setup-service` ever provisioned,
because the product itself wrote `providers.goose` into those configs (F16), and
that the capture mechanism it prescribed cannot see two of the four leftover
shapes (F17). D3 is reversed to *warn and ignore*. The pass also found the
draft's file inventory incomplete (F22), its phase order unable to compile
(F18), its historical-stamp rule inapplicable to 7 of 10 files and wrong for
MADR 0073 (F20), and its verification commands unrunnable on this host (F21).
Findings F16–F23 and decisions D13–D14 are new; D3, D9 and D10 are rewritten;
every other finding was re-verified and corrected where a line number or count
had drifted.

### What was measured, not assumed

All on this tree at `b3d3355`, 2026-09-17 (reading) and 2026-09-18 (reading,
probes, and driving a `go build ./cmd/mcremote` binary from the scratchpad).
Host: Windows 11 Home 10.0.26200, go1.26.6, Git Bash. No live `goose` binary is
installed (`which goose` → not found); none was needed — the question is what
*this* codebase wires, not what upstream Goose speaks. `~/.config/goose/config.yaml`
does exist on this host (doctor reports `present: yes`).

**The provider exists as a real dialect over a real transport.**
`internal/provider/goose/goose.go` is a spec package: `Provider = acphttp.Provider`,
`New`/`NewWithLogger` construct `acphttp.New(newSpec(...), cfg.Config)`, and
the serve argv is fixed as `serve --host 127.0.0.1 --port PORT --dangerously-unauthenticated`
plus repeatable `--with-builtin` flags. `provider.IDGoose` is declared at
`internal/provider/provider.go:69-70`. The daemon registers it when
`cfg.Providers.Goose.Enabled` (`internal/daemon/daemon.go:247-262`), after
reconciling Goose's keyring setting (`:251`).

**`acphttp` has no other production consumer.**
`git grep -l 'internal/provider/acphttp"' -- '*.go'` hits six files:
`internal/daemon/daemon.go` and five under `internal/provider/goose/`
(`goose.go`, `goose_test.go`, `live_test.go`, `live_model_test.go`,
`live_purge_test.go`). Grok speaks ACP over stdio through `acpagent`. OpenCode
and Kilo speak HTTP+SSE through `httpagent`. Codex has its own JSON-RPC
app-server adapter. There is no fourth ACP-over-HTTP agent waiting to inherit
the package.

Size (`wc -l`, all files including blank lines and `testdata/`):

| Tree | Files | Lines | Role |
| --- | --- | --- | --- |
| `internal/provider/goose/` | 17 | 2,302 | dialect: spec, catalog (pinned vendor table), auth, keyring, command table, live tests, wire fixture `testdata/wire/1.48.0` |
| `internal/provider/acphttp/` | 20 | 6,301 | transport: engine lifecycle, WS JSON-RPC, session mapping, catalog harvest, Goose-specific file-log tail |

(The 09-17 draft's ~2,140 / ~5,820 came from PowerShell `Measure-Object -Line`,
which skips blank lines. Same trees, different counter.)

The three third-party modules these trees import (`github.com/coder/acp-go-sdk`,
`github.com/coder/websocket`, `github.com/google/uuid`) each have other
importers (26, 39 and 18 files respectively, `git grep -l`), so **no `go.mod`
line becomes orphaned**; `go mod tidy` should produce no diff.

`acphttp` is not a clean generic leftover. `internal/provider/acphttp/engine_log_tail.go:33`
hard-codes `filepath.Join(state, "goose", "logs", "cli")`, and `:40` gates the
tail on `p.spec.ID != provider.IDGoose`. The transport has already grown a
Goose-only side path (MADR 0122).

**Config treats Goose as a default-on agent, not an opt-in experiment.**
`GooseProviderConfig` embeds `ACPProviderConfig` and adds `with_builtins`,
`stream_coalesce_ms`, and `keyring_disabled` (`internal/config/config.go:544-570`).
Defaults set `Enabled: true`, `Bin: "goose"`, `KeyringDisabled: true`,
`StreamCoalesceMs: 80` (`config.go:834-850`). Viper registers
`providers.goose.*` defaults (`internal/config/load.go:323-333`), which is the
*only* reason `MCREMOTE_PROVIDERS_GOOSE_*` env vars resolve: `AutomaticEnv`
resolves only keys Viper already knows (`load.go:71-76` says so of the retired
OpenCode key). Validation is at `config.go:1153-1180`; cwd resolution at
`load.go:232-233`. `KnownProviderIDs` includes `"goose"`
(`internal/config/prewarm_write.go:27`, with `case "goose"` arms at `:47` and
`:66`). Four YAML files ship a full `goose:` block: `configs/config.example.yaml`,
`configs/config.mesh-grok.yaml`, `configs/config.prod.example.yaml`,
`internal/cli/service/defaults_mcremote.yaml:75-88`.

**The product wrote `providers.goose` into operator configs (new, F16).**
`defaults_mcremote.yaml` is `//go:embed`ded (`internal/cli/service/setup.go:33-34`)
and `ensureDefaultConfig` (`setup.go:1058-1127`) writes it to the operator's
config path when no file exists, and "never overwrites" one that does. The
`goose:` block has been in that template since `66d6f1e` (2026-07-27). So every
host provisioned by `setup-service` since then holds a product-written
`providers.goose` block with `enabled: true`, and upgrading the binary does not
remove it. **Measured on this host:** `mcremote paths` reports
`config_file: C:\Users\macsm\AppData\Roaming\mcremote\config.yaml`, and that
file has `goose:` at line 75 with `enabled: true` beneath it; `mcremote doctor`
reports the Task Scheduler service `present: yes, active: yes`.

MADR 0019's retired `providers.opencode.transport` is not the same situation:
that key was never in the template (`config.go:705-706`: the template omits
"keys that never applied: no `transport`"), so only operators who had written it
by hand could trip the refusal.

**What refusing would do to self-update [inferred from code, not run].**
`mcremote update` swaps the binary, reconciles the unit, restarts, and polls
`Lifecycle.WaitHealthy` (`internal/updateclient/lifecycle.go:89-128`), which
treats "the service reports active" as healthy. The systemd unit is
`Type=simple`, `Restart=always`, `RestartSec=5`
(`mcremote.user.service.tmpl:21,30-31`). A daemon that exits at config load is
therefore "active" for the moment between fork and exit, and "activating" while
it waits to restart. Depending on which state the 30 s poll samples, the update
either rolls back or commits a binary that crash-loops until
`StartLimitBurst` stops it. Neither is acceptable for a change the operator did
not configure.

**How Viper sees a leftover `goose` key once its defaults are gone (new
probe, F17).** A throwaway program in the scratchpad, built against this
repository's `go.mod` (viper v1.21.0, same `SetEnvPrefix("MCREMOTE")`,
key replacer and `AutomaticEnv` as `load.go:38-40`), with a
`RetiredGoose map[string]any \`mapstructure:"goose"\`` capture field — the
09-17 draft's prescription — and no `providers.goose.*` default:

```text
absent     capture=nil          IsSet=false InConfig=false
null       capture=nil          IsSet=false InConfig=false   # "providers:\n  goose:\n"
emptymap   capture=nil          IsSet=true  InConfig=true    # "goose: {}"
disabled   capture={enabled:false}  IsSet=true InConfig=true
populated  capture={bin:goose enabled:true} IsSet=true InConfig=true
env-only   capture=nil          IsSet=false InConfig=false   # MCREMOTE_PROVIDERS_GOOSE_ENABLED=true
```

So: a `map` capture misses `goose: {}`; `v.InConfig("providers.goose")` sees
every *non-null* file shape; nothing Viper-level sees a bare null `goose:`; and
an env-only leftover is invisible to Viper entirely and can be found only by
scanning `os.Environ()` for the `MCREMOTE_PROVIDERS_GOOSE_` prefix. A null
`goose:` carries no settings, so missing it loses nothing.

**Tests couple the template, the examples and the struct (new, F18).**
`internal/cli/service/template_parity_test.go` holds three relevant tests:
`TestTemplateProviderKeysMatchExample` (`:26-65`) fails if a provider appears
in the template but not `configs/config.example.yaml` or vice versa;
`TestTemplateTopLevelKeysMatchExample` (`:191-207`) does the same for every
flattened key; `TestTemplatesSpellEveryConfigKey` (`:213-253`) walks
`config.Config`'s mapstructure tags (`walkConfigKeys`, `:135-166`) and requires
every non-struct key — maps included — in all four YAML files unless it is in
`omittedConfigKeys` (`:84-88`, which today holds `providers.opencode.transport`,
`providers.goose.args`, `providers.goose.fs_roots`). Consequences: the template
and `config.example.yaml` must lose their `goose:` blocks in the *same commit*;
and any struct field tagged `goose` would become a required key in all four
files unless listed as omitted.

**Goose-only credential and keyring code sits inside a shared package.**
`internal/provider/credstore` is used by Grok, Codex, OpenCode, Kilo, doctor,
and the WS server. Goose-specific exported symbols:

* paths: `GooseConfigPath` (`credstore.go:83`), `GooseSecretsPath` (`:93`)
* reads: `ReadGooseConfig` (`:338`), type `GooseConfig` (`:322`), `GooseKeyringDisabled` (`:109`), `ReadGooseSecretNames` (`write.go:227`)
* writes: `SetGooseActiveProvider` (`write.go:178`), `SetGooseSecret` (`:267`), `DeleteGooseSecret` (`:284`), `SetGooseKeyringDisabled` (`:491`)
* errors: `ErrGooseKeyringManaged` (`write.go:220`), `ErrGooseKeyringOperatorOwned` (`:473`)
* marker: `GooseKeyringMarker` (`write.go:468`)

**Unexported helpers that die with them (new, F19).** A caller map of every
unexported `credstore` function: `splitYAMLScalar` is called only from
`GooseKeyringDisabled`, `ReadGooseConfig` and the two Goose writers;
`readGooseSecrets`, `writeGooseSecrets`, `gooseKeyringLine` and
`gooseKeyringDisabledValue` only from Goose code; `isFalsey` (`credstore.go:160`)
has **zero** callers today (only comments and a test comment mention it).
`write.go`'s `yaml "go.yaml.in/yaml/v3"` import is used only at `:252` and
`:297`, both inside `readGooseSecrets`/`writeGooseSecrets` — so removing those
without removing the import is a compile error. `xdg`, `writeFileAtomic`,
`escapeTOML` and the Grok helpers stay in use.

Tests: `goose_keyring_test.go` and `goose_keyring_parity_test.go` (whole files);
`credstore_test.go` `TestReadGooseConfig*` (3 funcs, `:79-157`); `write_test.go`
`TestSetGooseActiveProvider*` (2 funcs, `:207-256`).

Doctor probes Goose's config (`internal/cli/doctor.go:79-88`). The WS server
maps `credstore.ErrGooseKeyringManaged` to `protocol.ErrKeyringManaged`
(`internal/ws/server.go:2699-2700`) and a comment at `:2274` cites "goose's
keyring".

**The `keyring_managed` string has Goose as its only producer, in two roles.**
It is an error code (`protocol.ErrKeyringManaged`, `internal/protocol/errors.go:175-178`,
registered at `:316`) and an auth-method unavailability reason
(`protocol.AuthReasonKeyringManaged`, `internal/protocol/messages.go:781`).
`git grep` for both outside the two deleted packages finds no other producer.
Phone copy is `apps/mobile/lib/data/ws/mc_exception.dart:47-49` and names
"`goose configure`"; `apps/mobile/test/friendly_op_error_test.dart:10-12` asserts
only that the copy contains `keyring` and `host`. `internal/ws/method_availability_test.go:21,33`
uses the reason as a synthetic fixture and needs no change.

**Cross-provider Go tests import the Goose package by name.**

* `internal/command/conformance_test.go:14,55` — Goose is one of six `command.Tabler`s
* `internal/provider/auth_conformance_test.go:47-52` — Goose is the catalog-without-device-OAuth case
* `internal/chunkbuf/provider_mode_test.go:41-42` — reads `../provider/acphttp/session.go` as "goose (acphttp)"
* `internal/daemon/goose_keyring.go` / `goose_keyring_test.go` — production + 4 tests

Goose-named config tests: `acp_config_test.go` `TestGooseWithBuiltinsParseAndValidate`
(`:100`), `TestGooseWithBuiltinsRejectsEmptyAndDuplicate` (`:116`);
`config_test.go` `TestDefaultsGooseKeyringDisabled` (`:1112`),
`TestGooseKeyringDisabledExplicitFalse` (`:1124`),
`TestGooseKeyringDisabledAbsentKeepsDefault` (`:1142`),
`TestGooseKeyringDisabledEnvOverride` (`:1161`), plus the `{"goose", …}` row in
the prewarm table at `config_test.go:625`.

Many other tests use `"goose"` only as fixture *data* (session ids, mode lists,
provider picker rows, fake provider ids). Those encode generic behaviour
discovered via Goose (dangerous `auto` as a default, large catalogs,
keyring-managed methods) and must be retargeted, not deleted.

**The full inventory is larger than the 09-17 plan listed (new, F22).**
`git ls-files | grep -v '^docs/spec/' | xargs grep -il goose` → **130 files**,
37 of them inside the two deleted trees. Of the remaining 93, the 09-17 plan's
scope omitted 22 that still contain Goose text (all comments or fixture
strings, none compile-breaking):

* `internal/chunkbuf/chunkbuf.go:111`, `internal/chunkbuf/toollane_mode_test.go:32`
* `internal/procutil/reap.go:11`
* `internal/provider/cwd.go:46` (names `acphttp`)
* `internal/provider/acpagent/acpagent.go:124`, `automode_test.go:142`, `session.go:35,153,1554,2023`, `sessioncaps.go:18,24-25`, `subagents.go:83`, `subagents_test.go:232`, `version.go:14`
* `internal/provider/codex/provider.go:571`, `session.go:2317`, `tool_lane_baseline_test.go:17`
* `internal/provider/httpagent/currentmodel_test.go:58`, `supervise_wiring_test.go:64` (names `acphttp`)
* `internal/provider/kilo/lifecycle.go:122`, `lifecycle_test.go:61`
* `internal/provider/opencode/lifecycle.go:122`, `upstream.go:22-23`
* `apps/mobile/lib/state/transcripts_notifier.dart:107` (names `acphttp/session.go`)

One hit is not Goose at all: `internal/provider/codex/testdata/wire/0.152.1/frames.jsonl`
contains the path string `/home/user/gitrepos/goose` inside a captured Codex
`config.toml` trust table. It is a wire capture and stays byte-identical.

**Live suite and Makefile.**
Three files are `//go:build live_goose`: `live_test.go`, `live_model_test.go`,
`live_purge_test.go`. `Makefile:159` lists `live-goose` in `.PHONY`;
`Makefile:351-355` is the comment and target. `AGENTS.md:132` lists
`-tags live_goose ./...` next to the other live agents. `.github/workflows/` and
`scripts/` contain no Goose reference; CI never runs the live tag.

**Mobile has no Goose enum.** Provider ids are strings from `providers.list`
(MADR 0026). The phone stops showing Goose when the daemon stops advertising
it. What has to be edited: the manifest entry
`'goose': 'assets/vendor_icons/goose.svg'`
(`apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart:46`),
`apps/mobile/assets/vendor_icons/goose.svg` (the asset is bundled by directory,
`pubspec.yaml:75`, so no pubspec edit), `tools/vendor-icons/ids.txt:83`, the
`sync.sh:5-8` input comment, 9 test files and 6 lib files that name Goose. No
`map.json` entry and no other manifest entry points at `goose.svg`. The rest of
`ids.txt` is a union of Goose's pinned vendor table *and* live OpenCode/Kilo
catalogs; those vendor logos stay. `sync.sh` regenerates every icon from a
pinned npm package, so rerunning it is a network-dependent operation that can
rewrite unrelated SVGs.

**An old Goose session on the phone shows a raw string (new, F23).**
`friendlyOpError` (`mc_exception.dart:31-75`) has no `unknown_provider` case, so
it falls through to `e.message`, and the daemon's message for a resume/create
against a missing provider is the literal `"unknown provider"`
(`internal/ws/server.go:2149,2352,2500,2534,2669`).

**`agenterr` classifiers were written against Goose logs and are not Goose-only.**
`ExtractText` pulls JSON `fields.message` and Rust Debug `String("…")` bodies
(`internal/agenterr/agenterr.go:351-384`). `LooksLikeLongBackoff` matches
`Backing off for 3600s` and `retry_delay: Some(3600s)`. Tests use live Goose
1.45/1.48 *shapes* (`agenterr_test.go:234-319`); the fixture strings themselves
contain no "goose" text. The same helpers are documented as feeding Codex/Grok
stderr and OpenCode `session.status` retries (`agenterr.go:315-316`). Deleting
the matchers would weaken remaining providers' silent-hang abort.

**Default provider selection does not prefer Goose.**
`defaultProviderID` prefers ready Grok, else Fake, else the first ready
registered provider (`internal/ws/server.go:3408-3432`); no other WS handler
branches on a provider id except `codex_handlers.go:152`. Removing Goose does
not change the happy-path default, and retargeting a test fixture to
`opencode` exercises no special branch.

**Two different "Goose"s share a name in this repository.**
`AGENTS.md:67` ("Registered there for claude, grok, goose, opencode, kilo and
agy") and `AGENTS.md:148` (commit-message rule across agent environments,
"Claude, Codex, OpenCode, Grok, and Goose") name Block's Goose *as a coding
agent that works on this repo*. `AGENTS.md:132` (`-tags live_goose`) is the
product provider.

**Durable sessions with `provider: goose` will not resume after the ID is
gone.** `provider.Registry.Get` returns `unknown provider %q`
(`internal/provider/registry.go:34`); `session.Manager.Create`
(`internal/session/manager.go:988`) and the WS handlers map it to
`unknown_provider`. The store does not delete rows when a provider disappears.
No code auto-purges transcripts by provider id.

**Historical records: shape and status (new, F20).**
Goose-topic records under `docs/spec/`, with their current status form:

| File | Status form today |
| --- | --- |
| `0025-MADR-goose-provider.md` | legacy bullet `- **Status**: **Implemented** …`, no YAML |
| `0025-PLAN-goose-provider.md` | legacy `**Status**: Implementation-ready …`, no YAML |
| `0026-MADR-mobile-goose-support.md` | legacy bullet, already "Superseded by MADR 0030" |
| `0030-MADR-goose-remote-parity.md` | legacy bullet `Accepted — …` |
| `0030-PLAN-goose-remote-parity.md` | legacy `**Status:** Accepted — …` |
| `0073-MADR-goose-prompt-hang-and-debug-pass.md` | legacy bullet `Findings recorded …` |
| `0110-MADR-goose-keyring-prompts-block-headless-launch.md` | YAML `status: accepted` |
| `0110-PLAN-goose-keyring-prompts-block-headless-launch.md` | prose `Plan status: **implemented 2026-08-21.**` |
| `0122-MADR-deterministic-goose-file-log-tail-attach.md` | YAML `status: accepted` |
| `0122-PLAN-deterministic-goose-file-log-tail-attach.md` | YAML `status: completed` |

Only 3 of the 10 have a YAML `status:` key to change. No `0073-PLAN-*` exists.
The PLAN status vocabulary (`proposed | in-progress | completed | abandoned`)
has no `superseded` value — that form is MADR-only. And **0073 is not a Goose-only
record**: its title includes a "codebase debug pass", and live non-Goose code
cites it as rationale — `internal/provider/codex/provider.go:571` and
`session.go:2317` (shared quota path, "MADR 0073 F1"),
`internal/provider/kilo/lifecycle.go:122` and `opencode/lifecycle.go:122`
("parity with goose/codex/grok — MADR 0073"). Stamping it superseded would tell
a reader those decisions are void.

Dozens of later records mention Goose as one of several agents (0023, 0028,
0029, 0043, 0044, 0069, 0074, 0083, 0086, 0089, 0095, …). Those decisions still
bind the remaining providers.

**Living product docs still sell Goose.** Case-insensitive hit counts:
`README.md` 24, `docs/config.md` 23, `docs/protocol-v1.md` 13 (including
`:1297`, which names "the acphttp transport"), `docs/ops-macos-tcc.md` 1,
`docs/ops-android-emulator.md` 1, `apps/mobile/README.md` 1,
`configs/config.example.yaml` 14, `configs/config.mesh-grok.yaml` 3,
`configs/config.prod.example.yaml` 2. `docs/agent_cli_slash_commands_matrix.md`
(6) is a 2026-07-25 survey, not operator docs. `docs/config.md` has no
retired-keys section today.

**Shared ACP config is not Goose-only.** `ACPProviderConfig` is embedded by
Grok (`config.go:509-511`) and its comment still says "grok today; goose and
codex next" (`config.go:473`); the same phrase is at `daemon.go:722`.
`mcp_servers` is configured for both Grok and Goose in the example YAML. Grok
keeps MCP. Codex never took the "next" slot.

**What the host can and cannot verify (new, F21).**

* `rg` was **not installed** on this host when this pass began (`which rg` →
  not found), so every `rg` command in the 09-17 draft failed here. `git grep`
  (2.53.0) is available everywhere the repository is.
* `flutter` and `dart` were **not installed** on this host, nor in its
  `Ubuntu-24.04` WSL distro.
* **Update, later on 2026-09-18:** the owner had the toolkit installed on both
  sides of this host — Flutter 3.47.2 / Dart 3.13.2 (the CI `FLUTTER_VERSION`),
  JDK 17, the Android SDK, and `rg` 15.2.0. Measured then: `pubspec.lock`
  reproduces unchanged, `dart format` changes 0 files, `flutter analyze` reports
  no issues, `flutter test` passes 1415, and a release arm64 APK builds, on
  Windows and in WSL alike. wonder was moved to Flutter 3.47.2 the same day and
  passes the same gates. This host is therefore now a Flutter host, and the
  mobile phase can run here. The commands still use `git grep`, now because it
  searches exactly the tracked files on every host, which the allow-list in D14
  depends on; `rg` also searches untracked files.
* `go test -race` works here (`CGO_ENABLED=1`, `CC=gcc`; `go test -race
  ./internal/picker/` → `ok`).
* **Baseline `go test ./...` is not green on this host:** 42 packages `ok` (of 48; 5 have no test files), one
  `FAIL` — `internal/config` `TestLoadDisplayNameUnset`
  (`DisplayName="mac420-laptop", want empty`). Cause: the test isolates
  `XDG_CONFIG_HOME` (`config_test.go:220`), but on Windows the default config
  path is the Known Folder `%APPDATA%\mcremote\config.yaml`, which ignores XDG,
  so `config.Load(config.LoadOptions{})` reads this host's live config (which
  sets `display_name: "mac420-laptop"` at line 36). Ten tests call
  `Load(LoadOptions{})` with no file (`acp_config_test.go:91`;
  `config_test.go:54,68,85,107,181,223,673,1069,1163`). Every one of them reads
  the live config on a Windows host — **the same config that holds the
  product-written `goose:` block.** This is a pre-existing test-isolation defect,
  independent of Goose; it is recorded here because it constrains how this
  work's tests may be written and what "green" means on this host.

### Findings

**F1 — Goose is a default-on product provider, not a hidden extra.** Removing
it is a user-visible surface change: the phone picker loses an entry and the
example configs lose a block.

**F2 — `internal/provider/acphttp` is Goose's transport and has no other
importer.** Deleting the dialect and keeping the transport leaves 6,301 lines
plus a Goose-hard-coded log tail compiling for nobody. The git history of
MADR 0025 is the reuse path if a future ACP-over-HTTP agent appears. No
`go.mod` dependency is orphaned by the deletion.

**F3 — Viper ignores unknown keys, and env leftovers resolve only for
registered keys.** Once `GooseProviderConfig` and its defaults are gone, a
leftover `providers.goose` block loads silently and `MCREMOTE_PROVIDERS_GOOSE_*`
is not even read. Unlike MADR 0019's case, silence here cannot *change the
behaviour* of a surviving provider — Goose is gone either way — so the risk is
an operator not knowing why, not a silent behaviour switch.

**F4 — `credstore` is shared; only its Goose API is not.** Deleting the package
would break Grok/Codex/OpenCode/Kilo auth. Surgical removal is required, and
"surgical" includes the unexported helpers of F19.

**F5 — `keyring_managed` is a protocol string with Goose as its only
producer**, as both an error code and an auth-method reason. Removing the
producer is required. Removing the string from the protocol in the same change
is a separate, breaking-for-mixed-upgrade choice.

**F6 — Generic tests used Goose as fixture data.** Mode-danger, large-catalog,
and keyring-unavailable tests encode protocol behaviour that still exists,
including compatibility with *older daemons* that still advertise Goose.
Deleting them because they say "goose" would drop coverage.

**F7 — `agenterr` Goose-shaped matchers still serve other engines.** Keep the
classifiers and fixture strings; rename comments/tests so they cite the
shape's provenance by MADR number, not as a current integration.

**F8 — Durable Goose sessions become unresumable, not missing.** Resume fails
with `unknown_provider`. Auto-deleting transcripts would be data loss the
owner did not ask for.

**F9 — Vendor icons are a union, not a Goose dump.** Drop the Goose *agent*
brand only. Regenerating via `sync.sh` is network-dependent and can rewrite
unrelated icons; a hand edit of one manifest line is exact.

**F10 — "Goose" in AGENTS.md is two things.** Only the `live_goose` tag
(`:132`) is product. `:67` and `:148` are the developer coding agent.

**F11 — Historical MADRs must not be rewritten or deleted.** Product claims
move to this record.

**F12 — The mobile client is provider-id-generic.** No Flutter enum of agents
needs deleting. Icon, copy, comments and tests that *name* Goose still do.

**F13 — CI does not run `live_goose`.** Removing the Makefile target and the
tagged tests cannot redden GitHub CI.

**F14 — Orphan `goose serve` processes are already reaped by ownership
markers, not by binary name** (`daemon.go:173-185`, `mcremote engines --reap`).
Removal needs no special Goose killer. Help text that lists `goose` as an
engine kind (`internal/cli/engines.go:18`, measured via `mcremote engines
--help`) does.

**F15 — `ACPProviderConfig`, `acpagent`, Grok `mcp_servers`, and Fake stay.**
Comments that say "goose and codex next" are stale and are corrected, not used
as a reason to delete the type.

**F16 — The product itself wrote `providers.goose` into operator configs.**
`setup-service` seeded `goose: enabled: true` into every config it created
since 2026-07-27 and never rewrites an existing file; this host's live config
has it at line 75. Refusing to load such a config would stop the daemon on
every provisioned host, and under `mcremote update` would either roll the
update back or commit a crash-looping daemon. The 0019 refusal precedent
does not transfer: that key was never product-written.

**F17 — A struct capture field cannot see every leftover shape.** Measured: a
`map[string]any` capture misses `goose: {}` and bare `goose:`;
`v.InConfig("providers.goose")` sees every non-null file shape; an env-only
leftover is invisible to Viper and needs an `os.Environ()` prefix scan; a
bare null `goose:` is invisible to everything and carries nothing.

**F18 — The template, `config.example.yaml`, and the struct must change in one
commit.** Three parity tests tie them together, and a struct field tagged
`goose` would become a required key in all four YAML files. A phase plan that
edits the template in one commit and the examples in another cannot pass
`go test` in between.

**F19 — `credstore` has Goose-only unexported code, and one dead function.**
`splitYAMLScalar`, `readGooseSecrets`, `writeGooseSecrets`, `gooseKeyringLine`,
`gooseKeyringDisabledValue` die with the Goose API; `isFalsey` is already
uncalled; `write.go`'s yaml import must go or the package will not compile.

**F20 — The historical stamp rule of the 09-17 draft does not fit the files.**
7 of 10 Goose-topic records have no YAML status; PLANs cannot carry
`superseded by`; and MADR 0073 is a mixed record cited by live Codex, Kilo and
OpenCode code.

**F21 — The draft's verification could not run as written on this host.** No
`rg` and no Flutter when measured (both installed later the same day; see
"What the host can and cannot verify"), and baseline `go test ./...` already
has one Windows-only failure caused by tests reading the live `%APPDATA%`
config — the same file that carries the leftover `goose:` block. The baseline
failure still stands.

**F22 — The draft's scope list missed 22 files.** All are comments or fixture
strings, but the owner asked for removal "in its entirety", and a completion
check that greps for Goose will find them.

**F23 — The phone has no copy for `unknown_provider`.** A user who taps an old
Goose session sees the raw string "unknown provider".

## Decision Drivers

* **Completeness** — after execution, the tree has no Goose provider, no
  `acphttp` package, no live-goose target, and a *mechanical* check (an exact
  allow-list of files that may still say "goose") proves it.
* **An upgrade must not break a host the product configured** — the binary
  must start, and `mcremote update` must succeed, on a config that
  `setup-service` wrote (F16).
* **Leftovers are visible, not fatal** — an operator whose YAML or environment
  still names Goose is told, once per start and in `mcremote paths`, that the
  setting is ignored and can be deleted.
* **No collateral damage** — Grok/OpenCode/Kilo/Codex/Fake, `credstore` for
  those agents, `agenterr` classifiers, the protocol error-code registry, and
  developer-Goose docs stay.
* **No user-data deletion** — transcripts, operator config files, and
  `~/.config/goose/` are the operator's.
* **History stays history** — record bodies are not rewritten to pretend Goose
  was never supported; mixed records are not stamped.
* **Every commit compiles and tests green** — relative to the measured
  baseline (F21), not to an idealised one.

## Considered Options

Overall approach:

* **A — Delete the dialect, the transport, and every living product mention;
  warn on leftover config; mark Goose-only records superseded** (chosen)
* **B — Default `providers.goose.enabled: false` and leave the code**
* **C — Delete `internal/provider/goose/` but keep `acphttp` as an unused
  generic transport**
* **D — `//go:build` the Goose provider out of default builds, keep sources**

Leftover-config handling (a sub-choice inside A, reopened by F16/F17):

* **L1 — Refuse to load** (the 09-17 draft's D3; MADR 0019 pattern)
* **L2 — Load, ignore, and warn via log + `Diagnostics`** (chosen)
* **L3 — Rewrite the operator's config to strip the block** during
  `setup-service --refresh` or at daemon start

## Decision Outcome

**Chosen: A with L2 — complete surgical removal, including `acphttp`; a
leftover `providers.goose` is ignored with a warning, never refused.**

B keeps the maintenance cost the owner asked to end. C keeps 6,301 lines and a
Goose-hard-coded log tail for a hypothetical future agent; git history is the
cheaper reuse path. D is B with extra build-tag complexity.

L1 would take down every `setup-service`-provisioned host on upgrade (F16),
and its prescribed mechanism would still miss `goose: {}` and env-only
leftovers (F17). L3 fixes the file but makes the daemon an editor of the
operator's config on a path (refresh, start) that has never written it;
`setup-service` deliberately never overwrites. L2 costs nothing at upgrade,
tells the operator exactly what to delete, and — because Goose is gone either
way — hides no behaviour change.

### The decisions

**D1 — Stop shipping Goose as a product agent.** Remove `provider.IDGoose`,
daemon registration, `GooseProviderConfig`, its Viper defaults and validation,
the `KnownProviderIDs` entry and both `case "goose"` arms, the doctor probe,
all four YAML `goose:` blocks, `make live-goose`, and `-tags live_goose` from
`AGENTS.md`. After this, `providers.list` never contains `goose`.

**D2 — Delete `internal/provider/goose/` and `internal/provider/acphttp/` in
the same commit.** They are one unit. A future ACP-over-HTTP agent starts from
git history / MADR 0025.

**D3 — Ignore a leftover Goose config, and say so (L2).** At load:

* detect `v.InConfig("providers.goose")` (any non-null file shape) and any
  `os.Environ()` entry whose name starts with `MCREMOTE_PROVIDERS_GOOSE_`;
* on detection, emit one `slog` `Warn` and append one
  `appdirs.Diagnostic{Code: "retired_provider_goose"}` to `cfg.Diagnostics` —
  the MADR 0155 `config_not_owner_only` pattern (`load.go:525-527`) — with a
  message that names the source (file key or env var names), says the setting
  is ignored, says it can be deleted, and cites MADR 0160;
* never return an error for it; the load otherwise proceeds unchanged;
* add **no** struct field, **no** `SetDefault`, and **no** `BindEnv` for
  `providers.goose` (F17: none is needed for detection; F18: a struct field
  would become a required template key);
* do not rewrite the operator's config file or unset the env var.

A bare null `goose:` is not detected and not warned about; it carries no
settings (F17).

**D4 — Strip Goose from `credstore`; keep the package.** Delete the exported
symbols listed in the measurement, the unexported helpers of F19 (including
the already-dead `isFalsey`) and `write.go`'s yaml import, the two Goose
keyring test files and the Goose test functions in `credstore_test.go` /
`write_test.go`, the doctor Goose probe, `internal/daemon/goose_keyring*.go`,
and the WS mapping from `ErrGooseKeyringManaged`. Do not read or write
`~/.config/goose/` after this. Do not delete that directory.

**D5 — Leave durable `provider: goose` sessions on disk.** List them. Resume
and create fail through the existing `unknown_provider` path. No purge, no
rewrite of stored provider ids.

**D6 — Keep `keyring_managed` in the protocol and on the phone as a reserved
generic string.** Keep both `protocol.ErrKeyringManaged` and
`protocol.AuthReasonKeyringManaged`, their registration, and
`docs/protocol-v1.md`'s entry. Remove the only producer. Rewrite the phone
sentence so it does not say `goose configure` and still contains `keyring` and
`host` (`friendly_op_error_test.dart:11-12`). A new phone talking to an old
daemon still decodes the code; a new daemon never emits it.

**D7 — Keep `agenterr` classifiers and fixture strings.** Reword comments to
describe the log *shape* and cite MADR 0073 for provenance; rename
`TestExtractTextGooseJSON` to `TestExtractTextStructuredJSON`.

**D8 — Retarget generic tests; do not delete them.** Command and auth
conformance drop the Goose row. Mode-danger, catalog-size, keyring-unavailable,
and legacy-daemon-compat tests switch to a remaining agent id or a synthetic
id and keep every assertion. `chunkbuf` drops the `acphttp/session.go` case.

**D9 — Mark Goose-only records superseded, additively, in the form each file
already uses.** For the two MADRs with YAML frontmatter (0110, 0122): set
`status: superseded by 0160-MADR-remove-goose-cli-support.md` and update
`date:`. For every other Goose-only record — MADRs 0025, 0026, 0030 and PLANs
0025, 0030, 0110, 0122 — insert one fixed banner line directly under the H1 and
change nothing else (PLAN status lines stay as they are: a PLAN's status
records whether it ran, and these did). **Do not mark 0073**: it is mixed (F20).
Do not mark any other mixed record. Leave
`docs/agent_cli_slash_commands_matrix.md` as a dated survey.

**D10 — Drop the Goose agent brand icon by hand.** Delete `goose.svg`, delete
`ids.txt:83`, delete manifest line 46 by hand (do **not** rerun `sync.sh`,
F9), and fix `sync.sh`'s input comment so it no longer cites
`internal/provider/goose/catalog.go`.

**D11 — Do not touch developer-Goose documentation.** `AGENTS.md:67` and
`:148` stay.

**D12 — Shared ACP types stay.** `ACPProviderConfig`, `acpagent`, Grok
`mcp_servers`, Fake. Correct the "goose and codex next" comments at
`config.go:473` and `daemon.go:722`.

**D13 — The phone explains an unresumable session.** Add an
`unknown_provider` case to `friendlyOpError` whose copy says the agent for
this session is not available on the host, with a widget/unit test. This is
the user-visible face of D5.

**D14 — "Removed in its entirety" is a mechanical check, not a judgement.**
After execution, a case-insensitive `git grep -il goose` over the tree,
excluding `docs/spec/`, returns exactly the allow-list in Confirmation. Living
code and docs that need provenance cite a MADR number, not the agent name.

### Consequences

* Good, because the product surface, the extra ACP stack, the pinned vendor
  catalog, and the macOS keyring special case all leave together.
* Good, because an upgraded binary starts on every existing config, and
  `mcremote update` is unaffected by a config the product wrote (F16).
* Good, because a leftover is reported in the daemon log and in
  `mcremote paths --json` `diagnostics`, the same channel operators already
  use for the config-permission warning.
* Good, because Grok/OpenCode/Kilo/Codex keep `credstore`, `agenterr`, MCP on
  Grok, and the protocol strings they share.
* Good, because completion is a grep with a fixed answer (D14), which an
  executor cannot satisfy by judgement.
* Bad, because a leftover `providers.goose` block stays in operator files
  indefinitely, warning at every start until someone deletes it. Accepted:
  noise is recoverable; a daemon that will not start is not.
* Bad, because operators with Goose sessions lose the ability to resume them
  from this product. Accepted: F8; D5 keeps the transcripts and D13 explains
  the failure. Goose's own CLI still works against `~/.config/goose/`.
* Bad, because a future ACP-over-HTTP agent must rebuild `acphttp` from
  history. Accepted: F2.
* Neutral, because the mobile phase needs a Flutter 3.47.2 host. When first
  measured, the Windows host where the Go phases run was not one (F21); since
  the later 2026-09-18 install it is, natively and in WSL.
* Neutral, because `keyring_managed` remains a protocol string with no
  producer (D6).
* Neutral, because historical MADRs still mention Goose, and 0073 is not
  stamped. That is the point of D9.
* Neutral, because `TestLoadDisplayNameUnset` stays red on Windows hosts whose
  live config sets `display_name`. It is pre-existing and out of scope; this
  work must not make it worse and must not add another test with the same
  defect.

### Confirmation

All commands run from the repository root in Git Bash. They use `git grep`
because it searches exactly the tracked files on every host, which the D14
allow-list depends on (F21).

```bash
# 1. The two packages are gone and nothing imports them.
test ! -e internal/provider/goose && test ! -e internal/provider/acphttp && echo OK
git grep -n -e 'internal/provider/goose"' -e 'internal/provider/acphttp"' -- '*.go'
#   → no output

# 2. D14 — the exact allow-list. Output must equal these paths, no more, no fewer:
git grep -il goose -- . ':!docs/spec'
#   AGENTS.md                                                (developer Goose, :67 and :148 only)
#   README.md                                                (one design-table row linking 0160)
#   docs/agent_cli_slash_commands_matrix.md                  (dated survey)
#   docs/config.md                                           (one retired-key row)
#   internal/config/retired_goose.go                         (D3 detection)
#   internal/config/retired_goose_test.go                    (D3 tests)
#   internal/provider/codex/testdata/wire/0.152.1/frames.jsonl  (path string in a wire capture)

# 3. Build, vet, tests — relative to the measured baseline.
go build ./... && go vet ./...
go test ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'
#   → on a Windows host whose live config sets display_name: only
#     "--- FAIL: TestLoadDisplayNameUnset" and the internal/config FAIL line;
#     elsewhere: no output
go test -race ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'          # same rule
go mod tidy && git diff --exit-code go.mod go.sum               # → exit 0

# 4. D3 — a leftover warns and loads, driven through the real binary.
T=$(mktemp -d)                         # Git Bash leaves $TMPDIR unset
go build -o "$T/mcremote.exe" ./cmd/mcremote
printf 'log:\n  level: info\n' > "$T/base.yaml"
printf 'providers:\n  goose:\n    enabled: true\n' > "$T/leftover.yaml"
"$T/mcremote.exe" paths --json --config "$T/leftover.yaml"
#   → exit 0; "diagnostics" contains code "retired_provider_goose" whose message cites 0160
MCREMOTE_PROVIDERS_GOOSE_ENABLED=true "$T/mcremote.exe" paths --json --config "$T/base.yaml"
#   → exit 0; same diagnostic code, message names MCREMOTE_PROVIDERS_GOOSE_ENABLED
"$T/mcremote.exe" paths --json --config "$T/base.yaml"
#   → exit 0; no retired_provider_goose diagnostic
cp "$APPDATA/mcremote/config.yaml" "$T/live-copy.yaml"   # a real product-seeded config (F16)
"$T/mcremote.exe" paths --json --config "$T/live-copy.yaml"
#   → exit 0; retired_provider_goose present

# 5. Build surface.
make -n live-goose            # → "No rule to make target"
git grep -n live_goose        # → no output outside docs/spec

# 6. Protocol string kept (D6).
git grep -n '"keyring_managed"' -- internal/protocol
#   → errors.go and messages.go constants still present

# 7. Phone (on a Flutter host).
cd apps/mobile && dart format --output=none --set-exit-if-changed lib test \
  && flutter analyze && flutter test
test ! -e apps/mobile/assets/vendor_icons/goose.svg

# 8. Historical records (D9).
git grep -n '^status:' -- docs/spec/0110-MADR-*.md docs/spec/0122-MADR-*.md
#   → both "superseded by 0160-MADR-remove-goose-cli-support.md"
git diff --stat <base>.. -- docs/spec/0073-MADR-goose-prompt-hang-and-debug-pass.md
#   → no change

# 9. Windows gate, before calling the work done.
make ci-windows
```

## Pros and Cons of the Options

### A — Complete surgical removal, including `acphttp` (chosen)

* Good, because it matches the request: Goose is gone as a product agent,
  and so is the transport that existed only to drive it (F2).
* Good, because every commit can be specified as "compiles and tests green
  relative to baseline", which is checkable.
* Good, because completion is a fixed-answer grep (D14).
* Bad, because it is a wide diff (dialect + transport + config + credstore +
  CLI + mobile + living docs). Accepted: completeness over a sequence of
  "disabled but still there" releases.
* Bad, because `acphttp` is not trivial to rewrite. Accepted: git history.

### B — Default enabled false, keep the code

* Good, because it is a one-line default plus docs, and operators could turn
  Goose back on.
* Bad, because the owner asked for removal in its entirety, not a hide.
* Bad, because the pinned catalog, keyring reconcile, live suite, and second
  ACP stack keep rotting behind a flag.
* Bad, because it would not even hide Goose on existing hosts: their
  product-seeded configs say `enabled: true` explicitly (F16), so a new default
  changes nothing there.

### C — Delete the dialect, keep `acphttp`

* Good, because a future ACP-over-HTTP CLI could plug a new spec into an
  existing engine supervisor.
* Bad, because `acphttp` already hard-codes Goose log paths (F2). It is not
  the generic package the name suggests.
* Bad, because 6,301 lines would still compile, still need `chunkbuf`
  pairing, and still confuse the next reader about whether Goose is "sort of"
  supported.

### D — Build-tag the provider out of default builds

* Good, because sources remain greppable and a tag could still compile them.
* Bad, because config, credstore, and protocol mapping would still need a
  story for the tagged build — most of A's surface with none of the cleanup.

### L1 — Refuse to load a leftover `providers.goose`

* Good, because it is the house precedent (MADR 0019) and the loudest possible
  signal.
* Good, because an operator can never be confused about whether Goose is
  still configured.
* Bad, because the product wrote the key into every provisioned config (F16):
  this is a fleet-wide start failure on upgrade, including on this host.
* Bad, because under `mcremote update` it either rolls back or commits a
  crash-looping daemon, depending on poll timing.
* Bad, because the prescribed map capture misses `goose: {}` and env-only
  leftovers (F17), and would itself become a required template key (F18).
* Bad, because on Windows ten existing config tests read the live host
  config (F21) and would all fail on any provisioned developer host.

### L2 — Ignore and warn (chosen)

* Good, because nothing that worked before the upgrade stops working except
  Goose itself.
* Good, because it reuses an existing, operator-visible channel
  (`slog` + `cfg.Diagnostics` → `mcremote paths --json`).
* Good, because detection needs no struct field, default or env binding (F17).
* Bad, because the warning repeats at every start until the operator edits the
  file. Accepted.
* Bad, because a bare null `goose:` is not reported. Accepted: it configures
  nothing.

### L3 — Strip the block from the operator's config automatically

* Good, because the file converges to the supported shape and the warning goes
  away on its own.
* Good, because `internal/config/prewarm_write.go` already round-trips the
  config through `yaml.Node` with comments preserved, so the mechanism exists.
* Bad, because it makes the daemon (or the refresh child) an editor of the
  operator's config on a path that has never written it, and `setup-service`
  deliberately never overwrites an existing config.
* Bad, because a config passed by `--config` may be read-only or shared
  (mesh examples), and a failed rewrite would need its own error policy.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Goose dialect is a thin `acphttp.Spec` | `internal/provider/goose/goose.go` |
| `IDGoose` declared | `internal/provider/provider.go:69-70` |
| Daemon registers Goose when enabled, after keyring reconcile | `internal/daemon/daemon.go:247-262` |
| Only six Go files import `acphttp` | `git grep -l 'internal/provider/acphttp"' -- '*.go'` |
| Tree sizes 17 files/2,302 lines and 20 files/6,301 lines | measured: `find … -type f \| xargs wc -l` |
| No `go.mod` dependency orphaned | measured: `go list -f '{{join .Imports "\n"}}'` on both packages; `git grep -l` counts 26/39/18 |
| Goose log tail hard-coded in `acphttp` | `internal/provider/acphttp/engine_log_tail.go:33,40` |
| Goose enabled by default | `internal/config/config.go:834-850` |
| Viper `providers.goose.*` defaults | `internal/config/load.go:323-333` |
| `AutomaticEnv` resolves only known keys | `internal/config/load.go:71-76` (comment on the retired OpenCode key) |
| 0019 capture pattern; template never carried `transport` | `internal/config/config.go:577-581`, `:705-706`, `:1182-1190` |
| Template is embedded and seeded, never overwritten | `internal/cli/service/setup.go:33-34`, `:1058-1127` |
| Template has carried `goose:` since 2026-07-27 | `git log -S'  goose:' -- internal/cli/service/defaults_mcremote.yaml` → `66d6f1e` |
| This host's live config has `goose: enabled: true` | measured: `mcremote paths`; `%APPDATA%\mcremote\config.yaml:75-76` |
| This host's service is installed and running | measured: `mcremote doctor` → `present: yes`, `active: yes` |
| Update health = "service active"; unit is `Type=simple`, `Restart=always` | `internal/updateclient/lifecycle.go:89-128`; `internal/cli/service/mcremote.user.service.tmpl:21,30-31` |
| Viper detection matrix for leftover shapes | measured: scratchpad probe against repo `go.mod` (viper v1.21.0) |
| Parity tests couple template, examples and struct | `internal/cli/service/template_parity_test.go:26-65`, `:84-88`, `:135-166`, `:191-253` |
| `KnownProviderIDs` includes goose | `internal/config/prewarm_write.go:27,47,66` |
| credstore Goose exported API | `internal/provider/credstore/credstore.go:83,93,109,322,338`; `write.go:178,220,227,267,284,468,473,491` |
| credstore Goose-only helpers; `isFalsey` uncalled; yaml import only in Goose funcs | measured: caller map of unexported funcs; `write.go:13,252,297` |
| WS maps Goose keyring error to protocol | `internal/ws/server.go:2699-2700`; comment `:2274` |
| `keyring_managed` as error code and auth reason | `internal/protocol/errors.go:175-178,316`; `internal/protocol/messages.go:781` |
| Phone copy names `goose configure`; test asserts `keyring`/`host` | `apps/mobile/lib/data/ws/mc_exception.dart:47-49`; `apps/mobile/test/friendly_op_error_test.dart:10-12` |
| Phone has no `unknown_provider` copy; daemon sends raw string | `apps/mobile/lib/data/ws/mc_exception.dart:31-75`; `internal/ws/server.go:2149,2352,2500,2534,2669` |
| Command conformance imports Goose | `internal/command/conformance_test.go:14,55` |
| Auth conformance Goose row | `internal/provider/auth_conformance_test.go:47-52` |
| chunkbuf pins `acphttp/session.go` | `internal/chunkbuf/provider_mode_test.go:41-42` |
| Goose-named config tests | `internal/config/acp_config_test.go:100,116`; `config_test.go:625,1112,1124,1142,1161` |
| 130 tracked files mention Goose outside `docs/spec` | measured: `git ls-files \| grep -v '^docs/spec/' \| xargs grep -il goose` |
| 22 files missed by the 09-17 scope | measured: that list minus the 09-17 plan's in-scope list |
| Codex fixture's "goose" is a path string | `internal/provider/codex/testdata/wire/0.152.1/frames.jsonl` |
| live_goose tests and target | `internal/provider/goose/live_*.go`; `Makefile:159,351-355`; `AGENTS.md:132` |
| CI YAML and scripts have no Goose reference | `git grep -il goose -- .github scripts` → none |
| Doctor Goose probe | `internal/cli/doctor.go:79-88`; measured `mcremote doctor` output |
| Engines help names goose | `internal/cli/engines.go:18`; measured `mcremote engines --help` |
| Default provider prefers Grok, not Goose | `internal/ws/server.go:3408-3432` |
| Registry unknown provider error | `internal/provider/registry.go:34`; `internal/session/manager.go:988` |
| Vendor icon union; icon bundled by directory; single manifest entry | `tools/vendor-icons/sync.sh:5-8`; `apps/mobile/pubspec.yaml:75`; `vendor_icon_manifest.g.dart:46`; `ids.txt:83` |
| AGENTS.md live tag vs developer Goose | `AGENTS.md:67,132,148` |
| agenterr Goose-shaped classifiers also used elsewhere | `internal/agenterr/agenterr.go:315-329,351-384` |
| Historical record status forms; no `0073-PLAN` | measured: `head` of each file; `ls docs/spec \| grep -i goose` |
| 0073 cited by live non-Goose code | `internal/provider/codex/provider.go:571`; `codex/session.go:2317`; `kilo/lifecycle.go:122`; `opencode/lifecycle.go:122` |
| PLAN status vocabulary has no `superseded` | `madr-and-plan-writing` skill, PLAN frontmatter rules |
| `rg`, `flutter`, `dart` absent at first; race works | measured: `which`; WSL `Ubuntu-24.04` `command -v`; `go test -race ./internal/picker/` |
| Later 2026-09-18: Flutter 3.47.2 gates pass on Windows, WSL and wonder; APK builds on Windows and WSL | measured: `flutter --version`; `flutter pub get` + `git diff --stat pubspec.lock` (empty); `dart format`/`flutter analyze`/`flutter test` (+1415); `flutter build apk --release --target-platform android-arm64` |
| Baseline `go test ./...`: 42 ok of 48 packages, 1 FAIL `TestLoadDisplayNameUnset` | measured 2026-09-18 at `b3d3355` |
| Ten tests load the default config path | `internal/config/acp_config_test.go:91`; `config_test.go:54,68,85,107,181,223,673,1069,1163` |
| Diagnostic pattern to copy | `internal/config/load.go:453-455,525-527`; `internal/cli/paths.go:48,69` |

### Related records

* [0025-MADR-goose-provider.md](0025-MADR-goose-provider.md) — introduced the
  dialect and `acphttp`. Marked superseded by D9.
* [0030-MADR-goose-remote-parity.md](0030-MADR-goose-remote-parity.md) —
  remote-parity bar for Goose. Marked superseded by D9.
* [0073-MADR-goose-prompt-hang-and-debug-pass.md](0073-MADR-goose-prompt-hang-and-debug-pass.md)
  — mixed record; **not** marked (F20). Its F1 quota path still binds Codex,
  Kilo and OpenCode.
* [0110-MADR-goose-keyring-prompts-block-headless-launch.md](0110-MADR-goose-keyring-prompts-block-headless-launch.md)
  — `keyring_disabled` reconcile. Leaves with D4; marked superseded.
* [0122-MADR-deterministic-goose-file-log-tail-attach.md](0122-MADR-deterministic-goose-file-log-tail-attach.md)
  — Goose-only `acphttp` log tail. Leaves with D2; marked superseded.
* [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md)
  — the refuse-on-leftover pattern. Considered as L1 and **not** followed, for
  the reason in F16.
* [0155-MADR-one-daemon-guards-its-config-the-other-does-not.md](0155-MADR-one-daemon-guards-its-config-the-other-does-not.md)
  — the `slog` + `Diagnostics` warning pattern D3 copies.
* [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md)
  — Goose catalog + `ErrGooseKeyringManaged`. Remaining agents keep the rest.
* [0083-MADR-provider-auth-activation-and-layout-gaps.md](0083-MADR-provider-auth-activation-and-layout-gaps.md)
  — `keyring_managed` wire reason. Kept as reserved (D6).
* [0026-MADR-mobile-goose-support.md](0026-MADR-mobile-goose-support.md) —
  phone has no provider enum; F12 still true after removal.
* [0159-MADR-the-windows-service-path-is-half-wired.md](0159-MADR-the-windows-service-path-is-half-wired.md)
  — its evidence counts `acphttp/provider.go:361` among six `SuperviseStarted`
  call sites; after D2 there are five. Its F1 (Windows `update` always rolls
  back) is the Windows face of the update-path risk L1 would have created
  everywhere.
* [0158-MADR-move-madr-plan-files-to-docs-spec.md](0158-MADR-move-madr-plan-files-to-docs-spec.md)
  — this pair lives in `docs/spec/`.

### Open questions for the plan

The 09-17 draft's four questions are answered: (1) no capture type — D3 uses
`InConfig` plus an env scan, measured in F17; (2) the mobile phase runs
`dart format`, `flutter analyze` and `flutter test` on any Flutter 3.47.2
host, which since the later 2026-09-18 install includes this Windows host
(F21); (3) the record list is fixed by D9 and F20,
and there is no `0073-PLAN`; (4) `docs/protocol-v1.md` Goose examples are
rewritten onto `grok`. None remain open.

## Amendment — 2026-09-18: the wire-fixture guard counts the Goose fixture

Found by running P1, not by reading. `internal/wirecap/fixtures_test.go`
`TestCommittedFixturesCarryNoIdentifiers` (MADR 0147 D11, 0151) fails any
`go test ./...` in which fewer than 5 `internal/provider/*/testdata/wire/*/frames.jsonl`
fixtures exist. One of the five was `internal/provider/goose/testdata/wire/1.48.0/frames.jsonl`,
which D1 deletes. After P1's deletions the glob matches 4 (codex, grok, kilo,
opencode), and the test fails with "glob matched 4 fixtures, want at least 5".

**F24 — Deleting the Goose tree lowers a fixture floor that another package
enforces.** The inventory (F22) searched for the word "goose", and this test
never says it; it counts files.

**D15.** Lower that floor to 4 in the same commit as D1 (PLAN 0160 P1 step 13).
The floor exists to catch a glob that silently stops matching, and it still
does. The per-fixture redaction checks are unchanged.

Also found by running P1: `internal/cli/service/template_parity_test.go` is
`//go:build unix`. The parity check that F18 depends on therefore runs only on
Linux or macOS. On the Windows host `-run 'Template'` reports
`[no tests to run]`, so the P1 verification must run it in WSL.

## Observed — P1 execution (2026-09-18)

D1–D4, D6, D8, D12 and D15 landed in `9a567bf`. D3 behaved as decided: a
leftover block, a Goose env variable, and a copy of this host's real
product-seeded config each load with exit 0 and a `retired_provider_goose`
diagnostic, and a clean config draws none.

Confirmation §4's `cp "$APPDATA/mcremote/config.yaml" "$T/live-copy.yaml"` does
not work as written on Windows. The copy inherits `%TEMP%`'s ACL, and MADR
0155's credential guard then refuses it. Restrict the copy to the owner
(`icacls … /inheritance:r /grant:r *<SID>:F`) before loading it. See PLAN 0160's
execution record.

## Amendment — 2026-09-18: D14's allow-list gains `internal/config/load.go`

Found when P2 started. D14 said that after P2 only `retired_goose.go` and
`retired_goose_test.go` may mention Goose among Go files. But `Load` must call
the detector, and P1 step 5 named that call `noteRetiredGoose` and placed it in
`internal/config/load.go:130`. The record contradicted itself. Owner decision:
keep the honest name and extend the list rather than rename the function.

**D14 (amended).** Confirmation §2's output gains one path, sorted in place:
`internal/config/load.go` (the D3 call site). The list is now eight paths. The
same pass rewords one comment added by P1 step 13
(`internal/wirecap/fixtures_test.go:50`) so that it no longer names Goose.
