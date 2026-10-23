---
status: in-progress
date: 2026-10-18
associated-madr: "0186-MADR-integrate-current-library-releases-and-prepare-gobble.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement integrating the current library releases as two tracks, and preparing gobble

Associated MADR: [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)

The two library tracks are not this PLAN's:

* the self-update track is
  [0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md](0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md),
  S1–S8;
* the ACP track is
  [0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md),
  A1–A5 and B1.

This PLAN carries only what 0186 decides itself:

* the records work (R1);
* the compatibility check (P1, D7);
* the full-SemVer version pin (P2, D8).

gobble's contract rewrite and registration (D9) are planned in
[0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md).

The MADR is `proposed`. P1 and P2 start only after it is accepted, and the owner approves the phase
by name. Agents stage, and the owner commits with `git commit --no-edit`.

## Goal

* The program's records agree with each other and with the owner's answers.
* Magic can check both libraries' newest tags on demand, with the shared tool.
* No version pin in Magic can match a prerelease or pseudo-version of the release it names.

## Scope

* **R1:** records only, all under `docs/` (bootstrap exception).
* **P1:**
  * `check_libs.json` (new, at the root);
  * `Makefile` (a `check-libs` target);
  * `docs/guides/` text naming the target, if a guide lists the Make targets.
* **P2:**
  * `internal/provider/version.go` and its tests;
  * the older copies in `internal/provider/kilo` and `internal/provider/opencode`;
  * `go.mod` and `go.sum`, where `golang.org/x/mod` becomes a direct requirement. It is already in
    the module graph.

## Implementation Steps

### R1 — Records (executed 2026-10-13; see the execution record)

1. Record the owner's answers Q1–Q7 in the MADR.
2. Rename the 0179 pair to its gobble filenames, repoint every link, and regenerate the index
   (MADR, Q2).
3. Amend
   [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
   and its PLAN as the MADR's D4, Q3 and Q7 require.
4. Write this PLAN.

### P1 — Adopt `make check-libs` (D7, answer Q6)

Lands after 0054's S1 and 0184's A1, because until then this repository requires neither library.
`go-shared`'s check reports exactly that ("`required: FAIL`… nothing to check"), as its own run
against this repository showed on 2026-10-13.

1. Add `check_libs.json` naming:
   * `github.com/maccavelli/acp-go-sdk2`, tested through `./internal/provider/acpagent/...` and
     `./internal/provider/grok/...`;
   * `github.com/maccavelli/go-selfupdate-lib`, tested through `./internal/updateclient/...`,
     `./internal/cli/...` and `./internal/relay/...`, or the packages that replace them after S1.
2. Add `check-libs` to the `Makefile`. It runs
   `python3 $(GO_SHARED)/scripts/check_libs.py --repo .`, with `GO_SHARED ?= ../go-shared`, and
   fails with a clear message when that directory is missing. It is not a prerequisite of
   `preflight`, which runs offline.
3. Show it failing first: on a scratch clone whose `check_libs.json` names a module the repository
   does not require, and with `GO_SHARED` pointing at a missing directory.
4. Run it, and record the verdicts.

### P2 — Full-SemVer version pins (D8)

1. Write the test first: `1.3.0-rc.1`, and a pseudo-version such as
   `v1.2.4-0.20261013000000-abcdef123456`, must not match a `1.3.0` or `1.2.4` pin, while `v1.3.0`
   and `1.3.0+build` still match `1.3.0`. Show it failing on today's `SameVersion`.
2. Re-implement `provider.SameVersion` and `ParseSemver` over `golang.org/x/mod/semver`. An
   unparseable version still never matches.
3. Move kilo's and opencode's copies onto the same function. Their existing pin tests must still
   pass.
4. Run `make pre-add-check` on the changed Go files, `go test -race ./...` and `make preflight`.

## Verification

* **R1:**
  * `python3 scripts/check_records.py --write-index`, `make check-records` and
    `make markdownlint-docs` exit 0;
  * an identifier scan of the changed records is clean;
  * a file scan finds the 0179 `pigo` filenames only in
    `0175-PLAN-conform-docs-tree-to-adopted-record-layout.md`'s historical text and in the rename
    notes.
* **P1 and P2:** the steps above, each new assertion seen failing first on a scratch copy.

## Rollout and Rollback

* R1 is records only. Before commit, rename back and revert the link edits.
* P1 adds an on-demand target and a config file. Removing both reverts it.
* P2 changes when a version pin matches. Reverting restores major.minor.patch matching.

## Execution record

### R1, 2026-10-13

* **Approval.** On 2026-10-13 the owner answered the MADR's Q1–Q7, then said "write it, then
  proceed" after the go-shared deviation notes. R1 is records only, under the bootstrap exception.
* **Done:**
  * the MADR's amendment "the owner's answers";
  * `git mv` of the 0179 pair to `0179-MADR-gobble-native-acp-provider.md` and
    `0179-PLAN-gobble-native-acp-provider.md`, with rename notes in both;
  * links repointed in six files: 19 occurrences, by a script that asserted none remained;
  * the 0184 MADR and PLAN amendments;
  * this PLAN;
  * the index, regenerated.
* **Verification,** each exit status recorded before reading its output:

  ```text
  python3 scripts/check_records.py --write-index   exit=0
  make check-records                               exit=0
  make markdownlint-docs                           exit=0
  ```

  * **Default-rules lint** of copies of the six changed decision records: one finding is new, and
    it is fixed. My rename section in the 0179 PLAN used `*` where that file uses `-` (MD004), and
    after the fix the copy lints clean. The one other finding, MD058 at `0179-MADR…:59`, is in the
    record's original text and was left alone.
  * **File scan** for the 0179 `pigo` filenames: one occurrence, in
    `0175-PLAN-conform-docs-tree-to-adopted-record-layout.md`'s historical text, as intended.
  * **Identifier scan** of the eight changed records: two hits, both in text that was already
    there. One is a generic `/private/var/folders/` scan target named in 0184-PLAN's Verification;
    the other is `~/.local/bin` in 0179's original text. Neither carries an account name or
    hostname.
* **Not done, deliberately:**
  * no source change;
  * P1 and P2 wait for the MADR's acceptance and a named approval;
  * nothing was committed (the owner commits).

## Status note — 2026-10-18

Found by the record audit in [0200-REPORT-record-status-audit.md](../reports/0200-REPORT-record-status-audit.md).

Still `in-progress`. R1 is done, and P1 and P2 have not started. The code agrees:

* **P1 (`make check-libs`).** There is no `check_libs.json` and no `check-libs` target in the
  `Makefile`. Its precondition is half met: `go.mod` requires `acp-go-sdk2` (v1.15.0), but
  not `go-selfupdate-lib`, because
  [0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md](0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md)'s
  S1 has not landed.
* **P2 (full-SemVer pins).** The defect is still present. `provider.SameVersion`
  (`internal/provider/version.go:18`) cuts the version at the first `-` or `+`, so
  `1.3.0-rc.1` matches a `1.3.0` pin. opencode keeps its own `parseSemver`
  (`internal/provider/opencode/version.go:44`). `golang.org/x/mod` is only an indirect
  dependency.
* Each phase still needs the owner's approval by name.

## Status note — 2026-10-22

P2 and P1 are carried out by
[0207-PLAN-finish-full-semver-pins-and-check-libs.md](0207-PLAN-finish-full-semver-pins-and-check-libs.md),
under 0207's D5. Its V1 is this plan's P2, and its V2 is P1. That plan's execution record is the
record of both. This plan is set `complete` when V2 is done.
