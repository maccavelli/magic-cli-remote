# Mobile App Goose Support — Assessment

- **Status**: Superseded by [MADR 0030](./0030-MADR-goose-remote-parity.md)
- **Date**: 2026-07-26
- **Scope**: Add goose provider support to the Flutter Android app
- **Related**: [MADR 0025 goose provider](./0025-MADR-goose-provider.md), [PLAN 0005 Flutter scaffold](./PLAN-flutter-android-client-assessment.md)

---

## 1. Findings

### 1.1 Provider IDs are strings — no client-side enum

`ProviderInfo.id` is `String` (not an enum). The app calls `providers.list` and renders whatever the daemon returns. `'goose'` will appear in the provider dropdown automatically once the daemon advertises it.

**Only affected location:** `McremoteClient.preferredProvider()` at `data/ws/mcremote_client.dart:1499` hardcodes a priority-ordered list:

```dart
for (final id in ['grok', 'opencode', 'fake']) {
```

Goose is already included in the preference order. No hard-coded provider enum
or one-off selection path is needed.

### 1.2 Models come from the daemon — no client-side change

`models.list` returns an allow-custom bootstrap catalog for Goose. The
authoritative model choices arrive as ACP `session_config` options after the
session is created; the app displays those generically.

### 1.3 Modes come from the daemon — no client-side change

`SessionMode` is parsed generically. Goose's ACP-provided modes appear in the
same mode selector as other providers; the negotiated response, rather than a
terminal-mode assumption, is authoritative.

### 1.4 Commands come from the daemon — no client-side change

`AvailableCommand` and `RemoteCommand` are parsed generically, but a terminal
slash command is not automatically a remote command. `/compact` and `/goal`
are explicitly unavailable for Goose until a version-pinned ACP execution
probe proves a supported contract. Terminal-local `/status`, `/grind`,
`/skills`, `/doctor`, extensions, recipes, editor, theme, and diagnostic
commands are not forwarded remotely.

### 1.5 Permissions are provider-agnostic — no client-side change

The permission sheet in `chat_screen.dart` uses generic `PermissionOption` parsing. Goose's `always_approve` config is handled server-side; the client never sees permission requests when it's enabled.

### 1.6 Provider-specific UI branching — only one check

`chat_screen.dart` only exposes operations backed by a verified provider API.
Goose does not currently expose remote diff/fork/unrevert/compact/goal
operations through ACP, so it has no corresponding action menu entries.

### 1.7 Settings are provider-agnostic — no client-side change

The settings screen queries `preferredProvider()` and `listModels(provider)` dynamically. No goose-specific settings needed.

---

## 2. Changes Required

### 2.1 Native-session picker

The new-session dialog calls `agent_sessions.list` after selecting a provider.
The results are metadata only; choosing one supplies its id to the existing
`session.create.agent_session_id` load flow. Listing never creates a daemon
session or transfers ownership.

### 2.2 Negotiated capability UI

`SessionCapabilities` now carries image/audio/load, embedded context, native
session list/close, and MCP transport support. The client uses the same model
for all ACP providers.

---

## 3. Verification checklist

| # | Check | How |
|---|-------|-----|
| 1 | Goose appears in provider dropdown | Connect to daemon with goose enabled, open new-session dialog |
| 2 | Goose model selection correctly | Select Goose, then verify its ACP session config options |
| 3 | Goose modes switch correctly | Create goose session, tap mode chip, switch mode |
| 4 | Unsupported terminal commands are honest | Verify `/compact` and `/goal` explain they are unavailable over ACP |
| 5 | Permission request works (if not always_approve) | Trigger tool call, verify permission sheet appears |
| 6 | Session cancel works | Start a long goose turn, tap stop |
| 7 | Resume native session | Select a discovered session and verify the normal load path opens it |

---

## 4. Effort

Covered by the shared protocol/client and session-flow tests; live Goose probes
remain version-pinned acceptance checks.
