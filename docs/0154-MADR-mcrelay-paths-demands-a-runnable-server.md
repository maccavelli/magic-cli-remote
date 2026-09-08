---
status: proposed
date: 2026-09-08
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# `mcrelay paths` refuses to print a path until the relay could serve

## Context and Problem Statement

`mcrelay paths` is documented as *"Print resolved XDG path layout (no mutation)"*.
On a host with no relay configuration it exits 1:

```console
$ mcrelay paths
error: at least one host must be configured (hosts: in YAML, MCRELAY_HOSTS, or --allow)
```

`mcremote paths` prints its layout on the same host with no configuration at
all. The first thing a new relay operator would try — asking where the thing
keeps its files — fails, and fails for a reason unrelated to the question.

Found by driving both binaries on Windows for the first time (2026-09-08),
recorded there as W-3.

### What was measured, not assumed

All on `199c5c4`, Windows 11, with no `MCRELAY_*` environment variable set and
no `%AppData%\Roaming\mcrelay\` directory present.

**Which commands the precondition actually blocks.** Exit codes captured
individually, not through a pipeline:

| Command | Exit | Note |
| --- | --- | --- |
| `mcrelay version` | 0 | |
| `mcrelay --help` | 0 | |
| `mcrelay setup-service --print-only` | 0 | does not call `Load` |
| `mcrelay paths` | **1** | `at least one host must be configured` |
| `mcrelay paths --json` | **1** | same |
| `mcrelay paths` with `MCRELAY_HOSTS` set | 0 | prints the layout |

So the blast radius is exactly `paths`, in both output modes. An earlier pass
of this measurement reported every command as exit 0; that was a shell error —
`$?` after a command substitution reports the substitution, not the binary —
and it is recorded here because the same trap produced a false "build OK"
earlier in this codebase's history.

**Where the check lives.** `FileConfig.Validate()`,
`internal/relay/fileconfig.go:611`:

```go
if len(c.Hosts) == 0 {
    return fmt.Errorf("at least one host must be configured (…)")
}
```

`Validate()` is called from exactly two places:

```text
internal/relay/fileconfig.go:361   at the end of Load()
internal/relay/cli.go:243          in serve's RunE, explicitly
```

**`serve` validates twice.** It calls `Load` (which validates at line 361) and
then calls `fc.Validate()` again itself at line 243, after applying the
runtime-scoped flags that are deliberately post-`Load`. The second call is what
actually guards serving.

**`Load` has two production callers**, `paths` (cli.go:112) and `serve`
(cli.go:217). `setup-service` does not call it, which is why it is unaffected.

**Seven tests pin "Load rejects a bad config"**, and none of them depends on
the hosts check. Every one supplies a valid `hosts:` block and fails on
something else — a secret under 16 characters, a Let's Encrypt config missing
its email, a config file with the wrong mode, four TLS shape errors.

**One test pins the hosts requirement, and it drives `serve`.**
`internal/relay/cli_test.go:102`:

```go
// TestCLIServeInvalidConfig: serve must fail fast on a config with no hosts —
    home := t.TempDir() // empty: no config file, no hosts anywhere
    if err == nil || !strings.Contains(err.Error(), "at least one host") {
```

### Findings

**F1 — `paths` inherits a serve-readiness precondition it does not need.** It
needs `finalizePaths` to have run; it does not need the relay to be capable of
accepting a registration. The two are bundled inside `Load`.

**F2 — the requirement is about running, not about shape.** Every other rule in
`Validate()` describes whether a value is well-formed: a secret long enough, a
port in range, an email present when ACME needs one. "At least one host" is
different in kind — a relay with none is perfectly well-formed and simply has
nothing to do. That is a property of a *server about to start*, not of a
configuration.

**F3 — `serve` does not depend on `Load` validating.** It re-runs `Validate()`
itself (cli.go:243). Whatever `Load` stops checking, `serve` still checks — so
the guard `TestCLIServeInvalidConfig` pins keeps its meaning.

**F4 — this is not a difference between the two products' `paths` commands.**
Both call their product's `Load`, and both `Load`s validate. `mcremote paths`
works because every field mcremote validates has a default; mcrelay has one
field that cannot have one. The asymmetry is in the configuration surface, not
in the command. An earlier note characterised it as the commands differing;
that was wrong.

**F5 — the diagnostic is otherwise good.** The message names all three ways to
supply a host. Nothing about the error is unclear; it is simply being raised by
a command that had no business asking.

## Decision Drivers

* `paths` is a diagnostic. A diagnostic that requires a working configuration
  is least useful exactly when it is most needed.
* The hosts check must keep protecting `serve` — a relay that starts and
  accepts nobody is worse than one that refuses to start.
* `Load`'s validation is pinned by seven tests and should not be weakened
  wholesale to fix one command.

## Considered Options

* **A — Move the hosts check from `Validate` to serve-readiness** (chosen)
* **B — Add `LoadOptions.SkipValidate` and set it in `paths`**
* **C — Have `paths` swallow this one error**
* **D — Give `hosts` a default**

## Decision Outcome

Chosen option: **A**. It puts the check where the requirement actually applies,
and F3 makes it safe: `serve` already validates on its own.

### The decisions

**D1 — `Validate()` stops requiring hosts.** The `len(c.Hosts) == 0` check
moves out. Everything else in `Validate()` — including the per-host id and
secret rules, which are shape checks and still apply to whatever hosts *are*
configured — stays exactly where it is.

**D2 — a new `ValidateServeable()` carries it.** It calls `Validate()` and then
requires at least one host, with the same message. `serve` calls it in place of
its current `fc.Validate()` at cli.go:243.

**D3 — `Load` keeps calling `Validate()`.** Not `ValidateServeable`. This is
what keeps the seven `Load` tests meaningful and keeps `paths` reporting a
genuinely malformed config rather than printing paths over the top of one.

**D4 — the error text does not change.** Operators, docs and
`TestCLIServeInvalidConfig` all match on "at least one host"; moving where a
check runs is not a reason to reword it.

### Consequences

* Good: `mcrelay paths` answers the question it was asked, on a host with no
  configuration — which is when someone is most likely to ask it.
* Good: `paths` still fails on a config that is actually wrong, because `Load`
  still validates shape (D3). The command becomes tolerant of an *absent*
  relay, not of a broken one.
* Good: the split names something true. `Validate` = is this configuration
  well-formed; `ValidateServeable` = could this relay serve.
* Neutral: one new exported method on `FileConfig`.
* Bad: `Validate()` alone is now insufficient to start a relay, so a future
  caller that validates and serves without using `ValidateServeable` would
  start a relay nobody can register with. The naming is the mitigation, and it
  is a weak one — this is the risk the plan's C2 exists to hold.

### Confirmation

```bash
# 1. paths works with no configuration at all:
mcrelay paths            # exit 0, prints the layout
mcrelay paths --json     # exit 0

# 2. serve still refuses:
go test ./internal/relay/ -run TestCLIServeInvalidConfig -count=1 -v

# 3. paths still refuses a config that is genuinely malformed:
go test ./internal/relay/ -run TestPaths -count=1 -v

# 4. Load's existing rejections are untouched:
go test ./internal/relay/ -count=1
```

## Pros and Cons of the Options

### A — Move the hosts check to serve-readiness (chosen)

* Good, because it places the requirement where it is true, and F2 says that is
  a different kind of rule from everything around it.
* Good, because F3 makes it a no-op for `serve`: the check it relies on is the
  one it already runs itself.
* Good, because `paths` keeps the rest of validation, so it cannot print a
  confident layout for a config that would never load.
* Bad, because it introduces two validation entry points, and the wrong one is
  the shorter, more obvious name.

### B — `LoadOptions.SkipValidate`, set by `paths`

* Good, because it is the smallest diff and touches no shared semantics.
* Bad, because it is indiscriminate: `paths` would then print a layout for a
  config with a malformed TLS block or a two-character secret, reporting
  success over a file that `serve` will reject. The failure it removes is the
  one we want; the failures it also removes are ones we want kept.
* Bad, because "skip validation" is a flag future callers will reach for
  whenever validation is inconvenient, which is how a validation layer stops
  meaning anything.

### C — `paths` swallows this one error

* Good, because it is surgical and needs no API change.
* Bad, because it couples a command to the *text* or identity of one error
  raised three layers down. That coupling is invisible from the validator, so
  the next person to reword the message breaks `paths` and no test says why.

### D — Give `hosts` a default

* Good, because it would make the whole problem disappear at the source.
* Bad, and disqualifying: there is no safe default. Any built-in host id and
  secret would be a shared credential that lets anyone register against a relay
  whose operator never configured one.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| `paths` exits 1, `version`/`--help`/`setup-service` exit 0 | exit codes captured individually, table above |
| The check is at `fileconfig.go:611` | source |
| `Validate()` has exactly two callers | `grep -rn 'Validate()' internal/relay/` |
| `serve` validates a second time | `internal/relay/cli.go:243` |
| `Load` has two production callers | `internal/relay/cli.go:112`, `:217` |
| `setup-service` does not call `Load` | absent from the same grep; exits 0 above |
| Seven `Load` tests reject bad configs, none over hosts | `fileconfig_test.go`, `fileconfig_tls_test.go` |
| The hosts rule is pinned by a serve test | `internal/relay/cli_test.go:102` |

### Related records

* **MADR 0115** — the relay flag/config precedence work that gave `Load` its
  shape; F15 there is why `serve` applies some flags after `Load` and then
  re-validates, which is the fact D2 depends on.
* **The 2026-09-08 Windows drive** — the report that recorded this as W-3,
  alongside W-1 (pair codes advertise the configured port), W-2 (runtime
  directories accumulate) and W-4 (the CLI never mentions Windows). None of the
  other three is addressed here.

### Open questions for the plan

1. **Should `paths` warn when no host is configured?** It would keep the
   signal — "this relay could not serve as configured" — while still printing
   the layout. Against: `paths` is machine-readable in `--json` mode and a
   warning has no place in that output.
2. **Do the other `paths`-style commands share this shape?** `mcremote paths`
   does not, because its validation has defaults everywhere. Whether a future
   mandatory field would reintroduce the same bug there is **[unverified]**.
