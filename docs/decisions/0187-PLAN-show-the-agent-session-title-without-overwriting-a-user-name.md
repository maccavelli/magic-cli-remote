---
status: proposed
date: 2026-10-13
associated-madr: "0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing the agent's session title without overwriting a user name

Associated MADR: [0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"Map the title as a generated title; a name the user set wins" (D1–D5). If the owner chooses another
option, this PLAN is rewritten first. A phase starts only after the owner approves it by name. Each
phase ends with `make pre-add-check` on its staged Go files, `go test -race ./...` and the 0145 gates
on a Windows host. The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

An unnamed grok (or gobble) session shows the agent's title on the phone. A name the user set, at
create or by Rename, is never replaced by a generated title. Codex's title behavior does not change.

## Scope

* `internal/event/event.go`: the `TitleGenerated` field (D1).
* `internal/provider/acpagent/session.go`: the `u.SessionInfoUpdate` case (D1).
* `internal/session/manager.go` and `internal/session/store.go`: `name_source` on `Meta` and
  `Record`, set at create, rename and fork, and the `session_title` gate (D2–D4).
* Tests beside each, and `docs/guides/protocol-v1.md` for the new event field and the `name_source`
  key.
* **Out of scope:**
  * any Flutter change: the phone's `session_title` handling is unchanged;
  * a "reset to the agent's title" action;
  * grok's recap (`sessionRecap`);
  * any capability advertisement (D5).

## Implementation Steps

### T1 — Map the title in acpagent (D1)

1. Write a test that replays the two `session_info_update` frames from
   `internal/provider/grok/testdata/wire/1.0.13/frames.jsonl` (lines 88 and 197) through
   `SessionUpdate`. It requires two `session_title` events, in order, with `Title` set and
   `TitleGenerated` true. Show it failing on the current decline.
2. Add `TitleGenerated bool` (`json:"title_generated,omitempty"`) to `event.Event`, documented as
   "set by a provider whose titles the agent writes itself".
3. In the `u.SessionInfoUpdate` case, emit `session_title` with the trimmed title when it is
   non-empty. Decline `session_info_update.updated_at` and `session_info_update._meta` by name when
   present.
4. Update `sessionupdate_test.go`'s `SessionInfoUpdate` row from "declined" to "emits
   `session_title`".

### T2 — Name source and the gate in the manager (D2–D4)

1. Write the manager tests listed in the MADR's Confirmation:
   * a generated title into an unnamed session is applied, with source `generated`;
   * a refined title is applied again;
   * a generated title into a `user`-named session is ignored;
   * a codex-style title (no flag) into a `user`-named session is applied, as today;
   * an old record with a name and no source keeps its name.

   Show them failing on the current code.
2. Add `NameSource string` (`json:"name_source,omitempty"`) to `Meta` and `Record`, with the
   constants `NameSourceUser` and `NameSourceGenerated`.
3. Set the source:
   * create with a non-empty name: `user`;
   * `Manager.Rename`: `user`;
   * fork: the parent's source;
   * an applied generated title: `generated`.

   On load, a record with a name and no source is `user` (D4).
4. Gate the `session_title` branch at `manager.go:1342-1348`: when `ev.TitleGenerated` is true and
   the source is `user`, do not apply it.
5. A persistence test round-trips `name_source` through `meta.json`, and reads a record written
   without it.

### T3 — Document

1. In `docs/guides/protocol-v1.md`, add `title_generated` to the `session_title` event and
   `name_source` to the session metadata. State that a client may ignore both.

### T4 — Live probe (owner gate: quota and a funded grok account)

1. Create an unnamed grok session and run two turns. Record the titles, and check that the phone
   shows them.
2. Rename the session from the phone and run another turn. Record whether grok emits a title after
   `x.ai/session/rename`, and check that the user's name is kept.
3. Commit the capture, redacted to the frames that show this, beside the existing grok captures.

## Verification

* T1 and T2: every new test is seen failing first, on a scratch copy, and passes after.
* `go test -race ./...`, `make pre-add-check` on the staged files, `make lint`.
* `make ci-windows` on the Windows test host.
* `python3 scripts/check_records.py` and `make markdownlint-docs` after the record and guide changes.
* T4's live probe, with its capture.

## Rollout and Rollback

* T1 alone would let generated titles rename user-named sessions. It ships together with T2, never
  before it.
* `name_source` is additive and `omitempty`. Rolling back leaves the key in `meta.json`, and an older
  daemon ignores it. A name set while this was live is still the session's name.
* Reverting T1 restores the decline. Names already applied stay, as any rename's would.
