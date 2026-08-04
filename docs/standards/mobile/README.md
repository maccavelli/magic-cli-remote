---
title: "Mobile Standards"
version: "3.12.2-v3"
last_updated: "2026-07-28"
status: "active"
applies_to: ["apps/mobile"]
dart_sdk: "^3.12.2"
---

# Mobile Standards

These standards apply to the Flutter companion in `apps/mobile`, which
targets Android and iOS from one codebase (iOS added by
[MADR 0067](../../0067-MADR-ios-port.md), 2026-08). Platform-specific
behaviour is documented per platform — do not describe one platform's
configuration as the other's.

## Repository facts

- Dart is constrained to `^3.12.2`; `flutter_lints ^6.0.0` is included through
  `analysis_options.yaml`.
- The app uses Riverpod 3, `go_router`, `web_socket_channel`, a custom
  Material 3 Celestial theme, and `flutter_secure_storage`.
- Source is organised as `data/`, `state/`, `features/`, and `theme/`. It is a
  pragmatic layered structure, not a complete Clean Architecture or generated
  Riverpod codebase.
- The daemon is a self-hosted mcremote endpoint. Pairing, certificate pinning,
  mTLS client identity, relay transport, and background notifications are core
  security/lifecycle concerns.

## Required checks

Run from `apps/mobile`:

```bash
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

`make preflight` runs the same mobile trio after the Go checks. Before staging
Dart files, format them with `dart format`; analyzer and tests do not detect
format drift.

## Documents

- [Dart](dart.md)
- [Flutter UI](flutter.md)
- [Architecture and state](architecture.md)
- [Android](android.md)
- [iOS](ios.md)
- [Networking and TLS](networking.md)

## External guidance

These documents use the official [Effective Dart](https://dart.dev/effective-dart),
[Flutter architecture guide](https://docs.flutter.dev/app-architecture/guide),
and [Android architecture recommendations](https://developer.android.com/topic/architecture/recommendations)
as general guidance. Repository behavior and security ADRs take precedence where
they deliberately differ.
