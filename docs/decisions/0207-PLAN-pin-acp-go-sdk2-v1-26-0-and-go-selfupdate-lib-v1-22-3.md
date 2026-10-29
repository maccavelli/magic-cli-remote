---
status: complete
date: 2026-10-29
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the `acp-go-sdk2 v1.26.0` and `go-selfupdate-lib v1.22.3` pins (0207 amendment I)

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its "Amendment — 2026-10-29 (I)": D48 and D49. D50 is not this plan's; each of its items takes its
own MADR.

Sibling plans of the same MADR, all `complete`:
[0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md),
[0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md](0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md),
[0207-PLAN-finish-full-semver-pins-and-check-libs.md](0207-PLAN-finish-full-semver-pins-and-check-libs.md),
[0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md) and
[0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md).

## Goal

* `go.mod` requires `acp-go-sdk2 v1.26.0` and `go-selfupdate-lib v1.22.3`.
* Both `ci.yml` pins name `0620e96ba7eb47d45cc94b474dc2db87d029ce40`, with the comment
  `# go-selfupdate-lib v1.22.3`.
* No source file changes, and every gate passes on macOS, the Linux test host and the Windows
  test host.

## Scope

* **In scope:** `go.mod`, `go.sum`, `.github/workflows/ci.yml` (the two pin lines), this plan, the
  MADR, and `docs/README.md`'s records index.
* **Not in scope:** D50's items; any source or test change. A gate that fails because of the move
  is a deviation: stop, and ask.
* **No provider quota.** Nothing on the wire changes (amendment I, I-2), so no live suite runs.
  `agentproc`'s own tests and Magic's `agentproc_test.go`, which spawns real test agents, cover the
  process path.

## Implementation Steps

### P1 — The pins (D48, D49)

1. **Re-check the tags.** `git ls-remote` still peels `acp-go-sdk2 v1.26.0` to `59a43be7…` and
   `go-selfupdate-lib v1.22.3` to `0620e96b…`, and neither has a newer tag. A newer tag stops the
   phase for an amendment.
2. **Move them.**
   * `go get github.com/maccavelli/acp-go-sdk2@v1.26.0` and
     `go get github.com/maccavelli/go-selfupdate-lib@v1.22.3`, then `go mod tidy`; `go mod tidy
     -diff` shows nothing.
   * `ci.yml:743` and `:764` move from `50deb3f1…` / `v1.22.2` to `0620e96b…` / `v1.22.3`, by an
     exact replace that asserts two matches.
   * `git diff --stat`: `go.mod` 2 lines, `go.sum` 4, `ci.yml` 2. Anything more is a deviation.
3. **Gates on macOS:**
   * `make pre-add-check` (whole module: no Go file is staged, so this is for `govulncheck`);
   * `make staticcheck`: 56 ignored findings, as before;
   * `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64 with
     `CGO_ENABLED=0`;
   * `make test`, `make race`; `TestWorkflowPinsMatchGoMod` among them;
   * `make check-records`, `make markdownlint-docs`, and the guard's redact preview.
4. **Gates on the test hosts** (the owner's word for host runs is asked with this plan):
   * the Linux test host: `go test -race -count=1 ./...` in a scratch copy, removed afterwards;
   * the Windows test host: `make ci-windows`, in a scratch clone at the working tree's content,
     removed afterwards. `go-selfupdate-lib`'s Windows replace, cleanup, Task Scheduler and SCM
     code changed (amendment I, I-3), and only this host runs it.
5. **Records.** This plan's execution record; `docs/README.md`'s index regenerated with
   `scripts/check_records.py --write-index`; the MADR's D48 and D49 marked carried out.
6. **Stage** for the owner: `go.mod`, `go.sum`, `ci.yml`, the MADR, this plan, `docs/README.md`.

## Verification

* `go list -m github.com/maccavelli/acp-go-sdk2 github.com/maccavelli/go-selfupdate-lib` gives
  `v1.26.0` and `v1.22.3`.
* Step 3's and step 4's gates, quoted in the execution record.
* No new test is written, so nothing is seen failing; the existing pin test,
  `TestWorkflowPinsMatchGoMod`, already fails on a pin that differs from `go.mod`.

## Rollout and Rollback

* **Rollout.** With the next tag.
* **Rollback.** Revert the commit: `go.mod`, `go.sum` and the two `ci.yml` lines go back together.
  Nothing else depends on the new versions.

## Execution record

~~Nothing has run. The plan waits for the owner's approval, and for the word on the host runs.~~

*Approved 2026-10-29:* "Approve, both hosts (Recommended)", which is also the word for step 4's
host runs. Question 16 was answered with it (the MADR records both).

### P1 Deviation 1, 2026-10-29 — two tests fail on Windows, on master too. The owner decided: "Fix under the gaps plan, first (Recommended)"

Steps 1 to 3 ran: the tags held, the pins moved (`go.mod` 2 lines, `go.sum` 4, `ci.yml` 2), and
every macOS gate passed. Step 4 (`leftovers/i_hosts.py`): the Linux test host's
`go test -race ./...` passed, 44 packages ok. The Windows test host's `make ci-windows` failed:

* `TestReadTextFileMissingIsResourceNotFound`, `conformance_test.go:40`: "data =
  {\"uri\":\"C:\\\\Users\\\\<user>\\\\…\\\\missing.txt\"}, want it to name C:\\Users\\<user>\\…\\missing.txt";
* `TestListAgentSessionsStopsAtAnyRepeatedCursor`, `sdkhelpers_test.go:121`: "session/list
  response: ListSessionsResponse: sessions[0]: SessionInfo: cwd: \"/w\" is not an absolute path".

**Not the pins'.** Master's CI at `d4f22efc` (run `40890754513`) failed its `Go (windows/amd64)` job
on the same two tests with the same messages; the last green CI was at `3cc402fb`. The tests are
the SDK-gaps plan's C1 (`d4a45c4d`) and C1b (`3ccaf700`), whose gates ran on macOS and Linux and
not on Windows.

* **Offered:** fix them under the SDK-gaps plan, in their own commit before this one
  (recommended); or fold the fixes into this plan's commit.
* **Decided:** under the SDK-gaps plan, first. This plan's step 4 runs again on Windows once that
  commit is in. No file of this plan changes.

### P1, 2026-10-29: the pins

The owner committed the SDK-gaps plan's Windows follow-up as `b0219278` (on origin `1566fd06`; see the correction of 2026-10-29) and answered "committed,
proceed".

**Steps 1 and 2** (`leftovers/i_p1_pins.py`):

* `git ls-remote`: the newest tags are `acp-go-sdk2 v1.26.0`, peeling to `59a43be7…`, and
  `go-selfupdate-lib v1.22.3`, peeling to `0620e96b…`, as amendment I says.
* "go: upgraded github.com/maccavelli/acp-go-sdk2 v1.25.0 => v1.26.0"; "go: upgraded
  github.com/maccavelli/go-selfupdate-lib v1.22.2 => v1.22.3". `go mod tidy`, then `go mod tidy
  -diff`: no diff.
* Both `ci.yml` pins replaced, the replace asserting two matches before and after.
* `git diff --stat`: `ci.yml` 4, `go.mod` 4, `go.sum` 8 lines changed: 2, 2 and 4 replaced.
* `go list -m`: `acp-go-sdk2 v1.26.0`, `go-selfupdate-lib v1.22.3`.

**Step 3, the macOS gates** (`leftovers/i_gates.py`), run first before Deviation 1 and again on
`b0219278` (on origin `1566fd06`; see the correction of 2026-10-29), passing both times:

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` | "987 file(s) clean (gofmt, golint, govulncheck)" |
| `make staticcheck` | exit 0; "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name" |
| `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64, with `CGO_ENABLED=0` | exit 0, each |
| `TestWorkflowPinsMatchGoMod` | PASS |
| `make test`, `make race` | exit 0; 44 packages ok |
| `make check-records`, `make markdownlint-docs`; the guard's redact preview | exit 0; "0 issues"; "would edit 0 file(s)" |

**Step 4, the test hosts** (`leftovers/i_hosts.py`), on `b0219278` (on origin `1566fd06`; see the correction of 2026-10-29) with the pins, each in a
scratch copy removed afterwards and its absence checked:

| Host | Gate | Result |
| :--- | :--- | :--- |
| Linux | `go test -race -count=1 -timeout=40m ./...`, go1.27.3 linux/amd64 | exit 0; 44 packages ok |
| Windows | `make ci-windows`, go1.27.3 windows/amd64, after the `make -n` check | exit 0; "ci-windows-local: ALL SELECTED CHECKS PASSED"; 44 packages ok |

The first run of step 4, before `b0219278` (on origin `1566fd06`; see the correction of 2026-10-29), is Deviation 1: Linux passed, and Windows failed on
the two tests that commit fixed.

**Step 5.** `scripts/check_records.py --write-index` added this plan's row to `docs/README.md`,
and nothing else. The MADR's D48 and D49 are marked carried out.

**Files for this commit:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`, the MADR, this plan,
`docs/README.md`.

P1 is done, and so is this plan.

### Correction, 2026-10-29 — the fix's commit id

The owner pushed both commits, and the push rebased them onto `84d35609`, a row the CI flake
ledger had added on origin
([0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md)).
On origin:

* the SDK-gaps plan's Windows follow-up, cited above as `b0219278`, is `1566fd06`;
* this plan's commit is `0edeffba`.

`git diff --stat b0219278 1566fd06` shows only `ci-flakes.tsv`, one line. So the gates above ran
on the content that was pushed, apart from that row. `b0219278` was the local commit id when this
record was written; it is on no branch of origin. The citations above are annotated, not rewritten.
The disclosure guard passed over `d4f22efc..0edeffba`.
