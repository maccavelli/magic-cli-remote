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
