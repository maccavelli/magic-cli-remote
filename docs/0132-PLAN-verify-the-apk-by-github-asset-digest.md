---
status: in-progress
date: 2026-09-02
associated-madr: "0132-MADR-verify-the-apk-by-github-asset-digest.md"
---

# Implement: verify the downloaded APK by GitHub's per-asset digest

Associated MADR: [0132-MADR-verify-the-apk-by-github-asset-digest.md](0132-MADR-verify-the-apk-by-github-asset-digest.md)

## Goal

Make the Android in-app updater verify and install the **already-published,
immutable** `v0.16.0` APK, by taking the expected SHA-256 from the release
asset's `digest` field and falling back to the `SHA256SUMS` lookup only when
that field is absent. Failure to obtain an expected hash, and any hash
mismatch, must still abort and delete the partial download.

## Scope

**In scope — two source files and two test files:**

* `apps/mobile/lib/data/update/app_update.dart`
* `apps/mobile/lib/features/settings/app_update_tile.dart`
* `apps/mobile/test/app_update_test.dart`
* `apps/mobile/test/app_update_tile_test.dart`

**Explicitly out of scope:**

* `.github/workflows/ci.yml` — unchanged. The release pipeline is not the
  repair path for an immutable release, and the chosen option needs nothing
  from it.
* mcplib and its `verify-selfupdate-release.sh` contract — unchanged.
* The Go self-update path (`internal/updateclient`, `internal/update`) and the
  installers — they consume `SHA256SUMS` over the canonical binaries, which is
  correct and unaffected.
* The `v0.16.0` bridge assets.
* Publishing a new release or tag. Phase 4 sideloads a locally built APK only.

## Implementation Steps

### Phase 1 — carry the digest through the check result

`apps/mobile/lib/data/update/app_update.dart`

1. Add `final String? digest;` to `UpdateAsset`, defaulting to `null` in the
   constructor, holding the **bare lowercase hex** — strip a leading `sha256:`
   and lowercase on ingest; treat any other algorithm prefix as unusable and
   store `null`.
2. In `_pickApk` (`:154`), read `a['digest'] as String?` and pass it through
   the normaliser from step 1.
3. Tighten `_pickSums` (`:168`): prefer an asset named exactly `SHA256SUMS`;
   only if none exists fall back to the first `SHA256SUMS*` match. This removes
   the ordering dependence recorded in the MADR's "second, latent defect".

Commit at the end of the phase.

### Phase 2 — prefer the digest, keep `SHA256SUMS` as the fallback

`apps/mobile/lib/data/update/app_update.dart`

4. Change `downloadAndVerify` to take `UpdateAsset? sums` (nullable) and
   resolve the expected hash before touching the network:
   * if `apk.digest != null`, that is the expected hash — **do not fetch the
     sums asset at all**;
   * else if `sums != null`, fetch it as today and call `sha256For`;
   * else, or when the fetched manifest has no matching line, throw
     `AppUpdateException('no checksum for ${apk.name}')`.
5. Leave the streaming download, the chunked hash, the mismatch branch and the
   `dest.delete()` on failure exactly as they are. Verification strength is
   unchanged; only the source of the expected value moves.

`apps/mobile/lib/features/settings/app_update_tile.dart`

6. Relax the gate at `:87` from `r.apk == null || r.sums == null` to
   `r.apk == null`, and pass `sums: r.sums` (nullable) at `:110`. Without this
   the tile still refuses `v0.16.0` before `downloadAndVerify` is ever called.

Commit at the end of the phase.

### Phase 3 — tests, including the negative test run against unfixed code

`apps/mobile/test/app_update_test.dart`

7. `digest is preferred over SHA256SUMS`: asset carries
   `"digest": "sha256:<hash of the fixture bytes>"`, and the fake `http.Client`
   **asserts it is never asked for the sums URL**. Verification succeeds.
8. `falls back to SHA256SUMS when digest is absent`: no `digest` key; the
   existing manifest path still verifies. Guards the older-release case.
9. `mismatched digest aborts and deletes the file`: digest is a valid-shape
   hash that is not the body's. Expect `AppUpdateException` containing
   `sha256 mismatch`, and assert the destination file does **not** exist.
10. `no digest and no sums entry fails closed`: expect the
    `no checksum for …` message; assert no file is left behind.
11. `regression: the real v0.16.0 asset shape`: a fixture asset list of one
    `.apk` with a `digest`, plus `SHA256SUMS` and `SHA256SUMS-0.16.0` in that
    order with **neither listing the APK** — mirroring the live release. Assert
    the download verifies.

`apps/mobile/test/app_update_tile_test.dart`

12. Widget test: a release whose only checksum source is the APK's `digest`
    reaches `Ready to install`, not `No APK asset on this release`.

**Proving the instrument (global rule: a check is not trusted until it has been
seen to fail).** Before landing the source changes — or with them stashed to a
scratch copy, never by dirtying the tree with `git checkout` afterwards:

* Run test 11 against **unmodified** `app_update.dart`. It must fail with
  `no checksum entry for magic-cli-remote-v0.16.0-arm64.apk` — the user's exact
  reported string. Record the full failure output in this plan's execution
  record, read in full rather than through `head`/`grep`.
* Run test 9 against a deliberately broken build in a **scratch copy** of the
  tree (`cp -R` to the scratchpad, edit there) in which the mismatch branch is
  neutered, and confirm it fails there. This proves the mismatch assertion is
  load-bearing rather than passing because verification is skipped.

Commit at the end of the phase.

### Phase 4 — end-to-end on the device, against the live release

13. `make apk` (or the repo's local debug/release build), stamped below
    `0.16.0` so the tile offers the update.
14. Sideload once by hand. **This is unavoidable**: the installed build cannot
    contain the fix that repairs its own update path. Say so in the handoff.
15. In Settings → app update: check, download, verify, install `v0.16.0` from
    the live GitHub release. Capture the tile's status text at each stage.
16. Record the result in this plan's execution record, including the installed
    `versionName` afterwards.

## Verification

Run from `apps/mobile`:

```bash
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test test/app_update_test.dart test/app_update_tile_test.dart
flutter test
```

Independent confirmation that the data the fix relies on is really there:

```bash
gh api repos/maccavelli/magic-cli-remote/releases/latest \
  --jq '.assets[]|select(.name|endswith(".apk"))|{name,digest}'
```

`dart format` runs first, before every commit: CI fails on it ahead of the
analyzer.

### Acceptance criteria

* The four Dart commands above pass with no findings.
* Test 11 fails with `no checksum entry for magic-cli-remote-v0.16.0-arm64.apk`
  against unmodified `app_update.dart`, and passes after Phase 2 — both
  observed, both recorded.
* Test 9 fails in the scratch copy with the mismatch branch neutered — observed
  and recorded.
* On the device, `v0.16.0` downloads, verifies and installs from the live
  release; the tile then reports `Up to date (0.16.0)`.
* `git diff --stat` touches only the four files named in Scope.

## Rollout and Rollback

**Rollout.** The change ships in the next APK. The already-installed build must
be updated once by hand, because it predates the fix; from that build onward
in-app updates work again, including for `v0.16.0` itself. No release needs to
be republished, no tag needs to be cut for the fix to take effect, and no
GitHub release asset changes.

**Rollback.** Revert the commits from Phases 1–3. The `SHA256SUMS` path is left
intact throughout, so a revert restores exactly today's behaviour — including
today's failure on `v0.16.0`.

**Deferred, and deliberately not done here.** Verifying the APK against its
build-provenance attestation, which mcplib's publish workflow already produces,
would be stronger than either checksum source. It needs its own MADR: it is a
new trust model for the client, not a bug fix.

## Execution Record

### Phases 1-3 — 2026-09-02

Approved and executed on 2026-09-02. Landed in one commit rather than three; see
the deviation note below.

**Source.** `lib/data/update/app_update.dart`: `UpdateAsset` gained
`digest`, `_pickApk` populates it through a new `normalizeDigest`,
`_pickSums` now prefers the asset named exactly `SHA256SUMS`, and
`downloadAndVerify` takes a nullable `sums` and resolves the expected hash from
the digest first — skipping the manifest request entirely when it has one.
The mismatch branch, the delete-on-failure and the streaming single-pass hash
are untouched. `lib/features/settings/app_update_tile.dart`: the gate no longer
requires `r.sums`, and passes it through nullable.

The "no checksum entry for X" message became "no checksum for X" — it is now
raised when *no source at all* yields a hash, not when a manifest lacks a line,
and the old wording named a cause that is no longer the only one.

**The instruments, seen to fail.** Test 11 was written and run **first, against
unmodified `app_update.dart`**, and failed with the field symptom exactly:

```text
00:00 +9 -1: 0132 regression: the real v0.16.0 asset shape the APK verifies from its digest alone [E]
  no checksum entry for magic-cli-remote-v0.16.0-arm64.apk
  package:magic_cli_remote/data/update/app_update.dart 116:7  AppUpdateService.downloadAndVerify
```

The remaining assertions were then proved load-bearing against a **deliberately
broken scratch copy** of the package (`lib`, `test`, `assets`, pubspec copied to
the scratchpad; the working tree was never dirtied and no `git checkout` was
run). Two edits there, both asserted to have landed before the run: the sha256
mismatch branch was short-circuited (`if (false && got != want)`), and the tile
gate was restored to its pre-0132 `|| r.sums == null` form. Three tests failed
in that copy and only those three:

```text
Failing tests:
  .../test/app_update_test.dart: 0132 digest-first verification a mismatched digest aborts and discards the file
  .../test/app_update_test.dart: downloadAndVerify checksum mismatch discards file
  .../test/app_update_tile_test.dart: a digest-only release downloads and verifies
    Expected: no matching candidates
      Actual: Found 1 widget with text containing No APK asset
```

The second is the pre-existing mismatch test, which failing confirms the neuter
actually took effect rather than the run silently skipping it. The scratch copy
was deleted afterwards.

**Verification on the real tree.**

```text
dart format --output=none --set-exit-if-changed lib test   -> Formatted 208 files (0 changed)
flutter analyze                                            -> No issues found! (ran in 4.8s)
flutter test test/app_update_test.dart                     -> +17 All tests passed!
flutter test test/app_update_tile_test.dart                -> +4  All tests passed!
flutter test                                               -> +1397 ~3: All tests passed!
```

### Deviations

**2026-09-02 — Phases 1-3 landed as one commit, not three.** The plan says
"Commit at the end of the phase" for each. Phase 3 requires test 11 to be run
against **unmodified** source, which inverts the order: the test had to be
written before the Phase 1 and 2 edits existed. Committing after that would have
recorded a tree with a knowingly failing test, and committing Phases 1-2 first
would have destroyed the chance to observe the failure. One commit covering the
three phases is what was actually built. No scope, file list or behaviour
changed — the four files in Scope are the only ones touched.

**2026-09-02 — the tile widget test needed `runAsync`.** Step 12 as written
assumed `pumpAndSettle()` would carry the tile to `Ready to install`. It does
not: the download's tail is real file I/O (`sink.close()`), which the widget
tester's fake-async zone never drains, so the tile parks on "Downloading… 100%"
indefinitely. Observed directly by dumping the rendered `Text` widgets. The tap
and the wait are therefore driven inside `tester.runAsync`. This is a
test-harness fact, not a product defect — the same download completes correctly
in the non-widget tests, and on the device. Note for whoever writes the next
one: the pre-existing 0126 test in the same file sidesteps this by asserting on
a synchronously created directory rather than on the final state.

**2026-09-02 — the scratch copy needed `assets/`.** `flutter test` builds the
asset bundle and fails on the `pubspec.yaml` asset entries, so `lib`, `test`,
pubspec and `analysis_options.yaml` alone were not enough. Recorded so the next
negative-test run does not rediscover it.

### Phase 4 — not yet done

Device end-to-end against the live `v0.16.0` release is outstanding.
