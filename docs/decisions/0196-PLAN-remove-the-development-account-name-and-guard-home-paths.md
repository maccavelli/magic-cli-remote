---
status: proposed
date: 2026-10-17
associated-madr: "0196-MADR-remove-the-development-account-name-and-guard-home-paths.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement removing the development account name and guarding home paths

Associated MADR: [0196-MADR-remove-the-development-account-name-and-guard-home-paths.md](0196-MADR-remove-the-development-account-name-and-guard-home-paths.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option
(R1–R5).

* The owner approves the PLAN by name.
* Go files pass `make pre-add-check`. Dart files pass `dart format`, and the phase runs
  `make preflight`.
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

No tracked file carries the development account name (`<dev-account>` in these records) in a
home path. A test fails whenever a tracked file
gains a home path whose account segment is not a known placeholder.

## Scope

* The 28 files the survey found:
  * Go tests: `internal/cli/service/setup_test.go` and `plist_render_test.go`.
  * Dart tests: `apps/mobile/test/settings_store_test.dart` and `chat_back_arrow_test.dart`.
  * Fixtures: the three files under `internal/provider/codex/testdata/codex-spike-0.145.0/`.
  * Scripts: `scripts/quick-probe.py` and `scripts/probe-codex-item-stream.py`.
  * 19 records: 15 under `docs/` and 4 under `apps/mobile/docs/` (the MADR's survey lists the
    shapes).
* A new test file, `internal/homepaths_test.go`.
* **Out of scope:** git history (R4), and identifiers other than home paths (R5).

## Implementation Steps

### H1. The guard, first (R3)

1. Write `internal/homepaths_test.go` with `TestTrackedFilesUseOnlyPlaceholderHomes`, as the MADR's
   R3 describes. It finds the repository root as `internal`'s parent, and lists files with
   `git -C <root> ls-files -z`.
2. Seed the allowlist with the survey's placeholders: `user`, `alice`, `REPLACE_ME`, `dev`, `Dev`,
   `me`, `x`, `X`, `u`, `you`, `proj`, `secret`, `sentinel`, `testuser`, `mcremote`, `alpine`,
   `a&b`, `Zoë`, `RUNNER~1`, `...` and `%3Cuser%3E`. Any `<…>` segment is also a placeholder.
   * Each entry is checked against its survey line before it goes in. An entry that turns out to
     be a real account is not added. It is replaced in H2 instead.
3. Run it on today's tree. It must fail, naming `<dev-account>` at the survey's locations: 104 hits in 28
   files. That is the fail-first proof against real data.

### H2. Replace (R1, R2)

1. A scratchpad Python script replaces the account segment only: `/home/<dev-account>` → `/home/<user>`
   or `/home/user`, `/Users/<dev-account>` → `/Users/<user>` or `/Users/user`, and
   `C:\Users\<dev-account>` likewise. The script takes the name as an argument, so it is not
   written in any committed file.
   `<user>` goes in records and fixtures, and `user` in Go and Dart tests. It asserts each file's
   count before and after, and changes nothing else.
2. Each changed record gains, under its title or its "More Information", the line: "Account names
   in paths redacted on 2026-10-17 by 0196." Nothing else in the record changes.
3. The scripts take `REPO = Path(__file__).resolve().parents[1]`. Each is run with `--help`, or the
   least that shows it starts, without starting codex.
4. Re-run H1's test: it passes. Re-run the survey: only allowlisted segments remain.

### H3. Prove the guard on planted input (scratch copy)

1. A new tracked file containing `/home/realname/x`: the test fails, naming the file, the line and
   `realname`.
2. A JSON-escaped `C:\\Users\\realname\\x`: it fails.
3. `git ls-files` replaced by a command that lists nothing, through an empty `PATH` `git` stub: it
   fails rather than passing.
4. A segment followed by `.`, as in `/home/user.trust_level`: it passes, since `user` is
   allowlisted.

### H4. Gates

* `go test -race ./...`, `make pre-add-check`, `make lint`, `make staticcheck`.
* `make preflight`, for the two Dart files.
* `make ci-windows` on the Windows test host.
* `check_records.py --check-all`, `make markdownlint-docs`.

## Verification

Confirmation is the MADR's: H1's real-data failure, H3's planted failures, and the gates in H4.

## Rollout and Rollback

* Tests, fixtures, scripts and records. No product code changes. Rollback is reverting the commit,
  which would restore the account name, so it is not expected.
