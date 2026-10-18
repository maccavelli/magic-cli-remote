---
status: proposed
date: 2026-10-18
associated-madr: "0197-MADR-reattach-doc-comments-split-from-their-declarations.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement reattaching the Flutter app's doc comments that were split from their declarations

Associated MADR: [0197-MADR-reattach-doc-comments-split-from-their-declarations.md](0197-MADR-reattach-doc-comments-split-from-their-declarations.md)

* The owner approves the PLAN by name.
* Dart files pass `dart format`, and the phase runs `make preflight`.
* The owner commits (`git commit --no-edit`). An agent stages only.

## Goal

Every doc comment in the MADR's table sits directly above the declaration it describes, and no
code changes.

## Scope

* Eight files under `apps/mobile/lib`, comments only:
  * `features/chat/chat_bubble.dart`, `features/chat/chat_screen.dart`;
  * `data/chat/chat_models.dart`, `data/protocol/models.dart`;
  * `features/sessions/sessions_screen.dart`, `features/settings/provider_detail_screen.dart`;
  * `theme/starfield.dart`, `theme/widgets.dart`.
* `docs/decisions/0190-PLAN-show-agent-context-compaction-on-the-phone.md`: its "Noticed, not
  changed" note (F4).
* Out of scope: Go sources, which the MADR found unaffected. Rewording any comment beyond F2.

## Implementation Steps

### D1. Move (F1, F2)

1. A scratchpad Python script makes each move. For each site it asserts that the orphan's text, and
   the owner's declaration line, each occur exactly once before it changes anything. It removes the
   orphan's lines and inserts them directly above the owner, above any annotation.
2. `chat_bubble.dart:787–789` is merged by hand (F2). The merged doc keeps `_AssistantMarkdown`'s
   own text and adds one sentence: the reply is rendered selectable once it has finished, and is
   sized to the surrounding body text.
3. Line numbers in the MADR's table are as of `655a974e`. The script locates sites by text, not by
   line number.

### D2. Prove nothing but comments moved

1. For each file, the text with every `///` line removed is byte-identical to the same text from
   `HEAD`. The scratchpad script prints one line per file.
2. Run the same check on a scratch copy with one code line changed. It must fail, naming the file.
   That shows the check can see a code change.
3. Run the MADR's scan again. None of the eleven lines may appear, so 69 candidates should become
   58. Any candidate that is new is read in context and recorded.

### D3. Records (F4)

1. The 0190 PLAN's note gets a strike-through on "introduced by 0188 N2 (`fc737d36`)" and the
   correction beside it, linking the 0197 MADR. Nothing else in 0190 changes.

### D4. Gates

* `dart format` on the eight files, then `make preflight`.
* `check_records.py --check-all`.

## Verification

The MADR's Confirmation: D2's byte-identity per file and its planted failure, D2's rescan, and
D4's gates.

## Rollout and Rollback

* Comments only, in the app. Rollback is reverting the commit.

## The survey's method

Recorded so it can be repeated (F3). Both scripts lived in the session scratchpad.

* **Dart.** A `///` line is a candidate when the previous `///` line in the same block ends in `.`,
  and the line starts with a capitalised word other than a common continuation (`This`, `It`,
  `If`, `When`, `Tests`, and similar). The first run also skipped `The`, `A` and `An`, which gave
  58 candidates. Allowing them gave 69. Each candidate was read in context.
* **Go.** A `//` line in a block directly above a top-level declaration is a candidate when the
  previous line ends in `.`, and the line starts with the name of a different top-level declaration
  in the same package. That gave 8 candidates, none of them fused.

## Execution record

Not started.
