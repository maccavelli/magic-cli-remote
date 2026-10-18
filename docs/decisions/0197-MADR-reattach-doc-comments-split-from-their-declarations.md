---
status: accepted
date: 2026-10-18
decision-makers: Project Owner
consulted: none
informed: contributors who edit the Flutter app's sources
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Reattach the Flutter app's doc comments that were split from their declarations

## Context and Problem Statement

[0190-PLAN-show-agent-context-compaction-on-the-phone.md](0190-PLAN-show-agent-context-compaction-on-the-phone.md)
noted, and left alone, that `_LimitNotice`'s doc comment in `chat_bubble.dart` sits on top of
`_AgentNotice`'s, so the two read as one. A survey on 2026-10-18 found that this one comment is
part of a larger pattern.

Dart attaches a `///` block to the declaration directly below it. When a new declaration, with its
own doc comment, is inserted between an existing doc comment and the declaration it describes:

* the new declaration's doc now opens with a paragraph about something else;
* the old declaration is left with no doc at all, and the IDE hover and `dart doc` show nothing for
  it;
* nothing fails. `flutter analyze` has no rule for it, and the code compiles and behaves the same.

### What the survey found

A scratchpad scan of `apps/mobile/lib` listed every `///` line that starts a new capitalised
sentence directly after a line ending in `.`. Each of its 69 candidates was read by hand. Eleven
places are fused, in eight files. In each one the orphaned comment's owner still exists:

| File and lines | The orphaned comment describes | It belongs on |
| --- | --- | --- |
| `features/chat/chat_bubble.dart:468–470` | the usage-limit card | `_LimitNotice` (`:652`) |
| `features/chat/chat_bubble.dart:787–789` | assistant text as markdown, "Selectable" | `_AssistantMarkdown` (`:802`), which has its own, newer doc |
| `data/chat/chat_models.dart:5–8` | an attachment on a user bubble | `ChatAttachment` (`:42`) |
| `data/protocol/models.dart:1955–1956` | the `usage_update` report | `Usage` (`:2114`) |
| `data/protocol/models.dart:2475–2476` | a collaboration preset | `CollaborationMode` (`:2512`) |
| `features/sessions/sessions_screen.dart:32–40` | a past timestamp for a picker row | `_humanTimestamp` (`:118`) |
| `features/sessions/sessions_screen.dart:41–45` | a session's one-line accounting | `sessionUsageLabel` (`:106`) |
| `features/chat/chat_screen.dart:3466` | a staged image's thumbnail | `_AttachmentThumb` (`:3554`) |
| `features/settings/provider_detail_screen.dart:666–673` | removing one credential | `_clearCredential` (`:721`) |
| `theme/starfield.dart:7–10` | the static starfield painter | `StarfieldPainter` (`:19`) |
| `theme/widgets.dart:137–138` | the connection banner | `ConnBanner` (`:146`) |

`sessions_screen.dart:32–50` holds three comments in one block: two orphans and the doc of
`ProjectPickerList`, the declaration that block is actually attached to.

The second `chat_bubble.dart` case is not a plain move. The orphan is an older doc of the same
class, and part of it is now wrong: the markdown is selectable only once the reply has finished
(`selectable: !streaming`).

The 0190 PLAN's note blames 0188 N2 (`fc737d36`). That is not correct. The comment was first split
from `_LimitNotice` by `0c55297e` (MADR 0069, 2026-08-04), which put `_PermissionNotice` and
`_AuthNotice` between them. 0188 N2 then inserted `_AgentNotice`'s doc directly below it, which is
what made the two read as one. The other ten were not traced to their commits.

**Go is not affected.** The same scan, adapted to Go's rule that a doc comment opens with the
declared name, flagged 8 lines across `internal/`. Each is a sentence naming another function
partway through a comment, not a second comment. golint already rejects an exported Go
declaration whose doc does not start with its name.

### The question

How should the eleven be fixed, and should anything stop the pattern recurring?

## Decision Drivers

* A doc comment on the wrong declaration misinforms more than a missing one does.
* The fix must not change behaviour. Doc comments only.
* A guard is worth keeping only if it fails on the fault and stays quiet on correct code.
* `apps/mobile` changes go through `make preflight` (`dart format`, `flutter analyze`, `flutter test`).

## Considered Options

* Move each orphan onto its owner, and merge the stale `_AssistantMarkdown` one; no guard
* The same fix, plus a test that runs the scan's heuristic with an allowlist of reviewed lines
* Fix only `_LimitNotice`, the one the backlog names
* Delete the orphaned comments

## Decision Outcome

Chosen option: "Move each orphan onto its owner, and merge the stale `_AssistantMarkdown` one; no
guard", because it puts every comment back where Dart and the IDE look for it with no behaviour
change, and because the only guard available is a heuristic whose candidates are mostly correct
prose.

* **F1.** Each orphan in the table moves, unchanged, to directly above its owner, above any
  annotation such as `@visibleForTesting`. The doc left behind on the declaration below then starts
  where it should.
* **F2.** `chat_bubble.dart:787–789` merges into `_AssistantMarkdown`'s own doc. Its one fact that
  is not already there, that the text is selectable and sized to the body text, is kept and
  corrected to "selectable once the reply is finished". Nothing else is reworded.
* **F3.** No guard. The scan's method and both scripts' rules are recorded in the PLAN, so the next
  survey can be repeated.
* **F4.** The 0190 PLAN's note is annotated, not rewritten. It gets a pointer to this record and
  the corrected commit.

### Consequences

* Good, because eleven declarations get back the doc that was written for them, and the eleven
  docs that were fused read as written.
* Good, because the fix touches comments only, so `flutter test` stays unchanged.
* Bad, because nothing stops the pattern from coming back. The next survey will find it only if
  someone runs one.
* Neutral, because 0190's note keeps its wrong attribution, struck through, with the correction
  beside it.

### Confirmation

* Each site's diff moves comment lines only. A per-file check confirms that the file minus every
  `///` line is byte-identical before and after.
* The scan, run again, no longer lists the eleven lines.
* `make preflight` passes.

## Pros and Cons of the Options

### Move each orphan onto its owner; no guard

* Good, because every comment ends up on the declaration it describes.
* Good, because it changes no behaviour, so the change is small to review.
* Bad, because a recurrence will not be caught.

### The same fix, plus a heuristic test with an allowlist

* Good, because it would catch a new fusion that opens with a capitalised sentence.
* Bad, because 58 of the scan's 69 candidates are correct prose. The allowlist would be larger than
  the fault, and each new two-sentence comment would need an entry.
* Bad, because a fusion whose second comment starts with a word the heuristic skips, such as "The",
  passes it anyway.

### Fix only `_LimitNotice`

* Good, because it is the smallest change, and it is what the backlog named.
* Bad, because it leaves ten known faults in place, and the survey that found them would have to
  be repeated.

### Delete the orphaned comments

* Good, because it is mechanical.
* Bad, because it throws away accurate documentation, such as the `Usage` and
  `_clearCredential` docs, to fix where it sits.

## More Information

* The scripts are scratchpad-only. The PLAN records their rules and their candidate counts.
* MADR 0069 (`0c55297e`) and 0188 N2 (`fc737d36`) are where `_LimitNotice`'s comment was split.
