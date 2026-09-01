---
status: proposed
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

* `scripts/assert-actions-current.sh` (new), `Makefile` — P1
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
* **Re-adding an Actions updater.** D1 is a check. A bot is the thing 0127 D5
  removed.
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

**C3 — The check in P1 does not update anything.** D1. No `--fix`, no PR, no
write outside its own stdout.

**C4 — A trigger names an observable, not an intention.** D5. "When #1236 is in
a published release" is checkable; "revisit later" is not.

**C1 and C2 are the ones at risk** — C1 because four small fixes feel too
obvious to bother; C2 because P4 will produce a number that invites an
immediate fix.

## Dependency and delivery order

P1–P4 are independent and can land in any order or be dropped individually.
P5 is last: it records what the earlier phases actually concluded.

## Implementation Steps

### P1 — An Actions drift check, not a bot (D1)

`scripts/assert-actions-current.sh`. For each `uses: owner/repo@<40-hex> # tag`
in `.github/workflows/*.yml`: resolve the action's latest release, compare the
pinned SHA against it, print one line per action, and summarise.

* Advisory by default (exit 0, drift on stdout) — a stale upstream must not
  redden an unrelated PR.
* `--strict` exits 1 on any drift, for deliberate invocation.
* Degrades cleanly with no network or no `GITHUB_TOKEN` (rate limits): report
  "unknown" per action and exit 0. **Never** report "current" for an action it
  could not resolve — that is the failure mode that makes a check worthless.
* `make actions-current` runs it. **Not** wired into `preflight`: it depends on
  a network call to a third party, and preflight's promise is that green means
  CI will be green.

**Verification (C1/C3):** run it — it must reproduce the MADR's table including
`actions/setup-java … behind`. Then a negative test on a **copy** of the
workflow with one SHA replaced by an older one, asserting the check reports that
action as behind. Confirm `git status` is clean afterwards and that the script
made no writes.

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
cd ../.. && make actions-current
make apk && "$ANDROID_HOME"/build-tools/*/aapt dump badging \
  apps/mobile/build/app/outputs/flutter-apk/app-release.apk | head -1
grep -A5 '^## Deferred' docs/0126-PLAN-*.md docs/0127-PLAN-*.md
```

### Acceptance criteria

1. `make actions-current` reproduces the MADR's table and flags `setup-java`;
   the check has been seen to fail on a mutated copy and writes nothing.
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
* **`actions/setup-java` v5 → v6.** P1 makes the staleness visible; taking a
  major action bump is a separate decision with its own CI blast radius.
* **Wiring the Actions check into CI.** Advisory-by-default is the first step; a
  scheduled job that reports is the obvious follow-on, and needs a decision
  about where its output goes that this record does not have.
