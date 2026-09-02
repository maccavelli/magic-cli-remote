# Android emulator for phone-app testing

Set up 2026-08-13 to run the MADR 0082/0083/0084 device checks without a
physical phone. Everything here was verified on this host; the two gotchas at
the top cost an hour and are the reason this file exists.

## Two gotchas, both non-obvious

**1. `adb exec-out screencap` returns a pure-black image for Flutter.**
The Flutter surface is a hardware overlay the guest screenshot path cannot
see. The launcher captures fine, so it looks like the app is broken when it is
not. Use the **emulator's host-side capture** instead:

```sh
adb emu screenrecord screenshot <output-dir>/     # writes Screenshot_*.png
```

`scripts/` has no wrapper for this; the one used during 0084 testing lived in
the scratchpad as `shot.sh` (rm stale files → `adb emu screenrecord
screenshot` → rename newest).

**2. Gradle invoked directly needs JDK 21.**
`./gradlew` bypasses `flutter config --jdk-dir`, and Gradle 9.1 rejects this
host's default JDK 26 during its own `jlink` step. Prefix with:

```sh
JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home ./gradlew …
```

Also note `android/gradlew` is **gitignored** by the Flutter template, so a
fresh checkout has no wrapper — `flutter build apk --config-only` generates
it (this is what CI does before the lint step).

## Setup

```sh
export ANDROID_SDK_ROOT=/opt/homebrew/share/android-commandlinetools
sdkmanager --install "emulator" "system-images;android-36;google_apis;arm64-v8a"
avdmanager create avd -n mcremote_test \
  -k "system-images;android-36;google_apis;arm64-v8a" -d pixel_7
```

API 36 (Android 16) is deliberate: it **enforces edge-to-edge**, which is the
condition MADR 0083's inset fixes exist for. An older image would not exercise
them.

```sh
$ANDROID_SDK_ROOT/emulator/emulator -avd mcremote_test -no-audio -no-snapshot -gpu host
adb shell cmd overlay enable com.android.internal.systemui.navbar.gestural
```

Gesture navigation must be enabled explicitly — the default is three-button,
which has a different (larger, opaque) inset and hides the bug class 0083
addresses.

## Pairing the emulator with a local daemon — working recipe

Verified end to end 2026-08-13: the emulator paired and reached
`Connected to 100.64.0.3 over Mesh`.

Two constraints shape this, and neither is a bug:

* The daemon binds **only to its tailnet IPv4** (`listen: tailscale`), so
  `10.0.2.2` — the emulator's alias for the host loopback — cannot reach it.
  Use the tailnet address; the emulator's NAT routes to it fine.
* A **typed pair code cannot complete pairing**: it carries no certificate
  fingerprint, and the app refuses to trust an unpinned host. Observed message:
  *"This host's certificate can't be verified — no fingerprint is stored for
  it. Scan the QR from `mcremote pair code`: a typed code doesn't carry the
  fingerprint."* That is MADR 0046/0074 working as designed — treat it as a
  passed security check, not an obstacle to route around.

So the QR is the only way in, and it must reach the virtual camera:

```sh
SDK=/opt/homebrew/share/android-commandlinetools
S=/tmp/mcremote-emu; mkdir -p "$S"

# 1. AVD must use the 3D scene camera (one-time).
sed -i '' 's/hw.camera.back=emulated/hw.camera.back=virtualscene/' \
    ~/.android/avd/mcremote_test.avd/config.ini

# 2. Mint a code and render its URI as a QR. -t PNG32 is REQUIRED (see below).
URI=$(mcremote pair code --name emulator --ttl 20m --host 100.64.0.3:7531 \
      | sed -n 's/.*Pair URI: *//p')
qrencode -t PNG32 -o "$S/qr.png" -s 16 -m 4 -l L "$URI"
sips -z 1024 1024 "$S/qr.png" --out "$S/qr1024.png"   # match the stock poster

# 3. Overwrite the scene's DEFAULT poster (back it up once).
R=$SDK/emulator/resources
[ -f "$R/poster.png.orig" ] || cp "$R/poster.png" "$R/poster.png.orig"
cp "$S/qr1024.png" "$R/poster.png"

# 4. Boot, then in the app: Scan QR, and turn the camera to face the poster.
$SDK/emulator/emulator -avd mcremote_test -no-audio -no-snapshot -gpu host
```

Restore the stock scene afterwards with
`cp "$R/poster.png.orig" "$R/poster.png"`.

### Why it has to be done this way

* **The scene poster is loaded at BOOT and cached.** Replacing `poster.png`
  while the emulator is running changes nothing on screen — the camera keeps
  showing whatever was on disk when it started. Swap the poster *first*, then
  boot; if the emulator is already up, restart it. Found the hard way on
  2026-09-01: three successive pair QRs were written to disk and the app kept
  scanning the first one, failing with **"code already used"** because that
  first one-shot code had been consumed an hour earlier. `mcremote pair list`
  is the check — a code that was never claimed does not appear there at all.
* **`-virtualscene-poster wall=<file>` is silently ignored** (emulator
  37.1.11). No log line, no effect — verified with a correctly formatted
  1024×1024 RGBA image on both `wall` and `table`. Overwriting the default
  `poster.png` that `Toren1BD.posters` declares is what actually works.
* **`qrencode` writes a 1-bit palette PNG by default**, which the scene's
  texture loader will not display. The stock `poster.png` is 8-bit RGBA, and
  `-t PNG32` matches it.
* **The scene camera cannot be aimed from adb.** `adb emu sensor set
  orientation` does not move it (verified: identical frames across a full yaw
  sweep) — the pose is driven by WASD/mouse in the emulator window only. A
  human turns to face the poster in seconds; a script cannot. This is the one
  manual step.
* Pair codes default to a 5-minute TTL; `--ttl 20m` removes the time pressure
  while you aim.

## What this environment can and cannot cover

**Can** — all verified 2026-08-13 against the live daemon:

* MADR 0082: the settings hub (search, grouped containers), the Providers
  fleet with per-agent brand icons and worst-status folding, and the per-agent
  detail screen (status / session defaults / active upstream / credentials).
* MADR 0083: bottom insets under gesture nav — *Add credential*, the row the
  bug report was about, clears the `navigationBars` inset (y=2337 on a 2400 px
  display); semantic status chips with `Active` as its own pill; and
  confirm-before-remove, cancelled without deleting.
* MADR 0083 D4, the clearest result — two catalogs, same UI, correctly
  different: goose reads *"Offline catalog · 73 vendors · list pinned to a
  known CLI version"* with greyed **"Host only · keyring"** rows, while
  opencode reads *"Live catalog · showing 100 of 184"* with enabled rows and
  "API key" / "Device code" method chips. The 184 count and 100-per-page match
  the Go live tests exactly.
* MADR 0082 D5: monogram fallback (`digitalocean` → "DI", `gitlab` → "GI",
  distinct hash-derived colours) beside real brand marks.
* MADR 0084: the Recent errors row and screen. Nothing was recorded across a
  full session of navigation — the boundary caught no failures.

**Cannot**: device flows that must be *completed* (they consume a real
authorization against the user's vendor account — and codex's is destructive
by design, MADR 0074 D8); and meaningful cold-start/chat-open timings, which
do not transfer from an emulator to hardware.
