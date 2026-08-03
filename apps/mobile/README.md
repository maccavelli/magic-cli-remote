# Magic CLI Remote (Android)

Flutter companion app for the `mcremote` daemon. **Android is the product
target**; a Linux desktop target is included for local development.

## Features

- Connect via **Enter code** (8-char, 5 min), QR scan, or long-lived token
- Session list / create with provider picker (**grok**, **opencode**, **goose**, **codex**, **fake** — as the daemon reports ready)
- Model catalog, model-provider scope, thinking levels, and session modes (including auto-approve / dangerous modes when offered)
- Live chat stream (thoughts, tools, assistant text, questions)
- **In-session transcript** survives navigating away from chat, with daemon-history replay and a bounded best-effort phone cache
- **Foreground resume** reconnects the WebSocket when credentials are still active; path selection (mesh / relay / LAN)
- Permission sheet → `permission.respond`
- Cancel in-flight turn
- Settings, notifications, secure token storage; invalid/revoked tokens clear and prompt re-pair

## Prerequisites

- Flutter 3.44+ / Dart 3.12+
- One of:
  - **Linux desktop** (easiest on this host — no Android emulator required)
  - Android emulator / physical device on Headscale
- Running `mcremote` daemon (see repo root)

## Run (Linux desktop — recommended here)

This machine currently has **no Android emulator**. The project includes a **Linux desktop** target for local UI + protocol testing:

```bash
# terminal 1 — daemon (loopback is fine for desktop)
cd ../..   # repo root
make build
./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531
./bin/mcremote pair code --name desktop --qr

# terminal 2 — app
cd apps/mobile
flutter run -d linux
```

In the app: **Enter code** / **Scan QR**, or host **`127.0.0.1:7531`** + long token under Advanced.

If `flutter run -d linux` fails with a display error on a headless AWS box, you need either:

- an X11/Wayland display / VNC / RDP session, or  
- `Xvfb :99 &` then `export DISPLAY=:99`, or  
- a real Android device/emulator elsewhere

**Linux keyring:** headless sessions often hit `KeyringLocked` from `flutter_secure_storage`. The app falls back to SharedPreferences for the device token so connect still works. Unlock/login your desktop keyring for production-like secure storage.

Build-only (no GUI):

```bash
flutter build linux
```

## Run (Android emulator)

On the host:

```bash
# from repo root
make build
./bin/mcremote serve --listen-host 0.0.0.0 --listen-port 7531
./bin/mcremote pair code --name android --qr
# Enter code or scan QR in app
```

> Emulator reaches the host via **`10.0.2.2`**. Daemon must listen on `0.0.0.0` (or the host LAN IP), not only `127.0.0.1`.

On the app:

```bash
cd apps/mobile
flutter pub get
flutter run
```

In the app:

- Host: `10.0.2.2:7531`
- Token: **Scan QR** or paste `mcr_…`
- Connect → New session → chat

## Physical device (Headscale)

1. Phone joins the same Headscale tailnet as the host  
2. Host: `mcremote serve` bound to tailnet IP or `0.0.0.0` with grants for TCP **7531**  
3. App host field: MagicDNS name or tailnet IP, e.g. `devbox:7531`  
4. Scan pair QR (or paste device token)  

See [docs/headscale.md](../../docs/headscale.md).

## Cleartext WebSocket

Android rejects `ws://` (cleartext) in both debug and release builds. Linux
development may explicitly use it for a trusted local or mesh deployment, but
TLS is required for Android pairing.

## Connection lifecycle

- **Cold start:** if a host + device token are stored, the connect screen auto-connects once.
- **App resume:** if the socket dropped while the app was backgrounded and you have not logged out, the client reconnects automatically.
- **Logout (Disconnect):** stops auto-reconnect for this process; stored token remains for the next cold start unless you **Clear saved credentials**.
- **Invalid token:** storage token is cleared; host is kept so you can re-pair with a new code.

The app rebuilds transcripts from the daemon's bounded history ring and keeps a
best-effort bounded phone cache. The daemon remains authoritative; force
stopping can still lose an unflushed local tail.

## Platforms

| Platform | Status |
|----------|--------|
| **Android** | Product target (release APK on `v*` tags) |
| **Linux desktop** | Dev convenience (same UI, localhost daemon) |
| iOS / Web | Not enabled |

## Project layout

```text
lib/
  data/protocol/   # envelope + event models
  data/ws/         # McremoteClient
  data/local/      # secure settings
  features/        # connect, sessions, chat
  state/           # Riverpod providers
```

## Build APK (this host)

Android SDK is already installed at `$HOME/Android/Sdk`. On low-RAM machines use:

```bash
# from repo root
./scripts/build-apk.sh
# or: make apk
```

Outputs:

- `apps/mobile/build/app/outputs/flutter-apk/app-release.apk`
- `dist/magic-cli-remote-latest-arm64.apk` (copy for sideload)

Signed with **debug keys** for now (easy sideload). Install:

```bash
adb install -r dist/magic-cli-remote-latest-arm64.apk
# or scp/rsync the APK to your phone and open it
```

## Profiling (runtime performance)

Use **profile** mode on a real Android device (near-release AOT + DevTools). Do
**not** trust FPS from debug builds.

From the **repo root**:

```bash
make profile-devices          # list devices
make profile                  # flutter run --profile
make profile DEVICE=<id>      # pin a device
make profile-apk              # arm64 profile APK only
```

Then open DevTools (`dart devtools`) and paste the VM service URI from the run
terminal. Full guide: [docs/mobile-profiling.md](../../docs/mobile-profiling.md).

Chat-specific knobs: [docs/chat-performance.md](../../docs/chat-performance.md).

## Tests

```bash
flutter test
```
