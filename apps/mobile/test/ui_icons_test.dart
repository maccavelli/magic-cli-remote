import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/session_controls/ui_icons.dart';

/// MADR 0123 P9/A19–A20.
///
/// A missing SVG asset renders as a silent blank rather than throwing, so a
/// widget test that only checks "an icon is present" would pass with an empty
/// row. These assert the files on disk instead.
void main() {
  test('every declared glyph exists on disk', () {
    for (final name in UiIcons.all) {
      final file = File('assets/ui_icons/$name.svg');
      expect(
        file.existsSync(),
        isTrue,
        reason:
            '${UiIcons.pathOf(name)} is referenced but not bundled — it would '
            'render as an invisible gap, not an error',
      );
      expect(
        file.readAsStringSync(),
        contains('currentColor'),
        reason: '$name must be tintable, or state cannot be shown by ink',
      );
    }
  });

  test('the ISC notice ships with the glyphs', () {
    // Lucide is ISC: the licence permits redistribution provided the notice
    // travels with the copies (MADR 0123 D15).
    final licence = File('assets/ui_icons/LICENSE');
    expect(licence.existsSync(), isTrue);
    expect(licence.readAsStringSync(), contains('ISC License'));
  });

  test('the asset directory is declared to Flutter', () {
    // Declared in pubspec, or every glyph is missing at runtime while every
    // widget test still passes.
    final pubspec = File('pubspec.yaml').readAsStringSync();
    expect(pubspec, contains('assets/ui_icons/'));
  });

  test('no Material glyph or iOS holdover survives in the controls', () {
    // A19. The row previously mixed Material outlined, Material filled and one
    // iOS glyph, which is why it never read as a set. Guard the regression:
    // reaching for Icons.* here is how the mixture comes back.
    final dir = Directory('lib/features/chat/session_controls');
    for (final f in dir.listSync().whereType<File>()) {
      if (!f.path.endsWith('.dart')) continue;
      final src = f.readAsStringSync();
      for (final banned in ['Icons.ios_share', 'Icons.tune', 'Icons.bolt']) {
        expect(
          src,
          isNot(contains(banned)),
          reason: '${f.uri.pathSegments.last} reintroduced $banned',
        );
      }
    }
  });

  test('declared names are unique', () {
    expect(UiIcons.all.toSet().length, UiIcons.all.length);
  });
}
