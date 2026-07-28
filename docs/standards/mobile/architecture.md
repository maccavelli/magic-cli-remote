---
title: "Mobile Architecture and State Standards"
version: "3.12.2-v3"
last_updated: "2026-07-28"
component: "architecture"
state_management: "flutter_riverpod ^3.3.2"
router: "go_router ^17.3.0"
---

# Mobile Architecture and State Standards

## Current structure

Keep the established boundaries:

```text
lib/
├── data/       # Protocol, WebSocket/relay, storage, notifications, chat
├── state/      # App-wide Riverpod providers and controllers
├── features/   # Screens and feature-specific widgets
├── theme/      # Celestial design system and shared visual behavior
├── app.dart    # Router and application composition
└── main.dart   # Entrypoint
```

This is a layered, feature-oriented app. Do not claim it already has a strict
repository/service/ViewModel stack; introduce those layers only when they make a
concrete data source, test seam, or shared business rule clearer.

## Riverpod and routing

- Providers own app-scoped dependencies and disposal. `mcremoteClientProvider`
  owns the client lifecycle; callers must not instantiate competing clients.
- Use `ref.watch` for reactive UI dependencies and `ref.read` for event-driven
  commands.
- **Async State & Error Boundaries**: Use `Notifier` / `AsyncNotifier` patterns with
  `AsyncValue.guard()` for async state transitions to handle loading, data, and error
  states cleanly without uncaught exceptions:

```dart
Future<void> performAction() async {
  state = const AsyncValue.loading();
  state = await AsyncValue.guard(() => repository.executeAction());
}
```

- Keep state transitions immutable and independently testable, as the transcript
  reducer does.
- `goRouterProvider` is the routing authority. Preserve its pairing-aware
  redirect behavior: a temporary disconnected socket does not mean the device
  is unpaired.
- Inject fakes or optional dependencies at package boundaries for tests rather
  than reaching into widget internals.

Flutter recommends separate UI and data layers, narrow responsibilities, and
state that can be tested independently of widgets. Apply that direction
incrementally to this existing structure rather than performing architecture by
label. See the [Flutter architecture guide](https://docs.flutter.dev/app-architecture/guide).
