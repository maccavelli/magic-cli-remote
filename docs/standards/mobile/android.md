---
title: "Android Platform and Security Standards"
version: "3.12.2-v3"
last_updated: "2026-07-28"
component: "android"
gradle_dsl: "Kotlin DSL"
java_version: "17"
---

# Android Platform and Security Standards

## Build configuration

The Android project uses Kotlin DSL (`build.gradle.kts`, `settings.gradle.kts`), AGP 9.0+,
Kotlin 2.3+, Java 17 source/target compatibility, and core-library desugaring (`desugar_jdk_libs:2.1.4`)
for `java.time`. Plugin versions are managed centrally in `android/settings.gradle.kts`;
do not duplicate plugin version declarations in app module files. Release builds use a private
keystore specified in `android/key.properties` when present. The debug-key fallback is for local
development only and MUST NOT be distributed.

## Secrets and application security

- Device tokens, certificate pins, client certificates, and private keys use
  `FlutterSecureStorage`. On Android and iOS, a secure-storage failure must fail
  closed; the app's `SharedPreferences` fallback is desktop-only.
- Configure `FlutterSecureStorage` with `AndroidOptions(resetOnError: true)` to handle
  hardware-keystore invalidated keys safely without crashing.
- Keep backup disabled (`android:allowBackup="false"`) and preserve data extraction rules.
  Do not enable cleartext traffic globally; TLS is the mandatory transport.
- Set `android:enableOnBackInvokedCallback="true"` in `AndroidManifest.xml` for Predictive Back support.
- Review every new permission, exported component, intent filter, and manifest query. Treat deep
  links as untrusted input boundaries requiring validation and explicit user confirmation.

## Foreground work and notifications

The app uses `flutter_foreground_task` to keep the **process** (and main-isolate
socket) eligible while backgrounded. Until service-owned connection (MADR 0056 H-5b),
the FGS does not own the WebSocket — start it from foreground-friendly paths and
reflect real connection state in the notification (H-5a).
- Manifest MUST declare the service as `remoteMessaging` with `FOREGROUND_SERVICE` and
  `FOREGROUND_SERVICE_REMOTE_MESSAGING` permissions.
- Request runtime `POST_NOTIFICATIONS` permission on Android 13+ (API level 33+) before
  triggering local notifications or foreground services.
- Create explicit Android Notification Channels with appropriate importance levels.
- Keep service declaration, notification content, and background activity aligned with
  actual user-visible tasks per [Android Foreground Services guidance](https://developer.android.com/develop/background-work/services/fgs/declare).
