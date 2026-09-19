---
status: accepted
date: 2026-09-19
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Config tests read the host's live config; give them an isolated root on every platform

## Context and Problem Statement

`config.Load` and `relay.Load` find their config file in the platform's config
directory when no file is named. Thirteen tests call them that way to exercise
defaults and environment overrides. On a host that has a real mcremote install,
those tests read the operator's live config, and whatever it says leaks into
the assertions. On this Windows laptop that makes `TestLoadDisplayNameUnset`
fail on every run, so `make ci-windows` can never pass here. On Linux the same
tests pass only because the installed configs happen not to set the keys they
check.

MADR 0160 found this (F21) and deferred it to its own record. This is that
record.

### What was measured, not assumed

All on 2026-09-18. The Windows and WSL measurements were taken at `1d358de`;
wonder's checkout was at `293f5bd`.

**How `Load` picks a file.** In `internal/config/load.go:78-111`, an explicit
`LoadOptions.ConfigFile` wins. Otherwise a non-empty `MCREMOTE_CONFIG` is used
(`:86-91`). Otherwise viper searches `basePaths.ConfigDir` for `config.yaml`
(`:103-111`). That directory comes from `appdirs.SystemRoots` (`load.go:26`),
which is XDG on Unix and Known Folders on Windows (MADR 0116 D3).
`relay.Load` has the same shape: `appdirs.SystemRoots` at
`internal/relay/fileconfig.go:212` and `MCRELAY_CONFIG` at `:273`.

**Which tests take the default path.** `git grep` finds 13 calls with no
`ConfigFile`:

* `internal/config/config_test.go`, 10 tests: `TestReceiptsEnvOverride`,
  `TestRequireClientKeyEnvOverride`, `TestProviderEnvOverrides`,
  `TestRoute53MaxRetriesEnvOverride`, `TestLoadDisplayNameFromEnv`,
  `TestLoadDisplayNameUnset`, `TestKiloEnvOverrides`,
  `TestLoadRejectsRetiredOpencodeTransportFromEnv`,
  `TestRemoteMutationPolicyOffAfterLoad`,
  `TestRemoteMutationPolicyEnvIndependence`.
* `internal/config/acp_config_test.go`, 1 test: `TestGrokAuthMethodEnvOverride`.
* `internal/relay/fileconfig_test.go`, 2 tests: `TestLoadHostsEnv`,
  `TestServeFlagsSingleMechanism`.

Only 4 of the 13 isolate at all, and all 4 do it by setting
`XDG_CONFIG_HOME`: the two display-name tests and both relay tests. None
clears `MCREMOTE_CONFIG`, `MCRELAY_CONFIG` or any other `MCREMOTE_*` variable.

**Windows.** Known Folders ignore `XDG_CONFIG_HOME` (`roots_windows.go:38-72`),
so all 13 read the live directory. `%APPDATA%\mcremote\config.yaml` exists
here, written by `setup-service`, and sets `display_name`. `go test
./internal/config/` fails `TestLoadDisplayNameUnset` with
`DisplayName="mac420-laptop"`. It did so on every run at `b3d3355`, `293f5bd`,
`445a1d0` and `7494c4a`. `make ci-windows` therefore always fails its A6 step
on this host.

**Linux (WSL `Ubuntu-24.04`, a clean clone of `1d358de`).**

* With a planted `$HOME/.config/mcremote/config.yaml`
  (`display_name: planted`, `providers.opencode.allow_remote_share: true`) and
  `XDG_CONFIG_HOME` unset, 2 tests fail: `TestRemoteMutationPolicyOffAfterLoad`
  and `TestRemoteMutationPolicyEnvIndependence`.
* With `MCREMOTE_CONFIG` pointing at the same file and a clean `HOME`, 3 fail:
  those two plus `TestLoadDisplayNameUnset`. Its `XDG_CONFIG_HOME` isolation
  does not stop `MCREMOTE_CONFIG`.
* The control run, with nothing planted, is `ok`.

**wonder.** A real Linux install whose `~/.config/mcremote/config.yaml` sets
`display_name: "wonder-lallygag-net"`. `go test ./internal/config/
./internal/relay/` is `ok` there. The two display-name tests are isolated by
`XDG_CONFIG_HOME`, and the other nine read the live file but check keys it does
not set. They pass because of what the file happens not to contain.

**Test seams.** `appdirs` already has one, `knownFolder` in
`roots_windows.go:21-23`. It is unexported, Windows-only, and unreachable from
`config_test`, which is an external test package. `git grep -c 't.Parallel()'`
finds no parallel test in `internal/config` or `internal/relay`, so a swapped
package variable cannot race another test in either package.

### Findings

**F1 — Thirteen tests load the default config path.** Eleven are in
`internal/config` and two in `internal/relay`. MADR 0160 F21 counted ten.

**F2 — On Windows none of them is isolated.** Known Folders ignore
`XDG_CONFIG_HOME`, so the only isolation anyone wrote does nothing there. The
visible symptom is `TestLoadDisplayNameUnset` failing on every run on a host
with a live config. That keeps `make ci-windows` red, which trains people to
ignore it.

**F3 — On Linux and macOS, 9 of the 13 are not isolated either.** The planted
config broke 2 of them. They pass on real hosts only because of what those
configs leave out.

**F4 — The environment leaks as well.** `MCREMOTE_CONFIG` defeats even the
tests that set `XDG_CONFIG_HOME` (measured: 3 failures). Any other
`MCREMOTE_*` variable set on a developer machine overrides the value a test
expects in the same way.

**F5 — No seam reaches these tests.** `appdirs.knownFolder` is package-private
and Windows-only. Environment variables cannot redirect Known Folders.

## Decision Drivers

* A test's result must not depend on the machine it runs on.
* `make ci-windows` must be able to pass on a Windows host that has mcremote
  installed, which is the host it exists for.
* No production behaviour change: no new environment variable, flag or
  exported API that an operator could reach.
* Keep testing the default-path branch of `Load`, which is what several of
  these tests exist to exercise.

## Considered Options

* A — A package-private `systemRoots` variable in `config` and in `relay`,
  swapped by a test-only helper (`export_test.go` for the external `config`
  tests) that also clears the product's environment variables.
* B — Give every test an explicit `ConfigFile` under `t.TempDir()`.
* C — Add an environment override for the roots to `appdirs`, for example
  `MCREMOTE_TEST_CONFIG_HOME`.
* D — Export a test hook from `appdirs`, for example `SetKnownFolderForTest`.

## Decision Outcome

Chosen option: **A**. It isolates every platform the same way, keeps the
default-path branch under test, and adds nothing an operator can reach:
`export_test.go` compiles only into test binaries.

### The decisions

**D1 — A root seam in each loader.** `internal/config/load.go` gains
`var systemRoots = appdirs.SystemRoots`, and `Load` calls `systemRoots(...)`.
`internal/relay/fileconfig.go` gets the same. Production behaviour is unchanged,
because the variable defaults to the function it replaces.

**D2 — One isolation helper per package.**

* `internal/config/export_test.go` (`package config`) exports
  `SetSystemRootsForTest(t testing.TB, fn …)`, which swaps the variable and
  restores it with `t.Cleanup`.
* `internal/config/isolation_test.go` (`package config_test`) defines
  `isolateConfig(t) string`. It points every root at a fresh `t.TempDir()`,
  sets every `MCREMOTE_*` variable present in the environment to `""`, and
  returns the isolated config directory. Viper treats an empty value as unset,
  because `AllowEmptyEnv` is off.
* `internal/relay` does the same inside its own test file with
  `isolateRelay(t) string`. Those tests are internal, so no export is needed.

**D3 — All 13 tests call the helper first.** The four `XDG_CONFIG_HOME`
lines are replaced by it. No assertion changes.

**D4 — A guard test proves the seam is wired.** Write
`display_name: isolated` into the directory `isolateConfig` returns, then call
`Load(LoadOptions{})`. The test expects `DisplayName == "isolated"` and a
`ConfigFile` under that directory. If a later refactor bypasses
`systemRoots`, this test fails instead of every test quietly reading the live
config again. `internal/relay` gets the matching guard.

### Consequences

* Good, because `TestLoadDisplayNameUnset` passes on this host, so
  `make ci-windows` can be fully green here for the first time.
* Good, because a planted `HOME` config and a stray `MCREMOTE_CONFIG` no longer
  change any result, on every platform.
* Good, because the default-path branch of `Load` stays covered, and D4 now
  asserts it directly.
* Neutral, because it adds one unexported variable to each production file.
* Bad, because a new test that calls `Load(LoadOptions{})` without the helper
  reintroduces the leak. D4 cannot catch that. MADR 0160 C8 already bans the
  pattern for its own tests, and Confirmation §4 counts the call sites.

### Confirmation

```bash
# 1. Windows (this host, live config present): the package is green.
go test ./internal/config/ ./internal/relay/ -count=1     # → ok, ok

# 2. Windows gate: fully green for the first time on this host.
make ci-windows                                           # → exit 0

# 3. Linux (WSL): the planted-config experiments no longer fail anything.
#    HOME with a planted config, and MCREMOTE_CONFIG pointing at it
#    → internal/config ok in both runs (was 2 and 3 failures).

# 4. Every default-path Load in these tests is preceded by the helper.
git grep -c 'isolateConfig(t)' -- internal/config      # → at least 11 call sites, plus D4's guard
git grep -c 'isolateRelay(t)' -- internal/relay        # → at least 2, plus D4's guard
git grep -n 'XDG_CONFIG_HOME' -- internal/config internal/relay   # → only in the helpers

# 5. Race and the pre-add rule.
go test -race ./internal/config/ ./internal/relay/        # → ok
make pre-add-check FILES="…"                              # → clean
```

## Pros and Cons of the Options

### A — Package root seam plus test helper (chosen)

* Good, because it is one mechanism on every platform.
* Good, because the default-path branch stays under test.
* Good, because `export_test.go` never reaches a production binary.
* Bad, because the seam is a package variable. That is safe only while these
  packages have no parallel tests, which holds today (0 found).

### B — An explicit temp `ConfigFile` in every test

* Good, because it needs no production change at all. This is the strongest
  argument for it.
* Bad, because it stops testing the default-path branch. An explicit file takes
  the "must be readable" path (`load.go:94-102`), not the search.
  `TestRemoteMutationPolicyOffAfterLoad` is specifically about a load with no
  config file.
* Bad, because it would not clear the environment, so F4 remains.

### C — An environment override for the roots in `appdirs`

* Good, because tests in every package would get it for free.
* Bad, because it is a production behaviour change that an operator, or a
  stray variable, can trigger. It is also a new way for the daemon to read a
  config from somewhere unexpected.

### D — An exported `appdirs` test hook

* Good, because it reuses the existing `knownFolder` seam.
* Bad, because it adds exported, test-only API to a production package. It is
  also Windows-only, so Unix would still need `XDG_CONFIG_HOME`: two
  mechanisms instead of one.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| File selection order | `internal/config/load.go:78-111`; `internal/relay/fileconfig.go:212,273` |
| 13 default-path loads, 4 isolated by XDG only | `git grep` for `Load(…LoadOptions{})` without `ConfigFile` |
| Windows ignores XDG | `internal/appdirs/roots_windows.go:38-72` |
| `TestLoadDisplayNameUnset` fails on this host every run | `go test ./...` at `b3d3355`, `293f5bd`, `445a1d0`, `7494c4a`; `make ci-windows` A6 at `7494c4a` |
| Linux leak: 2 failures (planted HOME), 3 (MCREMOTE_CONFIG), control ok | WSL `Ubuntu-24.04`, clone of `1d358de`, 2026-09-18 |
| wonder passes by luck | `ssh wonder`: live config sets `display_name`; `go test ./internal/config/ ./internal/relay/` ok at `293f5bd` |
| No parallel tests in either package | `git grep -c 't.Parallel()' -- internal/config internal/relay` → no matches |
| Existing seam is private and Windows-only | `internal/appdirs/roots_windows.go:21-23` |
| An empty `MCREMOTE_*` value counts as unset | viper v1.21.0 `viper.go:449` (`ok && (v.allowEmptyEnv \|\| val != "")`); the repo never calls `AllowEmptyEnv` (`git grep`) |

### Related records

* [0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md):
  F21 found this, and its "Deferred" section named this record. Its C8 already
  forbids the pattern in new tests.
* MADR 0116 D3: why Windows resolves roots through Known Folders.
* MADR 0145: the `make ci-windows` gate this unblocks.

### Open questions for the plan

None.

## Observed — execution results (2026-09-19)

D1–D4 landed in `9c66309`. Confirmation §1, §2, §3 and §5 all passed. On this
host that includes `make ci-windows` exiting 0 for the first time, and both WSL
planted-config experiments going from 2 and 3 failures to `ok`.

§4 held for the 13 tests but was worded too strongly for `internal/relay`.
Three other relay tests still set `XDG_CONFIG_HOME`, and none of them reaches
`Load`'s default search (PLAN 0162 execution record, item 1). The guards were
shown to fail when the seam is bypassed.
