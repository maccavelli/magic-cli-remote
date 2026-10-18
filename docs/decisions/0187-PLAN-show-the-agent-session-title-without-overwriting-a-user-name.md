---
status: complete
date: 2026-10-18
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

### T1 and T2, 2026-10-13: complete (committed as `145713d7`)

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
  * ~~T3 (`docs/guides/protocol-v1.md`) and~~ T4 (the live probe): not approved. T3 is below;
  * ~~no commit: T1 and T2 are staged, and a commit needs the owner's word in its turn;~~ the owner
    committed `145713d7` and pushed it;
  * CI on push;
  * no Flutter change, as the PLAN says: the phone already handles `session_title`.

### T3, 2026-10-13: implemented and verified; not committed

**Approval:** the owner on 2026-10-13, "committed and pushed, do T3".

`docs/guides/protocol-v1.md`:

* **Session `Meta`:** the example carries `"name_source": "user"`. A new bullet gives its values,
  where each comes from, and the rule for records persisted before the field. It also says a client
  may ignore the field.
* **Domain event fields:** a `title_generated` bullet beside `title`, which a client may ignore.
* **`session_title` event:** the two sources (an ACP agent's own title, and a rename inside the
  engine), with `title_generated` in the example.
* **Correction.** The section said a client rename and an agent title "either may win — the last one
  applied is the title". That has been false since T2. It now says a generated title never replaces
  a `user` name, and is dropped before history and push. An unflagged title is applied as before.
* **`session.rename`:** a successful rename sets `name_source` to `user`.

**Verification,** each exit status recorded before reading its output:

```text
make markdownlint-docs                          exit=0, "Summary: 0 issues in 0 files" (51 files)
go test -count=1 ./internal/protocol/ ./internal/event/   exit=0 (the event-type and error-code guards)
```

The new in-page link, `#session_title-event-session-title-update`, is checked by markdownlint's
MD051. That rule was seen catching a broken fragment on a scratch copy of the guide (the repo's
`.markdownlint-cli2.jsonc` beside it):

```text
broken.md:1163:5 error MD051/link-fragments Link fragments should be valid
  [Context: "[`session_title`](#session_title-event-no-such-heading)"]
```

The intact copy, linted in the same run, reported nothing.

**Not done:**

* T4 (the live probe): **deferred by the owner, 2026-10-13** ("we will run the grok live test when
  my account resets, i used all my quota"). It runs once the grok account's quota resets. Until
  then, grok's title behavior is known only from the two committed captures, and this PLAN stays
  `in-progress`;
* no commit: T3 is staged, and a commit needs the owner's word in its turn.

### T4, 2026-10-18: steps 1 to 3 on the wire; the phone check is not done

The owner approved the quota on 2026-10-18 ("OK to proceed with quota"). Four probes ran on
grok `1.0.50 (c58f321264ba)`, each in its own session with `AlwaysApprove` and
`MCREMOTE_WIRE_CAPTURE_DIR` set. They came from a `live_grok` test in a scratch copy of the tree,
which was not committed:

* **titles:** two one-word turns, `x.ai/session/rename`, and a third turn;
* **plan:** a turn asking grok to plan with its todo tool first;
* **ask:** a turn asking grok to ask the user a question, which the probe cancelled;
* **compact:** two turns, then `CompactSession.Compact`.

None of the four captures holds a standard `notice`, `usage_update`, `compaction_update`,
`compaction_summary_chunk`, `plan_update`, `plan_removed` or `elicitation/create`.

**Step 1.** The titles session ran two turns. grok sent one `session_info_update`, about 2 s after
the second turn ended, with nothing after the first turn:

```text
{"sessionUpdate":"session_info_update","title":"Sequential one-two exact-word replies"}
```

Magic emitted it as `session_title` with `title_generated: true` (D1). Each of the other three
sessions also got exactly one title:

* the compact session, which also ran two turns, got it 2 s after its second turn;
* the plan and ask sessions, which ran one turn each, got it during or just after that turn.

These turns took about 2 s each, so a title written from the first turn may simply arrive after
the second. The probe does not tell those two cases apart.

**Step 2.** `x.ai/session/rename` to "probe user name" returned no error. grok echoed the name at
once, and marked it as manual:

```text
{"sessionUpdate":"session_info_update","title":"probe user name"},"_meta":{"x.ai/titleIsManual":true}
```

Magic emitted the echo as `session_title` with `title_generated: true`, because acpagent does not
read `_meta`. A third turn brought no further title.

* When the rename came from the phone, this is harmless. The manager already holds the name as a
  `user` name, and D3 keeps a generated title from replacing it. The echo is also the same text.
* A name the user sets inside grok itself arrives marked as generated, so Magic would treat it as
  replaceable. Reading `x.ai/titleIsManual` is a new decision, not this PLAN's.

**Step 3.** Both frames are appended verbatim to
`internal/provider/grok/testdata/wire/1.0.50/frames.jsonl`, and its `meta.json` says so (14 to 16
frames). Neither carries a path or an account. `TestGrok1050FramesDecodeAsStandardConfigOptions`
and the `wirecap` fixture guard still pass.

**Not done.**

* **Showing on the phone.** Steps 1 and 2 ask for a check that the phone shows the titles and
  keeps the user's name. The probe ran at the provider, not through the daemon and a paired phone.
  The manager's half is covered by the D2 and D3 unit tests, but not live. ~~The check waits for
  the owner's decision. The PLAN stays `in-progress`.~~ Done the same day; see the phone check
  below.

### Deviation, 2026-10-18: the phone check needs a scratch daemon and one manual step

The owner chose to run the phone check on the Android emulator. Two facts stood in the way:

* The emulator is paired only with the host's installed daemon. That build is 0.20.1, from
  2026-09-30, which predates this record (2026-10-13), so it cannot show 0187's behaviour.
* Pairing with a daemon built from this tree needs a QR code read by the emulator's virtual
  camera, and the camera can only be turned toward it by hand
  (`apps/mobile/docs/guides/ops-android-emulator.md`). A typed code is refused by design, because
  it carries no certificate fingerprint (MADR 0046/0074).

**Decision (owner, 2026-10-18): "Scratch daemon + you aim".**

* The tree's daemon runs with a scratch config, on its own port and with its own state. The
  installed daemon and its store are not touched.
* The tree's app is installed on the emulator.
* The owner turns the camera toward the pairing QR once. Everything after that is driven over adb.
* The stock poster is restored afterwards.

**Amended the same day: no manual step was needed.** The connect screen's "Paste URI / code /
token" accepts the whole pair URI. That URI carries the certificate fingerprint (`fp=`), which a
typed short code does not. The URI was typed into a text field inside the emulator, copied with
the field's own Copy, and pasted. The scratch daemon logged `device paired via short code`. The
host's clipboard was not used. The emulator guide says the QR is the only way in, which is no
longer right; that guide is not this PLAN's to change.

**The emulator's existing pairing was protected.** The app keeps one host and one device token,
so pairing would have overwritten the pairing with the installed daemon. The owner chose a
backup. The app's private data was copied out with `run-as` before pairing, and copied back
afterwards:

* All 20 files in `shared_prefs`, `databases` and `files` matched the backup by checksum.
* No file was added or removed.
* The app came back "Connected to" the installed daemon's host, with its three earlier sessions.
* The first restore attempt left `shared_prefs` and `databases` out. It was redone from a smaller
  archive before the check above.
* The backup was deleted afterwards. It held the device token only as ciphertext, under a key
  kept in the emulator's keystore.

### T4, 2026-10-18: the phone check — complete

The tree's daemon and debug app were used. The installed daemon was not touched, and the app
build installed on the emulator is now this tree's debug build.

1. A grok session was created from the phone with no name. It showed as "Session db5c01c5".
2. Two turns were sent from the phone, "Reply with exactly the word: one", then "… two". The chat
   header then showed grok's generated title, "Exact One-Word The…". The sessions list showed
   "Exact One-Word Then …". **Step 1 passes on the phone.**
3. The session was renamed from the sessions list's Rename to "my phone name". The dialog was
   prefilled with the generated title, 36 characters. The list showed the new name at once.
4. A third turn was sent. Afterwards the chat header still read "my phone name". The daemon's
   record held `{'name': 'my phone name', 'name_source': 'user'}`. **Step 2 passes on the
   phone:** the user's name is kept (D2, D3).

Screenshots stayed in the session scratch directory. They show host paths and are not committed.

**Cleanup.**

* The emulator, which was not running before, was shut down.
* The scratch daemon was stopped, and no grok agent was left running.
* The scratch daemon's runtime directory, data, state, pair code and QR were deleted.
* The stock poster was restored and checked with `cmp`.
* The installed daemon was still running afterwards.

T4 is complete, so this PLAN is `complete`.
