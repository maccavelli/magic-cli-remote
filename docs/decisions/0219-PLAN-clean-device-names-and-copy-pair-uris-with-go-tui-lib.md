---
status: proposed
date: 2026-10-30
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

* `go.mod`, `go.sum`: `github.com/maccavelli/go-tui-lib/v2 v2.6.2`, and F8's five indirect
  modules (D1);
* `check_libs.json`: the library, with `./internal/auth/...` and `./internal/cli/...` (D1);
* `internal/auth/store.go`: `sanitizeDeviceName`'s character pass (D2);
* `internal/auth/store_terminal_test.go` (new): F6's names through `sanitizeDeviceName` (D2);
* `internal/cli/termtext.go` (new): the helper that cleans a name for a terminal line (D3);
* `internal/cli/pair.go`: the five prints (D3), and `--copy` on `pair` and `pair code` (D4);
* `internal/cli/pair_terminal_test.go` (new): the prints and `--copy` (D3, D4);
* `internal/modguard_test.go`: D6's rules beside the `acp-go-sdk2` ones (D6);
* `internal/cli/examples.go`: one `--copy` example each in `pairExample` and `pairCodeExample`;
* `README.md:703`: `--copy` in `pair` / `pair code`'s flag row;
* `docs/README.md`: the index rows for this pair (`check_records.py --write-index`);
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

1. `git log -1` shows 0217's phase 3 committed, and `git status --short` shows only this pair.
2. `git ls-remote --tags https://github.com/maccavelli/go-tui-lib.git`: `v2.6.2` is still the
   newest, and `v2.6.2^{}` is `696f4af…`. Otherwise C7.
3. The MADR's answers to Q1 to Q3 are recorded, and the steps below follow them.

### S1 — The tests, seen failing first (D2, D3, D4)

1. `leftovers/scratch_copy.py 0219-red`: a copy of `HEAD` with nothing laid over it. In the copy:
   `go get github.com/maccavelli/go-tui-lib/v2@v2.6.2` and `go mod tidy`, so the tests compile.
2. Add to the copy only:
   * `internal/auth/store_terminal_test.go`, `TestSanitizeDeviceNameRemovesTerminalControls`: each
     F6 row's input gives the `safetext` column, after the sanitiser's other duties. The zero-width
     joiner is kept. The existing `sanitizeDeviceName` tests stay as they are.
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

1. In the tree: `go get github.com/maccavelli/go-tui-lib/v2@v2.6.2`, then `go mod tidy`. The
   `go.mod` diff is F8's six lines and nothing else. `go 1.27.3` is unchanged.
2. `check_libs.json`: the library, with its packages (Scope).
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
  0217's P3 ran it. `go-tui-lib/v2` is checked, and every check is ok;
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
3. Stage Scope's files. The owner commits. After the commit, `make check-libs` on `HEAD`.
4. `leftovers/guard_range.py` before asking the owner to push. After the push, every Go job and
   the Docs workflow pass; record the run ids.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | S1's tests were seen failing on the scratch copy, and pass in the tree | D2, D3, D4 |
| V2 | `go.mod` gains exactly F8's six lines; `go list -deps ./cmd/...` links no Charm package but `x/ansi` and `x/ansi/parser` (*amended 2026-10-30, MADR F10: it said `x/ansi` alone*) | D1 |
| V6 | D6's test passes in the tree, and failed in each of S1 step 5's five plant copies, naming the plant | D6 |
| V3 | `make check-libs` checks `go-tui-lib/v2`, and passes on the committed `HEAD` | D1 |
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
