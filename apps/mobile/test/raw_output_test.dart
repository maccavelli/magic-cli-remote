import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/raw_output.dart';

/// The fixtures: real tool output and replies from grok, kilo and codex,
/// recorded by PLAN 0208 P3.0 and redacted. Each provider directory holds an
/// expected.json naming each file's kind.
final _fixtures = Directory('test/fixtures/raw_output');

/// Every fixture under [root] whose kind differs from its expected.json, as
/// `provider/file: got X, want Y`. A file missing from expected.json, or a
/// kind it names that does not exist, is a mismatch too.
List<String> fixtureMismatches(Directory root) {
  final out = <String>[];
  final dirs = root.listSync().whereType<Directory>().toList()
    ..sort((a, b) => a.path.compareTo(b.path));
  for (final dir in dirs) {
    final provider = dir.uri.pathSegments.where((s) => s.isNotEmpty).last;
    final expected =
        jsonDecode(File('${dir.path}/expected.json').readAsStringSync())
            as Map<String, dynamic>;
    final files =
        dir
            .listSync()
            .whereType<File>()
            .where((f) => f.path.endsWith('.txt'))
            .toList()
          ..sort((a, b) => a.path.compareTo(b.path));
    for (final f in files) {
      final name = f.uri.pathSegments.last;
      final want = expected[name] as String?;
      final got = classifyRawOutput(f.readAsStringSync()).name;
      if (want == null || want.isEmpty) {
        out.add('$provider/$name: no expected kind');
      } else if (got != want) {
        out.add('$provider/$name: got $got, want $want');
      }
    }
  }
  return out;
}

void main() {
  test('every recorded fixture is classified as its sidecar says', () {
    expect(_fixtures.existsSync(), isTrue);
    expect(fixtureMismatches(_fixtures), isEmpty);
  });

  test('a fixture with a deliberately wrong sidecar is caught', () {
    // The planted misclassification: grok's real JSON output, declared a diff.
    final tmp = Directory.systemTemp.createTempSync('raw_output_plant');
    addTearDown(() => tmp.deleteSync(recursive: true));
    final dir = Directory('${tmp.path}/planted')..createSync();
    File('${dir.path}/01-tool.txt').writeAsStringSync(
      File('${_fixtures.path}/grok/03-tool.txt').readAsStringSync(),
    );
    File(
      '${dir.path}/expected.json',
    ).writeAsStringSync(jsonEncode({'01-tool.txt': 'diff'}));
    expect(fixtureMismatches(tmp), [
      'planted/01-tool.txt: got json, want diff',
    ]);
  });

  group('unifiedDiff', () {
    test("builds a hunk from a tool's old and new text, and classifies as a "
        'diff', () {
      final d = unifiedDiff(
        '/src/a.go',
        'package a\n\nfunc A() {}\n',
        'package a\n\nimport "fmt"\n\nfunc A() { fmt.Println() }\n',
      );
      expect(
        d,
        '--- a/src/a.go\n'
        '+++ b/src/a.go\n'
        '@@ -1,3 +1,5 @@\n'
        ' package a\n'
        ' \n'
        '-func A() {}\n'
        '+import "fmt"\n'
        '+\n'
        '+func A() { fmt.Println() }',
      );
      expect(classifyRawOutput(d), RawOutputKind.diff);
    });

    test('a new file is all added lines against /dev/null', () {
      expect(
        unifiedDiff('/src/new.go', '', 'package a\n'),
        '--- /dev/null\n+++ b/src/new.go\n@@ -0,0 +1,1 @@\n+package a',
      );
    });
  });

  group('the classifier never mistakes', () {
    test('a Markdown reply with aligned spaces for terminal output', () {
      const md =
          '## Results\n\n- first item  with two spaces\n- second  item\n\n'
          'Some prose.  Two spaces after a stop.';
      expect(classifyRawOutput(md), RawOutputKind.markdown);
    });

    test('one line of prose for anything but plain', () {
      expect(classifyRawOutput('done'), RawOutputKind.plain);
    });

    test('a brace-led line that is not JSON for JSON', () {
      expect(classifyRawOutput('{ not json'), isNot(RawOutputKind.json));
    });
  });
}
