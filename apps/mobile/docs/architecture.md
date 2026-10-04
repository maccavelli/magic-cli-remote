# Architecture

Magic CLI Remote is the Flutter companion for the `mcremote` daemon. Android is
the shipped product APK. Linux desktop is the development target. iOS builds
and runs on the simulator.

The daemon owns providers, sessions, TLS, and pairing. This app is a WebSocket
client: it authenticates with a device token, drives session lifecycle, and
renders the live turn.

## Platforms

| Target | Role |
| :--- | :--- |
| Android | Product APK. Pairing, chat, permissions, notifications, secure storage. |
| Linux desktop | Local UI and protocol testing without an emulator. |
| iOS | Simulator builds. Same Dart codebase; platform plugins differ. |

Flutter pin: **3.47.5** / Dart matching CI `FLUTTER_VERSION` in
`.github/workflows/ci.yml`. `flutter pub get` must not change `pubspec.lock`.

## Connection

The phone selects the path (mesh, relay, or LAN). Pairing is an 8-character
code (five minutes), a QR (`mcremote://pair?…`), or a stored `mcr_…` token.
Invalid or revoked tokens clear and prompt re-pair.

The control plane is `GET /v1/ws` after `GET /v1/hello`. Debug builds may use
cleartext `ws://`. Foreground resume reconnects when credentials are still
active.

## What the app drives

- Session list and create, with the providers the daemon reports ready
  (grok, opencode, codex, kilo, fake)
- Model catalog, thinking levels, session modes (including auto-approve when
  offered)
- Live chat: thoughts, tools, assistant text, questions
- In-session transcript with daemon-history replay and a bounded phone cache
- Permission sheet → `permission.respond`
- Cancel in-flight turn
- Settings and notifications

Sessions are owned by the pairing device. Protocol and daemon layout live in
the root tree: [docs/architecture.md](../../../docs/architecture.md),
[protocol-v2.md](../../../docs/guides/protocol-v2.md).

## Source layout

```text
apps/mobile/
  lib/data/      wire models and the WebSocket client
  lib/state/     Riverpod
  lib/features/  pairing, sessions, chat, settings
  lib/theme/     Material 3
  docs/          this tree
```

JDK 21 builds the Android APK in CI and on every dev host (0168).
