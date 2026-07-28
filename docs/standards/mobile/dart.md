---
title: "Dart Standards"
version: "3.12.2-v2"
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

## Code idioms

- Give public and non-obvious asynchronous APIs explicit `Future<T>` or
  `Stream<T>` return types. Use `unawaited()` from `dart:async` for intentional
  background work and make its error/lifecycle behavior clear.
- Prefer immutable values and constructor initialization. Avoid `late` unless
  a framework lifecycle guarantees initialization before every read.
- Use switch expressions, sealed types, records, and pattern matching when they
  make a closed state model or small return value clearer; they are tools, not
  a requirement for every conditional.
- Catch the narrowest exception type that can be handled. Do not swallow errors
  or catch `Error`; rethrow with `rethrow` when preserving a stack trace.
- Keep parsing, wire models, TLS decisions, and persistence outside widgets.

```dart
final theme = switch (savedValue) {
  'light' => ThemeMode.light,
  'dark' => ThemeMode.dark,
  _ => ThemeMode.system,
};
```

See [Effective Dart](https://dart.dev/effective-dart) and the
[Flutter lints guidance](https://dart.dev/tools/linter-rules).
