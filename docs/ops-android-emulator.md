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

## Pairing the emulator with a local daemon

Two constraints interact, and neither is a bug:

* The daemon binds **only to its tailnet IPv4** (`listen: tailscale`), so
  `10.0.2.2` — the emulator's alias for the host loopback — cannot reach it.
  Use the tailnet address (`100.64.0.3:7531` here); the emulator's NAT routes
  to it fine.
* A **typed pair code cannot complete pairing**: it carries no certificate
  fingerprint, and the app refuses to trust an unpinned host. Verified message:
  *"This host's certificate can't be verified — no fingerprint is stored for
  it. Scan the QR from `mcremote pair code`: a typed code doesn't carry the
  fingerprint."* That is MADR 0046/0074 working as designed.

So the emulator needs the **fingerprint**, which means the QR or the pair URI.
`adb` cannot set the Android clipboard on modern Android (the paste button
reports "Clipboard is empty"), and the `mcremote://pair` deep link was removed
deliberately. Remaining options, untried:

* emulator virtual-scene camera with the QR as a poster image, so **Scan QR**
  works — the only route that exercises the real onboarding path. **Tried
  2026-08-13, partially working**: set `hw.camera.back=virtualscene` in the
  AVD's `config.ini`, then

  ```sh
  qrencode -o qr.png -s 20 -m 6 -l L "$(mcremote pair code --name emu \
      --host 100.64.0.3:7531 | sed -n 's/.*Pair URI: *//p')"
  emulator -avd mcremote_test -gpu host -virtualscene-poster wall=qr.png
  ```

  The scanner then shows the live 3D room, so the camera pipeline works end to
  end. The blocker is **aiming**: `Toren1BD.posters` places `wall` at rotation
  −150°, behind the default camera pose, and only `wall` and `table` are valid
  poster names. `adb emu sensor set orientation` does **not** move the scene
  camera (verified: identical frames across a full yaw sweep) — the pose is
  driven by WASD/mouse in the emulator window only. So a human can complete
  this in seconds by turning the camera to face the poster; a script cannot;
* a debug-only paste affordance;
* pair a physical phone for host-connected flows and keep the emulator for
  everything reachable offline.

## What this environment can and cannot cover

**Can** (verified 2026-08-13, all offline): app launch and render; the MADR
0082 settings hub — search field, grouped section containers, Providers spoke
with its disconnected copy; MADR 0083 bottom insets under gesture nav (the
last row clears the `navigationBars` inset at y=2337 by ~100 px on a 2400 px
display); MADR 0084's Recent errors row and screen, empty state.

**Cannot**: anything host-connected (provider credentials, catalogs, device
flows) until pairing is solved; real vendor accounts; and meaningful
cold-start/chat-open timings — emulator numbers do not transfer to hardware.
