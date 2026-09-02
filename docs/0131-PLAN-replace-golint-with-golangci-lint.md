---
status: "proposed"
date: 2026-09-02
associated-madr: "0131-MADR-replace-golint-with-golangci-lint.md"
---
# Implement: replace golint with golangci-lint

Associated MADR: [0131-MADR-replace-golint-with-golangci-lint.md](0131-MADR-replace-golint-with-golangci-lint.md)

## Goal

`golangci-lint run ./...` reports zero findings, the pre-add gate enforces it in
place of `golint`, and CI enforces it too — with `master` green at every commit
along the way.

## Scope

### In scope

* `.golangci.yml` (new) — the pinned linter set (MADR D1, D2).
* `scripts/go-precheck.sh` — the single implementation of the pre-add rule.
* `Makefile` — the `lint` target.
* `.github/workflows/ci.yml` — a lint job; `go vet ./...` retired into it (D4).
* `AGENTS.md`, `README.md` — the rule is documented in both.
* Whatever Go files the 85 findings live in.

### Out of scope

* **`revive`** (MADR D5). Deferred with its measurement recorded; adopting it
  is a separate decision.
* `gofmt` and `govulncheck` — untouched. Only the middle third of the pre-add
  rule changes.
* `gofumpt`. Still rejected, for the reason `AGENTS.md` already gives: it
  reflows unrelated code and buries real changes in noise.
* The Dart/Flutter side of the gate.
* The per-machine hooks in `~/.global-agent-hooks/`. They call
  `scripts/go-precheck.sh` from the repository being staged into, so they pick
  this up with no change — which is worth verifying (P3) rather than assuming.

## Stability rule

**`master` is never left in a state where the gate fails on it.** The gate
switches in P3, after the count reaches zero in P2, and not before. Each P2
batch is its own commit and its own verification, so a regression bisects to one
linter rather than to "the golangci-lint change".

## Implementation Steps

### P1 — Add the runner, advisory only (D1, D2, D6)

Commit `.golangci.yml` naming the five linters explicitly rather than relying on
the default set, which moves between releases:

```yaml
version: "2"
linters:
  default: none
  enable: [errcheck, govet, ineffassign, staticcheck, unused]
```

Add a `make lint-new` target that runs it, leaving the existing `lint`
(golint) target in place. **No gate change, no CI change** — at this point the
tool is informational and `master` stays green under the old rule.

**Verification:** `make lint-new` runs and reports the 85 findings; `make
pre-add-check` still passes; `git commit` still works.

### P2 — Fix the 85, one linter per batch, cheapest and most valuable first

Order chosen so that each batch is independently reviewable and the risky one
comes last:

1. **`ineffassign` (1)** — trivial.
2. **`unused` (5)** — delete dead code. Confirm each is genuinely unreachable
   and not an exported API or a build-tagged sibling before deleting.
3. **`staticcheck` (29)** — `SA9003` (empty branch) is a real smell and gets
   fixed. The `QF1001`/`QF1002`/`ST1005` findings are refactor and style
   suggestions: fix the ones that read better, and for any that would reflow
   code for no gain, **disable that specific check in `.golangci.yml` with a
   written reason** rather than contorting the code. That is a scoped,
   recorded configuration decision, not a blanket exclusion — the distinction
   matters and the reason goes in the file.
4. **`errcheck` (50)** — the slow one, and deliberately last. Each site is a
   judgement: handle the error, or discard it explicitly as `_ =` **with a
   comment saying why it cannot matter**. A bare `_ =` with no reason is not an
   acceptable outcome; it is the same information loss as the unchecked call,
   only harder to grep for.

**Each batch:** its own commit, `make pre-add-check` on the touched files, and
`go test -race ./...` clean. The race suite matters most for the errcheck
batch, which by definition edits error handling.

**Verification:** after batch 4, `golangci-lint run ./...` reports **0**.

### P3 — Switch the gate (D3)

Only once P2 reports zero. In `scripts/go-precheck.sh`, replace the per-file
`golint` loop with `golangci-lint`. Two details that are easy to get wrong:

* **golangci-lint is package-scoped, not file-scoped.** The current gate runs
  `golint` per file "so the output names what to fix". golangci-lint takes
  paths but analyses whole packages; running it once per staged file would
  re-analyse the same package repeatedly and be slow. Run it once over the set
  of packages containing the staged files, and keep the output — it already
  names file, line and linter.
* **Absence must still fail loudly.** The script's `need` helper prints an
  install hint and returns 1; give `golangci-lint` the same treatment
  (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<pinned>`),
  because 0118 records a gate that silently could not run when `golint` was
  missing.

Update `Makefile`: `lint` becomes golangci-lint, `lint-new` is removed.

**Verification, and this is the gate's own "seen to fail" check:** introduce a
deliberate finding in a **scratch copy** of a file, confirm
`scripts/go-precheck.sh` exits non-zero and names it, then confirm it passes
without. Never dirty the working tree to do this.

### P4 — CI (D4)

Add a lint job to `ci.yml` running the pinned golangci-lint over `./...`, and
**delete the two `go vet ./...` steps** (`:108`, `:314`) — `govet` is in the
set, so keeping them runs the same analysis twice.

Pin the action or the binary version, per D6 and the precedent in
`.github/dependabot.yml`: an unpinned linter can turn the gate red on someone
else's release.

**Verification:** a branch carrying a known finding fails the job; the same
branch without it passes.

### P5 — Documentation

`AGENTS.md` "The pre-add rule (Go)" and its "What each check means here" table,
and `README.md`'s two references. State the pinned version and that `go vet` is
subsumed. Note in `AGENTS.md` that `revive` was measured and deferred, so the
next reader does not re-litigate it from scratch.

## Verification (whole plan)

```bash
golangci-lint run ./...        # 0 findings
make pre-add-check             # clean, using golangci-lint
go test -race ./...            # clean
```

plus a CI run green on all lanes with the new job.

### Acceptance criteria

1. `.golangci.yml` names the five linters explicitly; no reliance on the
   default set. (P1)
2. `golangci-lint run ./...` reports 0. (P2)
3. `scripts/go-precheck.sh` has been **observed to fail** on an injected
   finding and pass without it, against a scratch copy. (P3)
4. `make pre-add-check` and `go test -race ./...` are clean on `master` at
   every commit in P2–P4, not only at the end. (stability rule)
5. CI runs golangci-lint, `go vet ./...` is gone, and the version is pinned.
   (P4)
6. `AGENTS.md` and `README.md` describe the gate that actually runs, including
   the deferred `revive` decision. (P5)

## Rollout and Rollback

No runtime effect: nothing here ships in a binary. The blast radius is
developer and CI workflow only.

Rollback is per phase. P4 alone: drop the CI job, restore `go vet`. P3 alone:
restore the `golint` loop in `scripts/go-precheck.sh` — the P2 fixes are
independently valuable and do not need reverting. P1 is a config file and a
Makefile target.

The per-machine agent hooks need no rollback either way, because they call the
repository's script rather than carrying their own copy.

## Deferred

* **`revive`** (MADR D5): 50 findings — `unused-parameter` 36,
  `redefines-builtin-id` 9, `package-comments` 2, `empty-block` 2,
  `time-naming` 1. Measured 2026-09-02 so a future decision starts from data.
  Trigger: a wish for style enforcement beyond what `golint` gave, which it
  never gave here — `golint` reports 0 on this codebase today.
* **Dart/Flutter lint parity.** `flutter analyze` is already enforced; whether
  it should gain custom lint rules is a separate question.
* **`gofumpt`.** Rejected, not deferred. `AGENTS.md` gives the reason.
