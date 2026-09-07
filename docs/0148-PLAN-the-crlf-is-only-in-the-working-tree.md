---
status: proposed
date: 2026-09-07
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0148 — The CRLF is only in the working tree, so fixing it costs nothing to commit

Implements [0148-MADR-the-crlf-is-only-in-the-working-tree.md](0148-MADR-the-crlf-is-only-in-the-working-tree.md)
decisions D1–D5, closing findings F1–F8.

## Goal

Observable states on the Windows dev laptop:

1. Zero tracked text files in the working tree contain CRLF.
2. `git status --porcelain` is empty before and after, and `git log` is
   unchanged — the refresh contributes no commit.
3. `gofmt -l $(git ls-files '*.go')` is empty, so the local formatting check is
   usable for the first time since `e929614`.
4. `go test ./...` is green and `TestSourceScansSurviveCRLF` still exists and
   still passes.
5. Untracked files and directories — `dist/`, build output, local scratch — are
   untouched.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| *(the working tree itself — no file's content changes)* | P1 | line endings only (D1, D2) |
| `docs/0147-MADR-windows-gate-measures-the-host-not-the-code.md` | P2 | correct Option B's cost estimate (D5) |

P1 changes no file's *content*. Every byte difference it produces is a `\r`
that git already ignores, which is why it commits nothing.

### Out of scope

* **Any `.go`, `.dart` or config file's content.** If P1 produces a single
  staged or unstaged change, the record is falsified — stop (D1).
* **`core.autocrlf`.** System-scope, installer-set, already overridden by
  `.gitattributes` (D4, MADR F4).
* **`.gitattributes`.** It has been correct since `e929614`; the tree is what
  is stale.
* **MADR 0147 D5's read-time normalisation.** It stays (D3). See C2 — this is
  the contract most likely to be violated.
* **Other machines' checkouts.** Nothing here can reach them; named in Deferred.

## Stability rule

P1 is not a code change, so the usual build/test gate is the *confirmation*
rather than a precondition. Run, in this order:

```bash
git status --porcelain          # MUST be empty before starting
# ... the refresh ...
git status --porcelain          # MUST be empty after
go build ./...
go test ./...
gofmt -l $(git ls-files '*.go')
```

and on this Windows host:

```bash
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

P1 produces no commit. P2 is a docs-only commit under the AGENTS.md bootstrap
exception. **`git push` needs an explicit instruction in the same turn** — this
plan does not authorise it.

## Cross-cutting contracts

**C1 — nothing enters history from P1.** `git status` empty before and after,
`git log --oneline -1` unchanged. If a diff appears, stop and amend the MADR:
it would mean a file's content differs from `HEAD`, which F1 says cannot happen.

**C2 — `TestSourceScansSurviveCRLF` and the `readSource` normalisation stay.**
(D3, MADR F7.)

**C3 — `core.autocrlf` is not touched**, at any scope. (D4.)

**C4 — untracked files survive.** The recipe operates on `git ls-files` /
the index only.

**The contract most at risk is C2.** Once the tree is uniformly LF, the CRLF
guard added by 0147 P2 looks like dead weight — it defends against a state that
no longer exists *on this machine*. Deleting it would be a one-line change with
a plausible justification and would silently re-create MADR 0147 F8 for every
contributor whose checkout is stale in the same way this one was. If anyone
proposes removing it, this line is the answer.

## Dependency and delivery order

P1 then P2, though they are independent; P2 is a documentation correction that
does not depend on the refresh having run.

## Implementation Steps

### P1 — refresh the working tree (D1, D2; closes F1–F5, F8)

**Recipe, verified in a scratch repository that reproduced the exact situation**
(LF blobs, CRLF working tree, `.gitattributes` added after the fact,
`git status` clean):

```bash
git status --porcelain          # must be empty; abort if not
git rm --cached -r -q .
git reset --hard
```

`git rm --cached -r .` empties the index without touching the disk;
`git reset --hard` then rewrites every working-tree file from `HEAD`, and
`.gitattributes` (`* text=auto eol=lf`) makes that write LF.

Why not the obvious alternatives:

* `git checkout-index -f` leaves existing files untouched — measured, MADR F3.
* `git ls-files -z | xargs -0 rm -f && git checkout -- .` works, but leaves the
  working tree *empty* between the two commands. `git rm --cached` never does.

**If it is interrupted**, the index may be empty while the disk is intact. Re-run
`git reset --hard`; it restores the index from `HEAD`. Nothing is lost, because
nothing was uncommitted (the first command asserted that).

**Verification.**

```bash
git status --porcelain          # empty — nothing entered history
git log --oneline -1            # unchanged
# CRLF census must now be 0:
git ls-files -z | while IFS= read -r -d '' f; do
  case "$f" in *.png|*.jpg|*.jpeg|*.gif|*.ico|*.webp|*.pdf|*.zip|*.gz|*.mp4|*.ttf|*.otf|*.woff|*.woff2|*.jks|*.keystore|*.apk) continue;; esac
  [ -f "$f" ] || continue
  head -c 8000 "$f" | grep -qU $'\r' 2>/dev/null && echo "$f"
done | wc -l
gofmt -l $(git ls-files '*.go')   # empty
go build ./... && go test ./...
go test ./internal/ws/ -run TestSourceScansSurviveCRLF -count=1   # still passes (C2)
ls dist 2>/dev/null              # untracked output still there (C4)
```

The CRLF census going from ~1671 to 0 while `git status` stays empty is the
whole record in two numbers — capture both.

### P2 — correct MADR 0147 Option B (D5; closes F6)

0147's Option B says renormalisation is "a 1668-file diff that touches nearly
every review". The diff is empty. Add a short amendment to 0147 pointing at this
record, rather than editing the original argument — the rationale as believed at
decision time is the thing a MADR exists to preserve, and Option B was still
correctly *rejected*, just for a reason that was not true.

**Verification.** 0147 carries an amendment naming 0148; its Option B text is
unedited; a reader arriving at that cost estimate is sent here.

## Verification (whole plan)

```bash
git status --porcelain                        # empty
gofmt -l $(git ls-files '*.go')               # empty
go build ./... && go test ./... && go test -race ./...
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | CRLF census is 0 (was ~1671) | Confirmation |
| A2 | `git status --porcelain` empty before and after; no new commit from P1 | C1, D1 |
| A3 | `gofmt -l` over all tracked `.go` is empty | Confirmation, F5 |
| A4 | `go test ./...` and `-race` green; gate green | Confirmation |
| A5 | `TestSourceScansSurviveCRLF` still present and passing | C2, D3 |
| A6 | Untracked files and `dist/` untouched | C4 |
| A7 | `core.autocrlf` unchanged at every scope | C3, D4 |
| A8 | 0147 carries an amendment pointing here; its Option B text is unedited | D5, F6 |

**A2 is the criterion that makes this record true or false, and it is the one
most likely to be glossed.** "It worked, tests pass" is not the claim — the
claim is that 1671 files changed on disk and *nothing* changed in git. Print the
`git status` output rather than asserting it.

**A5 is the one most likely to be dropped later**, not during this plan but in
the next cleanup pass, for the reason in C2.

## Rollout and Rollback

P1 has no rollback because it has nothing to roll back: no commit, no history,
no reachable state change. If the refresh somehow produced a wrong file, `git
checkout -- <path>` restores it from `HEAD`, which is the same content it
already had.

P2 is an ordinary docs commit, revertible.

Nothing ships. No other clone is affected — a fresh clone on any platform has
always produced LF, which is why CI has never seen any of this.

## Deferred (named, so they are not mistaken for oversights)

* **Other stale Windows checkouts.** Any clone made before `e929614`
  (2026-08-27) has the same staleness. Nothing in this repository can detect or
  fix that remotely; the recipe in P1 is the answer if it comes up.
* **A permanent CRLF check.** MADR open question 3 asks whether the census
  deserves to be a test. It is a shell walk over the whole tree, it would only
  ever fail on a stale checkout, and 0147 D5 already makes the tests immune —
  so it is deferred rather than added. If a second stale checkout appears, that
  is the evidence to revisit.
* **`internal/ws/op_timeout_test.go`'s nil-index panic**
  (`FindAllStringSubmatch(...)[0][1]`). Carried over from 0147's execution
  record; unrelated to line endings and still wants its own record.
* **`core.autocrlf` at system scope.** Left alone by D4. If a future repository
  without a `.gitattributes` is worked on from this host, that setting will
  matter — but that is that repository's problem to declare, not this one's.
