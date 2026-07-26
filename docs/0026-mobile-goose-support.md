# Mobile App Goose Support — Assessment

- **Status**: Assessment complete, implementation trivial
- **Date**: 2026-07-26
- **Scope**: Add goose provider support to the Flutter Android app
- **Related**: [MADR 0025 goose provider](./0025-goose-provider.md), [MADR 0005 Flutter scaffold](./0005-flutter-android-client-assessment-and-plan.md)

---

## 1. Findings

### 1.1 Provider IDs are strings — no client-side enum

`ProviderInfo.id` is `String` (not an enum). The app calls `providers.list` and renders whatever the daemon returns. `'goose'` will appear in the provider dropdown automatically once the daemon advertises it.

**Only affected location:** `McremoteClient.preferredProvider()` at `data/ws/mcremote_client.dart:1499` hardcodes a priority-ordered list:

```dart
for (final id in ['grok', 'opencode', 'fake']) {
```

Adding `'goose'` is the single client-side change needed for auto-selection behavior.

### 1.2 Models come from the daemon — no client-side change

`models.list` RPC returns whatever models the provider advertises. The goose static model catalog is served by the daemon; the app displays it generically.

### 1.3 Modes come from the daemon — no client-side change

`SessionMode` is parsed generically in `models.dart:370-388`. The goose modes (auto, approve, smart_approve, chat) will appear in the `_ModeSelector` popup automatically. Mode switching via `session/set_config_option` with `optionId=mode` is already handled generically by the `/mode` slash command.

### 1.4 Commands come from the daemon — no client-side change

`AvailableCommand` and `RemoteCommand` are parsed generically. The goose command table (status, grind, skills, doctor, compact, goal) will appear in the slash autocomplete automatically. No built-in command override is needed.

### 1.5 Permissions are provider-agnostic — no client-side change

The permission sheet in `chat_screen.dart` uses generic `PermissionOption` parsing. Goose's `always_approve` config is handled server-side; the client never sees permission requests when it's enabled.

### 1.6 Provider-specific UI branching — only one check

`chat_screen.dart:1775` checks `_provider == 'opencode'` to show diff/fork/unrevert menu actions. Goose has equivalent native commands (`compact`, `goal`, `status`, `grind`, `skills`, `doctor`) but these are slash-command-based, not popup-menu-based. **No change needed** — goose's native commands work through the existing autocomplete.

### 1.7 Settings are provider-agnostic — no client-side change

The settings screen queries `preferredProvider()` and `listModels(provider)` dynamically. No goose-specific settings needed.

---

## 2. Changes Required

### 2.1 `data/ws/mcremote_client.dart` — add goose to preferred provider list

```dart
final ids = ['grok', 'opencode', 'goose', 'fake'];
```

This ensures goose is preferred over fake when both are installed, but defers to grok and opencode.

### 2.2 Nothing else

Every other aspect of the provider interface (model listing, mode switching, command autocomplete, permission handling, session create/load, cancel, event streaming) is already fully generic. The mobile app and the daemon communicate through the same RPC protocol that all providers use.

---

## 3. Verification checklist

| # | Check | How |
|---|-------|-----|
| 1 | Goose appears in provider dropdown | Connect to daemon with goose enabled, open new-session dialog |
| 2 | Goose models list correctly | Select goose, verify model dropdown shows 7 goose models |
| 3 | Goose modes switch correctly | Create goose session, tap mode chip, switch mode |
| 4 | Goose commands appear in autocomplete | Type `/` in composer, verify status/grind/skills/doctor appear |
| 5 | Permission request works (if not always_approve) | Trigger tool call, verify permission sheet appears |
| 6 | Session cancel works | Start a long goose turn, tap stop |
| 7 | Slash commands work | Type `/compact`, `/goal`, `/status`, send |

---

## 4. Effort

One-line change plus manual verification (~15 minutes).
