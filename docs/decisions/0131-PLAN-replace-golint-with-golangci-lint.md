---
status: "in-progress"
date: 2026-11-18
associated-madr: "0131-MADR-replace-golint-with-golangci-lint.md"
---
# Implement: replace golint with golangci-lint

Associated MADR: [0131-MADR-replace-golint-with-golangci-lint.md](0131-MADR-replace-golint-with-golangci-lint.md)

> **Update (2026-11-18), read first.** The owner ruled on 2026-11-18 to adopt golangci-lint v2.15.0 here, and the MADR is
> now `accepted`. The measurement of what that means is in the MADR's "Update (2026-11-18)" section. This plan was
> written on 2026-09-02 for a default set of five linters and 85 findings. **Its text below is kept, and its phases
> P1 to P5 are superseded in shape by "Update (2026-11-18): the revised phases" at the end of this file**, which
> follows the fleet's configuration (1671 findings). The five open questions below were answered by the
> owner the same day; see "Decided (2026-11-18)". The status is `in-progress`.

## Decided (2026-11-18)

Relayed by the coordinator. Each label is the owner's, quoted exactly and only as a label:

1. Q1, how the adoption runs: "Phased by linter; each phase turns on only linters already at zero (Recommended)". As put
   to the owner: phase 1 adopts the tool with the linters that are clean today and removes `golint`; each later phase
   fixes one group and switches its linter on, so CI is green and honest throughout and nothing is suppressed; it ends
   matching the fleet configuration apart from any differences the owner chooses.
2. Q2, the old linters: "Remove golint; keep the pinned staticcheck and its ignore bound (Recommended)".
3. Q3, unchecked errors: "Allow the explicit `_ =` discards here; fix the 332 bare calls (Recommended)". As put: a
   documented local difference from the fleet configuration (`check-blank: false`), matching this repository's guide;
   the 332 calls that silently drop an error are fixed, with command output going through one helper.
4. Q4, the goimports formatter: "Follow the fleet configuration and amend that sentence of the guide (Recommended)". As
   put: the same formatter rule as every sibling repository; the guide's sentence is updated to say gofmt plus
   goimports ordering; three files change.
5. Q5, the 35 deprecated-name findings: "Pair each with a `//nolint:staticcheck`, counted by the existing bound
   (Recommended)". As put: each existing, already-justified ignore gets the comment golangci-lint understands, and the
   script that bounds ignored findings is extended to count them, so the number cannot quietly grow. This is the one
   place the owner has chosen a `//nolint`: it applies to those 35 paired directives only.

**The coordinator's reading, recorded as the coordinator's and told to the owner:** the owner's "Yes, move it onto
golangci-lint v2.15.0 now" together with these five answers approves the PLAN as phased, each phase committed and pushed
by the coordinator after its own gate, each ending with CI green. Any phase that meets a finding whose fix is a real
choice (a behaviour change, an exported rename, a wire value, a security verdict that is not clear-cut) STOPS and comes
back to the owner with three options. P7's per-site security verdicts are written as a REPORT and put to the owner
before any of those fixes are built.

What this settles of the items the MADR update left "not decided": the phasing and how CI stays honest (Q1); that
`golint` goes and the pinned `staticcheck`, its ignore bound, `go vet` and `vet-tags` stay (Q2, which also keeps D4's
`go vet` in place, so D4's retirement of `go vet` is not adopted); one local difference from the fleet file,
`errcheck.check-blank: false`, because the guide allows an explicit, reasoned discard (Q3); the `goimports` formatter as
the fleet has it (Q4); and the form of the `SA1019` ignores (Q5). Any other difference from the fleet file is still the
owner's to choose, and none is chosen.

## Open questions for the owner (2026-11-18)

Each has three options, the recommended one first. They are in the order to answer them: the later ones depend on the
earlier. The options are kept as they were asked; each question is marked answered under its heading, with the owner's
label in "Decided (2026-11-18)" above. The measurements behind them are in the MADR update.

### Q1. One change or phased, and how CI stays honest between phases

**Answered 2026-11-18: A. "Phased by linter; each phase turns on only linters already at zero (Recommended)".**

* **A (recommended): phased, one linter group per phase, enabled in `.golangci.yml` only once it reports 0.** Every
  phase's configuration is the fleet file with the not-yet-clean linters left out of `enable:` (and the not-yet-clean
  `revive` rules left out of its list). Each phase ends with `golangci-lint` at 0 on linux, darwin, windows and every
  opt-in tag, and CI runs it from phase 1. The last phase's file equals the fleet file apart from the differences
  chosen in Q3 to Q5. Cost: nine phases (each a CI run), a configuration that grows, and the gate is weaker than the
  final one until the last phase, though never red. This is 0131 D3's rule ("at no point is the repository in a state
  where the gate fails on `master`") applied linter by linter.
* **B: one change.** The whole fleet configuration and all 1671 fixes (289 files, 211 of them production code) land
  before the gate flips, as 0131 D3 originally said, and `golint` stays the gate until then. Cost: the largest review
  this repository has had for a lint change, a long-lived branch against a tree that other plans keep changing (0246's
  cut-over, 0247), CI gains no lint gate until the end, and a regression bisects to "the lint change".
* **C: phased with a `new-from-rev` baseline.** The full fleet configuration is on from phase 1, and only findings in
  code changed after `e7de8568` fail; the old findings are fixed later. Cost: it is a baseline, which 0131's option D
  rejected and this repository's rules name as a workaround, the 1671 stay invisible to CI and age into permanence, and a
  touched line pulls in its neighbours' findings. Listed because every route was asked for; by this repository's own
  rule it is not a recommendation.

### Q2. `golint` and the repository's pinned `staticcheck`: removed, or kept beside golangci-lint

**Answered 2026-11-18: A. "Remove golint; keep the pinned staticcheck and its ignore bound (Recommended)".**

* **A (recommended): remove `golint`; keep the pinned `staticcheck`, its three-GOOS pass and its ignore bound; keep
  `go vet` and `make vet-tags`.** `golint` is archived and phase 1 enables the three `revive` rules that are its checks
  (`exported`, `package-comments`, `var-naming`) before it goes. `staticcheck` stays because the 0207 amendment H bound
  (`scripts/check_staticcheck_ignored.py`, Makefile `:618-652`) is built on its `-show-ignored` output and on
  `//lint:ignore`, which golangci-lint does not read. This is what `gobble-cli`, the nearest sibling and the only one
  that shares this repository's `acp-go-sdk2` dependency, does. Cost: `staticcheck` runs twice (about a minute per GOOS
  of CI time), and an `SA1019` has two ways to be silenced (Q5).
* **B: remove `golint` and the standalone `staticcheck`.** This is the shape of `go-llmprovider-sdk`, `go-tui-lib` and
  `go-selfupdate-lib`. golangci-lint's own `staticcheck` replaces it, the nine `//lint:` directives become
  `//nolint:staticcheck`, and the Makefile block `:618-652` and `check_staticcheck_ignored.py` go. Cost: the 0207
  amendment H D45 bound ("the only ignore allowed is `SA1019` on `acp-go-sdk2`'s unstable-schema names") needs a new
  mechanism written (for example `nolintlint` plus a script over `//nolint:staticcheck` reasons), and 0170 D3/D4 and 0207
  amendment H need dated amendments. One more phase.
* **C: keep both `golint` and `staticcheck`.** No behaviour change to the current gate; golangci-lint is added beside
  them. Cost: an archived tool stays in the gate, every `golint` check is checked twice (`revive` is in the fleet
  configuration), and the fleet's repositories have all moved off it.

### Q3. `errcheck`: the fleet sets `check-blank: true`, and this repository's guide allows what that reports

**Answered 2026-11-18: A. "Allow the explicit `_ =` discards here; fix the 332 bare calls (Recommended)".**

Measured: 825 findings, about 493 of them explicit `_ =` discards and about 332 bare calls (257 `fmt.Fprint*` to a
command's writer). `docs/guides/standards/go/logging.md` says ignoring an error is acceptable for "a clearly best-effort
cleanup/write where failure cannot affect correctness and the reason is evident in nearby code".

* **A (recommended): follow the fleet configuration except `check-blank`, which is `false` here, as a documented local
  difference** (with `check-type-assertions: true` kept). The 493 explicit discards stay, as the guide allows. The 332
  bare calls are fixed: the 257 `fmt.Fprint*` through one small output helper that discards the error in one place with
  its reason, the rest handled or discarded explicitly with a reason. Cost: one configuration line differs from the
  fleet, and a reviewer must still judge each new `_ =`, because the tool no longer does.
* **B: follow the fleet configuration exactly.** All 825 are fixed: each error is handled, or discarded with a
  `//nolint:errcheck // <reason>` that is the owner's choice, or the code is restructured. `logging.md` is amended to say
  that form. Cost: the largest phase by far (up to about 135 files), 493 sites of churn in code that is already explicit, and
  hundreds of `//nolint` lines.
* **C: A, plus `errcheck`'s `exclude-functions` for `fmt.Fprint*`, `Close` and `os.Remove*`.** Leaves about 25 edits.
  Cost: a function-class exclusion the fleet does not carry, which the fleet's rule treats as blanket, and the reasons
  for it are the guide's, not each site's.

### Q4. The `goimports` formatter against the guide's "do not introduce a competing formatter rule"

**Answered 2026-11-18: A. "Follow the fleet configuration and amend that sentence of the guide (Recommended)".**

3 findings. `goimports` is a superset of `gofmt` that also groups and prunes imports; it does not reflow unrelated code,
which is the reason `gofumpt` was refused (`AGENTS.md`).

* **A (recommended): follow the fleet: enable the `goimports` formatter beside `gofmt`, fix the 3 files, and amend
  `core.md`'s sentence to say plain `gofmt` is the layout authority and `goimports` only orders imports.** Cost: a
  guide sentence changes; `make fmt` and the pre-add `gofmt` step are unchanged, so a file `gofmt` accepts can still fail
  `goimports`.
* **B: a documented local difference: formatters are `gofmt` alone.** The guide is unchanged. Cost: one difference from
  the fleet file, and the 3 files stay as they are.
* **C: make golangci-lint's formatters the authority:** `make fmt` becomes `golangci-lint fmt`, and the `Gofmt` step of
  CI and `preflight` follows. Cost: the most files and scripts touched for 3 findings, and a second formatter entry
  point to keep in step with `scripts/go-precheck.sh`.

### Q5. The 35 `SA1019` findings that golangci-lint reports under `//lint:ignore`

**Answered 2026-11-18: A. "Pair each with a `//nolint:staticcheck`, counted by the existing bound (Recommended)".**

Six files (`plans.go` 11, `plans_test.go` 17, `session.go` 4, and one each in `acpagent.go`, `advertise_test.go` and
`turnusage.go`, all in `internal/provider/acpagent`) hold `acp-go-sdk2` unstable-schema names under 0207 amendment H.

* **A (recommended): pair each `//lint:ignore SA1019 ...` with a `//nolint:staticcheck // as the lint:ignore above`
  line, and put a file-level `//nolint:staticcheck` on the two files that carry `//lint:file-ignore`.** This is
  `gobble-cli`'s form. The fleet's `nolintlint` (a named linter, and an unused directive is a finding) keeps each one
  honest. Cost: about eleven source lines, and `check_staticcheck_ignored.py` should require the same reason text on the
  `//nolint` lines. Needs Q2 A or B.
* **B: one scoped entry in `.golangci.yml`:** `staticcheck`, text `SA1019` and `unstable ACP schema`, path
  `internal/provider/acpagent`, with the reason beside it. Cost: no source change, but it is a path-and-text exclusion
  that would also hide a new deprecation of the same kind anywhere in that directory.
* **C: leave golangci-lint's `staticcheck` out of `enable:`** and rely on the standalone pinned `staticcheck` (needs
  Q2 A). Cost: a local difference from the fleet, and the 16 findings golangci-lint's `staticcheck` adds that the
  standalone one never prints (`QF1001`, `QF1002`, `QF1003`, `QF1008`, `QF1011`, `SA9003`) are lost or must be fixed
  first and then not guarded.

Not asked, because the fleet configuration decides them and nothing in the guide conflicts: `goconst` (379),
`gocognit` 50 and `gocyclo` 40, `revive` `confusing-naming`, `gocritic`, `errorlint`, `gosec`, `bodyclose`, `unparam`.
The plan fixes them or, where a site cannot be fixed, reports it to the owner, because a `//nolint` is the owner's to
choose.

## Goal

`golangci-lint run ./...` reports zero findings, the pre-add gate enforces it in
place of `golint`, and CI enforces it too — with `master` green at every commit
along the way.

## Scope

### In scope

* `.golangci.yml` (new) — the pinned linter set (MADR D1, D2).
* `scripts/go-precheck.sh` — the single implementation of the pre-add rule.
* `Makefile` — the `lint` target.
* `.github/workflows/ci.yml` — a lint job; `go vet ./...` retired into it (D4).
* `AGENTS.md`, `README.md` — the rule is documented in both.
* Whatever Go files the 85 findings live in.

### Out of scope

* **`revive`** (MADR D5). Deferred with its measurement recorded; adopting it
  is a separate decision.
* `gofmt` and `govulncheck` — untouched. Only the middle third of the pre-add
  rule changes.
* `gofumpt`. Still rejected, for the reason `AGENTS.md` already gives: it
  reflows unrelated code and buries real changes in noise.
* The Dart/Flutter side of the gate.
* The per-machine hooks in `~/.global-agent-hooks/`. They call
  `scripts/go-precheck.sh` from the repository being staged into, so they pick
  this up with no change — which is worth verifying (P3) rather than assuming.

## Stability rule

**`master` is never left in a state where the gate fails on it.** The gate
switches in P3, after the count reaches zero in P2, and not before. Each P2
batch is its own commit and its own verification, so a regression bisects to one
linter rather than to "the golangci-lint change".

## Implementation Steps

### P1 — Add the runner, advisory only (D1, D2, D6)

Commit `.golangci.yml` naming the five linters explicitly rather than relying on
the default set, which moves between releases:

```yaml
version: "2"
linters:
  default: none
  enable: [errcheck, govet, ineffassign, staticcheck, unused]
```

Add a `make lint-new` target that runs it, leaving the existing `lint`
(golint) target in place. **No gate change, no CI change** — at this point the
tool is informational and `master` stays green under the old rule.

**Verification:** `make lint-new` runs and reports the 85 findings; `make
pre-add-check` still passes; `git commit` still works.

### P2 — Fix the 85, one linter per batch, cheapest and most valuable first

Order chosen so that each batch is independently reviewable and the risky one
comes last:

1. **`ineffassign` (1)** — trivial.
2. **`unused` (5)** — delete dead code. Confirm each is genuinely unreachable
   and not an exported API or a build-tagged sibling before deleting.
3. **`staticcheck` (29)** — `SA9003` (empty branch) is a real smell and gets
   fixed. The `QF1001`/`QF1002`/`ST1005` findings are refactor and style
   suggestions: fix the ones that read better, and for any that would reflow
   code for no gain, **disable that specific check in `.golangci.yml` with a
   written reason** rather than contorting the code. That is a scoped,
   recorded configuration decision, not a blanket exclusion — the distinction
   matters and the reason goes in the file.
4. **`errcheck` (50)** — the slow one, and deliberately last. Each site is a
   judgement: handle the error, or discard it explicitly as `_ =` **with a
   comment saying why it cannot matter**. A bare `_ =` with no reason is not an
   acceptable outcome; it is the same information loss as the unchecked call,
   only harder to grep for.

**Each batch:** its own commit, `make pre-add-check` on the touched files, and
`go test -race ./...` clean. The race suite matters most for the errcheck
batch, which by definition edits error handling.

**Verification:** after batch 4, `golangci-lint run ./...` reports **0**.

### P3 — Switch the gate (D3)

Only once P2 reports zero. In `scripts/go-precheck.sh`, replace the per-file
`golint` loop with `golangci-lint`. Two details that are easy to get wrong:

* **golangci-lint is package-scoped, not file-scoped.** The current gate runs
  `golint` per file "so the output names what to fix". golangci-lint takes
  paths but analyses whole packages; running it once per staged file would
  re-analyse the same package repeatedly and be slow. Run it once over the set
  of packages containing the staged files, and keep the output — it already
  names file, line and linter.
* **Absence must still fail loudly.** The script's `need` helper prints an
  install hint and returns 1; give `golangci-lint` the same treatment
  (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<pinned>`),
  because 0118 records a gate that silently could not run when `golint` was
  missing.

Update `Makefile`: `lint` becomes golangci-lint, `lint-new` is removed.

**Verification, and this is the gate's own "seen to fail" check:** introduce a
deliberate finding in a **scratch copy** of a file, confirm
`scripts/go-precheck.sh` exits non-zero and names it, then confirm it passes
without. Never dirty the working tree to do this.

### P4 — CI (D4)

Add a lint job to `ci.yml` running the pinned golangci-lint over `./...`, and
**delete the two `go vet ./...` steps** (`:108`, `:314`) — `govet` is in the
set, so keeping them runs the same analysis twice.

Pin the action or the binary version, per D6 and the precedent in
`.github/dependabot.yml`: an unpinned linter can turn the gate red on someone
else's release.

**Verification:** a branch carrying a known finding fails the job; the same
branch without it passes.

### P5 — Documentation

`AGENTS.md` "The pre-add rule (Go)" and its "What each check means here" table,
and `README.md`'s two references. State the pinned version and that `go vet` is
subsumed. Note in `AGENTS.md` that `revive` was measured and deferred, so the
next reader does not re-litigate it from scratch.

## Verification (whole plan)

```bash
golangci-lint run ./...        # 0 findings
make pre-add-check             # clean, using golangci-lint
go test -race ./...            # clean
```

plus a CI run green on all lanes with the new job.

### Acceptance criteria

1. `.golangci.yml` names the five linters explicitly; no reliance on the
   default set. (P1)
2. `golangci-lint run ./...` reports 0. (P2)
3. `scripts/go-precheck.sh` has been **observed to fail** on an injected
   finding and pass without it, against a scratch copy. (P3)
4. `make pre-add-check` and `go test -race ./...` are clean on `master` at
   every commit in P2–P4, not only at the end. (stability rule)
5. CI runs golangci-lint, `go vet ./...` is gone, and the version is pinned.
   (P4)
6. `AGENTS.md` and `README.md` describe the gate that actually runs, including
   the deferred `revive` decision. (P5)

## Rollout and Rollback

No runtime effect: nothing here ships in a binary. The blast radius is
developer and CI workflow only.

Rollback is per phase. P4 alone: drop the CI job, restore `go vet`. P3 alone:
restore the `golint` loop in `scripts/go-precheck.sh` — the P2 fixes are
independently valuable and do not need reverting. P1 is a config file and a
Makefile target.

The per-machine agent hooks need no rollback either way, because they call the
repository's script rather than carrying their own copy.

## Deferred

* **`revive`** (MADR D5): 50 findings — `unused-parameter` 36,
  `redefines-builtin-id` 9, `package-comments` 2, `empty-block` 2,
  `time-naming` 1. Measured 2026-09-02 so a future decision starts from data.
  Trigger: a wish for style enforcement beyond what `golint` gave, which it
  never gave here — `golint` reports 0 on this codebase today.
* **Dart/Flutter lint parity.** `flutter analyze` is already enforced; whether
  it should gain custom lint rules is a separate question.
* **`gofumpt`.** Rejected, not deferred. `AGENTS.md` gives the reason.

## Update (2026-11-18): the revised phases

Everything above is kept as written. Where this section differs from P1 to P5 above, this section is the plan to
execute. The owner has answered the open questions at the top (Q1 A, Q2 A, Q3 A, Q4 A, Q5 A), and the coordinator reads
the answers with the owner's "move it onto golangci-lint v2.15.0 now" as approving this plan as phased: see "Decided
(2026-11-18)". The phases below are written to those answers. The words "under Q3 B" and the like in this section
describe options the owner did not choose and are kept only so the sizing stays readable.

Provenance: drafted by a mainstream-tier agent at high effort, from `master` at `e7de8568`, read-only, from the
measurements in the MADR's update. The owner's rulings are those of 2026-11-18, relayed by the coordinator.

### Size and shape of the work

* 1671 findings in 289 files (211 production, 78 test) of 1038 Go files, with the fleet's `go-llmprovider-sdk`
  configuration, over linux, darwin, windows and every opt-in tag. About 140 are mechanical; about 380 (`goconst`) are
  mechanical but wide; the rest, about 1150, need judgement at each site, of which about 490 are explicit `_ =` discards
  that Q3 A leaves alone.
* Under the recommended answers, the work is about 1180 findings (the 493 explicit discards are left), in eight phases
  after the adoption phase; the file count falls only slightly from 289. Under Q3 B it is the whole 1671.
* **Packages and API.** Every Go package of the module is `internal/...`, `cmd/...`, `scripts/...` or `test/...`: there
  is no importable public API, so "exported" below means cross-package names inside the module and the wire protocol.
  The apps under `apps/mobile` are Dart and are not touched.

### Findings that touch exported names, behaviour or the wire

Listed separately, because a fix there can change what the daemon does. Each is a per-site decision, not a rote edit.

| Class | Sites | What can change |
| :--- | ---: | :--- |
| `errorlint` `%v` to `%w` in `fmt.Errorf("%w: %v", ErrX, err)` (for `ErrPersist`, `ErrMalformedJWS`, `ErrInvalidCandidate`, `ErrDaemonNotRunning`, `ErrExecutionOutcomeUnknown` and others) | 22 | The inner error becomes visible to `errors.Is` and `errors.As`, so a caller that branches on it now takes a different branch. The message text does not change. |
| `errorlint` `==`, type assertion, `switch` on errors (`io.EOF`, `bufio.ErrBufferFull`, `viper.ConfigFileNotFoundError`, `*exec.ExitError`, two `windows.ERROR_*` codes) | 8 in production | The same value is matched when wrapped; a wrapped error now matches where it did not. |
| `nilerr` (an error is swallowed and `nil` returned) | 6 | Each is deliberate or a bug. Either a reason is written (an owner-chosen `//nolint:nilerr`, as `providerauth/reconcile.go` already does) or the error is returned. |
| `gosec` G301, G302, G306 (modes `0o755`, `0o700`, `0o644`) | 22 | Tightening a mode changes what other users and services can read. Service-unit directories and supervisor files must stay readable. These are not rote. |
| `gosec` G118 (goroutines on `context.Background()`) | 6 | A goroutine's lifetime and cancellation. |
| `gosec` G115 (integer conversions) | 17 | A bounds check changes behaviour at the extremes (windows handle and PID values, a PTY size, a paging limit). |
| `gosec` G101 (names such as `credential_failed`, `provider.set_credential`) | 6 | Never rename: they are wire-protocol values. The finding is a false positive. |
| `errcheck` bare calls, if the error is now handled | up to 332 | A path that ignored a failure now returns or logs it. Under Q3 A the 257 `fmt.Fprint*` go through one helper. |
| `revive` `time-naming` | 1 | `agenterr.LongBackoffMin` is an exported variable; renaming it touches its callers. |
| `revive` `confusing-naming` | 35 | The rename goes on the unexported side (`systemRoots`, `resolve`, `close`, `emit`, `renderPlist` and the like), so no exported name changes. Where the exported side must change, it is an interface method of `acpagent`'s host or `provider` (`Emit`, `NoteSubagent`, `AwaitAnswers`, `SupportsCapability`) and the plan stops for the owner. |
| `unparam` (unused parameters, always-same arguments, never-used results) | 16 | Unexported signatures only, in `session`, `provider/codex`, `provider/kilo`, `providerauth`, `relay`, `cli/service`, `scripts`. |
| `gocognit` and `gocyclo` | 13 production, 1 | A refactor of `newPairCmd`, `newEnginesCmd`, `parseReset`, and `ws.(*Server).handleMessage`, the protocol dispatcher. |
| `goconst` | 351 | A new constant with the same string value changes nothing, provided the value is identical. |

### What else must change with it

Each is part of a phase below, and each phase carries its docs step.

| Where | What |
| :--- | :--- |
| `Makefile:615-616` | `lint` runs golangci-lint v2.15.0, once for each of linux, darwin and windows with `CGO_ENABLED=0`, and once more for linux with every opt-in build tag (the tag list is the one `scripts/vet_build_tags.py` already derives). `lint-new` from the 2026-09-02 plan is not created. |
| `Makefile:443` onward | `preflight` gains a `lint` step beside `go vet`, `vet-tags` and `staticcheck`. |
| `Makefile:658` and the `pre-add-check` comment | "gofmt, golint and govulncheck" becomes "gofmt, golangci-lint and govulncheck". |
| `scripts/go-precheck.sh:44-47, 64-78, 96` | The `need` hint, the per-file `golint` loop and the closing line change. golangci-lint analyses packages: the step runs once over the packages that hold the staged files, with the host `GOOS`. A windows-only file is therefore linted only by `make lint` and CI, which the script's header says. |
| `.github/workflows/ci.yml`, job `go` (`:42-186`) | A step installs the pinned binary (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`, as the fleet does) and runs `make lint`. `go vet` (`:133`, `:238`) and `vet-tags` (`:139`) stay. `go-native` needs no lint, the three GOOS being covered in `go`. |
| `AGENTS.md:19` and `:30` | "gofmt, golint and govulncheck" becomes "gofmt, golangci-lint and govulncheck"; the `golint` bullet is replaced. The plain-`gofmt` rule stays. |
| `README.md:1508`, `:1573`, `:1586`, `:1634` | The CI table row, `make lint # golint`, "gofmt + golint + govulncheck", and the pre-add rule line. |
| `docs/guides/standards/go/README.md:47-49` | "Do not substitute `gofumpt` or `golangci-lint`: neither is configured by this repository" becomes a statement that `gofmt`, golangci-lint v2.15.0 and `govulncheck` are the gate, with the gofumpt prohibition kept as it is. |
| `docs/guides/standards/go/testing.md:27, :38` | "`gofmt`, `golint`, and `govulncheck`" and the preflight list. |
| `docs/guides/standards/go/logging.md` | Unchanged (Q3 A). |
| `docs/guides/standards/go/core.md` | One sentence (Q4 A), in P2. |
| `0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md` and its PLAN | A dated addition: the golangci-lint standard is v2.15.0 from 2026-11-18, replacing v2.13.2 (MADR `:110`, `:486`; PLAN `:450`). The record is accepted and its plan in progress: it is annotated, not rewritten. |
| `.claude/settings.json`, `opencode.json`, `.gobble/settings.yaml` | `make lint` is already allowed. A new Makefile target (none is planned) would need an entry in each. |
| `~/.global-agent-hooks/pre-add-go.sh` (not in the repository) | It calls `scripts/go-precheck.sh`, so it follows the script. P1 verifies this by running it, as `AGENTS.md`'s probe does. |
| `docs/README.md` | Regenerated by `python3 scripts/check_records.py --write-index` if a record title changes; the 0131 rows do not change, because the index carries no status and the titles are kept. |
| `CHANGELOG.md` | An `Unreleased` "Changed" entry if 0248's changelog policy covers developer tooling; the executor reads it and decides. |

`golint`: removed under Q2 A and B; the Makefile, script, README, AGENTS and guides stop naming it. `staticcheck`: kept
under Q2 A with its Makefile block, CI step, `scripts/check_staticcheck_ignored.py` and 0170's pin unchanged;
removed under Q2 B with those, and a dated amendment to 0170 and to 0207's amendment H. The apps' records under
`apps/mobile/docs` that name `golint` are historical and are not touched.

### Conventions every phase follows

* **C1. Start state.** `git status --short`, `HEAD` and `origin/master` read first; the tree is clean or each
  difference is named and left alone. If a line this plan cites has moved, it is re-read before use.
* **C2. The gate for every phase**, each run as `<command> > "$T/<name>.out" 2>&1; echo "exit=$?"` and the output read
  after the status: `make check-records`, `make markdownlint-docs`, `make pre-add-check FILES="..."`, `make lint`
  (from P1), `make staticcheck`, `go vet ./...`, `make vet-tags`, and `go test -race ./...`. The last is a heavy run:
  `~/.agents/skills/available-dev-hosts/scripts/pick-host`, then `scripts/run-on-host` (with `--race`), or
  `scripts/run-heavy` on the session host, one at a time per host. A phase that touches a `_windows.go` file also runs
  `make ci-windows` on a Windows host (0145) or leaves that to the CI `go-native` Windows lane and says so. `make
  preflight` before the phase's last commit.
* **C3. Seen to fail.** A new gate step, and any change to `scripts/go-precheck.sh`, is run on a scratch copy against a
  planted finding, seen to fail with the finding named, and seen to pass without it. The execution record says what the
  failure looked like. The working tree is never dirtied for this.
* **C4. Each phase ends green.** `.golangci.yml` at the end of a phase enables exactly the linters, and exactly the
  `revive` rules, that report 0 in that phase on linux, darwin, windows and every tag. CI is green on every lane at the
  phase's last commit. A finding is never silenced by a baseline, a blanket exclusion or a `//nolint` the owner has not
  chosen; a site that cannot be fixed is reported to the owner and the phase waits.
* **C5. Docs step.** Every phase lists the docs it changes, and the phase is not done until they are changed in the same
  phase: the guide, `AGENTS.md`, the README, and this plan's execution record.
* **C6. Commits.** Agents stage by path and do not commit, push or tag. The coordinator commits with `git commit
  --no-edit` after its own gate, one commit per batch, and pushes under the standing approval after the disclosure guard
  exits 0. Tags and releases need the owner's word.
* **C7. Identifiers.** No hostname, account name, address or real-machine path in any committed file; placeholders only.
* **C8. Deviations and stops.** Anything that differs from this plan stops the phase and gets a dated entry here, and an
  amendment in the MADR where a decision or fact changes. An unexpected state, or the same step failing twice, ends the
  task with the state and its evidence.

### The phases, each priced by model class and effort

The prices follow the house form: the class and effort of the agent that does the phase.

#### P1. Adopt the runner, with the gate on a clean subset

* **Model class and effort:** mainstream, high. A script, a Makefile target and a CI step that three GOOS and a tag
  pass depend on; it needs the C3 experiment.
* **Findings fixed:** 4 (`package-comments`: `cmd/mcrelay`, `cmd/mcremote`, `internal/provider/codex`,
  `internal/provider/opencode`).
* **Configuration at the end:** the fleet file's structure (`version: "2"`, `run.tests: true`, `default: none`, the
  fleet's settings and exclusions, `issues` limits at 0) with only the linters that are at zero on `HEAD` enabled:
  `misspell`, `nakedret`, `unused`, and `revive` with only `exported`, `package-comments` and `var-naming` (re-measured
  on `HEAD`; the 4 `package-comments` findings are fixed in this phase). Every other fleet linter is listed, disabled,
  each with a one-line comment naming the phase that enables it, so the file's distance from the fleet file is
  visible. `errcheck.check-blank: false` is set now, with its comment, although `errcheck` is enabled in P5: it is the
  one local difference the owner chose (Q3 A), recorded with its reason, which is that `logging.md` allows an explicit,
  reasoned discard. No path exclusion beyond the fleet file's own. The formatters are `gofmt` and `goimports` as in
  the fleet (Q4 A); the 3 files `goimports` flags are fixed in P2, so the `goimports` formatter is listed disabled with
  its phase until then.
* **Changes:** `.golangci.yml` (new); `Makefile` (`lint` with a `GOLANGCI_LINT` variable, `preflight`, the comment);
  `scripts/go-precheck.sh`; `ci.yml` (job `go`: `go install ...@v2.15.0`, then `make lint`); `golint` removed from the
  script, Makefile, README, AGENTS and guides; `docs/guides/standards/go/README.md:47-49`, only the golangci-lint half
  of the sentence (the gofumpt prohibition stays); the dated note on 0169 and its PLAN, drafted in this phase; a
  version-agreement check for the pin, if the repository has one (the executor finds out and says "none" if not).
  `staticcheck`, `go vet`, `vet-tags`, `gofmt` and the vulnerability check are untouched (Q2 A).
* **Test, first:** in a scratch copy, a planted finding of each enabled linter fails the new `make lint` (a misspelling in
  a comment, an unused function, an exported name without a comment, a package without a package comment, a badly named
  variable, a naked return in a long function), the clean tree passes, and a planted finding of a not-yet-enabled
  linter passes, so the phasing is real; the pre-add script rejects a staged file with a planted finding;
  `~/.global-agent-hooks/pre-add-go.sh` runs the script; CI green on `go`, `go-native` (three lanes) and the rest.
* **Rollback:** revert the commit; `golint` returns to the script and Makefile.

#### P2. Mechanical classes

* **Model class and effort:** mainstream, medium. Rote edits, each checked by the tool.
* **Findings fixed:** about 100 in about 80 files: `ineffassign` 1, `whitespace` 1, `unconvert` 7, `nolintlint` 13,
  `gocritic` 32, `govet` 3, `makezero` 1, `revive` `redefines-builtin-id` 36, `time-naming` 1, `if-return` 1,
  `empty-block` 1, and the `goimports` formatter's 3 (Q4 A). `nolintlint` is enabled last in the phase, after the
  others, because removing a stale directive is only provable by a run.
* **Configuration at the end:** the P1 file plus those linters, and the `revive` rules `redefines-builtin-id`,
  `time-naming`, `if-return`, `empty-block`.
* **Behaviour:** none intended; `agenterr.LongBackoffMin`'s callers are renamed together. The race suite runs.
* **Docs step:** `core.md`'s sentence "Let `gofmt` decide layout. Do not introduce a competing formatter rule" is amended
  to say `gofmt` plus `goimports` import ordering (Q4 A); this plan's record.

#### P3. `staticcheck` and the `SA1019` directives

* **Model class and effort:** mainstream, medium.
* **Findings fixed:** 51: the 16 `QF` and `SA9003` suggestions, and the 35 `SA1019` per Q5 A: each existing,
  already-justified `//lint:ignore SA1019` gets a paired `//nolint:staticcheck`, and each `//lint:file-ignore` file a
  file-level `//nolint:staticcheck`. This is the one `//nolint` the owner has chosen, and it applies to those paired
  directives only.
* **Configuration at the end:** `staticcheck` enabled.
* **Also:** `scripts/check_staticcheck_ignored.py` is extended to count the `//nolint:staticcheck` lines and to require
  the 0207 amendment H reason text on them, so the number cannot quietly grow (Q5 A); seen failing on a planted
  `//nolint:staticcheck` without the reason and on a planted extra one.
* **Docs step:** 0207's amendment H gets a dated note of the added directive form.

#### P4. `goconst`

* **Model class and effort:** mainstream, medium.
* **Findings fixed:** 379 in 113 production files: 351 new constants and 28 uses of an existing one. Wire values keep
  their text exactly; each new constant is placed where its package already keeps its sibling constants.
* **Verification beyond the gate:** the protocol golden tests and the whole race suite, because a constant is a string.
* **Configuration at the end:** `goconst` enabled.

#### P5. `errcheck`

* **Model class and effort:** mainstream, high. Each site is a decision about a path that has been silent.
* **Findings fixed:** under Q3 A, which the owner chose, about 332 bare calls in about 47 files (257 `fmt.Fprint*`
  through one helper, 44 `Close`, the rest handled or discarded with a stated reason); the 493 explicit discards stay.
  A site where handling the error is a behaviour change stops the phase for the owner.
* **Behaviour:** a handled error changes a path. Each changed path has a test, or the record says why no test is
  possible.
* **Configuration at the end:** `errcheck` enabled, with the fleet's settings and the recorded local difference
  `check-blank: false` (Q3 A), already in the file from P1.
* **Docs step:** `logging.md` is unchanged (Q3 A); the helper is named in the execution record.

#### P6. `errorlint` and `nilerr`

* **Model class and effort:** mainstream, high. Per-site analysis of who branches on the error.
* **Findings fixed:** 54 in 31 files (27 production errorlint, 21 in tests, 6 nilerr).
* **Behaviour:** the table above. Each `%v` to `%w` site is followed by a search for its sentinel's callers, and a test
  asserts the chain where a caller branches. A `nilerr` site is fixed or reported to the owner.

#### P7. `gosec`

* **Model class and effort:** strongest, high for the per-site verdict (a REPORT in `docs/reports` with a verdict for
  each of the 101: fix, false positive with its reason, or owner decision); then mainstream, high to apply it. The
  modes and goroutine contexts are security-relevant.
* **Findings fixed:** 101 in 49 files, apart from those the report sends to the owner. The per-site REPORT is put to
  the owner before any of these fixes are built, and each verdict that is not clear-cut comes with three options.
* **Configuration at the end:** `gosec` with `excludes: [G104]`, as the fleet has it.
* **Amended 2026-11-18 (Deviation 1 and 2 at the end of this file):** the 13 `//nolint:gosec` directives are removed in this
  phase, as `nolintlint` reports them once `gosec` is on: eight in test files (permanently unused: the fleet excludes tests
  from `gosec`) and five in production code, each removal proven by a run. Should `gosec` report a finding on one of the five
  lines once it is on, the finding goes to the per-site REPORT instead, and the directive stays until the owner chooses.

#### P8. `bodyclose`, `unparam`, `revive` `confusing-naming`, `gocognit`, `gocyclo`

* **Model class and effort:** mainstream, high. The refactors are of cobra command constructors and one protocol
  dispatcher.
* **Findings fixed:** `bodyclose` 72 (6 production: confirm that `websocket.Dial` owns the body, then close or document
  per the library's guidance; 66 in tests), `unparam` 16, `confusing-naming` 35, `gocognit` 31 and `gocyclo` 1. A
  function that is better left as it is goes to the owner, never to a `//nolint`.
* **Configuration at the end:** the whole fleet file apart from the Q3 to Q5 differences.

#### P9. Close-out

* **Model class and effort:** lightweight.
* **Does:** diffs `.golangci.yml` against the fleet reference file and records every difference with its reason;
  repeats the full measurement of the MADR update on a fresh archive (0 on every pass); `make preflight`; sets this plan
  to `complete` and the MADR's Confirmation to met; adds the dated execution record; removes the "enabled so far"
  comments from the file.

### Verification (whole plan, revised)

```bash
make lint                      # 0 on linux, darwin, windows, and linux with every opt-in tag
make staticcheck               # unchanged under Q2 A
go vet ./... && make vet-tags
make pre-add-check             # gofmt, golangci-lint, govulncheck, clean
go test -race ./...            # via pick-host and run-on-host
make preflight
```

plus a CI run green on `go` and on the `go-native` lanes at every phase's last commit.

### Rollout and rollback (revised)

No runtime effect for P1 to P4 apart from the CI and developer workflow. P5 to P8 change code paths and ship in the next
release; each is its own commit set, reverted independently. Reverting P1 alone restores `golint` in the script and
Makefile and removes the CI step; later phases' fixes are independently valuable and are not reverted with it.

### What this update does not do

It does not change source, tests, CI, the Makefile or a guide. It does not answer the open questions. It does not
adopt `gofumpt`, `depguard`, `forbidigo`, `sloglint` or the nine "ext" linters (`perfsprint` 339, `contextcheck` 130,
`noctx` 97, `modernize` 31, and five more): they are not in the configuration the owner named, and the 616 findings
they add would need a decision of their own.

## Execution record

### P1. Adopt the runner, with the gate on a clean subset, 2026-11-18

Run by a mainstream-tier agent at high effort, on the coordinator's brief, from `master` at `e7de8568` (clean at the
start). The changes are in the working tree, unstaged, for the coordinator to commit after its own gate. The owner's
answers of 2026-11-18 (Q1 A, Q2 A, Q3 A, Q4 A, Q5 A) are those in "Decided (2026-11-18)".

**Done.**

* `.golangci.yml` (new): the fleet's structure; enabled `misspell`, `nakedret`, `unused` and `revive` with `exported`,
  `package-comments`, `var-naming`; the `gofmt` formatter; every other fleet linter listed under `disable:` with the
  phase that enables it; the other 22 fleet `revive` rules listed as comments with their phase; `errcheck.check-blank:
  false` with its reason (Q3 A); the fleet's own exclusions and nothing else. `goimports` is a commented formatter line
  for P2 (Q4 A).
* `scripts/lint.py` (new): the one implementation of the lint rule. It holds the pin (`PIN = "2.15.0"`), refuses any other
  version (exit 2), and runs golangci-lint once per GOOS of the host, linux, darwin and windows (`CGO_ENABLED=0`) and once
  for linux with every opt-in build tag (the tags `scripts/vet_build_tags.py` finds). With package directories it lints
  just those. `python3 scripts/lint.py --pin` prints the version.
* `Makefile`: `lint` runs `scripts/lint.py` (with a `GOLANGCI_LINT` variable); `preflight` gains a `golangci-lint` step
  after `staticcheck`; the `pre-add-check` comment. `golint` is gone.
* `scripts/go-precheck.sh`: step 2 runs `scripts/lint.py` over the packages that hold the staged files (or `./...` with no
  arguments); the `need` hint and the closing line say golangci-lint.
* `.github/workflows/ci.yml`, job `go`: a step installs `...@v$(python3 scripts/lint.py --pin)` and runs `make lint`. The
  pin therefore has one source. `staticcheck`, `go vet`, `vet-tags`, `gofmt` and the vulnerability check are unchanged.
* The 4 `package-comments` findings: package comments on `cmd/mcrelay`, `cmd/mcremote`, `internal/provider/codex`,
  `internal/provider/opencode`.
* Docs: `AGENTS.md` (the rule and the bullet), `README.md` (the CI table row, `make lint`, the pre-add line, the
  code-standards line), `docs/guides/standards/go/README.md` (only the golangci-lint half of the sentence; the gofumpt
  prohibition is kept; a `make lint` row), `docs/guides/standards/go/testing.md` (two lines), and dated additions to
  0169's MADR and PLAN naming v2.15.0.

**Re-measured on `HEAD` with the new configuration and `scripts/lint.py`, before the fix:** exactly 4 findings, all
`package-comments`, on each of the four passes; every other enabled linter 0. After the fix: 0 on all four.

**Implementation note.** `scripts/lint.py` is a file this plan's P1 text did not name. The plan said the Makefile, the
pre-add script and CI would each run golangci-lint; one script they all call is how the GOOS and tag passes and the pin
stay identical in the three places (the same reason `go-precheck.sh` is the single implementation of the pre-add rule).
It is not a change of decision; the coordinator may ask for it inline instead.

**Seen to fail, on scratch copies** (`HEAD` plus the working-tree changes, never the working tree). Each planted finding
failed `scripts/lint.py` with exit 1 naming its linter:

| Planted | Result |
| :--- | :--- |
| a misspelling in a comment | exit 1, `misspell` |
| an unused unexported function | exit 1, `unused` |
| an exported function without a comment | exit 1, `revive` `exported` |
| a package without a package comment | exit 1, `revive` `package-comments` |
| a naked return in a 42-line function | exit 1, `nakedret` |
| a variable named with underscores | exit 1, `revive` `var-naming` |
| an exported function without a comment in a `_windows.go` file | exit 1 (a host-only run does not see it) |
| the same in a file behind `//go:build live_grok` | exit 1 (only the tag pass sees it) |
| an unchecked `os.Remove` (errcheck, not yet enabled) | exit 0; and exit 1 with errcheck switched on in that copy |
| an ineffectual assignment (ineffassign, not yet enabled) | exit 0; and exit 1 with ineffassign switched on in that copy |
| a golangci-lint of the wrong version (a stub reporting v2.14.0) | exit 2, "pins v2.15.0" |
| no golangci-lint binary | exit 2, "not found" |
| `scripts/go-precheck.sh` on a staged file with an uncommented exported function | exit 1, `golangci-lint:` and the finding |
| `scripts/go-precheck.sh` on a documented file | exit 0, "clean (gofmt, golangci-lint, govulncheck)" |
| `scripts/go-precheck.sh` on an unformatted file | exit 1, `gofmt:` (the gofmt step is unchanged) |

The clean tree passes `make lint` (exit 0, four passes). The not-yet-enabled cases passing is the proof that the phasing is
real: the same findings fail the moment their linter is enabled.

**Gates** (start, end and exit status recorded before any filter; the scratch log holds the full output):

* `make pre-add-check` on the four changed Go files, `GO_PRECHECK_SKIP_VULN=1`: exit 0.
* `make lint`: exit 0. `make staticcheck`: exit 0. `go vet ./...`: exit 0. `make vet-tags`: exit 0.
* `go mod tidy -diff`: exit 0. `actionlint` on `ci.yml`: exit 0 (it first reported shellcheck SC2155 on this phase's own
  step, fixed by assigning `gobin` apart from the `export`). `make check-records`: exit 0.
* `make markdownlint-docs` (offline, from the npx cache): exit 0.
* `go test -race ./...` on a clean copy of the tree (`git archive HEAD` plus the changes), through `run-on-host --race`:
  exit 0, 49 packages `ok`. The host `pick-host` chose was the Windows laptop, as WSL is excluded for `-race`; the suite
  has not been run under linux or darwin for this phase.
* `make preflight` through `run-heavy`: **not run.** The ticket waited 16 minutes in the shared queue (position 5, behind another session's long runs) and was cancelled by this agent before it started, so nothing ran under it. Its steps were run as the separate gates in this list, with these exceptions: `make verify-units` (this host has no `systemd-analyze`; the CI `go` job runs it), the release build (replaced by `go build` of both commands, exit 0, and `go test -race` above), the pub advisories check (network), and the Flutter and Dart steps (the phase touches no Dart). Also run and exit 0: `scripts/install-binary_test.sh` and `scripts/assert-flutter-pin.sh`. `make preflight` itself is left for the coordinator to run through `run-heavy` when the queue clears.

**Not done, and why.** govulncheck was skipped (`GO_PRECHECK_SKIP_VULN=1`), because it needs the vulnerability database
beyond the module proxy. The version-agreement check the brief asked about does not exist in this repository: `scripts/` has
no check of tool pins (`check_libs.json` and `make check-libs` cover library module pins and need the network). The pin's one
source is `scripts/lint.py`, and `make lint` refuses a binary that differs from it. `make ci-windows` was not run: no
`_windows.go` file changed.

### P1, close-out, 2026-11-18

P1 passed the coordinator's own gate (`make lint`; `make check-records`; `make pre-add-check` with the vulnerability check,
over all 1038 Go files, clean; `make preflight` through `run-heavy`, passed) and is committed and pushed: the records as
`ef1364de`, then the change as `a6300dab`, on `master`. CI on `a6300dab` is green: CI, Docs and the flake ledger.

The two departures from the plan's letter that P1 recorded (the one `scripts/lint.py` entry point, and the pin
single-sourced through `--pin`) are accepted by the coordinator as within the phase's purpose. A departure from a plan's
letter is reported before it is built, not after, from P2 on.

Correction of a count above, by annotation: P1's record says the other 22 fleet `revive` rules are listed as comments. The
fleet's rules number 26; with `exported`, `package-comments` and `var-naming` on, the other 23 were listed, `confusing-naming`
among them. P2 turns 22 of the 23 on.

### P2. Mechanical classes, 2026-11-18

Run by a mainstream-tier agent at high effort, on the coordinator's brief, from `master` at `a6300dab` (clean at the start).
The changes are in the working tree, unstaged, for the coordinator to commit after its own gate. No stop was needed: no
finding's mechanical fix changed behaviour, a wire value, an exported name's meaning or a public constant.

**Re-measured first**, on `HEAD`, with the P2 linters, 22 of the 23 listed `revive` rules (all but `confusing-naming`, P8)
and the `goimports` formatter switched on in a scratch copy, limits raised, `CGO_ENABLED=0`:

| Pass | Findings |
| :--- | ---: |
| darwin | 87 |
| linux | 87 |
| windows | 85 |
| linux with every opt-in tag | 88 |
| **union of the four, the work list** | **89** |

By linter: `revive` 39 (`redefines-builtin-id` 36, `time-naming` 1, `empty-block` 1, `if-return` 1), `gocritic` 34
(`importShadow` 8, `httpNoBody` 8, `nestingReduce` 3, `preferFilepathJoin` 3, `emptyStringTest` 2, and one each of
`regexpSimplify`, `filepathJoin`, `rangeExprCopy`, `deferInLoop`, `appendAssign`, `preferStringWriter`,
`tooManyResultsChecker`, `typeDefFirst`, `assignOp`, `unlambda`), `unconvert` 7, `goimports` 3, `govet` 3 (the `inline`
analyzer), `ineffassign` 1, `makezero` 1, `whitespace` 1, `nolintlint` 0 (see below). **After: 0 on all four passes**, and
`make lint` with the new `.golangci.yml` exits 0. The full list and the after-run are kept in the scratch folder.

**Each class, its fix, and why it cannot change behaviour.**

* `redefines-builtin-id` (36 findings, 32 declarations: `max`, `min`, `real`, `cap`). Every one is an unexported local, a
  parameter, or an unexported package-level constant. Each declaration was renamed with `gopls rename`, which renames its
  references and nothing else (`maxLen`, `maxBytes`, `limit`, `minSel`, `maxSel`, `highest`, `ceiling`, `resolved`).
  None is a struct field, a tag, a JSON key or a flag name; a struct key that spelled `Cap: cap` became `Cap: limit`, its
  key unchanged. No string literal changed. The doc comments that named the parameter by its old name were changed in step
  (13 comment lines). `output.Clip` and `output.Line` are exported, but only their parameter names changed, which no caller
  can see.
* `time-naming`: `agenterr.LongBackoffMin` is now `agenterr.LongBackoffThreshold`, same type and value. Its users are all in
  this module: `agenterr.go` itself (two), `provider/kilo/lifecycle.go` and `provider/opencode/acp_extensions.go`, and
  `internal/` allows no importer outside it. `0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md`
  names the old identifier in prose; it is a record and is left as written.
* `empty-block`: `if promote(); err == nil { /* fall through */ } else { cleanup }` became `if promote(); err != nil { cleanup }`
  with the comment kept. `if-return`: `if err := v(); err != nil { return err }; return nil` became `return v()`; `validate`
  returns `error`. `ineffassign`: `chain := "-"` became `var chain string`; the next statement assigns it on every path.
  `makezero`: `append(chunk, tail...)` became `slices.Concat(chunk, tail)`, the same bytes in a new slice.
  `whitespace`: one blank line deleted (a live-tagged test). `unconvert`: seven conversions of a value to its own type
  removed (four `json.RawMessage(request.ID)`-style in one test file, one `string(res.UnitBody)`, one `int64(...)` on an
  `int64`, and `json.RawMessage(value)` on a `json.RawMessage` in `codex/conn.go`).
* `gocritic`, by checker. Safe and local: `importShadow` (8 renames by `gopls`: `providerID`, `line`, `flags`, `tlsCfg`,
  `authn`), `httpNoBody` (8 GET requests, `nil` to `http.NoBody`: neither sends a body), `emptyStringTest` (2: `len(s) > 0`
  on a string to `s != ""`), `assignOp` (`name = name + x` to `name += x`), `preferStringWriter` (`f.Write([]byte(s))` on an
  `*os.File` to `f.WriteString(s)`), `unlambda` (`var execCommand = procutil.Command`, the same signature),
  `regexpSimplify` (`[^0-9]` to `\D`, identical in RE2), `filepathJoin` (`fmt.Sprintf("/run/user/%d", uid)` in a unix-only
  file), `rangeExprCopy` (an index loop that only reads, under the lock). Judged per site: `preferFilepathJoin` (3: the two in
  `codex/logout.go` join a `os.MkdirTemp` result, already clean and absolute; the one in `providerauth/transaction.go` joins
  `txnHome`, already a `filepath.Join`, with `CandidateName()`, which is `"auth.json"` in both adapters, so the path is
  byte for byte the same); `nestingReduce` (3: the `if` is inverted to `continue`, and nothing followed the `if` in any of
  the three loop bodies, so the same statements run in the same order); `appendAssign` (`env = append(env, ...)`, then
  `cmd.Env = env`, so the append no longer assigns elsewhere); `deferInLoop` (`return func() error { defer a.restart(...);
  return a.adopt(...) }()`, which keeps the defer's timing exactly, even on a panic, and takes it out of the loop);
  `tooManyResultsChecker` (`sessionRelaunchInfo` returns a `relaunchInfo` struct and `ok`; the three callers unpack it into
  the same locals); `typeDefFirst` (`type Manager struct` moved, unchanged, above its first method).
* `govet` `inline` (3, in one test file): the `//go:fix inline` helper `ptr` wraps the builtin `new`, and the analyzer cannot
  inline a generic call. The three calls are now `new(...)`, and the helper, which nothing else used, is removed.
* `goimports` (3): `diagnostics.go`, `managed_daemon.go`, `tailnet.go`: the module import moved into its own group by
  `goimports`. `core.md`'s sentence is amended as the owner ruled (Q4 A): `gofmt` decides layout and `goimports` orders the
  imports; `gofumpt` stays refused.
* `nolintlint`: **0 findings now, and no directive removed.** With `gosec` off, `nolintlint` cannot see a `//nolint:gosec` as
  unused, so it reports none. The 13 directives it reported in the full-fleet measurement are all `//nolint:gosec`: eight in
  test files (`certs`, `daemon`, `provider/codex`, `provider/grok`, `providerauth`, `relay`, and two in `updateclient`), which
  the fleet's own exclusion keeps out of `gosec` for good, and five in production code (`provider/codex/adapter.go`,
  `provider/codex/logout.go` twice, `provider/codex/store_reality.go`, `providerauth/cli.go`), where `gosec` reports nothing on
  a call to `procutil.Command`. `gosec` is enabled in P7, so all 13 wait for P7, where `nolintlint` will name each and the run
  will prove the removal. The three `//nolint:nilerr` directives were not reported unused in that measurement and stay.

**Configuration.** `.golangci.yml` now enables `gocritic`, `govet`, `ineffassign`, `makezero`, `nolintlint`, `unconvert`,
`whitespace` and the `goimports` formatter, and lists 22 `revive` rules as on. Still disabled, each with its phase:
`goconst` P4; `staticcheck` P3; `errcheck` P5; `errorlint` and `nilerr` P6; `gosec` P7; `bodyclose`, `gocognit`, `gocyclo`,
`unparam` P8; and the `revive` rule `confusing-naming` P8. `scripts/lint.py` is unchanged.

**Seen to fail, on scratch copies** (`HEAD` plus the changes): each planted finding failed `scripts/lint.py`, exit 1, naming
its rule: `gocritic` `httpNoBody`; `govet` `printf`; `ineffassign`; `makezero`; `nolintlint` (an unused `//nolint:misspell`);
`unconvert`; `whitespace`; `revive` `redefines-builtin-id`, `time-naming`, `if-return`, `empty-block`, `error-strings`,
`indent-error-flow`; and the `goimports` formatter (a module import inside the standard-library group, which `gofmt` accepts).
A planted `errcheck`, `gosec`, `goconst`, `staticcheck` (a De Morgan form) and `confusing-naming` finding each passed, exit
0, and the clean tree passed. Three plants were corrected first because they tripped a different, enabled rule (an error
variable not named `errFoo`; an unsorted import block that `gofmt` itself refuses; `errors.New(fmt.Sprintf(...))`, which
`revive` `errorf` refuses).

**Tests.** The suite is unchanged, and no assertion changed. Five test files were touched, for expressions only:
`internal/cli/service/setup_test.go` (a redundant `string(...)`), `internal/provider/codex/provider_test.go` (four redundant
`json.RawMessage(...)`), `internal/relayhost/bridge_default_test.go` (a redundant `int64(...)`),
`internal/provider/acpagent/elicitation_test.go` (three `ptr(x)` calls to `new(x)`, and the helper removed) and
`internal/provider/codex/live_device_auth_test.go` (a blank line). `go test -race ./...`, on a clean copy of the tree through
`run-on-host --race`, on the Windows laptop `pick-host` chose: exit 0, 50 packages `ok`. `make ci-windows` on
that Windows host: exit 0 ("ALL SELECTED CHECKS PASSED"), because `sockpath_windows.go` changed. The macOS race run is
the one inside `make preflight` below.

**Gates** (start, end, exit status, each recorded before any filter):

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `pre-add-check` | 12:49:43 | 13:00:52 | 0 |
| `lint` | 12:43:02 | 12:43:25 | 0 |
| `staticcheck` | 12:49:10 | 12:49:37 | 0 |
| `vet` | 12:48:31 | 12:48:34 | 0 |
| `vet-tags` | 12:48:34 | 12:49:04 | 0 |
| `tidy` | 12:49:04 | 12:49:05 | 0 |
| `check-records` | 12:49:05 | 12:49:08 | 0 |
| `markdownlint-docs` | 12:49:08 | 12:49:10 | 0 |
| `actionlint` | 12:49:37 | 12:49:37 | 0 |
| `gofmt-goimports` | 12:49:37 | 12:49:37 | 0 |
| `make preflight` through `run-heavy` | 12:57:52 | 13:04:09 | 0 |

`make pre-add-check` ran with the vulnerability check, over the 55 changed Go files, through `run-heavy`.

**Not done.** `confusing-naming` and the other P8 and later linters are not enabled; the 13 `//nolint:gosec` directives wait for
P7; nothing outside this phase's list was touched.

### P2, review follow-up, 2026-11-18

An independent review of P2's judged sites (a scratch copy byte-identical to the tree) found every one behaviour-preserving
except one class, and two record gaps. It is recorded here; nothing was committed yet.

**`preferFilepathJoin` in `internal/provider/codex/logout.go`, two sites: a behaviour change in an edge case.** The earlier
note that an `os.MkdirTemp` result is "already clean" was wrong. `os.MkdirTemp` joins `$TMPDIR` and the new name by text and
does not clean the result. With `TMPDIR=<base>/link/..` (`link` a symlink to a deeper directory), the old concatenation named
the directory that was created, because the filesystem follows `link`; `filepath.Join` folds `link/..` by text and names another
directory. `copyLiveForRevoke` then failed (`open ...: no such file or directory`) and `verifyLogoutOnClone` would have
looked for a file where codex does not work.

* **The test, written first.** `TestIsolatedHomesAreFoundWhereTheyAreCreated` (`internal/provider/codex/logout_test.go`; a new
  test, no existing line changed) builds the fixtures from the clean temporary directory, then sets `TMPDIR` (and `TMP` and
  `TEMP`) to `<base>/link/..`, with `link` a symlink to `<base>/a/b`, and runs `CoordinatedLogout` in both modes: the revoking
  one (`copyLiveForRevoke`) and the non-revoking one (`verifyLogoutOnClone`). Its fake `codex` journals whether
  `$CODEX_HOME/auth.json` exists, which the child resolves through the filesystem, and the test asserts `found:` and no
  `missing:`. It skips where symlinks need a privilege that is not held (`testexec.SkipIfNoSymlink`, which fails instead under
  `MC_REQUIRE_SYMLINK`, as CI sets) and where there is no POSIX shell for the fixture.
  * Red on the tree as it stood, both subtests: `logout under an uncleaned temporary directory: open
    <tmp>/mcremote-codex-revoke-N/auth.json: no such file or directory`, and the same for `mcremote-codex-logout-N`.
  * Green on a scratch copy of `a6300dab` (the original concatenation): both subtests pass, so the test is a guard that the
    original behaviour holds.
  * Green on the fixed tree, under `-race`.
* **The fix.** One helper in `logout.go`, `mkdirTempResolved(pattern)`: `os.MkdirTemp("", pattern)`, then `filepath.EvalSymlinks`
  of the result, removing the directory again if it cannot be resolved. Both isolated homes are created through it, so a later
  `filepath.Join` over the path is exact. No `//nolint` and no concatenation.
* **Why the resolved path is safe at every use.** The variable is read in `copyLiveForRevoke` (`os.Chmod`, `os.WriteFile`,
  `os.RemoveAll` in `fail`, the return value), by `CoordinatedLogout` (`defer os.RemoveAll(tmp)`, `p.runLogoutIn(ctx, tmp)`), in
  `verifyLogoutOnClone` (`os.Chmod`, `os.WriteFile`, `os.Stat`, `os.RemoveAll`, `p.runLogoutIn`), and in `runLogoutIn`, which
  passes it as `CODEX_HOME=<home>` to the child. Every one hands the path to the filesystem or to a child that does, and the
  resolved path names the same directory (`os.SameFile` confirmed). None compares it with another path, stores it, or logs it;
  the only place it can surface as text is the child's own output in an error string, where a resolved form is no less true.
  On macOS the text differs: `os.MkdirTemp` returns `/var/folders/.../T/probe-N` and `EvalSymlinks` returns
  `/private/var/folders/.../T/probe-N`, the same directory. On Windows, run on the Windows host, `EvalSymlinks` returned the
  path unchanged and `os.SameFile` was true. `logout.go` has no build constraint and builds for every GOOS. On Windows the
  uncleaned shape cannot arise as it does on unix (the Win32 path APIs fold `..` by text before the filesystem sees it), and
  the new test's subtests skip there for want of a shell the fixture can run.
* **The third site, `providerauth/transaction.go`.** Preserved: `txnHome` is already a `filepath.Join`, and `CandidateName()` is
  the constant `"auth.json"` in the codex adapter, the grok adapter and the test fake. The doc comment of the interface method
  `providerauth.Adapter.CandidateName` now says it returns a bare file name, with no path separator and no `.` or `..` element,
  because the coordinator joins it onto the home's path. No code change.

**Gates after the follow-up** (start, end, exit status, each recorded before any filter). The measurement with this tree's own
`.golangci.yml`, JSON counted, limits raised: darwin 0, linux 0, windows 0, linux with every opt-in tag 0.

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `make pre-add-check` on the 57 changed Go files, with the vulnerability check, through `run-heavy` | 13:30:31 | 13:41:17 | 0 |
| `lint` | 13:28:50 | 13:29:17 | 0 |
| `vet` | 13:29:17 | 13:29:20 | 0 |
| `vet-tags` | 13:29:20 | 13:29:46 | 0 |
| `tidy` | 13:29:46 | 13:29:47 | 0 |
| `staticcheck` | 13:29:47 | 13:30:10 | 0 |
| gofmt over `cmd`, `internal`, `scripts`, `test` | 13:30:10 | 13:30:10 | 0 |
| `go test -race` on `TestIsolatedHomes` and the logout tests, `codex` and `providerauth` | 13:30:10 | 13:30:18 | 0 |
| `check-records` | 13:35:28 | 13:35:31 | 0 |
| `markdownlint-docs` | 13:35:26 | 13:35:28 | 0 |

### Deviations

#### Deviation 1, 2026-11-18: P2 switched on 22 `revive` rules, not the four the plan names

* **What the plan says.** P2's text enables the `revive` rules `redefines-builtin-id`, `time-naming`, `if-return` and
  `empty-block`.
* **What was done.** `.golangci.yml` enables 22 rules: those four, and 18 more: `atomic`, `blank-imports`,
  `constant-logical-expr`, `context-as-argument`, `context-keys-type`, `dot-imports`, `error-naming`, `error-return`,
  `error-strings`, `errorf`, `increment-decrement`, `indent-error-flow`, `range`, `receiver-naming`, `superfluous-else`,
  `unexported-return`, `unreachable-code`, `var-declaration`.
* **Why it is within the phase's purpose.** The owner's Q1 A ruling is that each phase turns on only linters already at zero.
  These 18 were at zero, P1's configuration had left them commented with the label "P2", and the fleet file carries all of
  them. Leaving them off would have kept zero-finding rules out of the gate until a later phase for no cost saved, and no
  code was changed for them. Nothing is suppressed. It should have been reported before it was built, not after: the
  coordinator's rule for the phases that follow.
* **The measurement, repeated for this entry.** On a scratch copy of `HEAD` (`a6300dab`), with P1's `.golangci.yml` and only
  those 18 rules uncommented, `golangci-lint` v2.15.0, limits raised, `CGO_ENABLED=0`: darwin 0 (exit 0), linux 0 (exit 0),
  windows 0 (exit 0), linux with every opt-in tag 0 (exit 0): 0 findings in all. Because a zero is meaningful only if the
  rule is on, the same configuration on a copy with one planted violation of each of the 18 in one file reported each rule
  by name (`atomic`, `blank-imports`, ..., `var-declaration`: all 18 fired).

#### Deviation 2, 2026-11-18: the 13 `nolintlint` findings are not fixed in P2; they move to P7

* **What the plan says.** P2's finding list counted `nolintlint` 13 among the mechanical classes ("delete a directive that
  suppresses nothing").
* **What was found.** All 13 are `//nolint:gosec`. With `gosec` off, `nolintlint` cannot see a `//nolint:gosec` as unused, so
  it reports 0 in P2 and there is nothing for the phase to fix. Eight are in test files, which the fleet's own exclusion keeps
  out of `gosec` for good; five are in production code (`provider/codex/adapter.go`, `provider/codex/logout.go` twice,
  `provider/codex/store_reality.go`, `providerauth/cli.go`), where `gosec` v2.15.0 reports nothing on a call to
  `procutil.Command`. Removing them in P2 would be unprovable by a run, which is the reason the plan enables `nolintlint`
  last.
* **Decision.** P2 enables `nolintlint` at 0 and removes none. P7 carries them: see the amendment under P7's step. P2's count
  is therefore 89 findings, not about 100.

### P2, close-out, 2026-11-18

P2 passed the coordinator's gate on the corrected tree (`make lint`, the record check, `make pre-add-check` with the vulnerability
check, `make preflight`) and is committed and pushed: the record as `29bb92e0`, then the change as `08e9e7ef`, on `master`. CI on
`08e9e7ef` is green: CI, Docs and the flake ledger. The independent review's verdict on P2: every judged site is behaviour-preserving
except the two `logout.go` path joins, which were then fixed, with a test.

Correction of the P2 entry above, by annotation: it says the `go test -race ./...` run on a clean copy went to the Windows laptop. The
`run-on-host` output of that run names a `linux/amd64` target. The suite passed on a Linux host (50 packages `ok`), and the Windows
host ran `make ci-windows` and, after the follow-up, the logout tests.

### P3. `staticcheck` inside golangci-lint, 2026-11-18

Run by a mainstream-tier agent at high effort, on the coordinator's brief, from `master` at `08e9e7ef` (clean at the start). The phase
was interrupted once by the owner's usage limit, with edits in the working tree and nothing staged. On resuming, the 20 modified files
were reviewed one by one against their diffs: all 20 were finished edits (the 18 Go files and `.golangci.yml` are the edits below as
applied by checked scripts, and `scripts/check_staticcheck_ignored.py` is its extension, verified by the planted runs), and none was
redone. No heavy run was executing or queued at the stop; the plant experiment that was cut short was run again in full, and every
gate below was run after the resume. The changes are in the working tree, unstaged, for the coordinator to commit after its own gate.

**Re-measured first**, on a scratch copy of `HEAD` with `staticcheck` switched on, four passes (darwin, linux, windows, linux with every
opt-in tag), limits raised: 50, 50, 50 and 51, **51 in the union**, all `staticcheck`: `SA1019` 35, `QF1011` 6, `QF1001` 5, `QF1002` 2,
`QF1003` 1, `QF1008` 1, `SA9003` 1. After the phase: 0 on all four passes with the tree's own `.golangci.yml`.

**The 35 `SA1019` findings, and the owner's pairing (Q5 A).** Every one is an `acp-go-sdk2` name deprecated only because it comes from
the unstable ACP schema, and every one is covered today by an existing, already-justified directive; none is uncovered, so there was
nothing to stop on. The pairing is nine directives: seven line-level and two file-level.

| File | Covers | Existing directive (line) and its reason | Findings it covers |
| :--- | :--- | :--- | ---: |
| `internal/provider/acpagent/acpagent.go` | line 1404 | `//lint:ignore SA1019` at line 1403: advertises the unstable ACP schema's plan capability (MADR 0192); 0207 amendment H | 1 |
| `internal/provider/acpagent/advertise_test.go` | line 69 | `//lint:ignore SA1019` at line 68: the unstable ACP schema's subagent capability, as an example (MADR 0195); 0207 amendment H | 1 |
| `internal/provider/acpagent/plans.go` | file | `//lint:file-ignore SA1019` at line 3: identified plans are the unstable ACP schema's plan_update (MADR 0192); 0207 amendment H | 11 |
| `internal/provider/acpagent/plans_test.go` | file | `//lint:file-ignore SA1019` at line 3: identified plans are the unstable ACP schema's plan_update (MADR 0192); 0207 amendment H | 17 |
| `internal/provider/acpagent/session.go` | line 2113 | `//lint:ignore SA1019` at line 2112: identified plans are the unstable ACP schema's plan_update (MADR 0192); 0207 amendment H | 1 |
| `internal/provider/acpagent/session.go` | line 2115 | `//lint:ignore SA1019` at line 2114: identified plans are the unstable ACP schema's plan_update (MADR 0192); 0207 amendment H | 1 |
| `internal/provider/acpagent/session.go` | line 2117 | `//lint:ignore SA1019` at line 2116: identified plans are the unstable ACP schema's plan_update (MADR 0192); 0207 amendment H | 1 |
| `internal/provider/acpagent/session.go` | line 2120 | `//lint:ignore SA1019` at line 2119: identified plans are the unstable ACP schema's plan_update (MADR 0192); 0207 amendment H | 1 |
| `internal/provider/acpagent/turnusage.go` | line 27 | `//lint:ignore SA1019` at line 26: the unstable ACP schema's turn usage (MADR 0207 D25); 0207 amendment H | 1 |

* **Line level (7 findings, 7 directives).** The form is `gobble-cli`'s: the `//lint:ignore` stays on the line above, and
  `//nolint:staticcheck // as the lint:ignore above` ends the line it covers. Each reason, with its record, stays on the
  `//lint:ignore`; the `//nolint` points at it rather than repeating it, so the reason is stated once.
* **File level (28 findings, 2 directives).** `golangci-lint` reads a `//nolint:staticcheck` placed on the line above the `package`
  clause for the whole file, which was tested on a scratch copy before use (11 findings suppressed in `plans.go`, none in the rest,
  and `nolintlint` took it as used). So each file-wide `//lint:file-ignore` is paired with one
  `//nolint:staticcheck // as the lint:file-ignore below` above `package`. That is two comments, not 28, and no `exclusions` entry in
  `.golangci.yml`: no stop was needed.
* `nolintlint` is already on: it takes the nine as used, and flags one that silences nothing (planted, below).

**The bound script, `scripts/check_staticcheck_ignored.py`.**

* **Before:** it held that every `//lint:ignore` and `//lint:file-ignore` names `SA1019` and cites "0207 amendment H", and that
  `staticcheck -show-ignored` reports nothing ignored but `SA1019` on an `acp-go-sdk2` unstable-schema name. It had no count: it
  printed "56 ignored finding(s)" on each GOOS.
* **After:** those two rules, plus pairing both ways (each `//lint:ignore` is followed by a line ending in the `//nolint` form, each
  `//lint:file-ignore` has the file-level `//nolint` above `package`, and no `//nolint:` naming `staticcheck` stands without its
  `//lint:ignore`/`//lint:file-ignore`), plus two recorded bounds: `BOUND_NOLINT = 9` and `BOUND_IGNORED = 56`. It prints "56 ignored
  finding(s) ...; 9 //nolint:staticcheck directive(s), each paired" on each GOOS; a count below the bound passes with "lower it".
* **Tests.** The script has none, and none was added: its evidence is the planted runs below, on scratch copies of the tree.

| Planted | Result |
| :--- | :--- |
| the tree as it is | exit 0, 9 directives, 56 ignored |
| a new `//nolint:staticcheck` with no `//lint:ignore` above | exit 1, "with no //lint:ignore on the line above" |
| a paired `//lint:ignore` removed | exit 1, the `//nolint` is unpaired |
| a `//nolint` removed from a covered line | exit 1, "does not end in `//nolint:staticcheck // as the lint:ignore above`" |
| the file-level `//nolint` removed above `package` | exit 1, the `//lint:file-ignore` has none |
| a file-level `//nolint` with no `//lint:file-ignore` | exit 1, "with no //lint:file-ignore in the file" |
| a tenth directive (a new, paired deprecated use) | exit 1, "10 ... over the recorded bound of 9" |

**The other 16 staticcheck findings, fixed by class, none changing behaviour.**

* `QF1001` De Morgan (5: `auth/token_test.go`, `codex/runtime.go`, `fake/fake_test.go`, `opencode/control/control.go`,
  `ws/receipt_e2e_test.go`): the negation pushed inside the boolean; the operands are comparisons and plain booleans, so there is no
  side effect to reorder, and short-circuiting is unchanged.
* `QF1002` and `QF1003` tagged switch (3: `cli/service/supervisor.go`, `acpagent/version_test.go`, `kilo/live_tool_stream_test.go`):
  `switch { case c == x }` to `switch c { case x }`, with `' '` and `'\t'` as one case, and an if/else-if on one value to a switch
  on it. Each case is a comparison of the same value, in the same order, with the same `default`.
* `QF1008` (1, `grok/methods.go`): `c.Provider.ClearCredential(...)` to `c.ClearCredential(...)`. `CoordinatedProvider` declares no
  `ClearCredential` of its own (its methods are `ClearCredentialMethod`, `clearOAuthSession`, ...), so the promoted method is the
  embedded provider's, and the `c.Provider == nil` guard above is unchanged.
* `QF1011` (6): `var started int = -1` to `started := -1` (an untyped constant, so `int` either way). Five in
  `kilo/session_ops_test.go` were `_ func(...) error = s.Compact`: signature assertions, where the declared type is the check, and
  dropping it would have made them vacuous. They became `hasSignature[func(...) error](s.Compact)`, a one-line generic helper whose
  type argument is the same assertion and fails to compile in the same way. That keeps the guard intact; dropping the types, or a
  `//nolint`, would not have.
* `SA9003` (1, `certs/acme_http_test.go`): an `if errors.Is(...) { /* comment */ }` whose body is a comment checks nothing and has no
  side effect; it is removed, the comment kept as the explanation, and the unused `errors` import dropped. No assertion existed to lose.

**Configuration.** `.golangci.yml` enables `staticcheck`. Still disabled, each with its phase: `goconst` P4, `errcheck` P5, `errorlint`
and `nilerr` P6, `gosec` P7, `bodyclose`, `gocognit`, `gocyclo`, `unparam` and `confusing-naming` P8.

**Seen to fail, both ways, on scratch copies.** A new deprecated use with no directive fails `make lint`'s own command (exit 1, `SA1019`)
and the standalone `staticcheck` (exit 1, `SA1019`). The same use with the pair passes (0 issues). A planted
`//nolint:staticcheck // silences nothing` fails (exit 1, `nolintlint`: "directive ... is unused for linter "staticcheck""). A planted
`errcheck`, `gosec` and `goconst` finding together passes (exit 0), the linters still disabled.

**Tests and host.** The suite is unchanged and no assertion changed; test files touched are the expression-only edits above
(`token_test.go`, `acme_http_test.go`, `fake_test.go`, `receipt_e2e_test.go`, `version_test.go`, `live_tool_stream_test.go`,
`methodid_test.go`, `session_ops_test.go`) and the two pairing comments in `advertise_test.go` and `plans_test.go`. `go test -race ./...`
on a clean copy through `run-on-host --race`: on the Windows laptop, exit 0, 49 packages `ok`; on the Linux host, exit 0, 50
packages `ok`. The macOS race run is the one inside `make preflight`. No `_windows.go` file changed, so `make ci-windows` was not run.

**Gates** (start, end, exit status, each recorded before any filter):

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `make pre-add-check` on the 18 changed Go files, with the vulnerability check, through `run-heavy` | 17:10:49 | 17:10:58 | 0 |
| `lint` | 17:09:37 | 17:09:55 | 0 |
| `make staticcheck` (with `scripts/check_staticcheck_ignored.py`, three GOOS) | 17:09:56 | 17:10:12 | 0 |
| `vet` | 17:10:12 | 17:10:14 | 0 |
| `vet-tags` | 17:10:14 | 17:10:37 | 0 |
| `tidy` | 17:10:37 | 17:10:38 | 0 |
| gofmt over `cmd`, `internal`, `scripts`, `test` | 17:10:38 | 17:10:38 | 0 |
| `make markdownlint-docs` (offline, from the npx cache) | 17:10:38 | 17:10:41 | 0 |
| `check-records` | 17:10:41 | 17:10:43 | 0 |
| `make preflight` through `run-heavy` | 17:10:58 | 17:14:15 | 0 |

**Not done.** The other P4 and later linters are not enabled; `.golangci.yml` still lists them disabled. The three directive forms in
this phase follow `gobble-cli`; `golangci-lint`'s handling of a trailing `//nolint` on a `case` line was not examined beyond the lint
passing, and the standalone `staticcheck`, kept by the owner's ruling, remains the exact check of each site.
