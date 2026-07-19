# Magic CLI Remote (Android)

Flutter companion app for the `mcremote` daemon. **Android-only** for Phase 3a.

## Features (3a)

- Connect with host + paste device token (`mcremote pair create`)
- Session list / create (provider: **grok if ready, else fake**)
- Live chat stream (thoughts, tools, assistant text)
- Permission sheet → `permission.respond`
- Cancel in-flight turn
- Secure token storage

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
./bin/mcremote pair create --name desktop

# terminal 2 — app
cd apps/mobile
flutter run -d linux
```

In the app: host **`127.0.0.1:7531`**, paste the `mcr_…` token.

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
./bin/mcremote pair create --name android
# copy mcr_… token
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
- Token: paste `mcr_…`
- Connect → New session → chat

## Physical device (Headscale)

1. Phone joins the same Headscale tailnet as the host  
2. Host: `mcremote serve` bound to tailnet IP or `0.0.0.0` with grants for TCP **7531**  
3. App host field: MagicDNS name or tailnet IP, e.g. `devbox:7531`  
4. Paste device token  

See [docs/headscale.md](../../docs/headscale.md).

## Cleartext WebSocket

Debug builds allow `ws://` (cleartext) for emulator and mesh development. Prefer Headscale (WireGuard) rather than public internet cleartext.

## Platforms

| Platform | Status |
|----------|--------|
| **Android** | Product target (Phase 3a) |
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

## Tests

```bash
flutter test
```
