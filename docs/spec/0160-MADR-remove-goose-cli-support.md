---
status: proposed
date: 2026-09-17
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

### What was measured, not assumed

All on this tree, 2026-09-17, by reading the source. No live `goose` binary
was required: the question is what *this* codebase still wires, not what
upstream Goose currently speaks.

**The provider exists as a real dialect over a real transport.**
`internal/provider/goose/goose.go` is a spec package: `Provider = acphttp.Provider`,
`New`/`NewWithLogger` construct `acphttp.New(newSpec(...), cfg.Config)`, and
the serve argv is fixed as `serve --host 127.0.0.1 --port PORT --dangerously-unauthenticated`
plus repeatable `--with-builtin` flags. `provider.IDGoose` is declared at
`internal/provider/provider.go:70`. The daemon registers it when
`cfg.Providers.Goose.Enabled` (`internal/daemon/daemon.go:247-262`), after
reconciling Goose's keyring setting.

**`acphttp` has no other production consumer.**
`grep` for the import path `internal/provider/acphttp` hits six Go files:
`internal/daemon/daemon.go` and five files under `internal/provider/goose/`
(`goose.go` plus four tests). Grok speaks ACP over stdio through `acpagent`.
OpenCode and Kilo speak HTTP+SSE through `httpagent`. Codex has its own
JSON-RPC app-server adapter. There is no fourth ACP-over-HTTP agent waiting
to inherit the package.

Line counts (PowerShell `Get-Content | Measure-Object -Line`, this host):

| Tree | Approx. lines | Role |
| --- | --- | --- |
| `internal/provider/goose/` | ~2,140 | dialect: spec, catalog (73 vendors), auth, keyring, command table, live tests, wire fixture `1.48.0` |
| `internal/provider/acphttp/` | ~5,820 | transport: engine lifecycle, WS JSON-RPC, session mapping, catalog harvest, goose-specific file-log tail |

`acphttp` is not a clean generic leftover. `internal/provider/acphttp/engine_log_tail.go:33`
hard-codes `filepath.Join(state, "goose", "logs", "cli")`, and `:40` gates the
tail on `p.spec.ID != provider.IDGoose`. The transport has already grown a
Goose-only side path (MADR 0122).

**Config treats Goose as a default-on agent, not an opt-in experiment.**
`GooseProviderConfig` embeds `ACPProviderConfig` and adds `with_builtins`,
`stream_coalesce_ms`, and `keyring_disabled` (`internal/config/config.go:544-570`).
Defaults set `Enabled: true`, `Bin: "goose"`, `KeyringDisabled: true`,
`StreamCoalesceMs: 80` (`config.go:834-850`). Viper registers
`providers.goose.*` keys so `MCREMOTE_PROVIDERS_GOOSE_*` env vars resolve
(`internal/config/load.go:323-333`). `KnownProviderIDs` includes `"goose"`
(`internal/config/prewarm_write.go:27`). Example configs ship a full `goose:`
block (`configs/config.example.yaml`, `config.mesh-grok.yaml`,
`config.prod.example.yaml`, `internal/cli/service/defaults_mcremote.yaml`).

**Viper will silently ignore a leftover `providers.goose` block if the struct
field is deleted.** The codebase already knows this failure mode. MADR 0019
retired `providers.opencode.transport` by *keeping* a capture field and
failing validation, with an explicit comment at `config.go:1182-1185`:
"Fail loudly — viper ignores unknown keys, so staying silent would quietly
change behaviour." `internal/cli/service/template_parity_test.go:85-89`
already lists `providers.goose.args` and `providers.goose.fs_roots` in
`omittedConfigKeys` for the same reason: squash leftovers the templates must
not re-document.

**Goose-only credential and keyring code sits inside a shared package.**
`internal/provider/credstore` is used by Grok, Codex, OpenCode, Kilo, doctor,
and the WS server. Goose-specific symbols that have to leave with the
provider, not with the package:

* paths: `GooseConfigPath`, `GooseSecretsPath`
* reads: `ReadGooseConfig`, `GooseConfig`, `ReadGooseSecretNames`, `GooseKeyringDisabled`
* writes: `SetGooseActiveProvider`, `SetGooseSecret`, `DeleteGooseSecret`, `SetGooseKeyringDisabled`
* errors: `ErrGooseKeyringManaged`, `ErrGooseKeyringOperatorOwned`
* marker: `GooseKeyringMarker`
* tests: `goose_keyring_test.go`, `goose_keyring_parity_test.go`, plus Goose cases in `credstore_test.go` / `write_test.go`

`isFalsey` in `credstore.go` is only referenced from Goose keyring comments and
`goose_keyring_parity_test.go`. Doctor probes Goose's config
(`internal/cli/doctor.go:79-88`). The WS server maps
`credstore.ErrGooseKeyringManaged` to `protocol.ErrKeyringManaged`
(`internal/ws/server.go:2699-2700`).

**The protocol error `keyring_managed` has Goose as its only producer.**
Declared at `internal/protocol/errors.go:175-178` and
`internal/protocol/messages.go:781`. Phone copy is in
`apps/mobile/lib/data/ws/mc_exception.dart:47-49` and names
"`goose configure`". Tests that exercise the code without needing a Goose
engine: `internal/ws/auth_err_code_test.go`,
`apps/mobile/test/friendly_op_error_test.dart`,
`auth_method_availability_test.dart`, `upstream_catalog_sheet_test.dart`,
`provider_detail_screen_test.dart`. No remaining provider (Grok, OpenCode,
Kilo, Codex, Fake) stores secrets in the OS keyring through this path.

**Cross-provider Go tests import the Goose package by name.**
Not comments — compile-time imports:

* `internal/command/conformance_test.go` — Goose is one of six `command.Tabler`s
* `internal/provider/auth_conformance_test.go` — Goose is the catalog-without-device-OAuth case
* `internal/chunkbuf/provider_mode_test.go` — reads `internal/provider/acphttp/session.go` as "goose (acphttp)"
* `internal/daemon/goose_keyring.go` / `_test.go` — production + tests

Many other tests use `"goose"` only as fixture *data* (session ids, mode lists,
provider picker rows). Those tests encode generic behaviour discovered via
Goose (dangerous `auto` as a default, large catalogs, keyring-managed methods)
and must be retargeted, not deleted.

**Live suite and Makefile.**
Three files are `//go:build live_goose`: `live_test.go`, `live_model_test.go`,
`live_purge_test.go`. `Makefile:354-355` defines `make live-goose`.
`AGENTS.md:132` lists `-tags live_goose` next to the other live agents.
`.github/workflows/` and `scripts/` contain no Goose-specific job; CI never
runs the live tag. Removing the target does not change CI YAML.

**Mobile has no Goose enum.** Provider ids are strings from `providers.list`
(MADR 0026). The phone will stop showing Goose when the daemon stops
advertising it. What *does* have to be edited: the vendor-icon manifest entry
`'goose': 'assets/vendor_icons/goose.svg'`, `apps/mobile/assets/vendor_icons/goose.svg`,
`tools/vendor-icons/ids.txt` line `goose`, Goose-named widget tests, and
comments that treat Goose as a current agent. The rest of `ids.txt` is a union
of Goose's pinned vendor table *and* live OpenCode/Kilo catalogs
(`tools/vendor-icons/sync.sh:5-8`); those vendor logos stay — OpenCode and
Kilo still render them.

**`agenterr` classifiers were written against Goose logs and are not Goose-only.**
`ExtractText` pulls JSON `fields.message` and Rust Debug `String("…")` bodies
(`internal/agenterr/agenterr.go:351-384`). `LooksLikeLongBackoff` matches
`Backing off for 3600s` and `retry_delay: Some(3600s)`. Tests use live Goose
1.45/1.48 shapes (`agenterr_test.go:258-319`). The same helpers are documented
as feeding Codex/Grok stderr and OpenCode `session.status` retries
(`agenterr.go:315-316`). Deleting the matchers would weaken remaining
providers' silent-hang abort. De-goosing comments and test names is enough.

**Default provider selection does not prefer Goose.**
`defaultProviderID` prefers ready Grok, else Fake, else the first ready
registered provider (`internal/ws/server.go:3408-3428`). Removing Goose does
not change the happy-path default.

**Two different "Goose"s share a name in this repository.**
`AGENTS.md:67` ("Registered there for claude, grok, goose, opencode, kilo and
agy") and `AGENTS.md:147-148` (commit-message rule across agent environments,
including Goose) name Block's Goose *as a coding agent that works on this
repo*. That is not the product provider. `AGENTS.md:132` (`-tags live_goose`)
is the product provider. A careless sweep that deleted every "goose" string
would strip developer-hook documentation that must stay.

**Durable sessions with `provider: goose` will not resume after the ID is
gone.** `provider.Registry.Get` returns `unknown provider %q`
(`internal/provider/registry.go:34`). Session create/resume already maps that
to `unknown_provider` on the wire. CloseAll keeps sessions listable
(`internal/session/manager_durable_test.go`); the store does not delete rows
when a provider disappears. No code auto-purges transcripts by provider id.

**Historical MADRs are the design spine, not the product surface.**
Goose-topic records under `docs/spec/`:

* `0025-MADR-goose-provider.md` / `0025-PLAN-goose-provider.md` — introduced `acphttp` + dialect
* `0026-MADR-mobile-goose-support.md` — already superseded by 0030
* `0030-MADR-goose-remote-parity.md` / `0030-PLAN-goose-remote-parity.md`
* `0073-MADR-goose-prompt-hang-and-debug-pass.md`
* `0110-MADR-goose-keyring-prompts-block-headless-launch.md` / PLAN
* `0122-MADR-deterministic-goose-file-log-tail-attach.md` / PLAN

Dozens of later records mention Goose as one of several agents (0023, 0028,
0029, 0043, 0044, 0069, 0074, 0083, 0086, 0089, 0095, …). Those decisions still
bind the remaining providers. Rewriting them to look as if Goose was never
supported would falsify the history this process exists to keep.

**Living product docs still sell Goose.** README "Current product surface",
architecture diagram, provider table, `## Provider: Goose`, live-test
instructions; `docs/config.md` key table and env table; `docs/protocol-v1.md`
provider enum and Goose examples; `docs/ops-macos-tcc.md`;
`docs/ops-android-emulator.md`; `apps/mobile/README.md`.
`docs/agent_cli_slash_commands_matrix.md` is a 2026-07-25 survey, not operator
docs.

**Shared ACP config is not Goose-only.** `ACPProviderConfig` is embedded by
Grok (`config.go:509-511`) and still documents "grok today; goose and codex
next" (`config.go:472-474`). `mcp_servers` is configured for both Grok and
Goose in the example YAML. Grok keeps MCP. Codex never took the "next" slot;
it grew its own config.

### Findings

**F1 — Goose is a default-on product provider, not a hidden extra.** Removing
it is a user-visible surface change: the phone picker loses an entry, example
configs lose a block, and a leftover `providers.goose` in an operator's YAML
must not become a silent no-op.

**F2 — `internal/provider/acphttp` is Goose's transport and has no other
importer.** Deleting the dialect and keeping the transport leaves ~5,820 lines
plus a Goose-hard-coded log tail compiling for nobody. The git history of
MADR 0025 is the reuse path if a future ACP-over-HTTP agent appears.

**F3 — Viper ignores unknown keys.** Deleting `GooseProviderConfig` without a
capture field would load existing configs successfully and drop Goose with no
error. The 0019 `RetiredTransport` pattern is the house answer.

**F4 — `credstore` is shared; only its Goose API is not.** Deleting the package
would break Grok/Codex/OpenCode/Kilo auth. Surgical removal of Goose symbols
is required.

**F5 — `keyring_managed` is a protocol code with Goose as its only producer.**
Removing the producer is required. Removing the code from the protocol in the
same change is a separate, breaking-for-mixed-upgrade choice.

**F6 — Generic tests used Goose as fixture data.** Mode-danger, large-catalog,
and keyring-unavailable tests encode protocol behaviour that still exists.
Deleting them because they say "goose" would drop coverage the remaining
agents still need.

**F7 — `agenterr` Goose-shaped matchers still serve other engines.** Keep the
classifiers; rename comments/tests so they do not claim a current Goose
integration.

**F8 — Durable Goose sessions become unresumable, not missing.** Resume fails
with `unknown_provider`. Auto-deleting transcripts would be a data loss the
owner did not ask for.

**F9 — Vendor icons are a union, not a Goose dump.** Drop the Goose *agent*
brand. Keep vendor logos OpenCode/Kilo still display.

**F10 — "Goose" in AGENTS.md is two things.** Product live-tag vs developer
coding-agent. Only the live tag is in scope.

**F11 — Historical MADRs must not be rewritten or deleted.** Product claims
move to this record. Goose-topic records can be stamped superseded. Mixed
records stay as written.

**F12 — The mobile client is provider-id-generic.** No Flutter enum of agents
needs deleting. Icon, copy, and tests that *name* Goose still do.

**F13 — CI does not run `live_goose`.** Removing the Makefile target and the
tagged tests cannot redden GitHub CI. `make test` / `make race` will redden
if any remaining test still imports the deleted packages.

**F14 — Orphan `goose serve` processes are already reaped by ownership
markers, not by binary name** (`daemon.go:173-185`, `mcremote engines --reap`).
Removal does not need a special Goose killer. Help text that lists `goose`
as an engine kind does.

**F15 — `ACPProviderConfig`, `acpagent`, Grok `mcp_servers`, and Fake stay.**
They are the remaining ACP path. Comments that say "goose and codex next"
are stale and should be corrected, not used as a reason to delete the type.

## Decision Drivers

* **Completeness** — after execution, `go test ./...` has no Goose provider,
  no `acphttp` package, and no live-goose target.
* **Fail-loud leftovers** — an operator YAML or env that still names Goose
  must refuse to start, with a message that names this record, not quietly
  drop the block (0019).
* **No collateral damage** — Grok/OpenCode/Kilo/Codex/Fake, `credstore` for
  those agents, `agenterr` classifiers, protocol error-code registry, and
  developer-Goose hook docs stay.
* **No user-data deletion** — transcripts and `~/.config/goose/` on the host
  are the operator's.
* **History stays history** — MADR bodies are not rewritten to pretend Goose
  was never supported.
* **One cut that still compiles** — the first mutating phase must leave
  `go test ./...` green, not a half-deleted import graph.

## Considered Options

* **A — Delete the dialect, the transport, and every living product mention;
  fail-loud on leftover config; stamp Goose-topic MADRs superseded** (chosen)
* **B — Default `providers.goose.enabled: false` and leave the code**
* **C — Delete `internal/provider/goose/` but keep `acphttp` as an unused
  generic transport**
* **D — `//go:build` the Goose provider out of default builds, keep sources**

## Decision Outcome

**Chosen: A — complete surgical removal, including `acphttp`, with a fail-loud
retired `providers.goose` capture.**

B keeps the maintenance cost the owner asked to end (live probes, keyring
reconcile, 73-vendor catalog drift, a second ACP stack). C keeps ~5,820 lines
and a Goose-hard-coded log tail for a hypothetical future agent; git history
is the cheaper reuse path. D is B with extra build-tag complexity. A matches
the request and the 0019 leftover-key pattern this repo already trusts.

### The decisions

**D1 — Stop shipping Goose as a product agent.** Remove `provider.IDGoose`,
daemon registration, `GooseProviderConfig` as a live type, Viper defaults,
`KnownProviderIDs` entry, doctor probe, example YAML blocks, `make live-goose`,
and `-tags live_goose` from `AGENTS.md`. After this, `providers.list` never
contains `goose`.

**D2 — Delete `internal/provider/goose/` and `internal/provider/acphttp/` in
the same phase.** They are one unit. Do not keep `acphttp` "for later." A
future ACP-over-HTTP agent starts from git history / MADR 0025, not from a
rotting package.

**D3 — Fail load if Goose is still configured.** Capture leftover
`providers.goose` YAML the way `RetiredTransport` captures
`providers.opencode.transport`. Also refuse `MCREMOTE_PROVIDERS_GOOSE_*` in
the process environment. The error names this record and says to delete the
block. Do not seed a Viper default for the capture field (0019's lesson:
a default makes every config look like it set the key).

**D4 — Strip Goose APIs from `credstore`; keep the package.** Delete the
symbols in F4's list, the two Goose keyring test files, doctor Goose probe,
daemon `goose_keyring.go`, and the WS mapping from
`ErrGooseKeyringManaged`. Do not read or write `~/.config/goose/` after
this. Do not delete that directory on disk.

**D5 — Leave durable `provider: goose` sessions on disk.** List them. Resume
and create fail through the existing `unknown_provider` path. No purge, no
rewrite of stored provider ids.

**D6 — Keep `keyring_managed` in the protocol and on the phone as a reserved
generic reason.** Remove the only producer. Rewrite the phone sentence so it
does not say `goose configure`. Mixed-upgrade phones talking to an old
daemon still decode the code; new daemons never emit it. Do not remove it
from `protocol.ErrorCodes()` / `docs/protocol-v1.md` in this work — that is
a protocol-narrowing change for a later record if anyone wants it.

**D7 — Keep `agenterr` classifiers.** De-goose comments and test names. Keep
JSON extract, Rust Debug `String("…")`, `backing off`, and
`retry_delay: Some(Ns)` matchers, and keep the fixture *strings* (they are
wire samples, not a provider import).

**D8 — Retarget generic tests; do not delete them.** Command conformance and
auth conformance drop the Goose row. Mode-danger, catalog-size, and
keyring-unavailable widget tests switch to a remaining agent or a synthetic
id. `chunkbuf` drops the `acphttp/session.go` case when that file dies.
Comments that say "goose, grok, and codex" become "grok and codex" (or
whatever is still true).

**D9 — Living docs lose Goose; historical MADRs do not.** Strip README,
`docs/config.md`, `docs/protocol-v1.md` provider enum/examples,
`docs/ops-macos-tcc.md`, `docs/ops-android-emulator.md`,
`apps/mobile/README.md`, and config examples. Leave
`docs/agent_cli_slash_commands_matrix.md` as a dated survey. Stamp
Goose-topic records listed in the measurement with
`status: superseded by 0160-MADR-remove-goose-cli-support.md`. Do not edit
their rationale. Do not stamp mixed records (0023, 0028, 0029, 0043, …).

**D10 — Drop the Goose agent brand icon only.** Remove `goose` from
`tools/vendor-icons/ids.txt`, delete `apps/mobile/assets/vendor_icons/goose.svg`,
regenerate or hand-edit `vendor_icon_manifest.g.dart` to drop the entry.
Leave other vendor SVGs. Fix `sync.sh`'s comment so it no longer cites
Goose's pinned table as an input (OpenCode/Kilo dumps + remaining agent ids).

**D11 — Do not touch developer-Goose documentation.** `AGENTS.md` hook
registration and the commit-message agent list stay. Only the `live_goose`
sentence in the Tests section is product.

**D12 — Shared ACP types stay.** `ACPProviderConfig`, `acpagent`, Grok
`mcp_servers`, Fake. Correct comments that still promise "goose and codex
next."

### Consequences

* Good, because the product surface, the extra ACP stack, the 73-vendor
  pinned catalog, and the macOS keyring special case all leave together,
  which is the only way "removed in its entirety" is true.
* Good, because leftover operator config fails closed instead of dropping
  Goose silently (F3).
* Good, because Grok/OpenCode/Kilo/Codex keep `credstore`, `agenterr`, MCP
  on Grok, and protocol codes they still share.
* Bad, because operators with Goose sessions lose the ability to resume
  them from this product. Accepted: F8, and D5 refuses to delete the
  transcripts. They can keep using Goose's own CLI against `~/.config/goose/`.
* Bad, because a future ACP-over-HTTP agent must rebuild `acphttp` from
  history. Accepted: F2, cheaper than maintaining a dead transport.
* Neutral, because `keyring_managed` remains a protocol code with no
  producer (D6). Cheap, and it avoids a mixed-upgrade hole.
* Neutral, because historical MADRs still mention Goose. That is the point
  of D9.

### Confirmation

```text
rg -n 'internal/provider/goose|internal/provider/acphttp' --glob '*.go'
  → no production or test imports (deleted packages)

rg -n 'IDGoose|GooseProviderConfig|providers\.goose|live_goose|live-goose' --glob '*.{go,yml,yaml,md,mk}'
  → hits only: this pair, superseded Goose-topic MADRs/PLANs, dated survey
     docs/agent_cli_slash_commands_matrix.md, and AGENTS.md developer-Goose
     lines (hooks / commit-message agents)

go test ./...
  → pass, including command/auth conformance without a Goose row

go test -race ./...
  → pass

# leftover config must refuse
printf 'providers:\n  goose:\n    enabled: true\n' > /tmp/leftover.yaml
# load that config
  → error mentioning providers.goose and MADR 0160

# env leftover
MCREMOTE_PROVIDERS_GOOSE_ENABLED=true <load>
  → same class of error

# phone
rg -n "goose" apps/mobile --glob '*.{dart,svg}'
  → no product-provider remaining; fixture comments may name it only as
     historical protocol samples if a test still quotes a wire reason

make live-goose
  → no such target

# Windows host, before considering the work done
make ci-windows
```

## Pros and Cons of the Options

### A — Complete surgical removal, including `acphttp` (chosen)

* Good, because it matches the request: Goose is gone as a product agent,
  and so is the transport that existed only to drive it (F2).
* Good, because the 0019 leftover-key pattern already exists; this is not a
  new kind of retirement (F3).
* Good, because the first phase can be specified as "tree compiles and
  `go test ./...` is green without those packages," which is checkable.
* Bad, because it is a large, wide diff (dialect + transport + config +
  credstore + CLI + mobile + living docs). Accepted: completeness over a
  sequence of "disabled but still there" releases.
* Bad, because `acphttp` is not trivial to rewrite. Accepted: git history.

### B — Default enabled false, keep the code

* Good, because it is a one-line default plus docs, and operators could
  turn Goose back on.
* Bad, because the owner asked for removal in its entirety, not a hide.
* Bad, because the 73-vendor catalog, keyring reconcile, live suite, and
  second ACP stack keep rotting behind a flag.
* Bad, because example configs and the phone picker still advertise it
  unless those are stripped too — at which point the leftover code has no
  supported entry point and is option C in slow motion.

### C — Delete the dialect, keep `acphttp`

* Good, because a future ACP-over-HTTP CLI could plug a new spec into an
  existing engine supervisor.
* Bad, because `acphttp` already hard-codes Goose log paths (F2). It is not
  the generic package the name suggests.
* Bad, because ~5,820 lines plus tests would still compile, still need
  `chunkbuf` pairing, still appear in coverage, and still confuse the next
  reader about whether Goose is "sort of" supported.
* Bad, because unused transports in this repo have historically grown
  Goose-shaped special cases (0122) rather than staying generic.

### D — Build-tag the provider out of default builds

* Good, because sources remain greppable and a `live_goose` tag could still
  compile them.
* Bad, because default `go test ./...` would hide the code while every
  living doc still has to be maintained against two worlds.
* Bad, because config, credstore, and protocol mapping would still need a
  story for the tagged build, which is most of option A's surface with
  none of the cleanup.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Goose dialect is a thin `acphttp.Spec` | `internal/provider/goose/goose.go` |
| `IDGoose` declared | `internal/provider/provider.go:69-70` |
| Daemon registers Goose when enabled, after keyring reconcile | `internal/daemon/daemon.go:247-262` |
| Only six Go files import `acphttp` | `rg 'internal/provider/acphttp' --glob '*.go'` |
| Goose log tail hard-coded in `acphttp` | `internal/provider/acphttp/engine_log_tail.go:33,40` |
| Goose enabled by default | `internal/config/config.go:834-850` |
| Viper `providers.goose.*` defaults | `internal/config/load.go:323-333` |
| Viper ignores unknown keys; 0019 capture pattern | `internal/config/config.go:1182-1190` |
| `KnownProviderIDs` includes goose | `internal/config/prewarm_write.go:27` |
| credstore Goose path/API | `internal/provider/credstore/credstore.go:82-156`, `write.go` Goose funcs |
| WS maps Goose keyring error to protocol | `internal/ws/server.go:2699-2700` |
| `keyring_managed` protocol code | `internal/protocol/errors.go:175-178` |
| Phone copy names `goose configure` | `apps/mobile/lib/data/ws/mc_exception.dart:47-49` |
| Command conformance imports Goose | `internal/command/conformance_test.go:14,55` |
| Auth conformance Goose row | `internal/provider/auth_conformance_test.go:47-52` |
| chunkbuf pins `acphttp/session.go` | `internal/chunkbuf/provider_mode_test.go:41-44` |
| live_goose tests | `internal/provider/goose/live_*.go` `//go:build live_goose` |
| `make live-goose` | `Makefile:354-355` |
| CI YAML has no goose job | `rg goose .github/workflows` → none |
| Doctor Goose probe | `internal/cli/doctor.go:79-88` |
| Default provider prefers Grok, not Goose | `internal/ws/server.go:3408-3428` |
| Registry unknown provider error | `internal/provider/registry.go:34` |
| Vendor icon union comment | `tools/vendor-icons/sync.sh:5-8` |
| Manifest has `'goose'` | `apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart:46` |
| AGENTS.md live tag vs developer Goose | `AGENTS.md:67,132,147-148` |
| agenterr Goose-shaped classifiers also used elsewhere | `internal/agenterr/agenterr.go:315-329,351-384` |
| Example YAML goose block | `configs/config.example.yaml:137-177` |
| Service template goose block | `internal/cli/service/defaults_mcremote.yaml:75-88` |
| Goose command table | `internal/provider/goose/commandtable.go` |
| 73-vendor pinned catalog rationale | `internal/provider/goose/catalog.go:24-40` |

### Related records

* [0025-MADR-goose-provider.md](0025-MADR-goose-provider.md) — introduced the
  dialect and `acphttp`. Superseded as product direction by this record.
* [0030-MADR-goose-remote-parity.md](0030-MADR-goose-remote-parity.md) —
  remote-parity bar for Goose. Superseded as product direction.
* [0110-MADR-goose-keyring-prompts-block-headless-launch.md](0110-MADR-goose-keyring-prompts-block-headless-launch.md)
  — `keyring_disabled` / `GOOSE_DISABLE_KEYRING` reconcile. The mechanism
  leaves with the provider (D4).
* [0122-MADR-deterministic-goose-file-log-tail-attach.md](0122-MADR-deterministic-goose-file-log-tail-attach.md)
  — Goose-only `acphttp` log tail. Leaves with D2.
* [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md)
  — leftover-key capture pattern D3 copies.
* [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md)
  — Goose catalog + `ErrGooseKeyringManaged`. Remaining agents keep the
  rest of 0074.
* [0083-MADR-provider-auth-activation-and-layout-gaps.md](0083-MADR-provider-auth-activation-and-layout-gaps.md)
  — `keyring_managed` wire reason. Kept as reserved (D6).
* [0026-MADR-mobile-goose-support.md](0026-MADR-mobile-goose-support.md) —
  phone has no provider enum; F12 still true after removal.
* [0158-MADR-move-madr-plan-files-to-docs-spec.md](0158-MADR-move-madr-plan-files-to-docs-spec.md)
  — this pair lives in `docs/spec/`.

### Open questions for the plan

1. Exact capture type for D3 (`map[string]any` vs a dedicated struct) so an
   empty `goose:` key, a populated block, and a purely env-driven leftover
   are all refused, and a config with no Goose mention still loads.
2. Whether `flutter analyze` / `dart format` must run in the mobile phase on
   this Windows host, or only `flutter test`.
3. The precise list of Goose-topic MADRs/PLANs to stamp in D9 (measurement
   lists six topics; confirm no `0073-PLAN` exists before the phase claims
   it).
4. Whether `docs/protocol-v1.md` Goose *examples* are rewritten to Grok or
   deleted; the enum line must not list `goose` either way.
