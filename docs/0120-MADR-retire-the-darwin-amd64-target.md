---
status: accepted
date: 2026-08-28
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Retire darwin/amd64 and publish exactly four targets

## Context and Problem Statement

The owner has named the target set the project should support:

```text
darwin/arm64, linux/amd64, linux/arm64, windows/amd64
```

The release job builds five. `darwin/amd64` is the extra, and it is published
today in both binaries. Nothing else in the owner's list is missing, and
`windows/arm64` is already excluded by [0116](0116-MADR-windows-and-linux-arm64-build-targets.md)
D19, so the whole gap is this one target.

The question is not whether the list is right — the owner set it. It is what
retiring a published Tier 1 platform actually costs, and which of the several
places that encode the target list have to move together so the set does not
end up meaning different things in different files.

### What was measured, not assumed

**`darwin/amd64` is published now, in both binaries.** The current release
carries it, versioned and aliased:

```text
$ gh release view --json assets -q '.assets[].name' | grep darwin
mcrelay-darwin-amd64
mcrelay-darwin-amd64-0.14.10.1
mcrelay-darwin-arm64
mcrelay-darwin-arm64-0.14.10.1
mcremote-darwin-amd64
mcremote-darwin-amd64-0.14.10.1
mcremote-darwin-arm64
mcremote-darwin-arm64-0.14.10.1
```

This is not a paper target being cleaned up. It ships.

**The target list is encoded in three places, and they agree.**
`ci.yml:180-185` sets `PLATFORMS` and calls `build_matrix` twice
(`ci.yml:213-214`), once per binary, so both get the same five.
`scripts/verify-build-metadata.sh:20-24` independently rebuilds the same five
to assert the build-tag policy. `scripts/install_test.sh:139-143` carries a
`darwin-amd64` dry-run fixture.

**The documentation does not agree with any of them.** `README.md:1409`
describes the tag build as **"linux/amd64, darwin/arm64, darwin/amd64"** —
three platforms. The actual list is five; `linux/arm64` and `windows/amd64`
are missing from that sentence. So the docs are already stale by two targets
*before* this change touches anything.

**`darwin/amd64` has never been executed. Anywhere.**

```text
$ grep -n 'runs-on:\|runner:' .github/workflows/ci.yml
38:    runs-on: ubuntu-latest
274:  - { runner: ubuntu-24.04-arm,  label: linux/arm64 }
275:  - { runner: windows-latest,    label: windows/amd64 }
340:  - { runner: ubuntu-24.04-arm,  label: linux/arm64 }
341:  - { runner: windows-latest,    label: windows/amd64 }
```

There is no macOS runner in CI at all. Darwin binaries are linked, checksummed,
and asserted cgo-free and tag-correct on the artifact, and then published
without ever having run.

**The stated reason for that is false, and this record does not repeat it.**
`ci.yml:322-325` says *"Re-enable a go-macos job when ready to pay for hosted
runners again."* GitHub's runner documentation: *"Use of the standard
GitHub-hosted runners is free and unlimited on public repositories."* macOS
runners are standard runners — the per-minute macOS price applies to private
repositories. This repository is public: `api.github.com` serves it
unauthenticated. **macOS CI here would be free.** The binding limit is
concurrency, not billing — 5 concurrent macOS jobs on Free/Pro/Team — which one
or two legs would not approach. The comment may have been true when the
repository was private; it is not true now.

**Intel macOS CI is available, and is expiring.** `macos-13` (Intel) retired in
December 2025. GitHub added `macos-15-intel` as the migration path, and it is
documented as **the last x86-64 image**, available until **August 2027**, after
which x86-64 is unsupported on GitHub Actions entirely — Apple having
discontinued the architecture. So a native `darwin/amd64` job is possible today,
free, for about another year, and then not at all.

**`darwin/arm64` is covered off-CI; `darwin/amd64` is not covered at all.** The
owner acceptance-tests `darwin/arm64` on an Apple Silicon laptop. There is no
Intel Mac in the fleet. So the two Darwin targets are not in the same position:
one has a human running it before release, the other has nobody.

**Rosetta does not rescue an Intel Mac.** Rosetta 2 translates x86-64 to run on
Apple Silicon. It does not run arm64 code on Intel hardware. An Intel Mac that
loses `darwin/amd64` has no path to a working binary — not a slower one, none.

**Both removal failure modes are clean; only one is legible.**

The self-updater resolves its own asset by `runtime.GOOS`/`GOARCH`
(`internal/update/run.go:67`) and returns a plain error when the asset is gone
(`internal/update/github.go:92-94`):

```text
no asset matching mcremote-darwin-amd64-* in release v0.15.0
```

The curl installer maps `x86_64 → amd64` (`scripts/install.sh:85`), then fails
at download (`:214`):

```text
could not download mcremote-darwin-amd64 from <url>
```

Neither crashes. Neither says *why*. Contrast `scripts/install.sh:88`, where an
unpublished architecture is rejected with a reason before any network call:

```sh
die 1 "32-bit ARM ($uname_m) is not published; only amd64 and arm64 are built."
```

That is the existing house pattern for a deliberate non-target, and this change
creates a second instance of exactly that situation.

**`scripts/install_test.sh` is not run by anything.**

```text
$ grep -rn 'install_test.sh' Makefile .github/workflows/ci.yml
(no matches)
```

`install-binary_test.sh` is in `make preflight` and in CI; `install_test.sh` —
the one holding the installer's platform-matrix assertions, including
`"Darwin x86_64 accepted (exit 0)"` at `:144` — is wired to neither. The
assertion this change must invert lives in a file no gate executes.

**The decision that published `darwin/amd64` is 0059, not 0116.**
[0059](0059-MADR-native-paths-and-linux-macos-parity.md) D10: *"Release both
`darwin/arm64` and `darwin/amd64`."* 0116 inherited the target and mentions it
only in passing (`:124`). So this record supersedes half of 0059 D10 and leaves
0116's decisions untouched.

### Findings

**F1 — `darwin/amd64` ships today and its removal is user-visible.** Both
binaries, versioned and aliased, through `v0.14.10`. An Intel Mac user on the
self-updater or the curl installer will see this change as a failure, not as an
absence.

**F2 — `darwin/amd64` has no acceptance host and no CI execution.** No macOS
runner is configured, and the owner's Mac is Apple Silicon. It is the only
target in the published set that no human and no machine has ever run. This is
the strongest argument for the owner's list.

It is *not* an argument that the coverage was unobtainable — F8 and F9 say a
free, native Intel runner exists today. The point is narrower and holds anyway:
the target has been published for the whole of its life without anyone running
it, and the coverage that would fix that has a 2027 expiry (F9). Compare 0116
D19's case for excluding `windows/arm64`, which rested partly on the CI image
being divergent; here the image is fine and simply going away.

**F3 — The target list has four encodings and they already disagree.**
`ci.yml`, `verify-build-metadata.sh`, `install_test.sh`, and `README.md:1409`.
The README is stale by two targets right now, which means the drift predates
this change and will outlive it unless the phase that shrinks the list also
reconciles the prose.

**F4 — An Intel Mac has no fallback.** Rosetta runs amd64 on arm64, not the
reverse. "Use the arm64 build" is not available as advice.

**F5 — The removal is silent where it should be explanatory.** Both the updater
and the installer fail cleanly and uninformatively. The installer already has
the right pattern for this exact case (`install.sh:88`) and does not use it
here.

**F6 — The regression test for the installer's platform matrix does not run.**
`install_test.sh` is referenced by no Makefile target and no CI job. Inverting
its `darwin-amd64` assertion is necessary but, on its own, buys no protection.

**F7 — `verify-build-metadata.sh` must not lose its Darwin arm.** Its
`:57` loop asserts that Darwin binaries carry *no* `netgo,osusergo` — the
0059 D9 policy. `darwin/arm64` remains in that loop, so the assertion survives;
this is worth stating because deleting the wrong line would silently retire the
check along with the target.

**F8 — macOS CI is free for this repository, and `ci.yml` says otherwise.** The
comment at `ci.yml:322-325` gives cost as the reason native macOS CI is off.
Standard macOS runners are free and unlimited on public repositories, and this
repository is public. Any argument in this record or a future one that rests on
"macOS runners cost money" is unsound and must not be made. The comment itself
is a defect: it will mislead the next person who asks this question.

**F9 — Intel macOS CI has a hard expiry of August 2027.** `macos-15-intel` is
GitHub's last x86-64 image; after it, the architecture is unsupported on
Actions. So the option of *earning* `darwin/amd64` coverage is real, free, and
time-boxed to roughly a year — after which the target would return to exactly
the unverifiable state that motivates retiring it now.

## Decision Drivers

* The owner has set the supported target list; this record implements it, it
  does not relitigate it.
* A dropped platform must be dropped *loudly* — a 404 is a bug report waiting
  to happen, a stated reason is a decision.
* One target list, one meaning, in every file that encodes it. F3 shows what
  happens when that slips.
* Retiring a target must not retire a *check* that happens to be attached to it
  (F7).
* Published releases are immutable. Whatever is already out stays out.

## Considered Options

* **A — Drop `darwin/amd64` from the next tag, and say why in the places a user
  will hit it.**
* **B — Keep `darwin/amd64` and add a macOS CI runner** to earn the coverage it
  has never had.
* **C — Keep publishing it, untested, as today.**
* **D — Demote to Tier 2** rather than dropping: keep building, document it as
  unsupported.

## Decision Outcome

**Chosen: A — drop it, and make the drop legible.**

The owner's list is the requirement, and F2 independently justifies it: a
target nobody runs is a target nobody can vouch for, and publishing it implies
a claim the project cannot support.

**B deserves more than it first appeared to.** The obvious rejection — hosted
macOS runners cost money, per `ci.yml:322-325` — is simply false for a public
repository (F8), and a native Intel runner does exist (F9). So B is not blocked
by money or by hardware. It is rejected on two other grounds. First, F9: the
coverage it would buy expires in August 2027 with the last x86-64 image, after
which `darwin/amd64` returns to being unverifiable and this decision has to be
made again with less runway. Second, and decisively, B spends effort earning
confidence in a platform the owner has said they do not want to support —
solving the wrong problem well. C is the status quo and F2 is the argument
against it. D keeps every cost of building and publishing the target while
dropping the promise, which is the worst trade of the four: the artifact still
exists, users still find it, and the docs say not to trust it.

**What B is right about survives this decision.** Its real insight is not about
`darwin/amd64` at all — it is that free macOS CI has been available the whole
time and the project declined it for a reason that does not hold. That applies
to `darwin/arm64`, which after this record is the only published target with no
automated execution anywhere. Retiring `darwin/amd64` does not answer it; it
sharpens it. See the deferred item in the plan, and D10.

The real work is not the deletion — it is F3 and F5. Shrinking `PLATFORMS` by
one line takes a minute; making the four encodings agree and making the removal
explain itself is the rest of it.

### The decisions

**D1 — The published target set is exactly four:** `linux/amd64`,
`linux/arm64`, `darwin/arm64`, `windows/amd64`. Effective from the next version
tag.

**D2 — Supersede 0059 D10's Darwin release clause additively.** 0059 is
`accepted`; its rationale is not to be edited. Append an
`## Amendment — 2026-08-28` section to 0059 recording that its
"release both `darwin/arm64` and `darwin/amd64`" is narrowed to `darwin/arm64`
by this record, and why. Everything else in D10 stands.

**D3 — Every encoding of the target list moves in the same phase.**
`ci.yml`, `scripts/verify-build-metadata.sh`, `scripts/install_test.sh`,
`README.md` (both the platform table and the stale CI sentence at `:1409`), and
`docs/ops-linux-install.md:66`. F3's existing drift is repaired at the same
time — leaving `README.md:1409` naming three of five while changing five to
four would make it wrong in a new way.

**D4 — `scripts/install.sh` rejects `darwin/amd64` with a stated reason,**
before any download, in the shape of the existing `:88` rejection. The message
names the target, says it is not published, and states that Rosetta does not
help (D7). Exit code matches the existing unsupported-architecture path.

**D5 — Published releases are not rewritten.** `v0.14.10` and everything before
it keep their `darwin/amd64` assets. An Intel Mac pinned to an old release
keeps working; it simply stops receiving updates. Do not delete assets, do not
re-cut tags.

**D6 — The README states the retirement with a reason, in the row that already
exists for this purpose.** `darwin/amd64` moves to the `windows/arm64` pattern:
a `—` tier and a "not supported" note pointing at this record. Deleting the row
would leave a reader who has the binary with no explanation.

**D7 — Make no Rosetta claim.** F4. Any prose that mentions the retirement says
plainly that Intel Macs have no supported build, rather than implying the arm64
artifact substitutes.

**D8 — Do not weaken `verify-build-metadata.sh` while shrinking it.** F7. After
the change its Darwin arm still asserts the no-tags policy on `darwin/arm64`,
and the script still fails if a Darwin binary acquires `netgo` or `osusergo`.

**D9 — Wiring `install_test.sh` into a gate is out of scope, and named as
deferred rather than done.** F6 is real, and fixing it is a CI-policy change
with its own blast radius. This record inverts the assertion and runs the
script by hand; it does not add a job.

**D10 — Correct the false cost comment in `ci.yml`, and do not add a macOS job
under this record.** F8. The comment at `ci.yml:322-325` must stop saying that
re-enabling macOS CI means paying for runners; it should say that standard
macOS runners are free on public repositories, that the concurrency cap is 5
concurrent macOS jobs, and that `darwin/arm64` is presently covered by owner
acceptance testing rather than CI. This is a two-line prose fix to a comment
that is actively misleading — it is the reason this question had to be asked at
all. **Adding the job itself is a separate decision** and does not ride along
here: it changes what gates a release, and it belongs to the deferred
`darwin/arm64` coverage record, not to a target retirement.

### Consequences

* Good: the published set matches the set anyone can actually vouch for — three
  targets exercised natively in CI plus one the owner acceptance-tests by hand.
* Good: one fewer cross-compile per binary per release, and one fewer artifact
  pair in every release's asset list and `SHA256SUMS`.
* Good: F3's documentation drift is repaired rather than merely not worsened.
* Bad: Intel Macs lose support outright, with no fallback (F4). Accepted: the
  target was never tested, so what is being withdrawn is an implied promise the
  project was not keeping.
* Bad: anyone currently updating an Intel Mac install gets an error on the next
  release. Mitigated by D4's message and by D5 leaving old releases intact —
  not eliminated.
* Neutral: `darwin/arm64` is unaffected in every respect, including its absence
  from CI.

### Confirmation

```bash
grep -c 'darwin/amd64' .github/workflows/ci.yml            # → 0
grep -c 'darwin amd64' scripts/verify-build-metadata.sh    # → 0
./scripts/verify-build-metadata.sh                         # → exit 0, 4 targets
./scripts/install_test.sh                                  # → exit 0
```

```text
next tag's release assets            → no mcremote/mcrelay-darwin-amd64*
next tag's SHA256SUMS                → 8 lines (2 binaries x 4 targets)
install.sh on a Darwin/x86_64 stub   → rejected with a reason, no download
README platform table                → darwin/amd64 present, tier —, reason given
README:1409                          → names all four targets
0059                                 → carries an Amendment section
```

## Pros and Cons of the Options

### A — Drop it and make the drop legible (chosen)

* Good: implements the owner's list.
* Good: the published set becomes one the project can defend (F2).
* Good: forces the F3 drift repair, which nothing else was going to force.
* Bad: Intel Macs lose support with no fallback (F4).
* Bad: touches six files for what looks like a one-line change — but F3 is why
  the one-line version would be wrong.

### B — Keep it and add a macOS CI runner

* Good: the strongest argument against dropping — it converts F2 from a reason
  to retire into a solved problem, and would cover `darwin/arm64` too, which is
  currently the least-verified target in the *keep* list.
* Good: **it is free.** Standard macOS runners are free and unlimited on public
  repositories (F8). The cost objection recorded at `ci.yml:322-325`, which an
  earlier draft of this record repeated as evidence, does not hold here.
* Good: **it would be native, not emulated.** `macos-15-intel` is a real x86-64
  runner (F9). An earlier draft of this record claimed a `darwin/amd64` job
  would run under Rosetta on Apple Silicon; that was wrong.
* Bad: the Intel image is withdrawn in August 2027 (F9), so the coverage is
  rented rather than bought — and when it lapses, this decision recurs with
  less runway than it has now.
* Bad: earns confidence in a platform the owner has said they do not want,
  which is effort spent against the requirement rather than toward it.
* Bad: adding a release-gating job is a larger change than a target
  retirement. Mixing them would mean one commit that both narrows the published
  set and widens what can block a release.

### C — Keep publishing it untested

* Good: no work, no user breakage.
* Bad: F2. The project publishes a binary nobody has run, on hardware nobody
  owns, and implies support for it.

### D — Demote to Tier 2, keep building

* Good: preserves a path for existing Intel users.
* Bad: keeps the full build and publish cost while withdrawing the promise.
* Bad: Tier 2 in this project means *"built, unit-tested and smoke-tested in CI
  on every push and tag"* (`README.md:163`). `darwin/amd64` meets none of that,
  so calling it Tier 2 would make the tier definition a lie.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Five targets built, both binaries | `.github/workflows/ci.yml:180-185`, `:213-214` |
| Same five rebuilt for tag policy | `scripts/verify-build-metadata.sh:20-24` |
| Darwin no-tags assertion | `scripts/verify-build-metadata.sh:57` |
| Installer darwin-amd64 fixture | `scripts/install_test.sh:139-144` |
| `darwin/amd64` published in v0.14.10 | `gh release view --json assets` |
| No macOS runner in CI | `.github/workflows/ci.yml:274-275, 340-341` |
| Native macOS CI deliberately off | `.github/workflows/ci.yml:322-325` |
| README CI sentence stale by two targets | `README.md:1409` |
| README lists darwin/amd64 Tier 1 | `README.md:160` |
| Tier 2 definition | `README.md:163` |
| ops doc names both Darwin arches | `docs/ops-linux-install.md:66` |
| Updater resolves by runtime GOOS/GOARCH | `internal/update/run.go:67` |
| Updater missing-asset error | `internal/update/github.go:92-94` |
| Installer arch mapping | `scripts/install.sh:85` |
| Installer download failure | `scripts/install.sh:214` |
| Existing stated-reason rejection | `scripts/install.sh:88` |
| `install_test.sh` runs nowhere | `grep -rn 'install_test.sh' Makefile ci.yml` → empty |
| Darwin release clause originates in 0059 | `docs/0059-MADR-...:433` (D10) |
| `windows/arm64` exclusion precedent | `docs/0116-MADR-...` D19 |
| Standard runners free/unlimited on public repos | [GitHub-hosted runners reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) |
| macOS runners are standard, not larger | same page; larger runners are a separate opt-in product |
| 5 concurrent macOS jobs (Free/Pro/Team), 50 (Enterprise) | [Actions limits](https://docs.github.com/en/actions/reference/limits) |
| This repository is public | unauthenticated `api.github.com` serves its runs |
| `macos-13` (Intel) retired December 2025 | [Changelog: macOS 13 runner image closing down](https://github.blog/changelog/2025-09-19-github-actions-macos-13-runner-image-is-closing-down/) |
| `macos-15-intel` is the last x86-64 image, until Aug 2027 | [runner-images #13045](https://github.com/actions/runner-images/issues/13045) |

### Related records

* [0059-MADR](0059-MADR-native-paths-and-linux-macos-parity.md) — D10 is the
  clause this record narrows; D9 is the build-tag policy D8 protects.
* [0116-MADR](0116-MADR-windows-and-linux-arm64-build-targets.md) — D19 is the
  precedent for a documented non-target. Its PLAN is `in-progress`; this record
  does not touch its decisions and does not depend on it closing.
* [0104-MADR](0104-MADR-installer-linux-and-macos.md) — owns `install.sh`; D4
  edits it within that record's design, not against it.
* [0119-MADR](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md) — the
  arm64 lane is red, independently. This record's phases do not depend on it
  and must not be used to fix or excuse it.

### Open questions for the plan

1. Does the retirement need a release-note line on the first tag that omits the
   target, or is the README row enough? A user who never reads the README meets
   this as an installer error.
2. `install.sh` rejects on `uname` before it knows the OS-arch pair is
   unpublished for *this* project version. Should the rejection be a static
   `darwin/x86_64` case (simple, but wrong for anyone installing an old
   version), or driven by the SHA256SUMS it already downloads (accurate, more
   code)? D4 does not settle this.
3. ~~Are there other asset-count assertions that assume five targets?~~
   **Closed.** `SHA256SUMS` is generated by glob (`ci.yml:242`,
   `sha256sum mcremote-* mcrelay-*`) and the publish job enumerates assets by
   glob too (`ci.yml:795`). No line-count or asset-count assertion anywhere in
   `scripts/` or `ci.yml` is tied to the number of targets, so the four
   encodings in F3 are the whole surface. The plan still greps to confirm
   nothing was added since.
4. ~~**Should `darwin/arm64` get a free `macos-15` CI job?** … This is now the
   most valuable question this record raises …~~
   **Answered: no.** See [the 2026-08-29 amendment](#amendment--2026-08-29-the-macos-ci-job-is-not-worth-it-open-question-4-answered)
   and D11. The question was posed on the assumption that "no automated
   execution" implied a large untested surface. Measured, it is 67 lines in two
   files, already compile-gated on every push, with no darwin-gated test files
   at all and the interesting macOS logic (plist, TCC, launchd) ungated and
   already tested on both existing lanes. The "most valuable question" framing
   was wrong and is retained here only so the error is legible.
5. Does anything still depend on `darwin/amd64` being *buildable* rather than
   published — a developer running `make build GOOS=darwin GOARCH=amd64`? The
   Makefile keeps cross-compiling it either way; only the release matrix and
   `verify-build-metadata.sh` change. Worth confirming nobody's local workflow
   assumes the tag-policy script covers it.

## Amendment — 2026-08-29: the macOS CI job is not worth it; open question 4 answered

Open question 4 asked whether `darwin/arm64` should get a free `macos-15` job,
and called it *"the most valuable question this record raises."* That framing
was wrong. It rested on an unmeasured assumption — that "no automated execution"
meant a large body of untested code — and the measurement contradicts it.

### What was measured

**The darwin-gated surface is 67 lines in two files.**

```text
$ grep -rl '//go:build darwin' --include='*.go' . | grep -v _test
internal/procutil/owner_darwin.go        47 lines
internal/procutil/starttoken_darwin.go   20 lines
```

**There are no darwin-gated test files.**

```text
$ grep -rl '//go:build darwin' --include='*_test.go' .
(empty)
```

So the usual justification for adding a platform lane — tests that exist but
never run — does not apply. Nothing is being skipped.

**The macOS-specific logic that matters is not build-gated at all.**
`internal/cli/service/plist_render.go` (LaunchAgent rendering), `internal/tcc/`
(Full Disk Access detection), and the launchd setup path carry no build tags.
They are ordinary Go, and their tests already run on both existing lanes:

```text
$ go test ./internal/cli/service/ ./internal/tcc/ ./internal/procutil/
ok  internal/cli/service
ok  internal/tcc
ok  internal/procutil
```

That run is from the owner's **Windows** host. The same tests run on
`ubuntu-latest`. The interesting macOS code is therefore covered twice already.

**The 67 gated lines are compile-checked on every push.**
`make verify-build-metadata` cross-compiles `darwin/arm64`, and `procutil` is in
the binary's dependency tree, so a compile break in either file reddens CI
today. Confirmed: `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...` → exit
0, and that build is already a CI step.

**Four `runtime.GOOS == "darwin"` branches exist in non-test code**
(`appdirs/roots_unix.go:52`, `cli/service/setup.go:678`,
`cli/setup_service.go:149`, `provider/grok/device_auth.go:87`).

### The finding

**F10 — a `macos-15` job would add runtime execution of 67 lines and 4
branches, and nothing else.** Every other test it ran would be a third copy of a
suite already green on two platforms. Those 67 lines are process-owner lookup
and start-token derivation, whose real failure modes are launchd, TCC and
permissions — conditions a hosted runner reproduces no better than the owner's
own Mac, and arguably worse, since the runner's environment matches neither the
owner's nor a user's.

Against that sits a fourth lane that can block releases.
[0119](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md) is this record's
own evidence for what that costs: a flaky lane consumed a day and produced false
signal before yielding a genuine defect. A lane earns its keep by the failures it
catches, and 67 compile-gated lines is a thin catch.

**D11 — do not add a macOS CI job on this record's reasoning.** Open question 4
is answered *no*. F8 (macOS CI is free) remains true and is still worth knowing;
it removes the *cost* objection but never established a *benefit*, and this
amendment supplies the missing half. Anyone reopening this should argue from the
gated surface, not from the phrase "unexercised target".

### What the measurement does not settle

macOS verification is a **manual step with no record**. Nothing in the
repository states what the owner checks before a release, and nothing detects a
release that shipped on a week the check was skipped. That is a real exposure,
but it is a process gap, not a test-coverage gap, and a written acceptance
checklist addresses it far more cheaply than a CI job. It is the owner's call
and is not a decision this record makes.
