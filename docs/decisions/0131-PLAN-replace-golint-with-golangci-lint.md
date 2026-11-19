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

### P3, close-out, 2026-11-18

P3 passed the coordinator's gate on the tree as handed back (`make lint` 0, `make check-records` 0, `make pre-add-check` with the
vulnerability check 0, `make preflight` 0, the tree unchanged by the gate) and is committed and pushed: the record as `a2c16a70`, the
change as `e446770b`, on `master`. The coordinator read the whole P3 diff and found each of the 16 non-`SA1019` edits behaviour-preserving.
CI on `e446770b`, as the coordinator reported it: the workflow "CI", attempt 1, succeeded: Go (test) with 23 steps, Go on
darwin/arm64, linux/arm64 and windows/amd64 with 12 steps each, Live supervisors (runit, s6), Flutter (analyze and test) and Flutter
(test on windows); the APK and the two self-update jobs were skipped, there being no tag. "Docs" succeeded. "CI flake ledger" succeeded.

**What a `//nolint:staticcheck` covers, stated plainly.** golangci-lint's `//nolint:staticcheck` silences every staticcheck check it runs
at that scope, not only `SA1019`: for the seven line directives, one line each; for `internal/provider/acpagent/plans.go` and
`plans_test.go`, the whole file. Tested on a scratch copy of the tree: an `S1012` (`time.Now().Sub(t)`, which `time.Since` replaces) was
planted at `plans.go:86` and, as a control, in an uncovered file (`zz_control.go`), and both tools were run over `./internal/provider/acpagent`.

| Tool | Exit | `plans.go:86` (file-level `//nolint`) | `zz_control.go:5` (uncovered) |
| :--- | ---: | :--- | :--- |
| golangci-lint, `make lint`'s own command | 1 | not reported | `S1012: should use time.Since instead of time.Now().Sub` |
| standalone `staticcheck` v0.8.2, `make staticcheck`'s binary | 1 | `plans.go:86:50: should use time.Since instead of time.Now().Sub (S1012)` | reported |

So golangci-lint does not see a non-`SA1019` finding in `plans.go`, and the standalone `staticcheck` does, because `//lint:file-ignore
SA1019` names one check. There is no hole while the standalone `staticcheck` stays in the gate, which the owner's ruling (Q2 A) keeps. One
class is covered by neither: the quick-fix checks (`QF1xxx`) run only in golangci-lint's `staticcheck`, and are silenced at the nine
scopes; they are style suggestions, and a plant of one there would not be reported.

**Does this repository keep tests for its Python scripts?** No. Searched: `scripts/` (no `*_test.py`, no `test_*.py`, no tests directory),
the whole tracked tree for Python test files, `Makefile`, `make/`, `.github/workflows`, `AGENTS.md`, `README.md`, `docs/guides`, and the
Go tests (two Go tests mention `live_supervisors.py`; none tests `check_staticcheck_ignored.py`, `lint.py`, `check_records.py`,
`vet_build_tags.py` or any other `.py`). The repository's convention for test scripts is shell: `scripts/*_test.sh` for shell
scripts (`install-binary_test.sh`, `vulncheck_test.sh`, `coverage-delta_test.sh`, `ci-flake-capture_test.sh`), and of those only
`install-binary_test.sh` is wired (`make preflight`, `Makefile:467`, and the CI `go` job, `ci.yml:195`); the other three are not
referenced by the Makefile, the workflows or the guides. No Python-script test harness was invented. The gap is reported and put to the
owner with three options in the hand-back: a `scripts/check_staticcheck_ignored_test.sh` in `vulncheck_test.sh`'s style, wired like
`install-binary_test.sh`; a Python `unittest` file with its own make target and CI step, a new convention; or the planted runs as the
standing evidence, as P3 recorded.

**Owner's ruling on tests for the Python scripts, recorded 2026-11-19.** The owner chose "A Python unittest file with its own make
target and CI step". It tests the script's functions directly. It would be the repository's first Python test, so a new convention, and
it is not the option the agent recommended. It is to be built as the first step after phase 4 is committed, and handed back by itself.
Nothing was built for it in phase 4.

### Tests for the bound script, 2026-11-19

The step the owner ruled on under the P3 close-out ("A Python unittest file with its own make target and CI step"), built after phase 4
was committed (record `b82b96e7`, change `f37dda87`) and handed back by itself. It is the repository's first Python test, so it sets
the convention; no Go code changed and no race run was needed. The changes are in the working tree, unstaged; only this PLAN is staged.

**What and where.** `scripts/check_staticcheck_ignored_test.py`, a `unittest` file beside the script, standard library only, no
network, no staticcheck binary and not the real tree: the Go files are fixtures in a temporary directory, and the `git ls-files` call
and the staticcheck run are passed in or patched. **The convention chosen:** the tests of `scripts/<name>.py` are
`scripts/<name>_test.py`. The name follows the repository's `scripts/<name>_test.sh` convention for the shell scripts, with the
underscore a Python module name needs; a second script's tests sit beside its script under the same rule, and the one target finds them
by that pattern, so nothing is registered. `make test-scripts` runs
`python3 -m unittest discover -s scripts -p '*_test.py'`. Plain `python3 -m unittest` from the root finds nothing, because `scripts/`
is not a package and unittest's default pattern is `test*.py`; `python3 -m unittest scripts/check_staticcheck_ignored_test.py` from the
root works, and so does the discover form.

**The script was refactored, and only so far as the tests needed** (the command line is unchanged): `tracked_go_files()` holds the
`git ls-files *.go` call; `bad_directives` and `bad_pairs` take an optional list of files (the default is the tracked files);
`bad_ignored` takes the process runner (the default is `subprocess.run`); and its two `open(path).read()` calls became `with` blocks,
because each test leaked a file handle and Python reported it. `make staticcheck` on the real tree, before and after, through
`run-heavy`: the same six lines, byte for byte, on linux, darwin and windows (`check_staticcheck_ignored: 56 ignored finding(s), each
an acp-go-sdk2 unstable-schema name; 9 //nolint:staticcheck directive(s), each paired`), exit 0.

**The tests: 55**, in five classes. *DirectiveRule* (rule 1): a conforming directive passes; a code other than `SA1019` (ignore,
file-ignore, and `SA1019` among other codes); no reason; a reason that does not cite the record; an unreadable file skipped; the
default file list. *PairingRules* (rule 3, each both ways): an ignore with its `//nolint` on the next line passes, and is flagged with no
`//nolint` there, on the last line, naming another linter, with the wrong pairing comment or with none; a file-ignore with the
file-level `//nolint` directly above `package` passes, and is flagged without it, with a blank line between, with the wrong pairing
comment, with another linter, or in a file with no `package` clause; a `//nolint:staticcheck` with no ignore above it (or a blank line
between) is flagged; a file-level `//nolint` with no file-ignore in the file is flagged and with one passes; a `//nolint` naming
staticcheck among several linters counts, and is flagged when unpaired; one that does not name staticcheck (`gosec`, `staticcheckx`) is
neither counted nor flagged; the count over several files; an unreadable file; the default file list. *IgnoredFindingsRule* (rule 2,
with a stub for staticcheck): the argument vector; an SDK declaration with the unstable-schema notice, in both subject forms, is allowed
and counted; another code, another package, and another SDK deprecation are each flagged; a finding that is not ignored is skipped;
unreadable output; a staticcheck that fails to run; exit status 1 with findings is not a failure; a clean empty run. *Main* (the
bounds, the status and the lines): for each of the two bounds, at the bound passes, one over fails, below passes with "lower it"; the
exact summary line; each of the three rules failing the run and printing its line; no summary on failure; the binary argument is
passed on; usage with no argument exits 2. *TrackedFiles*: git is asked for `*.go`.

**Each test seen to fail.** 54 breaks of the script, each on a scratch copy of it (one rule broken at a time: a clause removed, a
comparison loosened or tightened, a message reworded, a branch deleted), run against the unmodified test file: every break turned at
least one test red, and every one of the 55 tests turned red under at least one break (a test that no break reddens fails the run of
the mutation script). The first draft had one test no break reddened (a clean empty staticcheck run), and a break that reddens it was
added. The list of breaks and, for each test, the breaks that redden it, is saved with the scratch results.

**Which Python.** The Makefile and every workflow call a bare `python3` and nothing pins it: there is no `setup-python`, no
`.python-version` and no `requires-python`. CI's `go` job (`ubuntu-latest`, with the checkout) uses the image's own `python3`. On a
developer's macOS the oldest in practice is Apple's `/usr/bin/python3`, 3.9.6, which is what a bare `python3` is when Homebrew's is not
first on the path. The test file uses nothing newer than 3.9 (no `match`, no `X | Y` at run time, no parenthesised `with`, no
`ignore_cleanup_errors`, no `onexc`), and the script's annotations are under `from __future__ import annotations`. All 55 pass, and
all 54 breaks are caught, on 3.9.6 and on 3.14.10.

**Wiring.** `Makefile`: the target `test-scripts` (after `staticcheck`) and in `.PHONY`; `make preflight` runs it as the step
`==> python script tests`, directly after staticcheck and before golangci-lint. `.github/workflows/ci.yml`, job `go` (the job that
runs the other Python-driven steps, `make check-records` and `make lint`): one step, `Python script tests (unittest)`, `run: make
test-scripts`, directly after the staticcheck step and before golangci-lint; nothing else in the workflow changed. `AGENTS.md` says no
workflow edit without the owner's permission; this edit is the step the owner ruled on, made at the coordinator's instruction.

**Docs.** `AGENTS.md` (Tests: the convention in one paragraph), `README.md` (the Development target list and the `go` job row of the CI
table) and `docs/guides/standards/go/README.md` (the paragraph that names the script now names its tests).

**Gates** (start, end, exit status, each recorded before any filter; `make staticcheck` waited in the queue behind another agent's run). `make lint`, `make check-records`, `make pre-add-check` and `make preflight` ran with the two PLAN entries of this step already in place. The working tree was unchanged by the gates: 8 files, 7 modified and 1 new, nothing else.

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `make staticcheck` through `run-heavy`, output compared with the run before the refactor: identical | 09:26:28 | 09:26:40 | 0 |
| `make test-scripts` (55 tests) | 09:27:38 | 09:27:39 | 0 |
| `/usr/bin/python3` 3.9.6, `-m unittest discover -s scripts -p '*_test.py'` (55 tests) | 09:27:39 | 09:27:39 | 0 |
| `python3 -m unittest scripts/check_staticcheck_ignored_test.py`, from the root (55 tests) | 09:27:39 | 09:27:39 | 0 |
| `make lint` (four passes) through `run-heavy` | 09:27:39 | 09:27:49 | 0 |
| `make check-records` | 09:27:49 | 09:27:51 | 0 |
| `make markdownlint-docs` (offline, from the npx cache) | 09:27:51 | 09:27:54 | 0 |
| `actionlint` over `.github/workflows` | 09:27:54 | 09:27:54 | 0 |
| `make pre-add-check`, 1,038 tracked Go files, with the vulnerability check, through `run-heavy` | 09:27:54 | 09:28:07 | 0 |
| `make preflight` through `run-heavy` (the new step runs after staticcheck) | 09:28:07 | 09:30:48 | 0 |
| the 54 breaks of the script, each run against the tests, on 3.9.6 and on 3.14.10 | not timed | not timed | 0 |

### P4. `goconst`, 2026-11-18

Run by a mainstream-tier agent at high effort, on the coordinator's brief, from `master` at `e446770b` (clean at the start). The
changes are in the working tree, unstaged, for the coordinator to commit after its own gate. The PLAN says nothing about two equal
strings that mean different things, and the brief says they must not share a constant; the rule applied is one constant per meaning
(the same text may have two constants), and no departure from the PLAN was needed.

**Re-measured first**, on a scratch copy of `HEAD` with `goconst` switched on, four passes, limits raised: darwin 378, linux 378,
windows 379, linux with every opt-in tag 378, **379 in the union**, as the PLAN expected: all in 113 production files, none in tests
(the fleet's own exclusion). 172 distinct (package, string) groups. By package: `internal/provider/codex` 153, `internal/cli/service`
96, `internal/provider/kilo` 55, `internal/provider/acpagent` 12, `internal/provider/grok` 10, `internal/provider/opencode` 8,
`internal/ws` 7, `internal/agenterr` 6, `internal/provider/fake` 6, and 26 more across 13 packages. After the phase: **0 on all four
passes** with the tree's own `.golangci.yml`.

**How it was done.** golangci-lint's `goconst` reports one occurrence of a string per file, so the first pass found 379 sites and each
pass after it found more of the same strings in the same files (121, 50, 27, 14, then the last by hand). Each round was decided by
reading the sites, recorded as a table of decisions, and applied by a checked script (assert-before-replace on the exact column, new
constants inserted after the file's imports, `goimports` and `gofmt` after). A blanket "replace every occurrence in the file" sweep was
tried once and discarded: it replaced `json:"text"` struct tags and a map key, and `gocritic` and `govet` (`structtag`) found it; the
affected files were restored from the scratch snapshot taken just before it, and the rest was done site by site. A struct tag that
had been changed once more by a later script was found by `govet` and restored; no struct tag differs from `HEAD` in the diff.

**What was added.** 235 unexported constants in 22 packages (counted on the tree as handed back after the correction round of
2026-11-19; the first hand-back said 222). No exported name was added or changed. By package: `internal/provider/codex` 106,
`internal/provider/kilo` 38, `internal/cli/service` 27, `internal/provider/acpagent` 11, `internal/ws` 9, `internal/provider/grok` 8,
`internal/provider/opencode` 7, `internal/provider/fake` 5, `internal/agenterr` 3, `internal/session` 3, `internal/appdirs` 2,
`internal/cli` 2, `internal/pairuri` 2, `internal/provider/acpagent/acptest` 2, `internal/provider/gobble` 2, `internal/relay` 2,
`internal/auth` 1, `internal/provider/credstore` 1, `internal/provider/httpagent` 1, `internal/receipt` 1, `internal/testexec` 1,
`scripts/smoke-protocol` 1. By class: other 47, wire field name 20, event status 17, wire method or notification name 16, mode id 13,
wire item type 11, environment variable name 10, approval decision 9, wire status 9, wire action name 8, command-line flag 7, GOOS
name 5, stop reason 5, approval policy / scope 4, environment status 4, config layer kind 4, binary name 4, engine part type 4, todo
status 4, tool / binary name 4, provider error code 3, plist kind 3, URL scheme 3, option id / kind 3, sandbox mode 3, subcommand name
2, JSON literal 2, content type 2, provenance label 2, summary kind 2, picker group 2, message role 2, version label 1. Each carries a
one-line comment naming what it is. The complete list (package, name, value, class, uses, file, the tests that keep the literal) is
saved with the scratch results.

**How the uses are counted** (corrected 2026-11-19; the first hand-back's "843 uses" was wrong and is not repeated). A *replaced
site* is a string literal in non-test code that the change turned into another expression. The independent checker pairs every such
site in the `HEAD` tree with its place in the working tree by type-resolved position, in four configurations (darwin, linux,
windows, and linux with every opt-in tag), and a site is counted once however many configurations compile it. On the tree as handed
back: **1,008 replaced sites**; **908** of them now read one of the 235 new constants, which is the number of references to the new
constants in non-test code, and every one of the 235 has at least one; **100** read an expression that already existed (no new
constant): `appdirs.ProductMcremote.Name` 29 and `appdirs.ProductMcrelay.Name` 22 (variables, see the correction round, item 5),
`string(KindLaunchdAgent)` 10, `string(KindWindowsTask)` 10, `string(KindSystemdUser)` 3, `string(provider.IDCodex)` 7,
`string(provider.IDOpencode)` 5, `string(provider.IDGrok)` 5, `string(provider.IDGobble)` 3, `string(provider.IDKilo)` 2,
`string(provider.IDFake)` 1, `collaborationModePlan` 1, `wsJWTIssuer` 1 and `wsJWTAudience` 1. The figure the first hand-back gave was the checker's
count of mentions plus one per constant for its declaration, and, for `kilo`'s `partText`, ten more, because that count included the
struct field `partText` (`internal/provider/kilo/session.go:73`) and its nine uses; that constant is now named `partTypeText`, so the
two no longer share a name. Saved counts of identifier mentions (`total uses` in the constants list) are a different number and are not
the references.

Existing expressions were used instead of new constants where one already held the meaning: `appdirs.ProductMcremote.Name` and
`appdirs.ProductMcrelay.Name` for the product names in `internal/cli`, `internal/cli/service` and `internal/relay`,
`string(KindLaunchdAgent)`, `string(KindWindowsTask)` and `string(KindSystemdUser)` for the scope names, `string(provider.ID<X>)` for a
provider's own name in its `Section` and registry entry (the same literal sat in `ID: provider.IDCodex, Name: "codex"`, so a second
constant would have named one meaning twice), `collaborationModePlan` for the `ModeID` of the codex `/plan` command, and `wsJWTIssuer`
for the issuer claim `launch.go` writes.

**Equal text, different meaning, kept apart.** Where one string had more than one meaning in a package, each meaning has its own
constant: in `codex`, `unknown` (a version, `versionUnknown`, an environment status, `environmentUnknown`, and a config layer's kind,
`layerUnknown`), `file` (a tool name, a URL scheme and a summary kind), `command` (a tool name, a JSON member and a summary kind, and
the approval `kind` that runs a command), `session` (an approval scope and a config layer), `error` (a notification name and a stop
reason), `pending`, `running`, `completed` and `failed` (the wire's item status and our event Status), `disconnected` (an environment
status and a session Status), `user` and `project` (a layer type the wire sends, a layer kind we project, and a setting's
provenance), `mcremote` (the thread source and the client name), `accept`, `decline` and `cancel` (an approval decision, an MCP
elicitation action, and for `cancel` an option kind: `decisionX`, `mcpActionX`, `optionKindCancel`), `plan` (the `/plan` command-table key,
`commandPlan`, against the mode id), and `codex` (the binary, `defaultBin`, against the provider's own name, which is
`provider.IDCodex`); in `acpagent`, `true` (a boolean option id, a configuration value and a picker meta value:
`boolOptionTrue`, `configValueTrue`, `metaConnectedTrue`, with `boolOptionFalse` beside the first) and `unknown` (a compaction kind, an
elicitation field kind, a plan status); in `kilo`, `kilo` (the binary and the model provider id of Kilo's own gateway,
`providerKilo`, against the provider's own name, `provider.IDKilo`), `cancelled`, `error`, `pending`, `retry` and `completed` (a
message, a todo item and our event Status), `text` (a JSON member and an engine part type) and `plan` (command-table key and mode
id); in `grok`, `plan` and `default` (a mode id and a permission-mode setting; `plan` also the command-table key); in `opencode`
and `fake`, the picker group and the binary or model-provider name (`fake` and `fake-remote` are the ids of the fake catalog's two
model providers, `modelProviderFake` and `modelProviderFakeRemote`, which is not the provider's own name). A mode's label and name,
which `kilo`'s picker entries spell the same as its id (`{ID: modeCode, Label: modeCode, ...}`), use the mode's id constant;
`grok`, `fake` and `opencode` keep independent display labels (`"Build"`, `"Echo"`), which are text, not ids.

**Tests.** No test file changed except seven, by comment only (the correction round, item 3): 897 (constant, test file) pairs, where
a test file contains a literal equal to a new constant's value, in 17 packages, are all left as they were, and the list is saved. A
test that asserts a wire literal therefore still asserts the literal and not the constant it would share with the code under test. The
source-scanning tests that read the packages (`contract_inputs_test.go`, `decoded_names_test.go`, `rawpath_test.go` and the others
that parse Go) passed on the changed tree: the codex contract's `implementedMethods` scan reads string literals in call arguments and
case labels, and the only method names that became constants are server notification names, which the route table supplies (the
checker's scan emulation, below, shows no client or server request name lost from those inputs).

**Seen to fail, on scratch copies** (`HEAD` plus the changes, run again on 2026-11-19, before the last one-line edit to `codex/launch.go`, with the same four results), through `scripts/lint.py`: a new literal repeated four times fails with
"string `zzrepeated` has 4 occurrences, make it a constant (goconst)"; a literal repeated three times in `codex` where a constant
exists fails with "string `threadId` has 3 occurrences, but such constant `fieldThreadID` already exists (goconst)"; a planted
unchecked error and a planted `os.ReadFile(name)` together pass (errcheck and gosec still off); the clean tree passes.

**Configuration.** `.golangci.yml` enables `goconst`. Still disabled, each with its phase: `errcheck` P5, `errorlint` and `nilerr` P6,
`gosec` P7, and `bodyclose`, `gocognit`, `gocyclo`, `unparam` and `confusing-naming` P8. `scripts/lint.py` is unchanged.

**Tests and host, first hand-back.** The suite is unchanged. `go test -race ./...` on a clean copy through `run-on-host --race`: on the Windows laptop,
exit 0, 49 packages `ok`; on the Linux host, exit 0, 50 packages `ok`. The macOS race run is the one inside
`make preflight`. `make ci-windows`, because `owneronly_message_windows.go` changed, ran on the Windows host: exit 0
("ALL SELECTED CHECKS PASSED").

**Gates, first hand-back** (start, end, exit status, each recorded before any filter; the correction round's are at the end of this entry):

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `lint` | 18:14:25 | 18:14:59 | 0 |
| `make staticcheck` (with the bound script, three GOOS) | 18:14:59 | 18:15:18 | 0 |
| `vet` | 18:15:18 | 18:15:21 | 0 |
| `vet-tags` | 18:15:21 | 18:15:47 | 0 |
| `tidy` | 18:15:48 | 18:15:49 | 0 |
| gofmt over `cmd`, `internal`, `scripts`, `test` | 18:15:49 | 18:15:49 | 0 |
| `check-records` | 18:15:49 | 18:15:51 | 0 |
| `make markdownlint-docs` (offline, from the npx cache) | 18:26:18 | 18:26:21 | 0 |
| `make pre-add-check` on the 114 changed Go files, with the vulnerability check, through `run-heavy` | 18:16:02 | 18:16:13 | 0 |
| `make preflight` through `run-heavy` | 18:16:13 | 18:19:43 | 0 |

**Not done.** `goconst` strings that appear only in tests are untouched, as the fleet excludes tests. Singleton uses of a string that
the first pass did not report (for example `"read-only"` in a validation list beside the constant that now names it) were left as they
were unless goconst reported them.

### P4, correction round, 2026-11-19

Run by the same agent on the coordinator's brief, after the coordinator's gate and an independent verifier's read of the first
hand-back (verdict: equivalent, with listed exceptions). The changes are in the working tree, unstaged; only this PLAN is staged. Two
usage-limit interruptions fell inside the round. **State found on resume** (the second): 118 `git status` lines (117 modified files,
unstaged, and this PLAN, staged); the tree built on darwin, linux and windows, with `go vet` and `gofmt` clean; the one script that had
stopped part-way (it had asserted on a single-line import in `grok` after editing some of its targets) was made safe to re-run, and each
of its targets was then checked to be present exactly once, by counting; no file was left half-edited. Each later script asserted
before it replaced, and a failed assertion changed nothing in its file.

**1. The counts** are corrected in the P4 entry above, with how each is counted: 235 constants, 1,008 replaced sites, 908 references to
the new constants, 100 sites that read an existing expression. The earlier "843 uses" is withdrawn.

**2. `collaborationModePlan`.** The first record said the existing constant `collaborationModePlan` replaced a literal as the codex
`/plan` command's mode id. It replaced the literal that was the *key* of the command table (`codex/commandtable.go`, the
`"plan": {Kind: command.KindCollaborationMode, ModeID: "plan"}` entry); the `ModeID: "plan"` stayed a literal. Now the key is the new
constant `commandPlan` (the `/plan` command's name) and the `ModeID` is `collaborationModePlan` (the collaboration mode's id), the same
split `grok`, `kilo` and `opencode` have (`commandPlan` and `modePlan`). `fake`'s table keeps its keys as literals, like every other key
of that table, and its `ModeID` is `modePlan`.

**3. Comments that cite a line.** Searched every tracked `.go`, `.md`, `.py`, `.sh` and `.yml` file for `file.go:N`, outside the dated
records, for a changed file. Twelve comments cited a position; each now names the function or constant, the comment text only (the
only way a test file changed in this phase; the checker reports the seven test files as differing, and each difference is one of
these comment edits):

| Where (as edited) | Cited | Now names |
| :--- | :--- | :--- |
| `internal/provider/acpagent/stalledpump_test.go:93` | `acpagent.go:655` | `session.watchConnClose` |
| `internal/provider/codex/capability_stability_test.go:13` | `capabilities.go:195` | `buildCapabilitySnapshot` |
| `internal/provider/codex/commandtable.go:22` | `session.go:310-317` | `session.SetModel`, `session.beginTurn` |
| `internal/provider/codex/commandtable.go:24` | `commands.go:225,654-675` | `Manager.runCanonical`, `dispatchDaemon` |
| `internal/provider/codex/commandtable.go:30` | `commands.go:53` | `Manager.commandContext` |
| `internal/provider/codex/commandtable_test.go:29` | `session.go:457` | `session.Compact` |
| `internal/provider/codex/item_test.go:277` | `session.go:1498-1504` | `session.handleDecodedNotification` |
| `internal/provider/codex/session.go:457` | `session.go:26,1183-1217` | `buildPrompt` and its `dataURL` handling |
| `internal/provider/codex/session.go:2774` | `session.go:26,1183-1217` | the same |
| `internal/provider/kilo/catalog_test.go:102` | `catalog_live.go:124` | `connectedProvidersResponse` |
| `internal/session/noticededupe_test.go:125` | `manager.go:202-208` | `entry.appendHistoryLocked` |
| `internal/session/turnlatency_test.go:25` | `codex/session.go:2305` | codex's `emitTurnFailure` and the other providers' turn-end emitters |

Several already pointed at other code in `HEAD` (`acpagent.go:655` is a comment on `startFailed`; `manager.go:202-208` is the fields of
`entry`), so they were wrong before this phase moved anything. Not edited: the dated records (`docs/decisions` and
`apps/*/docs/decisions`, 2,590 citations of changed files, and `docs/reports`, 287), which say where code stood when they were
written; a rewrite would falsify them. There is none in `docs/guides`, `docs/architecture.md` or the scripts. The guide
`docs/guides/standards/go/core.md` now says a comment names the function, not a `file:line`.

**4. One constant per meaning.** (a) Command-table keys against mode ids: `commandPlan` added in `codex`, `grok`, `kilo` and
`opencode`; the mode ids stay `modePlan` (or `collaborationModePlan` in codex); `fake` as in item 2. (b) `codex/callbacks.go`: the
elicitation actions an MCP server is offered are `mcpActionAccept`, `mcpActionDecline` and `mcpActionCancel`, apart from the approval
decisions `decisionAccept`, `decisionDecline` and `decisionCancel` and the option kind `optionKindCancel`. (c) `acpagent`:
`boolOptionTrue` and `boolOptionFalse` for an elicitation's boolean option ids, `configValueTrue` for a configuration value, and
`metaConnectedTrue` for the picker meta, which stay three. (d) `kilo`: the picker `Group` of a mode is its kind, assigned from the
engine's own kind field, so one constant, `modeKindPrimary`, serves both, and `groupPrimary`, which named the same value for the group
alone, is gone. The part-type constants are `partTypeText`, `partTypeReasoning`, `partTypeTool` and `partTypeRetry`, which no longer
share a name with the struct field `partText`. (e) A literal left beside a constant of its meaning in the same package, found by a
token-level scan (comments, raw strings, runes and struct tags skipped, log keys listed apart) and replaced by reading each site: 257
sites replaced where a value had one constant, 55 more decided one by one where it had several (`error` as a notification name, a
stop reason, a status: each to its own), and the product names the first pass could not see because their constant is an expression
(`setup.go` and `setup_service.go`, 7 sites, among them a `case "mcremote", appdirs.ProductMcrelay.Name:` that mixed a literal and an
expression for one meaning; and `Scope: "systemd-user"` twice, to `string(KindSystemdUser)`). Where the same text
named the provider itself, the new `providerName<X>` constants of the first pass are removed and `string(provider.ID<X>)` is read, since
`ID: provider.IDCodex, Name: "codex"` sat side by side in each `Section` (six packages). `fake`'s two catalog ids are
`modelProviderFake` and `modelProviderFakeRemote`, because they are model-provider ids and not the provider's name. (f) `kilo`'s
picker labels are the mode's id expression throughout its list (`Label: modeCode`); `grok`, `fake` and `opencode` keep independent
display labels (`"Build"`, `"Echo"`), which are text and not ids.

Constants added since the first hand-back: `acpagent` `boolOptionFalse`, `configValueTrue`, `statusError`; `codex` `commandPlan`,
`environmentReady`, `environmentUnknown`, `fieldText`, `mcpActionAccept`, `mcpActionCancel`, `mcpActionDecline`; `fake`
`modelProviderFake`, `modelProviderFakeRemote`; `grok`, `kilo` and `opencode` `commandPlan`; `kilo` `partTypeReasoning`,
`partTypeRetry`, `partTypeText`, `partTypeTool`, `statusCompleted`, `stopReasonError`, `todoInProgress`, `todoPending`. Removed:
`codex` `limitProviderCodex` (the provider's name is `string(provider.IDCodex)`); `fake` `groupFake`, `groupFakeRemote` and
`providerNameFake` (the last two renamed to the model-provider constants); `kilo` `groupPrimary` and the four `part*` names above;
`opencode` `providerNameOpencode`. Net 222 to 235. No exported name was added; no `//nolint:goconst`, configuration exclusion or
threshold was needed.

**The sweep of the saved constants list for the three patterns**, over all 235, by reading what a script listed:

- *A constant at a site of another meaning:* found `codex` `contentText` as a JSON member in an outgoing message (the member is
  `fieldText`; the content type stays `contentText`), `opencode` `authTypeKey` as the request body's member (restored to the literal
  `"key"`), the product name constants at cli sites that spelled the wrong product, `gobble` and `grok` provider names against the binary
  name, `fake`'s group against its provider name, and `kilo`'s wire, status and stop reason `error`, `pending` and `completed`, which
  were one constant and are now one each. All fixed.
- *One meaning under two constants:* the six `providerName<X>` against `provider.ID<X>` (above); `codex` `limitProviderCodex`, same;
  `kilo` `groupPrimary` against `modeKindPrimary`; `fake` `groupFake` against `providerNameFake`. All fixed. Kept as two, on purpose,
  and listed so a reader does not take them for duplicates: `statusX` against `wireX` in `codex` and `kilo` (our event Status against
  what the engine sent), `layerX`, `wireLayerX` and `provenanceX`, `plistElemDict` (the XML element) against `plistKindDict` (a node
  kind) in `cli/service`, `binName` against `providerKilo` (the model provider id inside Kilo's gateway).
- *A literal left beside its constant in the same package:* four literals stand on lines this phase changed, kept on purpose:
  `codex/items.go` `"plan"` (a thread item type, not the command or the mode), `codex/provider.go` `"test-helper"` (a version string,
  two uses), `fake/fake.go` the `"plan"` table key (as item 2) and `ws/server.go` `Name: "dev"` (a display name beside the id
  `devDeviceID`). One more from the same scan, on a line this phase edited, was fixed (`codex/launch.go`, `"codex-app-server"` beside
  the issuer, now `wsJWTAudience`). The scan was widened to every string constant in a package, those that were there before this
  phase included: 40 more pairs exist that this phase did not touch, some of them local variables the scan cannot tell from
  constants (for example `grok` `"xai"` beside `xaiUpstreamID`, `kilo` `"/kilocode/provider-usage"` beside
  `providerUsagePath`, `codex` `"warn"`, `"refuse"` beside `sandboxPolicyWarn` and `sandboxPolicyRefuse`, and the two item-type
  allowlist maps of `codex/items.go`, which hold some names as constants and the rest as literals because only the repeated ones were
  named). They are listed in the saved results and **not changed**: a clean-up of old code is outside P4's scope, and is put to the
  owner at the end of this section.

**5. Recorded, not changed.** 51 sites now read `appdirs.ProductMcremote.Name` (29) or `appdirs.ProductMcrelay.Name` (22). Those are
package-level *variables*, not constants, so an expression made of them is not a constant expression. The consequence is in `case`
lists: `internal/cli/service/refresh.go:135` and `internal/cli/service/setup.go:313`, `999`, `1022` and `1322` are no longer constant
cases, and the compiler no longer rejects a duplicate there (for example `case appdirs.ProductMcremote.Name, appdirs.ProductMcrelay.Name`
written with one product twice). Nothing in the tree has a duplicate; `go vet` and the tests are the guard; a switch on the product that
must reject a duplicate would have to compare in code. The verifier's "not proven" list, kept here as it stands: the compiled binaries of
`HEAD` and the working tree were not compared; the five `_other.go` files are compiled in no configuration the checker ran, so
their sites are read, not compiled; and the source scanners were emulated by the scan emulation, not
run (the real scanners ran in `go test`, which passed).

**6. Verification again, on the tree as handed back.** The verifier's tree builder (read first; it reads the repository only) rebuilt
`old/` from `HEAD` and `new/` from the working tree, and its checker ran over darwin, linux, windows and linux with the opt-in tags
(`debugpprof`, `live_codex`, `live_codex_contract`, `live_codex_review`, `live_codex_turn`, `live_gobble`, `live_grok`, `live_kilo`,
`live_opencode`), with `GOPROXY=off`: exit 0; 235 inserted constants in the union; 128 changed Go files, 7 of them tests; every changed
file compiled in some configuration; 0 inner-scope identifiers named like a new constant (the struct field `partText` is no longer
shadowing one); 0 sites with a differing type (97 typed new expressions); the replaced sites are 1,001 on darwin, linux and the tag
pass and 1,008 on windows. It reports 40 findings, all of one kind: a comment added or removed, five comments in `codex/commandtable.go`
and `codex/session.go` (each found in all four configurations), which are the item 3 edits listed above, and the seven test files
that differ, each one comment of that table. There are no other findings. The scan emulation (`scanemu`), which lists the string
literals of each non-test file in call arguments, case labels and the other positions that the repository's scanners read, shows
55 values gone from those positions in `internal/provider/codex`; every one is a server notification name or not a method name at
all, and none is a client request or a server request name of the contract manifest (16 notification names and 39 other values:
enum words, flags, subcommand names). The notifications are classified by the route table, not by that scan.

**Tests and host.** The suite is unchanged. `go test -race -count=1 ./...` on a clean copy of the tree, through `run-on-host --race`: on the Windows laptop, exit 0, 49 packages `ok`, 0 failures or races; on the Linux host, exit 0, 50 packages `ok`, 0 failures or races (no WSL result is used for a race run). The macOS race run is the one inside `make preflight`. `make ci-windows`, because `owneronly_message_windows.go` differs from `HEAD`, ran on the Windows host: exit 0 ("ALL SELECTED CHECKS PASSED"). The working tree was the same before and after the gates: 132 `git status` lines (131 modified files and this PLAN, staged, with nothing untracked).

**Gates on the tree handed back** (start, end, exit status, each recorded before any filter):

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `make lint` (four passes, `goconst` on; the start includes time queued behind another agent's run, 46 s of its own) | 08:38:33 | 08:43:09 | 0 |
| `make staticcheck` (with the bound script, three GOOS) | 08:43:10 | 08:43:34 | 0 |
| `go vet ./...` | 08:43:34 | 08:43:36 | 0 |
| `make vet-tags` | 08:43:36 | 08:44:01 | 0 |
| `go mod tidy -diff` | 08:44:01 | 08:44:02 | 0 |
| `gofmt -l` over `cmd`, `internal`, `scripts`, `test` | 08:44:02 | 08:44:02 | 0 |
| `make pre-add-check` on the 128 changed Go files, with the vulnerability check, through `run-heavy` | 08:44:02 | 08:44:42 | 0 |
| `make preflight` through `run-heavy` | 08:44:42 | 08:47:48 | 0 |
| `make check-records` | 08:53:27 | 08:53:29 | 0 |
| `make markdownlint-docs` (offline, from the npx cache) | 08:53:29 | 08:53:31 | 0 |
| the verifier's checker and scan emulation, through `run-heavy` | 08:34:43 | 08:37:06 | 0 |
| the lint plants (`p4test`), through `run-heavy` | 08:31:07 | 08:32:34 | 0 |
| `go test -race -count=1 ./...`, Windows laptop, through `run-on-host --race` | 08:38:13 | 08:39:51 | 0 |
| `go test -race -count=1 ./...`, Linux host, through `run-on-host --race` (queued behind another agent's run; 308 s of its own) | 08:38:10 | 09:00:23 | 0 |
| `make ci-windows`, Windows host, through `run-on-host` | 08:46:42 | 08:48:05 | 0 |

**Put to the owner (three options each, in the hand-back):** the 40 untouched literal and constant pairs of old code; and the
`knownNonToolItems` and `itemsRenderedAsTools` maps of `codex/items.go`, which mix constants and literals for one meaning.

### P4, close-out, 2026-11-19

P4 is committed and pushed on `master`: the record as `b82b96e7` and the change as `f37dda87`. The coordinator's gate on the corrected
tree: `make lint` 0, `make check-records` 0, `make pre-add-check` with the vulnerability check 0, `make preflight` 0, the tree
unchanged by the gate. The coordinator re-ran the independent verifier's own checker binary on fresh copies of `e446770b` and of the
working tree: exit 0 on all four configurations; 1,001 replaced sites on darwin, linux and the tag pass and 1,008 on windows (950 and
957 to constants, 51 to non-constant expressions), 97 typed new expressions, 0 sites with a differing type, 235 inserted constants,
every changed file compiled in some configuration; 40 non-INFO findings, every one a comment difference in
`internal/provider/codex/commandtable.go` and `session.go`, and no other finding.

CI on `f37dda87`, as the coordinator reported it: the workflow "CI", attempt 1, succeeded: Go (test) with 23 steps; Go on darwin/arm64,
linux/arm64 and windows/amd64 with 12 steps each; Live supervisors (runit, s6); Flutter (analyze and test); Flutter (test on windows);
the APK and the two self-update jobs skipped, there being no tag. "Docs" succeeded.

**The two questions the correction round put to the owner**, decided by the coordinator under the owner's instruction "we need to limit
scope creep", and recorded as the coordinator's decision and not as the owner's choice between the options: the 40 older
literal-beside-constant pairs this phase did not touch, and the two lists in `internal/provider/codex/items.go` that mix constants and
literals, are **left as they are**, known and not pursued; `goconst` does not flag them and they are outside P4. The 2,877 line
citations in the dated records (2,590 in the decision records and 287 in the reports) stay untouched: records are history.

### Tests for the bound script, close-out, 2026-11-19

The step is committed and pushed on `master`: the record as `34b87b81`, the six changed files as `fa032e7f` and the test file as
`23b95c6c`. The coordinator's gate on the tree as handed back: `make test-scripts` 0, `make lint` 0, `make check-records` 0, `make
pre-add-check` with the vulnerability check 0, `make preflight` 0, the tree unchanged by the gate.

**A slip, recorded as the coordinator's and not the agent's.** The coordinator's commit script listed the change files with `git diff
--name-only`, which omits untracked files, so `scripts/check_staticcheck_ignored_test.py` was left out of `fa032e7f`. It was pushed alone
as `23b95c6c` minutes later, and GitHub cancelled the CI run on `fa032e7f` when the next push arrived (its "Docs" run succeeded).

CI on `23b95c6c`, as the coordinator reported it: "CI", attempt 1, succeeded: Go (test) with 24 steps (one more than before, the new
script-tests step); Go on darwin/arm64, linux/arm64 and windows/amd64 with 12 steps each; Live supervisors; both Flutter jobs; the APK and
the self-update jobs skipped, there being no tag. That answers the step's "not verified" item: the new step ran on the runner's own
`python3` and passed.

### P5. `errcheck`, 2026-11-19

Run by a mainstream-tier agent at high effort, on the coordinator's brief, from `master` at `23b95c6c` (clean at the start), and corrected
the same day after an independent verifier and the owner's rulings (the correction round, below). The changes are in the working tree,
unstaged, for the owner to commit; only this PLAN is staged. The governing ruling is Q3 A, label "Allow the explicit `_ =` discards here;
fix the 332 bare calls (Recommended)", with `errcheck.check-blank: false` already in the file from P1.

**A departure from the PLAN's stop rule, on the coordinator's instruction; the owner ruled afterwards.** The PLAN's P5 step says that a site
where handling the error is a behaviour change stops the phase for the owner, and the MADR says the same of an exported rename. The
coordinator's brief told the agent instead to make such changes and list them. The agent followed the brief. That was the coordinator's
error, and the first version of this entry, which said "No departure from the PLAN was needed", was wrong and is corrected here. The
independent verifier found it, read all ten class (a) changes (it judged them correct), and the owner then ruled on each. R1: "Accept the
nine real changes; replace `SectionOf` with plain checked assertions at its six sites (Recommended)". R2: "Fix it in this phase: those
sites return the write error" (not the option the coordinator recommended). R3, on the three outputs this entry first put to the owner:
"`auth recovery status` and `receipts verify` return it; `choose` is left (Recommended)". R4, on the receipt log's message: "Build it: a new
exported error and an exact log message" (not the option the agent or the coordinator recommended). All four are recorded below as
deviations from the stop rule, accepted by the owner. **This phase therefore adds one exported name, `receipt.ErrCloseAfterSync` (R4);**
the first version of this entry said it added none, and after R1 it said so again; both statements are superseded.

**Re-measured first**, on a scratch copy of `HEAD` with `errcheck` switched on, four passes, limits raised: darwin 305, linux 305,
windows 318, linux with every opt-in tag 305, **319 in the union** (the PLAN expected about 332; the 13 fewer are the part of its
estimate that was not `fmt.Fprint*`). All 319 are in non-test code: the fleet's own exclusion (`.golangci.yml`, `exclusions.rules`) removes
`errcheck` from `_test.go`. By callee: `fmt.Fprint*` 257 (`Fprintln` 129, `Fprintf` 123, `Fprint` 5), the PLAN's 257; `Close` 31 (response
bodies 9, files 7, websocket and network connections 7, temporary files 3, registry keys 2, a token, a directory and a listener 1 each);
`windows.CloseHandle` 10; a receive from a `chan error` that drops the value 9; a single-value type assertion 11 (`check-type-assertions`
is on); `procFreeConsole.Call` 1. By package: `internal/cli` 126, `internal/cli/service` 108, `internal/relay` 30, `internal/procutil` 10,
`internal/provider/httpagent` 10, `internal/relayhost` 6, `internal/provider/codex` 5, `scripts/smoke-protocol` 4, `internal/receipt`,
`internal/admin` and `internal/provider/credstore` 3 each, `internal/certs` 2, and 12 packages with 1. After the phase: **0 on all four
passes** with the tree's own `.golangci.yml`. The verifier re-measured `HEAD` independently and got the same 319 (305, 305, 318, 305),
matching `sites.tsv` site for site.

**The rule, applied to every site.** An ignored error is an explicit discard (`_ =`), never a bare call, and each is a decision:

1. A file that was **written** is closed with its error checked and returned, unless an earlier error is already being returned, in which
   case `_ = f.Close()`. Every written-file close in the tree already followed that on its success path (`certs.writeFile`,
   `setup.writeUnitAtomic`, `credstore.writeFileAtomic`); what `errcheck` found there were the error-path closes, which are the second
   half of the rule. The one written file whose close was dropped is `receipt.Store.Append`, class (a) below.
2. A handle that was only **read**, a response body, a socket, a directory opened to fsync it, a registry key, a process or job handle:
   `defer func() { _ = x.Close() }()` (or `_ = x.Close()` on an error path). A close error there cannot mean lost data. A reason comment
   is added only where the reason is not evident from the line.
3. A command's **status lines**: through one helper, `internal/cliout` (`Fprintf`, `Fprintln`, `Fprint`), which drops the write error in
   one place, with the reason in its package comment. A command's **result** (a generated file body, a JSON document, a statement of
   record, a value shown once) is not a status line: it returns its write error (R2, below).
4. A receive from a channel that carries an error, or a single-value type assertion: **handled**, class (a).

**Classes, after the correction round.** (a) the error matters, or the assertion could panic, and is handled: **35 sites**, 21 in the first
round's ten changes and 14 that are data output (R2 and R3). (b) the error cannot matter, an explicit discard or the helper: **284 sites**,
243 through `cliout` and 41 `_ =`. (c) test code: **0**, the fleet's exclusion. No site fit none of the classes. Every site is in `sites.tsv` in
the scratch results (path, HEAD line, working-tree line, callee, class, reason, and for (a) the test).

**Class (b), the 243 status lines.** The 31 functions of the 257 were read: all print to a cobra command's writer (`cmd.OutOrStdout()`,
`ErrOrStderr()`, a buffer a test passes) or to a `text/tabwriter` over one, whose own `Flush` is checked (`engines.go`, `receipts.go`).
That reading looked at what the writer is when a command runs, and missed that a writer can be a file or a pipe the operator chose, so a
function that prints a result is not a status function; the 14 data sites, found by the verifier and the owner's rulings, are the correction (R2, R3). The
status sites became `cliout.Fprint*` by a checked script (the text at errcheck's reported column asserted before each replace);
`goimports` then fixed the imports. Calls `errcheck` does not report (`os.Stderr`, a `bytes.Buffer`) were left as they were.

**Class (a), the first round's changes, each listed.** Lines are HEAD's; the working tree's are in `sites.tsv`.

| Where | Before | Now | Test |
| :--- | :--- | :--- | :--- |
| `internal/receipt/store.go:251` `Store.Append` | `defer f.Close()` on a file just written and fsynced; the close error was dropped | the file is closed by `appendLine`, which returns the close error (`close <path>: ...`); if the sync had succeeded the line is on disk, so the chain's last-hash cache is still updated and the next entry chains onto it. The callers (`session.Manager`) do not retry and log a warning: `receipt: appended, but closing its file failed` for this case (R4, below), `receipt: append failed` for any other failure, so the effect is a warning where there was none | `TestAppendLine` (four cases), and, through a new unexported opener seam on `Store`, `TestAppendWhoseCloseFailsAfterAGoodSyncStillChainsTheNextEntry` (E4, below) |
| `internal/provider/httpagent/provider.go:666` `startServer` | `case <-waitCh:` dropped the engine's exit status | `exited during startup (exit status 1)`; the status is in the returned error, which the phone shows | `TestStartServerBailsWhenEngineExitsImmediately`, now asserting `(exit status 1)` |
| `provider.go:1315` `freePort` | `l.Addr().(*net.TCPAddr)`, a panic if the address were not TCP | `tcpPort` returns an error for any other address | `TestTCPPort` |
| `provider.go:623, 647, 653, 686` `startServer`; `internal/provider/codex/provider.go:735, 795` | a bare `<-waitCh` after killing the engine: the exit status vanished | `p.awaitExit(waitCh)` receives and logs a non-nil status at debug level (`engine process ended`); these paths return their own error | `TestAwaitExitLogsHowTheKilledEngineEnded` in both packages |
| `internal/daemon/daemon.go:564` shutdown | the drain of the admin socket's result was a bare receive | `drainAdmin` logs a non-nil result as a warning (`admin socket ended with an error during shutdown`) | `TestDrainAdminLogsAFailure...`, `...IsSilentForACleanStop`, `...GivesUpAfterTheWait` |
| `internal/relay/server.go:300` `Serve` shutdown | `<-errCh` after `Shutdown` | `logServeEnd` warns on anything but `http.ErrServerClosed` | `TestLogServeEnd` |
| `internal/relay/server.go:949`, `internal/relayhost/client.go:320, 344` | `pool.Get().(*[]byte)`, a panic in a copy goroutine if the pool held another type | `poolBuf` answers a fresh buffer of the right size | `TestPoolBuf...` in both packages |
| `internal/relayhost/client.go:363` `httpClient` | `http.DefaultTransport.(*http.Transport).Clone()`, a panic if something else replaced it | `cloneTransport` falls back to a transport with the standard library's defaults | `TestCloneTransport...` |
| `codex/section.go:321, 324`, `gobble/section.go:67`, `grok/section.go:147`, `kilo/section.go:102`, `opencode/section.go:81` | `b.Section.(*Section)`, a single-value assertion: a panic with the runtime's text, `interface conversion: config.ProviderSection is <got>, not <want>`, which names both types | the two-value assertion, panicking on `!ok` with that text (R1, below) | `TestProvidersPanicNamingBothTypesWhenBuiltFromAnotherSection`, `TestCodexCredentialsPanicNamingBothTypesForAnotherSection` (`internal/provider/registered`) |

The changes that can be seen are the first three and the log lines; the assertion fallbacks are not reachable by construction. The first
version of this entry said a bare assertion "named only the interface" and proposed an exported helper to name both; both statements were
wrong, and the helper is gone (R1).

**R1. The six section assertions, and `provider.SectionOf` removed.** The owner accepted the nine real changes and replaced the helper
with plain checked assertions: `provider.SectionOf`, its test file and its doc comment are deleted (`internal/provider/descriptor.go` is
byte-identical to `HEAD`), so R1 adds no exported name; `internal/cliout` is a new package, and its functions are the helper the PLAN
names. This sentence stood before R4; see the header: R4 adds one exported name. The form, settled with evidence on a scratch copy through `make lint`'s command: a **two-value assertion that panics on `!ok`
with `fmt.Sprintf("interface conversion: config.ProviderSection is %T, not %T", x, (*Section)(nil))`**, the same at all six sites. Evidence:

* A bare `s.(*A)` is rejected by `errcheck` (`check-type-assertions`); the two-value form with that panic passes all four passes.
* Run on a throwaway program, the bare assertion panics with a `*runtime.TypeAssertionError` whose text is `interface conversion:
  main.Iface is *main.B, not *main.A`; the explicit panic carries **the same text** for every non-nil section. Its value is a string, not a
  `*runtime.TypeAssertionError`; nothing in this repository inspects the panic value (the recovers log it). The one other difference is a nil
  interface: the runtime says `is nil, not *codex.Section` and the explicit panic, through `%T`, says `is <nil>, not *codex.Section`. A nil
  section looks unreachable, because each provider's `Section` function returns a default section, but that was not proven by tracing the
  config loading.
* The only way to keep the runtime's own error type is to re-assert in the failed branch (`_ = s.(*A)`); `errcheck` rejects that too, so
  it is not available without a `//nolint`.

*Notes from the verifier on R1.* Reverting one of the six sites to the bare assertion does not fail the wrong-section test, because the
texts are identical for a non-nil section: that is the equivalence the ruling asked for, not a gap in the test.

**R2. Data output returns its write error.** The owner ruled that the sites that write a command's result stop using the helper and return a
failed write, so the command exits non-zero. Built as ruled: at each site `fmt.Fprint*` is called and its error returned, wrapped with what was
being written and **never with the data**; a healthy writer gets the same bytes as before (the same format and arguments). The sites, 11 of
the 257, by HEAD line: `internal/cli/setup_service.go:158` (`--refresh --print-only`, the refreshed definition) returns "write the refreshed
service definition: ..." and `:196` (`--print-only`) "write the service definition: ..."; `internal/relay/cli.go:492` and `:524` the same two
for mcrelay; `internal/cli/receipts.go:264, 266` (`receipts show`, the signature verdict) and `:268` (the statement's JSON) "write the receipt:
..."; `internal/cli/pair.go:88` (`pair create`, the token), `:97` (its pairing URI), `:157` (`pair code`, the code) and `:167` (its URI), each
with the sentence below. The verifier's list was nine; the two `receipts show` verdict lines are added here because they qualify the statement
the same command prints (a verdict a script reads next to the JSON), and are named so the owner can strike them.

*What `pair create` and `pair code` do when the print fails, exactly.* The credential is minted and stored before it is printed, and nothing is
revoked or minted again. For a token, the error is: "device `<id>` was created but its token could not be written, and only its hash is stored,
so it cannot be shown again; it stays valid until revoked (mcremote pair revoke `<id>`): `<write error>`". For a failed URI after the token
was written: "device `<id>` was created and its token was written, but the pairing URI that carries it could not be: ...". For a code: "the
pair code for `<name>` was created but could not be written; it stays valid for `<remaining time>` or until used (run the command again for
another): ...", and for its URI "... was written, but the pairing URI that carries it could not be: ...". The token or code stays valid;
the user is told so, and told the way out; the exit status is non-zero. Today the same failure exits 0 and says nothing. That is better,
not worse, in every case the agent could find, so there was no stop; the one consequence to know is that a token that could not be shown is
a live credential nobody has until it is revoked, and the error names the command that does that.

*The trace for other data sites.* The verifier's `tools/writers.py` groups the 243 remaining helper sites by function and writer
expression (`out`, `w`, `cmd.OutOrStdout()`, `tw`, `errOut`). Every remaining function was read for a result rather than a status line:
`--json` outputs (`PrintRefreshResult`'s and `printPaths`' return the encoder's error already; they are not among the helper's sites), a
generated file body, a secret or a value shown once. None of the rest prints a token, key, secret or generated file; the repository's own
`printPaths` already uses `_, _ =` for its text report and returns the JSON's error, the same line this ruling draws. What was judged a status
line and left, so the owner can overrule: `mcremote doctor`'s reports, the install, uninstall, remove and refresh result lines, the device
and engine tables (their `tabwriter.Flush` is checked), `pair list`, `pair revoke` and `pair prune` lines, the QR and the OSC 52 copy
sequence, and the host, fingerprint and relay lines of the pairing commands.

**R3. The three outputs that were put to the owner.** The owner ruled "`auth recovery status` and `receipts verify` return it; `choose` is
left (Recommended)". Built: `auth recovery status` (`auth_recovery.go`, HEAD line 147) returns "write the recovery status: ...", as an
`exitCodeError` with the failed-command code 3, like the command's other failures; `receipts verify` (HEAD lines 195 and 198, the OK and
the BROKEN line, one output) returns "write the verification result: ..." on the OK path. With these, the data outputs are 13 (14 call
sites). `auth recovery choose`'s confirmation (`:192`) stays through `cliout` and is judged status: its outcome is in its exit status.

*Does `receipts verify`'s exit status already carry OK or BROKEN?* Yes: `receipts.go` returns `fmt.Errorf("chain broken at line %d", broken)`
for a broken chain, which the root turns into exit status 1 (`ExitCode`'s default), and returns nil for an intact one. *A failed write and
a BROKEN result, so neither masks the other.* On a broken chain the error is `chain broken at line N, and the verification result could not
be written: <write error>`: it begins with the broken-chain sentence, wraps the write error (`errors.Is` finds it), and the exit status is
1 as it is when the write works. A broken chain is never reported as merely "could not write". On an intact chain a failed write is an error,
`write the verification result: ...`, and the text does not mention a broken chain. With a healthy writer both outputs and errors are
byte-for-byte what they were (the broken error is exactly `chain broken at line 2`). Tests, in `internal/cli/verdict_output_test.go`:
`TestAuthRecoveryStatusReturnsAFailedWrite` (and the exit code 3), `TestReceiptsVerifyIntactChainReturnsAFailedWrite`,
`TestReceiptsVerifyBrokenChainStaysBrokenWhenTheWriteFails` (both fail) and
`TestReceiptsVerifyBrokenChainWithAHealthyWriterIsUnchanged` (only the broken chain). Plants: the write check removed at each of the two
sites, the broken error replaced by the write error alone, the broken error without the write error, and the intact error made to say
"broken": each fails a named test.

*A consequence of the ruling, stated.* An audit script can no longer tell "the chain is intact but the result could not be printed" from
"the chain is broken" by `receipts verify`'s exit status alone: both exit 1. The error text tells them apart (`chain broken at line N...`
against `write the verification result: ...`). The behaviour is as ruled and is left as it is.


**R4. `receipt.ErrCloseAfterSync`, an exact log message.** The owner ruled "Build it: a new exported error and an exact log message", the agent's
E5 proposal, built as proposed:

* `internal/receipt/store.go` exports `ErrCloseAfterSync`, its doc stating exactly when it is returned: the receipt line was written and synced,
  the chain's last-hash cache advanced onto it, and only the close then failed; not for a failure to open, write or sync. `appendLine` wraps it
  around the close error, so `Append`'s error is `close <path>: <close error>: receipt written and synced` and `errors.Is` finds the sentinel.
* The two `internal/session/manager.go` callers go through `logReceiptAppendFailure`, which logs, at the existing line's level (Warn), `receipt:
  appended, but closing its file failed` (a receipt) or `receipt: appended unavailable marker, but closing its file failed` (the marker) when
  `errors.Is(err, receipt.ErrCloseAfterSync)` holds, and the old text (`receipt: append failed`, `receipt: append unavailable marker failed`)
  otherwise. The four texts are constants so a test holds their wording.
* Tests: `TestAppendWrapsErrCloseAfterSyncOnlyWhenTheLineIsInTheChain` (`Append`, through E4's opener seam: the sentinel holds when the close
  fails and not when the write or the sync does), the `TestAppendLine` table (with the sentinel column), and in
  `internal/session/receiptlog_internal_test.go` the manager's two tests (the exact message and level for each of the four cases, and the
  constants' exact text). Plants: the wrap removed (two tests fail), the wrap added to a write or to a sync failure, the `errors.Is` branch
  inverted, the new message shortened, the level changed.
* **One thing to know, not a stop.** The manager's tests cannot hand the manager a store whose file misbehaves (its `Store` is the concrete
  type with an unexported opener, in another package), and the seam's visibility was not widened. So the manager is tested through
  `logReceiptAppendFailure` given an error that wraps the sentinel and one that does not, which is the logic the callers use; which message
  pair each caller passes is read, not run, and the end-to-end path, a real `Append` failing its close inside the manager, is covered by the
  `receipt` test and this one together, not by one run. The verifier tried swapping the first caller's message pair and the whole `session`
  package still passed; that is this gap, and it accepted it by judgement: the pair is read from two adjacent lines with named constants,
  and both halves are tested.

*What the healthy-writer test holds.* `TestPairOutputIsUnchangedForAHealthyWriter` checks the labels of `pair create`'s lines and its closing
notice, not the bytes; a one-byte change to the token line is caught by the URI test's check instead. Recorded as it is.

**Candidates left through `cliout`, not decided.** The coordinator decided, under the owner's scope instruction ("we need to limit scope
creep"), that these are recorded and not pursued, to be told to the owner: the empty-list lines of `receipts list` ("No receipts for device
...", "No devices have a receipts chain.") and of `pair list` ("No devices paired."), which exit 0 on a failed write while the non-empty
forms return the tabwriter's flush error; the setup, uninstall, remove and refresh result lines; `doctor`'s report; and, in the pairing
commands, the host, URL and fingerprint lines and the clipboard copy.

**Not covered by a test, reported.** The call of `drainAdmin` inside `daemon.Run`'s closure, the call sites of `awaitExit`, `poolBuf` and
`cloneTransport`, which ride the existing suites, and which message pair each receipt caller passes (R4, above); the helpers they call are
each tested. The wiring of `appendLine`'s `durable` result
into `Store.Append`, which was reported as an untested gap in the first version, is now tested (E4).

**E4. The chain after a close that fails.** `Store` has an unexported opener (`openAppend`, nil meaning the real file). The new test makes
the close fail after a real, synced write, and asserts that `Append` returns the close error, that `LastHash` is the hash of the line that was
written, and that a second `Append` followed by `Verify` reports an intact chain. The verifier's mutant `if durable && err == nil` survived
the whole package before; with it the new test goes red at the `LastHash` assertion (the chain forks, which is what the comment above `durable`
warns about).

**E6.** Record 0216's PLAN, P4 step 3, now carries a dated pointer note: P5 of this plan added the exit status to the start-up error, and that
step must keep it (and its test no longer "passes unchanged"). An annotation; the step stands.

**E7.** The guide said a receive's value "is used" as if the linters forced it. They force only that the receive is not bare and not `_ =`;
`err := <-ch; _ = err` would pass both. The guide now says it is a choice, and that these sites log.

**E9. A defect that was there before and is not addressed here.** After a failed `fsync` the line may already be in the file (it was written)
while the chain's last-hash cache does not advance, so the next entry names the previous line as its parent and the chain forks. `appendLine`'s
`durable` result and name concern the sync, not whether the bytes are in the file, and the name suggests otherwise. It was true of `Append`
before this phase and is unchanged by it.

**Hidden by the fleet's exclusion, recorded and not pursued.** With the `_test.go` exclusion removed on a scratch copy of `HEAD`, one linux
pass reports 255 bare calls in 93 test files; the verifier confirmed the number and the classes. They are outside the PLAN, which keeps the
fleet's exclusions, and the coordinator decided, under the owner's instruction "we need to limit scope creep", not to pursue them. Class (c)
is empty for that reason.

**What the PLAN did not say.**

* *Two classes it does not discuss:* 9 bare receives from an error channel and 11 single-value type assertions (`check-type-assertions` was
  kept from the fleet). They are 20 of the 319 and are class (a).
* *`errcheck` and `staticcheck` disagree about `_ = <-ch`.* `errcheck` rejects the bare receive; `staticcheck` S1005 rejects the
  explicit blank. Both are on, and no `//nolint` is allowed, so the receive had to be taken into a variable; these sites log it. A plant of
  `_ = <-ch` fails with S1005 (below).
* *Test code.* The fleet's `_test.go` exclusion applies to `errcheck`, so a bare call planted in a test passes (above).

**Seen to fail, on scratch copies** (through `scripts/lint.py`, `make lint`'s own command): a bare `os.Remove(x)` in non-test code fails with
"Error return value of `os.Remove` is not checked (errcheck)"; `_ = os.Remove(x)` passes; a bare `os.Remove(x)` in a `_test.go` passes (the
exclusion); a single-value `v.(string)` fails (errcheck); `_ = <-ch` fails with S1005; a planted finding of two still-disabled linters
(`os.ReadFile` of an unvalidated name, gosec; `err == io.EOF`, errorlint) passes; the clean tree passes. The 37 new and extended test
functions were each seen to fail: 47 breaks of the code they cover, one at a time on a scratch tree, the named test red on each: a close
error dropped, a durable flag inverted or gated on `err == nil`, the exit status removed, an assertion inverted, a panic message changed, the
pool and transport fallbacks removed, a logged error not logged, a clean stop logged, the helper's output changed, and, for R2, the error
dropped again at each of the fourteen data sites, a token put in an error, a line of the healthy output removed, and, for R3 and R4, a
broken chain's error replaced by the write error, the sentinel's wrap removed or added to the wrong failures, and the log branch inverted. The plants were run
after the tests were written, so "red first" is shown by the break going red, not by the order of writing.

**Configuration.** `.golangci.yml` enables `errcheck`; it is removed from the disabled list. `check-blank: false` and
`check-type-assertions: true` are as P1 left them. No `//nolint:errcheck` (the tree has 44 `//nolint` before and after), no
`exclude-functions`, no new path exclusion. Still disabled, each with its phase: `errorlint` and `nilerr` P6, `gosec` P7, and `bodyclose`,
`gocognit`, `gocyclo`, `unparam` and `confusing-naming` P8. `scripts/lint.py` is unchanged.

**The verifier's checker, re-run on the tree as handed back** (fresh `old/` from `HEAD` and `new/` from the working tree, `GOPROXY=off`, darwin,
linux, windows and linux with every opt-in tag): exit 0; the shape sites are 283 (12 explicit discards, 28 deferred closures, 243 helper calls), the first round's 297 less the 14 data sites, and the
comparison by file and line shows exactly those 14 left the set and nothing entered it. The remaining 212 (57 file and function groups) differences (the union over the
four configurations) are each one of: the imports the shapes cause (`cliout` added, `fmt` removed or added), a reason comment, the body of a class (a)
function (the listed ones), the six constructors of R1, the functions of R2 and R3 (`newPairCmd`, `newReceiptsShowCmd`, the two `runSetupService`,
the verify and status commands), the `receipt` store of E4 and R4, or the session manager's `logReceiptAppendFailure` and its two callers (R4);
the `signalBreak` difference is the three-blank discard of `procFreeConsole.Call`, which the verifier had already proved equivalent. Nothing
else.

**Docs.** `docs/guides/standards/go/logging.md` (Errors) states the rule, the allowance, the data-output rule, the S1005 conflict as a choice,
and the checked assertion; `internal/cliout`'s package comment says it is for status lines, lists the data sites and says `choose` stays a status line; `CHANGELOG.md`
(Unreleased, Changed) names the visible changes: the engine's exit status, the logged shutdown warnings, the receipt close and its new log
line, and the non-zero exit when `--print-only`, `receipts show`, `receipts verify`, `auth recovery status` or the pairing output could not
be written. The PLAN's own sentence, "`logging.md` is
unchanged (Q3 A)", is superseded by this.

**Tests and host.** `go test -race -count=1 ./...` on a clean copy of the tree as handed back, through `run-on-host --race`: on the Windows laptop, exit 0, 50 packages `ok`, 0 failures or races; on the Linux host, exit 0, 51 packages `ok`, 0 failures or races (no WSL result is used for a race run). The macOS race run is the one inside `make preflight`. `make ci-windows`, because six `_windows.go` files differ from `HEAD`, ran on the Windows host: exit 0.

**Gates** (start, end, exit status, each recorded before any filter):

| Gate | Start | End | Exit |
| :--- | :--- | :--- | ---: |
| `make lint` (four passes, `errcheck` on) through `run-heavy` | 16:11:56 | 16:13:00 | 0 |
| `make staticcheck` (with the bound script, three GOOS) through `run-heavy` | 16:13:00 | 16:13:37 | 0 |
| `make test-scripts` (55 tests) | 16:13:37 | 16:13:37 | 0 |
| `go vet ./...` | 16:13:37 | 16:13:39 | 0 |
| `make vet-tags` through `run-heavy` | 16:13:39 | 16:14:02 | 0 |
| `go mod tidy -diff` | 16:14:02 | 16:14:03 | 0 |
| `gofmt -l` over `cmd`, `internal`, `scripts`, `test` | 16:14:03 | 16:14:03 | 0 |
| `make pre-add-check` on the 55 changed and new Go files, with the vulnerability check, through `run-heavy` | 16:14:03 | 16:14:13 | 0 |
| `make preflight` through `run-heavy` | 16:14:13 | 16:17:23 | 0 |
| `make check-records` | 16:18:20 | 16:18:22 | 0 |
| `make markdownlint-docs` (offline, from the npx cache) | 16:18:22 | 16:18:25 | 0 |
| the lint plants (`p5test`), through `run-heavy` | 16:17:31 | 16:18:12 | 0 |
| the verifier's checker, four configurations, through `run-heavy` | 16:13:01 | 16:13:20 | 0 |
| `go test -race -count=1 ./...`, Windows laptop, through `run-on-host --race` | 16:11:55 | 16:14:19 | 0 |
| `go test -race -count=1 ./...`, Linux host, through `run-on-host --race` | 16:11:53 | 16:16:53 | 0 |
| `make ci-windows`, Windows host, through `run-on-host` | 16:14:19 | 16:16:00 | 0 |
