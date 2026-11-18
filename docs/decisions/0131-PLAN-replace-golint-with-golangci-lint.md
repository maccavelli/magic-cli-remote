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
