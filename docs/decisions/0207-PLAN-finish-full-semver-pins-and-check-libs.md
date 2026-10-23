---
status: in-progress
date: 2026-10-22
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement full-SemVer version pins and `make check-libs` (0207 D5)

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its D5. D5 carries out two accepted phases of
[0186-PLAN-integrate-current-library-releases-and-prepare-gobble.md](0186-PLAN-integrate-current-library-releases-and-prepare-gobble.md):
P2 (0186 D8) and P1 (0186 D7, answer Q6).

## Goal

* Every engine version pin compares full SemVer through one function: a prerelease or
  pseudo-version ranks below its release and never matches a release pin (0186 D8).
* `make check-libs` runs go-shared's library-compatibility check against the two shared libraries
  Magic requires, and its first verdicts are recorded (0186 D7, Q6).

## Scope

* **In scope:** 0207 D5, which is 0186's P2 and P1, in that order.
* **Not in scope:**
  * 0207 D1, the `go-selfupdate-lib v1.17.2` pin. V2's run checks `v1.17.2` against this tree,
    which is the check 0186 D7 requires before a pin moves, but V2 moves no pin.
  * 0207 D3, D4, and D8's `binaryWarning` half. They wait for their own plan.
  * The silent-command marks of 0208 P4a, which key on the version string as reported. They do
    not compare versions.
  * Windows: `make check-libs` calls `python3`, as `make check-records` does. It is run on the
    macOS host only.

## Context: what the tree does today [read 2026-10-22]

**The two comparisons.**

| Where | What it does |
| :--- | :--- |
| `internal/provider/version.go:18-59`, `SameVersion` and `ParseSemver` | trims space and a leading `v`/`V`, cuts at the first `-`, `+` or space, and compares `major.minor.patch`. An unparseable version matches nothing, not even itself. |
| `internal/provider/opencode/version.go:44-104`, `parseSemver` and `CompareVersions` | the same, except that a space does not end the version. Unparseable sorts below parseable, and two unparseable versions are equal. |

**Who calls them.**

* `SameVersion`: `acpagent/version.go:44` (grok's pin, `1.0.13`) and `codex/version.go:69` (`0.152.1`).
* `ParseSemver`: `codex/version.go:48`, to accept a userAgent token as a version.
* opencode's pair: `VersionIsKnownGood` (pin `1.18.26`) and `VersionMeetsMin` (the session-tree
  floor `1.18.0`), from `opencode/http.go:677`, `:686` and `:720`.
* **kilo has no copy.** `kilo/version.go:42` compares the strings exactly, `v != KnownGoodVersion`
  (`7.5.6`). 0186 D8 and its PLAN's P2 step 3 speak of "kilo's copy", which does not exist.

**Tests that assert today's behavior, and which D8 reverses.**

* `internal/provider/version_test.go:16`: `{"1.0.13-rc.1", "1.0.13", true}`.
* `internal/provider/opencode/version_test.go:95`: "equal despite prerelease",
  `CompareVersions("1.18.21-rc.1", "1.18.21") == 0`.
* `internal/provider/opencode/version_test.go:129`: `VersionIsKnownGood("1.18.26-rc.1") == true`.

**`golang.org/x/mod/semver`, probed [2026-10-22, `x/mod v0.41.0`, a scratch module].**

| Call | Result |
| :--- | :--- |
| `Compare("v1.0.13+abc", "v1.0.13")` | `0`: build metadata is ignored |
| `Compare("v1.3.0-rc.1", "v1.3.0")` | `-1` |
| `Compare("v1.2.4-0.20261013000000-abcdef123456", "v1.2.4")` | `-1`, and `+1` against `v1.2.3` |
| `Compare("v1.0", "v1.0.0")` | `0`: `vMAJOR.MINOR` is a shorthand |
| `Compare("v1.18.0-rc.1", "v1.18.0")` | `-1` |
| `Compare("vnonsense", "vgarbage")` | `0`: invalid versions are equal, and below every valid one |
| `IsValid` of `V1.0.13`, `v01.0.13`, `v1.0-rc.1`, `v1.0+build`, `v1.0.13 (extra)` | all `false` |

The package requires a lowercase `v`, rejects leading zeros, and gives a two-part version no
suffix. `golang.org/x/mod v0.41.0` is already a direct requirement (`go.mod:19`); only
`internal/updateclient/spec_test.go` imports it today. The
[0207-REPORT-go-selfupdate-lib-deeper-adoption-assessment.md](../reports/0207-REPORT-go-selfupdate-lib-deeper-adoption-assessment.md)
found that the library's `NewSemverPolicy` accepts only strict `vX.Y.Z` tags, so it does not fit
provider versions.

**`make check-libs`'s inputs.**

* go-shared's `scripts/check_libs.py` (go-shared `0002-MADR-shared-library-compatibility-check.md`,
  accepted) reads `check_libs.json` from the consumer's working tree. It checks a `git clone` of
  the consumer's committed `HEAD`, outside the tree, against each library's latest SemVer tag. It
  needs the network, reads and sends no credential, and exits 1 on a failing check and 2 on a
  usage or config error. Its D3 picks a sibling checkout, `../go-shared`.
* The packages that import each library (`go list`, non-test imports):

| Library | Pinned | Latest tag | Importing packages |
| :--- | :--- | :--- | :--- |
| `github.com/maccavelli/acp-go-sdk2` | `v1.20.0` | `v1.20.0` | `internal/provider/acpagent`, `internal/provider/acpcommon` |
| `github.com/maccavelli/go-selfupdate-lib` | `v1.17.1` | `v1.17.2` | `internal/cli`, `internal/cli/service`, `internal/daemon`, `internal/relay`, `internal/updateclient` |

* 0186's status note of 2026-10-18 found P1's precondition half met. `go.mod` now requires both
  libraries (`go.mod:11-12`), so it is met.

## Questions for the owner

* **Q1 — kilo.** Move kilo's exact string comparison onto `provider.SameVersion`, as 0186 D8
  intends? *Recommended: yes.* `v7.5.6` and `7.5.6+build` then match the pin. A prerelease still
  does not. Only kilo's log line changes: info instead of a drift warning.
* **Q2 — opencode's floor.** Under D8, `1.18.0-rc.1` ranks below the `1.18.0` floor, so with the
  session tree on, opencode refuses to start on it, where today it starts. *Recommended: accept;
  this is D8 applied to the floor.* The alternative compares the floor without prerelease, which
  keeps two rules.
* **Q3 — order.** V1 (SemVer), then V2 (`check-libs`), each its own commit. *Recommended.* V2 checks
  the committed `HEAD`, so after V1 is committed, V2's run covers V1's code too.

**The owner's answers, 2026-10-22:** Q1 "Yes, use SameVersion (Recommended)"; Q2 "Accept: D8
applies to the floor (Recommended)"; Q3 "V1 then V2, approve the plan (Recommended)", which
approves this plan.

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Every new test or gate is seen failing first,** on a scratch copy of the tree, never by
  dirtying it. The execution record says what the failure looked like.
* **Before staging:** `make pre-add-check FILES="…"` on every staged `.go` file, then `make test`
  and `make race`.
* **A phase ends with its execution record** in this plan, and is staged for the owner, who
  commits it.

### V1 — One full-SemVer comparison (0186 P2, D8)

**Files:**

* `internal/provider/version.go`, `version_test.go`;
* `internal/provider/opencode/version.go`, `version_test.go`;
* `internal/provider/kilo/version.go`, and a kilo test file (Q1);
* `docs/decisions/0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md`, a
  note on D5;
* `docs/decisions/0186-PLAN-integrate-current-library-releases-and-prepare-gobble.md`, a dated
  note;
* this plan.

**Steps.**

1. **The tests first.** In `version_test.go`, a table for a new
   `provider.CompareVersions(a, b string) int` and for `SameVersion`:
   * `1.3.0-rc.1` and `v1.2.4-0.20261013000000-abcdef123456` do not match `1.3.0` and `1.2.4`, and
     rank below them;
   * `v1.3.0`, `1.3.0+build` and `1.3.0` match `1.3.0`;
   * `1.0` matches `1.0.0`; `V1.0.13`, and `1.0.13` with spaces around it, match `1.0.13`;
   * `1.0.13 (extra)` matches `1.0.13`, keeping `ParseSemver`'s rule that a space ends the version;
   * an unparseable version matches nothing, and sorts below every parseable one.

   The three assertions listed above that D8 reverses are changed to its answer. Seen failing on
   today's code.
2. **One implementation,** in `internal/provider/version.go`, over `golang.org/x/mod/semver`:
   * `normalize(v)` trims space, cuts at the first whitespace, drops a leading `v` or `V`, and
     prefixes `v`;
   * `CompareVersions(a, b)` is `semver.Compare` of the normalized strings;
   * `SameVersion(a, b)` is false unless both are valid, then `CompareVersions(a, b) == 0`;
   * `ParseSemver` keeps its signature, which codex uses, and reads `semver.Canonical`; `ok` is
     `semver.IsValid`. A prerelease keeps its `major.minor.patch`.
3. **opencode** deletes `parseSemver` and `CompareVersions`, and calls
   `provider.CompareVersions` and `provider.SameVersion`. `VersionIsKnownGood`, `VersionMeetsMin`
   and `VersionPinError` keep their signatures. Its existing matrices stay, with the two reversed
   cases, and `TestParseSemver` moves to the provider package's table.
4. **kilo** (Q1) compares with `provider.SameVersion`, and a test pins `v7.5.6` and `7.5.6+build`
   as matches and `7.5.6-rc.1` and `7.5.7` as drift.
5. **Records.**
   * 0207's MADR gets a dated note on D5. It records the facts above that 0186 stated otherwise
     (kilo compares strings; three tests assert the reverse of D8) and the answers to Q1 to Q3.
   * 0186's PLAN gets a dated note: P2 is carried out by this plan's V1.
6. `go mod tidy` must leave `go.mod` and `go.sum` unchanged. A change is a deviation.

**Verification.** `go test ./internal/provider/...`, `make test`, `make race`, `go vet` for
darwin, linux and windows, and `make pre-add-check` on the changed Go files. A plant on a scratch
copy, restoring today's cut at `-`, must fail the new prerelease cases.

### V2 — `make check-libs` (0186 P1, D7, Q6)

**Files:**

* `check_libs.json` (new, at the repository root);
* `Makefile`;
* `docs/README.md`, one row in the "I want to…" table;
* `docs/guides/standards/go/README.md` (its command table) and `README.md` (its `make` list near
  line 1535), which list the Make targets. 0186 P1 names such a guide;
* `docs/decisions/0186-PLAN-integrate-current-library-releases-and-prepare-gobble.md`, a dated
  note;
* this plan.

**Steps.**

1. **`check_libs.json`**, naming each library with the packages that import it:

   ```json
   {"libraries": [
     {"module": "github.com/maccavelli/acp-go-sdk2",
      "packages": ["./internal/provider/acpagent/...", "./internal/provider/acpcommon/...",
                   "./internal/provider/grok/..."]},
     {"module": "github.com/maccavelli/go-selfupdate-lib",
      "packages": ["./internal/updateclient/...", "./internal/cli/...", "./internal/relay/...",
                   "./internal/daemon/..."]}
   ]}
   ```

   `./internal/provider/grok/...` imports no SDK package itself. 0186 P1 names it, and its suites
   drive the SDK through `acpagent`.
2. **The target.** `GO_SHARED ?= ../go-shared`, and `check-libs` added to `.PHONY`. The recipe
   stops with exit 2 and a message naming `$(GO_SHARED)/scripts/check_libs.py` when that file is
   missing. Otherwise it runs `python3 $(GO_SHARED)/scripts/check_libs.py --repo .`. It is not a
   prerequisite of `preflight`.
3. **Seen failing first,** on a scratch clone:
   * a `check_libs.json` naming a module the repository does not require: exit 1, with the
     `required` check failing;
   * `GO_SHARED` pointing at a missing directory: exit 2, with the message;
   * `python3 ../go-shared/scripts/check_libs.py --self-test` passes, as go-shared's own check.
4. **The run.** `make check-libs` on the committed `HEAD` after V1. The verdicts for both
   libraries are recorded here: each check and its result, and where the scratch logs were.
   * For `acp-go-sdk2`, pinned at the latest tag, apidiff has nothing to compare.
   * For `go-selfupdate-lib`, the run is 0186 D7's check for 0207 D1. A failure is recorded and
     reported. It is not fixed here, because D1 is not in scope.
5. **The docs.** One "I want to…" row in `docs/README.md`: "check a library's newest release
   before moving a pin" → this plan's V2. One line for `make check-libs` in each of the two Make
   target lists, saying it needs the network and a `../go-shared` checkout.
6. 0186's PLAN gets a dated note: P1 is carried out by V2. It sets that plan to `complete` once
   both notes are in, since R1 is its only other phase and is done.

**Verification.** The two planted failures above, the self-test, the recorded run, and
`make check-records`.

## Verification

* V1: the Go gates above, and the plant.
* V2: the planted failures, the self-test, and the run's verdicts.
* `git status --porcelain` is unchanged by `make check-libs` (go-shared 0002's Confirmation).
* The records index in `docs/README.md` lists this plan. It is regenerated with
  `scripts/check_records.py --write-index` in the commit that adds this plan, never hand-edited.
* No live suite is needed. The pins' log lines are covered by the unit tests.

## Rollout and Rollback

* **V1** changes only which version strings match a pin, and opencode's floor (Q2). Reverting its
  commit restores both. A prerelease engine logs a drift warning, or for opencode below the floor
  with the session tree on, refuses to start. `providers.opencode.session_tree=false` remains the
  operator's switch.
* **V2** adds a target and a config file that nothing else reads. Reverting its commit removes
  them.

## Execution record

### V1 Deviation 1, 2026-10-22 — a bare major is not a version. The owner decided: "Require major.minor everywhere"

**Found** by the first `go test ./internal/provider/...` after V1's implementation:

```text
--- FAIL: TestVersionFromUserAgent (0.00s)
    version_test.go:38: versionFromUserAgent("codex_cli_rs/1 (x)") = "1", want ""
```

`internal/provider/codex/version_test.go:38` passed before V1. `x/mod/semver` accepts `vMAJOR`
as a shorthand for `vMAJOR.0.0`. Today's `ParseSemver` refuses a string without at least
`major.minor` (`version.go:43` at `HEAD`). So step 2 as written ("`ok` is `semver.IsValid`") would
let a lone number pass as a version.

**Decision.** Step 2 is corrected: ~~`ok` is `semver.IsValid`~~. A version needs at least
`major.minor`, for `ParseSemver`, `SameVersion` and `CompareVersions` alike. Anything shorter is
unparseable, as today. `normalizeVersion` enforces it, in `version.go`, so no file is added. The
provider table gains the cases `1` against `1.0.0`.

### V1, 2026-10-22: one full-SemVer comparison

**What changed.**

* `internal/provider/version.go`:
  * `SameVersion`, the new `CompareVersions`, and `ParseSemver` read `golang.org/x/mod/semver`
    through `normalizeVersion`.
  * `normalizeVersion` trims space, ends the version at the first space, drops `v` or `V`, and
    refuses fewer than two numeric parts.
  * `ParseSemver` keeps its signature for codex.
* `internal/provider/opencode/version.go`: `parseSemver` and `CompareVersions` are deleted.
  `VersionIsKnownGood` is `provider.SameVersion`, and `VersionMeetsMin` is
  `provider.CompareVersions`.
* `internal/provider/kilo/version.go`: the drift check is `provider.SameVersion` (Q1).
* Tests:
  * `version_test.go` gains the prerelease, ordering and `ParseSemver` tables. `TestParseSemver`
    moved there from opencode.
  * opencode's two reversed cases now expect D8's answer, and it gains the floor case
    `1.18.0-rc.1` (Q2).
  * `kilo/version_test.go` (new) pins `v7.5.6` and `7.5.6+build` as the pin, and `7.5.6-rc.1`,
    `7.5.7` and `nonsense` as drift.
* Records: the D5 note in 0207's MADR, the status note in 0186's PLAN, and this plan in the records
  index (`check_records.py --write-index`, one line).

**Seen failing first.** The new provider tests ran on a scratch copy holding `HEAD`'s
`version.go` and a stand-in `CompareVersions` with today's rule:

```text
--- FAIL: TestSameVersionRefusesUnparseableInput (0.00s)
    version_test.go:35: SameVersion("1.0.13-rc.1", "1.0.13") = true, want false
--- FAIL: TestPrereleaseNeverMatchesARelease (0.00s)
    version_test.go:58: CompareVersions("1.3.0-rc.1", "1.3.0") = 0, want -1
    version_test.go:58: CompareVersions("v1.2.4-0.20261013000000-abcdef123456", "1.2.4") = 0, want -1
    version_test.go:58: CompareVersions("1.18.0-rc.1", "1.18.0") = 0, want -1
```

The other cases passed on today's rule, so the failures are exactly D8's.

**Plants**, on a scratch copy; each failed as expected, and the restored copy passed:

| Plant | Failed |
| :--- | :--- |
| prerelease cut again, as today | `TestPrereleaseNeverMatchesARelease`, opencode's `TestCompareVersionsCompleteMatrix`, `TestKnownGoodPinComparesFullSemver` |
| bare major accepted | `TestSameVersionRefusesUnparseableInput`, codex's `TestVersionFromUserAgent` |
| unparseable equals unparseable | `TestSameVersionRefusesUnparseableInput` |
| kilo exact string again | `TestKnownGoodPinComparesFullSemver` |

**Gates.**

* `go test ./internal/provider/...`: every package ok, 0 FAIL lines.
* `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64, `make test`, `make race`,
  `make lint` and `make staticcheck`: each exit 0, with 0 FAIL lines.
* `make pre-add-check` on the six Go files: "6 file(s) clean".
* `go mod tidy -diff` printed nothing: `go.mod` and `go.sum` are unchanged.
* markdownlint: this plan and 0207's MADR are clean. 0186's PLAN reports 46 MD004 findings, all
  on lines 14–164, which are its existing asterisk lists. The note adds lines 165–171 and no list.
  `check_records.py --check-all` passes.

**Not done in V1.** V2. V1 was written while 0208's P4b was still staged. It was staged only
after the owner committed P4b as `03118fa1`, so V1 is a commit of its own.
