---
status: proposed
date: 2026-09-08
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0154 — Let `mcrelay paths` answer without a runnable relay

Implements [0154-MADR-mcrelay-paths-demands-a-runnable-server.md](0154-MADR-mcrelay-paths-demands-a-runnable-server.md)
decisions D1–D4, closing findings F1–F5.

## Goal

1. `mcrelay paths` and `mcrelay paths --json` exit 0 and print the layout on a
   host with no configuration and no `MCRELAY_*` environment.
2. `mcrelay serve` still refuses to start with no hosts, with the same message.
3. `mcrelay paths` still fails on a config that is genuinely malformed.
4. Every existing relay test passes unmodified.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `internal/relay/fileconfig.go` | P1 | move the check; add `ValidateServeable` (D1, D2) |
| `internal/relay/cli.go` | P1 | `serve` calls the new method (D2) |
| `internal/relay/fileconfig_test.go` | P2 | pin both halves of the split (D1, D2) |
| `internal/relay/cli_test.go` | P2 | pin `paths` with no config (F1) |

### Out of scope

* **`mcremote`.** Its `paths` already works; F4 says the difference is the
  configuration surface, not the command.
* **The error text** (D4). Docs, operators and an existing test match on
  "at least one host".
* **W-1, W-2, W-4** from the same Windows drive. Separate subjects, no records
  yet.
* **A warning when no host is configured** (MADR open question 1). Deferred.

## Stability rule

Every phase ends with:

```bash
go build ./... && go test ./... && go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')
```

The three-target cross-build is not required: nothing here is
platform-specific. That is worth stating because this finding came out of a
Windows drive and could be mistaken for Windows work — it reproduces on every
platform.

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — the hosts rule keeps guarding `serve`.** Moving where it runs must not
change whether a hostless relay can start. `TestCLIServeInvalidConfig` passes
unmodified, or the change is wrong.

**C2 — `Validate()` alone must never be enough to start a relay.** After the
split, a caller that validates and then serves without `ValidateServeable`
reintroduces the defect silently. The plan's answer is a test that names the
serve path, not a comment.

**C3 — `paths` gets more tolerant of an absent config, not of a broken one.**
`Load` keeps calling `Validate()`, so a malformed config still fails `paths`.

**C4 — no existing test is modified.** Every one passes as written; P2 only
adds.

**The contract most at risk is C2.** The tempting shape is to move the check
into `serve`'s `RunE` inline rather than into a named method — three lines,
no new API, and it works. It also puts the requirement somewhere no future
caller will find it, so the second place that ever needs a serveable config
will re-derive it or forget. The named method is the whole point of the split;
inlining gets the same test results and loses the reason.

## Dependency and delivery order

P1 then P2. The production change is small and self-contained; the tests that
prove it are worth their own commit so the diff that fixes the bug and the diff
that pins it are separable in history.

## Implementation Steps

### P1 — split the validator (D1, D2, D3, D4; closes F1, F2, F3)

In `internal/relay/fileconfig.go`, remove from `Validate()`:

```go
if len(c.Hosts) == 0 {
    return fmt.Errorf("at least one host must be configured (…)")
}
```

The per-host loop below it stays in `Validate()` — id and secret rules are
shape checks that apply to whatever hosts exist. Add:

```go
// ValidateServeable reports whether this config is well-formed AND describes a
// relay that could actually serve. The difference is one rule: a relay with no
// configured hosts is well-formed and has nothing to do.
//
// serve calls this; Load calls Validate. That split is why `mcrelay paths`
// works on a host with no configuration (MADR 0154 D1/D2).
func (c FileConfig) ValidateServeable() error {
    if err := c.Validate(); err != nil {
        return err
    }
    if len(c.Hosts) == 0 {
        return fmt.Errorf("at least one host must be configured (hosts: in YAML, MCRELAY_HOSTS, or --allow)")
    }
    return nil
}
```

In `internal/relay/cli.go:243`, `serve`'s explicit `fc.Validate()` becomes
`fc.ValidateServeable()`. `Load`'s call at `fileconfig.go:361` is unchanged
(D3).

**Verification.**

```bash
go test ./internal/relay/ -count=1              # all existing tests, unmodified
go run ./cmd/mcrelay paths                      # exit 0
go run ./cmd/mcrelay paths --json               # exit 0
```

Run the two `paths` commands with `MCRELAY_HOSTS` unset and no config file
present. `TestCLIServeInvalidConfig` passing here is C1; it must not need
editing.

### P2 — pin both halves (D1, D2; closes F5)

Add, modifying nothing:

* `TestValidateAcceptsNoHosts` — a config with zero hosts passes `Validate()`.
* `TestValidateServeableRequiresHosts` — the same config fails
  `ValidateServeable()`, and the error contains "at least one host". This is
  C2's guard: it fails if a later change moves the rule back or drops it.
* `TestValidateServeableRunsShapeChecksToo` — a config with zero hosts *and* a
  short secret fails `ValidateServeable()` on the secret, proving the method
  composes rather than replaces.
* `TestPathsWithNoHostsConfigured` in `cli_test.go` — runs the `paths` command
  against an empty `t.TempDir()` home, asserts exit 0 and that the output names
  `data_dir`. This is the regression test for the reported bug, and it must
  fail against the pre-P1 tree.

**Verification.**

```bash
go test ./internal/relay/ -run 'TestValidate|TestPaths|TestCLIServe' -count=1 -v
git stash && go test ./internal/relay/ -run TestPathsWithNoHostsConfigured -count=1; git stash pop
```

The stashed run is against the pre-P1 code and must fail. Without it, the
regression test proves only that `paths` works today.

## Verification (whole plan)

```bash
go build ./... && go test ./... && go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')
grep -rn 'ValidateServeable' internal/relay/ | grep -v _test
go run ./cmd/mcrelay paths && echo "paths OK with no config"
```

The grep must show exactly two lines: the definition and `serve`'s call.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `mcrelay paths` exits 0 with no config and no env | D1, F1 |
| A2 | `mcrelay paths --json` exits 0 in the same conditions | D1 |
| A3 | `TestCLIServeInvalidConfig` passes unmodified | C1, F3 |
| A4 | `paths` still fails on a malformed config | D3, C3 |
| A5 | `ValidateServeable` has exactly one production caller | C2 |
| A6 | The new `paths` regression test fails against the pre-P1 tree | P2 |
| A7 | No existing test is edited | C4 |
| A8 | The error text is byte-identical to today's | D4 |

**A6 is the criterion most likely to be skipped.** Once the fix is in, watching
the new test fail requires deliberately reverting, and the test passes either
way in the reviewer's eye. Without it, `TestPathsWithNoHostsConfigured` proves
only that the command works — not that it would have caught the bug.

**A7 is the one most likely to be violated quietly.** If any existing test needs
editing, the change is bigger than this record describes and the record is
wrong, not the test.

## Rollout and Rollback

One check moved, one method added, one call site changed. No protocol, no wire
format, no path layout, no user-visible output except that a previously failing
command now succeeds. Reverting P1 restores the old behaviour exactly; P2
reverts independently.

The only behaviour change for an existing user is that `mcrelay paths` stops
failing. Nothing that used to succeed changes.

## Deferred (named, so they are not mistaken for oversights)

* **A warning from `paths` when no host is configured** (MADR open question 1).
  It would preserve the signal that the relay could not serve. Left out because
  `--json` output is machine-read and a warning has no place in it; if it is
  wanted, the text mode is the only place it belongs.
* **Auditing other commands for the same shape** (open question 2). `mcremote
  paths` is fine today because its validation has defaults throughout, but
  nothing prevents a future mandatory field from reintroducing this exact bug
  there. A guard would be a test asserting `paths` succeeds on an empty home for
  both products — cheap, and worth doing if it ever happens twice.
* **W-1, W-2 and W-4** from the 2026-09-08 Windows drive. W-1 (pair codes
  advertise the configured port rather than the bound one) is the most
  user-visible of the three and has no record yet.
