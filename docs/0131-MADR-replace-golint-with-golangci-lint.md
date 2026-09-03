---
status: proposed
date: 2026-09-02
decision-makers: maccavelli
consulted: —
informed: —
---

# Replace golint with golangci-lint, and pay the 85 findings that exposes

## Context and Problem Statement

The Go pre-add gate runs `gofmt`, **`golint`** and `govulncheck`
(`scripts/go-precheck.sh`, the single implementation; `AGENTS.md` "The pre-add
rule"). `golint` is **archived upstream**: `golang/lint` was frozen and
deprecated in 2021, its README points users at other tools, and it receives no
new checks. A gate whose middle third is a dead tool is a gate that decays
quietly — it will never learn about a new class of mistake.

`golangci-lint` is the obvious replacement, and it is already installed here
(2.13.2, `~/go/bin`, built with the pinned go1.26.5). But it is not a
like-for-like swap, for two reasons that decide the shape of this record.

### golangci-lint is a *runner*, so the real question is which linters

It executes a configurable set. "Switch to golangci-lint" is therefore not one
decision but two: adopt the runner, and choose the set. The two candidate sets
behave very differently on this codebase.

### Measured on this repository, 2026-09-02, golangci-lint 2.13.2

**Today's bar:** `golint ./cmd/... ./internal/...` reports **0 findings**. The
gate is green, and every commit in this repository has passed it. Any swap
starts from red, and the gate's rule is absolute — `AGENTS.md`: "Any output
fails."

**golangci-lint default set** (`errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`) — **85 findings**:

| linter | count | character |
|---|---|---|
| `errcheck` | 50 | unchecked error returns; some deliberate, each needs judgement |
| `staticcheck` | 29 | mostly `QF1001` (De Morgan's), `QF1002` (tagged switch), `ST1005` (capitalised error strings); plus `SA9003` empty branch |
| `unused` | 5 | dead code |
| `ineffassign` | 1 | ineffectual assignment |

**`revive` alone** — the tool that actually succeeds golint, and the closest
thing to a like-for-like swap — **50 findings**:

| rule | count |
|---|---|
| `unused-parameter` | 36 |
| `redefines-builtin-id` | 9 |
| `package-comments` | 2 |
| `empty-block` | 2 |
| `time-naming` | 1 |

The comparison is the point: **revive's 50 are almost entirely cosmetic**
(three quarters are unused function parameters), while the default set's 85
contain the only findings with defect-finding value here — dead code, an
ineffectual assignment, an empty branch, and fifty places where an error is
discarded without a recorded reason.

### CI does not lint Go at all

`ci.yml` runs `go vet ./...` in two lanes (`:108`, `:314`) and nothing else.
`golint` appears only in the pre-add gate and in `make lint`. So "change CI to
use golangci-lint" is not a substitution in CI — it is **adding a lint gate
that does not currently exist**, and `go vet` is subsumed by it (`govet` is in
the default set).

## Decision Drivers

* The current gate's style third is a tool that has not changed since 2021 and
  never will again.
* The gate is all-or-nothing by design. Flipping it while 85 findings stand
  would block every commit in the repository, including the commits that fix
  them.
* The findings worth having are the *bug-shaped* ones, and they are in the
  default set, not in golint's successor.
* `errcheck`'s 50 need judgement, not a mechanical fix: some discarded errors
  are deliberate and the honest change is `_ =` plus a reason, which is a
  reviewable edit, not a sweep.
* A linter that upgrades itself can turn a green gate red without a code
  change — the same hazard `.github/dependabot.yml` and 0128 D1 already
  reason about for dependencies.

## Considered Options

* **A — golangci-lint with the default set, fix the 85 first, then flip**
* **B — golangci-lint with `revive` only (like-for-like golint replacement)**
* **C — golangci-lint with default set + `revive`**
* **D — Flip the gate now and baseline the existing 85**

## Decision Outcome

Chosen option: **"A — golangci-lint with the default set, fix the 85 first,
then flip"**, because it buys the only findings on this codebase that describe
defects rather than taste, and it refuses to make the gate meaningful and
red at the same moment.

* **D1 — Adopt the runner, pin the set explicitly.** A committed
  `.golangci.yml` names the enabled linters. The default set is a moving
  target across golangci-lint releases; naming it makes an upgrade that adds a
  linter a visible edit rather than a surprise red gate.
* **D2 — The set is the default five:** `errcheck`, `govet`, `ineffassign`,
  `staticcheck`, `unused`. `revive` is **not** enabled — see D5.
* **D3 — Findings are fixed before the gate enforces them.** The gate stays on
  `golint` until the count reaches zero, then switches in one commit. At no
  point is the repository in a state where the gate fails on `master`.
* **D4 — CI gains a lint job, and `go vet ./...` is retired into it.** `govet`
  is in the set, so keeping a separate `go vet` step would run the same
  analysis twice.
* **D5 — `revive` is deferred, not rejected.** Its 50 findings are dominated by
  `unused-parameter` (36), which on interface implementations and callback
  signatures is frequently the correct shape rather than a defect. Adopting it
  is a separate decision with a separate cost, and bundling it here would
  triple the fix work for the least valuable third of the findings.
* **D6 — The golangci-lint version is pinned** in CI and named in `AGENTS.md`,
  for the reason in the drivers: an unpinned linter is a gate that can go red
  on someone else's release schedule.

### Consequences

* Good, because the gate starts catching dead code, ineffectual assignments,
  empty branches and discarded errors — none of which `golint` ever looked at.
* Good, because `go vet` stops being a separate, weaker CI step.
* Good, because CI gains a lint gate it does not have today, so a finding
  cannot reach `master` through a machine whose per-machine hooks are not
  installed (`AGENTS.md`: "a `git add` typed in a plain terminal is not gated
  by anything").
* Neutral, because `gofmt` and `govulncheck` are untouched; only the middle
  third of the pre-add rule changes.
* Bad, because 85 findings is real work before any benefit lands, and
  `errcheck`'s 50 are the slowest kind — each is a judgement about whether an
  error matters.
* Bad, because `staticcheck`'s `QF*` findings are refactor suggestions, not
  defects, and fixing them touches code for style reasons in a codebase whose
  `gofmt`-not-`gofumpt` choice (`AGENTS.md`) exists precisely to avoid that.
  Mitigated by P2's option to disable individual noisy checks with a recorded
  reason rather than reflowing code to satisfy them.
* Bad, because the tool is heavier than `golint`: a full run takes appreciably
  longer, which the per-file pre-add path must account for (P3).

### Confirmation

1. `golangci-lint run ./...` reports **0 findings** before the gate is switched
   (D3), and the count is recorded per batch in the plan's execution record.
2. `scripts/go-precheck.sh` fails on a file with a deliberately introduced
   finding, and passes once it is removed — verified against a scratch copy,
   never the working tree.
3. CI's lint job fails a pull request carrying a known finding, then passes
   without it.
4. `go test -race ./...` stays clean across every fix batch — the errcheck work
   changes error handling, which is exactly where a regression would hide.

## Pros and Cons of the Options

### A — Default set, fix first, then flip

* Good, because the gate only ever tightens from a green state.
* Good, because the enabled set is the one with defect-finding value here.
* Bad, because the benefit is deferred behind 85 fixes.

### B — `revive` only

* Good, because it is the honest like-for-like replacement for `golint` and
  keeps the gate's scope unchanged (style).
* Good, because 50 findings is less work than 85.
* Bad, because it buys almost nothing: `golint` already reports 0, so this
  spends 50 fixes to arrive at the same class of check the repository already
  passes.
* Bad, because it leaves the dead code, the ineffectual assignment and the
  discarded errors entirely unexamined.

### C — Default set + `revive`

* Good, because it is the strictest option and settles the question in one go.
* Bad, because it is 135 findings, three quarters of the extra being
  `unused-parameter` on signatures that cannot change.

### D — Flip now, baseline the 85

* Good, because the gate starts guarding new code immediately.
* Bad, because a baseline is an assertion that has been switched off for the
  code that most needs it, and this repository's rules name that pattern
  explicitly as a workaround rather than a fix. The 85 would age into
  permanence.

## More Information

* `AGENTS.md`, "The pre-add rule (Go)" — the gate this changes, and the
  "any output fails" rule that makes the sequencing in D3 necessary.
* `scripts/go-precheck.sh` — the single implementation; `make pre-add-check`
  and the per-machine agent hooks in `~/.global-agent-hooks/` all call it, so
  changing it changes every enforcement point at once.
* [0128](0128-MADR-triage-the-0126-and-0127-deferred-items.md) D1 is the
  precedent for D6: tool currency is a deliberate, pinned, recorded act here.
* [0118](0118-PLAN-symlink-dependent-tests-on-unprivileged-windows.md) records
  a gate that could not run because `golint` was absent — the same class of
  fragility a pinned, single-binary runner reduces.
