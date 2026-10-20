---
status: accepted
date: 2026-10-20
decision-makers: Project Owner
consulted: "GitHub's workflow syntax reference (path filters) and its GITHUB_TOKEN trigger rule; the Actions timing API for run 39481886172; this repository's run history 2026-08-01 to 2026-10-20; go-selfupdate-lib's docs/decisions/0028-MADR-skip-full-ci-for-docs-only-pushes.md"
informed: anyone who pushes to this repository or cuts a tag
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Skip the full CI for a push or pull request that changes only Markdown, and check it with a docs workflow that also runs the tests that read Markdown

## Context and Problem Statement

Every push to `master`, and every pull request, runs all of `.github/workflows/ci.yml`
(`ci.yml:3-9`). A commit that changes only Markdown gets the same run as a code change: a record,
a phase closing, a report, an index row. About a fifth of the push runs on `master` are such
pushes (Facts, below).

The owner asked, on 2026-10-20, for this repository to stop running its full CI for pushes and
pull requests that change only Markdown, as go-selfupdate-lib had just done in its
`docs/decisions/0028-MADR-skip-full-ci-for-docs-only-pushes.md` (repository go-selfupdate-lib,
accepted and live). That record chose `paths-ignore: ['**.md']` on `ci.yml`'s `push` and
`pull_request` triggers, plus a one-job `docs.yml` that runs the Markdown checks `ci.yml` runs,
with the same commands. Its live check found a docs-only push ran only the Docs workflow, in
12 s, where the full run was 18 jobs and 15–20 minutes. Its run bills nothing, since that
repository is public, and its owner chose to go ahead for turnaround, less runner-queue exposure
and fewer runs.

This repository differs from that one in two ways that change the answer: Go tests in two places
read Markdown, and a second workflow, the flake ledger, feeds on `ci.yml`'s runs.

### Facts [observed 2026-10-20, read-only]

**The repository and its CI.**

* The repository is public; its default branch is `master` (GitHub API, `repos/…`).
* `ci.yml` is named `CI` (`ci.yml:1`). It runs on `push` to `master` and to tags `v*`, on
  `pull_request`, and on `workflow_dispatch` (`ci.yml:3-9`). Its concurrency group is per ref and
  cancels an older run, except on a tag (`ci.yml:15-17`).
* Its jobs (`ci.yml:36-733`): `go`; `go-native`, two legs (`linux/arm64` on `ubuntu-24.04-arm`,
  `windows/amd64` on `windows-latest`); `flutter`; `flutter-windows`; and, on a tag or a dispatch
  only, `android-apk`, `build` and `publish` (`ci.yml:422`, `:702`, `:721`). `ci.yml` defines no
  macOS job (`ci.yml:297-310`).
* **Minutes.** The Actions timing API for run `39481886172`, a push to `master`, reports 0
  billable ms for every job run: 4 Ubuntu and 2 Windows, `run_duration_ms` 333000. The run had 8
  jobs: 5 ran and 3 (`android-apk`, `build`, `publish`) were skipped. This repository's CI
  therefore bills no minutes, like go-selfupdate-lib's. Saving minutes is not a reason here.
* **How many pushes are docs-only.** Each push run on `master` was classified by the push's own
  diff, from the previous tip of `master` (the previous push run's head, or a ledger commit) to the
  run's head:
  * 2026-08-01 to 2026-10-20: 64 of 288 push runs (22 %) changed only `.md` files;
  * 2026-09-20 to 2026-10-20: 17 of 63 (27 %). Their wall time: median 6.5 min, range 5.5 to
    11.3 min.
  * From 2026-08-01, `ci.yml` also ran 19 times for pull requests and 16 times by dispatch.
* **The Markdown checks in CI.** The `go` job alone runs them, after setting up Node.js 24 and
  asserting its major version (`ci.yml:62-72`): `make check-records` (`ci.yml:77-78`, which runs
  `python3 scripts/check_records.py --check-all`, `Makefile:286-287`) and `make markdownlint-docs`
  (`ci.yml:80-81`, `npx markdownlint-cli2 --no-globs` over the user-facing docs,
  `Makefile:294-302`), as
  [0180-MADR-check-records-is-a-preflight-and-ci-gate.md](0180-MADR-check-records-is-a-preflight-and-ci-gate.md)
  decided. Skipping CI outright would skip both.

**Code that reads tracked Markdown.** No `//go:embed` names a Markdown file, the Flutter app
bundles none (`apps/mobile/pubspec.yaml`, `assets:`), and the tree-walking Go tests filter to
`.go` or `.json`. Two exceptions remain, and each makes a Markdown-only change able to fail CI:

* **The protocol spec guards.** `internal/protocol/doc_coverage_test.go:14` reads
  `docs/guides/protocol-v1.md`. `TestEventTypesAreDocumented` and `TestErrorCodesAreDocumented`
  check it against the code, and `TestSessionModeDangerousIsDocumented`
  (`internal/protocol/sessionmode_compat_test.go:75-76`) does the same for one field. This has
  failed CI before. `36cb45bd` (2026-10-03, "chore(docs): add markdown lint target") reflowed
  `protocol-v1.md` with 13 other Markdown files and the `Makefile`. The paragraph the guard parsed
  moved, and `TestEventTypesAreDocumented` failed in runs `37173869896` and `37177612995`, on
  `Go (windows/amd64)`, `Go (linux/arm64)` and the `go` job. The second run was a docs-only push
  (`b5ed5a30`, one PLAN file), so it reached the ledger as two `fail`/`fail` rows. `27956ea5`
  (2026-10-04) fixed the test. The reflow was pushed with a `Makefile` change, so it would still
  have run CI under a Markdown filter; a reflow of Markdown alone would not.
* **The home-path guard.** `TestTrackedFilesUseOnlyPlaceholderHomes`
  (`internal/homepaths_test.go:198-240`) reads every tracked text file, Markdown included, and
  fails on a home path whose account segment is not a placeholder
  ([0196-MADR-remove-the-development-account-name-and-guard-home-paths.md](0196-MADR-remove-the-development-account-name-and-guard-home-paths.md)).
  Records quote live evidence, and are where an account name has entered this public repository
  before. Under a filter alone, a record carrying one would reach `master` with nothing failing.

**The flake ledger**
([0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md)).

* **Trigger.** `.github/workflows/ci-flake-ledger.yml`, named `CI flake ledger`, runs on
  `workflow_run` of the workflow named `CI`, `types: [completed]`; on a daily `schedule`
  (`17 11 * * *`); and on `workflow_dispatch` (`ci-flake-ledger.yml:13-25`). It skips a fork's
  completions (`:57-58`).
* **What it writes.** It downloads the `ci-flake-*` artifacts that `go-native`'s retried legs
  upload (`ci.yml:272-295`). It appends them to `ci-flakes.tsv`, a TSV file at the root, not
  Markdown, and commits and pushes that one file to `master` with the job's `GITHUB_TOKEN`
  (`ci-flake-ledger.yml:133-141`). On a schedule it reads the newest completed `ci.yml` run
  through `actions/workflows/ci.yml/runs` (`:81`).
* **How often.** Since 2026-09-07 it has run 157 times (114 by `workflow_run`, 43 by
  `schedule`) and committed 18 times, each commit one line of `ci-flakes.tsv`. The commit on
  `origin/master` that this tree is behind, `057fe379`, is one of them: one line of
  `ci-flakes.tsv`, from run `39481886172`.
* **Its commits start no CI run.** GitHub: an event the repository's `GITHUB_TOKEN` causes, other
  than `workflow_dispatch` and `repository_dispatch`, does not create a new workflow run. The
  run lists for the ledger commits `b65a7438`, `82fb0356` and `a9843f59` hold no `push` run of
  `CI`. (The `CI` run that shares `057fe379`'s head is a `workflow_dispatch`, started 26 s after
  the commit.)
* **What docs-only runs gave it.** Of the ledger's 18 rows, 5 came from 4 docs-only push runs.
  Three were flakes (`fail` then `pass`): `TestACPConnectionSurvivesAStalledPump` (2026-09-07),
  `TestRegisterReplacement` (2026-09-24) and `TestExperimentalInitializeRetriesOnce`
  (2026-10-03). The other two were the `TestEventTypesAreDocumented` break above. A docs-only run
  tests the same code as the push before it, so it is an extra flake sample.
* **What counts runs.** 0143's Confirmation re-measures "the failure rate over completed runs",
  and promotes a test seen "3 or more times in 30 days" to its own MADR. Its PLAN's Phase 5, still
  open, gives the recount as `gh run list --limit 200 --json conclusion,headBranch,createdAt`
  ([0143-PLAN-ci-retries-once-and-records-every-retry.md](0143-PLAN-ci-retries-once-and-records-every-retry.md),
  "Verification"). That list has no `--workflow`, so it already includes the ledger's own runs,
  and would include a Docs workflow's runs too.

**GitHub's path filters** (workflow syntax reference, as go-selfupdate-lib's 0028 records):

* "Path filters are not evaluated for pushes of tags." A release tag keeps the full run.
* Branch and path filters must both be satisfied. `paths` and `paths-ignore` cannot both be set on
  one event. In `paths`, a later positive pattern re-includes a path an earlier `!` pattern
  excluded.
* A push of more than 1,000 commits, or a diff that times out, always runs.
* `workflow_dispatch` takes no path filter, so a release rehearsal (`ci.yml:695-698`) is unchanged.
* A workflow that a path filter skips leaves its checks "Pending", which blocks a pull request
  that requires them. No check is required here: the one ruleset, `Protect` (active, the default
  branch), holds only `deletion` and `non_fast_forward`, and `branches/master/protection` answers
  404.

**AGENTS.md** ("Tests") says: "No workflow edits without Mac permission." Approval of this pair is
that permission for the workflow change it names.

## Decision Drivers

* A docs-only change keeps every check that can fail on it: the records check, markdownlint, and
  the Go tests that read Markdown.
* A change to anything else keeps the full run, and so does every tag and every dispatch.
* The flake ledger keeps working: no lost rows from runs that happen, no runs triggered by its own
  workflow, and a measurement in 0143 that stays comparable.
* No third-party action, and nothing that hides a failure: a skipped job must not read as a pass.
* Simple enough that a later change cannot quietly disable CI for code.

## Considered Options

* **A.** `paths-ignore: ['**.md']` on `ci.yml`'s `push` and `pull_request` triggers, and nothing
  else.
* **B.** As A, plus a new `.github/workflows/docs.yml` running `ci.yml`'s two Markdown steps,
  `make check-records` and `make markdownlint-docs` (go-selfupdate-lib's choice, transposed).
* **C.** One workflow: a first job decides from `git diff --name-only` whether code changed, and
  every heavy job carries an `if:` on its output.
* **D.** `[skip ci]` in a docs-only commit's message.
* **E.** As B, and the docs job also runs the Go tests that read Markdown:
  `go test ./internal/protocol/` and `TestTrackedFilesUseOnlyPlaceholderHomes`.
* **F.** As B, but `ci.yml` uses `paths: ['**', '!**.md', 'docs/guides/protocol-v1.md']`, so a
  change to the protocol spec runs the full CI.
* **G.** As E, plus a daily `schedule` trigger on `ci.yml`, to give the ledger back the flake
  samples that docs-only pushes gave it.

## Decision Outcome

Chosen option: "E". The owner approved it as recommended on 2026-10-20, answering
"magic-cli-remote 0203" to "Which repos' pairs do you approve as recommended (MADR accepted, PLAN
in-progress, then Phase 0/records and Phase 1 per its plan)?". The recommendation approved is
option E, the new workflow named `Docs`, no change to `ci-flake-ledger.yml`, and the dated
amendment to 0143's MADR in the PLAN's P2. That approval is also the permission AGENTS.md
("Tests") requires for the workflow edit.

"E" was chosen because it is go-selfupdate-lib's B with
the one addition this repository's facts require. Two sets of Go tests read Markdown, and one of
them has failed CI on a Markdown change. The docs job runs both, at the cost of a Go setup in a
Linux job,
so a docs-only change keeps every check that can fail on it. `ci.yml`'s jobs stay untouched, and
tags and dispatches are never filtered.

**The flake ledger needs no change.** Its commits change a `.tsv` file and start no run, so the
filter neither affects them nor needs to. A docs-only push that `ci.yml` skips starts no `CI` run,
so no `workflow_run` event fires and no ledger run starts; nothing is lost from a run that
happens. The daily catch-up reads `ci.yml`'s own run list, which a Docs run never joins. The new
workflow is named `Docs`, not `CI`, because the ledger matches `workflow_run` by name.

**What changes for 0143 is its sample.** About a quarter of `CI`'s push runs go away (17 of 63 in
the last 30 days). Each remaining run still runs the full job set, so the failure rate per run
stays comparable. But there are fewer runs to see a flake, so the 3-in-30-days threshold trips
later. The recount should count `ci.yml`'s runs only (`--workflow ci.yml`), since the
unfiltered list holds the ledger's runs and would hold the Docs runs. The PLAN records both in
0143 as a dated amendment, so its Phase 5 recount is not misread.

### Consequences

* Good, because a docs-only push runs one Linux job, with no Windows or arm64 runner and no Flutter
  set-up, instead of 5 jobs and a median 6.5 minutes.
* Good, because a code push, every tag and every dispatch run as before.
* Good, because a docs-only push no longer cancels a code push's run still in progress
  (`ci.yml:15-17`); today a docs-only push within a few minutes of a code push replaces that
  run with one that tests the same code.
* Good, because the protocol spec guards and the home-path guard still run on a Markdown-only
  change.
* Neutral, because a push that changes code and Markdown runs both workflows, so the Markdown
  checks run twice.
* Neutral, because the head of `master` after a docs-only push carries a Docs status and no CI
  status; the code it holds is the code of the last push whose CI ran.
* Bad, because the flake ledger gets about a quarter fewer samples, and 0143's threshold and
  recount see fewer runs.
* Bad, because a required status check, if one is ever added, must not name a `ci.yml` job: a
  docs-only pull request would leave it "Pending". The record that adds one must say so.
* Bad, because there are two workflows to keep in step: a new test that reads Markdown must be
  added to the docs job, or a Markdown change can break it unseen until the next code push.

### Confirmation

* `actionlint` passes on `ci.yml`, `docs.yml` and `ci-flake-ledger.yml`. The tests that read
  `ci.yml` (`internal/updateclient/spec_test.go:163`, `scripts/assert-flutter-pin.sh`) still pass.
* Each of the docs job's commands fails on a scratch clone given one planted break, and passes on
  the unplanted clone.
* After the change lands, a docs-only push to `master` shows a `Docs` run, no `CI` run and no
  `CI flake ledger` run. A push that changes a non-Markdown file shows `CI`, and then a ledger
  run. The PLAN records the run IDs.
* The next release's tag push runs the full `CI`; that release's PLAN records it.

## Pros and Cons of the Options

### A. `paths-ignore` only

* Good, because it is one line per trigger.
* Bad, because a docs-only change then gets no records check, no markdownlint and none of the
  Markdown-reading tests. A broken link, a protocol-spec drift or an account name reaches `master`
  unchecked.

### B. `paths-ignore` and a docs workflow with the two Markdown steps

* Good, because the records check and markdownlint keep running, and `ci.yml` keeps its jobs.
* Good, because both filters are GitHub's own, documented behaviour.
* Bad, because it misses the protocol spec guards, which have failed on a Markdown change
  (`37173869896`), and the home-path guard, whose most likely input is a record.

### C. One workflow, gated jobs

* Good, because one file holds it all.
* Bad, because every heavy job gains an `if:`, and a wrong condition can skip CI for code with
  nothing to show it; a skipped job reads as neither pass nor fail.
* Bad, because the change-detection job runs on every push.

### D. `[skip ci]`

* Bad, because the global `prepare-commit-msg` hook writes the commit messages (AGENTS.md, "Commit
  messages"), so a person would have to add it by hand each time. It also skips every docs check.

### E. B, plus the Markdown-reading tests

* Good, because every check that can fail on a Markdown-only change still runs on one.
* Good, because `go test ./internal/protocol/` runs the whole package, so a new doc guard added
  there is covered without editing the workflow.
* Neutral, because the docs job sets up Go (cached) and compiles two test packages: longer than
  go-selfupdate-lib's 12 s Docs run (not measured here until P3), still one Linux job.
* Bad, because the list of tests that read Markdown lives in the workflow, and a new one elsewhere
  must be added by hand.

### F. `paths` that re-includes the protocol spec

* Good, because a change to `protocol-v1.md` gets the full CI, whatever reads it.
* Bad, because it does not cover the home-path guard, which reads every Markdown file, so the
  docs job would still have to run it.
* Bad, because a three-pattern `paths` list with an ordered re-include is easier to break than
  `paths-ignore`.

### G. E, plus a scheduled `ci.yml` run

* Good, because the ledger gets samples back, on a fixed cadence.
* Bad, because it adds runs back, which is the opposite of the request. Each of them tests code
  that already ran.
* Bad, because a scheduled run on unchanged code is a different sample from a push, which 0143's
  rate never counted.

## More Information

* The PLAN:
  [0203-PLAN-skip-full-ci-for-docs-only-pushes.md](0203-PLAN-skip-full-ci-for-docs-only-pushes.md).
* The model: go-selfupdate-lib's `docs/decisions/0028-MADR-skip-full-ci-for-docs-only-pushes.md`
  and `docs/decisions/0028-PLAN-skip-full-ci-for-docs-only-pushes.md` (repository
  go-selfupdate-lib; a relative link cannot reach them). The facts relied on are restated above:
  its option B, GitHub's tag rule for path filters, and its measured 12 s docs-only run.
* The CI being changed is described in `README.md`'s "CI / CD (GitHub Actions)" section, which
  still says "A single workflow, `ci.yml`".
* The ledger: [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md),
  its Amendment of 2026-09-06, and `ci-flakes.tsv`.

## Amendment — 2026-10-20 (actionlint already failed on `ci.yml`)

The Confirmation's first point, "`actionlint` passes on `ci.yml`, `docs.yml` and
`ci-flake-ledger.yml`", did not hold before this change. P1's run of actionlint v1.7.12 found one
finding on `ci.yml`, and the same command on the unchanged `ci.yml` of `8802be5e` found it too:
shellcheck's SC2012 ("Use find instead of ls to better handle non-alphanumeric filenames"), at
the `ls "$ANDROID_HOME/build-tools"` line of the `android-apk` job's "Assert release signature
(upload key, not debug)" step. That line came in with `64a0b7cd` (2026-08-02, "feat(ci): Add
release signing and signature assertion"). `docs.yml` and `ci-flake-ledger.yml` passed.

The PLAN put any change to `ci.yml`'s jobs or steps out of scope. Offered a choice, the owner
chose, on 2026-10-20, "Amend 0203: fix that line (Recommended)": this pair replaces that `ls`
with a `find` that selects the same `build-tools` directory and fails the same way when there is
none. It is the one change to `ci.yml`'s steps. The Confirmation stands as written, and is met
once that line is fixed. The decision, option E, is unchanged.
