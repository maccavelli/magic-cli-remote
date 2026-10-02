# iOS signing and provisioning

<!-- markdownlint-disable MD013 -->

The iOS sibling of [ops-android-signing.md](ops-android-signing.md).
Decision context: MADR 0067 D8/F9.

## Current state (2026-08-04)

- **Simulator-only.** No iPhone hardware exists yet; every 0067 phase was
  verified on the iOS Simulator, which requires no signing at all.
- A free-tier Apple ID is signed into Xcode (Personal Team exists, a
  development certificate is issued, no devices enrolled). That is
  sufficient for all current work.
- **No `DEVELOPMENT_TEAM` is committed** to `project.pbxproj`. Select the
  team locally in Xcode (Runner target → Signing & Capabilities →
  "Automatically manage signing") when device work starts; keep the team id
  out of commits unless a deliberate decision changes this.
- Bundle id: `com.maccavelli.magicCliRemote` (iOS forbids the underscores
  the Android id uses — the two ids intentionally differ).
- Entitlements: `ios/Runner/Runner.entitlements` — Keychain access group
  only. Anything more needs a MADR (0067 D2 forbids background modes).

## When a device exists

1. Xcode → Settings → Accounts: sign in (free tier is fine to start).
2. Runner target → Signing & Capabilities → tick automatic signing → pick
   the Personal Team. Xcode registers the device and mints the profile on
   first run.
3. On the phone: enable Developer Mode (Settings → Privacy & Security),
   then trust the developer cert (Settings → General → VPN & Device
   Management) after the first install.
4. Run Part F of [ops-hardware-validation.md](ops-hardware-validation.md).

## Free tier vs paid program

| | Free Personal Team | Paid ($99/yr) |
|---|---|---|
| Provisioning lifetime | **7 days**, then rebuild/reinstall | 1 year (development profile) |
| App ID slots | 10 per 7 days | Effectively unlimited |
| Push notifications (APNs) | **Unavailable** | Available |
| TestFlight | No | Internal testing, 90-day builds, no review |
| Keychain access groups | Works | Works |

The 7-day expiry makes the free tier untenable for a daily-driver device —
0067 D8 recommends the paid program at that point. The APNs line is the
hard dependency to remember: the 0067 D3 follow-up (background attention
via APNs relay) **cannot even be prototyped on a free team**.

## Release / distribution

Out of scope until the 0067 D3 follow-up MADR decides the background
story; App Review constraints (4.2.7 remote-desktop rules, 2.5.4
background-mode misuse) interact with that decision and are recorded in
0067 F9. There is no iOS analogue of the APK sideload/update channel
(0065) under any distribution route.
