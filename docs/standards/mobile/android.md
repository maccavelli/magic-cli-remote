---
title: "Android Platform and Security Standards"
version: "3.12.2-v2"
last_updated: "2026-07-28"
component: "android"
gradle_dsl: "Kotlin DSL"
java_version: "17"
---

# Android Platform and Security Standards

## Build configuration

The Android project uses Kotlin DSL, Java 17 source/target compatibility, and
core-library desugaring for `java.time`. Its Android Gradle Plugin and Kotlin
versions are controlled in `android/settings.gradle.kts`; do not duplicate them
in app modules. Release builds use a private keystore from
`android/key.properties` when available. The debug-key fallback is for local
use only and must never be distributed.

## Secrets and application security

- Device tokens, certificate pins, client certificates, and private keys use
  `FlutterSecureStorage`. On Android and iOS, a secure-storage failure must fail
  closed; the app's `SharedPreferences` fallback is desktop-only.
- Do not mandate `encryptedSharedPreferences: true`: this app configures
  `FlutterSecureStorage` with `AndroidOptions(resetOnError: true)`. Preserve
  that tested configuration unless changing the storage implementation with
  migration and security tests.
- Keep backup disabled and preserve the existing backup/data-extraction rules.
  Do not enable cleartext traffic globally; TLS is the normal transport.
- Review every new permission, exported component, intent filter, and manifest
  query. Treat a deep link as an untrusted input boundary requiring validation
  and explicit user confirmation.

## Foreground work

The app uses `flutter_foreground_task` to keep a user-visible remote-messaging
connection alive while backgrounded. Its manifest declares the service as
`remoteMessaging` with `FOREGROUND_SERVICE` and
`FOREGROUND_SERVICE_REMOTE_MESSAGING` permissions. Keep the type, notification,
and launch behavior aligned with the actual user-facing work; Android requires
foreground-service declarations and permissions to match the selected type.
See [Android's foreground-service guidance](https://developer.android.com/develop/background-work/services/fgs/declare).
