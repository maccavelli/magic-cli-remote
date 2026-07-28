---
title: "Flutter UI Standards"
version: "3.12.2-v3"
last_updated: "2026-07-28"
component: "flutter"
---

# Flutter UI Standards

`MagicCliRemoteApp` uses `MaterialApp.router`, the custom `celestialLight` and
`celestialDark` themes, and Material 3 (`useMaterial3: true`). Reuse the
Celestial color and component tokens; do not introduce a disconnected screen
palette or per-screen ThemeData.

## Widget boundaries and layout

- Keep features under `lib/features/`; keep shared visual primitives in
  `lib/theme/` or `lib/features/widgets/` when they are specific to feature UI.
- Prefer `const` widgets where the arguments are compile-time constants, but do
  not contort readable code merely to add `const`.
- Use `StatefulWidget` / `ConsumerStatefulWidget` for owned animation,
  controllers, subscriptions, and lifecycle cleanup. Do not mandate a stateless
  widget when state is genuinely local—the current app intentionally uses both.
- **Edge-to-Edge & System Insets**: Handle system bars and navigation cutouts cleanly.
  Use `SafeArea` or `MediaQuery.paddingOf(context)` / `View.of(context)` padding
  insets so content is not obstructed by system gesture bars or camera cutouts.
- **Predictive Back Gesture**: Handle back navigation via `PopScope` or GoRouter
  delegates to ensure compatibility with modern Android predictive back gestures.
- **Async & Lifecycle Safety**: After an `await`, check `context.mounted` before
  navigation, dialogs, snack bars, or `setState` calls that use the widget's context.
- Make loading, empty, error, and disconnected states explicit and accessible.
  Preserve user context during transient mesh or relay reconnects.

## Streaming UI

Transcript updates must remain bounded and user-respectful: do not force-scroll
when the reader has moved away from the bottom, and dispose scroll, animation,
and stream controllers. Rendering and layout logic belongs in widgets; protocol,
transport, and state-reduction logic belongs in the data/state layers.
