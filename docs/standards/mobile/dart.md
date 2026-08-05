---
title: "Dart Standards"
version: "3.12.2-v3"
last_updated: "2026-07-28"
component: "dart"
sdk_constraint: "^3.12.2"
---

# Dart Standards

## Language and tooling

- Keep `apps/mobile/pubspec.yaml` compatible with Dart `^3.12.2`.
- Format with `dart format`; analyze with the configured
  `package:flutter_lints/flutter.yaml`. Do not add individual lints just to
  enforce a stylistic preference without a demonstrated project need.
- Follow Effective Dart: `lowercase_with_underscores` for files and directories,
  `UpperCamelCase` for types, `lowerCamelCase` for members, and `///` for public
  API documentation where it adds value.

## Code idioms and modern features

- **Extension Types**: Use zero-cost extension types (`extension type Token(String value)`)
  for strongly-typed domain primitives (e.g. device IDs, session tokens, fingerprints)
  to prevent parameter misuse at compile time without allocation overhead.
- **Sealed Types & Class Modifiers**: Represent domain hierarchies (events, states,
  errors) using `sealed class` or `final class` modifiers to guarantee exhaustive
  pattern matching in `switch` expressions.
- **Pattern Matching & Switch Expressions**: Prefer `switch` expressions, records,
  and pattern destructuring for state reduction and value mapping:

```dart
final theme = switch (savedValue) {
  'light' => ThemeMode.light,
  'dark' => ThemeMode.dark,
  _ => ThemeMode.system,
};

final (host, port) = parseEndpoint(endpointString);
```

- **Async & Null Safety**: Give public and non-obvious asynchronous APIs explicit
  `Future<T>` or `Stream<T>` return types. Use `unawaited()` from `dart:async` for
  intentional background tasks and make its error/lifecycle behavior explicit.
- **Immutability & Safety**: Prefer immutable values and constructor initialization.
  Avoid `late` unless a framework lifecycle strictly guarantees initialization
  before every read.
- **Error Handling**: Catch the narrowest exception type that can be handled. Do
  not swallow errors or catch `Error`; rethrow with `rethrow` when preserving a
  stack trace.
- **Empty / discard catch (0070 F13)**:

  | Allowed | Not allowed |
  | --- | --- |
  | Teardown / best-effort socket or resource close | User-initiated send, create, pair, mode switch, connect |
  | Secondary decoration (clipboard, optional prefs, recent-paths) | Anything that leaves the UI claiming success when the host failed |
  | Must `debugPrint` at minimum if the failure is not user-visible | Bare `catch (_) {}` on primary actions |

  Prefer a banner / snack / `setState` error field for primary actions.
  Comment `// best-effort: …` when discard is intentional.
  Optional: a debug-only counter of discarded catches for field diagnosis
  (0071 F6) — not required in production builds.
- Keep parsing, wire models, TLS decisions, and persistence outside widgets.

See [Effective Dart](https://dart.dev/effective-dart) and the
[Flutter lints guidance](https://dart.dev/tools/linter-rules).
