---
status: complete
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

Approved 2026-10-18 ("committed, proceed. approved", after the records were committed as
`cafded90`). There were no deviations.

### D1, 2026-10-18: the moves

The script's first run stopped before writing anything. Its own expectation for the second
`sessions_screen.dart` site was wrong: once the lower orphan is removed, `ProjectPickerList`'s doc
follows the upper one. The expectation was corrected and the script re-run on the unchanged tree:

```text
features/chat/chat_bubble.dart: lines 468-470 (3) -> above 'class _LimitNotice extends StatefulWidget {'
data/chat/chat_models.dart: lines 5-8 (4) -> above 'class ChatAttachment {'
data/protocol/models.dart: lines 2475-2476 (2) -> above 'class CollaborationMode {'
data/protocol/models.dart: lines 1955-1956 (2) -> above 'class Usage {'
features/sessions/sessions_screen.dart: lines 41-45 (5) -> above 'String sessionUsageLabel(AgentSessionUsage usage) {'
features/sessions/sessions_screen.dart: lines 32-40 (9) -> above 'String _humanTimestamp(BuildContext context, DateTime at) {'
features/chat/chat_screen.dart: lines 3466-3466 (1) -> above 'class _AttachmentThumb extends StatelessWidget {'
features/settings/provider_detail_screen.dart: lines 666-673 (8) -> above 'Future<void> _clearCredential(UpstreamAuth up, {AuthMethod? '
theme/starfield.dart: lines 7-10 (4) -> above 'class StarfieldPainter extends CustomPainter {'
theme/widgets.dart: lines 137-138 (2) -> above 'class ConnBanner extends StatelessWidget {'
wrote 8 files
```

F2 was done by hand. The stale three lines above `_AssistantMarkdown` were removed. Its doc gained
one paragraph: "The reply is selectable once it has finished, and is sized to the surrounding body
text."

### D2, 2026-10-18: only comments moved

```text
ok   apps/mobile/lib/data/chat/chat_models.dart (file changed: True)
ok   apps/mobile/lib/data/protocol/models.dart (file changed: True)
ok   apps/mobile/lib/features/chat/chat_bubble.dart (file changed: True)
ok   apps/mobile/lib/features/chat/chat_screen.dart (file changed: True)
ok   apps/mobile/lib/features/sessions/sessions_screen.dart (file changed: True)
ok   apps/mobile/lib/features/settings/provider_detail_screen.dart (file changed: True)
ok   apps/mobile/lib/theme/starfield.dart (file changed: True)
ok   apps/mobile/lib/theme/widgets.dart (file changed: True)
exit 0
```

The planted run changed `_darkMaxOpacity` from `0.66` to `0.67` in a scratch copy of
`starfield.dart`. The check failed as it should:

```text
DIFF apps/mobile/lib/theme/starfield.dart (file changed: True)
exit 1
```

The rescan gives 58 candidates, down from 69. The 11 lines that went are exactly the MADR's eleven,
and no new candidate appeared.

### D3, 2026-10-18: records

The 0190 PLAN's note strikes through "It was introduced by 0188 N2 (`fc737d36`)". The correction
beside it names `0c55297e` and links the 0197 MADR.

### D4, 2026-10-18: gates

```text
dart format (8 files)         -> exit 0, "Formatted 8 files (0 changed)"
make preflight                -> exit 0: dart format 219 files (0 changed),
                                 flutter analyze "No issues found!",
                                 flutter test "+1473 ~3: All tests passed!"
check_records.py --check-all  -> exit 0
```
