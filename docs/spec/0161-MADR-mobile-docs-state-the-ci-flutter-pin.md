---
status: accepted
date: 2026-09-18
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The mobile docs state the CI Flutter pin, not a stale 3.44 floor

## Context and Problem Statement

Two developer-facing docs tell a reader to install "Flutter 3.44+ / Dart 3.12+".
CI pins Flutter 3.47.2 and fails the build when `pubspec.lock` does not
reproduce under exactly that version. A developer who follows those docs gets a
toolchain that CI's lockfile gate is designed to reject.

### What was measured, not assumed

All measured 2026-09-18 at `b3d3355`.

* `.github/workflows/ci.yml:24-27` sets `FLUTTER_VERSION: "3.47.2"`, commented
  "Bump deliberately; keep in step with the version developers run locally".
* `.github/workflows/ci.yml:504-517` (MADR 0127 D7 gate 1) fails when
  `flutter pub get` changes `pubspec.lock`, with the error "Run 'flutter pub
  get' locally on that exact version".
* Root `README.md:238` says "Flutter 3.47.x / Dart ≥ 3.13.2 (CI pins Flutter
  3.47.2)", and `README.md:1415` says "Flutter 3.47.2 pinned".
* `apps/mobile/README.md:20` says "Flutter 3.44+ / Dart 3.12+".
  `docs/mobile-profiling.md:27` says "Flutter 3.44+ / Dart 3.12+ (`flutter
  doctor`)".
* Commit `244636a` (2026-09-01, "bump flutter version to 3.47.2") changed only
  `.github/workflows/ci.yml`, `README.md` and `apps/mobile/.metadata`
  (`git show --stat 244636a`). The two docs above were not part of it.
* `git grep -n -E '3\.4[0-9]\.[0-9]+|Dart ?(≥|>=)? ?3\.1[0-9]' -- '*.md'
  ':!docs/spec' ':!docs/decisions'` finds only these four lines: the two root
  README lines, which agree with CI, and the two stale ones.
* Flutter 3.47.2 bundles Dart 3.13.2 (`flutter --version` on this host, in WSL
  and on wonder).
* `apps/mobile/pubspec.yaml` declares `sdk: ^3.12.2`. That is the Dart language
  floor for the package, not the toolchain developers should install.
* `markdownlint-cli2` v0.23.2 reports one existing finding across the two
  files: `apps/mobile/README.md:50` MD013.

**[unverified]** Whether `flutter pub get` under 3.44.6 would change this
`pubspec.lock`. wonder ran 3.44.6 until 2026-09-18 but was upgraded without
checking first. The decision does not depend on it: the gate's own error text
asks for "that exact version".

### Findings

**F1 — Two docs name a toolchain CI does not use.** `apps/mobile/README.md:20`
and `docs/mobile-profiling.md:27` say 3.44+ / 3.12+. CI and the root README say
3.47.2 / 3.13.2.

**F2 — "3.44+" is the wrong kind of statement.** The lockfile gate wants the
exact pinned version, so any "N+" floor tells the reader that versions CI treats
as wrong are acceptable.

**F3 — The drift came from a bump that left two docs behind.** `244636a`
changed three files, and nothing lists which docs repeat the version.

**F4 — `pubspec.yaml`'s `sdk: ^3.12.2` is not stale.** It is a
package-compatibility floor, and changing it is a dependency change that moves
the lockfile.

## Decision Drivers

* A new developer following either doc should end up with a toolchain that
  passes CI's lockfile gate.
* The next Flutter bump should be able to find every doc that repeats the
  version.
* Keep the change to documentation only.

## Considered Options

* A — State the exact pin and name where it lives.
* B — Match the root README's "Flutter 3.47.x / Dart ≥ 3.13.2".
* C — Give no number and point only at `FLUTTER_VERSION` in `ci.yml`.

## Decision Outcome

Chosen option: **A**, because it gives the reader the version to install and
tells the next person bumping Flutter where the authoritative value lives.

### The decisions

**D1.** Replace `apps/mobile/README.md:20` with:

```markdown
- Flutter **3.47.2** / Dart **3.13.2** — the CI pin (`FLUTTER_VERSION` in
  `.github/workflows/ci.yml`). Use exactly this version: CI fails when
  `flutter pub get` changes `pubspec.lock`.
```

**D2.** Replace `docs/mobile-profiling.md:27` with:

```markdown
1. Flutter **3.47.2** / Dart **3.13.2**, the CI pin (`FLUTTER_VERSION` in
   `.github/workflows/ci.yml`); check with `flutter --version`.
```

**D3.** Leave `apps/mobile/pubspec.yaml`'s `sdk: ^3.12.2` and the root
`README.md` unchanged (F4; the root README already agrees with CI).

### Consequences

* Good, because both docs now name a toolchain that passes the lockfile gate.
* Good, because each doc points at `FLUTTER_VERSION`, so a future bump has
  something to search for.
* Bad, because the number is still written in four places, and the next bump
  must update them all. Confirmation §1 is the check that catches a missed one.
* Neutral, because `apps/mobile/README.md` is also in MADR 0160 P3's scope, for
  its provider list at line 9. The two edits touch different lines.

### Confirmation

```bash
# 1. No doc outside the decision records names a Flutter or Dart version other
#    than the pin.
git grep -n -E 'Flutter 3\.[0-9]+|Dart ?(≥|>=)? ?3\.[0-9]+' -- '*.md' ':!docs/spec' ':!docs/decisions' \
  | grep -v -E 'Flutter \*{0,2}3\.47\.[2x]|Dart \*{0,2}(≥ )?3\.13\.2'
#   → no output

# 2. Both edited lines point at the pin.
git grep -n 'FLUTTER_VERSION' -- apps/mobile/README.md docs/mobile-profiling.md
#   → exactly two lines, one per file

# 3. No new lint findings in the two files (baseline: 1, README.md:50 MD013).
markdownlint-cli2 2>&1 | grep -cE '^(apps/mobile/README.md|docs/mobile-profiling.md):'
#   → 1

# 4. Docs only.
git diff --name-only HEAD~1 -- ':!docs/spec'
#   → apps/mobile/README.md, docs/mobile-profiling.md
```

## Pros and Cons of the Options

### A — State the exact pin and name where it lives (chosen)

* Good, because the reader gets an installable version and its source.
* Good, because the wording matches what the lockfile gate enforces.
* Bad, because it repeats the number, which is how F3 happened.

### B — Match the root README's "Flutter 3.47.x / Dart ≥ 3.13.2"

* Good, because every doc would then say the same thing.
* Good, because patch bumps inside 3.47 would not make it stale.
* Bad, because "3.47.x" still admits versions the lockfile gate may reject.
  That is F2's problem at a smaller scale.

### C — Give no number and point only at `FLUTTER_VERSION`

* Good, because it cannot go stale. This is the strongest argument against A.
* Bad, because a reader has to open a CI workflow to learn what to install,
  and the prerequisites list is where they look for that.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| CI pin 3.47.2, "keep in step" comment | `.github/workflows/ci.yml:24-27` |
| Lockfile gate demands the exact version | `.github/workflows/ci.yml:504-517`; MADR 0127 D7 |
| Root README agrees with CI | `README.md:238,1415` |
| Two stale lines | `apps/mobile/README.md:20`; `docs/mobile-profiling.md:27` |
| Bump commit left them behind | `git show --stat 244636a` |
| Only four version lines exist in the docs | `git grep` in "What was measured" |
| 3.47.2 bundles Dart 3.13.2 | `flutter --version` on this host, in WSL and on wonder, 2026-09-18 |
| Lint baseline: one finding, README.md:50 | `markdownlint-cli2` v0.23.2 |

### Related records

* MADR 0127 D7: the lockfile-reproducibility gate this record lines the docs
  up with.
* [0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md):
  also edits `apps/mobile/README.md` (P3), and relies on a 3.47.2 "Flutter
  host" for its P4.

### Open questions for the plan

None.

## Observed — execution results (2026-09-18)

D1 and D2 landed verbatim in `7c4479b`; D3 held, with `pubspec.yaml` and the
root README untouched. Confirmation §1–§4 passed as written. The only surprise
is the one recorded in PLAN 0161's execution record: the existing MD013 finding
moved from line 50 to line 52.

## Amendment — 2026-09-18: fix the MD013 finding, and stop citing it by line number

Owner instruction: "Fix the lint and the line number". The original text above,
including "`apps/mobile/README.md:50` MD013", is left as measured at `b3d3355`.

**F5 — The one lint finding in scope was left in place, and was cited by a line
number that the fix itself moved.** `markdownlint-cli2` v0.23.2 reports MD013
(248 characters, limit 200) on the single-line paragraph beginning
"**Linux keyring:**" in `apps/mobile/README.md`. It was line 50 at `b3d3355` and
line 52 after `7c4479b`, because D1 turned one line into three. Confirmation §3
and the evidence index named the old number.

**D4.** Re-wrap that paragraph so that no line exceeds 80 characters, with no
change to its words or punctuation. Identify lint findings by the text of the
offending line from now on, not by line number.

Confirmation §3 is superseded by:

```bash
# 3 (amended). No lint findings in the two files.
markdownlint-cli2 2>&1 | grep -cE '^(apps/mobile/README.md|docs/mobile-profiling.md):'
#   → 0
# and the re-wrap changed no words:
git diff --word-diff=porcelain HEAD~1 -- apps/mobile/README.md | grep -E '^[-+][^-+]'
#   → nothing
```

Option considered and rejected: exempt the file from MD013 or raise
`line_length` in `.markdownlint-cli2.jsonc`. That clears the report without
fixing the line, and the config applies to every Markdown file in the tree.
