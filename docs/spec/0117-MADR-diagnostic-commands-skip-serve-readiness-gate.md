---
status: proposed
date: 2026-08-27
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Diagnostic commands resolve paths without the serve-time readiness gate

## Context and Problem Statement

`mcrelay paths` is documented as "Print resolved XDG path layout (no mutation)"
(`internal/relay/cli.go:98`) and `docs/config-mcrelay.md:21` tells operators to
use it to inspect the layout. It was introduced by MADR 0059 A6/P2 as *the*
path-introspection diagnostic, on the stated invariant that `paths --json`
"matches serve resolution — identical" (`0059:471`).

On a machine with no relay config it does not work:

```text
$ mcrelay paths
error: at least one host must be configured (hosts: in YAML, MCRELAY_HOSTS, or --allow)
$ echo $?
1
```

`mcremote paths` on the same machine prints its layout normally.

An operator hits this at exactly the moment the diagnostic is most useful: first
run, before a config exists, when they want to know *where mcrelay expects its
config to be*. The command has already computed that answer before it refuses to
print it.

This record decides **what validation a read-only diagnostic command is allowed
to fail on, and where the seam between "path resolution" and "runtime
readiness" lives.**

### What was measured, not assumed

Probed on this tree at `b92c4e7` with the `0.14.10.2` binaries built from it,
on `windows/amd64`. The refusal is not platform-specific — it is a pure config
path with no OS-conditional code.

```text
$ ./bin/mcrelay.exe paths
error: at least one host must be configured (hosts: in YAML, MCRELAY_HOSTS, or --allow)

$ ./bin/mcrelay.exe paths --allow 'devbox-1:smoke-test-registration-secret-0123456789'
error: unknown flag: --allow

$ MCRELAY_HOSTS='devbox-1:smoke-test-registration-secret-0123456789' ./bin/mcrelay.exe paths
product:            mcrelay
config_dir:         C:\Users\macsm\AppData\Roaming\mcrelay
data_dir:           C:\Users\macsm\AppData\Local\mcrelay
state_dir:          C:\Users\macsm\AppData\Local\mcrelay\State
cache_dir:          C:\Users\macsm\AppData\Local\mcrelay\Cache
runtime_dir:        C:\Users\macsm\AppData\Local\mcrelay\Runtime\e168fe9c142a742c
instance_key:       e168fe9c142a742c

$ ./bin/mcremote.exe paths        → exit 0, full layout printed
```

The printed layout is **identical either way**: the invented host changes
nothing about the paths. That is the whole finding in one line — the gate is
orthogonal to the output it blocks.

### Findings

**F1 — `paths` runs the serve-time gate because `Load` always validates.**

`newPathsCmd` calls `Load(LoadOptions{...})` (`internal/relay/cli.go:101`).
`Load` ends with an unconditional `cfg.Validate()`
(`internal/relay/fileconfig.go:344`), and `Validate` contains the host
requirement (`fileconfig.go:593–595`). There is no way for a caller to ask for
resolution without readiness.

**F2 — Path resolution is already complete before the gate fires.**

In `Load`, `finalizePaths` runs at `fileconfig.go:337`; `Validate` runs at
`:344`. By the time the error is returned, `cfg.Paths` and `cfg.DataDir` are
fully populated. The command computes the correct answer, then discards it.

**F3 — Nothing in `Validate()` can change the printed paths.**

`Validate` (`fileconfig.go:535–616`) checks `log.level`, `log.format`,
`tls.mode`, the cert/key pairing, the Let's Encrypt domain/email/challenge
rules, `hosts`, `limits`, and `trusted_proxies`. None of those feed any field
in the `paths` report. The one genuinely path-affecting rule — `MCRELAY_DATA_DIR`
must be absolute — lives in `finalizePaths` (`fileconfig.go:356–360`), on the
correct side of the seam already.

So the current split is not "loose vs strict validation". Every rule in
`Validate` is a *runtime-readiness* rule, and the diagnostic needs none of them.

**F4 — The escape hatch is undiscoverable and requires inventing a fake host.**

`--allow` is registered on `serve`, not on `paths`; `paths --allow …` fails with
`unknown flag`. The only ways through are a `config.yaml` containing a `hosts:`
block or `MCRELAY_HOSTS`, and `Validate` additionally rejects any secret shorter
than 16 bytes (`fileconfig.go:601`). An operator who just wants to know where
their config goes must first fabricate a plausible-looking host credential.

**F5 — The tests cannot observe this, by construction.**

`TestCLIPathsJSON` and `TestCLIPathsText` (`internal/relay/cli_test.go:58,77`)
both call `hermeticConfig(t)`, which always writes
`hosts:\n  - id: h1\n    secret: 0123456789abcdef\n`
(`cli_test.go:38`). Every `paths` test is therefore run against an
already-serve-ready config. **No test runs `paths` against an empty config
root**, which is the failing case and the first-run case.

`TestCLIServeInvalidConfig` (`cli_test.go:91`) does cover the no-hosts refusal —
but only for `serve`, which is correct and must stay.

**F6 — `mcremote` has the same defect, reached by a different rule.**

*(Corrected 2026-08-27 after probing. This finding originally read "`mcremote`
is unaffected by accident"; the measurement below refuted it before this record
was accepted. The original claim was based on reading `Validate` rather than
running it.)*

`internal/cli/paths.go:23` has the identical shape: `config.Load(...)`, then
print. `mcremote`'s `Validate` (`internal/config/config.go:1106`) has no
required-collection rule, so the *no-config* case works — every rule it checks
is satisfied by a default. But it has 38 error returns, and any config file
that trips one of them takes `paths` down the same way:

```text
$ cat cfg/config.yaml
log:
  level: verbose

$ mcremote paths --config cfg/config.yaml
error: log.level must be debug|info|warn|error, got "verbose"   → exit 1

$ mcrelay paths --config cfg/config.yaml   # with a valid hosts: block present
error: log.level must be debug|info|warn|error                  → exit 1
```

So the defect is **one bug in two products**, not an mcrelay bug with an
mcremote near-miss. `mcrelay` is merely the product where the trigger
(no hosts) is the *default* state rather than a typo. This makes D5 a fix
rather than prophylaxis.

**F7 — The blast radius on `mcremote` is wider than `paths`.**

`config.Load` has three non-test callers: `paths` (`internal/cli/paths.go:23`),
`serve` (`internal/cli/serve.go:35`), and `loadConfigFromFlags`
(`internal/cli/pair.go:463`). That third is shared by four more commands —
`pair list` (`pair.go:70`), `pair` subcommands (`pair.go:485`), `receipts`
(`receipts.go:48`) and `auth-recovery status` (`auth_recovery.go:76`) — of
which `pair list`, `receipts list/show/verify` and `auth-recovery status` are
read-only reporting commands with the same claim on this record's reasoning.

`doctor` (`root.go:90`) and `engines` (`root.go:88`) load no config at all and
are unaffected.

**F8 — On Windows there is no `paths` coverage at all.**

Both relay `paths` tests call `testexec.SkipIfNoXDG(t)`
(`cli_test.go:59,78`), which is an unconditional `t.Skip` on Windows
(`internal/testexec/*.go:98–104`) because the fixtures place config via
`XDG_CONFIG_HOME`, which Windows does not consult (MADR 0116 D3). Confirmed by
probe: `XDG_CONFIG_HOME=… mcrelay paths` resolved `config_dir` to
`%AppData%\mcrelay`, ignoring the fixture.

So on the platform this defect was found on, the command has **zero** test
coverage. `--config` is honoured on every platform and is the portable fixture
mechanism.

**F9 — On the `mcrelay` side, exactly two call sites are involved.**

`relay.Load` has two non-test callers: `paths` (`cli.go:101`) and `serve`
(`cli.go:206`). The blast radius of changing its contract is two functions in
one file. (`mcremote`'s side is wider — see F7.)

**F10 — The workaround has already leaked into the project's own process.**

`docs/0115-PLAN-mcrelay-go126-audit-and-hardening.md:506` specifies smoke-testing
`paths --json` "(against a temp config)". The need to manufacture a config for a
read-only command was noticed and normalised rather than fixed.

## Decision Drivers

* **A diagnostic that only works once the system is configured is not a
  diagnostic.** Its purpose (0059 P2: "Operators cannot dump roots") is
  unserved precisely in the unconfigured state.
* **0059's invariant must survive.** `paths` must keep reporting what `serve`
  would resolve, through the same precedence chain. That rules out any fix that
  introduces a second resolver.
* **Not-serve-ready is information, not an error, for this command.** The
  operator running `paths` while `serve` is failing wants both facts, not one
  fact suppressed by the other.
* **The safe default must be the unannotated one.** Whatever seam is added,
  a caller that says nothing must get full validation, so a future command
  cannot silently opt out of the readiness gate by omission.
* **Small, and worth keeping small.** F9 bounds the `mcrelay` side to two call
  sites; F7 bounds the `mcremote` side to one shared helper plus two commands. The
  record should not grow a general config-refactor out of it.

## Considered Options

* **A — Document the `MCRELAY_HOSTS` workaround.** Change no code; add a note
  to `docs/config-mcrelay.md`.
* **B — Give `paths` its own resolver** that reproduces the precedence chain
  without `Validate`.
* **C — Make the purpose explicit in `Load`.** Add a purpose to `LoadOptions`;
  diagnostics skip the readiness gate and report the readiness verdict as a
  non-fatal diagnostic; `serve` is unchanged.
* **D — Delete the no-hosts check from `Validate`** and let `serve` fail later,
  when a registration is refused.

## Decision Outcome

Chosen option: **"C — Make the purpose explicit in `Load`"**.

A is refuted by F4 and F10: the workaround is undiscoverable, requires
fabricating a credential, and has already cost the project a line of process
documentation. B is refuted by F2 and by 0059's identical-resolution invariant —
`paths` already runs the real resolver and gets the right answer; a second
implementation would be strictly worse and would drift. D is refuted by
`TestCLIServeInvalidConfig` and by the driver behind that test: `serve` fails
fast on a config that cannot work, and that is correct behaviour to keep.

C is the only option that fixes the diagnostic without touching what `serve`
refuses.

### The decisions

**D1 — `LoadOptions` gains an explicit purpose, defaulting to the strict one.**

```go
type LoadPurpose int

const (
    // PurposeServe is the zero value: full validation, including runtime
    // readiness. An unannotated caller gets the strict path by construction.
    PurposeServe LoadPurpose = iota
    // PurposeDiagnostic resolves configuration for read-only reporting.
    PurposeDiagnostic
)
```

The zero value being `PurposeServe` is the point: a future command added
without thinking about this record inherits full validation rather than
silently skipping it.

**D2 — `Load` runs the readiness gate only for `PurposeServe`.**

`fileconfig.go:344` becomes conditional. Everything before it — YAML parse, env
and flag precedence, `--allow` merge, `finalizePaths` — is unchanged and runs
for both purposes, which is what preserves 0059's identical-resolution
invariant.

Per F3, every rule currently in `Validate()` is a readiness rule, so today
`PurposeDiagnostic` skips `Validate` in full. The seam is named by purpose
rather than by rule so that a future *path-affecting* rule has an obvious home
on the always-run side, next to the existing `MCRELAY_DATA_DIR` absoluteness
check.

**D3 — `paths` reports the readiness verdict as a non-fatal diagnostic.**

`paths` loads with `PurposeDiagnostic`, then calls `Validate` itself and, on
error, appends to the existing `cfg.Diagnostics` slice
(`fileconfig.go:39–40`, `appdirs.Diagnostic` at `internal/appdirs/roots.go:38`):

```go
appdirs.Diagnostic{
    Code:    "config_not_serve_ready",
    Message: err.Error(),
}
```

This reuses machinery that `paths --json` already emits (`cli.go:126`). The
text output grows a matching trailing line so the two formats stay in step. The
operator gets both facts — where the paths are, *and* why `serve` would refuse —
from one command.

**D4 — `paths` exits 0 when the config is merely not serve-ready.**

It is a diagnostic (0059 A6), and the not-ready state is its most useful
output. A malformed config file (unparseable YAML, non-absolute
`MCRELAY_DATA_DIR`) still fails, because those errors arise before the seam and
mean the paths themselves are unknown.

**D5 — `mcremote paths` adopts the same purpose seam.**

`internal/cli/paths.go` and `internal/config` take the same treatment even
because F6 proves the same defect is already reachable there through any config
file that trips one of `Validate`'s 38 error returns. Per F7 the seam is applied
at `config.Load` and inherited by `loadConfigFromFlags`, so `pair list`,
`receipts list/show/verify` and `auth-recovery status` are fixed with `paths`.

**D6 — Regression tests pin the unconfigured case for both products.**

New tests run `paths` and `paths --json` against an **empty** config root, with
`MCRELAY_HOSTS` explicitly cleared, and assert exit 0, a populated `data_dir`,
and the presence of the `config_not_serve_ready` diagnostic. The existing
`hermeticConfig`-based tests stay as the configured case, and must additionally
assert that the diagnostic is **absent** there.

This is F5 turned into a test: the gap was that no test ever ran the command the
way an operator first runs it.

Fixtures use `--config`, **not** `XDG_CONFIG_HOME`. Per F8 the existing tests
are skipped wholesale on Windows because of that fixture choice, so the new
tests would otherwise not run on the platform the defect was found on.

**D7 — `serve` behaviour is unchanged, and that is a test obligation.**

`TestCLIServeInvalidConfig` must pass untouched. `serve` continues to refuse a
config with no hosts, before any listener is opened.

**D8 — `docs/config-mcrelay.md` states the contract.**

The line that points operators at `mcrelay paths` (`config-mcrelay.md:21`)
notes that it works before any config exists and reports serve-readiness as a
diagnostic rather than an error.

**D9 — `diagnostics` becomes a typed array that is omitted when empty.**

*(Added 2026-08-27 by owner decision. This record originally deferred the shape
cleanup to a separate MADR on the grounds that it changes a published `--json`
contract; the owner elected to fold it in. F11 is the audit that supports
doing so safely.)*

Both products currently emit the key even when there is nothing to report,
measured on Go 1.26.5:

```text
any field, `json:",omitempty"`, holding a nil []Diagnostic → {"diagnostics":null}
any field, omitempty, holding []Diagnostic{}              → {"diagnostics":[]}
map[string]any{"diagnostics": nilSlice}                    → {"diagnostics":null}
```

`omitempty` on an `any`-typed field does not omit a nil slice: the zero value
it tests for is the nil *interface*, and a typed nil slice boxed into `any` is
not that. `mcremote`'s `pathReport.Diagnostics`
(`internal/cli/paths.go:69`) is exactly that shape, and `mcrelay` builds a
`map[string]any` literal (`internal/relay/cli.go:115–127`) where the key is
unconditional.

The field becomes `[]appdirs.Diagnostic` on `mcremote` — where slice
`omitempty` does omit both nil and empty — and `mcrelay` sets the map key only
when the slice is non-empty. Mechanism differs because the two products build
their JSON differently; the **emitted contract is identical**: `diagnostics` is
an array of `{code, message}` when there is something to report, and absent
otherwise. Never `null`.

This is a breaking change to the `--json` output of both `paths` commands, and
is only acceptable because of F11.

**F11 — Nothing consumes the current `diagnostics` shape.**

Audited 2026-08-27 across the repository: no test in `internal/relay`,
`internal/cli` or `internal/config` asserts on the field; no script under
`scripts/` and no workflow under `.github/` invokes `paths --json`; and the
Flutter client's many `diagnostics` references are all the unrelated
`session.diagnostics` protocol op (`apps/mobile/lib/data/ws/mcremote_client.dart:3775–3784`),
not CLI output.

The blast radius of D9 is therefore the two commands themselves and any
operator's ad-hoc `jq`. A consumer written against today's output would have
had to handle `null`, and `absent` is the same falsy case in every reasonable
JSON client.

### Consequences

* Good: the first-run diagnostic works on first run, which is the only time its
  documented purpose (0059 P2) actually bites.
* Good: one resolver, so 0059's identical-resolution invariant is preserved by
  construction rather than by discipline.
* Good: `paths` becomes strictly more informative — it now reports *why* serve
  would refuse, which it previously communicated only by failing.
* Good: one seam covers both products (F6), and on `mcremote` it reaches four
  more read-only commands through the shared loader (F7).
* Good: the new tests run on Windows, where `paths` currently has no coverage
  at all (F8).
* Bad: `LoadOptions` grows a field, and every future `Load` caller must decide
  what it is. D1's zero value makes the wrong answer the safe one, which bounds
  the cost but does not remove it.
* Bad: a script that today treats a non-zero exit from `mcrelay paths` as "no
  usable config" changes meaning. No such script exists in this repository, and
  `--json` consumers gain a machine-readable replacement in `diagnostics`.
* Bad: D9 changes the `--json` shape of both `paths` commands — `diagnostics`
  goes from always-present (often `null`) to present-only-when-non-empty. F11
  shows nothing in this repository depends on it, but an operator's `jq` might.
* Good: after D9, `diagnostics` finally means what its `omitempty` tag always
  claimed, and the text and JSON formats report the same set of notes.
* Neutral: no change to `serve`, to the wire protocol, to on-disk layout, or to
  any path this or any other command resolves.

### Confirmation

On a machine with no `mcrelay` config and `MCRELAY_HOSTS` unset:

```text
mcrelay paths                       → exit 0, full layout
mcrelay paths --json                → exit 0, diagnostics[] contains
                                      code=config_not_serve_ready
mcrelay paths --json (ready config) → exit 0, no "diagnostics" key at all
mcremote paths --json (ready)       → exit 0, no "diagnostics" key at all
mcremote paths                      → exit 0, unchanged from today
mcrelay serve                       → exit 1, "at least one host must be
                                      configured", no listener opened
```

With a valid config present, `paths` output is byte-identical to today's apart
from the absent diagnostic, and `go test ./internal/relay/ ./internal/cli/ ./internal/config/`
is green.

## Pros and Cons of the Options

### A — Document the workaround

* Good: no code change.
* Bad: leaves an operator fabricating a ≥16-byte host secret to read their own
  path layout (F4).
* Bad: the documented workaround already exists in practice (F8) and the defect
  survived it.

### B — Separate resolver for `paths`

* Good: fully decouples the diagnostic from serve validation.
* Bad: two implementations of the precedence chain, which is precisely what
  0059's identical-resolution invariant forbids.
* Bad: more code than C to achieve less, given F2 — the existing resolver
  already produces the right answer.

### C — Explicit purpose in `Load` (chosen)

* Good: one resolver, one precedence chain, one seam, named.
* Good: strict-by-default (D1), so the fix cannot erode by omission.
* Good: turns a suppressed error into reported information (D3).
* Bad: a new field on `LoadOptions` that every future caller must consider.

### D — Drop the no-hosts check

* Good: smallest possible diff.
* Bad: `serve` would start a listener that can never accept a registration,
  replacing a clear startup error with a runtime mystery.
* Bad: directly contradicts `TestCLIServeInvalidConfig` and the fail-fast
  posture of MADR 0091 D5.

## More Information

### Evidence index

| Claim | Evidence |
| --- | --- |
| `paths` calls the validating `Load` | `internal/relay/cli.go:101` |
| `Load` always validates | `internal/relay/fileconfig.go:344` |
| Host requirement | `internal/relay/fileconfig.go:593–595` |
| Secret minimum length 16 | `internal/relay/fileconfig.go:601` |
| Paths finalised before validation | `internal/relay/fileconfig.go:337` vs `:344` |
| Data-dir rule already on the correct side | `internal/relay/fileconfig.go:356–360` |
| `--allow` is serve-only | probe: `paths --allow …` → `unknown flag: --allow` |
| Both `paths` tests presuppose hosts | `internal/relay/cli_test.go:38,58,77` |
| `serve` no-hosts refusal is tested | `internal/relay/cli_test.go:91` |
| `mcremote paths` has the same shape | `internal/cli/paths.go:23` |
| `mcremote paths` fails on a bad `log.level` | probe: `mcremote paths --config …` → exit 1 |
| `mcremote Validate` has no required collection | `internal/config/config.go:1106`; 38 error returns, all default-satisfiable |
| Two `relay.Load` callers | `internal/relay/cli.go:101,206` |
| Three `config.Load` callers | `internal/cli/paths.go:23`, `serve.go:35`, `pair.go:463` |
| `loadConfigFromFlags` fans out to four commands | `internal/cli/pair.go:70,485`; `receipts.go:48`; `auth_recovery.go:76` |
| `doctor` / `engines` load no config | `internal/cli/root.go:88,90`; no `Load` call in either file |
| `paths` tests skip on Windows | `internal/relay/cli_test.go:59,78` → `internal/testexec/*.go:98–104` |
| `XDG_CONFIG_HOME` ignored on Windows | probe: fixture ignored, `config_dir` = `%AppData%\mcrelay` |
| Diagnostics channel already exists | `internal/relay/fileconfig.go:39–40`; `internal/appdirs/roots.go:38`; emitted at `cli.go:126` |
| Operators are told to run `paths` | `docs/config-mcrelay.md:21` |
| Workaround already in project process | `docs/0115-PLAN-mcrelay-go126-audit-and-hardening.md:506` |

### Related records

* **MADR 0059** — introduced `paths` / `paths --json` (A6, P2) and set the
  identical-resolution invariant this record preserves.
* **MADR 0091 D5** — mcrelay's fail-fast startup posture; D7 keeps it intact
  for `serve`.
* **MADR 0116 D3** — per-platform roots; the probe above ran on the Windows
  arm, and the defect is platform-independent.
* **MADR 0115** — the audit that normalised the workaround (F10) without
  identifying it as one; its P8 also added the in-process `runCLI` harness
  (`internal/relay/cli_test.go:14–29`) that D6's tests build on.

### Open questions for the plan

1. Should `paths` gain `--allow` for symmetry with `serve`? D3 makes it
   unnecessary; adding it would let an operator preview a host's effect on the
   layout, which is nothing, so the answer is probably no. Worth one line in
   the plan rather than a silent omission.
2. ~~Does any other read-only command route through a validating load?~~
   **Answered by audit, 2026-08-27 — see F7.** `mcremote`: `paths`, `pair list`,
   `receipts list/show/verify` and `auth-recovery status` do (via
   `config.Load`, three call sites, one of them the shared
   `loadConfigFromFlags`). `doctor` and `engines` do not load config at all.
   `mcrelay`: `paths` only. The plan applies the seam at both `Load` functions,
   so the shared helper carries it to the rest.
3. ~~Should the `mcremote` commands beyond `paths` (F7) adopt
   `PurposeDiagnostic`?~~ **Owner decision, 2026-08-27: yes — all read-only
   commands.** `loadConfigFromFlags` takes an explicit purpose parameter so each
   of its five call sites states its intent; `pair`'s mutating subcommands keep
   `PurposeServe`. See PLAN P4.
4. ~~Should the `diagnostics` JSON shape cleanup be folded in or deferred?~~
   **Owner decision, 2026-08-27: folded in.** See D9 and F11.
5. ~~Should text-mode `paths` print all diagnostics or only the readiness
   one?~~ **Owner decision, 2026-08-27: all of them**, which also ends the
   long-standing gap where text mode silently dropped the `appdirs`
   path-resolution notes that `--json` already carried.
