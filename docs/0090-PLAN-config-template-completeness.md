---
status: in_progress
date: 2026-08-15
associated-madr: "0090-MADR-config-template-completeness.md"
owner: [Implementer]
target-milestone: [Template sync + fitness test with the MADR; SetDefault follow-up after review]
---

# Plan: Keep the install-time config.yaml in lockstep with the daemon config surface

## Executive Summary & Goal

* **Associated Decision Record**: [0090-MADR-config-template-completeness.md](./0090-MADR-config-template-completeness.md)
* **Goal**: Make every operator-facing `config.Config` key visible in
  the file `setup-service` writes, in the documented example, and in
  the prod/mesh variants; lock that with a test that sees top-level
  sections (not just `providers.*`); close the viper `SetDefault`
  holes 0073 already named.
* **Success Criteria**:
  * [x] `defaults_mcremote.yaml` contains `receipts.*`, `pair.advertise_host`, `limits.tcp_keepalive.*`, `providers.codex.sandbox_broken_policy`, `providers.grok.permission_mode: default`
  * [x] `configs/config.example.yaml`, `config.prod.example.yaml`, `config.mesh-grok.yaml` have the same key set
  * [x] `TestTemplatesSpellEveryConfigKey` + `TestTemplateTopLevelKeysMatchExample` + `TestTemplateGrokPermissionModeIsDefault` green
  * [ ] `setDefaults` binds `limits.ws_read_deadline_seconds`, `limits.ws_resume_window_seconds`, `limits.tcp_keepalive.*`, `providers.codex.sandbox_broken_policy` (MADR D6)
  * [ ] Env-override tests for those keys (same shape as `TestReceiptsEnvOverride`)
  * [ ] `docs/config.md` env table lists the newly bound vars
  * [ ] `make pre-add-check FILES="<touched .go>"` clean before any `git add`

## Prerequisites & Dependencies

* **Infrastructure**: none. Template and test work is offline.
* **Dependencies**: none. Reflection over `config.Config`; existing
  `go.yaml.in/yaml/v3` already imported by the service package.
* **Pre-Flight Checks** (recorded at audit; these were the *before*
  state that justified the work):
  * [x] `rg -n '^receipts:' internal/cli/service/defaults_mcremote.yaml` empty
  * [x] `rg -n '^receipts:' configs/config.example.yaml` hits
  * [x] `rg -n 'permission_mode: ""' internal/cli/service/defaults_mcremote.yaml configs/` hits all four
  * [x] `TestTemplateProviderKeysMatchExample` only parsed `providers:`

## Architecture & Technical Design Summary

```
config.Config  (mapstructure tags = operator-facing key set)
        │
        ├─ config.Defaults()          runtime fill-in
        ├─ setDefaults(viper)         env/AutomaticEnv key set
        │
        ▼
defaults_mcremote.yaml   ──//go:embed──►  setup-service
        │                                 ensureDefaultConfig
        │                                 (write iff missing)
        ▼
~/.config/mcremote/config.yaml     live host file (never overwritten)

configs/config.example.yaml        annotated reference (same keys)
configs/config.prod.example.yaml   opinionated values, same keys
configs/config.mesh-grok.yaml      opinionated values, same keys

TestTemplatesSpellEveryConfigKey   walks Config tags vs all four files
TestTemplateTopLevelKeysMatchExample   seed ↔ example, every flattened key
TestTemplateProviderKeysMatchExample   0069, provider values still
TestTemplateGrokPermissionModeIsDefault   0050 D3 / 0073 finding 3
```

Omitted on purpose (MADR D3): `providers.opencode.transport`,
`providers.goose.args`, `providers.goose.fs_roots`.

## Phased Implementation Plan

### Phase 1: Template completeness (D1, D2, D5)

* **Objective**: Spell every required key in the four YAML files;
  pin `permission_mode: default`; stop commenting
  `sandbox_broken_policy` out of existence.
* **Tasks**:
  - [x] **Task 1.1**: `defaults_mcremote.yaml` — add `pair`,
        `receipts`, `limits.tcp_keepalive`; set
        `permission_mode: "default"`;
        `sandbox_broken_policy: warn`; kilo 7.4.22 comment.
  - [x] **Task 1.2**: `configs/config.example.yaml` — add the
        missing `limits.*` keys; same `permission_mode` /
        `sandbox_broken_policy` / kilo / auth_method_id /
        reasoning_effort comment fixes.
  - [x] **Task 1.3**: `config.prod.example.yaml` and
        `config.mesh-grok.yaml` — add `kilo`, `receipts`,
        `limits.ws_*` + `tcp_keepalive`; same permission_mode pin.
  - [x] **Task 1.4**: `docs/config.md` — document
        `limits.tcp_keepalive.*`; kilo known-good 7.4.22.
* **Verify**: `rg -n '^receipts:'` hits all four YAML files;
  `rg -n 'permission_mode: ""'` over those files is empty.

### Phase 2: Fitness tests (D3, D4)

* **Objective**: A dropped top-level section fails CI.
* **Tasks**:
  - [x] **Task 2.1**: `TestTemplateTopLevelKeysMatchExample` —
        flatten seed and example; both directions.
  - [x] **Task 2.2**: `TestTemplatesSpellEveryConfigKey` —
        walk `config.Config` mapstructure tags; require every
        non-omitted key in all four files.
  - [x] **Task 2.3**: `TestTemplateGrokPermissionModeIsDefault`.
  - [x] **Task 2.4**: `go test ./internal/cli/service/ ./internal/config/`
        and `make pre-add-check FILES="internal/cli/service/template_parity_test.go"`.
* **Verify**: Temporarily removing `receipts:` from the seed must
  fail Task 2.2 (do not commit that; the test's existence is the
  guard). Suite is green on the completed files.

### Phase 3: viper `SetDefault` holes (D6) — after MADR review

* **Objective**: Env vars for the MADR 0068 / 0048 knobs actually
  bind. 0073 finding 4.
* **Tasks**:
  - [ ] **Task 3.1**: `setDefaults` in `internal/config/load.go` —
        `limits.ws_read_deadline_seconds`,
        `limits.ws_resume_window_seconds`,
        `limits.tcp_keepalive.disable` / `idle_seconds` /
        `interval_seconds` / `count`,
        `providers.codex.sandbox_broken_policy`.
  - [ ] **Task 3.2**: Tests in `internal/config/config_test.go`
        (`MCREMOTE_LIMITS_WS_READ_DEADLINE_SECONDS=90` etc.), same
        shape as `TestReceiptsEnvOverride`.
  - [ ] **Task 3.3**: Add those variables to the
        `docs/config.md` environment table.
* **Verify**: `go test ./internal/config/ -run Env` green; a
  missing `SetDefault` would make the new test fail the same way
  `TestRoute53MaxRetriesEnvOverride` was written to fail.

### Phase 4: Comment-only leftovers (optional, same change or follow-up)

* **Objective**: Stop shipping the 7.4.20 pin in operator docs
  that are not the YAML templates.
* **Tasks**:
  - [ ] **Task 4.1**: README known-good kilo line (still 7.4.20).
  - [ ] **Task 4.2**: `KiloProviderConfig` comment in
        `internal/config/config.go` still says "Enabled defaults
        false until MADR 0075 M1–M3 acceptance".

## Verification & Testing Strategy

| Test | What it guards | Command |
| --- | --- | --- |
| `TestTemplateProviderKeysMatchExample` | 0069 F1, provider values seed ↔ example | `go test ./internal/cli/service/ -run TestTemplate` |
| `TestTemplateTopLevelKeysMatchExample` | 0090 F3, any top-level key seed ↔ example | same |
| `TestTemplatesSpellEveryConfigKey` | 0090 F4, struct tags vs four YAML files | same |
| `TestTemplateGrokPermissionModeIsDefault` | 0050 D3 / 0073.3 | same |
| `TestReceiptsEnvOverride` (existing) | receipts env still binds | `go test ./internal/config/` |
| Phase 3 env tests (not yet) | D6 | `go test ./internal/config/ -run Env` |

## Rollback & Mitigation Procedures

* **Trigger**: a template edit that a host already customized
  (e.g. they *want* `permission_mode: ""`) is not rolled back by
  this change — live files are never overwritten (D5).
* **If the fitness test is too strict** (a new squash field that
  should stay omitted): add it to `omittedConfigKeys` in
  `template_parity_test.go` with a comment naming the MADR that
  retired it. Do not weaken the test to provider-only again.
* **If a new install must not see receipts comments**: that is a
  reversal of D1; revert the `receipts:` block in the seed only
  after changing D1. Runtime behaviour is identical either way
  (`enabled: false`).

## Task Progress Checklist

- [x] **Phase 1: Template completeness**
  - [x] Task 1.1: install seed
  - [x] Task 1.2: documented example
  - [x] Task 1.3: prod + mesh
  - [x] Task 1.4: docs/config.md keepalive + kilo pin
- [x] **Phase 2: Fitness tests**
  - [x] Task 2.1–2.4
- [ ] **Phase 3: SetDefault / env (D6)** — wait for MADR review
  - [ ] Task 3.1: `setDefaults`
  - [ ] Task 3.2: env tests
  - [ ] Task 3.3: docs env table
- [ ] **Phase 4: leftover comments**
  - [ ] Task 4.1: README kilo 7.4.22
  - [ ] Task 4.2: `KiloProviderConfig` comment
