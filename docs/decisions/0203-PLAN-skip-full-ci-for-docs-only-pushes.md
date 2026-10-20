---
status: proposed
date: 2026-10-20
associated-madr: "0203-MADR-skip-full-ci-for-docs-only-pushes.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement skipping the full CI for docs-only pushes, with a docs workflow that runs the Markdown-reading tests

Associated MADR: [0203-MADR-skip-full-ci-for-docs-only-pushes.md](0203-MADR-skip-full-ci-for-docs-only-pushes.md)
(option E, as proposed; the owner decides).

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
  finds the ledger needs none.
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
