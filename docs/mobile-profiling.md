# Android app profiling (Flutter)

How to measure **runtime** performance of the Magic CLI Remote phone app in a
build mode close to production. Companion notes for chat UX live in
[chat-performance.md](chat-performance.md) and [MADR 0018](0018-MADR-mobile-chat-performance-action-plan.md).

## Modes (Flutter)

| Mode | Command | Use for |
|------|---------|---------|
| **debug** | `flutter run` | Day-to-day; **not** FPS/size truth |
| **profile** | `flutter run --profile` / `make profile` | Jank, rebuilds, CPU/memory (DevTools) |
| **release** | `flutter run --release` / `make apk` | Final FPS check; no DevTools attach. **CI tag releases only ship this mode** (`FLUTTER_BUILD_MODE=release` + `scripts/assert-flutter-release-apk.sh`) |

Profile is **near-release** AOT: assertions off, optimized code, but service
extensions stay on so DevTools and the performance overlay work. Prefer a
**physical Android device**. Emulators work but numbers are not representative.

Official references:

- [Flutter build modes](https://docs.flutter.dev/testing/build-modes)
- [DevTools](https://docs.flutter.dev/tools/devtools)
- [App size](https://docs.flutter.dev/perf/app-size) (binary size, not runtime)

## Prerequisites

1. Flutter 3.44+ / Dart 3.12+ (`flutter doctor`).
2. An Android device or emulator visible to Flutter:

   ```bash
   flutter devices
   # expect an android-arm64 (or android-x64 emulator) entry
   ```

3. `mcremote` reachable from the phone (same as normal Android runs):

   | Setup | Daemon listen | App host |
   |-------|---------------|----------|
   | Emulator | `0.0.0.0:7531` | `10.0.2.2:7531` |
   | Physical / Headscale | tailnet IP or `0.0.0.0` + grants | MagicDNS / mesh IP |
   | mcrelay | host registers to relay | pair URI with `relay` |

   See [apps/mobile/README.md](../apps/mobile/README.md) and [headscale.md](headscale.md).

4. Optional: pair code ready (`mcremote pair code --name phone --qr`).

## Quick start (Makefile)

From the **repo root**:

```bash
# List Android devices Flutter can see
make profile-devices

# Run the app in profile mode (interactive; blocks)
make profile

# Target a specific device id from `flutter devices`
make profile DEVICE=R5CNxxxxxxxx

# Build only (no run): arm64 profile APK for sideload
make profile-apk
```

| Target | What it does |
|--------|----------------|
| `profile` | `flutter run --profile` under `apps/mobile` |
| `profile-apk` | `flutter build apk --profile --target-platform android-arm64` |
| `profile-devices` | `flutter devices` (filter aid when choosing `DEVICE=`) |

Outputs for `profile-apk`:

- `apps/mobile/build/app/outputs/flutter-apk/app-profile.apk`

Install:

```bash
adb install -r apps/mobile/build/app/outputs/flutter-apk/app-profile.apk
```

Prefer **`make profile`** (run + attach) for DevTools work. A sideloaded profile
APK is closer to a cold-start install path but attaching DevTools is harder.

## Manual Flutter commands

Equivalent without Make:

```bash
cd apps/mobile
flutter pub get
flutter run --profile -d <device-id>

# profile APK only
flutter build apk --profile --target-platform android-arm64
```

## DevTools

While `make profile` / `flutter run --profile` is running:

1. Copy the **VM service URI** from the terminal (`http://127.0.0.1:…`).
2. Open DevTools:

   ```bash
   dart devtools
   # or: flutter pub global run devtools
   ```

3. Paste the URI when prompted.

### Panels that matter for this app

| Panel | Use |
|-------|-----|
| **Performance / Timeline** | Frame jank while streaming and scrolling |
| **CPU Profiler** | Hot Dart frames (markdown, transcript fold, Riverpod) |
| **Memory** | Growth on long sessions; GC pressure |
| **Flutter Inspector → Track widget rebuilds** | Chat rows rebuilding every chunk |

**On-device overlay** (from the `flutter run --profile` terminal):

- Press **`P`** — performance overlay (UI/GPU frame bars)
- Press **`q`** — quit

## Scenarios to record

Exercise production-like paths; start Timeline recording, then:

1. Cold connect / stored-token reconnect  
2. Open a session with large history  
3. Stream a long assistant reply (markdown + thoughts + tools)  
4. Scroll the transcript **while** streaming  
5. Background → foreground (foreground task / WS resume)  
6. QR scan / connect only if those feel slow  

Focus on (3)–(4) for chat work tracked in MADR 0018.

Suggested DevTools flow:

1. Performance → start recording  
2. Send a long prompt; scroll mid-stream  
3. Stop → inspect frames **> 16 ms** and expensive build/layout spans  
4. Enable **Track widget rebuilds** and confirm only the live bubble (or expected
   rows) flash during chunks  

## App size (not runtime)

Binary size uses **release** builds and a different tool:

```bash
cd apps/mobile
flutter build apk --release --target-platform android-arm64 --analyze-size
# then DevTools → App size tool → load *-code-size-analysis_*.json
```

See Flutter’s [Measuring your app’s size](https://docs.flutter.dev/perf/app-size).

## Gotchas

| Issue | Guidance |
|-------|----------|
| Only Linux desktop in `flutter devices` | Profile on a phone/emulator; desktop is for UI/protocol, not Android FPS |
| Jank only in debug | Expected — ignore debug FPS |
| Profile ≈ release but not identical | Spot-check critical paths with `flutter run --release` |
| Attach fails | `adb devices`, USB debugging, same machine; retry with `flutter run --profile -v` |
| Secure storage / camera / FGS | Profile on a **real phone** |
| Optimize without data | One Timeline + rebuild track beats guessing |

## Related

- [apps/mobile/README.md](../apps/mobile/README.md) — run, pair, APK sideload  
- [chat-performance.md](chat-performance.md) — chat architecture knobs  
- [0018-MADR-mobile-chat-performance-action-plan.md](0018-MADR-mobile-chat-performance-action-plan.md)  
- Root Makefile: `profile`, `profile-apk`, `profile-devices`, `apk`  
