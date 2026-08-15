---
status: proposed
date: 2026-08-15
decision-makers: [Project Owner]
consulted: [Implementer]
informed: [Operators installing mcremote via setup-service]
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Keep the install-time config.yaml in lockstep with the daemon config surface

## Context and Problem Statement

On a fresh `mcremote` install the operator's live file is **not**
`configs/config.example.yaml`. `mcremote setup-service` embeds
`internal/cli/service/defaults_mcremote.yaml` and writes it to
`$XDG_CONFIG_HOME/mcremote/config.yaml` when that path is missing
(`internal/cli/service/setup.go` `//go:embed defaults_mcremote.yaml`,
`ensureDefaultConfig` never overwrites an existing file). That seed is
what this host received. The signed-receipts section (`receipts.*`,
MADR 0077 / 0078) was absent from it; the operator had to add it by
hand after reading `docs/config.md` / `docs/receipts.md`.

The question is: **which file is the operator-facing contract for
"every effective setting", how did receipts (and other keys) fall out
of the generated file, and what check makes the next feature unable
to ship the same way?**

Scope: daemon YAML templates (`defaults_mcremote.yaml`,
`configs/config.example.yaml`, `config.prod.example.yaml`,
`config.mesh-grok.yaml`), the completeness test in
`internal/cli/service/template_parity_test.go`, and the related
`config.Defaults()` / `setDefaults` surface in `internal/config`.
Does **not** rewrite existing host files. Does **not** change receipt
semantics (still opt-in, `enabled: false`). Does **not** reopen 0077
D4 or 0069 F1's product decision — it applies the same "spell the
lever" rule to every top-level section.

Related:
[0069](0069-MADR-macos-permissions-and-sandbox-parity.md) (provider-key
template drift; `allow_full_access`),
[0071](0071-MADR-codebase-assessment.md) F2 (`sandbox_broken_policy`
comment-only),
[0073](0073-MADR-goose-prompt-hang-and-debug-pass.md) finding 3
(`permission_mode: ""` undoes 0050 D3; parity test never compared
`Defaults()`),
[0077](0077-MADR-signed-receipts-permission-handoffs.md) /
[0077-PLAN](0077-PLAN-signed-receipts-permission-handoffs.md) (receipts
added to the *example* only),
[0078](0078-MADR-session-handoff-and-receipt-surfacing.md) (`handoffs`),
[0050](0050-MADR-grok-cli-surface-drift.md) D3 (pin
`permission_mode: default`).

* Implementation Plan: [0090-PLAN-config-template-completeness.md](./0090-PLAN-config-template-completeness.md)

## Decision Drivers

* A generated `config.yaml` must show every operator-facing lever.
  Features that default off are the ones most likely to stay invisible
  if the key is omitted.
* `setup-service` must never overwrite a live host file (already
  locked: `ensureDefaultConfig` returns early when the path exists).
* `config.Defaults()` is the runtime source of truth. A YAML `""` is
  not "unset" — `Load` starts from `Defaults()` then `Unmarshal`, so
  an explicit empty string overwrites a non-empty default.
* The 0069 parity test is the existing fitness function; it must
  actually see the class of bug we just hit (top-level sections).
* `docs/config.md` already says "Keep `configs/*.yaml` in sync when
  keys change." That rule had no test that covered `receipts`.

## Considered Options

* Option 1: One completeness contract — every `mapstructure` key in
  `config.Config` is spelled in the install seed, the documented
  example, and the prod/mesh variants; a reflection test fails CI if
  any file drops a key. Values in the seed and example match
  `Defaults()` except for documented, opinionated variants (mesh
  `listen.host`, pinned models).
* Option 2: Leave the seed sparse. Operators who want receipts (or
  pair, or keepalive) copy from `config.example.yaml` / docs.
* Option 3: Generate the seed from `Defaults()` at `setup-service`
  time so the YAML cannot drift.

## Decision Outcome

Chosen option: **"Option 1: One completeness contract"**, because the
operator's complaint is exactly 0069 F1 at the next layer up — a
documented, implemented feature whose lever never reached the file
`setup-service` writes — and because Option 3 would strip comments
that teach the levers (sandbox, receipts patterns, ACME).

| ID | Decision |
| --- | --- |
| **D1** | `internal/cli/service/defaults_mcremote.yaml` is the **install-time template**. It must list every operator-facing `mapstructure` key on `config.Config` with the `Defaults()` value (or the documented equivalent, e.g. `sandbox_broken_policy: warn` for the empty/`warn` pair). Header stays "Values match `config.Defaults()`." |
| **D2** | `configs/config.example.yaml` is the **annotated reference**. Same key set as D1, same default values, richer comments. Prod and mesh variants may change *values* (`listen.host: tailscale`, a pinned opencode model) but must not omit keys. |
| **D3** | Keys that exist on the Go struct but are not operator-facing stay **out** of every template: `providers.opencode.transport` (retired, MADR 0019; `Validate` rejects it), `providers.goose.args` / `providers.goose.fs_roots` (squash leftovers; daemon ignores them — 0073 finding 8). |
| **D4** | The fitness test walks `config.Config` via `mapstructure` tags and requires every required key in all four YAML files. Provider-value equality between seed and example stays. A dedicated assert requires `providers.grok.permission_mode: default` in the seed and example (0073 finding 3). |
| **D5** | Existing host `config.yaml` files are **not** rewritten. `ensureDefaultConfig` is unchanged. Operators who installed before this record keep their file; they merge new keys by hand (as this host already did for `receipts`). |
| **D6** | `setDefaults` env-binding gaps (`limits.ws_read_deadline_seconds`, `limits.ws_resume_window_seconds`, `limits.tcp_keepalive.*`, `providers.codex.sandbox_broken_policy`) are **in scope of the companion PLAN**, not a reason to keep the YAML incomplete. AutomaticEnv only resolves keys viper already knows. |

## Consequences

* Good, because the next `setup-service` on a missing path writes
  `receipts`, `pair`, `limits.tcp_keepalive`, and an explicit
  `permission_mode: default`.
* Good, because a dropped top-level section fails
  `TestTemplatesSpellEveryConfigKey` the same way 0069's test failed
  for a dropped provider key.
* Good, because `permission_mode: "default"` in the seed stops a new
  install from undoing MADR 0050 D3.
* Bad, because four YAML files must still be edited by hand when a
  key is added — the test only fails after the fact.
* Bad, because existing hosts (this one included) will not pick up
  the new keys until the operator edits the live file. `setup-service
  --force` rewrites the *unit*, not the config.
* Neutral, because receipts remain opt-in (`enabled: false`).
  Completeness is discoverability, not a behaviour change for anyone
  who never sets the patterns.

## Pros and Cons of the Options

### Option 1: One completeness contract (Chosen)

Every operator-facing key is visible in the file the operator is
given.

* Good, because it matches `docs/config.md` ("Keep `configs/*.yaml`
  in sync") and the 0069 lesson.
* Good, because comments stay next to the keys they explain.
* Bad, because N YAML files remain a manual edit set.
* Neutral, because `Defaults()` still fills omitted keys at runtime —
  completeness is for humans, not for the daemon to boot.

### Option 2: Leave the seed sparse

* Good, because a short file is easier to scan.
* Bad, because that is how receipts disappeared: 0077-PLAN step 4
  updated `config.example.yaml` and `docs/config.md` only. The
  provider-only parity test stayed green.
* Bad, because omitted keys with a *non-zero* `Defaults()` value
  (`permission_mode: default`, `receipts.handoffs: true`) become
  invisible policy.

### Option 3: Generate YAML from `Defaults()`

* Good, because the seed cannot omit a field the struct has.
* Bad, because `yaml.Marshal` of `Defaults()` drops comments, emits
  zero-values that hide intent (`permission_mode` would need a custom
  marshal just to keep the 0050 pin visible), and still would not
  write `sandbox_broken_policy: warn` (the Go zero value is `""`).
* Bad, because the install file is also a teaching document.

## Confirmation

* `go test ./internal/cli/service/ -run 'TestTemplate|TestTemplates'`
  is green: seed ↔ example key+provider-value parity; all four YAML
  files contain every required `mapstructure` path;
  `grok.permission_mode == "default"` in seed and example.
* `rg -n '^receipts:' internal/cli/service/defaults_mcremote.yaml
  configs/` hits all four files.
* `rg -n 'permission_mode: ""' internal/cli/service/defaults_mcremote.yaml
  configs/` is empty.
* A missing-config `setup-service` (or a unit test that writes
  `defaultConfigMcremote`) produces a file whose `receipts` block
  unmarshals to `Enabled=false`, empty patterns, `Handoffs=true`.
* Existing `~/.config/mcremote/config.yaml` is untouched by this
  change (`ensureDefaultConfig` still returns early).

## More Information

### F1 — The generated file is the embed, not the example

`internal/cli/service/setup.go` embeds `defaults_mcremote.yaml` as
`defaultConfigMcremote` and `ensureDefaultConfig` writes that body
at 0600 when the path is absent. README already names this file
("Written by `setup-service` when config is missing"). Copying
`configs/config.example.yaml` is a documented *manual* path, not
what install does.

### F2 — Receipts were added to the example and the docs, never the seed

0077-PLAN P5 steps 4–5:

> 4. `configs/config.example.yaml`: add a documented `receipts:` block
> 5. `docs/config.md`: add the `receipts.*` keys

No step names `defaults_mcremote.yaml`, `config.prod.example.yaml`,
or `config.mesh-grok.yaml`. `docs/config.md` already documented
`receipts.enabled` / `allow_patterns` / `deny_patterns` /
`handoffs` and the `MCREMOTE_RECEIPTS_*` env vars. `config.Defaults()`
already sets `ReceiptsConfig{Handoffs: true}` (enabled false).
`setDefaults` already binds those four keys, so
`MCREMOTE_RECEIPTS_ENABLED=true` works without a YAML block —
`TestReceiptsEnvOverride` proves it. The gap is **discoverability
of the install file**, not a broken runtime default.

### F3 — The 0069 fitness test could not see this bug

`TestTemplateProviderKeysMatchExample` only unmarshals
`providers:` and compares per-provider maps. `receipts`, `pair`,
and `limits.tcp_keepalive` are siblings of `providers`. Adding
receipts to the example and leaving them out of the seed kept that
test green. This is the same drift class as 0069 F1
(`allow_full_access` documented, omitted from the seed) one layer
up.

### F4 — Audit of the four YAML files against `config.Config` (pre-fix)

Operator-facing `mapstructure` keys on `internal/config/config.go`
`Config` and nested types, compared to the four files as they stood
before this record.

| Key / section | `Defaults()` | seed | example | prod | mesh | Effect of omission |
| --- | --- | --- | --- | --- | --- | --- |
| `receipts.enabled` | `false` | **missing** | present | **missing** | **missing** | Feature stays off; lever hidden. Operator report. |
| `receipts.allow_patterns` | `[]` | **missing** | present | **missing** | **missing** | Same. |
| `receipts.deny_patterns` | `[]` | **missing** | present | **missing** | **missing** | Same. |
| `receipts.handoffs` | `true` | **missing** | present | **missing** | **missing** | Default-on *when enabled*; invisible. |
| `pair.advertise_host` | `""` | **missing** | present | present | present | Auto-detect still works; lever hidden on fresh install. |
| `limits.ws_read_deadline_seconds` | `120` | present | **missing** | **missing** | **missing** | Runtime default applies; example/docs claim "every key". |
| `limits.ws_resume_window_seconds` | `120` | present | **missing** | **missing** | **missing** | Same. |
| `limits.tcp_keepalive.*` | 25/5/4 enabled | **missing** | **missing** | **missing** | **missing** | Zero value ≡ enabled with those numbers (`KeepaliveConfig.NetConfig`). Lever hidden. |
| `providers.kilo.*` | enabled | present | present | **missing** | **missing** | Kilo still default-on via `Defaults()`; prod/mesh hid a default-on provider (0069 F1 shape). |
| `providers.codex.sandbox_broken_policy` | `""` (≡ `warn`) | comment only | comment only | comment only | comment only | 0071 F2; lever easy to miss in a grep for keys. |
| `providers.grok.permission_mode` | `"default"` | `""` | `""` | `""` | `""` | **Behaviour change on new install**: YAML `""` overwrites `Defaults()` (0073 finding 3). |

Intentionally omitted (D3), confirmed present on the struct and
absent from all four files:

* `providers.opencode.transport` — retired; `Validate` errors.
* `providers.goose.args`, `providers.goose.fs_roots` — parsed via
  squash, discarded (`daemon.go` does not forward them).

### F5 — `permission_mode: ""` in the seed is a real override

`Load` does `cfg := Defaults(); v.Unmarshal(&cfg)`.
`Defaults().Providers.Grok.PermissionMode` is `"default"` (MADR 0050
D3, `TestGrokPermissionModeDefaultsToDefault`). A seed that writes
`permission_mode: ""` therefore ships the *inherit grok's own
config* behaviour that 0050 deliberately stopped. 0073 finding 3
already named this; it was still in every template at the start of
this audit.

### F6 — Related, not a template-key gap: viper `SetDefault` holes

`setDefaults` in `internal/config/load.go` still does not call
`SetDefault` for:

* `limits.ws_read_deadline_seconds`
* `limits.ws_resume_window_seconds`
* `limits.tcp_keepalive.disable` / `idle_seconds` / `interval_seconds` / `count`
* `providers.codex.sandbox_broken_policy`

AutomaticEnv only resolves keys already in viper's key set (the
route53.max_retries / receipts lessons). `docs/config.md` does not
list env vars for those limits keys, so this is a silent-env trap
for anyone who follows the "Viper also accepts automatic env"
footnote, not a documented-but-broken table row. 0073 finding 4
already listed these. D6 sends them to the PLAN.

`tls.letsencrypt.domains` also has no `SetDefault`, but it has an
explicit `BindEnv("tls.letsencrypt.domains", "MCREMOTE_TLS_DOMAINS")`,
so that env path works.

### F7 — Stale comments around the templates

* Seed / example / `docs/config.md` still said kilo **7.4.20** after
  MADR 0088 pinned known-good to **7.4.22**.
* Example comment on `providers.grok.auth_method_id` said "Empty =
  none (grok needs none)." `ACPProviderConfig` documents the opposite:
  empty is correct because 0085 D2 auto-selects a headless-safe
  advertised method.
* `reasoning_effort` comments listed `low | medium | high` and omitted
  `xhigh` (documented in `docs/config.md` for grok-4.6).
* `config.go` `KiloProviderConfig` still says "Enabled defaults false
  until MADR 0075 M1–M3 acceptance" next to `Enabled: true` in
  `Defaults()`. Comment-only; not a YAML gap.

### F8 — What this host's live file still is

`ensureDefaultConfig` will not merge the new keys into an existing
`~/.config/mcremote/config.yaml`. The operator who added `receipts`
by hand is done for that section. Other F4 keys (`tcp_keepalive`,
`pair` if absent, `permission_mode: default` if the live file still
has `""`) need a manual edit if this host's file was provisioned
from the old seed.

### F9 — mcrelay is out of scope and looks complete

`defaults_mcrelay.yaml` / `configs/mcrelay.example.yaml` were not
part of the reported gap. A skim of the mcrelay seed shows the
current `limits.*` and ACME keys spelled out. No change in this
record.

## Grounding (file:line at audit time)

* `Config.Receipts` sibling of `Providers`:
  `internal/config/config.go` (`mapstructure:"receipts"`).
* `ReceiptsConfig` + default `Handoffs: true`:
  `internal/config/config.go` `Defaults()`.
* Seed embed + never-overwrite:
  `internal/cli/service/setup.go` (`//go:embed defaults_mcremote.yaml`,
  `ensureDefaultConfig`).
* Provider-only parity (pre-0090):
  `internal/cli/service/template_parity_test.go`
  `TestTemplateProviderKeysMatchExample`.
* 0077 plan listed only the example + docs:
  `docs/0077-PLAN-signed-receipts-permission-handoffs.md` P5 steps 4–5.
* Empty YAML overwrites Defaults: `internal/config/load.go` `Load`
  (`cfg := Defaults()` then `v.Unmarshal`).
* 0073 already flagged `permission_mode: ""` and the `SetDefault`
  holes: `docs/0073-MADR-goose-prompt-hang-and-debug-pass.md` §6
  findings 3–4.
