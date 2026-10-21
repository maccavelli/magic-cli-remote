---
status: in-progress
date: 2026-10-21
associated-madr: "0206-MADR-apply-the-go-1-27-go-fix-modernizers.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement applying the Go 1.27 `go fix` modernizers to the whole module

Associated MADR: [0206-MADR-apply-the-go-1-27-go-fix-modernizers.md](0206-MADR-apply-the-go-1-27-go-fix-modernizers.md)

## Goal

The module is `go fix`-clean on go1.27.3, with its behaviour unchanged and every gate passing.

## Scope

* Every Go file `go fix ./...` rewrites, and nothing else.
* Not in scope: a gate that keeps the tree `go fix`-clean (the MADR's More Information).

## Implementation Steps

### F1 — Apply and read

~~Starts after the `v0.22.2` tag is pushed, on a clean tree.~~ Approved by the owner on 2026-10-21,
"A, now": it starts at once, before the tag, on a tree clean but for these records. `v0.22.2` then
needs a new rehearsal of the commit that includes this change (PLAN 0054 S10b Deviation 1).

1. `go fix -diff ./...`, saved; exit 1. This is the "before" of the check.
2. `go fix ./...`. Then `gofmt -l` over the tree must print nothing.
3. Read the whole diff. Any rewrite that could change behaviour stops this PLAN, as a deviation for
   the owner: one that changes an exported signature other than by an added directive, a JSON or
   wire shape, error identity, or goroutine lifetime. The MADR counts the kinds; the record lists
   what was found.
4. `go fix -diff ./...` again; exit 0.

**Deviation 1, 2026-10-21 — `go fix` runs to a fixed point, not once.**

- **Found.** After step 2's single pass, step 4's `go fix -diff ./...` still exited 1. Fixes from the
  first pass had enabled more, in seven files: the redundant `f := f` copies it had left in
  `for f := range t.Fields()` loops, the remaining `ticksOf` and `f64` calls, one more
  `range attempts` loop, and the `start` clamp in `ws/server.go`.
- **Resolved within the decision, "everything `go fix` writes":** a second `go fix ./...`. Step 4
  then exited 0, with empty output.

**Deviation 2, 2026-10-21 — four test helpers `go fix` left unused are deleted. The owner decided:
"Delete the four".**

- **Found.** `make staticcheck` exited 2 on `U1000`: `go fix` inlined every call to four test-only
  helpers and left their definitions, each with a new `//go:fix inline` directive.
  - `ticksOf`, `internal/provider/acpagent/xaicost_test.go:46`;
  - `ptr`, `internal/provider/codex/execution_p7_test.go:216`;
  - `f64`, `internal/provider/opencode/usage_test.go:13`;
  - `boolPtr`, `internal/ws/method_availability_test.go:52`.
  On `HEAD` all four are used, so the failure is this change's.
- **The resolution.** The four definitions are deleted with their directives. These are the only
  edits beyond `go fix`'s own output. The helpers still in use, and the exported
  `picker.WithEnabled` and `httpagent.Bool`, keep theirs.
- **Not taken:** reverting `go fix`'s inlining for these four, which hand-edits its output and is
  proposed again by its next run.
- **The check.** `make staticcheck` exited 2 on the tree before the deletions, and must exit 0
  after.

### F2 — Gates

* `make pre-add-check` over every tracked Go file;
* `go vet ./...` for `darwin/arm64`, `windows/amd64` and `linux/amd64` with cgo off;
* `make lint`, `make staticcheck`, `go test -race ./...`;
* `make preflight`;
* `make ci-windows` on the Windows test host, and the race tests on the Linux test host.

Stage for the owner's commit. Push and CI wait for an explicit ask.

## Verification

* `go fix -diff ./...`: exit 1 before F1, exit 0 after.
* Every F2 gate exits 0.
* The CI run after the push passes.

## Rollback and Rollout

No release is needed. Rollback is reverting the commit.

## Execution record

### F1 and F2, 2026-10-21 — staged for the owner's commit; Deviations 1 and 2 decided

**Approval.** "A, now" (the MADR). The 0206 records and the 0054 note were staged as a commit of
their own. The code below is staged after them, so the two land separately.

**F1, on go1.27.3 at `93178369`.**

- **Step 1,** `go fix -diff ./...`: exit 1. The diff matched the MADR's facts: 149 files, +383 −520,
  the same totals as the read at `ec9032d6`.
- **Step 2,** `go fix ./...`: 149 files changed, +383 −520. It printed
  `fix: omitzero: ignoring alternative fix "Replace omitempty with omitzero (behavior change)"` for
  `internal/session`, and applied no tag change to `omitzero`. `gofmt -l` printed nothing.
- **Step 3, the diff read in full.** No rewrite changes an exported signature other than by an added
  directive, error identity, or goroutine lifetime. The one wire-visible line was checked:
  - **`session.Meta.UpdatedAt`**, the phone's session row: `json:"updated_at,omitempty"` became
    `json:"updated_at"`. `omitempty` never omits a struct, so the tag did nothing.
  - **A scratch probe on go1.27.3** marshalled a `time.Time` field under both tags. The output was
    byte-identical for a zero time (`"0001-01-01T00:00:00Z"`) and a set one, and "all equal: true".
  - **Cosmetic, left as `go fix` wrote it:** `wg.Go(func() { ; … })` in
    `internal/provider/codex/diagnostics_p4_test.go`, and the generated names `before0` and `ok0` in
    two `strings.Cut` rewrites.
- **Step 4:** exit 1 after one pass; a second pass; then exit 0, with empty output (Deviation 1).

**F2, the gates.**

- **The first run:** every gate passed but `make staticcheck`, which exited 2 with four `U1000`s
  (Deviation 2).
- **The four helpers deleted** (`f2_delete_helpers.py`, each block asserted to occur exactly once).
  `gofmt -l` and `go fix -diff ./...` were still clean.
- **Then:**

```text
pre-add-check                exit=0
vet darwin/arm64             exit=0
vet windows/amd64            exit=0
vet linux/amd64 cgo-off      exit=0
make lint                    exit=0
make staticcheck             exit=0
go test -race                exit=0  42 ok, 0 FAIL lines
make check-records           exit=0
make markdownlint-docs       exit=0
make preflight               ✅ preflight passed, no FAIL line
Windows test host            make ci-windows: ALL SELECTED CHECKS PASSED, exit 0 (go1.27.3 windows/amd64)
Linux test host              race tests exit 0, probe exit 0 (go1.27.3 linux/amd64)
```

**The change, in all:** 149 Go files, +390 −534 across the two passes and Deviation 2's deletions.
109 are tests and 40 production code. Seven directives were added and four were deleted with their
helpers, so three remain: `picker.WithEnabled`, `httpagent.Bool`, and acpagent's test `ptr`.

**Still to do:** the CI run after the owner's push; then `v0.22.2` (PLAN 0054 S10b), whose rehearsal
must be of this commit.

**Files for this commit:** the 149 Go files, and this PLAN.
