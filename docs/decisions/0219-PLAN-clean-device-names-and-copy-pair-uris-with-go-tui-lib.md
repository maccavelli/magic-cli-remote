---
status: in-progress
date: 2026-10-31
associated-madr: "0219-MADR-clean-device-names-and-copy-pair-uris-with-go-tui-lib.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement cleaning device names and copying pair URIs with go-tui-lib

Associated MADR: [0219-MADR-clean-device-names-and-copy-pair-uris-with-go-tui-lib.md](0219-MADR-clean-device-names-and-copy-pair-uris-with-go-tui-lib.md)

## Goal

Every device name Magic stores or prints is cleaned by `safetext`. `pair code --copy`, and bare
`pair --copy`, put the pair URI on the operator's clipboard through the terminal. Magic links
nothing from go-tui-lib but `safetext`, `termcap` and `termsvc` (MADR D1 to D4), and a test that
every `go test` runs holds it so (D6, issue #30).

## Scope

**Files:**

* `go.mod`, `go.sum`: `github.com/maccavelli/go-tui-lib/v2` ~~`v2.6.2`~~ `v2.7.0` (*deviation
  1*), and F8's five indirect modules (D1);
* ~~`check_libs.json`: the library, with `./internal/auth/...` and `./internal/cli/...` (D1);~~
  held for a follow-up, after go-shared supports a `/v2` path (*deviation 5*);
* `internal/auth/store.go`: `sanitizeDeviceName`'s character pass (D2);
* `internal/auth/store_terminal_test.go` (new): F6's names through `sanitizeDeviceName` (D2);
* `internal/auth/store_test.go`: `TestCreateSanitizesDeviceName`'s residue assertion, to D2's
  result (*deviation 3*);
* `internal/cli/termtext.go` (new): the helper that cleans a name for a terminal line (D3);
* `internal/cli/pair.go`: the five prints (D3), and `--copy` on `pair` and `pair code` (D4);
* `internal/cli/pair_terminal_test.go` (new): the prints and `--copy` (D3, D4);
* `internal/modguard_test.go`: D6's rules beside the `acp-go-sdk2` ones (D6);
* `internal/cli/examples.go`: one `--copy` example each in `pairExample` and `pairCodeExample`;
* `README.md:703`: `--copy` in `pair` / `pair code`'s flag row;
* `docs/README.md`: the index rows for this pair (`check_records.py --write-index`), and no
  other row (*deviation 2*);
* this pair.

**Out:** D5's packages; `pair create --copy` (Q1); rewriting stored names (Q3); anything the
daemon sends the phone beyond what D2 stores.

## Constraints

* **C1 — 0217's phase 3 lands first.** Its files are staged now. This pair is written but not
  staged until the owner commits that phase, so no commit holds both records and source.
* **C2 — Seen failing first, on a scratch copy.** Every new test fails against `cd2245ea`'s code in
  `leftovers/copies/0219-red`, never in the tree.
* **C3 — Stage only.** The owner commits with `git commit --no-edit`. `make pre-add-check
  FILES="…"` passes on each staged `.go` file first.
* **C4 — Host runs need the owner's word** (Q4 below, given 2026-10-30). Only scratch trees are
  used on the hosts, and they are removed afterwards.
* **C5 — No identifier in a record.** `leftovers/guard_preview.py` runs before staging.
* **C6 — A deviation is a stop** (AskUserQuestion, then a dated entry here, and in the MADR when a
  decision or a fact changes).
* **C7 — A newer go-tui-lib tag at P0 is a stop.** The pin is the current release (0217 D1).

## Implementation Steps

### P0 — Before the first write (read only)

1. `git log -1` shows 0217's phase 3 committed, and `git status --short` shows only this pair
   (*from 2026-10-31, also the 0220 report and its index row, which this plan leaves alone:
   deviation 2*).
2. `git ls-remote --tags https://github.com/maccavelli/go-tui-lib.git`: ~~`v2.6.2` is still the
   newest, and `v2.6.2^{}` is `696f4af…`~~ `v2.7.0` is still the newest, and `v2.7.0^{}` is
   `b8a2021e…` (*deviation 1*). Otherwise C7.
3. The MADR's answers to Q1 to Q3 are recorded, and the steps below follow them.

### S1 — The tests, seen failing first (D2, D3, D4)

1. `leftovers/scratch_copy.py 0219-red`: a copy of `HEAD` with nothing laid over it. In the copy:
   `go get github.com/maccavelli/go-tui-lib/v2@v2.7.0` (*was `@v2.6.2`; deviation 1*) and
   `go mod tidy`, so the tests compile.
2. Add to the copy only:
   * `internal/auth/store_terminal_test.go`, `TestSanitizeDeviceNameRemovesTerminalControls`: each
     F6 row's input gives the `safetext` column, after the sanitiser's other duties. The zero-width
     joiner is kept. ~~The existing `sanitizeDeviceName` tests stay as they are.~~ Invalid UTF-8
     gives U+FFFD (*deviation 4*). `TestCreateSanitizesDeviceName` expects `"evil name"` where it
     expected `"ev[31mil name"`, and its comment says why; its other checks stay (*deviation 3*).
   * `internal/cli/pair_terminal_test.go`:
     * `TestPairPrintsCleanNames`: a `devices.json` written directly, as a legacy record would be,
       with F6's names. `pair list`, `pair revoke <id>` and `pair prune` print each name cleaned,
       and no byte of an escape sequence or a bidi control reaches the output.
       `pair code --name` with a hostile name prints it cleaned.
     * `TestPairCodeCopyRefusesNonTerminal`: `pair code --copy` with stdout a buffer fails, names
       `--copy` in its error, and leaves `pair_codes.json` without a new code.
     * `TestPairCodeCopyWritesOSC52`: with the terminal check replaced through a package variable,
       `pair code --copy` writes `termsvc.CopySequence`'s bytes for the URI it printed, after the
       URI line, then the line saying the copy is unconfirmed. Bare `pair --copy` does the same.
     * `TestPairCreateHasNoCopy`: `pair create --copy` is an unknown flag.
3. Run them in the copy. Each must fail, for the reason it names: hidden characters kept, the
   residue of an escape sequence, `--copy` unknown. A test that passes there is wrong, and is
   rewritten before S2.
   * `TestPairCreateHasNoCopy` is the exception: it passes against `cd2245ea`, where no command
     has `--copy`. It is shown failing in a second copy, `leftovers/copies/0219-create-copy`,
     made after S2 with `--copy` also bound to `pair create`.
4. Record each failure's output here.
5. **D6's test** passes against `HEAD`, which imports no go-tui-lib package, so it too is shown
   failing after S2. Each plant is its own scratch copy of the tree as S2 leaves it, under
   `leftovers/copies/0219-guard-<plant>/`, with one file added and, where the plant needs a module,
   `go get` run in that copy only:
   * `ask-outside`: `internal/session` imports `go-tui-lib/v2/ask`, an unlisted package outside the
     importer scope;
   * `palette-inside`: `internal/cli` imports `go-tui-lib/v2/palette`, an unlisted package inside
     it;
   * `allowed-outside`: `internal/session` imports `go-tui-lib/v2/safetext`, a listed package
     outside the scope;
   * `bubbletea`: `internal/cli` imports `charm.land/bubbletea/v2`, which only the closure rule sees;
   * `v1-path`: `internal/cli` imports `github.com/maccavelli/go-tui-lib/safetext` at `v1.13.0`.

   `go test -count=1 -run` D6's test in each copy must fail, naming that plant's rule and the
   importing package. Each failure is quoted here. The copies are then removed.

### S2 — The change (D1 to D4)

1. In the tree: `go get github.com/maccavelli/go-tui-lib/v2@v2.7.0` (*was `@v2.6.2`;
   deviation 1*), then `go mod tidy`. The
   `go.mod` diff is F8's six lines and nothing else. `go 1.27.3` is unchanged.
2. ~~`check_libs.json`: the library, with its packages (Scope).~~ Held (*deviation 5*).
3. `sanitizeDeviceName`: `safetext.Clean(name, safetext.Line())` replaces the `unicode.IsControl`
   loop. The rest of the function is unchanged, and so is its doc comment's list, with the first
   item reworded for what `safetext` removes.
4. `internal/cli/termtext.go`: one unexported function, `termLine(s string) string`, which returns
   `safetext.Clean(s, safetext.Line())`, with a doc comment naming D3.
5. `pair.go`:
   * The five prints of F5 pass the name through `termLine`.
   * `--copy` on `pair` and `pair code`, both bound to one variable, as `--qr` is. The help text
     says it copies the URI through the terminal, unconfirmed.
   * Before `codes.Create`, `--copy` with stdout not a terminal is an error. The terminal check is
     the one `printPairQR` uses (`os.ModeCharDevice`), moved into a package variable so the test
     can replace it. `printPairQR` uses the same variable.
   * After the URI line, `--copy` writes `termsvc.CopySequence(termcap.EnvCaps(termcap.Env(os.Environ()), runtime.GOOS), uri)`,
     then "Copied the pair URI through the terminal (OSC 52); the terminal does not confirm
     it." A `CopySequence` error is printed as a warning, and the code stays valid.
6. `examples.go` and `README.md:703`: the flag (Scope).
7. The S1 tests, copied into the tree, pass.
8. `internal/modguard_test.go` (D6): a test that runs `go list -deps -test -json ./...` from the
   module root (`..`), with a timeout, decodes each package's `ImportPath`, `Imports` and `Deps`, and
   reports every violation of D6's four rules at once, each naming the importing package. Its
   rules are in plain functions over the decoded packages, so a table test drives each rule with
   no toolchain, as `TestGoModProblemsRejectsWhatItMust` does for the `acp-go-sdk2` rules. The
   real-tree test passes, and is then shown failing by S1 step 5.

### S3 — Gates on this Mac

* `make pre-add-check FILES="…"` on each changed `.go` file;
* `go vet ./...` for darwin/arm64, windows/amd64 and linux/amd64, cgo off;
* `make staticcheck` (its ignored count stays 56), `make vulncheck`, `make race`, `make preflight`;
* `go list -deps ./cmd/...`: no `charm.land/…`, and no `github.com/charmbracelet/…` but `x/ansi`
  and its `parser`; D6's test, which holds the same and more on every `go test`, passes;
* check-libs on the phase's content: `leftovers/check_libs_staged.py` with the changed files, as
  0217's P3 ran it. ~~`go-tui-lib/v2` is checked, and~~ every check is ok (*deviation 5: the
  two libraries already named are checked; go-tui-lib waits for the follow-up*);
* by hand, on this Mac's terminal and inside tmux: `mcremote pair code --name probe --copy
  --data-dir <scratch>`, then the clipboard holds the URI. A scratch data directory is used, so no
  daemon accepts the code. The result is recorded, including a terminal that drops the copy.

### S4 — Gates on the test hosts (C4)

`leftovers/pins_hosts.py` with `PINS_PHASE=0219`: `go test -race ./...` on the Linux test host,
and `make ci-windows` on the Windows test host, over `HEAD` plus the changed files.

### S5 — Records and staging

1. This plan's execution record: each step's output.
2. `python3 scripts/check_records.py --write-index`; `make check-records`;
   `make markdownlint-docs`; `leftovers/guard_preview.py`.
3. Stage Scope's files. `docs/README.md` is staged as this pair's two rows only, through
   `git apply --cached` of that hunk, and `check_records.py --check-all` runs on the staged tree
   (*deviation 2*). The owner commits. After the commit, `make check-libs` on `HEAD`.
4. `leftovers/guard_range.py` before asking the owner to push. After the push, every Go job and
   the Docs workflow pass; record the run ids.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | S1's tests were seen failing on the scratch copy, and pass in the tree | D2, D3, D4 |
| V2 | `go.mod` gains exactly F8's six lines; `go list -deps ./cmd/...` links no Charm package but `x/ansi` and `x/ansi/parser` (*amended 2026-10-30, MADR F10: it said `x/ansi` alone*) | D1 |
| V6 | D6's test passes in the tree, and failed in each of S1 step 5's five plant copies, naming the plant | D6 |
| V3 | `make check-libs` checks `go-tui-lib/v2`, and passes on the committed `HEAD` (*open until the go-shared change and the follow-up of deviation 5; this plan stays `in-progress` until then*) | D1 |
| V4 | S3's and S4's gates pass | all |
| V5 | `pair code --copy` put the URI on this Mac's clipboard, in a terminal and in tmux, or the record says where it did not | D4 |

## Rollout and Rollback

* **Rollout:** the next release carries it. Nothing on the wire changes, and the phone needs no
  change.
* **Rollback:** revert the commit. `go.mod`, `go.sum`, `check_libs.json` and the code return
  together. Names stored while it was in place stay cleaned, which is harmless.

## Owner decisions needed at approval

* **Q1 to Q3:** the MADR's questions.
* **Q4 — The host runs (C4).** S4 runs on the Linux and Windows test hosts, in scratch trees.

*Answered by the owner, 2026-10-30:* Q1 "pair code only (Recommended)"; Q2 "Store and print
(Recommended)"; Q3 "Leave them (Recommended)"; Q4 "Yes, both hosts (Recommended)". The steps
above already follow these answers, so none changes. C4 is met. The plan waits for approval.

*Amended 2026-10-30, before approval,* for issue #30, on the owner's word "Amend 0219 now
(Recommended)": MADR F10 and D6, `internal/modguard_test.go` in Scope, S1 step 5, S2 step 8, S3's
gate line, V2's wording and V6. The commit that lands this plan references #30.

## Not done by this plan

* D5's packages, and any terminal client for Magic.
* Cleaning names stored before D2, on disk or in what the daemon sends (MADR Q3).
* Confirming a copy: OSC 52 has no reply.

## Execution record

**Approval, 2026-10-30.** The owner wrote "proceed, approved and accepted": the MADR, D6 included,
is accepted, and this plan is approved. C4 was answered before (Q4).

**P0, 2026-10-30 (superseded by the re-run below).** `git log -1`: `a7ca1f0d`, the commit of this
pair, after 0217's phase 3 (`c2fcd3db`) and its records. `git status --short` was empty.
`git ls-remote --tags` on go-tui-lib: `v2.6.2` is the newest v2 tag, and `v2.6.2^{}` is
`696f4af5…`, so C7 does not apply. Q1 to Q3 are answered in the MADR. Before S1, the owner held the
plan for a new go-tui-lib tag (deviation 1).

**P0, re-run 2026-10-31.**

* `git log -1`: `814a8bd9` (0217's `acp-go-sdk2` v1.28.0, pushed). Between `cd2245ea` and it,
  `git diff --stat` of `internal/auth`, `internal/cli`, `internal/ws`, `internal/modguard_test.go`
  and `check_libs.json` is empty. So a red copy of `HEAD` tests the code C2 names.
* `git status --short`: this pair, plus `docs/README.md` and
  `docs/reports/0220-REPORT-opencode-v2-parity-assessment.md` (deviation 2).
* `git ls-remote --tags` on go-tui-lib: `v2.7.0` is the newest v2 tag (after `v2.6.2`), and
  `v2.7.0^{}` is `b8a2021ec7c6…`. C7 applied; deviation 1 resolves it.

**S1, 2026-10-31.**

* `leftovers/scratch_copy.py 0219-red`: `814a8bd9`. In the copy: `go get …/v2@v2.7.0`, the two new
  test files, `TestCreateSanitizesDeviceName`'s new assertion (deviation 3), and
  `go mod tidy`. The copy also got `internal/cli/zred_seam_test.go`, which declares
  `stdoutIsTerminal` returning false. S2 declares it in `pair.go`. Without it, the package would not
  compile against the old code, and every test would fail on an undefined name rather than on
  behaviour. It is never in the tree.
* `go test -count=1 -run '^(…the six…)$' -v ./internal/auth/ ./internal/cli/`, exit 1
  (`leftovers/0219-s1-red.log`):
  * `TestSanitizeDeviceNameRemovesTerminalControls` failed on six rows. Each quotes the old
    output: `"phone‮enohp"`, `"ab⁦cd⁩ef"`, `"pixel​‍name"`,
    `"red[31mtext[0m"`, `"c131mtext"` and `"badutf8"`. Its line-separator, plain and bracket rows
    passed, as they must: they hold what D2 keeps.
  * `TestCreateSanitizesDeviceName`: `name="ev[31mil name" want "evil name"`.
  * `TestPairPrintsCleanNames`: residue and missing clean names in `pair list`, `pair revoke`,
    `pair prune`, `pair code` and `pair create`. For example, "pair list printed "\x1b" of a device
    name" and "pair code did not print "redtext"". The test also covers `pair create`'s print, the
    fifth of F5's, which S1 step 2 did not list.
  * `TestPairCodeCopyRefusesNonTerminal`: "error = unknown flag: --copy, want one naming --copy and
    the terminal".
  * `TestPairCodeCopyWritesOSC52`, both subtests: "unknown flag: --copy".
  * `TestPairCreateHasNoCopy` passed, as step 3 expected.
* `leftovers/tree_copy.py 0219-create-copy create-copy`, after S2: the tree's changes over
  `814a8bd9`, with `--copy` also bound to `pair create`. `TestPairCreateHasNoCopy` failed:
  "pair create --copy: err = <nil>, want an unknown flag" (`leftovers/0219-s1-create-copy.log`).
* Step 5, after S2: `leftovers/d6_plants.py` (`leftovers/0219-s1-d6-plants.log`). Each plant is a
  copy of the tree with one file or edit, `go get` where named, and `go mod tidy`. Then
  `go test -run '^(TestGoTUILibStaysBounded|TestTUIProblemsRejectsWhatItMust)$' ./internal/` was
  run, and the copy removed. Every plant failed, naming its rule and the importing package:
  * `ask-outside`: "internal/session imports …/v2/ask: only safetext, termcap and termsvc are
    allowed".
  * `palette-inside`: "internal/cli imports …/v2/palette: only safetext, termcap and termsvc are
    allowed", and 13 "links" lines each for `internal/cli` and `cmd/mcremote`, from
    `charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, `ultraviolet` and `x/term` to
    `x/windows`.
  * `allowed-outside`: "internal/session imports …/v2/safetext: only internal/auth and internal/cli
    may import go-tui-lib".
  * `bubbletea` (`go get charm.land/bubbletea/v2@latest`, `v2.1.1`): "internal/cli links
    charm.land/bubbletea/v2: nothing from Charm but x/ansi and x/ansi/parser", and six more Charm
    packages it brings, for `internal/cli` and `cmd/mcremote`.
  * `v1-path` (`go get github.com/maccavelli/go-tui-lib@v1.13.0`): "internal/cli imports
    github.com/maccavelli/go-tui-lib/safetext: the v1 module path; require
    github.com/maccavelli/go-tui-lib/v2".
  * `rules-off`, added beyond step 5's five, because `TestTUIProblemsRejectsWhatItMust` is new
    too: `tuiProblems` returns nil, and the fixture test failed on its six bad rows ("ask outside
    the importers: accepted, but the guard must reject it", and so on).

  The `ask-outside` plant reports no Charm module: `v2/ask` is a line-mode package and links none.

**S2, 2026-10-31.**

* `go get github.com/maccavelli/go-tui-lib/v2@v2.7.0` and `go mod tidy`. `git diff go.mod`: the
  direct line and the five indirect ones of F8, and nothing else. `go 1.27.3` is unchanged.
* `check_libs.json`: `github.com/maccavelli/go-tui-lib/v2`, with `./internal/auth/...` and
  `./internal/cli/...`. Taken out again in S3 by the inverse edit (deviation 5). `git diff` of
  the file is empty.
* `store.go`: the loop and the `unicode` import are gone. One line,
  `strings.Join(strings.Fields(safetext.Clean(name, safetext.Line())), " ")`, does the character
  pass and the collapse. The doc comment's first item now says what `safetext` removes, U+FFFD
  included. `unicode/utf8` stays for the rune-boundary cut.
* `internal/cli/termtext.go`: `termLine`. `pair.go`:
  * the five prints of F5 use `termLine`;
  * `--copy` is bound to `pair` and `pair code`, with `copyFlagUsage`;
  * the refusal is the first statement of `codeFn`, before the config loads or
    `OpenPairCodeStore` creates `pair_codes.json`;
  * `stdoutIsTerminal` is the package variable, and `printPairQR` uses it. Its result is the same
    as before: a non-file writer or a failed `Stat` leaves the QR off by default, as it did;
  * `copyPairURI` writes the sequence and the line. Its warning goes to stderr, as
    `warnIfDaemonMissesDataDir`'s does.
* `examples.go`: one `--copy` line in each of `pairExample` and `pairCodeExample`. `README.md:703`:
  `--copy` in the `pair` / `pair code` row.
* The S1 tests, copied into the tree: `go test -count=1 ./internal/auth/... ./internal/cli/...
  ./internal/ws/...` exit 0, and the six pass by name (`leftovers/0219-s2-green-v.log`).
* Step 8: `TestGoTUILibStaysBounded` and `TestTUIProblemsRejectsWhatItMust` in
  `internal/modguard_test.go`. The real-tree test takes 2.2 s here. `go test -count=1 -v ./internal/`
  exit 0 (`leftovers/0219-s2-modguard.log`).

**S3, 2026-10-31, on this Mac.**

* `make pre-add-check FILES="…the eight .go files…"`: "8 file(s) clean (gofmt, golint, govulncheck)".
* `leftovers/vet_goos.py`: `go vet ./...` exit 0 for darwin/arm64, windows/amd64 and linux/amd64,
  cgo off.
* `make staticcheck`, first run, exit 2: four ST1018 findings in
  `internal/auth/store_terminal_test.go:13-16`. The file held the bidi and invisible characters
  themselves where escapes were meant, because the tool that wrote it decoded the `\u` escapes.
  `leftovers/nonascii_scan.py` found eight such characters there (one more, a U+FFFD, in this
  plan's deviation text). `leftovers/escape_literals.py` rewrote each as its `\u` escape, with an
  asserted count. A rescan of the new Go files found none. The strings are the same at run time,
  so S1's red results stand. Second run: exit 0 on linux, darwin and windows, with 56 ignored
  findings each, as before.
* `make vulncheck`: "clear; allowlisted required-module findings: GO-2026-5932", as before.
* `make race`: exit 0, 45 `ok` packages, with no FAIL, panic or DATA RACE line
  (`leftovers/0219-s3-race.log`).
* `make preflight`: exit 0. `flutter analyze` "No issues found!", 1501 Flutter tests "All tests
  passed!", "preflight passed" (`leftovers/0219-s3-preflight.log`).
* `go list -deps ./cmd/...`: from Charm, `github.com/charmbracelet/x/ansi` and `x/ansi/parser`
  only, and no `charm.land/…`. `TestGoTUILibStaysBounded` holds the same on every `go test`.
* `leftovers/check_libs_staged.py` on the changed files without `check_libs.json` (deviation 5):
  every check of `acp-go-sdk2` and `go-selfupdate-lib` ok, "0 failing checks"
  (`leftovers/0219-s3-checklibs-staged.log`).

**S4, 2026-10-31.** `PINS_PHASE=0219 leftovers/pins_hosts.py`, with the ten changed files named
(its default, `git diff`, would miss the three new ones):

* Linux test host, go1.27.3 linux/amd64: `go test -race -count=1 ./...` exit 0, 45 `ok` lines.
* Windows test host, go1.27.3 windows/amd64: the dry run, then `make ci-windows` exit 0,
  "ci-windows-local: ALL SELECTED CHECKS PASSED", 45 `ok` lines.
* Both scratch trees removed, and their absence checked.

**S3, the owner's manual check, 2026-10-31.** In this Mac's terminal, `pbpaste` gave the printed
URI. In tmux it did not (deviation 6). The scratch data directory, which no daemon reads, was
deleted after the first run, and again after the later ones.

**Deviation 6's steps, 2026-10-31.**

* **S1 step 2b.** `leftovers/tree_copy.py 0219-red-tmux`: the tree as S2 left it, plus a
  test-only declaration of `runTmuxLoadBuffer` (`zred_seam_test.go`, never in the tree).
  `go test -run '^(TestPairCodeCopyWritesOSC52|TestPairCodeCopyUsesTmuxBuffer|TestPairCodeCopyFallsBackWhenTmuxFails)$'`
  gave exit 1 (`leftovers/0219-s1b-red-tmux.log`):
  * `TestPairCodeCopyUsesTmuxBuffer`: "tmux load-buffer got [], want the printed URI once".
  * `TestPairCodeCopyFallsBackWhenTmuxFails`: "warning at -1". The old code wrote OSC 52 and its
    tmux passthrough copy, and no warning.
  * `TestPairCodeCopyWritesOSC52` passed, as it must: it only gained `TMUX=""` and a guard that
    the seam is not called.
* **S2 step 5b.** `pair.go`:
  * `runTmuxLoadBuffer`: `termsvc.TmuxLoadBuffer()` under a 5 s timeout, with the URI on standard
    input. tmux's output passes through `termLine` before it reaches an error.
  * `copyPairURI` follows `termsvc.CopyPlan`.
  * `copyFlagUsage` and `README.md:703` name tmux's buffer.

  The six pair tests pass in the tree (`leftovers/0219-s2b-green.log`).
* **A gate the change failed, and its fix.** `make race` failed in `internal/procutil`:
  `TestNoBareSpawnOutsideProcutil` reported "internal/cli/pair.go:383:9: exec.CommandContext".
  The repository starts every child through `procutil.Command`, which on Windows also decides
  `CREATE_NO_WINDOW` (MADR 0159 D19). `runTmuxLoadBuffer` now uses `procutil.Command`, and
  `os/exec` is no longer imported. The host run in flight at the time (`PINS_PHASE=0219b`) failed
  the same test on both hosts, with 44 other packages ok on each. Its trees were removed.
* **The gates again, on the final tree.**
  * `make pre-add-check` on the eight files: clean.
  * `leftovers/vet_goos.py`: exit 0 for all three systems.
  * `make staticcheck`: exit 0, 56 ignored findings on each system.
  * `go test ./internal/procutil/ ./internal/cli/`: ok.
  * `make race`: exit 0, 45 `ok` packages, and no FAIL, panic or DATA RACE line
    (`leftovers/0219-s3c-race.log`).
  * `leftovers/nonascii_scan.py` on the new Go files: none.
  * `make preflight`: exit 0. `flutter analyze` "No issues found!", 1501 Flutter tests "All tests
    passed!", "preflight passed" (`leftovers/0219-s3c-preflight.log`).
  * `PINS_PHASE=0219c leftovers/pins_hosts.py`: on the Linux test host, `go test -race` exit 0
    with 45 `ok` lines. On the Windows test host, `make ci-windows` exit 0, "ALL SELECTED CHECKS
    PASSED", with 45 `ok` lines. Both trees were removed.
* **The owner's manual check again, on the final tree (V5).** The owner ran the binary rebuilt
  from the final tree, with the same command as before, in this Mac's terminal and inside tmux with
  no config file. The owner reported "both the terminal and tmux tests were successful": in each
  case `pbpaste` gave the printed URI. V5 is met. The scratch data directory was deleted
  afterwards.

**S5, 2026-10-31.**

* `check_records.py --write-index` also wrote a row for an untracked
  `docs/decisions/0221-MADR-harden-httpagent-sse-liveness-parser-and-gap-resync.md`, which had
  appeared in the tree since P0. It was not written in this plan's session either. Deviation 2's
  decision covers it: that row was taken out again by the inverse edit, and `docs/README.md`
  hashed as before the run. Its other rows were unchanged: the generator writes this pair's two
  rows, and the 0220 row, exactly as the tree already held them.
* `make check-records`: exit 0.
* `make markdownlint-docs`: 57 files, "0 issues in 0 files". It is the same pair of targets
  that `docs.yml` and `ci.yml` run.
* `leftovers/guard_preview.py`: exit 0, "would edit 0 file(s), 0 path(s) need a rename by hand".
* Staged: Scope's files, and `docs/README.md` as this pair's two rows only, through
  `leftovers/stage_0219_index.py` (`git apply --cached` of the hunk with the 0220 row dropped).
  The 0220 report and the 0221 MADR are untracked, and their rows unstaged.
* `check_records.py --check-all` on the staged tree (`git checkout-index` into a scratch
  directory, which holds neither the 0220 nor the 0221 file): exit 0, no output.

### Deviations

**Deviation 1, 2026-10-30 / 2026-10-31: a newer go-tui-lib tag (C7).**

* **Found.** After approval, and before S1, the owner wrote "hold up. i am about to tag a new
  go-tui-lib release. we need to wait so we can update the pin." On 2026-10-31 the owner wrote
  "go-tui-lib v2.7.0 tagged, proceed".
* **Checked before any write, read only.**
  * `leftovers/release_diff.py` and `leftovers/tuilib_patches.py` over the compare
    `v2.6.2...v2.7.0`: 11 commits and 75 files. None is under `termsvc/` or is the library's
    `go.mod`.
  * `safetext/clean.go` adds a printable-ASCII early return, with the same output.
  * `termcap` adds `WithoutQuery`, `WithoutGatedQueries`, `WithoutQueries` and
    `ReasonUnknownQuery`, deprecates `WithDisabled`, and reads a probe's environment by the
    probe's system.
  * The advisories API answers `[]`. No release object exists for the tag.
  * `leftovers/tuilib_probe.py v2.7.0` (`tuilib_probe-v2.7.0.log`) and
    `leftovers/tuilib_clean_probe.py v2.7.0`, in a scratch copy of `814a8bd9`: the same `go.mod`
    lines, linked packages and F6 outputs as at `v2.6.2`.
* **Decision.** The owner's words: pin `v2.7.0`. Nothing but the version changes in this plan.
* **Records changed.** MADR F1 (amended), F6 and F8 (re-run notes), D1 (the version) and the
  evidence index. In this plan: Scope's `go.mod` line, P0 step 2, S1 step 1 and S2 step 1.

**Deviation 2, 2026-10-31: work that is not this pair's in the tree (P0 step 1).**

* **Found.** `git status --short` also shows the untracked
  `docs/reports/0220-REPORT-opencode-v2-parity-assessment.md`, and a `docs/README.md` change that
  adds this pair's two index rows and a 0220 row. Neither was written in this plan's session.
* **Options put to the owner.** Leave them, and stage only this pair's rows (recommended); hold S5
  until the 0220 work is committed; fold 0220 into this commit.
* **Decision.** "Leave; stage 0219 rows only (Recommended)". The 0220 report and its row are not
  touched. S5 step 3 stages `docs/README.md` as this pair's rows only.
* **Records changed.** Scope's `docs/README.md` line, P0 step 1 and S5 step 3.

**Deviations 3 and 4, 2026-10-31: what D2 changes in the existing behaviour (S1 step 2).**

* **Found, before S1's first write.** `leftovers/tuilib_existing_probe.py`, in the `v2.7.0` probe
  copy, ran `safetext.Clean(…, Line())` and the `Fields` join on the existing test's inputs:
  * `"  ev\x1b[31mil\x00  name\n  "`: the sanitiser gives `"ev[31mil name"`, which
    `TestCreateSanitizesDeviceName` asserts (`internal/auth/store_test.go:359`), and `safetext`
    gives `"evil name"`. The test fails after S2 by design. The plan said the existing tests stay
    as they are.
  * `"bad\xffutf8"`: the sanitiser gives `"badutf8"`, and `safetext` gives `"bad\ufffdutf8"`. No
    test covers it, and the MADR did not say so.
  * `"\x00\x1b\n"` still falls back to `"device"`, and `"device [work]"` and `"tab\there"` are the
    same under both.
* **Options put to the owner.** Deviation 3: update the assertion to D2's result (recommended);
  move the case to the new test; keep the residue. Deviation 4: U+FFFD as `safetext` does
  (recommended); drop invalid bytes as before; drop them and every U+FFFD.
* **Decisions.** "Update to D2's result (Recommended)" and "U+FFFD, as safetext does
  (Recommended)".
* **Records changed.** MADR D2 (amended). Here: Scope (`store_test.go`) and S1 step 2. The changed
  assertion is seen failing against the old code in the red copy, as S1 step 3 requires of a new
  one.

**Deviation 5, 2026-10-31: go-shared's check-libs cannot name a `/v2` module (S3).**

* **Found.** `leftovers/check_libs_staged.py`, with `check_libs.json` in the content, exit 2:
  "libraries[2] needs "module" as github.com/<owner>/<repo>"
  (`leftovers/0219-s3-checklibs-staged.log`, first run). go-shared's `scripts/check_libs.py:134`
  matches the module against `github\.com/[\w.-]+/[\w.-]+`, and `latest_tag` (`:206-213`) takes
  the newest semver tag of any major. The local go-shared is its remote's `HEAD`, `d62e2a23`.
  This is pre-existing in go-shared. Committing the entry would make `make check-libs` a config
  error for `acp-go-sdk2` and `go-selfupdate-lib` too. gobble-cli's own `check_libs.py` names the
  library with `path=` and `major=2` (`:88`).
* **Options put to the owner.** Fix go-shared, and hold the entry for a follow-up (recommended);
  fix go-shared before any of this lands; drop the check (named as a workaround).
* **Decision.** "Fix go-shared, hold the entry (Recommended)".
* **Done.** The entry was taken out by the inverse edit. `check_libs_staged.py` was run again on
  the rest: every check of `acp-go-sdk2` and `go-selfupdate-lib` is ok, and "0 failing checks".
* **Still to do, outside this commit.** A go-shared MADR and PLAN for `/vN` support, and, after
  it lands, a Magic change naming go-tui-lib in `check_libs.json`. V3 stays open, and this plan
  stays `in-progress` until then.
* **Records changed.** MADR D1 (amended) and its Confirmation line. Here: Scope, S2 step 2, S3's
  check-libs line and V3.

**Deviation 6, 2026-10-31: a default tmux drops the copy (S3's manual check, V5).**

* **Found.** The owner ran a scratch `mcremote pair code --name probe --copy --qr=false
  --data-dir <scratch>`, built from the tree. In this Mac's terminal, `pbpaste` gave the printed
  URI: a match. Inside tmux, `pbpaste` gave what the clipboard held before. The owner has no tmux
  config file. A throwaway tmux 3.7c server with `-f /dev/null` (`leftovers/tmux_defaults.py`)
  shows the defaults `set-clipboard external` and `allow-passthrough off`. Under those, tmux
  ignores a program's OSC 52 and drops the passthrough-wrapped copy. Those are the two
  `termsvc.CopySequence` writes inside tmux (`RouteOSC52Tmux`, "for a tmux whose set-clipboard
  is off"). This contradicts the MADR's consequence "Good, because an operator over SSH or in
  tmux can paste…".
* **Options put to the owner.** Add `termsvc`'s tmux-buffer route (recommended); record it and
  document `set -s set-clipboard on`; record it only.
* **Decision.** "Add the tmux-buffer route (Recommended)". Inside tmux, Magic runs
  `termsvc.TmuxLoadBuffer()` (`tmux load-buffer -w -`) with the URI on standard input, and falls
  back to the OSC 52 sequence when that fails. First, a probe by the owner proves the route under
  the default config. Then D4 is amended, and S1 and S2 gain the route's tests (seen failing
  first) and code, through a seam for the command.
* **Probe, 2026-10-31.** The owner, in a tmux session started with no config file:
  `printf probe-0219 | tmux load-buffer -w -`, then `pbpaste` printed `probe-0219`. The route
  works under tmux's defaults.
* **Steps added.**
  * **S1 step 2b.** In `pair_terminal_test.go`:
    * `TestPairCodeCopyUsesTmuxBuffer`: with `TMUX` set, the command seam is called once with
      the printed URI, no OSC 52 is written, and the tmux line follows the URI line.
    * `TestPairCodeCopyFallsBackWhenTmuxFails`: the seam fails, and the warning naming tmux's
      error is followed by `CopySequence`'s bytes, with tmux's caps.
    * `TestPairCodeCopyWritesOSC52` sets `TMUX` empty, and fails if the seam is called.

    All are seen failing against the tree as S2 left it, in a copy (`leftovers/tree_copy.py`)
    whose only addition is a test-only declaration of the seam.
  * **S2 step 5b.** `pair.go`: `runTmuxLoadBuffer`, a package variable that runs
    `termsvc.TmuxLoadBuffer()` with a timeout, with the URI on standard input, and cleans tmux's
    output with `termLine` before it reaches an error. `copyPairURI` follows `termsvc.CopyPlan`:
    `RouteTmuxBuffer` through the seam, then the OSC 52 routes through `CopySequence`.
  * **S3.** The gates again, and the owner's manual check in the terminal and in tmux.
* **Records changed.** MADR D4 (amended). Here: this entry, and S1, S2 and S3's records below
  them.
