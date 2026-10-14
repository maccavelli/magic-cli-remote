---
status: proposed
date: 2026-10-13
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Show the agent's session title from `session_info_update`, without overwriting a name the user set

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 takes up ACP capabilities one record at a time. The session title from `session_info_update`
comes first, because grok already sends it. Since 0184's A4 (`900178d3`), acpagent declines the
variant: it is named once per session at Debug and otherwise dropped
(`internal/provider/acpagent/session.go`, the `u.SessionInfoUpdate` case).

**What grok sends.** The committed `1.0.13` capture
(`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`, lines 88 and 197) carries two titles
in one session. The second refines the first:

```text
{"sessionUpdate":"session_info_update","title":"Initial User Greeting and Conversation Start"}
{"sessionUpdate":"session_info_update","title":"User Greeting to Initiate Conversation Session"}
```

0184's A3 live run against grok `1.0.50` sent six titles across its sessions. Each is a session's
first prompt cut to about 60 characters, for example "Create a file named probe.txt containing OK.
Do it now,". Every model call in that run was refused with HTTP 402 (the Grok Build balance was
exhausted), so this may be grok's fallback when it cannot summarise, not its normal title. Either
way, grok writes titles itself, without being asked, and may write more than one per session.

**What Magic already has.** Everything past the provider exists, and codex uses it.

* A provider event `session_title` (`internal/event/event.go:59-60`, field `Title` at `:572-573`) is
  must-deliver, and is documented in `docs/guides/protocol-v1.md`
  ([0036-MADR-protocol-contract-completeness.md](0036-MADR-protocol-contract-completeness.md)).
* The session manager applies it: any non-empty title that differs from the current name replaces
  `Meta.Name`, and is persisted to the session's `meta.json`
  (`internal/session/manager.go:1342-1348`).
* The phone updates the chat title, the session-list row and the notification labels on
  `session_title`. It needs no change. Codex emits `session_title` from `thread/name/updated`
  (`internal/provider/codex/session.go:1408-1413`).
* A user names a session in two ways. One is the optional name at create: the phone sends `null`
  when the field is empty, and shows `Session <id8>` for an unnamed session. The other is the phone's
  Rename action, `session.rename`
  ([0031-MADR-opencode-catalog-and-metadata-parity.md](0031-MADR-opencode-catalog-and-metadata-parity.md)),
  which for grok calls `x.ai/session/rename` (`internal/provider/acpagent/sessioncaps.go:256-270`).
  `Manager.Rename` sets `Meta.Name` and records nothing about where a name came from
  (`internal/session/manager.go:2391-2429`).

**The problem.** Mapping grok's title onto `session_title` is a few lines, and it makes unnamed grok
sessions readable on the phone. But the manager lets any title replace any name. A session the user
named, at create or by Rename, would be renamed by grok's next generated title. That includes grok's
`1.0.13` habit of refining a title mid-session, and possibly a title grok emits after the user's own
rename. Codex does not have this problem: its `thread/name/updated` follows a rename the user made.
grok's titles are generated.

Which wins when grok's generated title and a user's name disagree?

## Decision Drivers

* A name the user typed is the user's. Losing it silently is a defect, not a feature.
* Unnamed sessions, which the phone shows as `Session <id8>`, are the common case, and are the ones a
  generated title helps.
* No change on the phone. The `session_title` path already exists end to end.
* Codex's behavior does not change: its titles follow user renames.
* Old session records, written before this decision, must not lose their names either.
* The same mapping serves every ACP agent, gobble included
  ([0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md)), not only
  grok.
* Session names stay out of APNs-visible push content
  ([0121-MADR-achieve-iphone-functional-parity.md](0121-MADR-achieve-iphone-functional-parity.md)).
  This decision does not widen where a name is shown.

## Considered Options

* Map the title as a generated title; a name the user set wins
* Map the title; the newest title always wins, as codex's does today
* Map the title only into a session that has no name, and only once
* Keep declining `session_info_update`

## Decision Outcome

Chosen option: none yet, because this record is `proposed`. Recommended: "Map the title as a
generated title; a name the user set wins". It is the only option that gives unnamed sessions a
readable title and also keeps a user's name. It also leaves codex as it is.

Under the recommended option:

* **D1 — acpagent maps `session_info_update.title` to `session_title`, marked generated.**
  `event.Event` gains `TitleGenerated bool`, set only by acpagent. A title that is empty after
  trimming is ignored. `updatedAt` and `_meta` are not used, and are declined by name, once per
  session, as 0184's D7 declines `usage_update`'s `cost`.
* **D2 — the manager records where a session's name came from.** `Meta` and the on-disk `Record`
  gain `name_source`: `user` or `generated`.
  * A non-empty name at create, a Rename, and a fork of a `user`-named session set `user`.
  * A generated title the manager applies sets `generated`.
* **D3 — a generated title never replaces a `user` name.** The manager applies a `session_title`
  with `TitleGenerated` only when `name_source` is not `user`. A `session_title` without the flag
  (codex) is applied as today.
* **D4 — old records keep their names.** A record with a non-empty name and no `name_source` is
  treated as `user`. An empty name with no source accepts generated titles.
* **D5 — nothing is advertised.** `session_info_update` is a stable v1 variant that needs no
  client capability, so 0184's D4 advertisement is unchanged. D10's condition, phone behavior, a wire
  fixture and a live probe, is met by: the existing phone path; the committed `1.0.13` frames; and a
  live probe, which needs the owner's quota approval and a funded account (see Confirmation).

### Consequences

* Good, because an unnamed grok or gobble session shows the agent's title on the phone instead of
  `Session <id8>`, with no Flutter change.
* Good, because a name the user typed, at create or by Rename, survives every later generated title.
* Good, because codex behaves exactly as before.
* Neutral, because `Meta` and `Record` gain one field. It is additive and `omitempty`. The phone
  ignores an unknown key, and an older daemon ignores it on disk.
* Neutral, because a user who wants generated titles back on a session they named has no "reset"
  action. Renaming to the agent's title by hand is the workaround, and no one has asked for more.
* Bad, because a generated title arriving after a user's rename of the same session is dropped even
  when it is grok echoing the user's own new name. It is then equal to `Meta.Name` and changes
  nothing, so the cost is nil, but the event is not applied.

### Confirmation

* A test replays the committed `1.0.13` `session_info_update` frames through acpagent. It requires
  `session_title` events with `TitleGenerated` set, and the two titles in order.
* Manager tests cover each case: a generated title into an unnamed session (applied, source
  `generated`); a refined title (applied again); a generated title into a `user`-named session
  (ignored); a codex-style title into a `user`-named session (applied, as today); and an old record
  with a name and no source (kept).
* A persistence test round-trips `name_source` through `meta.json`, and reads an old record without
  the field.
* Each new test is seen failing on a scratch copy of the code before it, as the repository requires.
* **Live probe, owner-gated** (quota and a funded grok account): create an unnamed grok session and
  run two turns, recording the titles. Then rename it from the phone and run another turn. That
  records whether grok emits a title after `x.ai/session/rename`, and shows the user's name kept. The
  wire capture is committed, redacted, beside the existing ones.

## Pros and Cons of the Options

### Map the title as a generated title; a name the user set wins

* Good, because both the unnamed-session case and the user's name are served.
* Good, because the precedence lives in the manager, so it holds for every agent that sends
  generated titles.
* Neutral, because it adds one persisted field and one event flag.
* Bad, because it is the largest of the options: acpagent, `event`, the manager and the store.

### Map the title; the newest title always wins, as codex's does today

* Good, because it is the smallest change: one case in acpagent.
* Good, because it matches codex exactly.
* Bad, because grok renames sessions the user named. With `1.0.13`'s mid-session refinement, that
  can happen more than once in one conversation.

### Map the title only into a session that has no name, and only once

* Good, because a user's name is never touched, and it needs no new field: "has no name" is
  `Meta.Name == ""`.
* Bad, because the first generated title freezes. `1.0.13`'s refined second title, and a summary
  that replaces a truncated-prompt fallback, never arrive.
* Bad, because "only once" has to be remembered across a daemon restart anyway, which brings back
  the persisted field it was meant to avoid.

### Keep declining `session_info_update`

* Good, because nothing changes.
* Bad, because unnamed grok sessions stay `Session <id8>` on the phone, although grok sends a title.

## More Information

* The phone path and the codex precedent were mapped from the code on 2026-10-13, at `a7c390c6`. The
  file and line references above are from that read.
* grok's title behavior is measured only from the two captures named above. Whether `1.0.50` writes
  real summaries with a funded account, and whether it emits a title after `x.ai/session/rename`, is
  for the live probe.
* [0106-MADR-grok-1.0.5-surface-parity.md](0106-MADR-grok-1.0.5-surface-parity.md) listed session
  titles and the recap as later work. This record takes up the title only.
* The PLAN is [0187-PLAN-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-PLAN-show-the-agent-session-title-without-overwriting-a-user-name.md).
  Nothing is implemented until the owner accepts an option and approves the PLAN.
