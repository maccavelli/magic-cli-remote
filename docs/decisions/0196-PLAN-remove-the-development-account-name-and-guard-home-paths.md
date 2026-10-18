---
status: complete
date: 2026-10-17
associated-madr: "0196-MADR-remove-the-development-account-name-and-guard-home-paths.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement removing the development account name and guarding home paths

Associated MADR: [0196-MADR-remove-the-development-account-name-and-guard-home-paths.md](0196-MADR-remove-the-development-account-name-and-guard-home-paths.md)

**Approved 2026-10-17** ("committed, proceed", after `08470799`). This PLAN implements the MADR's
chosen option (R1–R5), with Deviation 1's scope.

* The owner approves the PLAN by name.
* Go files pass `make pre-add-check`. Dart files pass `dart format`, and the phase runs
  `make preflight`.
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

No tracked file carries the development account name (`<dev-account>` in these records) in a
home path. A test fails whenever a tracked file
gains a home path whose account segment is not a known placeholder.

## Scope

* ~~The 28 files the survey found:~~ Every file H1's guard flags: 37 with the development
  account, and 2 reports with `alpine` (Deviation 1). The survey's 28 were:
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
   ~~`git -C <root> ls-files -z`~~ the git index, `.git/index` (Deviation 2).
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

1. A new tracked file containing `/home/` followed by a name that is not a placeholder, then `/x`:
   the test fails, naming the file, the line and the name. (Reworded 2026-10-17, Deviation 1: the
   example's name tripped the guard in this record.)
2. The same name in a JSON-escaped `C:\\Users\\<name>\\x`: it fails.
3. ~~`git ls-files` replaced by a command that lists nothing, through an empty `PATH` `git` stub~~
   An index with no entries, and a missing index (Deviation 2): it
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

* Tests, fixtures, scripts and records. ~~No product code changes.~~ One product file changes, in a
  comment only (`chat_screen.dart`, Deviation 1). Rollback is reverting the commit, which would
  restore the account name, so it is not expected.

## Execution record

### Deviation 1, 2026-10-17: the guard found more than the survey

* **Found** in H1 step 3. On the tree, `TestTrackedFilesUseOnlyPlaceholderHomes` failed with 108
  hits in 40 files:
  * 104 on the development account, in 37 files where Scope listed 28 (first written here as 39
    and 42; corrected from H2's per-file totals);
  * 2 on `alpine`, in the 0098 and 0099 reports;
  * 2 in this PLAN's own H3 text.

  The survey's file list came from a `git grep` using `\b`, which this host's `git grep` does not
  support. The MADR's amendment of 2026-10-17 has the detail.
* **Decision.** The owner chose "Extend to all flagged":
  * H2 covers every file the guard flags;
  * `alpine` is replaced with `<user>`, not allowlisted, per H1.2;
  * H3's examples are reworded to an angle-bracket name.
* **Scope added.**
  * `apps/mobile/lib/features/chat/chat_screen.dart`, a comment;
  * Dart tests: `chat_render_test.dart`, `chat_send_failure_test.dart`, `jump_to_latest_test.dart`,
    `mode_selector_dangerous_test.dart`, `model_command_test.dart`, `permission_loop_test.dart` and
    `thinking_mutability_ui_test.dart`;
  * records and reports: `0089-MADR-long-running-session-stability.md`,
    `0098-REPORT-install-verification-sweep.md` and `0099-REPORT-reverification.md`;
  * this PLAN's H3 wording.
* H1.2's seed list named `alpine` and `...`. The guard's allowlist has neither: `alpine` is
  replaced, and `...` ends at the first `.` as an empty segment.

### Deviation 2, 2026-10-17: the guard may not run `git`

* **Found** in H4. `go test -race ./...` failed on macOS, and `make ci-windows` failed on the
  Windows test host, in the same test. The new guard itself passed on both.

  ```text
  --- FAIL: TestNoTestResolvesABareBinaryName
      pathbinaries_test.go:114: internal/homepaths_test.go:77: exec.Command("git") resolves a bare binary name from PATH: it will behave differently under Git Bash and PowerShell, and CI (shell: bash) cannot see the difference. Re-exec the test binary as a helper process instead — see MADR 0147 D9/D10.
  ```

  MADR 0147 D11 allows only `go` as a bare name. H1.1 and R3 said `git ls-files`, which breaks it.
* **Decision.** The owner chose "Read .git/index in Go". The guard reads the tracked-file list
  from the git index itself:
  * the `DIRC` header and entry paths, index versions 2, 3 and 4;
  * a `.git` file's `gitdir:` for a worktree;
  * SHA-256 repositories, read from `extensions.objectFormat`.

  It fails if there is no index, if the index is malformed, or if it holds no entries. No process
  is spawned.
* **Changed.**
  * H1.1: ~~lists files with `git -C <root> ls-files -z`~~ reads `.git/index`.
  * H3 plant 3: ~~a `git` stub that lists nothing~~ an index with no entries, and a missing
    index.
  * New: a test that builds small v2 and v4 indexes by hand, and requires the parser's paths.
  * The MADR's R3 is amended to match.
* **Scope.** No file joins the phase. The parser lives in `internal/homepaths_test.go`.
* **Harness note.** For the Windows run, the scratch harness's unpacked copy gets `git init` and
  `git add -A`, so it has an index of exactly the archived files. That is a scratch tool, not a
  committed file.

### H1, 2026-10-17: the guard

`internal/homepaths_test.go`:

* **`TestTrackedFilesUseOnlyPlaceholderHomes`**, as R3 describes, reading `.git/index` after
  Deviation 2. It fails below 100 listed or 100 read files.
* **`placeholderHomes`**, the survey's placeholders less `alpine` and `...` (Deviation 1), each
  with a comment where its origin is not obvious.
* **`TestIndexPathsReadsEveryVersion`**, added by Deviation 2.

On the tree before H2 (after Deviation 1's record edits), the guard failed with exactly the
expected hits:

```text
104 × home path account "<dev-account>" is not a placeholder   (37 files)
  2 × home path account "alpine" is not a placeholder          (0098, 0099 reports)
```

The first run, before those edits, had also flagged this PLAN's own H3 text twice, and one quote
in the MADR's amendment. Both were reworded.

### H2, 2026-10-17: the replacement

A scratchpad script took the account names as arguments, and the guard's log as its expected
per-file counts. It asserted each file's replacement count equal to the guard's, and printed
`total 106 in 39 files`.

* **Go and Dart tests** got `user`. Every other file got `<user>`.
* **Records:** each changed record gained one line after its title, "Account names in paths
  redacted on 2026-10-17 by 0196's MADR", as a link.
* **Scripts:** the two probe scripts take `REPO = Path(__file__).resolve().parents[1]`. Both
  compile (`py_compile`), and the expression resolves to the checkout's root.
* **Afterwards:**
  * the guard passes;
  * the survey finds only placeholders;
  * a case-insensitive `git grep` for the development account in home paths finds nothing.
* **Untested values:** every test value that changed was a whole path, such as a `cwd`, so no
  test asserted the bare name. One near miss is out of scope: `mac-host`, a generic relay host ID
  in four Dart tests, is not the account.

### H3, 2026-10-17: planted input

Each case ran on a fresh copy of the working tree, `.git` included, and the real tree was never
changed. `git` ran in the harness, never in the test.

```text
1  a tracked file with /home/<non-placeholder>/x   FAIL  docs/planted-unix.md:1: home path account "zqplantedname" is not a placeholder …
2  the JSON-escaped C:\\Users\\<same>\\x            FAIL  internal/planted.json:1: … "zqplantedname" …
3a an index with no entries                         FAIL  the git index lists 0 files: the guard is not reading the repository
3b no index                                         FAIL  read …/.git/index: open …: no such file or directory
4  /home/user.trust_level                           PASS
5  reader vs git ls-files, index v2                 1902 paths each, identical in order
5  reader vs git ls-files, index v4                 1902 paths each, identical in order
6  index v4 with case 1's file                      FAIL  docs/planted-unix.md:1: … "zqplantedname" …
P1 entry padding one byte short                     FAIL  paths = ["a" "bc" ""], want [… 6 paths …]
P2 version 3's extended flags word ignored          FAIL  paths = ["a" "" "dir/a.go"] (v3 case)
P3 version 4's prefix not applied                   FAIL  entry 6 has a bad path prefix
```

P2's first form deleted the whole block, so `flags` went unused and the copy did not compile.
That proved nothing, so it was re-planted as a condition that is always false, which compiles
and fails as shown.

### H4, 2026-10-17: gates

The first H4 run found Deviation 2. After it:

```text
go test -race -count=1 ./...                -> exit 0, 42 packages ok (D11's guard included)
make preflight                              -> exit 0: dart format 219 files (0 changed),
                                               flutter analyze "No issues found!",
                                               flutter test "+1473 ~3: All tests passed!"
make pre-add-check (homepaths_test.go, setup_test.go, plist_render_test.go)
                                            -> 3 file(s) clean (gofmt, golint, govulncheck)
make lint, make staticcheck                 -> exit 0
make ci-windows (the Windows test host)     -> ci-windows-exit=0; internal and internal/testexec ok
check_records.py --check-all, make markdownlint-docs -> exit 0
```

**Not done.** History is not rewritten (R4). The account name stays in published commits, and
rewriting them is the owner's decision alone.
