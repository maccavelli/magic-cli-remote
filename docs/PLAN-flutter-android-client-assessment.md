# Phase 3 Plan: Flutter Android Client Assessment & Scaffolding

- **Status**: Accepted — Phase 3a implementation in progress / landed under `apps/mobile`
- **Date**: 2026-07-19
- **Scope**: Android-only Flutter companion for `mcremote`
- **Product name**: Magic CLI Remote
- **Decisions**: monorepo; grok-if-ready else fake; paste-only pairing; cleartext `ws://` in debug
- **Related**: [protocol-v1](./protocol-v1.md), [MADR 0001](./0001-MADR-architecture-mcremote.md), [Phase 2 Grok ACP](./MADR-phase2-grok-acp.md)

---

## 1. Recommendation

**Yes — scaffold the Flutter Android client now.**

The daemon already exposes a usable control plane (`auth`, sessions, streaming events, `permission.respond`). The product gap is no longer the agent loop; it is a **phone-grade supervision UI**. That matches the #1 community demand across Claude Code, OpenCode, Grok remote tools, and Telegram/ntfy hacks: *leave the desk without leaving the agent stuck*.

**Android-only for this phase** is correct:

- Faster iteration (one platform, one store story later)
- Easier FCM / notification experiments later
- Flutter still leaves a path to iOS without rewriting domain code

---

## 2. Landscape: what popular clients do

### 2.1 Peer products (remote agent control)

| Product | Open? | Mobile surface | Networking | Strength | Weakness vs mcremote goal |
|---------|-------|----------------|------------|----------|---------------------------|
| **Claude Code Remote Control** | Closed | Official Claude app + web | Vendor outbound relay | Polish, QR, push, multi-device sync | Claude-only; cloud transcript; Max-gated; can’t always start/stop fully from phone |
| **Shellular** | Partial (relay OSS) | Native iOS/Android | Outbound relay + E2E | Full stack: agents + terminal + files + localhost | Product cloud; not Grok-ACP-first; not self-hosted mesh-first |
| **Agents At Work** | Closed | iOS + Mac companion | Firebase + E2E | Permission + push focus, multi-agent colors | Mac-only host; no Linux; proprietary |
| **grok-remote** (+ PWA) | OSS | PWA over Tailscale | Mesh | ACP-true, Grok-native, multi-agent | PWA push/background weak; Node host; Grok-only |
| **OpenCode Remote Android** | OSS | Native Android | To OpenCode server | Streaming chat, tool approve, slash cmds | OpenCode-specific protocol, not mcremote/ACP orchestrator |
| **CodeAgent Mobile** | Partial | Phone + IDE plugin | Pairing relay | IDE agent supervision | Editor-bound, not CLI daemon |
| **Telegram / ntfy bots** | Often OSS | Chat apps | Bot APIs | Fast approve buttons, zero app install | Ugly context; no rich tool stream; ad-hoc |

### 2.2 Popular opinion (what people celebrate)

Across Reddit (r/ClaudeCode, r/ClaudeAI, r/opencode), product docs, and HN-style discussions, the consensus “this finally works” features are:

1. **Approve/deny tool permissions from the phone** without returning to the laptop  
2. **Live status** (running / waiting / idle / error) for multiple sessions  
3. **Send follow-up prompts** while away  
4. **Push or strong interrupt** when the agent is blocked or finished  
5. **One-shot pairing** (QR or short code)  
6. **Local execution remains on the desktop** (code never moves to a random cloud sandbox)

People repeatedly say vendor remote control is great *when it exists*, but they still build Telegram/ntfy bridges because:

- permissions still stall them  
- push is missing or delayed  
- they want multi-CLI / multi-host  
- they refuse vendor-only networking  

### 2.3 Features people ask for that are weak or missing in the ecosystem

These are **high-signal gaps** (not fully solved by any one OSS Grok-oriented stack):

| Gap | Notes | mcremote opportunity |
|-----|-------|----------------------|
| **Native Android + self-hosted mesh** | grok-remote is PWA; Shellular needs their relay | Flutter Android + Headscale is rare and on-brand |
| **Start session from phone** | Claude RC often requires desktop first | We already support `session.create` remotely — **ship it prominently** |
| **Cancel / interrupt from phone** | Common Claude RC complaint | We have `session.cancel` — **must be first-class UI** |
| **Permission UX that isn’t buried in chat** | Approve buttons in Telegram win because they’re loud | Full-screen / sticky permission sheet + optional system notification (later) |
| **Multi-provider under one app** | Most apps are single-vendor | `providers.list` + fake/grok already; design UI for provider badge |
| **Offline / reconnect resilience** | Network drops on phones | Auto-reconnect WS + replay live sessions list |
| **Diff / file review on phone** | Shellular/OpenCode strength | **Defer** past scaffold (daemon lacks file API still) |
| **Push without opening app** | Agents At Work / Claude | **Daemon gap** — no push channel yet; plan for FCM later, not scaffold-blocking |
| **Message queue while agent is mid-turn** | OpenCode remote mentions this | Optional later; for now disable send or queue client-side |
| **Voice prompts** | Claude ecosystem hype | Out of scope for scaffold |

### 2.4 What *not* to copy early

- Full remote IDE / terminal emulator (Shellular scope creep)  
- Browser DevTools tunneling  
- Cloud relay as required path (conflicts with Headscale decision)  
- Perfect multi-platform theming before the core loop works  

---

## 3. Product positioning for this client

**Name (working):** `mcremote` Android companion (package id TBD, e.g. `com.maccavelli.mcremote`)

**One-liner:** Supervise Grok Build (and future agents) running on your machine, over Headscale, with first-class permission control.

**Differentiators to emphasize in UX copy:**

1. Self-hosted / Headscale (no vendor phone-home required for networking)  
2. Start + cancel sessions from the phone  
3. Structured tool + thought stream (not a dumb terminal scrape)  
4. Device-token auth layered on mesh  

---

## 4. Plan adjustments (daemon / protocol)

Scaffolding the client **does not require a large protocol rewrite**. Small, optional daemon enhancements improve Android UX; none block starting Flutter work.

| Change | Priority | Why |
|--------|----------|-----|
| Keep protocol v1 as-is | — | Sufficient for MVP |
| **Default provider** when payload omits provider | Low | Prefer `grok` when enabled, else `fake` (already defaults to fake — consider switching default to grok in Phase 3) |
| **`session.create` default cwd** from `providers.grok.default_cwd` | Medium | Phone users won’t type long paths |
| **Pairing QR helper** (`mcremote pair create --qr` printing `ws://…` + token payload) | Medium | Shellular/Claude parity for onboarding |
| **Push notifications (FCM)** | Later | Needs new daemon→push path or phone-local high-priority notif while app is backgrounded; design hook only |
| **History pagination API** | Later | Client can live on live stream first |
| **File/diff endpoints** | Later | Don’t block Android scaffold |
| Document **Android Tailscale** joining Headscale | High (docs only) | Critical for real-world use |

**Protocol defaults recommendation (small daemon tweak, same PR as Flutter or just before):**

- If `session.create.provider` empty → pick first ready provider preferring `grok` over `fake`.

---

## 5. Flutter Android client — architecture

### 5.1 Repo layout

Monorepo option (recommended for early stage):

```text
magic-cli-remote/
  cmd/mcremote/          # existing Go daemon
  internal/…
  apps/
    mobile/              # Flutter project (Android focus)
      lib/
      android/
      pubspec.yaml
  docs/
```

Alternative: separate repo later if packaging diverges. **Start monorepo** for protocol co-evolution.

### 5.2 Stack (Flutter 3.x)

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Platform | **Android only** (min SDK 26+) | Phase constraint |
| State | **Riverpod** | Testable, scalable for WS streams |
| Routing | **go_router** | Simple multi-screen app |
| WS | **`web_socket_channel`** | Mature, works with pure Dart |
| Models | Freezed or hand-written JSON | Hand-written OK for scaffold; freezed if preferred |
| Secure storage | **`flutter_secure_storage`** | Device token at rest |
| HTTP | `http` or `dio` | `/healthz`, `/v1/hello` |
| Local prefs | `shared_preferences` | Host URL, last session |
| UI | Material 3 | Android-native feel |
| Lint | `flutter_lints` | Defaults |

**Explicitly skip for scaffold:** Firebase, maps, complex DI, code gen overload.

### 5.3 App structure

```text
lib/
  main.dart
  app.dart
  core/
    config.dart
    theme.dart
  data/
    protocol/
      envelope.dart
      messages.dart
      events.dart
    ws/
      mcremote_client.dart      # connect, auth, send, event stream
    local/
      settings_store.dart       # host, token
  features/
    connect/                    # host URL + token entry
    sessions/                   # list + create
    chat/                       # stream + composer + permissions
    settings/
  widgets/
    event_tile.dart
    permission_sheet.dart
    status_chip.dart
```

### 5.4 Screens (MVP)

1. **Connect**  
   - Host: `host:7531` or full `ws://…/v1/ws`  
   - Token paste (from `mcremote pair create`)  
   - “Test connection” → `/healthz` then WS auth  
   - Persist securely  

2. **Sessions**  
   - List live + disconnected (from `session.list`)  
   - Status chips: idle / running / waiting / error / disconnected  
   - FAB: New session (provider picker: grok/fake, name, optional cwd)  
   - Pull-to-refresh  

3. **Session detail (chat)**  
   - Chronological event stream  
   - Collapse thoughts by default  
   - Tool cards with status  
   - Composer + Send  
   - Cancel button when running  
   - **Sticky permission bar / modal** when `permission_request` arrives  

4. **Settings**  
   - Disconnect / clear token  
   - Theme light/dark  
   - Default provider  
   - Show device id from last `auth_ok`  

### 5.5 Event rendering rules (from protocol)

| Event | UI |
|-------|----|
| `user_message` | Right-aligned bubble |
| `assistant_message_chunk` | Append to left bubble |
| `thought_chunk` | Collapsed “Thinking…” accordion |
| `tool_call` / `tool_call_update` | Card: title, status pill, expandable body |
| `permission_request` | Blocking bottom sheet with option buttons |
| `turn_complete` | Subtle divider + stop reason |
| `session_status` | App bar chip |
| `error` | Error banner |

### 5.6 Connection lifecycle

```text
App resume → if configured, reconnect WS
  → auth with stored token
  → session.list
  → if last session open, resubscribe (events are broadcast; no subscribe API yet)
On background → keep WS if possible (Android may kill; reconnect on resume)
Ping every 30s → pong
```

**Note:** Phase 1/2 daemon broadcasts all events to all authenticated clients — good enough for single-user Android.

---

## 6. Phased delivery (Flutter)

### Phase 3a — Scaffold (this effort)

**Goal:** Compilable Android app that talks to a local/mesh `mcremote`.

Deliverables:

- [ ] `apps/mobile` Flutter project (Android target documented)  
- [ ] Models for envelope + events  
- [ ] `McremoteClient` (connect, auth, request/response correlation by `id`, event broadcast stream)  
- [ ] Connect screen + secure token storage  
- [ ] Sessions list + create (fake + grok)  
- [ ] Chat stream + composer  
- [ ] Permission sheet wired to `permission.respond`  
- [ ] Cancel button  
- [ ] README: run on emulator against `10.0.2.2:7531` / physical device via Headscale  
- [ ] Manual test checklist  

**Out of 3a:** push, QR scanner, diffs, multi-account, iOS, beautiful polish pass.

### Phase 3b — Android UX polish

- Material You dynamic color  
- Haptic on permission  
- Better tool card formatting (JSON pretty-print)  
- Connection status banner (connected / reconnecting / offline)  
- Deep link scheme `mcremote://pair?...` (optional with QR)  

### Phase 3c — Differentiation features

- QR scan of pair payload (`mcremote pair create --qr`)  
- Optional local notification when `permission_request` arrives while app is in background (Android foreground service or FCM later)  
- Session resume UI using `agent_session_id`  
- Multi-host profiles (home lab / work laptop)  

### Phase 3d — Daemon co-features (if demand)

- Push bridge  
- History backfill API  
- File/diff preview API  

---

## 7. Android-specific constraints

| Topic | Guidance |
|-------|----------|
| Emulator → host daemon | `ws://10.0.2.2:7531/v1/ws` (special alias to host loopback) |
| Physical device | Phone must be on Headscale tailnet; use MagicDNS or tailnet IP |
| Cleartext WS | Dev may need `android:usesCleartextTraffic` for `ws://` (not `wss://`). Prefer Tailscale only; document risk |
| TLS | Later: terminate TLS or rely on mesh crypto; mesh WireGuard already encrypts path |
| Min SDK | 26+ (secure storage, modern notifications) |
| Permissions | INTERNET only for scaffold; POST_NOTIFICATIONS when push arrives |

---

## 8. Testing strategy

| Layer | Approach |
|-------|----------|
| Unit | Envelope encode/decode; event parser |
| Widget | Permission sheet options; session list empty state |
| Integration | Against local `mcremote` with fake provider (CI-friendly) |
| Manual | Grok live path + Headscale phone |

Fake provider remains essential so Flutter CI does not need `grok` auth.

---

## 9. Success criteria (Phase 3a done)

1. Install debug APK on Android emulator  
2. Pair token from host `mcremote pair create`  
3. Connect, create **fake** session, prompt, see streamed assistant text  
4. Create **grok** session (when CLI available), receive thoughts/tools  
5. On permission event, approve/deny from sheet; agent continues  
6. Cancel in-flight turn  
7. Kill app, relaunch, reconnect with stored credentials  

---

## 10. Effort estimate

| Slice | Effort |
|-------|--------|
| Flutter project + client core | S–M |
| Connect + sessions list | S |
| Chat + event rendering | M |
| Permissions + cancel | S |
| Docs + emulator/Headscale guide | S |

**Total scaffold:** roughly 2–4 focused days for a Flutter-comfortable engineer, or one dense multi-session push.

---

## 11. Explicit non-goals (this phase)

- iOS build / App Store  
- Play Store release pipeline  
- Full terminal emulator  
- Diff viewer / file browser  
- Voice input  
- Multi-user team spaces  

---

## 12. Open decisions for product owner

1. **Monorepo `apps/mobile` vs separate repo?** (Recommend monorepo.)  
2. **App display name:** `mcremote` vs `Magic CLI Remote` vs other?  
3. **Default create provider:** `grok` when ready, else `fake`?  
4. **Include QR scanner in 3a or 3b?** (Recommend **3b**; paste token in 3a.)  
5. **Cleartext `ws://` allowed in debug builds only?** (Recommend yes for emulator.)  

---

## 13. PR plan (Flutter 3a)

| Order | Title |
|-------|-------|
| 1 | chore(mobile): scaffold Flutter Android app under `apps/mobile` |
| 2 | feat(mobile): protocol models + WebSocket client + secure settings |
| 3 | feat(mobile): connect + sessions list/create screens |
| 4 | feat(mobile): session chat stream, composer, cancel |
| 5 | feat(mobile): permission sheet + polish + README runbook |
| 6 | (optional daemon) feat: smarter default provider + pair QR text helper |

---

*End of assessment & plan — implement after review of §12 open decisions.*
