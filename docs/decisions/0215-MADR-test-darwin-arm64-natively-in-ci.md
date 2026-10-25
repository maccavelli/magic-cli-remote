---
status: proposed
date: 2026-10-25
decision-makers: Project Owner
consulted: "0120-MADR-retire-the-darwin-amd64-target.md's 2026-08-29 amendment (D11); 0116-MADR-windows-and-linux-arm64-build-targets.md D17, D20 and D20a; 0143-MADR-ci-retries-once-and-records-every-retry.md; this repository's .github/workflows/ci.yml at 9c0c5e2b; gobble-cli's .github/workflows/ci.yml and its 0004-PLAN-go-module-architecture.md, read 2026-10-25"
informed: anyone who changes .github/workflows/ci.yml or the darwin code paths
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Test darwin/arm64 natively in CI with a macOS leg

## Context and Problem Statement

`darwin/arm64` is a Tier 1 target (`README.md`, "Supported platforms"), but no
CI job runs a darwin test binary. The other three published targets each have
one:

* `linux/amd64` runs on `ubuntu-latest`.
* `linux/arm64` and `windows/amd64` run on the `go-native` matrix, under 0116
  D17.
* Windows is only Tier 2, yet it has a native lane.

gobble-cli, by contrast, runs its whole validation on
`ubuntu-24.04`, `macos-15` and `windows-2025`, with the race detector on
Linux and macOS.

The absence is deliberate. `ci.yml` says so in a comment where the job
would be ("No go-macos job, deliberately (MADR 0120 D11)").
[0120](0120-MADR-retire-the-darwin-amd64-target.md)'s amendment of 2026-08-29
measured the case and declined it:

* **67 lines of darwin-gated code** in two files;
* **no darwin-gated test files**, so "nothing is being skipped";
* **four** `runtime.GOOS == "darwin"` branches in non-test code;
* every other test a macOS job ran would be a third copy of a suite already green on two lanes.

It asked anyone reopening the question to "argue from the gated surface, not
from the phrase 'unexercised target'". This record does that.

### The darwin surface, measured again at `9c0c5e2b`

| Measure | 0120 (2026-08-29) | Now |
| :--- | :--- | :--- |
| darwin-gated production files | `procutil/owner_darwin.go`, `procutil/starttoken_darwin.go` (67 lines) | the same two, unchanged |
| darwin-gated **test** files | none | `internal/updateclient/signer_darwin_test.go` (54 lines), added by 0207 |
| Non-test `runtime.GOOS == "darwin"` branches a test cannot steer from another OS | 4 | 5: `appdirs/roots_unix.go:52`, `cli/setup_service.go:215`, `provider/grok/device_auth.go:87`, `updateclient/client.go:183`, `updateclient/managed.go:124` |

The service package's `case "darwin":` arms are not counted. They switch on
`installOS`, which tests set, so the Linux and Windows lanes already run them.

The update path added the two new branches (0207 U3), and they are not a
process-owner lookup. They decide whether a self-update on macOS:

* checks the staged binary's code signature against
  `identifier "com.magiccliremote.<product>"` before the swap
  (`newProbes`, `client.go:183`);
* signs a binary through `/usr/bin/codesign` (`newTransformer`,
  `managed.go:124`).

A regression in either one weakens what a macOS update verifies before it
replaces the running binary. The tests that hold them assert the darwin half
only on darwin:

| Test | Off macOS it checks | On macOS it checks |
| :--- | :--- | :--- |
| `TestSignerSignsUnderTheFixedIdentifier` (`signer_darwin_test.go`) | does not compile in | the signer runs `/usr/bin/codesign` and requires the fixed identifier |
| `TestNewProbesTheStagedBinary` (`client_test.go:222`) | one probe (version) | two probes: the codesign check first, then the version |
| `TestNoTransformWithoutAnIdentityOrOffMacOS` (`managed_test.go:663`) | no signer even with an identity | the macOS half is the signer test above |
| `TestGrokDeviceAuthDarwinSandboxWrap` (`grok/device_auth_test.go:63`) | the unwrapped argv | the `sandbox-exec` profile wrap |
| `TestHandOffBackendCleansUp` (`daemon/daemon_handoff_test.go:88`) | skipped on Linux | the launchd backend's `CleanupHandOffs` |

0120's argument, that nothing is skipped, no longer holds. These five tests
run their macOS assertions on the owner's Mac and nowhere else. 0120's own
"what the measurement does not settle" names that exposure: "macOS
verification is a manual step with no record".

### The race detector

0116 D20 keeps every CI test `CGO_ENABLED=0` and accepts a gap in exchange:
races that show only on non-darwin scheduling. It says `-race` "is used where
it costs nothing: on darwin, under `CGO_ENABLED=0`, which is the per-phase
gate … and the owner's local machine." That gate is local. No CI job runs a
darwin race pass, so a push made without `make race` on a Mac is raced only
on Linux, with cgo, by the `go` job.

### Measured on the owner's Apple Silicon Mac, in a scratch clone of `9c0c5e2b`

This is the leg proposed below, run before proposing it:

| Step | Result |
| :--- | :--- |
| `CGO_ENABLED=0 go build ./...`, `go vet ./...` | exit 0, 4 s each |
| `CGO_ENABLED=0 go test -count=1 ./...` | exit 0, 61 s, 43 packages `ok` |
| `CGO_ENABLED=0 go test -race -count=1 ./...` | exit 0, 82 s, 43 packages `ok` |
| Distinct `--- SKIP` names | 7, all Linux-only or live or helper tests (for example `TestProcParentReadsTheRealParent`, `TestNotifySocketSends/abstract`, `TestLiveReplaceFirstProbeFailure`) |

A first attempt ran the race pass with `CGO_ENABLED=1` set explicitly, and the
identity tests failed. The `make` build inside them refused it: "refusing to
build a release binary with CGO_ENABLED=1 … (MADR 0116 D21)". That was the
probe's mistake, not a defect, and it confirms what D20 says: on darwin the race
pass needs no cgo, and must not set it.

### Cost and risk

* **Cost.** Standard macOS runners are free and unlimited for public
  repositories, with five concurrent macOS jobs (0120 F8). gobble-cli and
  go-selfupdate-lib already run `macos-15`.
* **Risk.** A fourth lane that can redden a release.
  [0119](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md) records what a
  flaky lane cost. Since 0120 was written,
  [0143](0143-MADR-ci-retries-once-and-records-every-retry.md) added a retry-once
  with a committed flake ledger to every `go-native` leg. A new leg in that matrix
  inherits both, so a timing flake is retried and recorded, not lost.

## Decision Drivers

* A test whose platform half runs on no CI lane is a test only the owner's
  machine enforces, and the update path's signature checks are such tests.
* Keep 0116 D20: cgo-free tests on every lane. The darwin race pass is the one
  D20 already names as costing nothing.
* Reuse what the other native legs have, the retry and the flake ledger, rather
  than a second copy of them.
* An image change should be a change in this repository, as the Flutter pin is.

## Considered Options

* **A**: A `darwin/arm64` row in the `go-native` matrix on `macos-15`, plus a cgo-free race step on that leg.
* **B**: A separate `go-macos` job.
* **C**: Restructure CI into gobble-cli's shape: one `validate` matrix that runs `make preflight` on each OS.
* **D**: Keep 0120 D11, and write a macOS acceptance checklist instead.

## Decision Outcome

Chosen option: **"A"**, because it runs the five tests' darwin halves and a
darwin race pass on every push, at no cost, through the matrix that already
retries and records flakes. It changes nothing about the other legs.

* **D1.** `go-native` gains `{ runner: macos-15, label: darwin/arm64, shell: bash }`. The leg inherits the job's
  `CGO_ENABLED: "0"` and `MC_REQUIRE_SYMLINK: "1"`, and the job's Build, Vet and retried Test steps with the
  flake-ledger artifact.
* **D2.** One step runs on that leg only (`if: runner.os == 'macOS'`): `go test -race ./...`, under the job's
  `CGO_ENABLED=0`, with no retry, as the `go` job's race step has none: a race is a defect, not a flake.
* **D3.** The runner is pinned to the `macos-15` image, not `macos-latest`. It is arm64, it is the image
  gobble-cli and go-selfupdate-lib run, and a GitHub image change cannot then change results without a commit
  here. When GitHub schedules `macos-15` for retirement, the bump is a dated amendment to this record.
* **D4.** This record supersedes 0120 D11.
  * The `ci.yml` comment that explains the missing job is replaced by one that cites this record.
  * 0120 gains a dated note under D11 that points here.
  * 0120's measurement stays as written. It was true when made.
* **D5.** Out of scope:
  * a macOS Flutter or iOS build lane, which is its own record if wanted;
  * the release build, which go-selfupdate-lib's reusable workflow already runs, with its own identity jobs per
    platform;
  * which checks branch protection requires. That is the owner's GitHub setting; see Consequences.

### Consequences

* Good, because the update path's macOS signature checks, the grok sandbox
  wrap and the launchd handoff cleanup are tested on every push, not only on
  the owner's Mac.
* Good, because darwin gets the race coverage 0116 D20 describes, in CI,
  with no cgo exception.
* Good, because it is one matrix row and one step. The retry, the ledger and the
  per-leg `fail-fast: false` already exist.
* Neutral, because the leg adds about a minute of tests and a minute and a
  half of race (61 s and 82 s on the owner's Mac), plus setup, to a run whose
  slowest job is longer.
  The macOS runner's queue time is GitHub's, and that is unmeasured here.
* Bad, because a fourth native lane is a fourth chance of a red run unrelated to
  the change. 0143's retry and ledger bound that: a deterministic failure still
  fails, and a flaky one is recorded.
* Bad, because a hosted runner's environment matches neither the owner's Mac nor
  a user's, as 0120 F10 said. This lane does not replace macOS acceptance on
  real hardware for launchd, TCC or permissions. It tests what unit tests can.
* Neutral, because if branch protection lists required checks, the owner adds
  `Go (darwin/arm64)` there. Until then the leg reports, but does not block a
  merge.

### Confirmation

* `actionlint` passes on the edited `ci.yml`, and is seen failing first on a
  planted defect in a copy.
* **The leg is seen failing first** on a planted darwin-only regression, in a scratch clone run on this Mac with the
  leg's commands. The plant is the codesign probe dropped from `newProbes`; `TestNewProbesTheStagedBinary` fails.
  * The same plant compiles for Linux, and that test accepts one probe there.
  * That makes it exactly what the Linux and Windows lanes cannot see.
* The first CI run after the push shows `Go (darwin/arm64)` green, with its race step. The PLAN's execution record
  gives the run's ID and durations.

## Pros and Cons of the Options

### A: a `go-native` row plus a darwin race step

* Good, because it inherits the retry, the flake ledger and the cgo-free env of the existing legs.
* Good, because the race step is the one 0116 D20 already describes.
* Neutral, because the race step needs an `if:`, the only leg-specific step in the matrix.
* Bad, because it is a fourth lane that can go red (Consequences).

### B: a separate `go-macos` job

* Good, because the job reads on its own, without a conditional step.
* Bad, because it copies the Build, Vet, retried Test and ledger steps, which then drift from the matrix's.

### C: gobble-cli's single `validate` matrix

* Good, because the three OSes would run one recipe, `make preflight`, as gobble-cli does.
* Bad, because it rewrites 0116 D17 and D20a's job structure and 0170's `go` job, for a gap that one matrix row
  closes. That is a different decision, and a larger one.

### D: keep 0120 D11 and write a checklist

* Good, because no CI changes.
* Bad, because a checklist records a manual check; it does not make the five tests run anywhere but the owner's
  Mac, and the update path's signature checks are not something to leave to a checklist.

## More Information

* The plan: [0215-PLAN-test-darwin-arm64-natively-in-ci.md](0215-PLAN-test-darwin-arm64-natively-in-ci.md).
* Records this supersedes or builds on:
  * 0120 D11, superseded;
  * 0116 D17 (the native matrix) and D20 (cgo-free tests; darwin races without cgo), both kept;
  * 0143, whose retry and ledger the leg inherits.
* gobble-cli: `.github/workflows/ci.yml` runs `validate` on `ubuntu-24.04`, `macos-15` and `windows-2025`,
  with `make race` on Linux and macOS ("The race detector needs cgo, which the Linux and macOS runners have").
  Magic's darwin race pass needs no cgo (0116 D20, and measured above).
