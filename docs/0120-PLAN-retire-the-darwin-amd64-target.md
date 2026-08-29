---
status: in-progress
date: 2026-08-28
associated-madr: "0120-MADR-retire-the-darwin-amd64-target.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0120 — Retire darwin/amd64 and publish exactly four targets

Implements [0120-MADR-retire-the-darwin-amd64-target.md](0120-MADR-retire-the-darwin-amd64-target.md)
decisions D1–D10, closing findings F1–F9.

## Goal

The project builds and publishes exactly four targets, every file that encodes
that list says the same thing, and an Intel Mac is told why it is not one of
them.

Finish line:

* `PLATFORMS` in `ci.yml` names four targets; `verify-build-metadata.sh` builds
  and asserts the same four;
* `scripts/install.sh` rejects Darwin/x86\_64 before any download, with a reason;
* `README.md` and `docs/ops-linux-install.md` describe four supported targets
  and one retired one, and `README.md:1409` is no longer stale;
* `docs/0059-MADR-*` carries an additive amendment narrowing its D10;
* no production Go file is changed.

## Scope

### In scope (the only files any phase may touch)

* `.github/workflows/ci.yml` — the `PLATFORMS` list, and the macOS-CI comment
  at `:322-325` (D10). No job is added, removed, or reordered.
* `scripts/verify-build-metadata.sh` — the `darwin amd64` build and its
  assertion loop
* `scripts/install.sh` — the D4 rejection
* `scripts/install_test.sh` — the inverted `darwin-amd64` fixture
* `README.md` — the platform table and the CI-job sentence at `:1409`
* `docs/ops-linux-install.md` — the macOS arch sentence at `:66`
* `docs/0059-MADR-native-paths-and-linux-macos-parity.md` — a new
  `## Amendment` section, appended; nothing above it edited
* `docs/0120-*` — this pair's own status and execution record

### Out of scope

* **Any production Go code.** `internal/update/` resolves its asset from
  `runtime.GOOS`/`GOARCH` (MADR `run.go:67`) and needs no change to stop
  offering a target that is no longer published. If a phase finds itself
  editing a non-test `.go` file, it has left scope.
* **Deleting or rewriting published release assets** (D5). `v0.14.10` and
  earlier keep their `darwin/amd64` binaries. No `gh release delete-asset`, no
  re-cut tags.
* **Wiring `install_test.sh` into CI or `make preflight`** (D9, F6). Real, and
  deferred with a reason below.
* **Adding a macOS runner job.** Option B. Note the MADR's correction: such a
  job would be *free* (F8) and *native* (F9), so it is not excluded on cost or
  capability — it is excluded because adding a release-gating lane is a
  different decision from retiring a target (D10). P3 fixes the comment that
  says otherwise; it does not add the job.
* **`windows/arm64`** — still excluded, still 0116 D19's call.
* **The red `Go (linux/arm64)` lane** — that is
  [0119](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md). This plan's
  phases must not be used to fix it, and a phase that lands while the lane is
  red is verified by the checks below, not by "CI is green".

## Stability rule

Every phase ends with:

```bash
bash -n scripts/install.sh scripts/install_test.sh scripts/verify-build-metadata.sh
./scripts/verify-build-metadata.sh          # exit 0
go build ./...                              # unaffected, proves it stayed unaffected
```

then **one commit** (`git commit --no-edit`; never `-m`).

`gofmt`/`go test` are not phase gates here: no Go file is in scope, and
`go build ./...` is present precisely to prove that.

**Local `gofmt -l` is not usable on this host.** The working tree is CRLF, so
`gofmt -l cmd internal` reports ~500 files. This is the pre-existing issue 0118
defers; it is not evidence of drift, and no phase may "fix" it.

**`git push` is not required by this plan and must not be assumed.** P1–P5 are
verifiable locally. P6 needs a push and a version tag and therefore an explicit
instruction in the same turn (AGENTS.md); without one it stops and says so
rather than claiming a result it did not observe.

## Cross-cutting contracts

**C1 — No production Go code.** Docs, CI config, shell scripts and shell tests
only.

**C2 — No check is retired along with the target.** D8/F7. After P1,
`verify-build-metadata.sh` must still fail if a Darwin binary acquires `netgo`
or `osusergo`, and must still fail if any target's tags drift. Deleting the
`darwin amd64` build line without checking what the `:57` loop iterates over
would silently take the Darwin arm of that assertion with it.

**C3 — The four encodings change together, in one commit each phase.** D3. A
tree where `ci.yml` says four and `README.md` says five is worse than either
state alone, because it makes the drift look intentional.

**C4 — The removal explains itself.** D4/D7. No phase may leave the Intel-Mac
path as a bare 404 or a bare "not found". The message names the target, gives
the reason, and does not suggest Rosetta.

**C5 — 0059 is amended, never edited.** D2. Append an `## Amendment` section.
No line above it changes, including its D10.

**C2 is the one at risk.** Shrinking a list is a deletion, deletions are
tempting to do quickly, and the assertion loop at `verify-build-metadata.sh:57`
takes its inputs from variables set by the very lines being deleted. The
failure mode is silent: the script still exits 0, having checked less.

## Dependency and delivery order

P1 → P2 → P3 → P4 are independent in content but ordered so the tree is never
self-contradictory (C3): the build matrix shrinks first, then the thing a user
hits (the installer), then the prose, then the historical record. P5 is a
whole-tree audit that only makes sense once P1–P4 have landed. P6 is
observation, not change, and is gated on a real tag.

## Implementation Steps

### P1 — Shrink the build matrix (D1, D8; closes F1, partially F3)

`.github/workflows/ci.yml` and `scripts/verify-build-metadata.sh`.

Remove `darwin/amd64` from `PLATFORMS` (`ci.yml:184`). Remove the
`build_one darwin amd64` line (`verify-build-metadata.sh:23`) **and** its entry
in the `:57` assertion loop — then read the resulting loop and confirm
`$tmpdir/mcremote-darwin` (the arm64 build) is still in it. C2 lives here.

Leave the `ci.yml:186-188` comment about `windows/arm64` alone; add a
neighbouring comment recording that `darwin/amd64` was retired by this record,
so the next person to read `PLATFORMS` learns it was a decision rather than
guessing it was an omission.

**Verification:**

```bash
grep -c 'darwin/amd64' .github/workflows/ci.yml         # → 0
grep -c 'darwin amd64' scripts/verify-build-metadata.sh # → 0
./scripts/verify-build-metadata.sh                      # → exit 0
```

and, proving C2 rather than assuming it, a deliberate break:

```bash
# temporarily add osusergo to the darwin/arm64 build, re-run, expect FAILURE
./scripts/verify-build-metadata.sh   # → non-zero, names the darwin binary
# revert the deliberate break before committing
```

A phase that cannot make the script fail on demand has not verified C2.

### P2 — Tell an Intel Mac why (D4, D7; closes F4, F5)

`scripts/install.sh` and `scripts/install_test.sh`.

Add a `darwin` + `amd64` rejection modelled on the existing `:88` case — before
any network call, with the same `die` shape and exit code as the other
unsupported-architecture paths. The message names the target, says it is not
published as of this record, points at the last release that carried it, and
states plainly that Rosetta does not help (C4, D7).

MADR open question 2 must be answered here, not deferred: the rejection is
`uname`-driven and therefore static, which is wrong for someone pinning an old
release that *did* publish the target. Either accept that (and say so in the
message and in this plan's execution record) or drive the rejection from the
`SHA256SUMS` the installer already downloads. **Recommendation: static.** The
SHA256SUMS-driven version is more accurate and more code, and the accurate
answer for a pinned old release is a niche the message can cover in a sentence.

Then invert `install_test.sh:139-144`: `"Darwin x86_64 accepted (exit 0)"`
becomes a rejection assertion checking both the exit code and that the message
names a reason — not merely that it is non-zero (C4).

**Verification:**

```bash
bash -n scripts/install.sh scripts/install_test.sh
./scripts/install_test.sh        # → exit 0
```

Note F6: this script runs nowhere but here. Running it by hand in this phase is
the only gate it has, so the phase does not skip it on the grounds that it is
slow or that Git Bash on this host is awkward. If it cannot run here, the phase
says so explicitly rather than committing an unverified inversion.

### P3 — Make the prose say four, and stop saying macOS CI costs money (D3, D6, D7, D10; closes F3, F8)

`README.md`, `docs/ops-linux-install.md`, and the comment at
`.github/workflows/ci.yml:322-325`.

* Platform table (`README.md:160`): `darwin/amd64` keeps its row, tier becomes
  `—`, note says "retired, see MADR 0120" — the `windows/arm64` pattern (D6).
  Do not delete the row.
* `README.md:1409`: currently names three of the five actual targets. Rewrite to
  the four. This is F3's pre-existing drift; fixing it is in scope precisely
  because leaving it would make the sentence freshly wrong.
* `docs/ops-linux-install.md:66`: drop `darwin/amd64` from the macOS arch list.
* Wherever the retirement is described, D7 applies: no Rosetta claim.
* `ci.yml:322-325` (D10): replace *"Re-enable a go-macos job when ready to pay
  for hosted runners again"* with the truth — standard macOS runners are free
  and unlimited on public repositories, the cap is 5 concurrent macOS jobs, and
  `darwin/arm64` is currently covered by owner acceptance testing rather than
  CI. **Comment text only. No job is added** — that is the deferred record.

**Verification:**

```bash
grep -n 'darwin/amd64' README.md docs/ops-linux-install.md
# → only the retired-row line in README, with a reason on it
grep -n 'pay for hosted runners' .github/workflows/ci.yml
# → 0 hits
git diff --stat .github/workflows/ci.yml   # → comment lines only, no job keys
```

Read `README.md:1409` aloud against `ci.yml`'s `PLATFORMS` and confirm they name
the same four. This is a human check; there is no assertion for prose.

### P4 — Amend 0059 (D2; closes nothing, discharges C5)

`docs/0059-MADR-native-paths-and-linux-macos-parity.md`.

Append `## Amendment — 2026-08-28: darwin/amd64 retired`. Two or three
sentences: D10's release clause is narrowed to `darwin/arm64` by 0120, the
reason is F2 (no acceptance host, no CI execution), and the rest of D10 —
native CI as a release gate, signing, notarization, the stated minimum macOS
version — is unaffected. Nothing above the new heading changes (C5).

**Verification:**

```bash
git diff docs/0059-MADR-native-paths-and-linux-macos-parity.md
# → additions only, all below the new '## Amendment' heading
```

### P5 — Audit for missed encodings (D3; closes F3 fully)

No file changes expected. MADR open question 3 was closed by inspection
(`SHA256SUMS` is glob-generated; no count assertions exist), but that was
measured before P1–P4 landed. Re-run the sweep over the whole tree:

```bash
grep -rn 'darwin.amd64\|darwin_amd64' \
  --include='*.go' --include='*.sh' --include='*.ps1' --include='*.yml' \
  --include='*.yaml' --include='*.md' --include='*.dart' --include='Makefile' . \
  | grep -v '^./docs/0[0-9][0-9][0-9]-'
```

Every surviving hit is either the README's retired row, this pair, or a bug.
Numbered docs are excluded because historical records legitimately mention the
target.

**Verification:** the command's output, pasted into the execution record, with
a one-line justification for each hit. An empty justification column is a
failed phase.

### P6 — Observe it on a real tag (D1, D5; closes F1)

**Needs an explicit push-and-tag instruction in the same turn. Without one this
phase does not run, and the plan closes at P5 with P6 marked pending.**

On the next version tag, confirm from the release:

```text
assets contain no mcremote-darwin-amd64* and no mcrelay-darwin-amd64*
SHA256SUMS has 8 lines (2 binaries x 4 targets)
v0.14.10's assets are untouched                (D5)
```

This is the only step that observes the change end-to-end, because the build
matrix runs only on `github.ref_type == 'tag'` (`ci.yml:169`). Nothing before
P6 proves the release actually omits the target — P1's verification proves the
list changed, which is not the same claim.

## Verification (whole plan)

```bash
grep -c 'darwin/amd64' .github/workflows/ci.yml            # → 0
grep -c 'darwin amd64' scripts/verify-build-metadata.sh    # → 0
./scripts/verify-build-metadata.sh                         # → exit 0
./scripts/install_test.sh                                  # → exit 0
bash -n scripts/install.sh
go build ./...                                             # → exit 0 (C1)
git diff --stat -- '*.go' | grep -v _test                  # → empty (C1)
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `PLATFORMS` and `verify-build-metadata.sh` build exactly four targets | D1, D3 |
| A2 | `verify-build-metadata.sh` still fails on a deliberately mis-tagged Darwin binary | D8, C2 |
| A3 | `install.sh` rejects Darwin/x86\_64 pre-download with a stated reason | D4, C4 |
| A4 | No text anywhere implies Rosetta is a fallback | D7, F4 |
| A5 | `install_test.sh` asserts the rejection and its reason, and was actually run | D4, F6 |
| A6 | README table keeps a retired `darwin/amd64` row with a reason | D6 |
| A7 | `README.md:1409` names all four targets | D3, F3 |
| A8 | 0059 amended additively, nothing above the heading edited | D2, C5 |
| A9 | No production Go file changed | C1 |
| A10 | Published releases untouched | D5 |
| A11 | `ci.yml`'s macOS comment states the free-runner fact; no job added | D10, F8 |

**A2 is the one to guard.** It is the only criterion whose failure is invisible:
the script keeps exiting 0 while checking one fewer thing, and every other
criterion would still pass. It is also the one most likely to be dropped,
because proving it requires deliberately breaking the build and reverting —
work that produces no diff and therefore looks like it can be skipped.

A5 is second. `install_test.sh` runs in no gate (F6), so "it should pass" will
be very tempting to substitute for running it.

## Rollout and Rollback

No runtime behaviour changes; nothing a running daemon observes. Each phase
reverts independently with `git revert`.

The user-visible effect begins at the first tag after P1 and cannot be rolled
back from the client side — an Intel Mac that has already failed an update sees
the failure again until a release carrying the target is published. Rollback is
therefore "restore the `PLATFORMS` line and cut a tag", not "revert and done".
D5 limits the blast radius: every previously published release still works.

## Deferred (named, so they are not mistaken for oversights)

* **Wiring `install_test.sh` into `make preflight` and CI** (F6, D9). The
  installer's platform-matrix assertions are enforced by nothing, which is why
  this plan runs the script by hand. Adding a gate is a CI-policy change that
  wants its own record, and doing it inside a target-retirement plan would mean
  a phase that reddens CI for reasons unrelated to the target.
* **A free `macos-15` job for `darwin/arm64`** (MADR open question 4, F8).
  This is the most valuable thing this record turned up and it is deliberately
  not done here. Once `darwin/amd64` is gone, `darwin/arm64` is the only
  published target with no automated execution anywhere — covered solely by the
  owner's laptop — and the reason the project declined that coverage (cost) is
  false for a public repository. It is deferred rather than folded in because
  adding a lane that can block a release is a different decision from removing
  a target, and because the owner already acceptance-tests this platform by
  hand, so the marginal value needs arguing rather than assuming. It should be
  the next record after this one.
* **A release-note line for the retirement** (MADR open question 1). The README
  row is the minimum; whether the first omitting tag also needs release-note
  prose depends on whether any Intel Mac installs actually exist, which nobody
  has measured.
* **The CRLF working tree.** Still 0118's deferral, still unrelated, still
  making local `gofmt -l` useless.
* **The red `Go (linux/arm64)` lane.** [0119](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md).
  This plan neither fixes nor depends on it, and P6's tag run will still show
  that lane red unless 0119 lands first.

## Execution record — 2026-08-28

**P1–P5 complete. P6 pending.** P6 requires a pushed version tag, while the
owner explicitly prohibited pushing in this execution turn. The plan remains
`in-progress`; no release, tag, remote branch, or existing release asset was
changed.

The MADR was accepted and the plan started in `3e8ba6e`. Completed phase
commits preceding this execution-record commit are:

* P1 `cd398e0` — removed the Intel Darwin release/build-metadata target;
* P2 `b2c674a` — added the static, pre-download Intel-Mac rejection and tests;
* P3 `a1bfcd8` — reconciled platform prose and corrected the macOS CI comment;
* P4 `e5c2725` — appended the additive 0059 amendment.

P2 took the plan's recommended static branch. That means the installer rejects
an Intel Mac even if its caller pins v0.14.10; the error says v0.14.10 was the
last published version and directs the user to install it manually. This keeps
the rejection before every network call and avoids adding a second
manifest-driven platform-selection path.

### P5 encoding audit

The prescribed sweep returned exactly these hits:

```text
./docs/ops-linux-install.md:68:[MADR 0120](0120-MADR-retire-the-darwin-amd64-target.md). The service there is
./README.md:160:| `darwin/amd64` | — | **retired** after v0.14.10; see [MADR 0120](docs/0120-MADR-retire-the-darwin-amd64-target.md) |
./scripts/install.sh:94:        die 1 "darwin/amd64 is retired and is not published after v0.14.10 (MADR 0120).
./scripts/install.sh:96:v0.14.10 was the last release to carry darwin/amd64; install it manually if needed."
./scripts/install_test.sh:139:S="$WORK/stub-darwin-amd64"; mk_stubs "$S" x86_64
./scripts/install_test.sh:142:run_installer "$S" - "$WORK/bin-darwin-amd64" --dry-run --verbose
./scripts/install_test.sh:144:contains "  Darwin x86_64 message names retired target" "$OUT" "darwin/amd64 is retired"
```

Every hit is intentional:

* `docs/ops-linux-install.md:68` is the decision link; the audit's `.` wildcard
  matches the hyphen in the MADR filename, not a supported-target claim;
* `README.md:160` is D6's required retained-and-retired platform row;
* `scripts/install.sh:94,96` are D4's explanatory rejection;
* `scripts/install_test.sh:139,142,144` are the retired-target fixture and its
  message assertion.

No missed build, release, installer, documentation, or test encoding was found.

### Verification evidence

* `grep -c 'darwin/amd64' .github/workflows/ci.yml` returned `0`.
* `grep -c 'darwin amd64' scripts/verify-build-metadata.sh` returned `0`.
* `bash -n` passed for all three in-scope shell scripts.
* `./scripts/verify-build-metadata.sh` passed with four targets.
* A deliberate `osusergo` injection into the surviving `darwin/arm64` build
  made that script fail and name `mcremote-darwin`; reverting the injection
  restored the passing result (A2/C2).
* `./scripts/install_test.sh` passed all 135 assertions, including rejection
  before download and checks for the reason, last release, and lack of a
  Rosetta fallback.
* `go build ./...` passed after every implementation phase and in the final
  whole-plan run.
* The range from the pre-execution baseline `be242ac` through the current
  worktree changes zero Go files, production or test (A9/C1).
* Targeted Markdown lint found README clean and six pre-existing MD004 findings
  in `docs/ops-linux-install.md`; none is on the changed lines.
