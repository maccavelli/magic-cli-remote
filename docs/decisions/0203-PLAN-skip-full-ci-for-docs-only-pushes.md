---
status: in-progress
date: 2026-10-20
associated-madr: "0203-MADR-skip-full-ci-for-docs-only-pushes.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement skipping the full CI for docs-only pushes, with a docs workflow that runs the Markdown-reading tests

Associated MADR: [0203-MADR-skip-full-ci-for-docs-only-pushes.md](0203-MADR-skip-full-ci-for-docs-only-pushes.md)
(option E, accepted by the owner on 2026-10-20; see "Approval (2026-10-20)" under
"Execution record").

* **Approval.** Nothing below runs until the owner accepts the MADR and approves this plan
  ("proceed", or a phase by name). Approval is also the permission AGENTS.md ("Tests") requires
  for a workflow edit.
* **If the owner chooses another option,** the MADR's outcome and this plan are amended first.
  For B, P1 step 2 drops its two Go steps. For A, P1 step 2 is dropped. For F, P1 step 1 uses
  `paths:` instead.

## Goal

A push to `master`, or a pull request, that changes only Markdown runs one Linux job, `Docs`,
instead of `ci.yml`'s five. That job runs every check that can fail on Markdown: the records
check, markdownlint, the protocol spec guards and the home-path guard. Any other change, every
`v*` tag and every dispatch run `ci.yml` as before. The flake ledger works unchanged, and 0143
records how its sample changed.

## Scope

### In scope

| Phase | What | Commit |
| :--- | :--- | :--- |
| P0 | this pair and its `docs/README.md` rows | records only |
| P1 | `ci.yml`'s two filters and their comment; the new `.github/workflows/docs.yml` | CI |
| P2 | `README.md`'s CI section; a dated amendment in 0143's MADR | docs |
| P3 | the live checks, and closing | records |

### Out of scope

* Any change to `ci.yml`'s jobs or steps, or to `.github/workflows/ci-flake-ledger.yml`. The MADR
  finds the ledger needs none. *(Amended 2026-10-20, Deviation 2: except the one SC2012 line in
  P1 step 5.)*
* `ci-flakes.tsv` and the ledger scripts.
* The rest of `README.md`'s stale CI table: its job names, its `release` job and its Flutter
  version. Only the sentence naming the workflows changes here.
* `make markdownlint-docs`'s unpinned `npx markdownlint-cli2`. The docs job copies it as `ci.yml`
  runs it.
* Required status checks: none exist (the `Protect` ruleset holds `deletion` and
  `non_fast_forward` only).
* A release. This change is CI and documents only.

## Rules

* AGENTS.md's gate, records and identifier rules: no hostname, account name or real home path in
  any committed file. Plants are described here, not written out; the home-path plant uses a
  made-up name, in the scratch clone only.
* `actionlint` on every changed workflow.
* A new check is seen failing on a scratch clone outside the repository before it is relied on.
  This repository has no plant helper, so each plant is an asserting Python edit in a
  `git clone` under the session scratchpad (`$SCRATCH` below), never in the tree.
* Each phase is one commit, `git commit --no-edit`, on the owner's word. Each push is on the
  owner's word.

## Implementation Steps

### Phase P0: records

This pair, `status: proposed`. Its two rows in `docs/README.md`, written by
`python3 scripts/check_records.py --write-index` (the generated block is never hand-edited).
`make check-records` clean.

On approval, the MADR becomes `accepted` with the chosen option, and this plan `in-progress`.

### Phase P1: the filters and the docs workflow

1. **`ci.yml`.** `on:` becomes:

   ```yaml
   on:
     # 0203-MADR-skip-full-ci-for-docs-only-pushes.md. A change to Markdown alone
     # runs docs.yml instead. GitHub does not evaluate path filters for a tag
     # push, so every v* tag runs this workflow in full, and workflow_dispatch
     # takes no filter.
     push:
       branches: [master]
       tags:
         - "v*"
       paths-ignore: ['**.md']
     pull_request:
       paths-ignore: ['**.md']
     workflow_dispatch:
   ```

   Nothing else in `ci.yml` changes. The workflow keeps the name `CI`, which the ledger's
   `workflow_run` names.

2. **`.github/workflows/docs.yml`, new.** Its Node, records and markdownlint steps are `ci.yml`'s
   (`ci.yml:62-81`), copied unchanged, as is its Go set-up (`ci.yml:83-88`). Its two test
   steps are new commands, narrowing `ci.yml`'s `go test -race ./...` (`ci.yml:134-135`) to the
   tests that read Markdown.

   ```yaml
   name: Docs

   # Checks a change to Markdown alone, which ci.yml skips: the records check
   # and markdownlint, with ci.yml's commands, and the Go tests that read
   # Markdown (docs/guides/protocol-v1.md, and every tracked file for home
   # paths). A change to anything else runs ci.yml, and so does every tag
   # (docs/decisions/0203-MADR-skip-full-ci-for-docs-only-pushes.md). Not named
   # "CI": ci-flake-ledger.yml's workflow_run matches that name.
   on:
     push:
       branches: [master]
       paths: ['**.md', '.markdownlint-cli2.jsonc', '.github/workflows/docs.yml']
     pull_request:
       paths: ['**.md', '.markdownlint-cli2.jsonc', '.github/workflows/docs.yml']

   concurrency:
     group: docs-${{ github.ref }}
     cancel-in-progress: true

   env:
     FORCE_JAVASCRIPT_ACTIONS_TO_NODE24: true
     NODE_VERSION: "24"

   permissions:
     contents: read

   jobs:
     docs:
       name: Docs (records, markdownlint, Markdown-reading tests)
       runs-on: ubuntu-latest
       timeout-minutes: 15
       steps:
         - name: Checkout
           uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
           with:
             persist-credentials: false

         - name: Set up Node.js (LTS 24)
           uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7.0.0
           with:
             node-version: ${{ env.NODE_VERSION }}
             check-latest: true

         - name: Assert Node.js major version
           run: |
             node -v
             major="$(node -p "process.versions.node.split('.')[0]")"
             test "$major" = "24" || { echo "expected Node 24, got $(node -v)" >&2; exit 1; }

         - name: Check records
           run: make check-records

         - name: Markdownlint (user-facing docs)
           run: make markdownlint-docs

         - name: Set up Go
           uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
           with:
             go-version-file: go.mod
             cache: true

         # The whole package, so a doc guard added to it later is covered.
         - name: Protocol spec guards (docs/guides/protocol-v1.md)
           run: go test -count=1 ./internal/protocol/

         # 0196-MADR: reads every tracked text file, Markdown included.
         - name: Home-path guard (every tracked file)
           run: go test -count=1 -run '^TestTrackedFilesUseOnlyPlaceholderHomes$' ./internal/
   ```

3. **Seen failing.** In `$SCRATCH`, `git clone --quiet` the repository, copy the P1 working
   files in, and run the docs job's four commands (`make check-records`,
   `make markdownlint-docs`, `go test -count=1 ./internal/protocol/`, and the home-path test
   line). Output goes to `$SCRATCH`, with each exit status captured before any filter. Each
   plant is an asserting Python edit, made one at a time, and the clone is reset between them:

   | Plant | Expected to fail | And only |
   | :--- | :--- | :--- |
   | none (the clone as cloned) | nothing: all four exit 0 | |
   | a link to a missing file appended to `docs/guides/config.md` | `make check-records`, `broken relative link:` | that check |
   | a `##Heading` line (no space) appended to `docs/guides/config.md` | `make markdownlint-docs`, MD018 | that check |
   | `` `session_status`, `` removed from the paragraph starting `` Event `type` values: `` in `docs/guides/protocol-v1.md` | `TestEventTypesAreDocumented` | the protocol step |
   | a home path whose account segment is a bare name not in `placeholderHomes`, appended to a tracked record | `TestTrackedFilesUseOnlyPlaceholderHomes` | the home-path step |
   | `${{ github.reff }}` in a copy of `docs.yml` | `actionlint` | |

   Each failure is recorded with its message. The account name in the fourth plant is made up,
   and appears in no committed file.

4. **Checks, on the tree:**
   * `actionlint .github/workflows/ci.yml .github/workflows/docs.yml .github/workflows/ci-flake-ledger.yml`;
   * the tests that read `ci.yml` still pass: `go test -count=1 ./internal/updateclient/` and
     `./scripts/assert-flutter-pin.sh`;
   * `make check-records`.

   The filters themselves are proven only on GitHub, at P3.

5. **Added 2026-10-20, Deviation 2: `ci.yml`'s SC2012 line.** In the `android-apk` job's
   "Assert release signature (upload key, not debug)" step, the `ls "$ANDROID_HOME/build-tools"`
   that picks the newest build-tools directory becomes an equivalent `find`, so step 4's
   actionlint passes on all three workflows. The same directory is chosen (`sort -V | tail -1`
   is kept), names starting with `.` stay excluded as `ls` excludes them, a symlinked
   `build-tools` is still followed (`find -H`), and a missing directory still fails the step.
   Checked by running the old and new lines on scratch directories, and actionlint re-run. This
   step overrides "Out of scope"'s first bullet for that one line only.

### Phase P2: the documents

1. **`README.md`, "CI / CD (GitHub Actions)".** The sentence "A single workflow, `ci.yml`, runs
   on push to `master`, pull request, tag `v*`, or manual dispatch" becomes three facts:
   * `ci.yml` runs on those events, except that a push or pull request changing only Markdown
     skips it, while a tag always runs it in full;
   * `docs.yml` runs for a Markdown change: the records check, markdownlint, and the Go tests
     that read Markdown;
   * `ci-flake-ledger.yml` runs after each `CI` run, and daily, and commits retried tests to
     `ci-flakes.tsv`.

   It links the MADR.
2. **[0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md),
   a dated amendment.** It states:
   * from P1's commit on, a docs-only push or pull request starts no `CI` run, and so no ledger
     run. It names the commit;
   * in the 30 days before, that was 17 of 63 push runs, and 5 of the ledger's 18 rows came from
     such runs;
   * Phase 5's recount counts `CI` runs only: `gh run list --workflow ci.yml --limit 200 --json
     conclusion,headBranch,createdAt`. The unfiltered list holds the ledger's runs and the Docs
     runs;
   * each remaining run has the full job set, so the per-run rate stays comparable, but the
     3-in-30-days threshold sees fewer runs.

   The amendment cites this MADR. 0143's PLAN text is not rewritten.
3. **Checks:** `make check-records` and `make markdownlint-docs` clean. This commit changes
   Markdown only, so its push is P3's docs-only test.

### Phase P3: live checks and closing

1. **A push with a non-Markdown change.** P1's push changes two workflow files. Expected: both
   `CI` and `Docs` start, and a `CI flake ledger` run follows `CI`'s completion. The run IDs and
   conclusions are recorded, along with the Docs run's wall time.
2. **A docs-only push.** P2's push. Expected, from
   `gh api "repos/maccavelli/magic-cli-remote/actions/runs?head_sha=<P2 sha>"`: one `Docs` run, no
   `CI` run, and no `CI flake ledger` run triggered by `workflow_run`. The run ID is recorded.
3. **The ledger's catch-up.** The next scheduled `CI flake ledger` run after P2 succeeds. It
   reads the newest completed `ci.yml` run. Its ID is recorded.
4. **Tags.** Not exercised here. The next release's tag runs the full `CI`; that release's PLAN
   records it (V4).
5. **Closing.** `### Verification, at closing`, this plan `complete`, and the index regenerated.

## Verification

* **V1 (filters):** `ci.yml` has `paths-ignore: ['**.md']` on `push` and `pull_request`, and keeps
  `tags` and `workflow_dispatch`. `docs.yml` exists with the seven steps after checkout, and is
  named `Docs`. `actionlint` passes on all three workflows, and the `ci.yml` readers pass (P1).
* **V2 (seen failing):** each planted break fails its own check and no other, and the unplanted
  clone passes all four (P1).
* **V3 (live):** a docs-only push ran only `Docs`, with no `CI` or ledger run. A push with
  non-Markdown files ran `CI`, then the ledger (P3).
* **V4 (tags):** the next tag runs the full `CI`, left to that release.
* **V5 (ledger):** the scheduled ledger run after the change succeeds. 0143 carries the dated
  amendment (P2, P3).
* **V6 (docs):** `README.md`'s CI section names the three workflows; `make check-records` and
  `make markdownlint-docs` clean (P2).

## Rollout and Rollback

* **Rollout:** P1 then P2 on `master`, each pushed on the owner's word. P1 first, so the Docs
  workflow exists before the first push it skips.
* **Rollback:** `git revert` of P1 restores the full CI for every push and removes `docs.yml`. The
  ledger needs nothing, since it never depended on the split. If P2's amendment has landed, a
  further dated amendment in 0143 says when docs-only pushes resumed running `CI`.

## Execution record

### Approval (2026-10-20)

The owner approved this pair as recommended, answering "magic-cli-remote 0203" to "Which repos'
pairs do you approve as recommended (MADR accepted, PLAN in-progress, then Phase 0/records and
Phase 1 per its plan)?". The recommendation is option E, the new workflow named `Docs`, no change
to `ci-flake-ledger.yml`, and the dated 0143 amendment in P2. It is also the permission AGENTS.md
("Tests") requires for the workflow edit. P0, P1 and P2 were executed under it, in the working
tree, each left for the owner to commit on its own. P3 needs pushes, which wait for the owner's
word.

### P0, 2026-10-20: records

Starting commit `8802be5e`, one commit behind `origin/master` (`057fe379`, a ledger commit that
changes only `ci-flakes.tsv`; not pulled here).

* The MADR is `accepted`, its Decision Outcome "Chosen option: "E"" with the owner's words. This
  plan is `in-progress`, with the approval entry above.
* The index was regenerated with `python3 scripts/check_records.py --write-index`, first on a
  scratch clone with the three records files copied in, then on the tree. The generated table has
  no status column, so the two 0203 rows did not change, and neither did anything else:

  ```text
  write-index=0
  diff=0
  ```

  (`diff` of `docs/README.md` before and after, on the clone and on the tree.)
* `make check-records`:

  ```text
  ==> records and docs links
  check-records=0
  ```

### P1, 2026-10-20: the filters and the docs workflow

**Steps 1 and 2.** `ci.yml`'s `on:` block and the new `.github/workflows/docs.yml` were written
as this plan gives them. A Python script in the session scratch directory extracted the two YAML
blocks from this plan, removed their three-space indent, and compared them with the files:

```text
ci.yml on: block matches plan: True
docs.yml matches plan: True
exit=0
```

The same script, run on a copy holding the actionlint plant below, printed
`docs.yml matches plan: False` and exited 1. `git diff --stat .github/` shows
`ci.yml | 6 ++++++`: nothing else in `ci.yml` changed.

**Step 3, seen failing.** A `git clone --quiet` of the repository in the session scratch
directory, with the P0 and P1 working files copied in and added to the clone's index, so the
home-path guard reads them. A Python script ran the docs job's four commands once unplanted and
once per plant, writing each command's output to a file and taking its exit status from the
process, not through a filter. Each plant was an
asserting edit; the planted file was restored from a saved copy, compared byte for byte, and
`git status --porcelain` asserted clean of unstaged changes before the next plant. Results:

```text
none check-records=0 markdownlint-docs=0 protocol=0 home-path=0
broken-link check-records=2 markdownlint-docs=0 protocol=0 home-path=0
md018 check-records=0 markdownlint-docs=2 protocol=0 home-path=0
event-type check-records=0 markdownlint-docs=0 protocol=1 home-path=0
home-path check-records=0 markdownlint-docs=0 protocol=0 home-path=1
```

Each plant failed its own check and no other, and the unplanted clone passed all four (V2). The
failures (`go test`'s tabs are shown as two spaces):

* A link to a missing file appended to `docs/guides/config.md`:

  ```text
  broken relative link: docs/guides/config.md:855: no-such-file-0203.md
  1 broken relative link(s)
  make: *** [check-records] Error 1
  ```

* A `##Heading` line appended to `docs/guides/config.md`:

  ```text
  Summary: 1 issue in 1 file
  docs/guides/config.md:855:1 error MD018/no-missing-space-atx No space after hash on atx style heading [Context: "##Heading"]
  make: *** [markdownlint-docs] Error 1
  ```

  Unplanted, the same command ended `Linting: 54 files` / `Summary: 0 issues in 0 files`.
* `` `session_status`, `` removed from the `` Event `type` values: `` paragraph of
  `docs/guides/protocol-v1.md` (asserted present once, inside that paragraph):

  ```text
  --- FAIL: TestEventTypesAreDocumented (0.00s)
      doc_coverage_test.go:68: event type "session_status" is emitted but missing from the "Event `type` values:" paragraph in ../../docs/guides/protocol-v1.md
  FAIL
  FAIL  github.com/maccavelli/magic-cli-remote/internal/protocol  0.260s
  ```

* A Unix home path with a made-up account segment, not a real account and not in
  `placeholderHomes`, appended to this pair's MADR in the clone only:

  ```text
  --- FAIL: TestTrackedFilesUseOnlyPlaceholderHomes (0.72s)
      homepaths_test.go:231: docs/decisions/0203-MADR-skip-full-ci-for-docs-only-pushes.md:288: home path account "zzfakeacct" is not a placeholder: write <user>, or, if it is not a real account, add it to placeholderHomes
  FAIL
  FAIL  github.com/maccavelli/magic-cli-remote/internal  0.955s
  ```

* `${{ github.reff }}` in the `concurrency.group` of a copy of `docs.yml`, outside the clone:

  ```text
  .github/workflows/docs.yml:17:19: property "reff" is not defined in object type {action: string; … workspace: string} [expression]
     |
  17 |   group: docs-${{ github.reff }}
     |                   ^~~~~~~~~~~
  actionlint-plant=1
  ```

  (The object type's member list is elided.) Unplanted, `actionlint .github/workflows/docs.yml`
  exited 0.

The Node.js major-version step was not run locally: it asserts the runner's Node 24, and the
local Node is a later major. It is `ci.yml`'s step, copied unchanged.

**Step 4, checks on the tree.**

* `go test -count=1 ./internal/updateclient/`:
  `ok    github.com/maccavelli/magic-cli-remote/internal/updateclient  6.716s`, exit 0.
* `./scripts/assert-flutter-pin.sh`: `assert-flutter-pin: OK local and pinned Flutter agree (3.47.5)`, exit 0.
* `make check-records` on the tree: `==> records and docs links`, exit 0.
* `actionlint` (v1.7.12) on the three workflows exited **1**, on one finding:

  ```text
  .github/workflows/ci.yml:626:9: shellcheck reported issue in this script: SC2012:info:3:33: Use find instead of ls to better handle non-alphanumeric filenames [shellcheck]
  ```

  The finding is pre-existing and is not in the lines P1 changed: on `8802be5e`'s `ci.yml`,
  unchanged, the same command reports the same finding at `ci.yml:620:9`, six lines earlier
  because P1 adds six. It is in the `android-apk` job's "Assert release signature" step, whose
  `ls "$ANDROID_HOME/build-tools"` line came in with `64a0b7cd` (2026-08-02). `docs.yml` and
  `ci-flake-ledger.yml` alone pass (`actionlint .github/workflows/docs.yml
  .github/workflows/ci-flake-ledger.yml`, exit 0). **Open, for the owner.** V1 and the MADR's
  Confirmation say `actionlint` passes on all three workflows; on `ci.yml` it does not, before or
  after this change. Fixing that step is outside this plan ("Out of scope": any change to
  `ci.yml`'s jobs or steps), so it was not touched, and V1 is not met as written until the owner
  decides.

  *(Annotated 2026-10-20: resolved by Deviation 2 below. The owner chose to fix the line in this
  pair; see "P1 step 5" for the change and its checks.)*

**Pre-add.** P1 changes no Go or Dart file, so AGENTS.md's pre-add rule (gofmt, golint,
govulncheck; dart format) has nothing to run on.

### P2, 2026-10-20: the documents

* **`README.md`, "CI / CD (GitHub Actions)".** The sentence "A single workflow, `ci.yml`, …"
  became a list of the three workflows, linking the MADR: `ci.yml` on its events, skipped by a
  push or pull request changing only Markdown, and always in full on a tag; `docs.yml` for a
  Markdown change, with the records check, markdownlint and the Go tests that read Markdown; and
  `ci-flake-ledger.yml` after each `CI` run and daily, committing retried tests to
  `ci-flakes.tsv`. The table below it is introduced as "`ci.yml`'s jobs:" and is otherwise
  unchanged (its staleness is out of scope).
* **0143's MADR** gained "Amendment — 2026-10-20 (docs-only pushes no longer run `CI`)", citing
  this pair's MADR, with the four points of step 2. Its recount command is
  `gh run list --workflow ci.yml --limit 200 --json conclusion,headBranch,createdAt`. 0143's PLAN
  was not changed.
* **Checks**, on the tree:

  ```text
  check-records=0
  ==> records and docs links
  markdownlint-docs=0
  Linting: 54 files
  Summary: 0 issues in 0 files
  ```

  Also run, beyond step 3: `npx --no-install markdownlint-cli2 --no-globs` on 0143's MADR and
  this pair (`Linting: 3 files`, `Summary: 0 issues in 0 files`, exit 0); and, since records
  changed, the home-path guard and the protocol tests on the tree (`ok` for `internal` and for
  `internal/protocol`, both exit 0). A scan of the changed files for the local account name and
  the session scratch path found nothing.

### Deviation 1, 2026-10-20 (P2): the amendment names P1's commit by its content, not its hash

P2 step 2 says the amendment "names the commit" from which docs-only pushes stop running `CI`.
P1 is not committed when P2 is written: the owner commits each phase. The amendment instead names
it as "the commit that adds `.github/workflows/docs.yml`" and says this plan records its hash.
P3 step 1, which records P1's push, records that hash. No decision and no fact in the MADR
changes.

### Deviation 2, 2026-10-20 (P1): actionlint already failed on `ci.yml`; the owner chose to fix the line here

**Found.** P1 step 4's actionlint run on the three workflows exited 1 on one finding,
shellcheck's SC2012 at the `ls "$ANDROID_HOME/build-tools"` line of the `android-apk` job's
"Assert release signature (upload key, not debug)" step. It is pre-existing: it is on
`8802be5e`'s unchanged `ci.yml`, and the line came in with `64a0b7cd` (2026-08-02). The MADR's
Confirmation ("`actionlint` passes on `ci.yml`, `docs.yml` and `ci-flake-ledger.yml`") and V1
did not hold before this change, and "Out of scope" excluded any change to `ci.yml`'s steps.

**Decision.** Offered the choice, the owner chose, on 2026-10-20, "Amend 0203: fix that line
(Recommended)". The MADR gained "Amendment — 2026-10-20 (actionlint already failed on
`ci.yml`)". This plan gained P1 step 5 and an annotation on "Out of scope"'s first bullet; the
original text of both stays. Files added to P1's scope: none (`ci.yml` was already in it); the
change is one more line of `ci.yml`.

### P1 step 5, 2026-10-20: the SC2012 line

The whole step was read first. The line picks the newest build-tools directory under
`$ANDROID_HOME` for `apksigner`, under the step's `set -euo pipefail`. Before:

```text
BT="$ANDROID_HOME/build-tools/$(ls "$ANDROID_HOME/build-tools" | sort -V | tail -1)"
```

After:

```text
BT="$ANDROID_HOME/build-tools/$(find -H "$ANDROID_HOME/build-tools" -mindepth 1 -maxdepth 1 ! -name '.*' -exec basename {} \; | sort -V | tail -1)"
```

What keeps it equivalent: `sort -V | tail -1` is unchanged, so the same name is chosen from the
same set; `-mindepth 1 -maxdepth 1` lists the directory's entries, as `ls DIR` does;
`! -name '.*'` drops dot-names, which `ls` without `-a` drops; `-H` follows a symlinked
`build-tools`, which `ls` given that path also follows; and a missing directory makes `find`
exit 1, which `pipefail` turns into the step's failure, as `ls` did. `-exec basename {} \;` is
used over GNU `-printf '%f\n'` so the line runs the same on BSD `find`, where it was checked.
`git diff -U0 .github/workflows/ci.yml` shows that line and P1's `on:` lines, nothing else.

**Same result, before and after.** A Python script in the session scratch directory took the
`BT=` line from `ci.yml` (from `git show HEAD:` for before, the tree for after) and ran it with
bash 5.3 under `set -euo pipefail` on scratch `ANDROID_HOME` trees: several versions
(`9.0.0`, `30.0.3`, `33.0.2`, `34.0.0`, `35.0.0`, `35.0.0-rc1`); those plus a dot-named
directory; those plus a dot-named directory and a regular file named with a space; the
versions behind a symlinked `build-tools`; an empty `build-tools`; one holding only a dot-named
directory; and no `build-tools`. The scratch path is shown as `$ANDROID_HOME`. Before:

```text
line: BT="$ANDROID_HOME/build-tools/$(ls "$ANDROID_HOME/build-tools" | sort -V | tail -1)"
several: exit=0 BT=$ANDROID_HOME/build-tools/35.0.0-rc1
hidden: exit=0 BT=$ANDROID_HOME/build-tools/35.0.0-rc1
mixed: exit=0 BT=$ANDROID_HOME/build-tools/36.0.0 rc
symlink: exit=0 BT=$ANDROID_HOME/build-tools/35.0.0-rc1
empty: exit=0 BT=$ANDROID_HOME/build-tools/
hidden-only: exit=0 BT=$ANDROID_HOME/build-tools/
missing: exit=1 (no output) stderr-from=ls
```

After:

```text
line: BT="$ANDROID_HOME/build-tools/$(find -H "$ANDROID_HOME/build-tools" -mindepth 1 -maxdepth 1 ! -name '.*' -exec basename {} \; | sort -V | tail -1)"
several: exit=0 BT=$ANDROID_HOME/build-tools/35.0.0-rc1
hidden: exit=0 BT=$ANDROID_HOME/build-tools/35.0.0-rc1
mixed: exit=0 BT=$ANDROID_HOME/build-tools/36.0.0 rc
symlink: exit=0 BT=$ANDROID_HOME/build-tools/35.0.0-rc1
empty: exit=0 BT=$ANDROID_HOME/build-tools/
hidden-only: exit=0 BT=$ANDROID_HOME/build-tools/
missing: exit=1 (no output) stderr-from=find
```

The two lists, with the `stderr-from=` field dropped, `diff` to nothing (`results-diff=0`). An
empty `build-tools` yields `$ANDROID_HOME/build-tools/` both ways, so the step then fails at
`apksigner`, as before. The comparison was seen to fail: on scratch copies of `ci.yml` with
`-H` removed, the `symlink` case gave `BT=$ANDROID_HOME/build-tools/` (`noH-diff=1`), and with
`! -name '.*'` removed, the `hidden-only` case gave `BT=$ANDROID_HOME/build-tools/.99.0.0`
(`nohidden-diff=1`). The host's `find` and `sort` are the BSD and Apple ones; the runner has GNU
`find` and `sort`, and both lines use the same `sort -V`, so the comparison is of `ls` against
`find` only.

**Checks, on the tree, after the change** (`go test`'s tab shown as two spaces):

```text
actionlint=0
```

(`actionlint .github/workflows/ci.yml .github/workflows/docs.yml .github/workflows/ci-flake-ledger.yml`,
v1.7.12, no output.)

```text
ok    github.com/maccavelli/magic-cli-remote/internal/updateclient  5.340s
assert-flutter-pin: OK local and pinned Flutter agree (3.47.5)
ci.yml on: block matches plan: True
docs.yml matches plan: True
```

`go test -count=1 ./internal/updateclient/` and `./scripts/assert-flutter-pin.sh` exit 0, and
the plan-text comparison of step 1 and 2 still exits 0. V1 is now met on the tree.
