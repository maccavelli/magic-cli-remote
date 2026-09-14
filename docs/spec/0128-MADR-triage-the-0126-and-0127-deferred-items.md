---
status: accepted
date: 2026-09-01
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Take four of the ten deferred items now, and give the rest named triggers instead of a backlog

## Context and Problem Statement

[0126](0126-MADR-android-client-debugging-pass-findings.md) and
[0127](0127-MADR-adopt-current-flutter-toolchain.md) each closed with a
*Deferred (named, so they are not mistaken for oversights)* list. Ten entries
between them. Left as prose in two execution records they will decay into the
thing the heading was written to prevent.

Each was re-checked on 2026-09-01 rather than triaged from its description.

### Actionable now

**A — GitHub Actions SHA pins have no updater.** 0127 D5 deleted
`.github/dependabot.yml`, whose own comment called it *"the only practical way
to run SHA pins without them rotting"*. `ci.yml` pins 7 distinct actions across
19 sites. Measured against each action's latest release:

```text
action                     pinned   latest    status
actions/checkout           v7.0.1   v7.0.1    current
actions/download-artifact  v8       v8.0.1    current
actions/setup-go           v7.0.0   v7.0.0    current
actions/setup-java         v5       v6.0.0    109 commits behind
actions/setup-node         v7.0.0   v7.0.0    current
actions/upload-artifact    v7.0.1   v7.0.1    current
subosito/flutter-action    v2       v2.23.0   current
```

Six of seven are current; one is a major behind. So the risk is real but small
today — which makes this the right moment to decide the mechanism, before it is
six of seven stale and the decision is made under pressure.

**B — the phone's update comparison, and a shape divergence this pair
introduced.** 0126 F8's closing note: `AppUpdateService.isNewerBase`
(`app_update.dart:28-35`) mirrors Go's `NewerBase`, which nothing calls any more
— `update/run.go:81` moved to `NewerPublished` under MADR 0103. All 97 release
tags are three-part, so base-only comparison is correct today and becomes wrong
the first time a four-part tag is published.

Found while re-checking it, and **introduced by 0126 P6**: local and CI APKs are
now stamped in different shapes.

```text
CI        --build-name="${{ needs.go.outputs.version }}"   -> 0.15.3.2  (4-part)
make apk  --build-name="${VER%.*}"                         -> 0.15.3    (3-part)
```

`make apk` copied the split from `scripts/build-apk.sh`, which has always
diverged from CI this way; P6 propagated it rather than inventing it, but P6 is
what put it in the Makefile. `ci.yml:631` states the intent plainly — *"versionName
is the full Go build version … so the APK and the binaries in the same release
agree"* — and a local build no longer does. It is harmless for `isNewerBase`
(both parse to the same three-part base) and stops being harmless the moment B's
first half is fixed, because a locally built APK carries no serial in its
`versionName` to compare.

**C — `NotificationService` cannot be restarted after `dispose()`.**
`dispose()` (`notification_service.dart:515-517`) closes the `_responses`
broadcast controller but leaves `_ready == true`, and `init()` opens with
`if (_ready) return;`. A `start()` after a `dispose()` therefore skips
re-initialisation and every `show*` adds to a closed controller. Unreachable
today because `NotificationCoordinator` is app-lifetime — but the coordinator's
own `dispose()` already nulls its subscriptions *specifically* so a later
`start()` re-subscribes (`notification_coordinator.dart:119-123`), so one half
of that pair supports restart and the other silently does not.

**D — the transcript cache spawns an isolate per debounced save.**
`compute()` at `transcript_cache.dart:339` (save) and `:383` (load). `compute`
spawns a fresh isolate per call. Saves are debounced at
`kTranscriptCacheDebounce` = 400 ms **per session**, so an actively streaming
session can reach ~2.5 isolate spawns/second, each encoding up to
`kTranscriptCacheMaxItems` = 150 chat items.

This is the one item with no measurement behind it. 0126 deferred it saying
exactly that — *"noted during the pass, not measured, and a performance question
rather than a defect"*. It stays a question until someone measures it, and this
record's decision for D is to measure, not to change anything.

### Blocked upstream, re-checked today

**E — `flutter_secure_storage 11.0.0`.** Still gated on
[PR #1236](https://github.com/juliansteenbakker/flutter_secure_storage/pull/1236)
(*"use flutter.compileSdkVersion instead of pinning compileSdk"*): re-queried
2026-09-01, **still open, not merged**, last updated 2026-08-30. 11.0.0 remains
the newest published version. The full analysis is in
[0066](0066-MADR-secure-storage-upgrade-resilience.md)'s amendment.

**F — F-KGP.** `mobile_scanner` 7.4.0 (published 2026-07-20) and
`speech_to_text` 7.4.0 (2026-05-19) are both still the newest published stable,
and neither has a Built-in-Kotlin release. Nothing to take.

### Not actionable here

**G — battery-optimisation exemption prompting.** 0126 gates it explicitly:
*"if P7 row 1 shows START_STICKY alone is not enough, it earns its own record."*
0126 P7 row 1 is still **open** — it needs a paired session, and pairing needs a
QR aimed through the emulator's virtual-scene camera, which cannot be driven
from adb. Taking this now would be adding a UX intrusion to fix a problem not
yet shown to exist.

**H — the second host.** 0124 ran on Windows at Flutter 3.47.1. 0127 D7's
gate 2 will tell that host's operator on their next `make preflight`. Nothing
this repository can do from here.

### Not deferred work at all

**I — `autoRunOnBoot` and raising `environment: sdk:`.** Both are settled
decisions *not* to act, recorded under a "Deferred" heading that makes them look
like pending work. They are closed, and this record says so rather than leaving
them to be rediscovered as a to-do.

## Decision Drivers

* A named deferral is only better than an oversight while someone re-reads it;
  ten of them across two execution records will not be re-read.
* Three of the four actionable items are small and independent; one (D) is a
  question that must not be answered by changing code first.
* The owner has removed dependabot deliberately, so A's answer must not
  reintroduce a bot by another name without saying so.
* 0126 P7 is still open, and G depends on its result.

## Considered Options

* **Take A–D now; convert E–H into triggers; close I.**
* **Take everything that is not upstream-blocked**, including G.
* **Record the triage only**, and schedule the work later.
* **Close the lists as won't-do**, keeping only E and F.

## Decision Outcome

Chosen option: **"Take A–D now; convert E–H into triggers; close I"**, because
it is the only option that distinguishes the four kinds of item on the lists —
work, blocked work, work gated on an unfinished verification, and decisions that
were never work. Taking everything would pull G forward past the evidence that
justifies it; recording only would leave the same prose in a third document.

### Decisions

* **D1 — Dependabot is restored for `github-actions` only.** `pub` and `gomod`
  stay deleted. This amends [0127](0127-MADR-adopt-current-flutter-toolchain.md)
  D5, which deleted all three because one failed.

  **Revised 2026-09-01**, after the owner asked what best practice actually is.
  The first version of D1 proposed a drift *check* rather than a bot, reasoning
  from the instruction to turn Dependabot off rather than from the evidence.
  Both the guidance and the local measurement point the other way:

  * [OpenSSF Scorecard's `Pinned-Dependencies` check](https://github.com/ossf/scorecard/blob/main/docs/checks.md)
    names this exact trade — *"pinning dependencies can inhibit software
    updates, either because of a security vulnerability or because the pinned
    version is compromised. Mitigate this risk by: using automated tools to
    notify applications when their dependencies are outdated; quickly updating
    applications that do pin dependencies."* A check satisfies the first clause
    and delegates the second to whoever reads it.
  * [GitHub's Actions security-hardening guidance](https://docs.github.com/en/actions/reference/security/secure-use)
    is more direct: *"Using Dependabot version updates to keep actions up to
    date … You can use Dependabot to ensure that references to actions and
    reusable workflows used in your repository are kept up to date."*
  * The measurement in this record shows the `github-actions` ecosystem
    **succeeding**: 6 of 7 pins current, and `actions/setup-java` sitting one
    major behind is the deleted config's own policy ("action majors get reviewed
    by hand before merge") working as written, not rot.

  So the deletion treated a `pub`-specific, structural failure as a
  Dependabot-wide one. Restoring the one ecosystem that worked keeps the
  protection 0127 D5 gave up, keeps `pub` off permanently, and leaves 0127 D7
  gate 1 in place to catch a bad lockfile if a bot ever proposes one again.
  `gomod` stays off on its own merits: `govulncheck` in the pre-add gate already
  covers *called* vulnerabilities, which is the case that matters for a Go
  binary.
* **D2 — The phone compares published versions, not bases**, matching
  `update/run.go` under MADR 0103, so a four-part tag cannot silently withhold a
  phone update. **And `make apk` stamps the same four-part `versionName` CI
  does** — the divergence 0126 P6 introduced is closed in the same change,
  because fixing the comparison while local builds carry no serial would leave
  the fix untestable locally.
* **D3 — `NotificationService.dispose()` leaves the object restartable**, so
  both halves of the coordinator/service pair agree about whether restart is
  supported.
* **D4 — D is measured before anything is decided.** A benchmark, committed, and
  the numbers recorded. No change to `compute()` usage lands in this record
  regardless of what it shows: if the cost is real it earns its own decision
  alongside MADR 0084's other measurements, and if it is not, the deferral is
  closed with evidence.
* **D5 — E, F, G and H get written triggers**, in the records that own them,
  naming the exact observable that reopens each. A trigger is checkable; "revisit
  later" is not.
* **D6 — I is closed.** `autoRunOnBoot` and the SDK constraint are decisions not
  to act and are moved out of the Deferred lists so they stop reading as work.

### Consequences

* Good, because the ten-item backlog becomes four commits, four triggers and two
  closures — nothing left that needs re-reading to understand its state.
* Good, because D1 restores the protection 0127 D5 gave up, on the ecosystem
  that measurably worked, and records the partial reversal in both records
  rather than leaving 0127 D5 reading as still-current.
* Good, because D2 closes a divergence this pair created, which is the kind of
  thing that otherwise ages into a puzzling difference between local and CI
  artifacts.
* Bad, because D1 reverses part of a decision the owner made explicitly, and
  reintroduces a monthly PR plus the CI run it triggers — the Actions-minutes
  cost the deleted config's own comment called the reason for its monthly
  cadence. Kept monthly and grouped for exactly that reason.
* Bad, because Dependabot proposes action *majors* too. The restored config
  groups them, and the standing policy that majors are reviewed by hand before
  merge is what stops a grouped PR becoming an unreviewed major bump.
* Bad, because D4 spends a phase producing a number that may say "no action
  needed". That is the cost of not guessing.
* Neutral, because A–D touch four unrelated areas and can land or be dropped
  independently.

### Confirmation

1. `.github/dependabot.yml` exists, declares **only** `github-actions`, parses
   as YAML, and its schema is accepted by GitHub (the file is validated on push,
   so a malformed one is silent — check the repository's Dependabot page).
2. A four-part remote tag against a four-part local version is compared on the
   serial, proven by a unit test that fails under the old `isNewerBase`.
3. `make apk` and CI produce the same `versionName` shape, checked with
   `aapt dump badging`.
4. A `NotificationService` `dispose()` → `init()` → `show*` sequence does not
   throw, proven by a test that fails before the fix.
5. The `compute()` benchmark's numbers are recorded in `0128-PLAN`, whatever
   they are.
6. Every remaining deferred entry in 0126 and 0127 names a trigger or is marked
   closed.

## More Information

* Sources: [0126-PLAN](0126-PLAN-android-client-debugging-pass-findings.md) and
  [0127-PLAN](0127-PLAN-adopt-current-flutter-toolchain.md) Deferred sections.
* [0103](0103-MADR-update-tracks-release-build-and-active-service.md) is the
  record D2 brings the phone into line with.
* [0066](0066-MADR-secure-storage-upgrade-resilience.md)'s 2026-09-01 amendment
  holds E's analysis and revisit trigger.
* [0084](0084-MADR-android-app-hardening-and-performance.md) is where D's
  measurement belongs if it turns out to matter.
* 0126 P7 rows 1–3 remain open and are **not** part of this record; G is gated
  on row 1.
