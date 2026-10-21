---
status: accepted
date: 2026-10-21
decision-makers: Project Owner
consulted: "`go fix -diff ./...` on go1.27.3 at ec9032d6, read 2026-10-21"
informed: anyone who edits this module's Go code
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Apply the Go 1.27 `go fix` modernizers to the whole module

## Context and Problem Statement

The owner asked on 2026-10-21 for `go fix ./...` to be run in this repository. Since Go 1.26,
`go fix` applies the modernizers of the Go analysis suite: it rewrites code to newer idioms the
module's Go version allows, and adds inline directives to wrappers its inliner can replace. The
module is now on `go 1.27.3`
([0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md),
amendment of 2026-10-21).

The question is whether to take every rewrite `go fix` proposes, and in what order with the
pending `v0.22.2` release.

### Facts [observed 2026-10-21, read-only: `go fix -diff ./...`, exit 1, on go1.27.3 at `ec9032d6`]

* **Size.** 149 files, +383 −520 lines. 109 are test files and 40 production code, across 28
  packages. The most affected are `internal/provider/codex` (19), `internal/provider/opencode` (18),
  `internal/provider/acpagent` (13), `internal/session` (13), `internal/ws` (12) and
  `internal/cli/service` (11).
* **What it rewrites,** by a rough count of added lines:
  * `for i := range n` loops (55);
  * `strings.SplitSeq` and `strings.FieldsSeq` in place of `Split` and `Fields` when only ranged
    over (35);
  * `sync.WaitGroup.Go` in place of `Add(1)`, `go func`, `defer Done()` (24);
  * the `min` and `max` builtins (16);
  * `new(expr)` in place of a temporary and its address (11);
  * `slices.Contains` and `slices.Index` (10);
  * `errors.AsType` (7);
  * `strings.Cut`, `reflect.TypeFor`, `strings.Builder`, `t.Context` and `maps` (14 together);
  * about 211 other added lines, among them struct literals that now set a promoted field directly:
    `captureHost: captureHost{api: …}` becomes `api: …` (`internal/provider/opencode/permission_test.go`).
* **Seven `//go:fix inline` directives** are added, on small exported wrappers whose callers it then
  rewrites. For example, `picker.WithEnabled(enabled bool) *bool` gains the directive, and its
  callers become `new(false)`.
* **One fix it declined:** `omitzero: ignoring alternative fix "Replace omitempty with omitzero
  (behavior change)"` in `internal/session`. It proposes no JSON tag change.

## Decision Drivers

* No change in behaviour, and none the test suite cannot see.
* The release that is rehearsed is the release that ships.
* One reviewable change, not a stream of follow-ups.

## Considered Options

* **A** — Apply everything `go fix ./...` writes (proposed after the `v0.22.2` tag; chosen before it)
* **B** — As A, without the seven `//go:fix inline` directives
* **C** — Do not apply it

## Decision Outcome

Chosen option: "A", decided by the owner on 2026-10-21 ("A, now"), because `go fix` applies only
fixes its analyzers mark as behaviour-preserving, and declined the one it could not. The full suite,
on all three operating systems, is the check that it kept behaviour.

**It lands now, before the `v0.22.2` tag.** That was the owner's choice over the proposed order,
which put it after the tag. So `v0.22.2` goes on a commit that includes it, and the rehearsal of
`ec9032d6`, run 39621247626, no longer covers the release. A new rehearsal of that later commit comes
first (PLAN 0054 S10b Deviation 1, as amended).

### Consequences

* Good, because the code uses the idioms of the Go version it declares, and new code is not written
  beside two styles.
* Good, because later `go fix` runs start from a clean tree, so their diffs show only what is new.
* Neutral, because 109 of the 149 files are tests.
* Bad, because the diff is large, and it touches the provider packages that see most other change.
  Work in flight there rebases over it.
* Bad, because the seven inline directives commit those wrappers to being inlinable. A later change
  to one of them must keep it a plain forwarding call, or drop the directive.

### Confirmation

* `go fix -diff ./...` exits 1 with this diff before, and 0 after.
* The gates pass after: `make preflight`, `make ci-windows` on the Windows test host, and the Linux
  test host's race tests.

## Pros and Cons of the Options

### A — Everything, after the tag

* Good, because it is exactly what `go fix` is designed to apply.
* Bad, because the inline directives come with it.

### B — Without the inline directives

* Good, because no wrapper is committed to being inlined.
* Bad, because their callers' rewrites come from the directives. Dropping them means hand-editing
  `go fix`'s output, and the next run proposes them again.

### C — Leave it

* Good, because nothing changes.
* Bad, because the gap grows with each Go release, and every later run shows it again.

## More Information

* The PLAN: [0206-PLAN-apply-the-go-1-27-go-fix-modernizers.md](0206-PLAN-apply-the-go-1-27-go-fix-modernizers.md).
* Not decided here: a gate that keeps the tree `go fix`-clean, such as `go fix -diff ./...` in
  `make preflight`. That would be a change to the gates, and a record of its own.
