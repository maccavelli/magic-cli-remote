# MADR 0065 — Implementation plan: update automation

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: **Implemented** (software P0–P6, 2026-08-05). Host:
  `internal/update` + service control + `mcremote`/`mcrelay update`
  (`--check` exit 10 via main, `--yes`, `--force`), U1–U4 unit coverage.
  Phone: Settings **App update** tile, streaming verify, MethodChannel
  `mcremote/app_update`, FileProvider + PackageInstaller session +
  MY_PACKAGE_REPLACED notification. Hardware U5–U11 in
  [ops-hardware-validation.md](ops-hardware-validation.md) Part D.
  **The 0066 phone gate remains satisfied** (E1/E2 ✔ 2026-08-03).
- **Date**: 2026-08-02
- **Source**: [0065-MADR-update-automation.md](0065-MADR-update-automation.md)
  (D1–D6 + phone deep-dive §2; open questions 1–5, 7 pending — see §6 of
  this plan for the ones that gate phases)
- **Prerequisites**: **all complete as of 2026-08-02** — stable upload
  keystore in CI with fail-closed provisioning and a pinned signer digest
  (`ANDROID_RELEASE_CERT_SHA256`); first signed release **v0.6.6**
  published; developer identity verified and
  `com.maccavelli.magic_cli_remote` registered
  ([ops-android-signing.md](ops-android-signing.md)).

Line anchors reference the working tree at the time of writing and will
drift.

---

## 0. Grounding (verified against the tree)

### 0.1 Reused as-is

| Need | Where it already exists |
|---|---|
| Release discovery | Public repo; `GET /repos/maccavelli/magic-cli-remote/releases/latest`, unauthenticated (60/hr/IP) |
| Deterministic assets | `mcremote-<os>-<arch>-<BASE.N>`, `mcrelay-…`, `magic-cli-remote-<tag>-arm64.apk`, `SHA256SUMS-<BASE.N>` (verified on v0.6.4 and v0.6.6) |
| Version identity | `main.version` ldflags → `cli.VersionString()`; release = clean `BASE.N`, dev builds carry `.g<hash>` suffix |
| Swap semantics + their regression test | `scripts/install-binary.sh` + `scripts/install-binary_test.sh` (stage-before-stop, rename, `.prev`, trap-restore keyed on *enabled*, `wait_for_teardown`/`wait_for_up`) |
| Service model in Go | `internal/cli/service`: `Options`, `Setup`, `Remove`, `RenderUnit`, `LaunchdLabel`, and — crucial for testing — the **`OverrideRunLaunchctl` / `OverrideRunSystemctl` injection seams** (`setup.go:202,215`) |
| CLI surfaces | `mcremote` root (`internal/cli/root.go:84-89`) and `mcrelay` root (`internal/relay/cli.go:53`) are both cobra trees with `version` commands |
| Health endpoints | mcremote `/healthz` (default `127.0.0.1:7531`, but `listen.host` may be `tailscale`); mcrelay `GET /healthz` (`internal/relay/server.go:90`) |
| Phone HTTP + hashing + version | `http ^1.6.0`, `crypto ^3.0.6`, `package_info_plus` already in `pubspec.yaml` |
| Kotlin host for a channel | `MainActivity.kt` exists (`android/app/src/main/kotlin/com/maccavelli/magic_cli_remote/`) |
| Settings as the home (0064) | Connection section already carries device-scoped actions; `showTopNotification` for outcomes |

### 0.2 Gaps (to build)

1. No `internal/update` package: discovery, version compare, download,
   checksum verify, swap, service cycle, rollback.
2. No `update` subcommand on either CLI.
3. `internal/cli/service` has no start/stop/is-active primitives — `Setup`
   bootstraps and `Remove` tears down, but the update cycle needs
   `Stop`/`Start`/`IsActive` with the same override seams.
4. Phone: no update checker, downloader, verifier, or install channel; no
   `FileProvider` declared in the app manifest (nothing merged one either
   — verified by grep); no install permissions.
5. No plan-level tests for any of it.

### 0.3 Findings that shape the plan

#### F1 — The service seams make the incident test portable to Go

`install-binary_test.sh` works by stubbing `launchctl` on `PATH` and
replaying the exact incident (a `SIGTERMed` unit misread as running).
`internal/cli/service` already exposes `OverrideRunLaunchctl` /
`OverrideRunSystemctl` for precisely this style of test — so the Go port of
the swap logic can replay the same incident **in `go test`, cross-platform,
without a real service manager**. The shell test stays as the guard on the
shell script; the Go test guards the Go port. Drift between them is checked
by both replaying the same scripted sequence (P1 acceptance).

#### F2 — Post-restart health is service-state first, HTTP second

`wait_for_up` in the shell script asserts unit state, not HTTP — and that
is the right primary signal for the updater too: the daemon's listen host
may be `tailscale` (not loopback), TLS may be self-signed, and the relay
may bind a public interface. **P1 asserts unit state; an HTTP `/healthz`
probe is attempted only when the listen address is resolvable from local
config, and its failure downgrades to a warning** (the unit being up and
stable is the rollback criterion, matching `install-binary.sh`).

#### F3 — The updater cannot hardware-test itself until the *next* release

The first release that ships `update` has nothing older to update *from*.
Unit tests cover the machinery against a stubbed API (P0–P2); the hardware
row (U5) becomes runnable only when the release *after* the feature
exists. The plan therefore ships the Go updater in one release and
schedules U5 against the following tag — not a blocker, a sequencing fact.

#### F4 — Self-relaunch after APK install is notification-shaped, not activity-shaped

The MADR's "optional polish: `MY_PACKAGE_REPLACED` receiver relaunches the
app" collides with Android's background-activity-start restrictions
(receivers cannot start activities from the background since Android 10).
The honest v2 shape: the receiver posts a **local notification** — "Updated
to \<version\> — tap to open" — through the app's existing notification
plumbing. One tap instead of zero; still strictly better than the user
wondering whether the update took.

#### F5 — `--check` needs a distinct exit code to be scriptable

`mcrelay update --check` over SSH is the obvious relay workflow. Exit
codes: `0` = up to date, `10` = update available, `1` = error. Documented
in the command help; keeps D6's "no background automation" while letting
an operator script the *check*.

#### F6 — Asset `VER` is discovered, not computed

The updater knows the release tag (`v0.6.7` → base `0.6.7`) but not the
build serial `N` in `0.6.7.N`. Resolution: pick the asset matching
`<product>-<GOOS>-<GOARCH>-<base>.` prefix, extract `VER` from its name,
and fetch `SHA256SUMS-<VER>` to match. Exactly one asset per
product/os/arch exists per release (verified v0.6.4, v0.6.6); zero or
multiple matches abort with a clear error.

### 0.4 Out of scope (per MADR D6 + open questions)

Windows; delta updates; background/scheduled updates; prerelease channel
(open Q4 — the API path is `releases/latest`, which never returns
prereleases); minisign signing (open Q5 — phase 2; P0 leaves a `Verifier`
seam); updating the *other* product; Play distribution.

---

## 1. Target architecture

### 1.1 Go: `internal/update`

```go
// Discovery (stdlib net/http + encoding/json only; no new deps)
type Release struct { Tag, Base string; Assets []Asset }
type Asset   struct { Name, URL string; Size int64 }
func Latest(ctx, base string) (Release, error)         // base URL injectable for tests
func (r Release) AssetFor(product, goos, goarch string) (Asset, string /*VER*/, error)  // F6

// Version rule (MADR D2)
func ParseBase(v string) (maj, min, pat int, dev bool, err error)  // "0.6.4.4.gf7fe252" → 0,6,4, dev=true
func NewerBase(remote, local string) (bool, error)                  // three-part compare only

// Download + verify (checksum today; Verifier seam for phase-2 signing)
func DownloadVerified(ctx, asset Asset, sums Asset, destDir string) (stagedPath string, err error)

// Service cycle — added to internal/cli/service, same override seams:
func IsActive(product string) (bool, error)
func Stop(product string) error
func Start(product string) error

// Swap (port of install-binary.sh, F1)
func SwapAndRestart(staged, dest, product string, opts SwapOpts) error
//  stage is already on dest's filesystem (DownloadVerified wrote it there);
//  stop if active → rename dest→dest.prev → staged→dest → start if it was
//  active (or was enabled-but-down: heal, like the script) → wait_for_up →
//  on any failure after the stop: restore .prev and restart (defer'd, the
//  trap equivalent) → F2 health note.
```

### 1.2 CLI surface (both products)

```text
mcremote update [--check] [--yes] [--force]
  --check   report only; exit 0 up-to-date, 10 update available, 1 error (F5)
  --yes     skip the confirmation prompt
  --force   reinstall the latest release even at an equal BASE, and over a
            dev-suffixed local build (MADR D2); ignored by --check
```

Flow: resolve local version → `Latest()` → compare → prompt
(`update 0.6.6 → 0.6.7? [y/N]`) → `AssetFor` + `DownloadVerified` into the
executable's directory (via `os.Executable`, symlinks resolved) →
`SwapAndRestart` when installed as a service, else swap + "restart it
yourself" (script parity). Optional `GITHUB_TOKEN` honoured for rate
limits.

### 1.3 Phone

- `lib/data/update/app_update.dart` — pure service: `checkLatest()`
  (compare tag base vs `PackageInfo.version` base), `downloadApk()` (to
  app cache, progress callback), `verifySha256()` (against
  `SHA256SUMS-<VER>`), all on injected `http.Client` for tests.
- Settings → new **App update** tile (Connection section, near Version):
  states idle → checking → up-to-date / available(vX) → downloading(n%) →
  verified/ready → error. "Restart to update" confirmation dialog; first
  run explains the "Install unknown apps" grant before firing.
- `MainActivity.kt` + `UpdateInstaller.kt` — `MethodChannel`
  `mcremote/app_update`:
  - v1 `installApk(path)`: `FileProvider` content URI + install intent;
    manifest gains `REQUEST_INSTALL_PACKAGES` + provider +
    `res/xml/file_paths.xml` (cache-path).
  - v2 `installApkSession(path)`: `PackageInstaller` session with
    `setRequireUserAction(USER_ACTION_NOT_REQUIRED)`; manifest adds
    `UPDATE_PACKAGES_WITHOUT_USER_ACTION`; status receiver handles
    `STATUS_PENDING_USER_ACTION` by launching the confirm intent (the
    graceful pre-Android-12 / first-time fallback);
    `MY_PACKAGE_REPLACED` receiver posts the "Updated — tap to open"
    notification (F4).

---

## 2. Phased delivery

Each phase: compiles, `make test` / `flutter analyze` + `flutter test`
green, committed with `--no-edit`. Nothing pushed until the owner says so.

### P0 — `internal/update` core: discovery, versions, verify (pure)

- `Latest`, `AssetFor` (F6), `ParseBase`, `NewerBase`, `DownloadVerified`
  with a `Verifier` seam defaulting to sha256-from-sums.
- Tests (`httptest` stub of the releases API + asset host):
  - **U1**: newer / equal / older / dev-suffix all classify correctly,
    nothing downloaded on `--check` paths.
  - **U2/U8**: checksum mismatch and truncated download abort, staged file
    removed, destination untouched.
  - F6 edge: zero and duplicate asset matches error.

### P1 — Service primitives + swap (the port, F1/F2)

- `IsActive`/`Stop`/`Start` in `internal/cli/service` using the existing
  runner vars; states `SIGTERMed`/`exited` read as down (the incident).
- `SwapAndRestart` with defer-based restore (trap parity) keyed on
  *enabled*, not "stopped by this run".
- **macOS TCC cross-reference (MADR 0069 D6):** a downloaded release
  binary carries a different code identity than the granted one, so an
  update silently drops any Full Disk Access grant. When
  `MC_CODESIGN_IDENTITY` is configured on the host, `SwapAndRestart` must
  re-sign the staged binary with the local identity before the swap
  (mirroring the Makefile's `codesign-maybe`); otherwise the update
  output must state the re-grant cost. See
  [ops-macos-tcc.md](ops-macos-tcc.md).
- Tests via the override seams:
  - **U3**: the `install-binary_test.sh` incident replayed in Go — a unit
    stranded down by a previous failed run comes back up; up/down state
    preserved on success, error, and simulated interrupt.
  - **U4**: post-swap start failure → `.prev` restored and started; the
    staged binary is quarantined, not left as `dest`.

### P2 — `update` subcommands, both CLIs

- `internal/cli/update.go` (`newUpdateCmd`) wired at `root.go:84-89`;
  thin parallel wiring in `internal/relay/cli.go` — both delegate to
  `internal/update` with product name + version string.
- Prompt, `--check`/`--yes`/`--force` semantics (F5, MADR D2), asset
  download into the executable's directory, no-service message parity.
- Tests: cobra-level against the P0 stub — full happy path on a temp
  "install" with stubbed service runner; `--check` exit codes; dev-suffix
  refusal without `--force`; `mcrelay update` shares everything but the
  product string.
- **Docs rider**: `docs/config.md` / command examples gain `update`.

### P3 — Phone: check + download + verify (Dart only)

- `app_update.dart` per §1.3; base-compare mirrors `NewerBase` exactly
  (the APK's `versionName` is the full `BASE.N`, tag base compares clean).
- Settings tile with the state machine; wired to the service; errors via
  the status line in the tile (not top notifications — this is a
  user-watched flow).
- Tests: `MockClient` unit tests (up-to-date / available / 404 / checksum
  mismatch / interrupted download); widget test for tile states and that
  a mismatch shows the error state and discards the file.

### P4 — Phone: v1 install channel (intent)

- Manifest: `REQUEST_INSTALL_PACKAGES`, `FileProvider` +
  `file_paths.xml`; `UpdateInstaller.kt` v1; channel wiring; "Restart to
  update" dialog + first-run unknown-sources explainer (MADR §2.5).
- **U6** (hardware): full flow on the real phone updates vX → vX+1 with
  one system dialog; **U7**: pairing intact after.
- Dart-side test: channel invoked with the verified path only (a
  never-verified path cannot reach the installer).

### P5 — Phone: v2 session installer (the acceptance bar, MADR D4)

- `UPDATE_PACKAGES_WITHOUT_USER_ACTION`, session-based
  `installApkSession`, `STATUS_PENDING_USER_ACTION` fallback,
  `MY_PACKAGE_REPLACED` → notification (F4).
- **U9** (hardware): after the first v2-installed release, an update
  completes with **no system dialog**; `pm list packages -i` shows the
  app as its own installer of record.
- v1 path retained as the automatic fallback wherever the session route
  reports it needs user action.

### P6 — Docs close-out

- `ops-hardware-validation.md`: **Part D** rows — U5 (daemon updates
  N−1→N on macOS launchd + Linux systemd, F3 timing note), U6/U7/U9/U10
  (phone), U11 (post-registration verification surfaces, already staged
  by B3 closure).
- MADR 0065 → Implemented (software); update `ops-android-signing.md`
  cross-reference.

## 3. File-level checklist

| File | P0 | P1 | P2 | P3 | P4 | P5 |
|---|---|---|---|---|---|---|
| `internal/update/*.go` (new) | ● | ● | | | | |
| `internal/cli/service/setup.go` (+state primitives) | | ● | | | | |
| `internal/cli/update.go` (new), `root.go` | | | ● | | | |
| `internal/relay/cli.go` | | | ● | | | |
| `apps/mobile/lib/data/update/app_update.dart` (new) | | | | ● | | |
| `apps/mobile/lib/features/settings/settings_screen.dart` | | | | ● | ● | |
| `android/.../AndroidManifest.xml`, `res/xml/file_paths.xml` | | | | | ● | ● |
| `android/.../MainActivity.kt`, `UpdateInstaller.kt` (new) | | | | | ● | ● |
| `docs/ops-hardware-validation.md`, MADR status | | | | | | P6 |

Untouched: the release workflow (already produces everything the updater
consumes), `transport_policy`, protocol code.

## 4. Verification map (MADR §6 sketch → phases)

| MADR row | Phase | Level |
|---|---|---|
| U1 discovery/version classification | P0 | unit |
| U2 checksum abort pre-touch | P0 | unit |
| U3 swap preserves service state (incident replay) | P1 | unit (seams) |
| U4 rollback on failed start | P1 | unit (seams) |
| U5 real daemon update N−1→N | P6 (after next release, F3) | hardware |
| U6 phone v1 flow end-to-end | P4 | hardware |
| U7 pairing survives update | P4 | hardware |
| U8 tampered artifact rejected | P0 (go) / P3 (dart) | unit |
| U9 v2 dialog-free update + installer-of-record | P5 | hardware |
| U10 no new Play Protect surface on stable-key update | P4/P5 | hardware |
| U11 post-registration install surfaces | P6 | hardware |

## 5. Testing strategy notes

- Go: everything network-facing takes an injected base URL; everything
  service-facing goes through the existing override seams — `go test`
  touches no network and no service manager.
- The one thing unit tests cannot prove: a *real* launchd/systemd cycle
  over a *real* release pair (U5) — scheduled per F3.
- Dart: `MockClient` everywhere; the installer channel is behind a seam so
  widget tests stop at "channel invoked with verified path".
- Kotlin installer code is hardware-verified only (U6/U9) — kept
  deliberately thin (~150 lines total) so review carries the weight.

## 6. Open items that gate nothing but need answers eventually

1. Relay system-unit deployments (MADR open Q2) — P1 ships `--user`/gui
  parity with today's tooling; a root/system branch is additive later.
2. Minisign (MADR open Q5) — the `Verifier` seam lands in P0; the signing
  decision is a follow-up.
3. MADR open Q7 (skip v1?) — this plan ships v1 in P4 *as the fallback
  path v2 needs anyway* (`STATUS_PENDING_USER_ACTION`), so the question
  dissolves: v1 is not a throwaway release, it is v2's degraded mode.
