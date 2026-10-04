# Magic CLI Remote — Mobile UX/UI Assessment & Research

*Prepared for review. Combines a code-grounded audit of the Flutter app with web
research on comparable "control your CLI coding agent from your phone" projects
and mobile-agent UX best practices. Research claims were extracted from primary
repos, blogs, HN, and Reddit and adversarially fact-checked (50 upheld / 3
refuted); confidence notes are at the end.*

---

## 1. Executive summary

The app already gets the **hard plumbing** right (pairing, TLS pinning, ACP
event streaming, reconnection, transcript replay) and — after this week's work —
a clean chat with rendered markdown, terse collapsed tool/thought tiles, and
daemon-handled slash commands. Where it lags the field is **the async, "walk
away and get pinged" workflow** that is the entire reason these tools exist.

**The single biggest gap: there are no push notifications.** Every comparable
project treats "the agent needs approval" / "the agent is done" alerts as the
headline feature — it's the most-requested and most-loved capability in the
category. Our app not only lacks it, it actively **stops reconnecting when
backgrounded** (`app_lifecycle.dart`), so it can't even surface events while you
hold the phone. Fixing this is worth more than any other change on this list.

**Top 5 moves, in priority order:**

1. **Push notifications** for `permission_request` and `turn_complete` (P0).
2. **Let users create a session from a populated list** — today the only "New
   session" button lives in the empty state (P0).
3. **Richer, granular approval UX** (approve-once vs approve-always, show
   repo/tool/command/scope) with a notification path (P1).
4. **Streaming-markdown "unclosed marker" buffering** to kill transient raw
   syntax during streaming (P1).
5. **One-handed ergonomics + a Settings screen** (model/theme/host/notif
   prefs) (P1).

---

## 2. Current app — code-grounded assessment

Structure: `ConnectScreen → SessionsScreen → ChatScreen` (go_router), Material 3,
seed color `#1B6B4A`, light/dark by system. No settings screen, no notifications
layer.

### What's already good

- **Chat rendering** (post-fix): markdown with cached/throttled parsing (no
  streaming flicker), horizontally-scrollable code blocks, terse **collapsed-by-
  default** tool & thought tiles, a stop/interrupt button (a verified must-have).
- **Slash commands**: daemon-handled `/model` (live switch), `/reset`, `/new`,
  `/help`; unknown `/commands` forward to the agent; autocomplete always offers
  the built-ins.
- **Connection resilience**: reconnect banners, transcript replay, permission
  queue draining, optimistic session removal.
- **Pairing**: QR / 8-char code / token, `healthz` test, cert-pinning modes.

### Gaps & rough edges (by area)

**Cross-cutting**

- **No push notifications** and background **kills** the socket
  (`app_lifecycle.dart` `_onBackground`). The core async use case is unsupported.
- **No Settings screen.** Model is only reachable via `/model`; no theme,
  host management, or notification prefs.
- Permission approvals are **blocking modal bottom sheets** — invisible when the
  app isn't foregrounded.

**SessionsScreen**

- **Cannot create a session when the list is non-empty.** The "New session"
  button only renders in the empty state; the populated screen has only Refresh +
  Sign-out (top-right) and, by design, no FAB. New sessions are otherwise only
  reachable via the `/new` command I just added. This mirrors the #1 complaint
  about Anthropic's own Remote Control (see §3).
- Connection-banner logic is **duplicated 4×** (the reconnecting/error ternaries
  repeat inline) — a refactor/readability liability.
- Status is shown as a **raw string chip** (`running`/`idle`/`error`) rather than
  human phrasing.
- Primary actions sit **top-right** — outside the thumb's natural reach on large
  phones (see one-handed research, §5).

**ChatScreen**

- Streaming still shows **transient raw markdown markers** (`**bol…`) before a
  token pair closes — the exact flicker best-practice calls for buffering unclosed
  markers (§5). Our caching/throttle fixed the perf flicker but not this.
- Tool/thought **detail** (on expand) is plain text — fine for raw output, but
  there's no copy button and no syntax highlighting.
- No conversation **fork / edit-and-resubmit / regenerate**.

---

## 3. Competitive landscape (what similar projects do)

| Project | What it is | Notable UX patterns |
|---|---|---|
| **Happy** (`slopus/happy`, MIT) | Native iOS/Android/web client for Claude Code **and** Codex; CLI wrapper (`happy` instead of `claude`) | **Push notifications when the agent needs permission or errors**; **E2E-encrypted, no telemetry**; **instant device handoff** (take control from phone/desktop with one keypress); **full feature parity** (plan mode, MCP, file mentions, slash commands); **multiple concurrent sessions**; **real-time voice** (11Labs + GPT-4.1); QR pairing |
| **Omnara** (YC S25, Apache-2.0, ~2.7k★; archived Feb 2026 → migrated to a voice-first platform) | Cross-device "command center" (terminal + web + mobile) for AI agents | Real-time cross-device dashboard; **human-in-the-loop input mid-task** via a message API; **"agent done / input needed" alerts via push, email, and SMS**; unlimited concurrent agents (Pro $9/mo); headless background execution |
| **Cursor Mobile** | Phone app to spin up / interact with coding agents | Prompt or start new agents from the phone; interact with desktop-initiated agents. Anthropic's Boris Cherny says **most of his coding is now on his phone** — the phone-driven workflow is real |
| **Anthropic "Remote Control"** (official Claude Android app, `claude remote-control`) | First-party phone control of a Claude Code session, via a session list in the Code tab | **Cautionary tale**: send button gets **stuck as a stop icon on idle connect** (can't send the first message from mobile); **no slash commands on mobile** (no `/compact`, `/clear`, `/context`); rendering bugs (infinite loading, responses not visible). Directly informs what to avoid |
| **Vibe Kanban** | Desktop-first kanban orchestration of agents (Claude/Codex/OpenCode); git worktrees, diff review | Agent-agnostic; **has a "Remote Access" feature** (the "no mobile" claim was refuted in verification); not a dedicated mobile app |
| **claude-push / ntfy pattern** | ~60-line bash hook, no custom app | `PermissionRequest` hook → **ntfy.sh push with Allow/Deny buttons** → response returns via SSE; **90-second timeout falls back to the terminal**; unique per-request ID disambiguates concurrent asks; topic name acts as the shared secret |

**Takeaway:** the winners are defined by **notifications + approvals + voice +
device continuity**, not by the chat transcript itself. Our transcript is now
competitive; our async loop is not.

---

## 4. What users ask for, love, and complain about

**Repeatedly requested**

- **Push notification on permission prompt + approve/deny from the phone** — to
  unblock long, unattended tasks (the dominant ask across HN/Reddit/GitHub).
- **Slash commands on mobile**, especially `/compact` when the context window
  fills during a long task; users also want `/`-prefix recognition or a command
  menu / swipe-long-press quick actions.
- **Be able to send the first message from mobile** (the stuck-button complaint).
- **Voice input** for hands-free / on-the-go use.
- **Multiple concurrent sessions** and **device handoff**.

**Loved**

- **Push notifications** (Happy's headline feature).
- **Instant device handoff** — pick up a running session on the phone with one
  keypress.
- **Voice** — enough that Omnara pivoted to voice-first.
- **Full feature parity on mobile** and **E2E-encryption / no telemetry** (trust).

**Common complaints / pitfalls (things to avoid)**

- Core action blocked by a UI-state bug (Anthropic's stuck send button).
- Missing slash commands / `/compact` on mobile.
- Rendering failures — infinite loading, responses not visible.
- **Streaming markdown flicker** and raw syntax leaking mid-stream.
- SSH/tmux from a mobile terminal is "clunky."
- Sync/reliability bugs across desktop↔mobile.

---

## 5. Mobile UX best practices relevant to us

**Streaming & markdown (validates + extends this week's work)**

- Incremental token streaming is the **baseline expectation**; non-streaming
  "feels broken." Streaming drops perceived time-to-first-token to ~200–500 ms.
- **Naive re-parse-per-chunk causes flicker/broken formatting/layout shift** —
  exactly what we fixed with cache + throttle. ✅
- **Buffer unclosed markers while streaming**: count paired `**`, `*`, `` ` ``,
  ` ``` ` and truncate at the first odd (unclosed) marker so raw syntax never
  shows; evaluate code fences before inline code; **hide incomplete code blocks
  until the closing fence**; apply only while `isStreaming`, then render the full
  unmodified text on completion (so intentional lone `*` survives). ← our next
  markdown improvement.
- Re-parse is cheap for chat-length text (<5 KB ≈ <5 ms); throttle to the frame
  rate for long outputs. **Markwon** is the recommended native Android markdown
  engine (we use `flutter_markdown_plus`, which is fine; Markwon is a fallback if
  we hit perf limits). Native rendering also **sidesteps the web XSS/sanitization
  concern** entirely.

**Approvals**

- Keep decisions **granular** — approve only the narrow request or a bounded
  pattern, never a broad category.
- Make **"approve always" deliberately harder** than "approve once," with a
  second confirmation for broad patterns.
- Surface **context**: repo/branch, agent, tool, normalized command/path, whether
  the target is outside the working dir, the exact scope of an "always," and
  recent related approvals.
- **Redact** sensitive data in the notification (full detail server-side; require
  app unlock to view).
- Be **event-driven, not polling**; use per-request IDs to avoid races between
  terminal and phone; provide a **timeout fallback** to the terminal.

**One-handed ergonomics**

- ~49% of users operate one-handed on the go; 90% of phones are >5" so top-placed
  controls fall outside thumb reach.
- **Bottom placement measurably lifts engagement** (Spotify's bottom-nav redesign:
  +9% clicks overall, +30% on menu items). Prefer flyout menus and swipe gestures
  over top-anchored controls. → Our composer is bottom (good); Sessions actions
  and "new session" are not.

---

## 6. Prioritized recommendations (mapped to our code)

### P0 — do first

1. **Push notifications for `permission_request` and `turn_complete`.**
   The daemon already emits both events (`internal/event`,
   `internal/session/manager.go` pump). Because the app kills its socket on
   background, we need a real push channel, not the WS. Options, cheapest→richest:
   - **FCM from the daemon**: daemon posts to Firebase when an owner device has a
     pending permission or a turn completes; app handles the data message,
     deep-links into the session, and (ideally) offers Allow/Deny actions.
   - **ntfy.sh-style relay** (fastest to ship, no app-store review friction):
     mirror the claude-push pattern with per-request IDs + a 90 s terminal
     fallback.
   - Add an **Android foreground service** only if we want live streaming while
     backgrounded; notifications are the higher-value piece.
2. **Create-session affordance on a populated list.** Add a reachable "＋ New
   session" (bottom-aligned button or app-bar action) in `sessions_screen.dart`;
   don't gate session creation behind the empty state. (Lesson from Anthropic's
   "can't send first message.")
3. **Guardrail the composer's busy state** so a fresh connect can always send —
   verify we never initialize "generating," which is the exact Anthropic bug.

### P1 — high value

4. **Granular approval UX + notification path.** Extend the permission sheet:
   approve-once vs approve-always (with a second confirm), show
   repo/tool/command/scope, redact in the notification, event-driven with a
   timeout fallback.
2. **Streaming markdown: unclosed-marker buffering** in `_AssistantMarkdown` /
   `_MarkdownText` — hide odd `**`/`*`/`` ` ``/` ``` ` and incomplete code blocks
   while streaming; render full text on completion. Kills the residual raw-syntax
   flash (ties off the earlier "raw markdown" complaint).
3. **One-handed pass**: move Sessions' primary actions into thumb reach; consider
   a bottom action bar or bottom nav; keep the composer where it is.
4. **Settings screen**: model picker (needs a model list from the daemon/grok),
   theme, host management, notification prefs. Gives `/model` a home and a GUI.

### P2 — differentiators / polish

8. **Voice input** (Happy/Omnara's most-loved) — larger lift.
2. **Device handoff / "take control"** continuity between desktop and phone.
3. **Conversation fork / edit-and-resubmit / regenerate**.
4. **Humanize status** (not raw `running/idle`) and **de-duplicate** the
    connection-banner logic in `sessions_screen.dart`.
5. **Copy buttons + syntax highlighting** in code blocks and tool output.

---

## 7. Method & confidence

- Web findings came from a fan-out research pass (multiple search angles → source
  fetch → **3-vote adversarial verification per claim**). Of the verified claims
  used here, **50 were upheld, 3 refuted** — the refuted ones all wrongly asserted
  "Vibe Kanban has no mobile," which its own Remote Access docs contradict; that
  correction is reflected above.
- Primary sources include the Happy (`github.com/slopus/happy`) and Omnara
  (`github.com/omnara-ai/omnara`) repos, `vibekanban.com`, Anthropic Claude Code
  GitHub issues (#28926, #29215), and mobile-UX blogs (one-handed reach, bottom-
  nav engagement). Product facts (licenses, features, YC batch) are primary-repo
  confirmed; engagement stats are single-blog-sourced and should be treated as
  directional.
- The codebase assessment is grounded in the current `apps/mobile/lib` source at
  the time of writing.
