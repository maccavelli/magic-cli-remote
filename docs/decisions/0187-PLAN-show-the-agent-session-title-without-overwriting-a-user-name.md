---
status: in-progress
date: 2026-10-13
associated-madr: "0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing the agent's session title without overwriting a user name

Associated MADR: [0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-13 the owner accepted the
MADR's recommended option and approved T1 and T2 ("accept option 1, do T1 and T2"). This PLAN
implements that option, "Map the title as a generated title; a name the user set wins" (D1–D5).
T3 and T4 still wait for approval by name. A phase starts only after the owner approves it by name. Each
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

## Execution record

### Deviation, 2026-10-13: three cases T2 step 3 did not name

They were found while implementing T2, are inside its files (`internal/session/manager.go`), and
are recorded in the MADR's amendment of the same date. Without them D3 does not hold:

* **Resume and relaunch keep the stored source.** The phone resumes by re-sending the session's id
  and name, and `relaunch` does the same for a live session. Under step 3 as written, either would
  mark an agent-titled name the user's. `Create` now delegates to `create(…, nameSource)`. When
  that call re-opens an existing id, live or stored, under its own name, it keeps the stored
  source, and D4 applies when there is none. Fork passes its source explicitly.
* **A refused generated title is withheld.** It is dropped before history and broadcast
  (`m.mu.Unlock(); continue`, the duplicate-notice pattern), because the phone renames the chat on
  any `session_title`.
* **An unflagged title marks the name `user`.** It follows a rename the user made in the engine
  (codex).

### T1 and T2, 2026-10-13: implemented and verified; not committed

**Approval:** the owner on 2026-10-13, "accept option 1, do T1 and T2".

**T1.**

* `event.Event.TitleGenerated` (`title_generated`) was added first, with no behavior, so the test
  could compile against the code before T1.
* `TestSessionInfoUpdateTitleIsAGeneratedSessionTitle` replays the two `session_info_update` frames
  of the committed `1.0.13` capture through `SessionUpdate`. On the decline it **failed**:
  `session_title events = 0, want 2`.
* The `u.SessionInfoUpdate` case now emits `session_title` with the trimmed title and
  `TitleGenerated: true`. It emits nothing for a missing or blank title, and declines `updatedAt`
  and `_meta` by name.
* `TestSessionInfoUpdateWithoutATitleEmitsNothing` covers the blank cases. The A4 table's
  `SessionInfoUpdate` row now expects `session_title` and the `updated_at` decline.

**T2.**

* `Meta.NameSource` and `Record.NameSource` (`name_source`, `omitempty`) were added first, with no
  behavior, with `NameSourceUser`, `NameSourceGenerated` and `effectiveNameSource` (D4).
* `sessionname_test.go` and `sessionname_fork_test.go` were written next. On the code before the
  gate, five tests **failed**:
  * `TestGeneratedTitleNamesAnUnnamedSession`: `"First title"/"", want First title/generated`;
  * `TestGeneratedTitleNeverReplacesAUserName`: `named at create: source "", want user`;
  * `TestUnflaggedTitleIsAppliedOverAUserName`: `"renamed in codex"/"", want …/user`;
  * `TestResumeKeepsTheStoredNameSource`: `resumed generated name: source "", want generated`;
  * `TestForkInheritsTheNameSource`: `"work (fork)"/"", want …/"user"`, and `"fork"/"", want
    …/"generated"`.

  `TestNameSourceIsPersisted` passed at once, since it exercises only the new field, and was proved
  by plant P10 below. `TestManagerWritesTheNameSource` was added after a review of the plants found
  nothing covering the manager's write.
* The source is set by `create`, `Rename` and `Fork`, and in the `session_title` branch. The branch
  withholds a generated title over a `user` name. `writePersist` writes the source, and the stored
  list reads it through `effectiveNameSource`.

**Fail first, on scratch copies.** All eleven plants failed their tests:

| Plant | Caught by |
| :--- | :--- |
| P1 title not marked generated | `TestSessionInfoUpdateTitleIsAGeneratedSessionTitle` |
| P2 blank title not trimmed | `TestSessionInfoUpdateWithoutATitleEmitsNothing` |
| P3 the gate removed | `TestGeneratedTitleNeverReplacesAUserName` ("Agent title"/"generated") |
| P4 refused title still forwarded | the same ("still reached history (and so the phone)") |
| P5 `Rename` not marking `user` | the same ("Agent title 2"/"generated", want picked/user) |
| P6 resume ignoring the stored source | `TestResumeKeepsTheStoredNameSource` (source "user", want generated) |
| P7 the pre-0187 rule removed | the same, and `TestNameSourceIsPersisted` |
| P8 every fork marked `user` | `TestForkInheritsTheNameSource` |
| P9 the manager not writing the source | `TestManagerWritesTheNameSource` |
| P10 the record dropping the field (`json:"-"`) | `TestNameSourceIsPersisted` |
| P11 an unflagged title marked generated | `TestUnflaggedTitleIsAppliedOverAUserName` |

**Verification,** each exit status recorded before reading its output:

```text
go test -race -count=1 ./...                         exit=0
./scripts/go-precheck.sh <8 changed Go files>        exit=0 (gofmt, golint, govulncheck)
make lint                                            exit=0
```

* **Windows.** `make ci-windows` ran on the Windows test host, in Git Bash, against the T1/T2
  working tree, as a tarball of the 1847 files git lists. It was taken after the last Go change.
  Result: exit 0, "ci-windows-local: ALL SELECTED CHECKS PASSED", with `internal/event`,
  `internal/provider/acpagent`, `internal/provider/grok` and `internal/session` all `ok`.
* **Not done:**
  * T3 (`docs/guides/protocol-v1.md`) and T4 (the live probe): not approved;
  * no commit: T1 and T2 are staged, and a commit needs the owner's word in its turn;
  * CI on push;
  * no Flutter change, as the PLAN says: the phone already handles `session_title`.
