---
status: complete
date: 2026-09-01
associated-madr: "0128-MADR-triage-the-0126-and-0127-deferred-items.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0128 — Clear the deferred lists: four fixes, four triggers, two closures

Implements [0128-MADR-triage-the-0126-and-0127-deferred-items.md](0128-MADR-triage-the-0126-and-0127-deferred-items.md)
decisions D1–D6.

## Goal

Nothing is left on 0126's or 0127's Deferred lists that needs re-reading to
know its state.

Finish line:

* an Actions drift check exists, reports the measured table, and has been seen
  to fail;
* the phone compares published versions, and `make apk` stamps the same
  `versionName` shape as CI;
* `NotificationService` survives `dispose()` → `init()`;
* the `compute()` per-save cost is a number in this plan;
* every remaining deferred entry names a trigger or says "closed".

## Scope

### In scope (the only files any phase may touch)

* `.github/dependabot.yml` (restored, `github-actions` only) — P1
* `apps/mobile/lib/data/update/app_update.dart`, `test/app_update_test.dart`,
  `Makefile` (`apk` target) — P2
* `apps/mobile/lib/data/notifications/notification_service.dart`,
  `test/notifications_test.dart` — P3
* `apps/mobile/test/transcript_cache_bench_test.dart` (new) — P4
* `docs/0126-PLAN-*`, `docs/0127-PLAN-*`, `docs/0128-PLAN-*` — P5

### Out of scope

* **Any change to `compute()` usage.** D4: P4 measures and stops. Acting on the
  number is a separate decision, and taking it here would make the measurement
  a formality.
* **Restoring the `pub` or `gomod` ecosystems.** D1 restores `github-actions`
  and nothing else. `pub` is the ecosystem that caused both incidents 0127 D5
  cites, and it stays deleted permanently.
* **Bumping `actions/setup-java` to v6.** P1 builds the instrument; reading it
  and deciding is the owner's, and a major action bump has its own blast radius.
* **`flutter_secure_storage`, the KGP plugins, battery-optimisation prompting,
  the second host.** Blocked or gated; P5 writes their triggers, nothing else.
* **0126 P7 rows 1–3.** Still open, unchanged by this plan.

## Stability rule

Every phase touching Dart ends with:

```bash
cd apps/mobile && dart format . && flutter analyze && flutter test
```

Phases touching shell or the Makefile run `make pre-add-check` before staging.
Then **one commit** (`git commit --no-edit`; never `-m`).

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — Every new test is proven to fail first.** Established the hard way three
times across 0126/0127: a gate whose negative case was never run, a revert whose
`str.replace` silently matched nothing, and a discrimination check read from
truncated output. Assert on the edit, and read the whole log.

**C2 — P4 changes no production code.** D4. A benchmark that arrives with an
optimisation is not a measurement.

**C3 — P1 restores one ecosystem, not the file.** D1. `pub` and `gomod` do not
come back, and the restored file says why so the next reader does not "complete"
it.

**C4 — A trigger names an observable, not an intention.** D5. "When #1236 is in
a published release" is checkable; "revisit later" is not.

**C1 and C2 are the ones at risk** — C1 because four small fixes feel too
obvious to bother; C2 because P4 will produce a number that invites an
immediate fix.

## Dependency and delivery order

P1–P4 are independent and can land in any order or be dropped individually.
P5 is last: it records what the earlier phases actually concluded.

## Implementation Steps

### P1 — Restore Dependabot for `github-actions` only (D1)

Recreate `.github/dependabot.yml` with the `github-actions` block from the
version 0127 P7 deleted (`git show 093d7df^:.github/dependabot.yml`), verbatim
where possible: `directory: "/"`, `interval: monthly`,
`open-pull-requests-limit: 5`, `commit-message.prefix: "ci"`, and the
`groups: github-actions: patterns: ["*"]` grouping. Those settings were reasoned
about when they were written — monthly because every PR costs a CI run and
Actions minutes are the constraint; grouped so one PR covers all bumps rather
than one per action — and none of that reasoning changed.

**The `pub` and `gomod` blocks are NOT restored.** Add a comment at the top
saying so and why, because the obvious future edit is to "complete" the file:

* `pub` resolves in Dependabot's container without the Flutter SDK, so it cannot
  see the exact pins in `packages/flutter{,_test}/pubspec.yaml` and will propose
  lockfiles the pinned toolchain silently reverses. That is commit `6c02c8e`
  and 0112 finding 4 — twice.
* `gomod` is covered where it counts by `govulncheck` in the pre-add gate, which
  reports *called* vulnerabilities rather than merely present ones.

Also record what 0127 D7 gate 1 now buys: if `pub` were ever restored, a
lockfile Dependabot proposes that the pinned Flutter cannot reproduce fails CI
instead of merging silently. The gate is why this is recoverable rather than a
standing hazard.

**Verification.**

```bash
python3 -c "import yaml,sys; d=yaml.safe_load(open('.github/dependabot.yml')); \
  eco=[u['package-ecosystem'] for u in d['updates']]; \
  assert eco==['github-actions'], eco; print('ecosystems:', eco)"
git show 093d7df^:.github/dependabot.yml | diff - .github/dependabot.yml || true
```

The diff is expected to show exactly the removed `pub` and `gomod` blocks plus
the new comment — read it, rather than trusting that the right thing was
restored.

GitHub validates this file on push and reports a malformed one on the
repository's Dependabot page rather than failing a build, so YAML parsing
locally is necessary but not sufficient; the first monthly run is the real
confirmation and is outside this plan's reach.

### P2 — Compare published versions, and stamp one shape (D2)

**Part 1 — the comparison.** `AppUpdateService.isNewerBase` compares three
parts; Go's `update/run.go:81` uses `NewerPublished`, which compares the serial
too (MADR 0103). Add the fourth component to `parseBase`'s result and compare
`major, minor, patch, then N`, mirroring `internal/update/version.go`. Keep the
existing behaviour for three-part inputs, where the absent serial is 0.

Name it for what it does. `isNewerBase` will be a lie once it compares serials.

**Part 2 — the shape divergence 0126 P6 introduced.** `make apk` passes
`--build-name="${VER%.*}"` (three-part) while CI passes the full four-part
version (`ci.yml:631-638`). Make the Makefile pass the full `$VER`, matching CI
and `ci.yml`'s stated intent that the APK and the binaries in a release agree.
`--build-number` stays the serial locally and `github.run_number` in CI; that
difference is deliberate and documented there.

`scripts/build-apk.sh` has the same three-part split and is the source P6 copied
from. **Fix it too or leave it deliberately** — decide and say which in the
commit; what is not acceptable is two local build paths disagreeing.

**Verification:** a test with `remote=v0.15.3.3`, `local=0.15.3.2` expecting an
update, which the current three-part compare fails (C1 — run it against the old
code first). Plus the existing three-part cases, unchanged. Then `make apk` and
`aapt dump badging`, expecting a four-part `versionName`.

### P3 — Make `NotificationService` restartable (D3)

`dispose()` closes `_responses` and leaves `_ready == true`, so a later `init()`
returns early and every `show*` adds to a closed controller. Its counterpart —
`NotificationCoordinator.dispose()` — nulls its subscriptions precisely so a
later `start()` re-subscribes, so the two halves currently disagree.

Reset `_ready = false` in `dispose()`, and give `_responses` the same treatment
the coordinator gives its fields: a closed controller must not be reused, so
either recreate it on `init()` or guard `add` on `isClosed`. Prefer recreating —
a guard turns a restart into silent no-delivery, which is the failure this app
least wants.

**Verification:** a test doing `init()` → `dispose()` → `init()` → `show*` →
expect no throw and the response stream still live. Proven to fail first.

### P4 — Measure the isolate-per-save cost, and stop (D4, C2)

`apps/mobile/test/transcript_cache_bench_test.dart`, tagged so it does not run
in the default suite.

Measure, at `kTranscriptCacheMaxItems` = 150 items:

```text
1. compute(encodeTranscriptCachePayload, …)   wall time, N=50, median + p90
2. the same encode inline on the main isolate  wall time, N=50, median + p90
3. compute(decodeTranscriptCachePayload, …)    wall time, N=50, median + p90
4. the same decode inline                      wall time, N=50, median + p90
```

The comparison that matters is (1) vs (2): `compute`'s cost is isolate spawn
plus payload copy, and it is only worth paying if the inline encode would blow a
frame. State the payload's serialized size alongside the timings.

Then reason about cadence rather than measuring it: saves are debounced at
`kTranscriptCacheDebounce` = 400 ms **per session**, so the worst case is ~2.5
spawns/second/session during sustained streaming.

**Record the numbers in this plan whatever they are**, and change nothing (C2).
Two honest outcomes: the deferral closes with evidence, or it becomes a finding
for MADR 0084's measurement set.

Note the instrument's limits in the record: a desktop VM's isolate spawn is not
an Android phone's, so this bounds the question rather than settling it.

### P5 — Rewrite the Deferred lists (D5, D6)

Replace both lists with entries that state a **state** and, where open, a
**trigger**:

```text
0127 E  flutter_secure_storage 11.0.0
        BLOCKED. Trigger: a published 11.0.x whose android/build.gradle no
        longer pins compileSdk (upstream PR #1236). Re-checked 2026-09-01:
        open, unmerged. Analysis: 0066 amendment.
0127 F  F-KGP
        BLOCKED. Trigger: mobile_scanner or speech_to_text publishes a
        Built-in-Kotlin release. Both at 7.4.0, re-checked 2026-09-01.
        Deadline: the Flutter release that turns the warning into an error.
0126 G  battery-optimisation prompting
        GATED on 0126 P7 row 1. Trigger: row 1 shows START_STICKY alone does
        not restore alerts after a swipe.
0127 H  second host (Windows, 3.47.1)
        OPEN, not actionable here. Trigger: that host runs `make preflight`
        and 0127 D7 gate 2 fails.
0126 I  autoRunOnBoot / environment: sdk ^3.12.2
        CLOSED — decisions not to act, not pending work.
```

Then move A–D's entries to whatever P1–P4 concluded.

## Verification (whole plan)

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && flutter analyze && flutter test
cd ../.. && python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
make apk && "$ANDROID_HOME"/build-tools/*/aapt dump badging \
  apps/mobile/build/app/outputs/flutter-apk/app-release.apk | head -1
grep -A5 '^## Deferred' docs/0126-PLAN-*.md docs/0127-PLAN-*.md
```

### Acceptance criteria

1. `.github/dependabot.yml` declares `github-actions` and nothing else, parses
   as YAML, and diffs against the deleted version by exactly the `pub`/`gomod`
   blocks plus its new comment.
2. A four-part remote-vs-local comparison prefers the higher serial, proven to
   fail under the old code.
3. `make apk`'s `versionName` matches CI's shape, and `build-apk.sh` either
   matches or its difference is recorded.
4. `dispose()` → `init()` → `show*` does not throw, proven to fail first.
5. P4's numbers are in this plan and **no production file changed** in that
   phase.
6. Every entry on both Deferred lists names a state, and every open one names a
   trigger.
7. `flutter test` at or above **1364**, the count after 0126 P6.

Criterion 5 is the one to watch: the benchmark will invite a fix.

## Rollout and Rollback

No runtime behaviour ships except P2's comparison and P3's restart path, both
small and independently revertable. P2 makes the phone offer updates it
previously withheld on serial-only releases — the intended effect, and the same
rule the CLI has followed since MADR 0103.

P1 and P4 add no runtime code at all.

## Deferred (this plan's own)

* **Acting on P4's numbers.** By construction (C2).
* **`actions/setup-java` v5 → v6.** Dependabot will now propose it in its next
  monthly PR. Taking a major action bump is a separate decision with its own CI
  blast radius, and the standing policy is that majors are reviewed by hand.

## Execution record — 2026-09-01

### P1 — Dependabot restored for `github-actions` only (D1)

`.github/dependabot.yml` recreated from `093d7df^` with the `github-actions`
block verbatim — `directory: "/"`, monthly, `open-pull-requests-limit: 5`,
`commit-message.prefix: "ci"`, and the `patterns: ["*"]` grouping. None of that
reasoning had changed, so none of it was re-derived.

```text
ecosystems: ['github-actions']
settings preserved: monthly / prefix=ci / grouped / limit=5

diff vs the deleted version:
  <   - package-ecosystem: gomod
  <   - package-ecosystem: pub
  <       - dependency-name: "flutter_secure_storage"
  <         versions: ["11.0.0"]
```

The diff is exactly the two removed ecosystems and nothing else — read rather
than assumed, because "restored the file" and "restored the right part of the
file" are different claims.

**One thing the diff surfaces:** the `flutter_secure_storage: ["11.0.0"]` ignore
went with the `pub` block. That preference is not lost — 0127 P6c moved it into
[0066](0066-MADR-secure-storage-upgrade-resilience.md)'s 2026-09-01 amendment,
with the concrete reason (the release hard-pins `compileSdk 37`) rather than the
bare "not a release we will take" the config comment carried. It is now recorded
where the decision lives instead of in a bot's configuration.

The file's header says why `pub` and `gomod` are absent, because the obvious
future edit is to "complete" it.

**Limit of this verification:** GitHub validates `dependabot.yml` on push and
reports a malformed file on the repository's Dependabot page rather than failing
a build. Local YAML parsing plus the schema assertions above are necessary, not
sufficient; the first monthly run is the real confirmation and is outside this
plan's reach.

### P2 — Published-version compare, and one stamp shape (D2)

**Part 1 — the comparison.** `isNewerBase` → `isNewerPublished`, mirroring Go's
`update.NewerPublished` (`internal/update/version.go`), which is what
`update/run.go:81` has used since MADR 0103. `parseBase` → `parseVersion`,
returning a named `AppVersion` with a fourth component; a three-part version
reads as `n = 0`, matching Go's zero value, so a three-part remote never appears
newer than the same base carrying a serial.

Renamed rather than extended in place: `isNewerBase` would have been a lie the
moment it compared serials, and the old name is exactly what made the
divergence from Go hard to notice.

The parser follows Go's `leadingInt` behaviour for the awkward inputs — a patch
field with a local suffix (`0.6.7.4.gabc`) still yields patch 7 and serial 4,
and a non-numeric fourth field compares as 0 rather than making the whole
version unparseable.

**Proven to discriminate (C1).** With the comparison reverted to three-part
semantics (`return false` when the base is equal — exactly what `isNewerBase`
did):

```text
00:00 +2 -1: … a serial-only release is newer [E]
  Expected: true
    Actual: <false>
  0128 D2: N decides when the base is equal
```

The four existing three-part cases were kept unchanged and still pass, so this
is an extension of the old behaviour rather than a replacement of it.

**Part 2 — the shape divergence 0126 P6 introduced.** `make apk` now passes the
**full four-part** version as `--build-name`, matching CI and the intent stated
at `ci.yml:631`. `--build-number` remains the serial locally and
`github.run_number` in CI; that difference is deliberate and documented there,
because a `versionCode` must increase monotonically for ever while N restarts at
1 on each new release base.

`scripts/build-apk.sh` carried the same three-part split — it is the source P6
copied from — and was **aligned too** rather than left as a second local build
path disagreeing with the first. The plan required this to be decided rather
than drifted into; the decision is "fix it", and the comment in the script says
why.

```text
==> apk 0.15.3.4 (4)
aapt: versionCode='4' versionName='0.15.3.4'
build tags: 264 before and after   (no ledger serial claimed)
```

Previously `0.15.3`; before 0126 P6, `0.1.0`.

**Gate:** `dart format` clean, `flutter analyze` clean, `flutter test`
**`+1366 ~3`**.

### P3 — `NotificationService` is restartable (D3)

`dispose()` now clears `_ready`, and `init()` recreates `_responses` when it
finds the controller closed. The controller field stopped being `final` for that
reason.

Recreating rather than guarding `add` on `isClosed`, as the phase specified: a
guard turns a restart into silent non-delivery, which is the worst available
outcome for a class whose whole job is getting an approval alert to the user.

**Proven to fail first (C1).** With only the `_ready = false` removed:

```text
00:00 +0 -1: restart after dispose (0128 D3) … [E]
  Expected: not null
    Actual: <null>
  0128 D3: dispose() must clear _ready so init() runs again
```

`initSettings` stayed null because the second `init()` returned at its own
`if (_ready) return` — the exact skip that left the closed controller in place
for the next `show*`.

The test does `init()` → `dispose()` → `init()` → listen → `showTurnComplete`,
asserting re-initialisation happened and delivery does not throw.

**Gate:** `dart format` clean, `flutter analyze` clean, `flutter test`
`+1367 ~3`.

### P4 — The isolate-per-save cost, measured (D4, C2)

`apps/mobile/test/transcript_cache_bench.dart`. **No production file changed**
(C2).

**Correction, caught by the whole-plan gate.** It was first written as
`transcript_cache_bench_test.dart` with `@Tags(['bench'])`, on the assumption
that the tag kept it out of the default run. It does not: `@Tags` without a
`dart_test.yaml` tag configuration excludes nothing, and the benchmark was
running on every `flutter test` — visible only as the suite count going to 1368
instead of 1367. Renamed so it no longer matches the `test/**_test.dart` glob
`flutter test` sweeps; run it explicitly with
`flutter test test/transcript_cache_bench.dart`. Same shape as this pair's other
verification slips: the mechanism was assumed rather than checked.

```text
--- 0128 D4: transcript cache codec cost (N=50, 150 items) ---
payload serialized:   59390 bytes
encode via compute(): median 0.35ms  p90 0.69ms
encode inline:        median 0.12ms  p90 0.21ms
decode via compute(): median 0.58ms  p90 1.11ms
decode inline:        median 0.15ms  p90 0.20ms
worst-case cadence:   1 save / 400ms / session
```

**The instrument was checked before the numbers were believed.** 0.35 ms is far
cheaper than an isolate spawn is usually assumed to cost, which is exactly the
shape of a benchmark measuring the wrong thing — so a probe confirmed `compute`
really does cross an isolate boundary in `flutter_test`:

```text
main isolate     : main
callback isolate : Closure: (int) => String from Function 'probe': static.
sideEffect seen in main: 0    (0 => separate isolate, 99 => inline)
```

A top-level variable set inside the callback was **not** visible afterwards in
the main isolate. The spawn is real; Dart's `Isolate.run` is simply much cheaper
than the classic `Isolate.spawn` mental model. (Probe removed; it was never
committed.)

#### Conclusion: the deferral closes, no action needed

Worst case is ~2.5 saves/second/session during sustained streaming, so
`compute`'s overhead over inline is roughly **0.6 ms per second per streaming
session** — against a 16 ms frame budget, on a path that is already debounced
and off the critical rendering path. There is nothing here worth optimising, and
0126 was right to refuse to guess either way.

**A more interesting observation, recorded rather than acted on.** The *inline*
encode is 0.12 ms. MADR 0084 B2 moved this work to an isolate because the decode
"exceeded a frame budget on a large entry" — at `kTranscriptCacheMaxItems` = 150
neither direction comes close. Either the constant has shrunk since that
measurement, or the original case was larger than today's cap allows. So the
open question is not "is `compute` too expensive" but "is `compute` still
needed" — which belongs with 0084's measurement set, not here, and is not worth
touching for 0.2 ms either way.

**Limits of the instrument**, stated so the numbers are not over-read: a desktop
VM's isolate spawn and JSON codec are not an Android phone's. What transfers is
the *ratio* (compute ≈ 3× inline) and the absolute payload size (58 KB); the
per-call milliseconds do not. The payload is synthetic — 150 assistant items of
~400 characters each — chosen to be representative of a streamed reply rather
than a one-liner.

### P5 — Both Deferred lists rewritten (D5, D6)

Every entry in 0126 and 0127 now sits under **CLOSED**, **BLOCKED**, **GATED**
or **OPEN**, and every non-closed one names an observable trigger rather than an
intention (C4).

```text
0127  Actions SHA currency        CLOSED   reversed by 0128 D1
0127  environment: sdk ^3.12.2    CLOSED   decision not to act
0127  flutter_secure_storage 11   BLOCKED  trigger: a published 11.0.x
                                           without the compileSdk pin (#1236)
0127  F-KGP                       BLOCKED  trigger: either plugin publishes a
                                           Built-in-Kotlin release
0127  second host                 OPEN     trigger: that host's `make preflight`
                                           fails 0127 D7 gate 2
0126  isNewerBase/NewerPublished  CLOSED   fixed, 0128 P2
0126  NotificationService.dispose CLOSED   fixed, 0128 P3
0126  compute() per save          CLOSED   measured, 0128 P4 — no action
0126  autoRunOnBoot               CLOSED   decision not to act
0126  battery-optimisation prompt GATED    trigger: 0126 P7 row 1 shows
                                           START_STICKY is not enough
0126  P7 rows 1-3                 OPEN     trigger: a paired session
```

Six closed, two blocked upstream with re-checked dates, one gated on evidence
that does not exist yet, and two open with the exact thing that would unblock
them. Nothing left that needs re-reading to know its state — which was the goal.

## Execution summary

| phase | outcome |
|---|---|
| P1 | Dependabot restored, `github-actions` only |
| P2 | published-version compare; local stamp shape aligned with CI |
| P3 | `NotificationService` restartable |
| P4 | measured: ~0.35 ms/save — deferral closed, nothing changed |
| P5 | both lists rewritten as state + trigger |

Suite `+1367 ~3`, up from 1364 at the start of this plan (P2 +1 net, P3 +1;
P4's benchmark is excluded from the default run). Three of the four fixes carry
a test proven to fail first (C1); P4 carries none by design (C2), and its
instrument was itself verified before its numbers were believed.

## Deliberate Go dependency act — 2026-09-02: fsnotify 1.9.0 → 1.10.1

Recorded here because [D1](0128-MADR-triage-the-0126-and-0127-deferred-items.md)
removed the `gomod` ecosystem from `.github/dependabot.yml` with the words
"Dependency currency for Go is a deliberate, recorded act here". This is that
act, so it lives with the policy rather than minting a decision record for a
patch bump.

**Origin.** Dependabot PR #19, opened 2026-09-01 *before* D1 landed and orphaned
by it — the gomod ecosystem no longer runs, so nothing would ever revisit or
retire that PR. Adopted on master by the owner's instruction and the PR closed.

**What changed.** `go.mod` / `go.sum` only:

```text
github.com/fsnotify/fsnotify v1.9.0 -> v1.10.1
```

`fsnotify` is used in exactly one place, `internal/providerauth/watch.go` (plus
its test), which watches the provider-auth directory.

**Verification.**

* `go mod tidy` clean, 3 lines changed across go.mod/go.sum.
* `govulncheck ./...` — "Your code is affected by 0 vulnerabilities." (Two
  vulnerabilities exist in required modules that this code does not call, which
  is the pre-existing baseline and the reason D1 relies on govulncheck's
  *called* analysis rather than presence.)
* `go test -race ./...` — one failure, `TestDiffUsesCWDOnlyAndValidatesSHA`
  (`internal/provider/codex/diff_fork_test.go:88: timeout`).

**That failure is pre-existing and unrelated, confirmed rather than assumed.**
Reproduced on a clean `git worktree` at HEAD — i.e. with fsnotify still at
v1.9.0 and none of this change present — where it fails identically. `codex`
does not import `fsnotify`; only `internal/providerauth` does. Go CI is green on
master, so this is a local, timing-sensitive failure on a machine that had been
running Android emulators for several hours, not a regression from this bump.

**Left open deliberately:** that timeout is not investigated here. It is
someone else's finding, it is not caused by this change, and folding it into a
dependency bump would bury it.
