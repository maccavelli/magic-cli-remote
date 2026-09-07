---
status: proposed
date: 2026-09-07
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The CRLF is only in the working tree, so fixing it costs nothing to commit

## Context and Problem Statement

The Windows dev laptop's checkout holds 1671 tracked text files with CRLF line
endings. This has been deferred four times — 0118 first, then restated
without action in 0119, 0120 and 0147 — each time on the same reasoning: it is
a tree-wide change, therefore large, therefore someone else's problem.

That reasoning rests on a cost estimate nobody measured. This record measures
it, and the estimate is wrong by the largest possible margin: **there is
nothing to commit.** Every blob in `HEAD` is already LF. The CRLF exists only
in this working tree, git considers the tree clean, and the fix is a local
refresh with an empty diff.

MADR 0147 Option B stated the cost as "a 1668-file diff that touches nearly
every review" and rejected renormalisation partly on that basis. That sentence
is false, and this record exists partly to correct it.

### What was measured, not assumed

All measurements on `76a5023`, Windows 11 Home, Git for Windows 2.x,
`git status` clean throughout.

**The repository content is already correct.**

```console
$ git add --renormalize .
$ git status --porcelain | wc -l
0
```

Renormalising the entire tree stages **zero** changes. For every tracked file,
the content git would store already equals what it stores. There is no
normalisation commit to make, because normalisation has nothing to do.

**The divergence is working-tree-only, and a re-checkout fixes it.** Byte-level,
on `internal/ws/codex_handlers.go`:

```console
$ head -c 40 internal/ws/codex_handlers.go | od -c
0000000   p   a   c   k   a   g   e       w   s  \r  \n  \r  \n   i   m

$ git show HEAD:internal/ws/codex_handlers.go | head -c 40 | od -c
0000000   p   a   c   k   a   g   e       w   s  \n  \n   i   m   p   o

$ rm internal/ws/codex_handlers.go && git checkout -- internal/ws/codex_handlers.go
$ head -c 40 internal/ws/codex_handlers.go | od -c
0000000   p   a   c   k   a   g   e       w   s  \n  \n   i   m   p   o

$ git status --porcelain internal/ws/codex_handlers.go
```

Working tree CRLF, stored blob LF, LF after re-checkout, and `git status` clean
at every step.

**`git checkout-index -f` does not do it.** The obvious command for "rewrite the
working tree from the index" leaves an existing file untouched:

```console
$ git checkout-index -f -- internal/provider/acphttp/provider_test.go
$ head -c 8000 internal/provider/acphttp/provider_test.go | grep -qU $'\r' && echo CRLF
CRLF
```

The file must be *absent* for the checkout to write it. This is worth recording
because it is the first thing a reader will try, and its silent success looks
like a fix.

**The cause is a checkout that predates the policy.** `.gitattributes` was added
on 2026-08-27 in `e929614` ("fix(platform): secure providerauth dirs and enforce
LF on Windows") and declares `* text=auto eol=lf`. Attributes bind at checkout,
not retroactively: files written before that commit — under
`core.autocrlf=true`, which this host still has — kept their CRLF and no later
operation has rewritten them. Files that a subsequent pull happened to touch are
LF. Hence a *mixed* tree rather than a uniformly stale one.

**`core.autocrlf=true` is the system default, not a local choice.**

```console
$ git config --local  --get core.autocrlf   # (empty)
$ git config --global --get core.autocrlf   # (empty)
$ git config --system --get core.autocrlf
true
```

It comes from the Git for Windows installer. `.gitattributes` overrides it for
every tracked text file in this repository, so it is inert here — but it is why
the pre-`e929614` checkout looks the way it does.

**The state is mixed, not uniform.** By extension, of 1671 CRLF files:

| Extension | Files |
| --- | --- |
| `.go` | 490 |
| `.md` | 231 |
| `.dart` | 156 |
| `.json` | 68 |
| others (`.txt`, `.xml`, `.sh`, `.yaml`, …) | ~726 |

### Findings

**F1 — there is nothing to commit.** `git add --renormalize .` stages zero
changes. The stored content is already LF for every tracked file. Any plan
built around "a large normalisation commit" is solving a problem that does not
exist.

**F2 — the fix is a local working-tree refresh with an empty diff.** Verified
byte-level: `rm` followed by `git checkout --` converts a CRLF file to LF, and
`git status` reports clean before and after. Nothing enters history.

**F3 — `git checkout-index -f` is not the recipe.** It leaves existing files
alone; the file must be deleted first. Recorded because it fails silently and
would be read as success.

**F4 — the cause is a pre-`.gitattributes` checkout under the installer's
`core.autocrlf=true`.** `.gitattributes` (`* text=auto eol=lf`) landed
2026-08-27 in `e929614` and binds only at checkout, so files written earlier
kept CRLF. `core.autocrlf` is set at **system** scope, not by any choice made in
this repository.

**F5 — the cost is not cosmetic, and has already been paid once.** The CRLF
state broke `TestEveryAsyncDispatchedMethodIsInTheTable` (MADR 0147 F8): a scan
searching for the LF-only delimiter `"\n}\n"` in `codex_handlers.go` found
nothing. It also makes `gofmt -l` unusable locally — 490 `.go` files are
reported — and that noise has been mistaken for formatting drift more than once.

**F6 — MADR 0147 Option B's cost estimate was wrong.** It described
renormalisation as "a 1668-file diff that touches nearly every review" and
weighed that against the alternative. The diff is empty (F1). The option was
rejected for other, still-valid reasons — it fixes neither F1 nor F4 nor F7 of
that record — but its stated cost was not real.

**F7 — 0147 D5 made two tests tolerant, which is correct and is not a fix
here.** Normalising on read keeps those scans correct on *any* checkout,
including a contributor's, and must stay regardless of what this record
decides. It is defence for the tests, not a reason to leave the tree wrong.

**F8 — the deferral has outlived its justification.** 0118 deferred it; 0119,
0120 and 0147 each restated the deferral verbatim and moved on. Four records is
long enough for "we have not measured this" to stop being a reason.

## Decision Drivers

* **A fix with an empty diff and no history impact has almost no downside.** The
  usual objection to tree-wide changes — review noise, merge conflicts, blame
  churn — does not apply, because nothing is committed.
* **The failure mode is silent and recurring.** F5 already cost a debugging
  session. The next source-scanning test, or any tool that reads bytes rather
  than lines, hits the same wall with no warning.
* **`gofmt -l` should be usable locally.** A formatting check that reports 490
  false positives is a check nobody runs.
* **Do not confuse "the repo is wrong" with "my checkout is stale."** The
  repository has been correct since `e929614`. Only this machine is behind.

## Considered Options

* **A — Refresh the working tree; commit nothing.** (chosen)
* **B — Leave it; rely on MADR 0147 D5's read-time tolerance.**
* **C — Re-clone the repository.**
* **D — Renormalise and commit the result.**

## Decision Outcome

Chosen: **Option A.** It is the only option that removes the hazard, and it
costs one local command and no history.

### The decisions

**D1 — refresh the working tree in place, and commit nothing.** Delete the
tracked files and check them out again so `.gitattributes` writes them with LF.
`git status` must be clean before and after; if the refresh produces any staged
or unstaged change, **stop** — that would mean a file's content, not merely its
endings, differs from `HEAD`, which F1 says is impossible and would falsify this
record.

**D2 — use `rm` + `git checkout`, not `git checkout-index -f`.** The latter
leaves existing files untouched (F3).

**D3 — keep MADR 0147 D5's read-time normalisation.** It is not redundant: it
keeps those scans correct on any contributor's checkout, including one made
before that contributor's `.gitattributes` arrived. Removing it because the
local tree is now clean would re-create the bug for everyone else.

**D4 — do not change `core.autocrlf`.** It is set at system scope by the Git for
Windows installer (F4), and `.gitattributes` already overrides it for every
tracked text file here. Changing a system-scope setting to fix a
repository-scope symptom is a wider blast radius than the problem justifies, and
the repository's own declaration is the right place for this policy to live.

**D5 — record the correction to MADR 0147 Option B.** That record's cost
estimate is quoted in its Pros and Cons and would mislead the next reader (F6).
Amend it to point here.

### Consequences

* Good: `gofmt -l` becomes usable locally, and the class of failure in F5
  disappears rather than being tolerated.
* Good: no commit, no diff, no review burden, nothing to revert. The operation
  is invisible to every other clone.
* Good: the tree stops being *mixed*, which is the property that made F5 hard to
  reason about — the same test passed or failed depending on which files a
  recent pull had happened to rewrite.
* Neutral: the refresh must run on a clean tree, so it needs a moment when
  nothing is in progress. It takes seconds.
* Bad: a reader who sees "1671 files changed on disk" without reading this
  record may think something drastic happened. The empty `git status` is the
  reassurance, and the plan should say so out loud.
* Bad: this fixes one machine. Any other Windows checkout made before
  `e929614` has the same staleness and no way to learn it from here.

### Confirmation

```bash
# Before: mixed tree, git clean
git status --porcelain            # expect empty
# count CRLF files (expect ~1671)

# After the refresh
git status --porcelain            # expect empty — nothing entered history
gofmt -l $(git ls-files '*.go')   # expect empty
go test ./...                     # expect green
# count CRLF files                # expect 0

# And the guard that must NOT be removed (D3):
go test ./internal/ws/ -run TestSourceScansSurviveCRLF -count=1
```

## Pros and Cons of the Options

### A — Refresh the working tree; commit nothing (chosen)

* Good, because the diff is empty and the operation never leaves this machine.
* Good, because it removes the hazard rather than tolerating it, and F5 shows
  the hazard is real and has already been paid.
* Good, because it is reversible in the strongest sense: there is nothing to
  reverse.
* Neutral, because it must run on a clean tree.
* Bad, because it fixes only this checkout; nothing propagates the knowledge to
  another stale Windows clone.

### B — Leave it; rely on 0147 D5's tolerance

* Good, because it is free, and the two known scans are already immune.
* **The strongest argument for it:** the tolerance in D5 is the *correct* fix
  regardless — a contributor's checkout can be CRLF for reasons this repository
  cannot control, so the tests must cope either way. Once they cope, the
  remaining cost is only `gofmt -l` noise on one laptop, which is a preference,
  not a defect.
* Bad, because "the two known scans" is the operative phrase: F5 was found by
  accident, and the next byte-level reader gets no warning.
* Bad, because it leaves a permanently unusable local `gofmt`, and a check
  nobody can run is a check that is not protecting anything.

### C — Re-clone the repository

* Good, because a fresh clone is unambiguously correct and needs no recipe.
* Bad, because it discards local state — worktrees, hooks, `.git` config,
  branches, stashes — to fix line endings, which is a sledgehammer.
* Bad, because it teaches nothing: the next stale checkout gets the same advice
  with the same cost.

### D — Renormalise and commit

* Good, because it is what the corpus has assumed for four records, so it would
  at least be unsurprising.
* Bad, because **it is a no-op** (F1). `git add --renormalize .` stages nothing.
  There is no commit to make, and attempting one would produce an empty commit
  that documents a misunderstanding.
* Bad, because this is exactly the assumption that made the problem look
  expensive and kept it deferred since 0118.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Renormalising stages zero changes | `git add --renormalize .` then `git status --porcelain` — empty |
| Working tree CRLF, blob LF | `od -c` on `internal/ws/codex_handlers.go` vs `git show HEAD:` |
| `rm` + `git checkout --` yields LF, status clean | same file, byte-level before/after |
| `git checkout-index -f` does not rewrite | `internal/provider/acphttp/provider_test.go` stayed CRLF |
| 1671 CRLF tracked text files; 490 `.go`, 231 `.md`, 156 `.dart`, 68 `.json` | binary-safe scan over `git ls-files` |
| `.gitattributes` added 2026-08-27 | `git log --diff-filter=A -- .gitattributes` → `e929614` |
| `* text=auto eol=lf` | `.gitattributes:1` |
| `core.autocrlf=true` is system scope | `git config --system --get core.autocrlf` (local and global empty) |
| CRLF broke a real test | MADR 0147 F8; `op_timeout_test.go:124` |
| `gofmt -l` reports 490 `.go` files | extension census above |
| 0147 Option B claimed a 1668-file diff | `docs/0147-MADR-…md`, Pros and Cons, Option B |
| Deferral restated without action | `docs/0119-PLAN-…:48,237`, `docs/0120-PLAN-…:362`, `docs/0147-MADR-…` D6 |

### Related records

* **MADR 0118** — origin of the deferral this record closes.
* **MADR 0147** — *Make the Windows gate measure the code, not the shell, the
  PATH, or the checkout.* F8/F11 are the measured cost of the CRLF state; D5 is
  the read-time tolerance this record keeps (D3); Option B is the cost estimate
  this record corrects (D5, F6).
* **MADR 0116** — the Windows port, and `e929614`'s wider context of enforcing
  LF on Windows.

### Open questions for the plan

1. **What exact recipe deletes and restores the whole tree safely?** `rm` +
   `git checkout --` is verified per file (F2); the tree-wide form must not
   leave the checkout empty if interrupted, and must not touch untracked files.
   The plan must state the command and what happens if it dies halfway.
2. **Does anything outside `git ls-files` need care?** Untracked build output,
   `dist/`, and the Flutter build directories are not tracked and must be left
   alone; the plan should confirm the recipe cannot reach them.
3. **Should the count be asserted, or just observed?** A post-refresh CRLF count
   of zero is the clearest confirmation, but it is a shell census rather than a
   test. **[unverified]** whether it is worth a permanent check anywhere.
