---
status: completed
date: 2026-09-19
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0162 — Config tests read the host's live config; give them an isolated root on every platform

Implements [0162-MADR-config-tests-read-the-live-config.md](0162-MADR-config-tests-read-the-live-config.md)
decisions D1–D4, closing findings F1–F5.

## Goal

1. On this Windows host, which has a live `%APPDATA%\mcremote\config.yaml`
   that sets `display_name`, `go test ./internal/config/ ./internal/relay/` is
   `ok`.
2. `make ci-windows` exits 0 on this host. This is the first fully green run of
   the gate here.
3. In WSL, the planted-`HOME` and `MCREMOTE_CONFIG` experiments recorded in the
   MADR both give `ok` for `internal/config` (before the fix: 2 and 3
   failures).
4. Each of the 13 listed tests calls the package's isolation helper before
   `Load`. `XDG_CONFIG_HOME` appears in neither package's tests except inside
   the helpers.
5. A guard test in each package fails if `Load` stops going through the seam.
6. No production behaviour changes. Production files gain one unexported
   variable each, and nothing else.

## Scope

### In scope (the only files any phase may touch)

**P1 (one commit):**

* `internal/config/load.go`: the `systemRoots` variable, used at `:26`
* `internal/config/export_test.go`: new, `package config`
* `internal/config/isolation_test.go`: new, `package config_test`; the helper
  and the D4 guard
* `internal/config/config_test.go`: the 10 tests
* `internal/config/acp_config_test.go`: 1 test
* `internal/relay/fileconfig.go`: the `systemRoots` variable, used at `:212`
* `internal/relay/fileconfig_test.go`: the helper, the 2 tests, and the relay
  guard

Every phase may also append to this file's `## Execution record`, and set this
pair's `status` and `date`.

### Out of scope

* **Tests in other packages that call `Load` with an explicit `ConfigFile`**,
  or set `MCREMOTE_CONFIG` to their own temp file (`internal/cli/pair_test.go`).
  They are already hermetic.
* **`appdirs`.** No change. Its private `knownFolder` seam stays as it is (MADR
  option D was rejected).
* **CLI commands (`paths`, `serve`, `pair`) and how they load.** This is a test
  defect, not a product one.
* **The `retired_goose_test.go` header comment.** It says that
  `Load(LoadOptions{})` reads the live config, which stays true for an
  unisolated call.
* **Memory and `AGENTS.md` wording.** Updated after execution if the owner
  wants it, not as part of this plan.

## Stability rule

Baseline at `1d358de` on this host: `go test ./...` gives 40 `ok`, 5 without
test files, and 1 `FAIL` (`TestLoadDisplayNameUnset`). After P1 the expectation
is **no failures at all**. That retires the baseline exception MADR 0160 has
carried since `b3d3355`.

P1 ends with, from the repository root in Git Bash:

```bash
git diff --cached --name-only --diff-filter=AM -z -- '*.go' | xargs -0 -r gofmt -l   # → nothing
go build ./... && go vet ./...                                                          # → exit 0
go test ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'                                         # → nothing
go test -race ./internal/config/ ./internal/relay/                                     # → ok, ok
make pre-add-check FILES="$(git diff --cached --name-only --diff-filter=AM -- '*.go' | tr '\n' ' ')"
make ci-windows                                                                         # → exit 0
```

It also runs the WSL lane (`wsl-linux-test-lane` memory) on the staged tree:
`go test ./internal/config/ ./internal/relay/`, then the two planted-config
experiments.

**Commits:** stage exactly P1's paths, then `git commit --no-edit`. The
global hook writes the message. Check that the hooks path resolves to
`~/.global-git-hooks` first. The pair is committed on its own before P1
(bootstrap exception). `git push` needs an explicit instruction.

## Cross-cutting contracts

* **C1 — No assertion changes.** The 13 tests keep every check. The helper
  call is the only line added to each one, and each `XDG_CONFIG_HOME` line it
  replaces is the only line removed.
* **C2 — No production behaviour change.** Each `systemRoots` variable is
  initialised to the function it replaces. No new flag, environment variable or
  exported symbol exists in a non-test file.
* **C3 — The helper restores everything.** It swaps the roots through
  `t.Cleanup`, and clears the environment through `t.Setenv`, which restores
  automatically. Nothing leaks between tests.
* **C4 — Isolation is proven, not assumed.** The D4 guard reads a file from
  the isolated directory. A seam that is not wired fails that test.

**C1 is the contract most at risk.** The quickest way to make
`TestLoadDisplayNameUnset` pass is to set `MCREMOTE_DISPLAY_NAME` or loosen the
assertion. That fixes the symptom on this host and leaves the leak in place.

## Dependency and delivery order

```text
P1   (Windows host; WSL for the Linux checks)
```

## Implementation Steps

### P1 — Seam, helpers, 13 tests, guards (D1–D4; closes F1–F5)

1. **`internal/config/load.go`.** Just above `Load`, add:

   ```go
   // systemRoots resolves the platform roots Load searches for config.yaml. It
   // is a variable only so tests can point it at a temp dir (MADR 0162 D1).
   var systemRoots = appdirs.SystemRoots
   ```

   Change `:26` to `roots, diags, err := systemRoots(appdirs.ProductMcremote)`.

2. **`internal/config/export_test.go`** (`package config`):

   ```go
   // SetSystemRootsForTest points Load at fn for the rest of t (MADR 0162 D2).
   func SetSystemRootsForTest(t testing.TB, fn func(appdirs.Product) (appdirs.Roots, []appdirs.Diagnostic, error)) {
       t.Helper()
       prev := systemRoots
       systemRoots = fn
       t.Cleanup(func() { systemRoots = prev })
   }
   ```

3. **`internal/config/isolation_test.go`** (`package config_test`):
   * `isolateConfig(t *testing.T) string`:
     * `d := t.TempDir()`.
     * Build `appdirs.Roots{Home: d, ConfigHome: d/config, DataHome: d/data,
       StateHome: d/state, CacheHome: d/cache, RuntimeHome: d/run, Temp: d/tmp,
       Logs: d/logs}`.
     * Call `config.SetSystemRootsForTest(t, …)` with a function that returns
       those roots.
     * For every `os.Environ()` entry whose name has the prefix `MCREMOTE_`,
       call `t.Setenv(name, "")`.
     * Return `filepath.Join(d, "config", "mcremote")`, which is the
       `ConfigDir` that `appdirs.Resolve` produces for these roots. Assert that
       inside the helper by calling `appdirs.Resolve` and comparing, so a
       change in the layout fails loudly.
   * `TestIsolatedLoadReadsOnlyTheIsolatedConfig` (D4):
     * `dir := isolateConfig(t)`.
     * Create `dir`, write `config.yaml` with `display_name: isolated` and mode
       `0o600`.
     * Call `config.Load(config.LoadOptions{})`.
     * Expect `DisplayName == "isolated"`, and `ConfigFile` equal to
       `filepath.Join(dir, "config.yaml")`.
   * `TestIsolatedLoadWithNoFileUsesDefaults`:
     * `isolateConfig(t)`, then `Load`.
     * Expect `ConfigFile == ""` and `DisplayName == ""`.

4. **The 11 config tests.** Make `isolateConfig(t)` the first statement of
   each of the 10 `config_test.go` tests and of
   `TestGrokAuthMethodEnvOverride`. In `TestLoadDisplayNameFromEnv` and
   `TestLoadDisplayNameUnset`, delete `t.Setenv("XDG_CONFIG_HOME",
   t.TempDir())`. The helper must come **before** a test's own `t.Setenv`
   calls: it clears `MCREMOTE_*`, and a later `t.Setenv` must win. Put it first
   and check that ordering in the diff.

5. **`internal/relay/fileconfig.go`.** Add the same kind of `systemRoots`
   variable above `Load`, and use it at `:212`.

6. **`internal/relay/fileconfig_test.go`.**
   * Add `isolateRelay(t *testing.T) string`. It is the same as
     `isolateConfig`, but it assigns `systemRoots` directly (internal test),
     clears the `MCRELAY_` prefix, and returns the relay `ConfigDir`.
   * Make it the first statement of `TestLoadHostsEnv` and
     `TestServeFlagsSingleMechanism`, and delete their `XDG_CONFIG_HOME` lines.
   * Add a guard, `TestIsolatedRelayLoadReadsOnlyTheIsolatedConfig`. It writes
     one host entry to the isolated `config.yaml` and expects `Load` to report
     that host.

**Verification (P1):** the Stability rule, plus:

```bash
git grep -n 'XDG_CONFIG_HOME' -- internal/config internal/relay   # → only inside isolateConfig / isolateRelay, if at all
for f in TestReceiptsEnvOverride TestRequireClientKeyEnvOverride TestProviderEnvOverrides \
  TestRoute53MaxRetriesEnvOverride TestLoadDisplayNameFromEnv TestLoadDisplayNameUnset \
  TestKiloEnvOverrides TestLoadRejectsRetiredOpencodeTransportFromEnv \
  TestRemoteMutationPolicyOffAfterLoad TestRemoteMutationPolicyEnvIndependence \
  TestGrokAuthMethodEnvOverride; do
  git grep -n -A2 "^func $f(" -- internal/config | grep -q 'isolateConfig(t)' || echo "MISSING $f"
done                                                               # → nothing
go test ./internal/config/ -run 'Isolated' -v | grep -E '^--- '    # → 2 PASS
go test ./internal/relay/ -run 'Isolated' -v | grep -E '^--- '     # → 1 PASS
```

Then run the WSL experiments against the staged tree: planted `HOME` config,
and `MCREMOTE_CONFIG` pointing at it. Both must be `ok`.

## Verification (whole plan)

The same as P1. P1 is the only phase.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `go test ./internal/config/ ./internal/relay/` is `ok` on this host, with the live config in place | D1–D3 (§1) |
| A2 | `make ci-windows` exits 0 on this host | D3 (§2) |
| A3 | Both WSL planted-config experiments are `ok` | D2, D3 (§3) |
| A4 | All 13 tests call the helper first; no stray `XDG_CONFIG_HOME` | D3 (§4) |
| A5 | The guard tests pass, and would fail if the seam were bypassed | D4 (§4) |
| A6 | `go test -race` for both packages is `ok`; `pre-add-check` is clean | (§5) |
| A7 | No production change beyond the two unexported variables | D1, C2 |

**A5 is the criterion most likely to be dropped quietly.** Once A1 is green,
the guards look redundant. They are the only thing that would notice a future
`Load` refactor calling `appdirs.SystemRoots` directly again. To show that A5
has teeth, temporarily revert step 1's call-site change and confirm that
`TestIsolatedLoadReadsOnlyTheIsolatedConfig` fails, then restore it. Record the
result.

## Rollout and Rollback

Test-only in effect; nothing ships differently. Rollback is `git revert` of
the P1 commit, which brings back the host-dependent failure.

## Deferred (named, so they are not mistaken for oversights)

* **A lint or vet check that bans `Load(LoadOptions{})` in tests without the
  helper.** Worth adding if the pattern returns. For now D4 guards the seam, and
  MADR 0160 C8 already bans the pattern for its own tests.
* **Retiring the baseline exception in other records.** MADR 0160's execution
  record and the `windows-config-tests-read-live-appdata` memory describe the
  old baseline. They are history, and are updated or annotated after P1 rather
  than rewritten.

## Execution record (2026-09-19)

P1 ran on the Windows host after the owner approved. The pair was accepted
and committed alone in `8930c82`; P1 is `9c66309`, 7 files, +214 −10. The
MADR's Observed section is `b5ffa1e`.

**Results.**

* Windows, this host, with the live `%APPDATA%\mcremote\config.yaml`
  (`display_name: mac420-laptop`) in place:
  * `go test ./...` exits 0 with 41 `ok` (40 before) and **no failures**.
    `TestLoadDisplayNameUnset` passes for the first time on this host.
  * **`make ci-windows` exits 0, "ALL SELECTED CHECKS PASSED"**: A3, A2, A4,
    A5 and A6 all pass. This is the first fully green gate here.
  * `go test -race` for both packages: `ok`.
  * `make pre-add-check`: 7 files clean.
* Linux (WSL, staged tree):
  * With a planted `$HOME` config for both products: `ok`, `ok` (before: 2
    failures).
  * With `MCREMOTE_CONFIG` and `MCRELAY_CONFIG` pointing at the planted
    files: `ok`, `ok` (before: 3 failures).
  * An extra check with a stray `MCREMOTE_DISPLAY_NAME=stray`: `ok`.
  * The full suite gives 41 `ok`, and race is `ok`.
* A5 has teeth. Reverting only the config call site to `appdirs.SystemRoots`
  made all three config guards fail, and `TestIsolatedLoadWithNoFileUsesDefaults`
  named the live `C:\Users\macsm\AppData\Roaming\mcremote\config.yaml`.
  Reverting the relay call site failed
  `TestIsolatedRelayLoadReadsOnlyTheIsolatedConfig`. Both files were restored,
  and the diff was checked before staging.
* C1 held: each of the 11 config tests gained exactly one line, and the only
  lines removed were the 2 `XDG_CONFIG_HOME` lines. The relay tests gained one
  line each and lost their `XDG_CONFIG_HOME` line and its comment.

**What the plan predicted incorrectly.**

1. **Goal 4 overstated the cleanup.** Its "no `XDG_CONFIG_HOME` outside the
   helpers" wording does not hold for `internal/relay`, which still sets it in
   `TestRecomputePaths`, in `cli_test.go`'s `runCLI` cases and in
   `setup_service_refresh_test.go`. None of these reach `Load`'s default
   search:
   * `TestRecomputePaths` computes paths and reads no config.
   * The `cli_test.go` cases pass `--config`, or skip on Windows by design; its
     comment at `:120-131` already explains why.
   * The setup-service test runs only where a Unix service manager exists.

   They were left alone. The census that counted 13 tests was right about
   `Load` calls; the Goal's wording went further than the census.
2. **The relay guard needed a private directory.** A host entry carries a
   secret, and the relay's credential guard refuses such a file in a
   non-private directory. The guard therefore creates the isolated config
   directory with `appdirs.EnsurePrivateDir`, the same way the existing
   `privateFixtureDir` does.
3. **`pre-add-check` failed on a golint warning that was already there.**
   `relay/fileconfig.go` had `validateLimitsConfig`'s doc line stranded above
   `ValidateServeable`, the "comment on exported method" warning `make lint`
   has shown for a while. The repo's pre-add rule bars staging that file with
   golint output, so the line was moved back above `validateLimitsConfig`.
   This was within scope, since the file was already in P1.
4. **My own slips:**
   * The first write of the relay guard produced an unterminated string (a
     shell-quoting accident); `go vet` caught it before anything was staged.
   * A first attempt at this execution record failed on a Python escape. Only
     the MADR's Observed section was committed then (`b5ffa1e`), and this
     record followed separately.

**The baseline exception is retired.** MADR 0160's stability rule excused
`TestLoadDisplayNameUnset` on this host. From `9c66309` on, a failure there is
a real signal.
