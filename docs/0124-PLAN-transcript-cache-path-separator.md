---
status: in-progress
date: 2026-08-29
associated-madr: "0124-MADR-transcript-cache-path-separator.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0124 — Separator-agnostic entry names in the transcript cache

Implements [0124-MADR-transcript-cache-path-separator.md](0124-MADR-transcript-cache-path-separator.md)
decisions D1–D7, closing findings F1–F7.

## Goal

`flutter test` is green on a Windows host, and the six tests that prove it were
not touched to make it so.

Finish line:

* `transcript_cache.dart` takes a basename that works with either separator;
* all six previously-failing tests pass **unmodified**;
* a new test fails on Linux if the separator assumption returns;
* CI runs the Flutter suite on Windows, so the next such defect is caught by
  something other than a developer noticing;
* no user-visible behaviour changes on any shipped platform.

## Scope

### In scope (the only files any phase may touch)

* `apps/mobile/lib/data/chat/transcript_cache.dart` — the basename helper and
  its one caller
* `apps/mobile/test/transcript_cache_test.dart` — **additions only** (D3's
  guard). No existing test is edited.
* `.github/workflows/ci.yml` — the `flutter` job only, gaining a Windows
  test leg (D6). No other job is touched.

### Out of scope

* **The six existing tests' assertions** (D2). They describe correct
  behaviour. Editing one to go green would mean the fix is wrong, and the
  temptation is real because five of them fail on a shared root cause.
* **Adopting the `path` package** and sweeping other path expressions (D5,
  option B). Right in the long run, oversized here.
* **`workspace_sheet.dart`'s `parentOf`** — investigated and **left alone**
  (D7, F6). The daemon normalises workspace paths to relative slash form on
  both ingress and egress, so splitting on `/` reads the contract correctly.
  Editing it would assert distrust of a wire format the app trusts everywhere
  else. **No phase may touch that file**; a diff there is a failed plan.
* **The five CRLF-stale test files** that `dart format` keeps rewriting on this
  host. Unrelated, and 0123 already names the likely one-line fix.

## Stability rule

Every phase ends with:

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed lib/data/chat/transcript_cache.dart test/transcript_cache_test.dart
flutter analyze
flutter test
```

then **one commit** (`git commit --no-edit`; never `-m`).

**Run the whole suite, not just the six.** The point of the phase is the total,
and a fix to shared enumeration code can move something else.

**`flutter analyze` locally is not authoritative.** This host runs Flutter
3.47.1 against CI's pinned 3.44.8, and the newer analyzer misses lints CI
enforces — it cost one red run during 0123. Local green means "probably";
CI green means green.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — The six tests are not edited.** D2. This is the whole proof.

**C2 — No behaviour change on `/` platforms.** On Android, iOS and Linux the
new basename must return exactly what `split('/').last` returned. A fix that
alters shipped behaviour has exceeded a bug fix.

**C3 — The fix is one concept in one place.** No second helper, no normalising
elsewhere, no defensive re-parsing at call sites.

**C1 is the one at risk.** Five of the six fail through the same call, so a
change that makes `retainOnly` merely *tolerant* of a mangled id would turn
them green without fixing anything — and the sixth, in `history_replay_test`,
would likely follow. Green is necessary here, not sufficient: the fix must make
`_storedIds` return `s1`, not make its callers cope with `transcripts\s1`.

## Implementation Steps

### P1 — Separator-agnostic basename (D1, D3; closes F1, F2, F5)

`transcript_cache.dart`. Replace `e.path.split('/').last` with a basename that
splits on `[\\/]`, as a small named helper so the intent is stated once and can
be tested directly (C3).

Then D3's guard, added to `transcript_cache_test.dart` **alongside** the
existing tests: feed the helper a backslash-separated path and assert a bare
id comes back. That test must fail on Linux against the old code — otherwise it
guards nothing that CI can see, which is exactly how F5 happened.

**Verification:**

```bash
cd apps/mobile
flutter test test/transcript_cache_test.dart test/history_replay_test.dart
flutter test                       # whole suite
git diff --stat apps/mobile/test/transcript_cache_test.dart
```

The last command must show **additions only**. Any deletion in that file is C1
violated.

Prove the guard bites before trusting it: revert the one-line fix, confirm the
new test fails, restore the fix. A guard never seen red is a guess.

### P2 — A Windows leg on the Flutter lane (D6; closes F5)

`.github/workflows/ci.yml`, the `flutter` job only.

Add a Windows runner that executes **`flutter test` and nothing else**. Format
and analyze stay on Linux: F7 says they answer identically anywhere, so a
second copy adds no signal and one more way to go red.

Grounded on what the file already does:

* `shell: bash` set explicitly, as `go-native` (`ci.yml:275`) and
  `smoke-native` (`:341`) both do on `windows-latest` — the runner defaults to
  PowerShell and `defaults.run` in the existing job does not set a shell;
* the same pinned `FLUTTER_VERSION` and `subosito/flutter-action` SHA as the
  Linux leg, so the two lanes cannot drift in toolchain;
* `working-directory: apps/mobile`, matching the existing job's `defaults`.

**This phase must land after P1**, not before: a Windows leg added first would
go red immediately on a defect the next commit fixes, which is how a new lane
earns a reputation for noise in its first hour.

**Verification:** push, and read the run. The leg must be **green on the fixed
tree**. Then, to prove it can actually see the class of bug it exists for,
confirm it goes **red with P1's fix reverted** — either on a scratch branch or
by reasoning from P1's local revert, and say which was done. A lane never
observed failing is decoration.

Open questions 3 and 4 get answered here from the run's own log: whether
`dart format`'s absence was the right call (it is not run, so this is about
whether the checkout is LF as `.gitattributes` requires), and whether
`cache: true` actually caches the SDK on `windows-latest` or the leg pays a
full download.

## Verification (whole plan)

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test          # expect 0 failing on this Windows host
```

```text
six named tests   -> pass, unmodified
new guard         -> fails without the fix, passes with it
git diff          -> lib: one helper + one call site; test: additions only
```

### Acceptance criteria

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | All six previously-failing tests pass | F1, F2 |
| A2 | None of the six was edited | D2, C1 |
| A3 | `_storedIds` returns bare ids, not path fragments | D1, C1 |
| A4 | A guard fails on Linux if `split('/')` returns | D3, F5 |
| A5 | Behaviour on `/` platforms is byte-identical | C2, F3 |
| A6 | One helper, one call site; no sweep | C3, D5 |
| A7 | Whole suite green on this host, not just the six | — |
| A8 | CI runs `flutter test` on `windows-latest`, green | D6, F5 |
| A9 | The Windows leg runs tests only; format/analyze stay on Linux | D6, F7 |
| A10 | `workspace_sheet.dart` is unchanged | D7, F6 |

**A2 is the one to guard**, for the reason under C1: the cheapest route to
green runs straight through the tests that are telling the truth.

**A10 is second, and it is a guard against helpfulness.** `parentOf` splits on
`/` and looks exactly like the bug this record fixes. It is not one, and the
temptation while "fixing path handling" is to normalise it too. Doing so would
add distrust of a wire contract at one call site while every other consumer
trusts it — inconsistency dressed as robustness.

## Rollout and Rollback

No user-visible change on any shipped platform (F3), no migration, no persisted
format change. One `git revert` undoes it.

## Deferred (named, so they are not mistaken for oversights)

* **A Windows leg on the Flutter CI lane** (open question 1). Nothing but a
  developer's own machine can currently catch a Windows-only Flutter
  regression, and this defect survived two records precisely because of that.
  Standard runners are free on public repositories, so the objection is wall
  time, not money — a CI-policy call.
* **`workspace_sheet.dart:136` `parentOf`** (open question 2). Splits remote
  paths on `/`, and the daemon now ships `windows/amd64`. Plausibly the same
  bug against a Windows *host*; unverified, and a different subsystem.
* **Adopting `path` / `basename` app-wide** (D5). The right long-term shape,
  deliberately not bundled into a one-line fix.
