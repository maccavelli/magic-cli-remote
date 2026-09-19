---
status: in-progress
date: 2026-09-18
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0161 — The mobile docs state the CI Flutter pin, not a stale 3.44 floor

Implements [0161-MADR-mobile-docs-state-the-ci-flutter-pin.md](0161-MADR-mobile-docs-state-the-ci-flutter-pin.md)
decisions D1–D4 (D4 from the 2026-09-18 amendment), closing findings F1–F5.

## Goal

1. `apps/mobile/README.md` and `docs/mobile-profiling.md` each name Flutter
   3.47.2 / Dart 3.13.2 and point at `FLUTTER_VERSION` in
   `.github/workflows/ci.yml`.
2. Outside `docs/spec` and `docs/decisions`, no Markdown file names a Flutter or
   Dart version other than the pin (MADR Confirmation §1 prints nothing).
3. After P1, `markdownlint-cli2` reports the same single finding in the two
   files as before: MD013 on the "**Linux keyring:**" paragraph of
   `apps/mobile/README.md`. After P2 it reports none.
4. Each phase's commit changes only the files that phase names.

## Scope

### In scope (the only files any phase may touch)

* `apps/mobile/README.md`: line 20 only.
* `docs/mobile-profiling.md`: line 27 only.
* `apps/mobile/README.md`: the "**Linux keyring:**" paragraph only (P2).

### Out of scope

* `apps/mobile/pubspec.yaml` `sdk: ^3.12.2`, which is a package floor and moves
  the lockfile (MADR F4, D3).
* Root `README.md`, which already agrees with CI (D3).
* `apps/mobile/README.md:9` (the provider list naming `goose`), which belongs
  to MADR 0160 P3.
* The rest of `apps/mobile/README.md`'s host-specific text ("This machine
  currently has no Android emulator", `$HOME/Android/Sdk`). Those lines describe
  an earlier host. They are a separate doc cleanup, not a version fix.
* ~~The existing MD013 finding on `apps/mobile/README.md`'s "**Linux
  keyring:**" paragraph.~~ Brought into scope as P2 by the 2026-09-18 amendment
  (MADR D4).

## Stability rule

P1 ends with MADR Confirmation §1–§3 run from the repository root in Git Bash,
with the stated results. It is docs-only: no Go, Dart or build command is
required, and none may be skipped in its place.

Commit discipline: stage exactly the two files, then `git commit --no-edit`.
The global `prepare-commit-msg` hook writes the message. First check that
`git rev-parse --path-format=absolute --git-path hooks` resolves to
`~/.global-git-hooks`. No `-m`, no `--amend`. `git push` and tags are not
permitted without an explicit instruction in the same turn.

This pair is committed on its own before P1 (bootstrap exception).

## Cross-cutting contracts

* **C1 — Two lines, two files.** No other line in either file changes. This is
  the contract most at risk: `apps/mobile/README.md` has other stale,
  host-specific text right next to line 20, and "while I'm here" is tempting.
  It is out of scope.
* **C2 — Text as written.** The replacement lines are MADR D1 and D2 verbatim.
* **C3 — No lint regression.** The finding count in the two files stays at 1
  through P1 and is 0 after P2. Findings are identified by the text of the
  offending line, not its line number, because both phases shift lines.

## Dependency and delivery order

```text
P1 ──► P2   (any host; no toolchain needed)
```

P1 is independent of MADR 0160. If 0160 P3 lands first, line 20 of
`apps/mobile/README.md` is unaffected, because 0160 edits line 9. Re-locate the
line with `git grep -n 'Flutter 3.44' apps/mobile/README.md` rather than
trusting the number.

## Implementation Steps

### P1 — Replace the two version lines (D1, D2, D3; closes F1, F2, F3, F4)

1. In `apps/mobile/README.md`, replace the line
   `- Flutter 3.44+ / Dart 3.12+` with the three lines in MADR D1.
2. In `docs/mobile-profiling.md`, replace the line
   `1. Flutter 3.44+ / Dart 3.12+ (\`flutter doctor\`).` with the two lines in
   MADR D2.
3. Change nothing else (C1, and D3 closes F4 by leaving `pubspec.yaml` alone).

**Verification:**

```bash
git grep -n -E 'Flutter 3\.[0-9]+|Dart ?(≥|>=)? ?3\.[0-9]+' -- '*.md' ':!docs/spec' ':!docs/decisions' \
  | grep -v -E 'Flutter \*{0,2}3\.47\.[2x]|Dart \*{0,2}(≥ )?3\.13\.2'      # → nothing
git grep -n 'FLUTTER_VERSION' -- apps/mobile/README.md docs/mobile-profiling.md   # → 2 lines
markdownlint-cli2 2>&1 | grep -cE '^(apps/mobile/README.md|docs/mobile-profiling.md):'   # → 1
git diff --stat                                                          # → 2 files changed
```

Then stage the two files and commit per the Stability rule.

### P2 — Wrap the over-long "Linux keyring" paragraph (D4; closes F5)

Owner instruction 2026-09-18: "Fix the lint and the line number".

1. In `apps/mobile/README.md`, re-wrap the paragraph that starts
   `**Linux keyring:**` so that no line exceeds 80 characters. Change no words
   and no punctuation. A soft-wrapped paragraph renders identically.
2. Change nothing else in the file (C1 applies to P2 as well).

**Verification:**

```bash
markdownlint-cli2 2>&1 | grep -cE '^(apps/mobile/README.md|docs/mobile-profiling.md):'   # → 0
git diff --word-diff=porcelain -- apps/mobile/README.md | grep -E '^[-+][^-+]'           # → nothing: only line breaks moved
git diff --stat                                                                            # → 1 file changed
```

Then stage `apps/mobile/README.md` alone and commit per the Stability rule.

## Verification (whole plan)

Same as P1 and P2, plus MADR Confirmation §4 after each phase's commit:

```bash
git diff --name-only HEAD~1 -- ':!docs/spec'   # → the two files
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | No non-pin Flutter/Dart version in Markdown outside the decision records | D1, D2 (§1) |
| A2 | Both edited lines name `FLUTTER_VERSION` | D1, D2 (§2) |
| A3 | Lint findings in the two files stay at 1 after P1 | C3 (§3) |
| A5 | Lint findings in the two files are 0 after P2, with no word changed | D4 (Amendment §3) |
| A4 | The commit touches only the two files; `pubspec.yaml` and root README unchanged | D3, C1 (§4) |

A4 is the criterion most likely to be dropped quietly, through C1: the stale
host-specific lines beside line 20 invite a wider cleanup.

## Rollout and Rollback

Docs only; nothing ships. Rollback is `git revert` of the P1 or P2 commit.

## Deferred (named, so they are not mistaken for oversights)

* **Host-specific text in `apps/mobile/README.md`** ("This machine currently has
  no Android emulator", "Android SDK is already installed at
  `$HOME/Android/Sdk`"). It describes a single earlier host. Rewriting it into
  host-neutral setup steps is its own doc change.
* **A Windows/WSL developer-setup doc.** None exists. The 2026-09-18 install on
  the Windows host found that Android cmdline-tools 23.0's `sdkmanager.bat`
  splits package names at `;`, which breaks Gradle's NDK install, and pinned
  cmdline-tools 19.0. That belongs in a setup doc, which would need its own
  record.
* **A single source for the version.** Generating the doc lines from
  `FLUTTER_VERSION` would end the repetition that MADR Bad-consequence names.
  It is not worth tooling for four lines yet.

## Execution record (2026-09-18)

P1 ran on the Windows host at the owner's "proceed", in one commit, `7c4479b`,
touching exactly `apps/mobile/README.md` and `docs/mobile-profiling.md`
(2 files, +5 −2). This pair was committed before it in `5b0153b`, together with
the 0159 and 0160 record revisions at the owner's request. That commit held
only files under `docs/spec`, so the bootstrap exception held.

Results: §1 printed nothing; §2 printed two lines, one per file; §3 counted 1;
§4 listed the two files. A1–A4 met.

What the plan predicted incorrectly:

* **The lint baseline's line number moved.** The finding cited as
  `apps/mobile/README.md:50` MD013 reported at line 52 after P1, because D1
  replaces one line with three. The count check (§3) was unaffected. The
  location in the Goal, C3 and the MADR's evidence was not. A baseline pinned to
  a line number in a file the plan edits should be stated as a count, or by
  the text of the offending line.
* Nothing else. `markdownlint-cli2` lints the whole tree from its config's
  `globs` whatever paths are passed, which is why §3 filters by file name. That
  had been measured before the plan was written.
