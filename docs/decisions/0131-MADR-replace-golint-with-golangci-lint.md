---
status: accepted
date: 2026-11-18
decision-makers: maccavelli
consulted: —
informed: —
---

# Replace golint with golangci-lint, and pay the 85 findings that exposes

## Context and Problem Statement

The Go pre-add gate runs `gofmt`, **`golint`** and `govulncheck`
(`scripts/go-precheck.sh`, the single implementation; `AGENTS.md` "The pre-add
rule"). `golint` is **archived upstream**: `golang/lint` was frozen and
deprecated in 2021, its README points users at other tools, and it receives no
new checks. A gate whose middle third is a dead tool is a gate that decays
quietly — it will never learn about a new class of mistake.

`golangci-lint` is the obvious replacement, and it is already installed here
(2.13.2, `~/go/bin`, built with the pinned go1.26.5). But it is not a
like-for-like swap, for two reasons that decide the shape of this record.

### golangci-lint is a *runner*, so the real question is which linters

It executes a configurable set. "Switch to golangci-lint" is therefore not one
decision but two: adopt the runner, and choose the set. The two candidate sets
behave very differently on this codebase.

### Measured on this repository, 2026-09-02, golangci-lint 2.13.2

**Today's bar:** `golint ./cmd/... ./internal/...` reports **0 findings**. The
gate is green, and every commit in this repository has passed it. Any swap
starts from red, and the gate's rule is absolute — `AGENTS.md`: "Any output
fails."

**golangci-lint default set** (`errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`) — **85 findings**:

| linter | count | character |
|---|---|---|
| `errcheck` | 50 | unchecked error returns; some deliberate, each needs judgement |
| `staticcheck` | 29 | mostly `QF1001` (De Morgan's), `QF1002` (tagged switch), `ST1005` (capitalised error strings); plus `SA9003` empty branch |
| `unused` | 5 | dead code |
| `ineffassign` | 1 | ineffectual assignment |

**`revive` alone** — the tool that actually succeeds golint, and the closest
thing to a like-for-like swap — **50 findings**:

| rule | count |
|---|---|
| `unused-parameter` | 36 |
| `redefines-builtin-id` | 9 |
| `package-comments` | 2 |
| `empty-block` | 2 |
| `time-naming` | 1 |

The comparison is the point: **revive's 50 are almost entirely cosmetic**
(three quarters are unused function parameters), while the default set's 85
contain the only findings with defect-finding value here — dead code, an
ineffectual assignment, an empty branch, and fifty places where an error is
discarded without a recorded reason.

### CI does not lint Go at all

`ci.yml` runs `go vet ./...` in two lanes (`:108`, `:314`) and nothing else.
`golint` appears only in the pre-add gate and in `make lint`. So "change CI to
use golangci-lint" is not a substitution in CI — it is **adding a lint gate
that does not currently exist**, and `go vet` is subsumed by it (`govet` is in
the default set).

## Decision Drivers

* The current gate's style third is a tool that has not changed since 2021 and
  never will again.
* The gate is all-or-nothing by design. Flipping it while 85 findings stand
  would block every commit in the repository, including the commits that fix
  them.
* The findings worth having are the *bug-shaped* ones, and they are in the
  default set, not in golint's successor.
* `errcheck`'s 50 need judgement, not a mechanical fix: some discarded errors
  are deliberate and the honest change is `_ =` plus a reason, which is a
  reviewable edit, not a sweep.
* A linter that upgrades itself can turn a green gate red without a code
  change — the same hazard `.github/dependabot.yml` and 0128 D1 already
  reason about for dependencies.

## Considered Options

* **A — golangci-lint with the default set, fix the 85 first, then flip**
* **B — golangci-lint with `revive` only (like-for-like golint replacement)**
* **C — golangci-lint with default set + `revive`**
* **D — Flip the gate now and baseline the existing 85**

## Decision Outcome

Chosen option: **"A — golangci-lint with the default set, fix the 85 first,
then flip"**, because it buys the only findings on this codebase that describe
defects rather than taste, and it refuses to make the gate meaningful and
red at the same moment.

* **D1 — Adopt the runner, pin the set explicitly.** A committed
  `.golangci.yml` names the enabled linters. The default set is a moving
  target across golangci-lint releases; naming it makes an upgrade that adds a
  linter a visible edit rather than a surprise red gate.
* **D2 — The set is the default five:** `errcheck`, `govet`, `ineffassign`,
  `staticcheck`, `unused`. `revive` is **not** enabled — see D5.
* **D3 — Findings are fixed before the gate enforces them.** The gate stays on
  `golint` until the count reaches zero, then switches in one commit. At no
  point is the repository in a state where the gate fails on `master`.
* **D4 — CI gains a lint job, and `go vet ./...` is retired into it.** `govet`
  is in the set, so keeping a separate `go vet` step would run the same
  analysis twice.
* **D5 — `revive` is deferred, not rejected.** Its 50 findings are dominated by
  `unused-parameter` (36), which on interface implementations and callback
  signatures is frequently the correct shape rather than a defect. Adopting it
  is a separate decision with a separate cost, and bundling it here would
  triple the fix work for the least valuable third of the findings.
* **D6 — The golangci-lint version is pinned** in CI and named in `AGENTS.md`,
  for the reason in the drivers: an unpinned linter is a gate that can go red
  on someone else's release schedule.

### Consequences

* Good, because the gate starts catching dead code, ineffectual assignments,
  empty branches and discarded errors — none of which `golint` ever looked at.
* Good, because `go vet` stops being a separate, weaker CI step.
* Good, because CI gains a lint gate it does not have today, so a finding
  cannot reach `master` through a machine whose per-machine hooks are not
  installed (`AGENTS.md`: "a `git add` typed in a plain terminal is not gated
  by anything").
* Neutral, because `gofmt` and `govulncheck` are untouched; only the middle
  third of the pre-add rule changes.
* Bad, because 85 findings is real work before any benefit lands, and
  `errcheck`'s 50 are the slowest kind — each is a judgement about whether an
  error matters.
* Bad, because `staticcheck`'s `QF*` findings are refactor suggestions, not
  defects, and fixing them touches code for style reasons in a codebase whose
  `gofmt`-not-`gofumpt` choice (`AGENTS.md`) exists precisely to avoid that.
  Mitigated by P2's option to disable individual noisy checks with a recorded
  reason rather than reflowing code to satisfy them.
* Bad, because the tool is heavier than `golint`: a full run takes appreciably
  longer, which the per-file pre-add path must account for (P3).

### Confirmation

1. `golangci-lint run ./...` reports **0 findings** before the gate is switched
   (D3), and the count is recorded per batch in the plan's execution record.
2. `scripts/go-precheck.sh` fails on a file with a deliberately introduced
   finding, and passes once it is removed — verified against a scratch copy,
   never the working tree.
3. CI's lint job fails a pull request carrying a known finding, then passes
   without it.
4. `go test -race ./...` stays clean across every fix batch — the errcheck work
   changes error handling, which is exactly where a regression would hide.

## Pros and Cons of the Options

### A — Default set, fix first, then flip

* Good, because the gate only ever tightens from a green state.
* Good, because the enabled set is the one with defect-finding value here.
* Bad, because the benefit is deferred behind 85 fixes.

### B — `revive` only

* Good, because it is the honest like-for-like replacement for `golint` and
  keeps the gate's scope unchanged (style).
* Good, because 50 findings is less work than 85.
* Bad, because it buys almost nothing: `golint` already reports 0, so this
  spends 50 fixes to arrive at the same class of check the repository already
  passes.
* Bad, because it leaves the dead code, the ineffectual assignment and the
  discarded errors entirely unexamined.

### C — Default set + `revive`

* Good, because it is the strictest option and settles the question in one go.
* Bad, because it is 135 findings, three quarters of the extra being
  `unused-parameter` on signatures that cannot change.

### D — Flip now, baseline the 85

* Good, because the gate starts guarding new code immediately.
* Bad, because a baseline is an assertion that has been switched off for the
  code that most needs it, and this repository's rules name that pattern
  explicitly as a workaround rather than a fix. The 85 would age into
  permanence.

## More Information

* `AGENTS.md`, "The pre-add rule (Go)" — the gate this changes, and the
  "any output fails" rule that makes the sequencing in D3 necessary.
* `scripts/go-precheck.sh` — the single implementation; `make pre-add-check`
  and the per-machine agent hooks in `~/.global-agent-hooks/` all call it, so
  changing it changes every enforcement point at once.
* [0128](0128-MADR-triage-the-0126-and-0127-deferred-items.md) D1 is the
  precedent for D6: tool currency is a deliberate, pinned, recorded act here.
* [0118](0118-PLAN-symlink-dependent-tests-on-unprivileged-windows.md) records
  a gate that could not run because `golint` was absent — the same class of
  fragility a pinned, single-binary runner reduces.

## Update (2026-11-18): the owner has ruled; the tool is golangci-lint v2.15.0, and the tree has changed

Everything above is kept as written on 2026-09-02. This section is an addition. Where it says a statement above is
stale, the statement stays and this section says what is true now.

Provenance: measured by a mainstream-tier agent at high effort, on the coordinator's brief, read-only, from `master` at
`e7de8568` (tree clean), on `git archive HEAD` copies under a scratch folder. Nothing in the repository's source,
tests, CI, Makefile or guides was changed by the measurement. The companion
[0131-PLAN-replace-golint-with-golangci-lint.md](0131-PLAN-replace-golint-with-golangci-lint.md) carries the open
questions, the phases and their prices.

### The owner's rulings, 2026-11-18

Relayed by the coordinator. Quoted as labels only:

* The owner's own words: "standardize on golangci-lint v2.15.0".
* Asked whether that includes moving this repository, which lints with `golint` plus `staticcheck` and has no
  golangci-lint configuration, onto it: "Yes, move it onto golangci-lint v2.15.0 now". As put to the owner, that means
  this record is brought up to date and accepted, the configuration added, the standards guide changed, and whatever
  the tool flags fixed, with the size of that not yet known. The measurement below is that size.

### What is stale in the text above

| Statement above | True on `e7de8568` |
| :--- | :--- |
| "golangci-lint ... already installed here (2.13.2 ... built with the pinned go1.26.5)" | The standard is v2.15.0, built with go1.27.4. The fleet's sibling repositories install it in CI with `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`. 0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md recorded v2.13.2 as the host standard on its date; the owner's ruling moves that one pin. |
| "85 findings" on the default five | The same five linters report 491 on v2.15.0 today (errcheck 437, staticcheck 50, govet 3, ineffassign 1, unused 0). The 2026-09-02 run is not reproducible from the record (tool 2.13.2, a smaller tree), so the difference is not attributed. The fleet configuration reports 1671 (below). |
| "`golint ./cmd/... ./internal/...` reports 0 findings" | Still true: 0 on `e7de8568`. |
| "CI does not lint Go at all. `ci.yml` runs `go vet ./...` in two lanes (`:108`, `:314`) and nothing else." | Stale. `ci.yml` now runs `go vet ./...` at `:133` and `:238`, `make vet-tags` at `:139`, govulncheck at `:130`, and the pinned `staticcheck` for linux, darwin and windows at `:177` (0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md D3, D4). A Go static-analysis gate exists in CI. `golint` is only in the pre-add script and `make lint`. What CI lacks is golangci-lint's class of check. |
| D2: "The set is the default five", and D5: "`revive` is not enabled" | Superseded in part by the fleet standard (below): 21 linters and 2 formatters, `revive` among them with 25 rules, of which `exported`, `package-comments` and `var-naming` are golint's own checks. Not decided by the ruling as such: see "What the ruling decides". |
| D4: "`go vet ./...` is retired into [the lint job]" | The fleet's repositories keep `go vet ./...` as its own CI step, and this repository additionally vets once per opt-in build tag (`make vet-tags`, 0225), which golangci-lint does not do. Not decided by the ruling; the PLAN proposes keeping `go vet`. |
| Mitigation "P2's option to disable individual noisy checks with a recorded reason" | Kept only as a scoped, owner-chosen configuration entry. The fleet rule is that a finding is never silenced with a blanket exclusion or `//nolint` without the owner choosing it. |
| Makefile and script references | `make lint` is at `Makefile:615-616`. The pre-add `golint` step is `scripts/go-precheck.sh:64-78`. `staticcheck` is pinned at `Makefile:628` (v0.8.2) with the ignore bound `scripts/check_staticcheck_ignored.py` (0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md amendment H, D45), neither of which existed on 2026-09-02. |

### The guide rule the ruling supersedes for golangci-lint

`docs/guides/standards/go/README.md:48` says: "Do not substitute `gofumpt` or `golangci-lint`: neither is configured by
this repository." It was added on 2026-07-28 by the commit "docs: add development and architecture standards for Go and
mobile", as a description of what the repository then ran. No MADR decided it: it predates 0131 (2026-09-02), which
proposed the opposite for golangci-lint. The reason for the gofumpt half is recorded in `AGENTS.md` ("plain `gofmt`,
not `gofumpt`": gofumpt reflows unrelated code and buries a real change in noise) and in the comment at the top of
`scripts/go-precheck.sh`. The ruling supersedes the golangci-lint half. **The gofumpt half stays**, and so does
`docs/guides/standards/go/core.md`'s "Let `gofmt` decide layout. Do not introduce a competing formatter rule", which
matters to one fleet setting (below).

### The fleet standard

golangci-lint v2.15.0 is the pin in the fleet's CI (`go-selfupdate-lib`, `go-tui-lib`, `gobble-cli`; `go-llmprovider-sdk`
still installs v2.14.0). Every sibling's `.golangci.yml` has `version: "2"`, `run.tests: true`, `linters.default: none`
with an explicit `enable` list, and `issues.max-issues-per-linter: 0`, `max-same-issues: 0`. **They are not identical.**

* `go-llmprovider-sdk` and `gobble-cli` differ. `go-llmprovider-sdk` has 21 linters (`bodyclose`, `errcheck`,
  `errorlint`, `gocognit`, `goconst`, `gocritic`, `gocyclo`, `gosec`, `govet`, `ineffassign`, `makezero`, `misspell`,
  `nakedret`, `nilerr`, `nolintlint`, `revive`, `staticcheck`, `unconvert`, `unparam`, `unused`, `whitespace`) and
  formatters `gofmt` and `goimports`. Settings: `errcheck` with `check-type-assertions: true` and `check-blank: true`;
  `gocognit` 50 and `gocyclo` 40; `gosec` excluding G104; `gocritic` with the diagnostic, style and performance tags and
  eight checks disabled; `nolintlint` requiring a named linter and refusing an unused directive; `revive` with 25
  rules. Exclusions: tests are excluded from `errcheck`, `gosec`, `unparam`, `revive`, `gocritic` and `goconst`.
  `gobble-cli` drops `nolintlint` and revive's `exported`, `package-comments` and `var-naming`, and adds nine general
  linters (`contextcheck`, `copyloopvar`, `fatcontext`, `intrange`, `modernize`, `noctx`, `perfsprint`,
  `usestdlibvars`, `usetesting`) plus three that encode that repository's own decisions (`depguard`, `forbidigo`,
  `sloglint`).
* The configuration that most sibling repositories share by content (thirteen identical copies, the MCP servers and
  `mcplib`, and a fourteenth that adds one `gosec` exclusion) carries a path-regex exclusion that switches off 12
  linters for any file whose name is `main.go`, `client.go`, `config.go` or one of ten others. That is a blanket
  exclusion by the fleet's own rule, and it is not measured here.
* `go-tui-lib`, `go-selfupdate-lib` and `go-llmprovider-sdk` do not keep a separate `staticcheck`; `gobble-cli` does,
  pinned, with its own copy of this repository's `scripts/check_staticcheck_ignored.py`, and keeps `go vet` beside
  golangci-lint (its pre-add script still runs `golint`). All four keep `go vet ./...` as its own CI step.
  `go-llmprovider-sdk`'s pre-add script replaced `golint` with golangci-lint, citing that `golint` is archived and
  that "a gate weaker than CI is not a gate".

**Measured with** `go-llmprovider-sdk`'s file, unchanged ("base"), because it is the base the others vary, and the one the
coordinator named. A second run adds `gobble-cli`'s nine general linters ("ext"); `depguard`, `forbidigo` and `sloglint`
are not measured, because their rules are that repository's, not a fleet standard.

### What golangci-lint v2.15.0 reports here

Method: `git archive HEAD` into a scratch folder; the binary reports `2.15.0 built with go1.27.4`; `GOPROXY=off` (the
module cache sufficed); `CGO_ENABLED=0`, as the release and the CI native lanes build; `--max-issues-per-linter 0
--max-same-issues 0`; the configuration file placed at the archive root. Each run takes about one minute. Exit status
**1** in every run. One early windows run was killed by the environment (signal 15) and passed on its single retry.

| Run | Findings |
| :--- | ---: |
| base, `GOOS=darwin` (the host) | 1612 |
| base, `GOOS=linux` | 1612 |
| base, `GOOS=windows` | 1630 |
| base, `GOOS=linux` with every opt-in build tag (`live_*`, `debugpprof`) | 1620 |
| **base, union of the four, de-duplicated by file, line, linter and text** | **1671** |
| ext (base plus nine linters), union of darwin, linux, windows | 2277 |
| the five linters of D2 above, `GOOS=darwin` | 491 |

The 1671 sit in **289 files** (211 non-test, 78 test) of 1038 tracked Go files, in `cmd`, `internal`, `scripts` and
`test` (the module's one `go.mod`; no `go.work`; `internal/updateclient/testdata/fixture/go.mod` is test data that
`./...` does not enter; the Dart app under `apps/mobile` has no Go and is not linted). Of the 1671, 825 are `errcheck`
and 379 `goconst`.

By linter (union):

| Linter | Findings | Test files | Linter | Findings | Test files |
| :--- | ---: | ---: | :--- | ---: | ---: |
| `errcheck` | 825 | 0 (excluded) | `unparam` | 16 | 0 (excluded) |
| `goconst` | 379 | 0 (excluded) | `nolintlint` | 13 | 8 |
| `gosec` | 101 | 0 (excluded) | `unconvert` | 7 | 6 |
| `revive` | 80 | 0 (excluded) | `nilerr` | 6 | 0 |
| `bodyclose` | 72 | 66 | `goimports` | 3 | 0 |
| `staticcheck` | 51 | 30 | `govet` | 3 | 3 |
| `errorlint` | 48 | 21 | `gocyclo`, `ineffassign`, `makezero`, `whitespace` | 1 each | 1 (`whitespace`) |
| `gocritic` | 32 | 0 (excluded) | `misspell`, `nakedret`, `unused` | **0** | |
| `gocognit` | 31 | 18 | | | |

By top-level package directory (union; `internal/provider` is split by provider): `internal/cli` 443,
`internal/provider/codex` 284, `internal/relay` 185, `internal/ws` 132, `internal/provider/acpagent` 89,
`internal/provider/kilo` 75, `internal/session` 40, `internal/config` 37, `internal/providerauth` 35,
`internal/provider/httpagent` 34, `internal/procutil` 31, `internal/provider/opencode` 31, `internal/provider/grok` 30,
`internal/fsutil` 23, `scripts` 20, `internal/relayhost` 18, `internal/certs` 17, `internal/receipt` 15,
`internal/admin` 12, `internal/agenterr` 11; 26 further directories carry 10 or fewer each.

The twenty most frequent distinct messages (normalised; the quoted names in a message are replaced):

| Count | Linter | Message |
| ---: | :--- | :--- |
| 733 | errcheck | Error return value of `X` is not checked |
| 351 | goconst | string `X` has N occurrences, make it a constant |
| 92 | errcheck | Error return value is not checked (a multi-assignment or type assertion) |
| 72 | bodyclose | response body must be closed |
| 31 | revive | redefines-builtin-id: redefinition of the built-in function max |
| 30 | gosec | G304: Potential file inclusion via variable |
| 28 | gocognit | cognitive complexity N of func `X` is high (> 50) |
| 28 | goconst | string `X` has N occurrences, but such constant `X` already exists |
| 22 | errorlint | non-wrapping format verb for fmt.Errorf. Use `%w` to format errors |
| 18 | errorlint | comparing with != will fail on wrapped errors. Use errors.Is |
| 13 | gosec | G703: Path traversal via taint analysis |
| 12 | nolintlint | directive `X` is unused for linter "gosec" |
| 10 | gosec | G301: Expect directory permissions to be 0750 or less |
| 10 | revive | confusing-naming: Method `X` differs only by capitalization to method `X` in the same source file |
| 9 | gosec | G115: integer overflow conversion int -> uint32 |
| 7 | gocritic | httpNoBody: http.NoBody should be preferred to the nil request body |
| 7 | revive | confusing-naming: Method `X` differs only by capitalization to function `X` in the same source file |
| 7 | staticcheck | SA1019: `PlanUpdateContent.Markdown` is deprecated: this declaration comes from the unstable ACP schema |
| 7 | unconvert | unnecessary conversion |
| 6 | gocritic | importShadow: shadow of imported package |

(The 825 `errcheck` findings by callee: `fmt.Fprint*` 276, the `Close` family 131, viper `BindEnv` 61,
`protocol.NewEnvelope` 58, `os.Remove` and `RemoveAll` 35, no callee text (a multi-assignment or type assertion) 92,
other 172, among them `json.Unmarshal` 23 and `os.UserHomeDir` 14. About **493 of the 825 are explicit `_ =`
discards** that `check-blank: true` turns into findings; about **332 are bare calls**, of which 257 are `fmt.Fprint*`
to a command's output writer.)

Three representative findings per linter, verbatim (`path:line: message`, then the code line):

```text
errcheck   cmd/mcrelay/main.go:28: Error return value of `os.Stderr.WriteString` is not checked
             _, _ = os.Stderr.WriteString("error: handoff environment: " + err.Error() + "\n")
           internal/cli/service/health.go:93: Error return value is not checked
             kind, _ = ResolveBackend(product)
           cmd/mcrelay/main.go:42: Error return value of `os.Stderr.WriteString` is not checked
             _, _ = os.Stderr.WriteString("error: " + err.Error() + "\n")
goconst     internal/agenterr/agenterr.go:77: string `insufficient_quota` has 3 occurrences, make it a constant
             "insufficient_quota",
           internal/cli/service/result_print.go:82: string `launchd-agent` has 10 occurrences, but such constant `KindLaunchdAgent` already exists
             case "launchd-agent":
           internal/agenterr/agenterr.go:98: string `freeusagelimiterror` has 3 occurrences, make it a constant
             "freeusagelimiterror",
gosec      internal/appdirs/ensure_unix.go:55: G302: Expect file permissions to be 0600 or less
             if err := os.Chmod(dir, 0o700); err != nil {
           internal/certs/certs.go:319: G304: Potential file inclusion via variable
             f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
           internal/cli/service/detect.go:144: G703: Path traversal via taint analysis
             if st, err := os.Stat(rd); err != nil || !st.IsDir() {
revive     cmd/mcrelay/main.go:1: package-comments: should have a package comment
             package main
           internal/agenterr/agenterr.go:363: time-naming: var LongBackoffMin is of type time.Duration; don't use unit-specific suffix "Min"
             const LongBackoffMin = 60 * time.Second
           internal/agenterr/agenterr.go:749: redefines-builtin-id: redefinition of the built-in function max
             const max = 400
bodyclose  internal/provider/codex/auth_p3_test.go:37: response body must be closed
             _, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
           internal/provider/codex/auth_p3_test.go:41: response body must be closed
             conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
           internal/provider/codex/transport.go:136: response body must be closed
             conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
staticcheck internal/auth/token_test.go:52: QF1001: could apply De Morgan's law
             if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
           internal/certs/acme_http_test.go:117: SA9003: empty branch
             if errors.Is(err, context.Canceled) {
           internal/provider/acpagent/plans.go: SA1019 (11 findings), under a file-wide //lint:file-ignore that
             golangci-lint does not read
errorlint  internal/admin/admin.go:248: non-wrapping format verb for fmt.Errorf. Use `%w` to format errors
             return Response{}, fmt.Errorf("%w: %v", ErrDaemonNotRunning, err)
           internal/config/load.go:111: type assertion on error will fail on wrapped errors. Use errors.As to check for specific errors
             if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !isNotExist(err) {
           internal/provider/httpagent/provider.go:859: comparing with == will fail on wrapped errors. Use errors.Is to check for a specific error
             if err == io.EOF {
gocritic   internal/agenterr/agenterr.go:809: regexpSimplify: can re-write `(?i)reset[^0-9]{0,20}\b(1[5-9]\d{8})\b` as `(?i)reset\D{0,20}\b(1[5-9]\d{8})\b`
             reUnix = regexp.MustCompile(`(?i)reset[^0-9]{0,20}\b(1[5-9]\d{8})\b`)
           internal/appdirs/roots_unix.go:79: filepathJoin: "/run/user" contains a path separator
             candidate := filepath.Join("/run/user", fmt.Sprintf("%d", uid))
           internal/cli/service/plist_render.go:144: nestingReduce: invert if cond, replace body with `continue`, move old body after the statement
             if v, ok := env[k]; ok {
gocognit   internal/agenterr/agenterr.go:831: cognitive complexity 51 of func `parseReset` is high (> 50)
             func parseReset(msg string, now time.Time) time.Time {
           internal/cli/engines.go:14: cognitive complexity 56 of func `newEnginesCmd` is high (> 50)
             func newEnginesCmd() *cobra.Command {
           internal/cli/pair.go:61: cognitive complexity 69 of func `newPairCmd` is high (> 50)
             func newPairCmd() *cobra.Command {
unparam    internal/cli/service/supervisortool.go:239: runBounded - result 0 (<module>/service.Output) is never used
             func runBounded(c toolCall) (libservice.Output, error) {
           internal/provider/acpagent/session.go:1769: (*session).deliver - control always receives true
             func (s *session) deliver(ev event.Event, control bool) {
           internal/provider/codex/conn.go:189: (*conn).sendNotification - ctx is unused
             func (c *conn) sendNotification(ctx context.Context, method string, params any) error {
nolintlint internal/certs/certs_test.go:262: directive `//nolint:gosec // pinning is what we're testing` is unused for linter "gosec"
             InsecureSkipVerify: true, //nolint:gosec // pinning is what we're testing
           internal/daemon/tls_alpn_test.go:52: directive `//nolint:gosec // a self-signed test listener; only ALPN is checked` is unused for linter "gosec"
             InsecureSkipVerify: true, //nolint:gosec // a self-signed test listener; only ALPN is checked
           internal/provider/codex/adapter.go:178: directive `//nolint:gosec // bin from provider config` is unused for linter "gosec"
             cmd := procutil.Command(ctx, a.bin, "login", "status") //nolint:gosec // bin from provider config
unconvert  internal/cli/service/setup_test.go:178: unnecessary conversion
             if !strings.Contains(string(res.UnitBody), "--config") {
           internal/provider/codex/conn.go:68: unnecessary conversion
             bounded[key] = json.RawMessage(value)
           internal/provider/codex/provider_test.go:81: unnecessary conversion
             "id": json.RawMessage(request.ID),
nilerr     internal/admin/admin.go:162: error is not nil (line 161) but it returns nil
             return nil
           internal/provider/codex/projects.go:284: error is not nil (line 283) but it returns nil
             return nil
           internal/provider/codex/threads.go:487: error is not nil (line 486) but it returns nil
             return provider.ThreadDeleteResult{Deleted: true, Reconciled: true}, nil
goimports  internal/provider/codex/diagnostics.go:9: File is not properly formatted
             "<module>/internal/procutil"
           internal/provider/codex/managed_daemon.go:8: File is not properly formatted
             "<module>/internal/procutil"
           internal/tailnet/tailnet.go:10: File is not properly formatted
             "<module>/internal/procutil"
govet      internal/provider/acpagent/elicitation_test.go:76: inline: cannot inline: type parameter inference is not yet supported
             Session: &acp.ElicitationSessionScope{SessionId: "a1", ToolCallId: ptr(acp.ToolCallId("call-7"))},
           (three findings, all the `inline` analyzer in one test file; `go vet ./...` reports none)
gocyclo    internal/ws/server.go:734: cyclomatic complexity 59 of func `(*Server).handleMessage` is high (> 40)
             func (s *Server) handleMessage(ctx context.Context, c *client, data []byte) error {
ineffassign internal/cli/receipts.go:139: ineffectual assignment to chain
             chain := "-"
makezero   internal/receipt/store.go:211: append to slice `chunk` with non-zero initialized length
             tail = append(chunk, tail...)
whitespace internal/provider/codex/live_device_auth_test.go:160: unnecessary trailing newline
             }
```

(`govet` is reported by golangci-lint's `govet` and not by `go vet ./...`; `gocyclo`, `ineffassign`, `makezero` and
`whitespace` have fewer than three findings, so fewer are shown.)

The nine added linters of "ext" report 616 more than the base does over the same three runs (1661), none of it in the
base: `perfsprint` 339, `contextcheck` 130, `noctx` 97
(91 in test files), `modernize` 31, `usetesting` 8, `intrange` 6, `usestdlibvars` 5, `copyloopvar` 0, `fatcontext` 0.

### What is already clean, with the current tools

On the same archive: `golint ./cmd/... ./internal/...` **0** (exit 0). `go vet ./...` **0**. `make vet-tags`' script,
every opt-in tag, **ok**. The repository's pinned `staticcheck` v0.8.2 (2026.2.2), taken from the repository's own
`bin/tools` build, **0** on `GOOS=linux`, `darwin` and `windows`, and `scripts/check_staticcheck_ignored.py` reports 56
ignored findings on each, every one an `acp-go-sdk2` unstable-schema name. The host's `staticcheck` is 2026.2.1 (v0.8.1),
which `Makefile:625-627` records cannot decode this toolchain's export data; it was not used. Nothing was installed.

### Classification

* **Mechanical, safe to fix by rote (about 140):** `goimports` 3 (only if a formatter is wanted: see the PLAN's Q4),
  `ineffassign` 1, `whitespace` 1, `unconvert` 7, `nolintlint` 13 (delete a directive that suppresses nothing; the run
  must be repeated to prove it), `staticcheck` QF1001/QF1002/QF1003/QF1008/QF1011 15, `revive` `redefines-builtin-id`
  36 (rename a local named `max`, `min`, `cap` or `real`), `if-return` 1, `empty-block` 1, `package-comments` 4,
  `gocritic` 32 (mostly `httpNoBody`, `importShadow`, `emptyStringTest`), `goconst` where the constant already exists
  28, `makezero` 1.
* **Mechanical but wide, and of low defect value:** `goconst` 351 new constants across 113 files. Strings that are wire
  values and `switch` labels. It is the fleet's setting; the cost is review, not risk.
* **Needs judgement at each site:**
  * `errcheck` 825 (see the next section: 493 explicit discards and 332 bare calls);
  * `gosec` 101 (G304 30 and G703 13 are path findings on paths the daemon derives itself; G301/G302/G306 22 flag modes
    such as `0o755` for service units and `0o644` for a supervisor `down` file that are wanted, and cannot be tightened
    by rote; G115 17 integer conversions; G118 6 goroutines; G101 6 are false positives on wire-protocol names such as
    `credential_failed`);
  * `errorlint` 48 (the `%v` to `%w` change makes the wrapped error visible to `errors.Is` and `errors.As`, which can
    change a caller's branch);
  * `nilerr` 6 (a deliberately swallowed error; each needs a stated reason or a change);
  * `bodyclose` 72, of which 66 are tests; the six in production are `websocket.Dial` calls whose response body the
    library owns, so they are probably false positives, and each needs confirming;
  * `gocognit` 31 and `gocyclo` 1 (thirteen production functions over 50; `newPairCmd` is 69 and `handleMessage` is
    59: a refactor or an owner-chosen exception);
  * `unparam` 16 (a signature change; all in internal packages);
  * `revive` `confusing-naming` 35 (the repository's idiom of an exported function over an unexported implementation,
    `Close` over `close`, `Resolve` over `resolve`; the fix renames the unexported side);
  * `staticcheck` `SA1019` 35 (below), `SA9003` 1.
* **Conflicts with this repository's standards guide, or false positives:**
  * `errcheck` with `check-blank: true` against `docs/guides/standards/go/logging.md` "Handle errors explicitly.
    Ignoring an error is acceptable only for a clearly best-effort cleanup/write where failure cannot affect
    correctness and the reason is evident in nearby code": the guide allows what the setting reports, 493 times.
  * `errcheck` on `fmt.Fprint*` to a command's output writer (257 bare calls) against the same file's "`fmt` and Cobra
    output are appropriate for user-facing command responses".
  * The `goimports` formatter against `core.md`'s "Let `gofmt` decide layout. Do not introduce a competing formatter
    rule" and `AGENTS.md`'s plain-`gofmt` rule (3 findings).
  * `SA1019` on the unstable-schema names of `acp-go-sdk2` against 0207's amendment H D45 bound (below).
  * `gosec` G101 (6) and `bodyclose` on `websocket.Dial` (6 in production): false positives.

### Three findings the numbers hide

1. **golangci-lint does not read `//lint:ignore` or `//lint:file-ignore`.** Six files carry those directives, under the
   0207 amendment H bound, and golangci-lint reports their 35 `SA1019` findings anyway (`plans.go` 11, `plans_test.go`
   17, `session.go` 4, and one each in `acpagent.go`, `advertise_test.go`, `turnusage.go`). `gobble-cli` pairs each
   `//lint:ignore SA1019 ...` with a `//nolint:staticcheck // as the lint:ignore above`. Either that, a scoped
   configuration entry, or leaving golangci-lint's `staticcheck` off is an owner choice; the PLAN asks it.
2. **The 493 explicit `_ =` discards are this repository's own convention**, including 0131's own text above ("`_ =` plus
   a reason"). Under `check-blank: true` none of them passes. This is the largest single decision in the adoption.
3. **Platform files hide findings from a host-only run.** Windows reports a net 18 findings more than the host, and a tagged
   run 8 more. The fleet siblings lint linux, darwin and windows; so must this repository, plus one pass with every
   opt-in tag, or CI will be green while a `_windows.go` file is not.

### What changes with it

The Makefile (`lint` at `:615-616`, the `pre-add-check` comment at `:658`, `preflight` at `:443`), CI
(`.github/workflows/ci.yml`, the `go` job), `scripts/go-precheck.sh`, `AGENTS.md:19` and `:30`, `README.md:1573`,
`:1586` and `:1634`, `docs/guides/standards/go/README.md:47-49` and `testing.md:27` and `:38`, and 0169's pin. The
PLAN lists each.

### What the ruling decides, and what it does not

Decided by the owner's two rulings:

* golangci-lint v2.15.0 is the runner here, pinned (D1, D6 as amended: the pin is v2.15.0, installed in CI by
  `go install ...@v2.15.0` as the fleet does).
* The repository moves onto it now, a `.golangci.yml` is committed, the standards guide's golangci-lint half is changed,
  and what the tool flags is fixed.
* CI gains a golangci-lint gate (D4, first half) and the pre-add gate runs golangci-lint (D3's end state).

Not decided by the rulings of the same date, and left as the PLAN's open questions for the owner (answered later the same day: see the next subsection): whether this is one change or phased, and how CI stays
honest between phases (D3 "the gate stays on `golint` until the count reaches zero" is the original answer, and the
fleet rule against a blanket exclusion still binds); whether `golint` and the repository's own pinned `staticcheck` are
removed or kept (D4's retirement of `go vet` is likewise not adopted by the ruling); the three places where the fleet
configuration conflicts with this repository's guide; and how the 35 `SA1019` findings are handled. D2 (the default
five) and D5 (`revive` deferred) are superseded by the fleet standard as the configuration to measure and to adopt, and
**D2's and D5's replacement is marked not decided** as to any local difference the owner chooses.

### The owner's answers to the five open questions, 2026-11-18

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

What this settles of the items the update above left "not decided": the phasing and how CI stays honest (Q1); that
`golint` goes and the pinned `staticcheck`, its ignore bound, `go vet` and `vet-tags` stay (Q2, which also keeps D4's
`go vet` in place, so D4's retirement of `go vet` is not adopted); one local difference from the fleet file,
`errcheck.check-blank: false`, because the guide allows an explicit, reasoned discard (Q3); the `goimports` formatter as
the fleet has it (Q4); and the form of the `SA1019` ignores (Q5). Any other difference from the fleet file is still the
owner's to choose, and none is chosen.

### Decision Outcome, as of 2026-11-18

Chosen option: **A's sequencing and the fleet's linter set**, because the owner ruled to standardize on golangci-lint
v2.15.0 and to move this repository onto it now. Of the four options above, the ruling is closest to **A** (adopt, fix
first, never leave `master` red), with **C**'s `revive` included because the fleet configuration includes it, and it is
not **D** (a baseline), which the owner has not chosen. The status is `accepted` as of this date.

### Confirmation, added

1. `golangci-lint run -c .golangci.yml ./...` reports **0** on `GOOS=linux`, `darwin` and `windows`, and on linux with
   every opt-in build tag, at the pinned v2.15.0, before the plan is complete.
2. The final `.golangci.yml` equals the fleet reference file apart from the local differences the owner chose, each
   recorded with its reason in the file and in the PLAN.
3. `scripts/go-precheck.sh` is seen to fail on a planted finding in a scratch copy, and to pass without it, in every
   phase that changes it.
4. `master` is green in CI at every commit of the plan, not only at the end.
