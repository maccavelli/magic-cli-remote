---
status: proposed
date: 2026-08-27
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0117 — Diagnostic commands skip the serve-time readiness gate

Implements [0117-MADR-diagnostic-commands-skip-serve-readiness-gate.md](0117-MADR-diagnostic-commands-skip-serve-readiness-gate.md)
decisions D1–D9, closing findings F1–F11.

Owner decisions folded in 2026-08-27 (MADR open questions 3–5): P4 covers **all**
read-only commands; text mode prints **all** diagnostics; the `diagnostics`
JSON-shape cleanup is **in scope** as P5.

## Goal

`mcrelay paths` and `mcremote paths` resolve and print the path layout on a
machine with no config, and on a machine whose config `serve` would reject.
They report *why* `serve` would refuse as a non-fatal diagnostic instead of
substituting that refusal for their output.

The finish line is mechanical:

* `mcrelay paths` on a clean machine, `MCRELAY_HOSTS` unset → **exit 0**, full
  layout, `config_not_serve_ready` present in `paths --json` `diagnostics[]`;
* `mcremote paths --config <config with a bad log.level>` → **exit 0**, full
  layout, same diagnostic;
* `mcrelay serve` with no hosts → **exit 1**, unchanged, no listener opened;
* the new tests **run on Windows** rather than skipping (F8);
* `paths --json` on a ready config omits `diagnostics` entirely — `null` is
  never emitted by either product (D9);
* `go test ./internal/relay/ ./internal/cli/ ./internal/config/` green on the
  host, and `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...` clean.

## Scope

### In scope (the only files any phase may touch)

**Modified:**

* `internal/relay/fileconfig.go` — `LoadPurpose`, `LoadOptions.Purpose`,
  conditional `Validate` call
* `internal/relay/cli.go` — `newPathsCmd` loads diagnostically and appends the
  readiness diagnostic; text output gains the trailing line
* `internal/config/load.go` — same seam for `mcremote`
* `internal/cli/paths.go` — same diagnostic reporting
* `internal/cli/pair.go` — `loadConfigFromFlags` gains an explicit purpose
  parameter (P4)
* `internal/cli/receipts.go`, `internal/cli/auth_recovery.go` — call sites
  updated for that signature (P4)
* `docs/config-mcrelay.md`, `docs/config.md` — D8, and the `--json` shape
  change from D9

**Tests (modified / created):**

* `internal/relay/cli_test.go`
* `internal/cli/paths_test.go` (created if absent)

### Out of scope

* `serve` behaviour on either product. D7 makes this a test obligation, not a
  change.
* The `--allow`-on-`paths` question (MADR open question 1). **Decided here: no.**
  Per F3 a host entry cannot change any printed path, so the flag would let an
  operator preview an effect that does not exist. Recorded so the omission is a
  decision, not an oversight.
* Any change to path resolution, `appdirs`, precedence order, or on-disk
  layout. If a phase finds itself editing `finalizePaths`, it has left scope.
* Widening `appdirs.Diagnostic` beyond its current `{Code, Message}` shape
  (`internal/appdirs/roots.go:38`). D9 changes how the slice is *emitted*, not
  what a `Diagnostic` is.
* Unifying the two products' JSON construction. D9 requires identical *output*,
  not identical mechanism: `mcremote` has a `pathReport` struct, `mcrelay` a
  map literal, and both stay.

## Stability rule

Every phase ends with, in order:

```bash
make pre-add-check                              # phase's staged Go files
gofmt -l cmd internal                           # must print nothing
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build ./...
go test -count=1 ./internal/relay/ ./internal/cli/ ./internal/config/
```

then **one commit** (`git commit --no-edit`; the repo hook writes the message,
never pass `-m`). **No `git push` and no tags at any point in this plan.**

Note on the host: this plan is being executed on `windows/amd64`, where
`-race` with `CGO_ENABLED=0` is unavailable (0116 P8 contract C7 puts race
coverage on the owner's Mac). The phase gate above therefore omits `-race`;
P6 states what a Darwin/Linux host must additionally run before this is
considered verified.

## Cross-cutting contracts

**C1 — One resolver.** No phase may add a second path-resolution routine. The
diagnostic and `serve` must reach `finalizePaths` through the same code, which
is what preserves 0059's identical-resolution invariant (`0059:471`).

**C2 — Strict by default.** `PurposeServe` is the zero value. A `LoadOptions`
literal that does not mention `Purpose` gets full validation. No phase may
invert this, and no phase may add a `Purpose` field to a struct where the zero
value is the permissive one.

**C3 — Readiness errors are never swallowed.** Where a diagnostic skips the
gate, it must run `Validate` itself and surface the result. Dropping the error
is a scope violation, not a simplification.

**C4 — Fixtures use `--config`.** No new test may place config via
`XDG_CONFIG_HOME`. Per F8 that is why the existing coverage does not run on
Windows. Existing tests keep their `SkipIfNoXDG` calls; new ones must not need
them.

**C5 — Exit codes.** A not-serve-ready config exits 0 from a diagnostic (D4).
A config that cannot be *parsed*, or whose `MCRELAY_DATA_DIR` is not absolute,
still exits non-zero — those arise before the seam and mean the paths are
genuinely unknown.

## Dependency and delivery order

P1 must land before P2 (P2 consumes the seam). P3 must land before P4 (P4
consumes the mcremote seam). P5 (the D9 shape change) must land before P6, so
P6's tests assert the final contract rather than being written twice. P1/P3 are
independent of each other and may be reordered; P2/P4 likewise.

Each phase is independently revertable: P1 and P3 are additive (a new field and
a conditional), and until P2/P4 pass a non-zero `Purpose`, behaviour is
byte-identical to today.

## Implementation Steps

### P1 — `relay`: the purpose seam (D1, D2; closes F1, F2, F3)

`internal/relay/fileconfig.go`:

1. Add above `LoadOptions`:

   ```go
   // LoadPurpose selects how much validation Load applies.
   //
   // The zero value is PurposeServe: a caller that does not mention Purpose
   // gets the full gate, so a new command cannot opt out of runtime-readiness
   // validation by omission (MADR 0117 D1).
   type LoadPurpose int

   const (
       // PurposeServe validates runtime readiness: the config must be able to
       // run a relay, not merely describe one.
       PurposeServe LoadPurpose = iota
       // PurposeDiagnostic resolves configuration for read-only reporting.
       // Path resolution is identical to PurposeServe; only the readiness
       // gate is skipped (MADR 0117 D2, F3).
       PurposeDiagnostic
   )
   ```

2. Add `Purpose LoadPurpose` to `LoadOptions` with a comment pointing at C2.

3. At `fileconfig.go:344`, make the gate conditional:

   ```go
   if opts.Purpose == PurposeServe {
       if err := cfg.Validate(); err != nil {
           return FileConfig{}, err
       }
   }
   ```

   Everything above line 344 — YAML parse, env binding, flag precedence,
   `--allow` merge, `finalizePaths` at `:337` — is untouched. That is C1.

   `cfg.TLS = cfg.TLS.Normalized()` at `:347` stays outside the conditional: it
   is a normalisation, not a validation, and the diagnostic should report
   normalised values.

**Verification:** `go build ./...`; `go test ./internal/relay/` green with no
test changes yet — behaviour is unchanged because no caller passes a purpose.

### P2 — `mcrelay paths` uses it and reports readiness (D3, D4; closes F4, F9)

`internal/relay/cli.go`, `newPathsCmd`:

1. Line 101 becomes:

   ```go
   cfg, err := Load(LoadOptions{
       ConfigFile: *cfgFile,
       Flags:      cmd.Flags(),
       Purpose:    PurposeDiagnostic,
   })
   ```

2. After the `data-dir` recompute block (`cli.go:105–110`), add — per C3:

   ```go
   if err := cfg.Validate(); err != nil {
       cfg.Diagnostics = append(cfg.Diagnostics, appdirs.Diagnostic{
           Code:    "config_not_serve_ready",
           Message: err.Error(),
       })
   }
   ```

   Placed *after* the recompute so a `--data-dir` override is reflected in what
   is validated. Requires an `appdirs` import in `cli.go`.

3. The `--json` branch already emits `"diagnostics": cfg.Diagnostics`
   (`cli.go:126`) — no change needed there.

4. The text branch gains, after `instance_key` (`cli.go:138`):

   ```go
   for _, d := range cfg.Diagnostics {
       _, _ = fmt.Fprintf(out, "diagnostic:         %s: %s\n", d.Code, d.Message)
   }
   ```

   This prints *all* diagnostics, including the pre-existing path-resolution
   notes from `appdirs` that the text format has been silently dropping.

**Verification:**

```text
./bin/mcrelay.exe paths                      → exit 0, layout + diagnostic line
./bin/mcrelay.exe paths --json               → diagnostics[] has the code
MCRELAY_HOSTS='h1:0123456789abcdef' ./bin/mcrelay.exe paths
                                             → exit 0, no diagnostic line
./bin/mcrelay.exe serve                      → exit 1, unchanged
```

### P3 — `config`: the same seam for `mcremote` (D5; closes F6)

`internal/config/load.go`: mirror P1 exactly — `LoadPurpose`, `PurposeServe`
zero value, `PurposeDiagnostic`, `LoadOptions.Purpose`, and the conditional at
`load.go:134`.

The two type declarations are deliberately **not** hoisted into a shared
package. `relay.FileConfig` and `config.Config` are separate types with
separate `Validate` methods and no common import; a shared enum would create a
dependency edge between the relay and the mcremote config packages that does
not exist today and buys nothing but a saved declaration.

**Verification:** `go build ./...`; `go test ./internal/config/ ./internal/cli/`
green with no test changes — no caller passes a purpose yet.

### P4 — `mcremote paths` and the shared loader (D5; closes F7)

1. `internal/cli/paths.go:23` passes `Purpose: config.PurposeDiagnostic`, and
   gains the same append-and-print blocks as P2. `printPaths`
   (`internal/cli/paths.go:36`) takes the diagnostics through `pathReport`.

2. `loadConfigFromFlags` (`internal/cli/pair.go:463`) gains an explicit
   purpose parameter rather than defaulting, so each of its five call sites
   states its intent:

   | Call site | Purpose |
   | --- | --- |
   | `pair.go:70` (`pair list`) | `PurposeDiagnostic` |
   | `pair.go:485` (`pair` mutating subcommands) | `PurposeServe` |
   | `receipts.go:48` (`list`/`show`/`verify`) | `PurposeDiagnostic` |
   | `auth_recovery.go:76` (`status`) | `PurposeDiagnostic` |

   `pair` mutating subcommands keep the strict gate: they write device state,
   so a config that cannot run is a real error there, not a note.

**Owner decision, 2026-08-27 (MADR open question 3): execute both steps.** The
alternative considered and rejected was scoping 0117 to `paths` alone, which
would have left `receipts list` and `auth-recovery status` failing on a typo'd
`log.level` — the same defect under a different command name.

**Verification:**

```text
mcremote paths --config <bad log.level>  → exit 0, layout + diagnostic
mcremote paths                            → exit 0, unchanged from today
mcremote serve --config <bad log.level>   → exit 1, unchanged
```

### P5 — `diagnostics` becomes a typed, omittable array (D9; closes F11)

Landing this **before** the test phase means P6 asserts the final contract once
instead of being written against an interim shape.

1. `internal/cli/paths.go:69` — `Diagnostics any` becomes:

   ```go
   Diagnostics []appdirs.Diagnostic `json:"diagnostics,omitempty"`
   ```

   For a slice type, `omitempty` omits both nil and empty, which is the
   behaviour the tag always claimed. Requires an `appdirs` import in
   `paths.go`. The assignment at `paths.go:48` (`Diagnostics: cfg.Diagnostics`)
   already supplies `[]appdirs.Diagnostic`, so it is unchanged.

2. `internal/relay/cli.go:115–127` — the map literal sets the key
   conditionally:

   ```go
   report := map[string]any{ /* … existing keys, minus diagnostics … */ }
   if len(cfg.Diagnostics) > 0 {
       report["diagnostics"] = cfg.Diagnostics
   }
   ```

   A map has no struct tags, so this is the only way to get omission. Mechanism
   differs from step 1 deliberately (see Out of scope); the emitted contract is
   what must match.

3. Confirm by hand, because this is the phase that changes a published shape:

   ```text
   mcrelay  paths --json   (ready config)     → no "diagnostics" key
   mcrelay  paths --json   (no config)        → diagnostics: [{code,message}]
   mcremote paths --json   (ready config)     → no "diagnostics" key
   mcremote paths --json   (bad log.level)    → diagnostics: [{code,message}]
   ```

   `null` must not appear in any of the four.

**Verification:** `go build ./...`; `go test ./internal/relay/ ./internal/cli/`;
the four probes above.

### P6 — Regression tests, docs, coverage (D6, D7, D8; closes F5, F8)

**Tests — `internal/relay/cli_test.go`:**

1. `TestCLIPathsNoConfigIsDiagnostic` — the case that had no coverage:

   ```go
   func TestCLIPathsNoConfigIsDiagnostic(t *testing.T) {
       // No SkipIfNoXDG: fixture is --config, honoured on every platform (C4).
       missing := filepath.Join(t.TempDir(), "absent.yaml")
       out, err := runCLI(t, map[string]string{"MCRELAY_HOSTS": ""},
           "paths", "--json", "--config", missing)
       if err != nil {
           t.Fatalf("paths must not fail on an unconfigured host: %v", err)
       }
       // assert: data_dir non-empty; diagnostics[] contains
       // code == "config_not_serve_ready"
   }
   ```

   Confirm during execution whether a **missing** `--config` path is tolerated
   or is itself an error; if it errors, the fixture becomes an existing file
   containing only a comment. Either way the assertion is unchanged. This is
   the one detail in the plan not settled by static reading.

2. `TestCLIPathsNotReadyConfigIsDiagnostic` — a config with a valid `hosts:`
   block and `log.level: verbose`; assert exit 0 and the diagnostic. This is
   the F6 probe, pinned.

3. `TestCLIPathsText` and `TestCLIPathsJSON` keep `hermeticConfig` and gain one
   assertion each: **no** `config_not_serve_ready` in the ready case. Without
   this, a bug that emits the diagnostic unconditionally passes every test
   above.

   **Assert two things, and understand why both.** After P5 the ready case
   omits the key entirely, so assert (a) no `config_not_serve_ready` code and
   (b) no `diagnostics` key at all. (b) is the P5 regression test; (a) is the
   D6 one, and (a) must not be dropped in favour of (b) — a future change that
   re-adds the key for an unrelated `appdirs` note would then still be caught
   emitting the *readiness* code wrongly.

   Before P5, a test written as "the key is absent" would have failed on both
   products. Measured on Go 1.26.5:

   ```text
   any field, `json:"...,omitempty"`, holding a nil []D  → {"diagnostics":null}
   any field, omitempty, holding []D{}                   → {"diagnostics":[]}
   field left entirely unset                             → key omitted
   map[string]any{"diagnostics": nilSlice}               → {"diagnostics":null}
   ```

   That measurement is the whole reason D9 exists; it is kept here so the test
   assertions are not later "simplified" back into a shape that cannot hold.

4. `TestCLIServeInvalidConfig` — **unmodified**, and its continued passing is
   the D7 obligation.

**Tests — `internal/cli/paths_test.go`:** the mcremote mirror of 1–3, using
`--config`. Create the file if absent.

**Docs:**

* `docs/config-mcrelay.md:21` — note that `paths` works before any config
  exists and reports serve-readiness as a diagnostic rather than an error.
* `docs/config.md` — the same note at its `mcremote paths` reference
  (`config.md:12,60`).

**Coverage:** `internal/relay` and `internal/cli` must not regress below the
80.0% floor (`scripts/coverage-delta.sh`, default `--minimum 80.0`). This phase
only adds tests, so the expected direction is up; run the floor check rather
than assuming.

## Verification (whole plan)

On a machine with no `mcrelay` config and `MCRELAY_HOSTS` unset:

```bash
mcrelay paths                 # exit 0, layout, diagnostic line
mcrelay paths --json          # exit 0, diagnostics[] has config_not_serve_ready
mcremote paths                # exit 0, unchanged
mcrelay serve                 # exit 1, "at least one host must be configured"
```

With a valid config, `paths` output is byte-identical to today apart from the
absent diagnostic.

```bash
gofmt -l cmd internal                                  # empty
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build ./...
go test -count=1 ./...
```

**Not verifiable on this host:** `go test -race` (0116 P8 C7 — Windows cannot
run `-race` with cgo off). Before this plan is called done, a Darwin or Linux
host must run `CGO_ENABLED=0 go test -race -count=1 ./internal/relay/ ./internal/cli/ ./internal/config/`.
Stated here rather than silently skipped.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `mcrelay paths` exits 0 with no config | D2, D4 |
| A2 | `paths --json` carries `config_not_serve_ready` | D3 |
| A3 | Text output shows the diagnostic | D3 |
| A4 | `mcremote paths` exits 0 on a not-ready config | D5 |
| A5 | `serve` still refuses, no listener opened | D7 |
| A6 | Ready config produces no `config_not_serve_ready` code | D6 |
| A6b | Ready config omits the `diagnostics` key entirely; `null` never emitted | D9 |
| A7 | New tests execute on Windows | F8, C4 |
| A8 | `LoadOptions{}` zero value still validates fully | D1, C2 |
| A9 | Docs state the contract | D8 |
| A10 | Coverage floor holds | — |

A8 is the one to guard hardest: it is the property that stops this change from
quietly disabling validation for some future command.

## Rollout and Rollback

No migration, no on-disk change, no protocol change, no config-file change. A
released binary and this one differ only in the exit code and stdout of two
diagnostic commands.

Rollback is `git revert` of any phase in reverse order. P2/P4 revert cleanly on
their own (the seam from P1/P3 becomes unused but harmless); P1/P3 must not be
reverted while P2/P4 stand.

## Deferred (named, so they are not mistaken for oversights)

* **`--allow` on `paths`** — decided no, see Out of scope.
* **A general `doctor` for mcrelay** — 0059 A6 already put this out of scope;
  nothing here reopens it. `mcremote doctor` exists (`root.go:90`) and loads no
  config, so it is untouched.
* **Filtering the text `paths` output to the readiness diagnostic only** —
  rejected by owner decision 2026-08-27 (MADR open question 5). P2 step 4
  prints the whole slice, which also ends the gap where text mode silently
  dropped the `appdirs` path-resolution notes that `--json` already carried.
* **Unifying `relay.LoadPurpose` and `config.LoadPurpose`** — rejected in P3
  with reasons; revisit only if a third config package appears.
* **Normalising the two `diagnostics` JSON shapes** — *no longer deferred.*
  Owner folded it in on 2026-08-27; it is P5, MADR D9.
